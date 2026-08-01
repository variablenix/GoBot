package plugins

import (
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Cheer struct {
	mu            sync.Mutex
	cheers        []string
	lastAutomatic map[string]time.Time
	cooldown      time.Duration
}

func (p *Cheer) Name() string { return "cheer" }

func (p *Cheer) Commands() []string {
	return []string{"cheer", "yay"}
}

func (p *Cheer) Help() string {
	return "!cheer — send a family-friendly cheer; also responds to \\o/ (alias: !yay)"
}

func (p *Cheer) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.cheers = readQuotes(c.String("cheers_file", "quotes/cheers.txt"))
	if len(p.cheers) == 0 {
		p.cheers = defaultCheers()
	}
	cooldown := c.Int("automatic_cooldown_seconds", 15)
	if cooldown < 1 {
		cooldown = 1
	}
	p.cooldown = time.Duration(cooldown) * time.Second
	p.lastAutomatic = make(map[string]time.Time)
	return nil
}

func (p *Cheer) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, _, isCommand := bot.IsCommand(m, b.Config.CommandPrefix)
	if isCommand {
		if cmd != "cheer" && cmd != "yay" {
			return false
		}
		p.send(b, m)
		return true
	}
	if !m.IsChannel || !strings.Contains(m.Text, `\o/`) {
		return false
	}
	key := b.Config.NetworkName + "\x00" + strings.ToLower(m.Target)
	now := time.Now()
	p.mu.Lock()
	last := p.lastAutomatic[key]
	if !last.IsZero() && now.Sub(last) < p.cooldown {
		p.mu.Unlock()
		return true
	}
	p.lastAutomatic[key] = now
	p.mu.Unlock()
	p.send(b, m)
	return true
}

func (p *Cheer) send(b *bot.Bot, m bot.Message) {
	p.mu.Lock()
	if len(p.cheers) == 0 {
		p.mu.Unlock()
		return
	}
	cheer := p.cheers[rand.Intn(len(p.cheers))]
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), cleanExternalText(cheer))
}

func defaultCheers() []string {
	return []string{
		"\\o/ Hooray! \\o/",
		"\u266b Cheer squad activated! \u266b",
		"High fives all around!",
		"That deserves a tiny parade!",
		"Three cheers for excellent teamwork! Hip hip hooray!",
		"\U0001f389 Confetti for everyone! \U0001f389",
	}
}
