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
	Config       Config
	DB           *storage.DB
	Stats        *Stats
	Plugins      []Plugin
	Log          *zap.Logger
	Queue        *Queue
	client       *irc.Client
	mu           sync.RWMutex
	commandMu    sync.Mutex
	lastCommands map[string]time.Time
	lastWarnings map[string]time.Time
	inviteMu     sync.Mutex
	lastInvites  map[string]time.Time
	lastInvite   time.Time
}

func New(cfg Config, db *storage.DB, plugins []Plugin, log *zap.Logger) *Bot {
	return NewWithStats(cfg, db, plugins, log, NewStats())
}

func NewWithStats(cfg Config, db *storage.DB, plugins []Plugin, log *zap.Logger, stats *Stats) *Bot {
	b := &Bot{Config: cfg, DB: db, Stats: stats, Plugins: plugins, Log: log, lastCommands: make(map[string]time.Time), lastWarnings: make(map[string]time.Time), lastInvites: make(map[string]time.Time)}
	b.Queue = NewQueue(cfg.RateLimit.MessagesPerSecond, cfg.RateLimit.Burst, func(o Outgoing) { b.sendNow(o.Target, o.Text) })
	return b
}
func (b *Bot) Send(target, text string) {
	if !b.Queue.Enqueue(Outgoing{target, text}) {
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
		if b.Config.Identity.SASLUser != "" && b.Config.Identity.SASLPass != "" {
			handleSASL(c, m, b.Config.Identity.SASLUser, b.Config.Identity.SASLPass, b.Log)
		}
		b.logIRCEvent(m)
		if m.Command == "INVITE" {
			b.handleInvite(m)
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
		client.Write("CAP LS 302")
	}
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
	client.Write("JOIN " + channel)
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
			if strings.Contains(strings.ToLower(message.Trailing()), "sasl") {
				log.Info("server supports SASL")
				client.Write("CAP REQ :sasl")
			} else {
				log.Warn("server did not advertise SASL")
				client.Write("CAP END")
			}
		case "ACK":
			if strings.Contains(strings.ToLower(message.Trailing()), "sasl") {
				log.Info("server acknowledged SASL capability")
				client.Write("AUTHENTICATE PLAIN")
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
	if _, _, ok := IsCommand(msg, b.Config.CommandPrefix); ok && !b.AllowCommand(msg) {
		return
	}
	command := false
	for _, p := range b.Plugins {
		consumed := false
		func() {
			defer func() {
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
