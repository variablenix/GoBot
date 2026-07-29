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
	p.rx = regexp.MustCompile(`(?i)([a-z0-9_][a-z0-9_-]{0,30})(\+\+|--)`)
	return nil
}
func (p *Karma) Handle(b *bot.Bot, m bot.Message) bool {
	for _, x := range p.rx.FindAllStringSubmatch(m.Text, -1) {
		v := 0
		if raw, e := p.db.Get("karma", strings.ToLower(x[1])); e == nil {
			json.Unmarshal(raw, &v)
		}
		if x[2] == "++" {
			v++
		} else {
			v--
		}
		p.db.Set("karma", strings.ToLower(x[1]), v)
	}
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "karma" {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(arg))
	v := 0
	if raw, e := p.db.Get("karma", key); e == nil {
		json.Unmarshal(raw, &v)
	}
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s has karma of %+d", key, v))
	return true
}
