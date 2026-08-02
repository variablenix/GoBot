package bot

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/variablenix/GoBot/storage"
	"go.uber.org/zap"
	"gopkg.in/irc.v3"
)

type Bot struct {
	Config         Config
	DB             *storage.DB
	Stats          *Stats
	Plugins        []Plugin
	Log            *zap.Logger
	Queue          *Queue
	client         *irc.Client
	reloadHandler  func(Message)
	mu             sync.RWMutex
	configMu       sync.RWMutex
	pluginMu       sync.RWMutex
	enabledPlugins map[string]bool
	startedPlugins map[string]bool
	commandMu      sync.Mutex
	lastCommands   map[string]time.Time
	lastWarnings   map[string]time.Time
	inviteMu       sync.Mutex
	lastInvites    map[string]time.Time
	lastInvite     time.Time
	warmupMu       sync.RWMutex
	warmupUntil    map[string]time.Time
}

func New(cfg Config, db *storage.DB, plugins []Plugin, log *zap.Logger) *Bot {
	return NewWithStats(cfg, db, plugins, log, NewStats())
}

func NewWithStats(cfg Config, db *storage.DB, plugins []Plugin, log *zap.Logger, stats *Stats) *Bot {
	enabledPlugins := make(map[string]bool, len(plugins))
	startedPlugins := make(map[string]bool, len(plugins))
	for _, plugin := range plugins {
		enabledPlugins[plugin.Name()] = true
	}
	b := &Bot{Config: cfg, DB: db, Stats: stats, Plugins: plugins, Log: log, enabledPlugins: enabledPlugins, startedPlugins: startedPlugins, lastCommands: make(map[string]time.Time), lastWarnings: make(map[string]time.Time), lastInvites: make(map[string]time.Time), warmupUntil: make(map[string]time.Time)}
	b.Queue = NewQueue(cfg.RateLimit.MessagesPerSecond, cfg.RateLimit.Burst, func(o Outgoing) { b.sendNow(o.Target, o.Text) })
	return b
}

// SetReloadHandler installs the owner-only private-message callback used by
// the process to reload plugin configuration in place.
func (b *Bot) SetReloadHandler(handler func(Message)) {
	b.mu.Lock()
	b.reloadHandler = handler
	b.mu.Unlock()
}

// ReloadPlugins applies configuration to active plugins that explicitly
// support runtime reloads and changes global enablement. Connection,
// identity, channel, owner, and per-channel override settings are handled
// separately by the process reload callback. Changes are applied independently
// so one plugin failure does not prevent unrelated safe changes.
func (b *Bot) ReloadPlugins(configs map[string]PluginConfig) (int, error) {
	b.pluginMu.Lock()
	defer b.pluginMu.Unlock()
	count := 0
	var firstErr error
	for _, p := range b.Plugins {
		config := configs[p.Name()]
		enabled := config.Bool("enabled", true)
		wasEnabled := b.enabledPlugins[p.Name()]
		if !enabled {
			if wasEnabled {
				if stopper, ok := p.(Stopper); ok {
					stopper.Stop(b)
					b.startedPlugins[p.Name()] = false
				}
				count++
			}
			b.enabledPlugins[p.Name()] = false
			continue
		}
		if !wasEnabled {
			if err := p.Init(config, b.DB); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", p.Name(), err)
				}
				continue
			}
			b.enabledPlugins[p.Name()] = true
			if starter, ok := p.(Starter); ok && !b.startedPlugins[p.Name()] {
				starter.Start(b)
				b.startedPlugins[p.Name()] = true
			}
			count++
			continue
		}
		reloadable, ok := p.(Reloadable)
		if !ok {
			b.enabledPlugins[p.Name()] = true
			continue
		}
		if err := reloadable.Reload(config); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", p.Name(), err)
			}
			continue
		}
		b.enabledPlugins[p.Name()] = true
		count++
	}
	return count, firstErr
}

// SetPluginEnabled records the initial global plugin enablement after all
// plugins have been initialized. ReloadPlugins handles later transitions.
func (b *Bot) SetPluginEnabled(name string, enabled bool) {
	b.pluginMu.Lock()
	b.enabledPlugins[name] = enabled
	b.pluginMu.Unlock()
}

