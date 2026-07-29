package plugins

import (
	"crypto/rand"
	"fmt"
	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

var diceRX = regexp.MustCompile(`^(?:(\d*)[dD])?(\d+)$`)

func ParseDice(s string) (int, int, error) {
	m := diceRX.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, 0, fmt.Errorf("invalid dice notation")
	}
	n := 1
	if m[1] != "" {
		n, _ = strconv.Atoi(m[1])
	}
	sides, _ := strconv.Atoi(m[2])
	if n < 1 || n > 100 || sides < 1 || sides > 10000 {
		return 0, 0, fmt.Errorf("dice limits are 1-100 dice and 1-10000 sides")
	}
	return n, sides, nil
}

type Dice struct{}

func (p *Dice) Name() string                                 { return "dice" }
func (p *Dice) Commands() []string                           { return []string{"roll"} }
func (p *Dice) Help() string                                 { return "!roll <NdN> or !roll <N> — roll dice" }
func (p *Dice) Init(_ bot.PluginConfig, _ *storage.DB) error { return nil }
func (p *Dice) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "roll" {
		return false
	}
	n, s, e := ParseDice(arg)
	if e != nil {
		b.Send(m.ReplyTarget(), e.Error())
		return true
	}
	vals := make([]string, n)
	sum := 0
	for i := 0; i < n; i++ {
		v, _ := rand.Int(rand.Reader, big.NewInt(int64(s)))
		x := int(v.Int64()) + 1
		vals[i] = strconv.Itoa(x)
		sum += x
	}
	b.Send(m.ReplyTarget(), fmt.Sprintf("Rolled %dd%d: [%s] = %d", n, s, strings.Join(vals, ", "), sum))
	return true
}
