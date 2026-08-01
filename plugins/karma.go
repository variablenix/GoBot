package plugins

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Karma struct {
	db *storage.DB
	rx *regexp.Regexp
}

func (p *Karma) Name() string       { return "karma" }
func (p *Karma) Commands() []string { return []string{"karma"} }
func (p *Karma) Help() string       { return "!karma <thing> — show karma; thing++ or thing-- changes it" }
func (p *Karma) Init(_ bot.PluginConfig, d *storage.DB) error {
	p.db = d
	p.rx = regexp.MustCompile(`(?i)(^|[^a-z0-9_-])([a-z0-9_][a-z0-9_-]{0,30})(\+\+|--)`)
	return nil
}
func (p *Karma) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok {
		if updates := p.applyTextChanges(m.Text); len(updates) > 0 {
			b.Send(m.ReplyTarget(), ircColor(ircCyan, "karma updated: "+strings.Join(updates, ", ")))
			return true
		}
		return false
	}
	if cmd != "karma" {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(arg))
	if key == "" {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !karma <thing>"))
		return true
	}
	v := 0
	if p.db != nil {
		if raw, e := p.db.Get("karma", key); e == nil {
			_ = json.Unmarshal(raw, &v)
		}
	}
	color := ircYellow
	if v > 0 {
		color = ircGreen
	} else if v < 0 {
		color = ircRed
	}
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s has karma of %s%+d%s", key, color, v, ircReset))
	return true
}

func (p *Karma) applyTextChanges(text string) []string {
	if p.db == nil || p.rx == nil {
		return nil
	}
	updates := make([]string, 0)
	for _, match := range p.rx.FindAllStringSubmatchIndex(text, -1) {
		if len(match) < 8 {
			continue
		}
		// The trailing boundary is checked here instead of in the regular
		// expression so a separator can be reused by the next match.
		if match[1] < len(text) && isKarmaWordByte(text[match[1]]) {
			continue
		}
		key := strings.ToLower(text[match[4]:match[5]])
		delta := -1
		if text[match[6]:match[7]] == "++" {
			delta = 1
		}
		value, err := p.change(key, delta)
		if err != nil {
			continue
		}
		updates = append(updates, fmt.Sprintf("%s=%+d", key, value))
	}
	return updates
}

func isKarmaWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		value == '_' || value == '-'
}

func (p *Karma) change(key string, delta int) (int, error) {
	if p.db == nil {
		return 0, fmt.Errorf("karma storage is unavailable")
	}
	value := 0
	if raw, err := p.db.Get("karma", key); err == nil {
		if err := json.Unmarshal(raw, &value); err != nil {
			return 0, err
		}
	}
	value += delta
	if err := p.db.Set("karma", key, value); err != nil {
		return 0, err
	}
	return value, nil
}