// MarkPluginStarted records a plugin background worker started by the process
// during initial setup, preventing a later reload from starting it twice.
func (b *Bot) MarkPluginStarted(name string) {
	b.pluginMu.Lock()
	b.startedPlugins[name] = true
	b.pluginMu.Unlock()
}

func (b *Bot) pluginEnabled(name string) bool {
	b.pluginMu.RLock()
	enabled, configured := b.enabledPlugins[name]
	b.pluginMu.RUnlock()
	return !configured || enabled
}

// PluginEnabled reports whether a plugin is globally enabled. Channel
// overrides are intentionally not considered here.
func (b *Bot) PluginEnabled(name string) bool {
	return b.pluginEnabled(name)
}

// ReloadPluginOverrides replaces the per-channel plugin overrides used by
// this network. The map is copied so the caller can safely discard or reuse
// the newly loaded configuration after the reload completes.
func (b *Bot) ReloadPluginOverrides(overrides map[string]map[string]bool) {
	b.configMu.Lock()
	b.Config.PluginOverrides = clonePluginOverrides(overrides)
	b.configMu.Unlock()
}

func clonePluginOverrides(overrides map[string]map[string]bool) map[string]map[string]bool {
	if overrides == nil {
		return nil
	}
	clone := make(map[string]map[string]bool, len(overrides))
	for channel, plugins := range overrides {
		pluginClone := make(map[string]bool, len(plugins))
		for plugin, enabled := range plugins {
			pluginClone[plugin] = enabled
		}
		clone[channel] = pluginClone
	}
	return clone
}
func (b *Bot) Send(target, text string) {
	if !b.Queue.Enqueue(Outgoing{target, text}) {
		b.Stats.dropped.Add(1)
		b.Log.Warn("outgoing queue full", zap.String("target", target))
	}
}
func (b *Bot) sendNow(target, text string) {
	b.mu.RLock()
	c := b.client
	b.mu.RUnlock()
	if c != nil {
		c.WriteMessage(&irc.Message{Command: "PRIVMSG", Params: []string{target, text}})
		b.Stats.sent.Add(1)
	}
}
func (b *Bot) connect(ctx context.Context) error {
	if !b.Config.Server.TLS && b.Config.Identity.SASLUser != "" && b.Config.Identity.SASLPass != "" {
		return fmt.Errorf("refusing to send IRC authentication credentials without TLS")
	}
	server := net.JoinHostPort(b.Config.Server.Host, fmt.Sprintf("%d", b.Config.Server.Port))
	var conn net.Conn
	var err error
	if b.Config.Server.TLS {
		conn, err = tls.Dial("tcp", server, &tls.Config{ServerName: b.Config.Server.Host, InsecureSkipVerify: !b.Config.Server.VerifyCert})
	} else {
		conn, err = net.Dial("tcp", server)
	}
	if err != nil {
		return err
	}
	errc := make(chan error, 1)
	client := irc.NewClient(conn, irc.ClientConfig{Nick: b.Config.Identity.Nick, User: b.Config.Identity.User, Name: b.Config.Identity.Realname, Handler: irc.HandlerFunc(func(c *irc.Client, m *irc.Message) {
		handleSASL(c, m, b.Config.Identity.SASLUser, b.Config.Identity.SASLPass, b.Log)
		b.logIRCEvent(m)
		if m.Command == "INVITE" {
			b.handleInvite(m)
		}
		if m.Command == "JOIN" {
			b.dispatchEvent(ParseMessage(m))
		}
		if m.Command == "001" {
			actualNick := ""
			if len(m.Params) > 0 {
				actualNick = m.Params[0]
			}
			b.Log.Info("connected to IRC",
				zap.String("network", b.Config.NetworkName),
				zap.String("server", b.Config.Server.Host),
				zap.String("requested_nick", b.Config.Identity.Nick),
				zap.String("actual_nick", actualNick),
			)
			if b.shouldUseNickServFallback() {
				go b.nickServFallback(c, actualNick)
			}
			for _, ch := range b.Config.Channels {
				b.markChannelWarmup(ch)
				c.Write("JOIN " + ch)
			}
		}
		if m.Command == "PRIVMSG" {
			b.Stats.received.Add(1)
			b.dispatch(ParseMessage(m))
		}
	})})
	b.mu.Lock()
	b.client = client
	b.mu.Unlock()
	b.Stats.connected.Store(1)
	if b.Config.Identity.SASLUser != "" && b.Config.Identity.SASLPass != "" {
		// irc.v3 has CAP parsing but no SASL mechanism implementation. Start
		// the capability exchange manually so PLAIN can complete before 001.
		b.Log.Info("starting SASL authentication",
			zap.String("network", b.Config.NetworkName),
			zap.String("server", b.Config.Server.Host),
			zap.String("account", b.Config.Identity.SASLUser),
		)
	} else {
		b.Log.Info("starting IRC capability negotiation", zap.String("network", b.Config.NetworkName), zap.String("server", b.Config.Server.Host))
	}
	client.Write("CAP LS 302")
	go func() { errc <- client.Run() }()
	select {
	case <-ctx.Done():
		client.Write("QUIT :shutting down")
		conn.Close()
		<-errc
		b.Stats.connected.Store(0)
		return nil
	case err := <-errc:
		b.Stats.connected.Store(0)
		b.mu.Lock()
		b.client = nil
		b.mu.Unlock()
		return err
	}
}

