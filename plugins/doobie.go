package plugins

import (
	"math/rand"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const (
	doobieDefaultDelaySeconds    = 1
	doobieDefaultCooldownSeconds = 15
)

var doobieInlineRX = regexp.MustCompile(`(?i)(^|[^a-z0-9_])\$(doobie|dooblie|420)([^a-z0-9_]|$)`)

var doobieCountdownSteps = []string{
	"📜 3... Grinding...",
	"👅 2... Rolling...",
	"🔥 1... Sparking...",
}

var doobieFallbackQuotes = []string{
	"Spark it up and pass it around! 🔥🍁",
	"Good vibes are better when they make the whole circle. 🌿",
	"Pass the peace, keep the laughter, and enjoy the moment. ✌️",
	"The session is officially in orbit. 🚀🌿",
	"Stay lifted, stay kind, and pass it on. 🔥",
	"A little green, a lot of good energy. 🍁✨",
	"The vibe check says: share the good mood. 😌🌿",
	"Roll with it—the best sessions are communal. 🔥",
	"Breathe in the good vibes, pass out the good vibes. 🌿✨",
	"Keep calm and let the mellow commence. 😎🍁",
}

type Doobie struct {
	mu       sync.Mutex
	delay    time.Duration
	cooldown time.Duration
	quotes   []string
	last     map[string]time.Time
	active   map[string]*doobieSequence
}

type doobieSequence struct {
	target string
	step   int
	timer  *time.Timer
}

func (p *Doobie) Name() string       { return "doobie" }
func (p *Doobie) Commands() []string { return []string{"doobie", "420"} }
func (p *Doobie) Help() string {
	return "!doobie or !420 — start a 3-2-1 smoke countdown; $doobie and $420 in channel text do the same"
}

func (p *Doobie) Init(c bot.PluginConfig, _ *storage.DB) error {
	delaySeconds := c.Int("countdown_delay_seconds", doobieDefaultDelaySeconds)
	if delaySeconds < 0 || delaySeconds > 60 {
		delaySeconds = doobieDefaultDelaySeconds
	}
	cooldownSeconds := c.Int("cooldown_seconds", doobieDefaultCooldownSeconds)
	if cooldownSeconds < 1 || cooldownSeconds > 3600 {
		cooldownSeconds = doobieDefaultCooldownSeconds
	}
	p.mu.Lock()
	p.delay = time.Duration(delaySeconds) * time.Second
	p.cooldown = time.Duration(cooldownSeconds) * time.Second
	p.quotes = readQuotes(c.String("quotes_file", filepath.Join("quotes", "doobie.txt")))
	if len(p.quotes) == 0 {
		p.quotes = append([]string(nil), doobieFallbackQuotes...)
	}
	p.last = make(map[string]time.Time)
	p.active = make(map[string]*doobieSequence)
	p.mu.Unlock()
	return nil
}

// Stop prevents a countdown from continuing after the plugin is disabled.
func (p *Doobie) Stop(_ *bot.Bot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, sequence := range p.active {
		if sequence.timer != nil {
			sequence.timer.Stop()
		}
	}
	p.active = make(map[string]*doobieSequence)
	p.last = make(map[string]time.Time)
}

func (p *Doobie) Handle(b *bot.Bot, m bot.Message) bool {
	if strings.TrimSpace(m.Nick) == "" {
		return false
	}
	cmd, _, isCommand := bot.IsCommand(m, b.Config.CommandPrefix)
	triggered := false
	if isCommand {
		if !isDoobieCommand(cmd) {
			return false
		}
		triggered = true
	} else if m.IsChannel && doobieInlineRX.MatchString(m.Text) {
		triggered = true
	}
	if !triggered {
		return false
	}

	key := doobieSenderKey(b.Config.NetworkName, m)
	channelKey := doobieChannelKey(b.Config.NetworkName, m)
	if !p.allow(key) {
		return true
	}
	p.start(b, m.ReplyTarget(), channelKey)
	return true
}

func isDoobieCommand(command string) bool {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "doobie", "420":
		return true
	default:
		return false
	}
}

func doobieSenderKey(network string, m bot.Message) string {
	if account := strings.TrimSpace(m.Account); account != "" && account != "*" {
		return "account:" + strings.ToLower(network) + "\x00" + strings.ToLower(account)
	}
	return "sender:" + strings.ToLower(network) + "\x00" + strings.ToLower(strings.Join([]string{m.Nick, m.User, m.Host}, "\x00"))
}

func doobieChannelKey(network string, m bot.Message) string {
	target := m.Target
	if target == "" {
		target = m.Nick
	}
	return strings.ToLower(network) + "\x00" + strings.ToLower(target)
}

func (p *Doobie) allow(key string) bool {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if last, ok := p.last[key]; ok && now.Sub(last) < p.cooldown {
		return false
	}
	p.last[key] = now
	return true
}

func (p *Doobie) start(b *bot.Bot, target, channelKey string) {
	p.mu.Lock()
	if _, exists := p.active[channelKey]; exists {
		p.mu.Unlock()
		return
	}
	sequence := &doobieSequence{target: target}
	p.active[channelKey] = sequence
	p.mu.Unlock()

	// The first line is sent immediately; scheduling afterward preserves the
	// countdown order even when countdown_delay_seconds is configured as zero.
	b.Send(target, doobieCountdownSteps[0])
	p.schedule(b, channelKey, sequence)
}

func (p *Doobie) schedule(b *bot.Bot, channelKey string, sequence *doobieSequence) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if current := p.active[channelKey]; current != sequence {
		return
	}
	sequence.timer = time.AfterFunc(p.delay, func() { p.advance(b, channelKey, sequence) })
}

func (p *Doobie) advance(b *bot.Bot, channelKey string, sequence *doobieSequence) {
	p.mu.Lock()
	if current := p.active[channelKey]; current != sequence {
		p.mu.Unlock()
		return
	}
	if !b.PluginEnabledForChannel(p.Name(), sequence.target) {
		delete(p.active, channelKey)
		p.mu.Unlock()
		return
	}
	sequence.step++
	if sequence.step >= len(doobieCountdownSteps) {
		delete(p.active, channelKey)
		p.mu.Unlock()
		b.Send(sequence.target, p.randomClosingLine())
		return
	}
	line := doobieCountdownSteps[sequence.step]
	sequence.timer = time.AfterFunc(p.delay, func() { p.advance(b, channelKey, sequence) })
	p.mu.Unlock()

	b.Send(sequence.target, line)
}

func (p *Doobie) randomClosingLine() string {
	p.mu.Lock()
	quote := ""
	if len(p.quotes) > 0 {
		quote = p.quotes[rand.Intn(len(p.quotes))]
	}
	p.mu.Unlock()
	if quote == "" {
		quote = doobieFallbackQuotes[0]
	}
	return truncateIRCMessage(ircColor(ircGreen, cleanExternalText(strings.TrimSpace(quote))), 240)
}
