package plugins

import (
	"math/rand"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Quote struct{ quotes []string }

func (p *Quote) Name() string       { return "quote" }
func (p *Quote) Commands() []string { return []string{"quote"} }
func (p *Quote) Help() string       { return "!quote — display a random quote" }
func (p *Quote) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.quotes = loadQuotes(c)
	return nil
}
func (p *Quote) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, _, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "quote" || len(p.quotes) == 0 {
		return false
	}
	for _, part := range splitIRCText(p.quotes[rand.Intn(len(p.quotes))], 350) {
		b.Send(m.ReplyTarget(), part)
	}
	return true
}