func (b *Bot) handleInvite(m *irc.Message) {
	if !b.Config.Invites.Enabled || len(m.Params) < 2 {
		return
	}
	if !strings.EqualFold(m.Params[0], b.Config.Identity.Nick) {
		return
	}
	channel := m.Params[1]
	if !validChannelName(channel) {
		return
	}
	cooldown := time.Duration(b.Config.Invites.CooldownSeconds) * time.Second
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	now := time.Now()
	key := strings.ToLower(channel)
	b.inviteMu.Lock()
	if !b.lastInvite.IsZero() && now.Sub(b.lastInvite) < cooldown {
		b.inviteMu.Unlock()
		return
	}
	last, exists := b.lastInvites[key]
	if exists && now.Sub(last) < cooldown {
		b.inviteMu.Unlock()
		return
	}
	b.lastInvites[key] = now
	b.lastInvite = now
	b.inviteMu.Unlock()

	b.mu.RLock()
	client := b.client
	b.mu.RUnlock()
	if client == nil {
		return
	}
	b.Log.Info("joining invited channel", zap.String("network", b.Config.NetworkName), zap.String("channel", channel), zap.String("invited_by", m.Name))
	b.markChannelWarmup(channel)
	client.Write("JOIN " + channel)
}

func (b *Bot) markChannelWarmup(channel string) {
	duration := time.Duration(b.Config.RateLimit.JoinWarmupSeconds) * time.Second
	if duration <= 0 {
		duration = 10 * time.Second
	}
	b.warmupMu.Lock()
	b.warmupUntil[strings.ToLower(channel)] = time.Now().Add(duration)
	b.warmupMu.Unlock()
}

func (b *Bot) channelWarming(channel string) bool {
	b.warmupMu.RLock()
	until := b.warmupUntil[strings.ToLower(channel)]
	b.warmupMu.RUnlock()
	return !until.IsZero() && time.Now().Before(until)
}

// ChannelWarming reports whether a channel is still inside the post-join
// backlog protection window. Event-driven plugins can use this to avoid
// reacting to replayed join/activity events during startup.
func (b *Bot) ChannelWarming(channel string) bool {
	return b.channelWarming(channel)
}

