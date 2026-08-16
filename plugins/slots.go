package plugins

import (
	"fmt"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type slotSymbol struct {
	value  string
	weight int
}

var slotSymbols = []slotSymbol{
	{value: "🍒", weight: 6}, {value: "🍋", weight: 5}, {value: "🍊", weight: 5},
	{value: "🍇", weight: 4}, {value: "🔔", weight: 3}, {value: "⭐", weight: 3},
	{value: "💎", weight: 2}, {value: "7️⃣", weight: 1}, {value: "🎰", weight: 1},
}

type Slots struct {
	cooldown  scopedCooldown
	maxLength int
}

func (p *Slots) Name() string       { return "slots" }
func (p *Slots) Commands() []string { return []string{"slots", "slot", "spin"} }
func (p *Slots) Help() string       { return "!slots — spin three weighted reels (aliases: !slot, !spin)" }
func (p *Slots) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.cooldown.configure(c.Int("cooldown_seconds", 8), 8)
	p.maxLength = c.Int("max_length", 300)
	if p.maxLength < 100 || p.maxLength > 400 {
		p.maxLength = 300
	}
	return nil
}
func (p *Slots) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, _, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "slots" && cmd != "slot" && cmd != "spin") {
		return false
	}
	if !p.cooldown.allow(scopedKey(b.Config.NetworkName, m.Target, pluginIdentity(m))) {
		return true
	}
	reels := make([]string, 3)
	for i := range reels {
		value, err := weightedSlotSymbol()
		if err != nil {
			b.Send(m.ReplyTarget(), "the slot machine is temporarily unavailable")
			return true
		}
		reels[i] = value
	}
	result := slotResult(reels)
	b.Send(m.ReplyTarget(), truncateRunes(fmt.Sprintf("[ %s ] — %s", strings.Join(reels, " | "), result), p.maxLength))
	return true
}

func weightedSlotSymbol() (string, error) {
	total := 0
	for _, symbol := range slotSymbols {
		total += symbol.weight
	}
	selected, err := secureRandomInt(int64(total))
	if err != nil {
		return "", err
	}
	for _, symbol := range slotSymbols {
		if selected < int64(symbol.weight) {
			return symbol.value, nil
		}
		selected -= int64(symbol.weight)
	}
	return "", fmt.Errorf("slot table is empty")
}

func slotResult(reels []string) string {
	counts := make(map[string]int)
	for _, reel := range reels {
		counts[reel]++
	}
	if counts["🎰"] == 3 {
		return "JACKPOT!"
	}
	if counts["7️⃣"] == 3 {
		return "JACKPOT!"
	}
	if counts["💎"] == 3 {
		return "Triple diamonds!"
	}
	for symbol, count := range counts {
		if count == 3 {
			return "Three of a kind!"
		}
		if count == 2 && symbol == "7️⃣" {
			return "Lucky sevens!"
		}
		if count == 2 && symbol == "💎" {
			return "Double diamonds!"
		}
	}
	for symbol, count := range counts {
		if count == 2 {
			return fmt.Sprintf("A pair of %s!", symbol)
		}
	}
	return "No match. Try again!"
}
