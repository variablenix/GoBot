package plugins

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Choose struct{}

func (p *Choose) Name() string       { return "choose" }
func (p *Choose) Commands() []string { return []string{"choose"} }
func (p *Choose) Help() string {
	return "!choose option 1 | option 2 — choose randomly between options"
}
func (p *Choose) Init(_ bot.PluginConfig, _ *storage.DB) error { return nil }
func (p *Choose) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "choose" {
		return false
	}
	separator := "|"
	if !strings.Contains(arg, separator) {
		separator = ","
	}
	parts := strings.Split(arg, separator)
	options := make([]string, 0, len(parts))
	for _, part := range parts {
		if option := strings.TrimSpace(part); option != "" {
			if len([]rune(option)) > 200 {
				b.Send(m.ReplyTarget(), ircColor(ircRed, "each choice must be 200 characters or less"))
				return true
			}
			options = append(options, option)
		}
	}
	if len(options) < 2 || len(options) > 20 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !choose option 1 | option 2 (2 to 20 options)"))
		return true
	}
	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(options))))
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "choice is temporarily unavailable"))
		return true
	}
	b.Send(m.ReplyTarget(), fmt.Sprintf("%sI choose:%s %s", ircGreen, ircReset, options[index.Int64()]))
	return true
}