// PluginEnabledForChannel reports whether a plugin is allowed to operate in
// a channel. Per-channel overrides are intentionally opt-out: a channel that
// is not listed, or a plugin that is not listed for that channel, keeps the
// global plugin setting.
func (b *Bot) PluginEnabledForChannel(pluginName, channel string) bool {
	if !b.pluginEnabled(pluginName) {
		return false
	}
	channel = strings.TrimSpace(channel)
	if channel == "" {
		return true
	}
	b.configMu.RLock()
	overrides := b.Config.PluginOverrides
	for configuredChannel, channelOverrides := range overrides {
		if !strings.EqualFold(strings.TrimSpace(configuredChannel), channel) {
			continue
		}
		for configuredPlugin, enabled := range channelOverrides {
			if strings.EqualFold(strings.TrimSpace(configuredPlugin), pluginName) {
				b.configMu.RUnlock()
				return enabled
			}
		}
	}
	b.configMu.RUnlock()
	return true
}

func validChannelName(channel string) bool {
	if len(channel) < 2 || len(channel) > 200 || (channel[0] != '#' && channel[0] != '&') {
		return false
	}
	return !strings.ContainsAny(channel, " \r\n,\x00\x07")
}

// IsOwner checks an authenticated IRC account tag against the configured
// owner accounts. Nicknames alone are intentionally not accepted.
func (b *Bot) IsOwner(m Message) bool {
	account := strings.TrimSpace(m.Account)
	if account == "" || account == "*" {
		return false
	}
	for _, owner := range b.Config.OwnerAccounts {
		if strings.EqualFold(strings.TrimSpace(owner), account) {
			return true
		}
	}
	return false
}

// AllowCommand prevents one sender from repeatedly triggering command
// handlers. The warning has its own cooldown so rejected commands cannot
// flood the channel with cooldown notices.
func (b *Bot) AllowCommand(m Message) bool {
	if commandBypassesCooldown(m, b.Config.CommandPrefix) {
		return true
	}
	cooldown := time.Duration(b.Config.RateLimit.CommandCooldownSeconds) * time.Second
	if cooldown <= 0 {
		cooldown = 2 * time.Second
	}
	warningCooldown := time.Duration(b.Config.RateLimit.CommandWarningCooldownSeconds) * time.Second
	if warningCooldown <= 0 {
		warningCooldown = 10 * time.Second
	}
	key := commandRateKey(m)
	now := time.Now()
	b.commandMu.Lock()
	if b.lastCommands == nil {
		b.lastCommands = make(map[string]time.Time)
	}
	if b.lastWarnings == nil {
		b.lastWarnings = make(map[string]time.Time)
	}
	last, exists := b.lastCommands[key]
	if exists && now.Sub(last) < cooldown {
		lastWarning, warned := b.lastWarnings[key]
		if !warned || now.Sub(lastWarning) >= warningCooldown {
			b.lastWarnings[key] = now
			b.commandMu.Unlock()
			b.Send(m.ReplyTarget(), "command cooldown—please wait a moment")
			return false
		}
		b.commandMu.Unlock()
		return false
	}
	b.lastCommands[key] = now
	b.commandMu.Unlock()
	return true
}

func commandBypassesCooldown(m Message, prefix string) bool {
	cmd, arg, ok := IsCommand(m, prefix)
	if !ok {
		return false
	}
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	arg = strings.ToLower(strings.TrimSpace(arg))
	switch cmd {
	case "hit", "stand", "double":
		return true
	case "21", "bj", "blackjack":
		return arg == "hit" || arg == "stand" || arg == "double" || arg == "h" || arg == "s" || arg == "d"
	case "poll":
		return arg == "vote" || arg == "results" || arg == "status" || arg == "close" || arg == "end" || strings.HasPrefix(arg, "vote ")
	default:
		return false
	}
}

func commandRateKey(m Message) string {
	if account := strings.TrimSpace(m.Account); account != "" && account != "*" {
		return "account:" + strings.ToLower(account)
	}
	return "user:" + strings.ToLower(strings.Join([]string{m.Nick, m.User, m.Host}, "\x00"))
}

