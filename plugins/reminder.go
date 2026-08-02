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
	id      string
	timer   *time.Timer
	target  string
	nick    string
	message string
	dueAt   time.Time
}

type Reminder struct {
	mu      sync.Mutex
	items   map[string][]*reminder
	pending []reminderRecord
	db      *storage.DB
}

const reminderLifetime = 15 * 24 * time.Hour

type reminderRecord struct {
	ID      string    `json:"id"`
	Network string    `json:"network"`
	Target  string    `json:"target"`
	Nick    string    `json:"nick"`
	Message string    `json:"message"`
	DueAt   time.Time `json:"due_at"`
}

func (p *Reminder) Name() string       { return "remind" }
func (p *Reminder) Commands() []string { return []string{"remind"} }
func (p *Reminder) Help() string {
	return "!remind <duration> <message> — send a reminder later (for example, !remind 30m check logs)"
}
func (p *Reminder) Init(_ bot.PluginConfig, db *storage.DB) error {
	p.items = make(map[string][]*reminder)
	p.db = db
	if db != nil {
		for _, id := range mustReminderList(db) {
			if raw, err := db.Get("reminders", id); err == nil {
				var saved reminderRecord
				if storage.Decode(raw, &saved) == nil && saved.DueAt.After(time.Now().Add(-reminderLifetime)) {
					p.pending = append(p.pending, saved)
				} else {
					_ = db.Delete("reminders", id)
				}
			}
		}
	}
	return nil
}

func (p *Reminder) Start(b *bot.Bot) {
	p.mu.Lock()
	pending := p.pending
	p.pending = nil
	p.mu.Unlock()
	var remaining []reminderRecord
	for _, saved := range pending {
		if saved.Network != "" && !strings.EqualFold(saved.Network, b.Config.NetworkName) {
			remaining = append(remaining, saved)
			continue
		}
		p.schedule(b, saved)
	}
	p.mu.Lock()
	p.pending = append(p.pending, remaining...)
	p.mu.Unlock()
}

// Stop cancels active timers while retaining their persisted records. A later
// enable reloads those records and schedules them again instead of allowing a
// disabled plugin to send messages.
func (p *Reminder) Stop(_ *bot.Bot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, items := range p.items {
		for _, item := range items {
			if item.timer != nil {
				item.timer.Stop()
			}
		}
	}
	p.items = make(map[string][]*reminder)
	p.pending = nil
}
func (p *Reminder) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "remind" {
		return false
	}
	parts := strings.SplitN(strings.TrimSpace(arg), " ", 2)
	if len(parts) != 2 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !remind <duration> <message> (for example, !remind 30m check logs)"))
		return true
	}
	duration, err := time.ParseDuration(parts[0])
	if err != nil || duration < time.Second || duration > reminderLifetime {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "duration must be between 1s and 360h"))
		return true
	}
	message := strings.TrimSpace(parts[1])
	if message == "" || len([]rune(message)) > 300 {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "reminder message must be between 1 and 300 characters"))
		return true
	}
	key := reminderKey(b.Config.NetworkName, m.ReplyTarget(), m.Nick)
	p.mu.Lock()
	if len(p.items[key]) >= 20 {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), ircColor(ircRed, "you already have 20 reminders pending"))
		return true
	}
	p.mu.Unlock()
	saved := reminderRecord{ID: fmt.Sprintf("%d", time.Now().UnixNano()), Network: b.Config.NetworkName, Target: m.ReplyTarget(), Nick: m.Nick, Message: message, DueAt: time.Now().Add(duration)}
	if p.db != nil {
		_ = p.db.Set("reminders", saved.ID, saved)
	}
	p.schedule(b, saved)
	b.Send(m.ReplyTarget(), ircColor(ircGreen, "reminder set for "+formatReminderDuration(duration)))
	return true
}

func (p *Reminder) remove(key string, item *reminder) {
	p.mu.Lock()
	defer p.mu.Unlock()
	items := p.items[key]
	for i, candidate := range items {
		if candidate == item {
			p.items[key] = append(items[:i], items[i+1:]...)
			if p.db != nil {
				_ = p.db.Delete("reminders", item.id)
			}
			return
		}
	}
}

func (p *Reminder) schedule(b *bot.Bot, saved reminderRecord) *reminder {
	key := reminderKey(b.Config.NetworkName, saved.Target, saved.Nick)
	item := &reminder{id: saved.ID, target: saved.Target, nick: saved.Nick, message: saved.Message, dueAt: saved.DueAt}
	p.mu.Lock()
	p.items[key] = append(p.items[key], item)
	p.mu.Unlock()
	delay := time.Until(saved.DueAt)
	if delay < 0 {
		delay = 0
	}
	item.timer = time.AfterFunc(delay, func() {
		if b.PluginEnabledForChannel(p.Name(), item.target) {
			b.Send(item.target, fmt.Sprintf("%s: reminder: %s", item.nick, item.message))
		}
		p.remove(key, item)
	})
	return item
}

func reminderKey(network, target, nick string) string {
	return strings.ToLower(network) + "\x00" + strings.ToLower(target) + "\x00" + strings.ToLower(nick)
}

func mustReminderList(db *storage.DB) []string {
	keys, _ := db.List("reminders")
	return keys
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
