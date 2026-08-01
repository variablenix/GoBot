package plugins

import (
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const defaultWelcomeFile = "data/welcome.txt"

var fallbackWelcomeLines = []string{
	"The Evil Has Landed.",
	"A wild plot twist has entered the channel.",
	"Alert: the guest list just got more interesting.",
	"The channel has acquired another witness.",
	"Incoming personality detected. Please remain calm.",
}

// Welcome adds occasional, short join greetings. It is deliberately event
// driven and cooldown-limited so a busy channel does not become a wall of bot
// messages.
type Welcome struct {
	mu          sync.Mutex
	probability float64
	cooldown    time.Duration
	lines       []string
	last        map[string]time.Time
}

func (p *Welcome) Name() string       { return "welcome" }
func (p *Welcome) Commands() []string { return nil }
func (p *Welcome) Help() string {
	return "occasionally posts a short, playful line when someone joins a channel"
}

func (p *Welcome) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.probability = c.Float("probability", 0.15)
	if p.probability < 0 {
		p.probability = 0
	}
	if p.probability > 1 {
		p.probability = 1
	}
	cooldownSeconds := c.Int("cooldown_seconds", 120)
	if cooldownSeconds < 1 {
		cooldownSeconds = 120
	}
	p.cooldown = time.Duration(cooldownSeconds) * time.Second
	p.lines = readQuotes(c.String("messages_file", defaultWelcomeFile))
	if len(p.lines) == 0 {
		p.lines = append([]string(nil), fallbackWelcomeLines...)
	}
	p.last = make(map[string]time.Time)
	return nil
}

// Handle is intentionally empty: welcome reacts to JOIN events through the
// EventHandler hook and never treats ordinary chat as a reason to speak.
func (p *Welcome) Handle(_ *bot.Bot, _ bot.Message) bool { return false }

func (p *Welcome) HandleEvent(b *bot.Bot, m bot.Message) bool {
	if m.Command != "JOIN" || !m.IsChannel || m.Nick == "" || b.ChannelWarming(m.Target) {
		return false
	}
	if strings.EqualFold(m.Nick, b.Config.Identity.Nick) {
		return false
	}

	key := strings.ToLower(b.Config.NetworkName + "\x00" + m.Target)
	now := time.Now()
	p.mu.Lock()
	if last, ok := p.last[key]; ok && now.Sub(last) < p.cooldown {
		p.mu.Unlock()
		return false
	}
	if rand.Float64() >= p.probability {
		p.mu.Unlock()
		return false
	}
	p.last[key] = now
	line := p.lines[rand.Intn(len(p.lines))]
	p.mu.Unlock()

	line = strings.ReplaceAll(line, "{nick}", cleanExternalText(m.Nick))
	line = cleanExternalText(line)
	if line == "" {
		return false
	}
	b.Send(m.Target, ircColor(ircCyan, "[Welcome] "+line))
	return true
}