func handleSASL(client *irc.Client, message *irc.Message, username, password string, log *zap.Logger) {
	switch message.Command {
	case "CAP":
		if len(message.Params) < 2 {
			return
		}
		subcommand := strings.ToUpper(message.Params[1])
		switch subcommand {
		case "LS":
			request := capabilityRequest(message.Trailing(), username != "" && password != "")
			if request != "" {
				if hasCapability(message.Trailing(), "sasl") {
					log.Info("server supports SASL")
				}
				if hasCapability(message.Trailing(), "account-tag") {
					log.Info("server supports account-tag")
				}
				if hasCapability(message.Trailing(), "server-time") {
					log.Info("server supports server-time")
				}
				client.Write("CAP REQ :" + request)
			} else {
				log.Warn("server did not advertise requested IRC capabilities")
				client.Write("CAP END")
			}
		case "ACK":
			if hasCapability(message.Trailing(), "sasl") && username != "" && password != "" {
				log.Info("server acknowledged SASL capability")
				client.Write("AUTHENTICATE PLAIN")
			} else {
				client.Write("CAP END")
			}
		}
	case "AUTHENTICATE":
		if message.Trailing() == "+" {
			log.Info("sending SASL credentials")
			payload := base64.StdEncoding.EncodeToString([]byte("\x00" + username + "\x00" + password))
			client.Write("AUTHENTICATE " + payload)
		}
	case "903":
		log.Info("SASL authentication succeeded")
		client.Write("CAP END")
	case "904", "905", "906", "907":
		log.Warn("SASL authentication failed",
			zap.String("code", message.Command),
			zap.String("detail", message.Trailing()),
		)
		client.Write("CAP END")
	}
}

func capabilityRequest(advertised string, saslEnabled bool) string {
	requested := make([]string, 0, 3)
	if saslEnabled && hasCapability(advertised, "sasl") {
		requested = append(requested, "sasl")
	}
	if hasCapability(advertised, "account-tag") {
		requested = append(requested, "account-tag")
	}
	if hasCapability(advertised, "server-time") {
		requested = append(requested, "server-time")
	}
	return strings.Join(requested, " ")
}

func hasCapability(advertised, wanted string) bool {
	for _, capability := range strings.Fields(strings.ToLower(advertised)) {
		if strings.SplitN(capability, "=", 2)[0] == strings.ToLower(wanted) {
			return true
		}
	}
	return false
}

func (b *Bot) shouldUseNickServFallback() bool {
	return b.Config.Identity.NickServFallback && b.Config.Identity.SASLUser != "" && b.Config.Identity.SASLPass != ""
}

func (b *Bot) nickServFallback(client *irc.Client, actualNick string) {
	time.Sleep(2 * time.Second)

	account := b.Config.Identity.SASLUser
	password := b.Config.Identity.SASLPass
	desiredNick := b.Config.Identity.Nick

	if account == "" || password == "" {
		return
	}

	if actualNick != "" && desiredNick != "" && actualNick != desiredNick && b.Config.Identity.NickServGhost {
		b.Log.Warn("bot connected with a different nick; attempting NickServ ghost recovery",
			zap.String("network", b.Config.NetworkName),
			zap.String("requested_nick", desiredNick),
			zap.String("actual_nick", actualNick),
		)
		client.Write(fmt.Sprintf("PRIVMSG NickServ :GHOST %s %s", desiredNick, password))
		time.Sleep(1 * time.Second)
		client.Write("NICK " + desiredNick)
		time.Sleep(1 * time.Second)
		actualNick = desiredNick
	}

	if actualNick != "" && desiredNick != "" && actualNick != desiredNick {
		b.Log.Warn("bot is not using the requested nick; skipping NickServ IDENTIFY fallback",
			zap.String("network", b.Config.NetworkName),
			zap.String("requested_nick", desiredNick),
			zap.String("actual_nick", actualNick),
		)
		return
	}

	b.Log.Info("sending NickServ IDENTIFY fallback",
		zap.String("network", b.Config.NetworkName),
		zap.String("account", account),
	)
	client.Write(fmt.Sprintf("PRIVMSG NickServ :IDENTIFY %s %s", account, password))
}

