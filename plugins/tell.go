package plugins

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const tellBucket = "tell"
const maxPendingTellMessages = 20

type tellRecord struct {
	Nick string    `json:"nick"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

type Tell struct{ db *storage.DB }

var selfTellResponses = []string{
	"You don't need to tell me, I'm right here!",
	"I'm already listening — no need to leave myself a note.",
	"Plot twist: I'm the one you're trying to tell.",
	"Message received before it was even queued. Efficient!",
}

func formatTellConfirmation(nick string) string {
	return fmt.Sprintf("I'll tell %s the next time they speak.", nick)
}

func isSelfTellTarget(target, self string) bool {
	return strings.TrimSpace(target) != "" && strings.EqualFold(strings.TrimSpace(target), strings.TrimSpace(self))
}

func selfTellResponse() string {
	return selfTellResponses[rand.Intn(len(selfTellResponses))]
}

func (p *Tell) Name() string       { return "tell" }
func (p *Tell) Commands() []string { return []string{"tell"} }
func (p *Tell) Help() string {
	return "!tell <nick> <message> — deliver a message when they next speak"
}
func (p *Tell) Init(_ bot.PluginConfig, d *storage.DB) error { p.db = d; return nil }
func (p *Tell) Handle(b *bot.Bot, m bot.Message) bool {
	if m.Command == "PRIVMSG" && m.Nick != "" && p.db != nil {
		key := tellKey(b.Config.NetworkName, m.Nick)
		if v, e := p.db.Get(tellBucket, key); e == nil {
			var messages []tellRecord
			if json.Unmarshal(v, &messages) == nil {
				for _, message := range messages {
					b.Send(m.ReplyTarget(), fmt.Sprintf("%s: you have a message from %s (%s ago): %s", m.Nick, message.Nick, time.Since(message.At).Round(time.Minute), message.Text))
				}
			}
			_ = p.db.Delete(tellBucket, key)
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
	targetNick := parts[0]
	if isSelfTellTarget(targetNick, b.Config.Identity.Nick) {
		b.Send(m.ReplyTarget(), selfTellResponse())
		return true
	}
	if p.db == nil {
		b.Send(m.ReplyTarget(), "tell storage is unavailable")
		return true
	}
	message := strings.Join(parts[1:], " ")
	key := tellKey(b.Config.NetworkName, targetNick)
	var messages []tellRecord
	if raw, err := p.db.Get(tellBucket, key); err == nil {
		if json.Unmarshal(raw, &messages) != nil {
			b.Send(m.ReplyTarget(), "could not read the pending message queue")
			return true
		}
	}
	if len(messages) >= maxPendingTellMessages {
		b.Send(m.ReplyTarget(), "that nickname already has too many pending messages")
		return true
	}
	messages = append(messages, tellRecord{Nick: m.Nick, Text: message, At: time.Now()})
	if err := p.db.Set(tellBucket, key, messages); err != nil {
		b.Send(m.ReplyTarget(), "could not save the pending message")
		return true
	}
	b.Send(m.ReplyTarget(), formatTellConfirmation(targetNick))
	return true
}

func tellKey(network, nick string) string {
	return strings.ToLower(network) + "\x00" + strings.ToLower(nick)
}
