package plugins

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const luvPointsBucket = "luv_points"

var luvMu sync.Mutex

// Luv awards persistent blue-heart points to a nickname. Totals are scoped to
// the IRC network so unrelated networks do not share social scores.
type Luv struct {
	db *storage.DB
}

func (p *Luv) Name() string       { return "luv" }
func (p *Luv) Commands() []string { return []string{"luv"} }
func (p *Luv) Help() string {
	return "!luv <nick> — spread LUV and award that nickname one persistent 💙 point"
}

func (p *Luv) Init(_ bot.PluginConfig, db *storage.DB) error {
	p.db = db
	return nil
}

func (p *Luv) Handle(b *bot.Bot, m bot.Message) bool {
	command, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || command != "luv" {
		return false
	}

	fields := strings.Fields(arg)
	if len(fields) != 1 || !validLuvNick(fields[0]) {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !luv <single nickname>"))
		return true
	}
	if strings.TrimSpace(m.Nick) == "" {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "LUV needs a sender nickname"))
		return true
	}
	target := fields[0]
	if strings.EqualFold(target, m.Nick) {
		b.Send(m.ReplyTarget(), ircColor(ircCyan, "LUV is sweetest when shared with someone else 💙"))
		return true
	}

	total, err := p.award(b.Config.NetworkName, target)
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "LUV could not be saved; please try again later"))
		return true
	}

	sender := cleanExternalText(m.Nick)
	displayTarget := cleanExternalText(target)
	unit := "points"
	if total == 1 {
		unit = "point"
	}
	message := fmt.Sprintf("%s %s spreads %s to %s. %s now has %d %s!",
		ircColor(ircCyan, "💙"), sender, ircColor(ircCyan, "LUV"), displayTarget,
		displayTarget, total, ircColor(ircCyan, "💙 "+unit))
	b.Send(m.ReplyTarget(), message)
	return true
}

func (p *Luv) award(network, target string) (int, error) {
	if p.db == nil {
		return 0, errors.New("LUV storage is unavailable")
	}

	luvMu.Lock()
	defer luvMu.Unlock()

	key := luvKey(network, target)
	total := 0
	raw, err := p.db.Get(luvPointsBucket, key)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return 0, err
	}
	if err == nil {
		if err := storage.Decode(raw, &total); err != nil {
			return 0, err
		}
	}
	total++
	if err := p.db.Set(luvPointsBucket, key, total); err != nil {
		return 0, err
	}
	return total, nil
}

func luvKey(network, target string) string {
	return strings.ToLower(strings.TrimSpace(network)) + "\x00" + strings.ToLower(strings.TrimSpace(target))
}

func validLuvNick(nick string) bool {
	if nick == "" || len([]rune(nick)) > 30 {
		return false
	}
	for i, r := range nick {
		if i == 0 {
			if !unicode.IsLetter(r) && !strings.ContainsRune("[]\\`^{}|_", r) {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("-[]\\`^{}|_", r) {
			return false
		}
	}
	return true
}
