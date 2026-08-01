package plugins

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

// Car provides short, local car make/model suggestions. Entries include an
// approximate production span so the response remains useful without making
// external requests or pretending to be a vehicle registry.
type Car struct {
	items     []string
	maxLength int
}

func (p *Car) Name() string       { return "car" }
func (p *Car) Commands() []string { return []string{"car", "cars"} }
func (p *Car) Help() string {
	return "!car — suggest a make/model and production span; !cars is an alias"
}

func (p *Car) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.maxLength = c.Int("max_length", 240)
	if p.maxLength < 80 || p.maxLength > 400 {
		p.maxLength = 240
	}
	path := c.String("data_file", filepath.Join("data", "cars.txt"))
	p.items = readFoodList(path)
	if len(p.items) == 0 {
		p.items = []string{"Toyota Corolla (1966-present)", "Honda Civic (1972-present)", "Ford Mustang (1964-present)", "Porsche 911 (1964-present)"}
	}
	return nil
}

func (p *Car) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (strings.ToLower(cmd) != "car" && strings.ToLower(cmd) != "cars") {
		return false
	}
	if len(p.items) == 0 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "the car list is unavailable"))
		return true
	}
	item := p.items[rand.Intn(len(p.items))]
	arg = strings.TrimSpace(arg)
	if arg != "" {
		name := truncateRunes(cleanExternalText(arg), 80)
		b.Send(m.ReplyTarget(), truncateRunes(ircColor(ircCyan, fmt.Sprintf("Car pick for %s: %s", name, item)), p.maxLength))
		return true
	}
	b.Send(m.ReplyTarget(), truncateRunes(ircColor(ircCyan, "Car pick: "+item), p.maxLength))
	return true
}
