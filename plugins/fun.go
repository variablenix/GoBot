package plugins

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

// Fun serves short, local text from operator-editable files.
// Keeping these catalogs local avoids external API failures and lets operators
// curate the tone for their own channels.
type Fun struct {
	items     map[string][]string
	maxLength int
}

var funCategories = []string{"yomomma", "oneliner", "pun", "wisdom"}

var funFiles = map[string]string{
	"yomomma":  "yo_momma.txt",
	"oneliner": "one_liners.txt",
	"pun":      "puns.txt",
	"wisdom":   "wisdom.txt",
}

func (p *Fun) Name() string { return "fun" }

func (p *Fun) Commands() []string {
	return []string{"yomomma", "yo", "yo-momma", "oneliner", "oneliners", "one", "pun", "puns", "wisdom", "wise"}
}

func (p *Fun) Help() string {
	return "!yomomma/!yo — random yo-momma joke; !oneliner/!one — random one-liner; !pun — random pun; !wisdom/!wise — short wisdom line"
}

func (p *Fun) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.maxLength = c.Int("max_length", 240)
	if p.maxLength < 80 || p.maxLength > 400 {
		p.maxLength = 240
	}
	dataDir := c.String("data_dir", "data/fun")
	p.items = make(map[string][]string, len(funCategories))
	for _, category := range funCategories {
		p.items[category] = readFoodList(filepath.Join(dataDir, funFiles[category]))
		if len(p.items[category]) == 0 {
			p.items[category] = funFallbacks[category]
		}
	}
	return nil
}

func (p *Fun) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, _, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok {
		return false
	}
	category, ok := funCommandCategory(cmd)
	if !ok {
		return false
	}
	items := p.items[category]
	if len(items) == 0 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "that fun-text catalog is unavailable"))
		return true
	}
	line := strings.TrimSpace(items[rand.Intn(len(items))])
	if category == "yomomma" {
		line = "Yo momma " + line
	}
	label := map[string]string{"yomomma": "Yo-momma", "oneliner": "One-liner", "pun": "Pun", "wisdom": "Wisdom"}[category]
	result := fmt.Sprintf("%s: %s", label, cleanExternalText(line))
	b.Send(m.ReplyTarget(), truncateRunes(result, p.maxLength))
	return true
}

func funCommandCategory(command string) (string, bool) {
	switch strings.ToLower(command) {
	case "yomomma", "yo", "yo-momma":
		return "yomomma", true
	case "oneliner", "oneliners", "one":
		return "oneliner", true
	case "pun", "puns":
		return "pun", true
	case "wisdom", "wise":
		return "wisdom", true
	default:
		return "", false
	}
}

var funFallbacks = map[string][]string{
	"yomomma":  {"has a calendar so full, every day is booked.", "can make a group chat go quiet with one typing bubble.", "is so organized, even the junk drawer has an index.", "can turn leftovers into a five-star encore."},
	"oneliner": {"I tried to organize a hide-and-seek tournament, but good players are hard to find.", "My calendar is full of dates, but none of them are edible.", "I bought a thesaurus yesterday; not only was it terrible, it was terrible.", "The future is looking bright, but I still keep a flashlight handy."},
	"pun":      {"I used to be a baker, but I could not make enough dough.", "I am reading a book about anti-gravity; it is impossible to put down.", "The calendar got promoted because it had a lot of dates.", "I wanted to learn origami, but the details were too paper-thin."},
	"wisdom":   {"A small reliable step beats a heroic plan that never starts.", "Leave a little room in the schedule for the unexpected good thing.", "The best shortcut is often a clear question asked early.", "A calm explanation can solve what a fast argument only enlarges."},
}
