package plugins

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type record struct {
	Nick, Channel, Text string
	At                  time.Time
}

type Seen struct{ db *storage.DB }

func (p *Seen) Name() string       { return "seen" }
func (p *Seen) Commands() []string { return []string{"seen"} }
func (p *Seen) Help() string {
	return "!seen <nick> — show when someone last spoke in the current channel"
}
func (p *Seen) Init(_ bot.PluginConfig, d *storage.DB) error { p.db = d; return nil }
func (p *Seen) Handle(b *bot.Bot, m bot.Message) bool {
	if m.Command == "PRIVMSG" && m.IsChannel && m.Nick != "" && p.db != nil {
		_ = p.db.Set("seen", seenKey(b.Config.NetworkName, m.Target, m.Nick), record{m.Nick, m.Target, cleanExternalText(normalizeSeenText(m.Nick, m.Text)), m.Timestamp})
	}
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "seen" {
		return false
	}
	if !m.IsChannel {
		b.Send(m.ReplyTarget(), "Use !seen in a channel; records are private to that channel.")
		return true
	}
	if p.db == nil {
		b.Send(m.ReplyTarget(), "Seen storage is unavailable.")
		return true
	}
	// Legacy records may contain private messages or another channel's history.
	// Leave them untouched, but never disclose them across channel boundaries.
	v, e := p.db.Get("seen", seenKey(b.Config.NetworkName, m.Target, strings.TrimSpace(arg)))
	if e != nil {
		b.Send(m.ReplyTarget(), "I haven't seen that nick yet.")
		return true
	}
	var x record
	if err := json.Unmarshal(v, &x); err != nil || strings.TrimSpace(x.Nick) == "" ||
		!strings.EqualFold(x.Channel, m.Target) {
		b.Send(m.ReplyTarget(), "That seen record is unavailable.")
		return true
	}
	b.Send(m.ReplyTarget(), fmt.Sprintf("👁️ %s was last seen in %s %s ago saying: %q", x.Nick, x.Channel, formatSeenAge(x.At, time.Now()), x.Text))
	return true
}

func seenKey(network, channel, nick string) string { return scopedKey(network, channel, nick) }

func normalizeSeenText(nick, text string) string {
	if len(text) >= 2 && text[0] == '\x01' && text[len(text)-1] == '\x01' {
		content := text[1 : len(text)-1]
		if len(content) >= len("ACTION") && strings.EqualFold(content[:len("ACTION")], "ACTION") {
			remainder := content[len("ACTION"):]
			if remainder == "" || unicode.IsSpace([]rune(remainder)[0]) {
				action := strings.TrimSpace(remainder)
				if action == "" {
					return strings.TrimSpace(nick)
				}
				return strings.TrimSpace(strings.TrimSpace(nick) + " " + action)
			}
		}
	}
	return text
}

func formatSeenAge(at, now time.Time) string {
	age := now.Sub(at)
	if age < 0 {
		age = 0
	}
	switch {
	case age < time.Minute:
		return fmt.Sprintf("%ds", int(age/time.Second))
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	}
}
