package plugins

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type reminder struct {
	timer   *time.Timer
	target  string
	nick    string
	message string
}

type Reminder struct {
	mu    sync.Mutex
	items map[string][]*reminder
}

func (p *Reminder) Name() string       { return "remind" }
func (p *Reminder) Commands() []string { return []string{"remind"} }
func (p *Reminder) Help() string {
	return "!remind <duration> <message> — send a reminder later (for example, !remind 30m check logs)"
}
func (p *Reminder) Init(_ bot.PluginConfig, _ *storage.DB) error {
	p.items = make(map[string][]*reminder)
	return nil
}
func (p *Reminder) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "remind" {
		return false
	}
	parts := strings.SplitN(strings.TrimSpace(arg), " ", 2)
	if len(parts) != 2 {
		b.Send(m.ReplyTarget(), "usage: !remind <duration> <message> (for example, !remind 30m check logs)")
		return true
	}
	duration, err := time.ParseDuration(parts[0])
	if err != nil || duration < time.Second || duration > 30*24*time.Hour {
		b.Send(m.ReplyTarget(), "duration must be between 1s and 720h")
		return true
	}
	message := strings.TrimSpace(parts[1])
	if message == "" || len([]rune(message)) > 300 {
		b.Send(m.ReplyTarget(), "reminder message must be between 1 and 300 characters")
		return true
	}
	key := strings.ToLower(m.ReplyTarget()) + "\x00" + strings.ToLower(m.Nick)
	p.mu.Lock()
	if len(p.items[key]) >= 20 {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), "you already have 20 reminders pending")
		return true
	}
	item := &reminder{target: m.ReplyTarget(), nick: m.Nick, message: message}
	item.timer = time.AfterFunc(duration, func() {
		b.Send(item.target, fmt.Sprintf("%s: reminder: %s", item.nick, item.message))
		p.remove(key, item)
	})
	p.items[key] = append(p.items[key], item)
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), "reminder set for "+formatReminderDuration(duration))
	return true
}

func (p *Reminder) remove(key string, item *reminder) {
	p.mu.Lock()
	defer p.mu.Unlock()
	items := p.items[key]
	for i, candidate := range items {
		if candidate == item {
			p.items[key] = append(items[:i], items[i+1:]...)
			return
		}
	}
}

func formatReminderDuration(duration time.Duration) string {
	if duration < time.Minute {
		return strconv.Itoa(int(duration/time.Second)) + " seconds"
	}
	if duration < time.Hour {
		return strconv.Itoa(int(duration/time.Minute)) + " minutes"
	}
	return strconv.Itoa(int(duration/time.Hour)) + " hours"
}
