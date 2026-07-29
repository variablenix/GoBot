package plugins

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Tell struct{ db *storage.DB }

func (p *Tell) Name() string                                 { return "tell" }
func (p *Tell) Commands() []string                           { return []string{"tell"} }
func (p *Tell) Help() string                                 { return "!tell <nick> <message> — deliver a message when they speak" }
func (p *Tell) Init(_ bot.PluginConfig, d *storage.DB) error { p.db = d; return nil }
func (p *Tell) Handle(b *bot.Bot, m bot.Message) bool {
	if m.Command == "PRIVMSG" && m.Nick != "" {
		key := strings.ToLower(m.Nick)
		if v, e := p.db.Get("tell", key); e == nil {
			var xs []record
			json.Unmarshal(v, &xs)
			for _, x := range xs {
				b.Send(m.ReplyTarget(), fmt.Sprintf("%s: you have a message from %s (%s ago): %s", m.Nick, x.Nick, time.Since(x.At).Round(time.Minute), x.Text))
			}
			p.db.Delete("tell", key)
		}
	}
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "tell" {
		return false
	}
	parts := strings.Fields(arg)
	if len(parts) < 2 {
		b.Send(m.ReplyTarget(), "usage: !tell <nick> <message>")
		return true
	}
	key := strings.ToLower(parts[0])
	var xs []record
	if v, e := p.db.Get("tell", key); e == nil {
		json.Unmarshal(v, &xs)
	}
	xs = append(xs, record{m.Nick, "", strings.Join(parts[1:], " "), time.Now()})
	p.db.Set("tell", key, xs)
	b.Send(m.ReplyTarget(), "message queued")
	return true
}
