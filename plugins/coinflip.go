package plugins

import (
	"fmt"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Coinflip struct {
	cooldown scopedCooldown
}

func (p *Coinflip) Name() string       { return "coinflip" }
func (p *Coinflip) Commands() []string { return []string{"coinflip", "flip", "coin"} }
func (p *Coinflip) Help() string {
	return "!coinflip [nick] — flip a coin (aliases: !flip, !coin)"
}
func (p *Coinflip) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.cooldown.configure(c.Int("cooldown_seconds", 3), 3)
	return nil
}
func (p *Coinflip) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "coinflip" && cmd != "flip" && cmd != "coin") {
		return false
	}
	if len(strings.Fields(arg)) > 1 {
		b.Send(m.ReplyTarget(), "usage: !coinflip [nick]")
		return true
	}
	key := scopedKey(b.Config.NetworkName, m.Target, "channel")
	if !p.cooldown.allow(key) {
		return true
	}
	flip, err := secureRandomInt(2)
	if err != nil {
		b.Send(m.ReplyTarget(), "the coin could not be flipped")
		return true
	}
	face := "Tails"
	if flip == 0 {
		face = "Heads"
	}
	label := cleanExternalText(strings.TrimSpace(arg))
	if label != "" {
		label = truncateRunes(label, 48)
	}
	if label == "" {
		b.Send(m.ReplyTarget(), fmt.Sprintf("🪙 %s!", face))
	} else {
		b.Send(m.ReplyTarget(), fmt.Sprintf("🪙 %s: %s!", label, face))
	}
	return true
}