func (b *Bot) logIRCEvent(m *irc.Message) {
	switch m.Command {
	case "433":
		b.Log.Warn("nickname already in use",
			zap.String("network", b.Config.NetworkName),
			zap.String("requested_nick", b.Config.Identity.Nick),
			zap.String("detail", m.Trailing()),
		)
	case "900":
		b.Log.Info("account login reported by server",
			zap.String("network", b.Config.NetworkName),
			zap.String("detail", m.Trailing()),
		)
	case "903":
		b.Log.Info("server confirmed SASL login",
			zap.String("network", b.Config.NetworkName),
		)
	case "904", "905", "906", "907":
		b.Log.Warn("server reported SASL issue",
			zap.String("network", b.Config.NetworkName),
			zap.String("code", m.Command),
			zap.String("detail", m.Trailing()),
		)
	}
}
func (b *Bot) dispatch(msg Message) {
	if b.handlePrivateReload(msg) {
		return
	}
	if msg.Command == "PRIVMSG" && msg.IsChannel && b.channelWarming(msg.Target) {
		return
	}
	if _, _, ok := IsCommand(msg, b.Config.CommandPrefix); ok && !b.AllowCommand(msg) {
		return
	}
	command := false
	for _, p := range b.Plugins {
		if !b.pluginEnabled(p.Name()) {
			continue
		}
		if msg.IsChannel && !b.PluginEnabledForChannel(p.Name(), msg.Target) {
			continue
		}
		consumed := false
		b.pluginMu.RLock()
		func() {
			defer func() {
				b.pluginMu.RUnlock()
				if r := recover(); r != nil {
					b.Log.Error("plugin panic", zap.String("plugin", p.Name()), zap.Any("panic", r))
				}
			}()
			consumed = p.Handle(b, msg)
		}()
		if consumed {
			if command {
				b.Stats.commands.Add(1)
			}
			return
		}
		if _, _, ok := IsCommand(msg, b.Config.CommandPrefix); ok {
			command = true
		}
	}
}

func (b *Bot) dispatchEvent(msg Message) {
	for _, p := range b.Plugins {
		if !b.pluginEnabled(p.Name()) {
			continue
		}
		if msg.IsChannel && !b.PluginEnabledForChannel(p.Name(), msg.Target) {
			continue
		}
		handler, ok := p.(EventHandler)
		if !ok {
			continue
		}
		b.pluginMu.RLock()
		func() {
			defer func() {
				b.pluginMu.RUnlock()
				if r := recover(); r != nil {
					b.Log.Error("plugin event panic", zap.String("plugin", p.Name()), zap.Any("panic", r))
				}
			}()
			handler.HandleEvent(b, msg)
		}()
	}
}

func (b *Bot) handlePrivateReload(msg Message) bool {
	if msg.Command != "PRIVMSG" || msg.IsChannel || !b.IsOwner(msg) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(msg.Text))
	if text != "reload" && text != "!reload" {
		return false
	}
	b.mu.RLock()
	handler := b.reloadHandler
	b.mu.RUnlock()
	if handler != nil {
		handler(msg)
	}
	return true
}
func (b *Bot) Run(ctx context.Context) error {
	backoff := 5 * time.Second
	for {
		err := b.connect(ctx)
		if ctx.Err() != nil {
			return nil
		}
		b.Stats.reconnects.Add(1)
		b.Log.Warn("IRC connection ended", zap.Error(err), zap.Duration("retry_in", backoff))
		jitter := time.Duration(rand.Int63n(int64(backoff/5 + 1)))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff + jitter):
		}
		if backoff < 5*time.Minute {
			backoff *= 2
			if backoff > 5*time.Minute {
				backoff = 5 * time.Minute
			}
		}
	}
}
func IsCommand(msg Message, prefix string) (string, string, bool) {
	if prefix == "" || (!msg.IsChannel && msg.Target == "") {
		return "", "", false
	}
	if !strings.HasPrefix(msg.Text, prefix) {
		return "", "", false
	}
	parts := strings.Fields(strings.TrimSpace(strings.TrimPrefix(msg.Text, prefix)))
	if len(parts) == 0 {
		return "", "", false
	}
	return strings.ToLower(parts[0]), strings.TrimSpace(strings.TrimPrefix(msg.Text, prefix+parts[0])), true
}
