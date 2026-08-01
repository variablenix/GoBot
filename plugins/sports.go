package plugins

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

// Sports provides short, local sports suggestions. The list is intentionally
// data-driven so operators can add regional, adaptive, or emerging sports
// without changing the plugin.
type Sports struct {
	items     []string
	maxLength int
}

func (p *Sports) Name() string       { return "sports" }
func (p *Sports) Commands() []string { return []string{"sports", "sport"} }
func (p *Sports) Help() string {
	return "!sports — suggest a random sport; !sport is an alias"
}

func (p *Sports) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.maxLength = c.Int("max_length", 200)
	if p.maxLength < 80 || p.maxLength > 400 {
		p.maxLength = 200
	}
	path := c.String("data_file", filepath.Join("data", "sports.txt"))
	p.items = readFoodList(path)
	if len(p.items) == 0 {
		p.items = []string{"athletics", "basketball", "football", "golf", "swimming", "tennis"}
	}
	return nil
}

func (p *Sports) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "sports" && cmd != "sport") {
		return false
	}
	if len(p.items) == 0 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "the sports list is unavailable"))
		return true
	}

	item := p.items[rand.Intn(len(p.items))]
	arg = strings.TrimSpace(arg)
	if arg != "" {
		name := truncateRunes(cleanExternalText(arg), 80)
		b.Send(m.ReplyTarget(), truncateRunes(ircColor(ircCyan, fmt.Sprintf("Sports pick for %s: %s", name, item)), p.maxLength))
		return true
	}
	b.Send(m.ReplyTarget(), truncateRunes(ircColor(ircCyan, "Sports pick: "+item), p.maxLength))
	return true
}
