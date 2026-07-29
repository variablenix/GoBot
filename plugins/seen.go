package plugins

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type record struct {
	Nick, Channel, Text string
	At                  time.Time
}

type Seen struct{ db *storage.DB }

func (p *Seen) Name() string                                 { return "seen" }
func (p *Seen) Commands() []string                           { return []string{"seen"} }
func (p *Seen) Help() string                                 { return "!seen <nick> — show when someone last spoke" }
func (p *Seen) Init(_ bot.PluginConfig, d *storage.DB) error { p.db = d; return nil }
func (p *Seen) Handle(b *bot.Bot, m bot.Message) bool {
	if m.Command == "PRIVMSG" && m.Nick != "" {
		p.db.Set("seen", strings.ToLower(m.Nick), record{m.Nick, m.Target, m.Text, m.Timestamp})
	}
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "seen" {
		return false
	}
	v, e := p.db.Get("seen", strings.ToLower(strings.TrimSpace(arg)))
	if e != nil {
		b.Send(m.ReplyTarget(), "I haven't seen that nick yet.")
		return true
	}
	var x record
	json.Unmarshal(v, &x)
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s was last seen in %s %s ago saying: %q", x.Nick, x.Channel, time.Since(x.At).Round(time.Minute), x.Text))
	return true
}
