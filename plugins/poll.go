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

type poll struct {
	Question string         `json:"question"`
	Options  []string       `json:"options"`
	Votes    map[string]int `json:"votes"`
	Expires  time.Time      `json:"expires"`
}

type Poll struct {
	mu     sync.Mutex
	active map[string]*poll
	db     *storage.DB
}

const pollLifetime = 15 * 24 * time.Hour

func (p *Poll) Name() string       { return "poll" }
func (p *Poll) Commands() []string { return []string{"poll"} }
func (p *Poll) Help() string {
	return "!poll create question | option 1 | option 2; !poll vote <number>; !poll results; !poll close"
}
func (p *Poll) Init(_ bot.PluginConfig, db *storage.DB) error {
	p.active = make(map[string]*poll)
	p.db = db
	if db == nil {
		return nil
	}
	for _, key := range mustList(db, "polls") {
		if raw, err := db.Get("polls", key); err == nil {
			var saved poll
			if storage.Decode(raw, &saved) == nil && saved.Expires.After(time.Now()) {
				p.active[key] = &saved
			} else {
				_ = db.Delete("polls", key)
			}
		}
	}
	return nil
}
func (p *Poll) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "poll" || !m.IsChannel {
		return false
	}
	key := strings.ToLower(b.Config.NetworkName) + "\x00" + strings.ToLower(m.Target)
	p.mu.Lock()
	p.expire(key)
	p.mu.Unlock()
	parts := strings.Fields(arg)
	action := "results"
	if len(parts) > 0 {
		action = strings.ToLower(parts[0])
	}
	p.mu.Lock()
	var response string
	switch action {
	case "create", "new":
		response = p.create(key, strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(arg, parts[0]), " ")))
	case "vote":
		if len(parts) != 2 {
			response = "usage: !poll vote <number>"
		} else {
			response = p.vote(key, m.Nick, parts[1])
		}
	case "results", "status":
		response = p.results(key)
	case "close", "end":
		response = p.results(key)
		delete(p.active, key)
		if p.db != nil {
			_ = p.db.Delete("polls", key)
		}
	default:
		response = "usage: !poll create <question> | <option 1> | <option 2>"
	}
	p.mu.Unlock()
	for _, part := range splitIRCText(response, 350) {
		b.Send(m.ReplyTarget(), part)
	}
	return true
}

func (p *Poll) create(key, raw string) string {
	parts := strings.Split(raw, "|")
	if len(parts) < 3 || len(parts) > 11 || len([]rune(raw)) > 700 {
		return "usage: !poll create <question> | <option 1> | <option 2> (up to 10 options)"
	}
	question := strings.TrimSpace(parts[0])
	if question == "" || len([]rune(question)) > 200 {
		return "poll question must be between 1 and 200 characters"
	}
	options := make([]string, 0, len(parts)-1)
	for _, part := range parts[1:] {
		option := strings.TrimSpace(part)
		if option == "" || len([]rune(option)) > 80 {
			return "poll options must be between 1 and 80 characters"
		}
		options = append(options, option)
	}
	p.active[key] = &poll{Question: question, Options: options, Votes: make(map[string]int), Expires: time.Now().Add(pollLifetime)}
	p.persist(key)
	return formatPoll(p.active[key])
}

func (p *Poll) vote(key, nick, rawOption string) string {
	current := p.active[key]
	if current == nil {
		return "no poll is active in this channel"
	}
	option, err := strconv.Atoi(rawOption)
	if err != nil || option < 1 || option > len(current.Options) {
		return fmt.Sprintf("vote with a number from 1 to %d", len(current.Options))
	}
	current.Votes[strings.ToLower(nick)] = option
	p.persist(key)
	return fmt.Sprintf("vote recorded for option %d", option)
}

func (p *Poll) results(key string) string {
	current := p.active[key]
	if current == nil {
		return "no poll is active in this channel"
	}
	return formatPoll(current)
}

func (p *Poll) expire(key string) {
	current := p.active[key]
	if current != nil && !current.Expires.After(time.Now()) {
		delete(p.active, key)
		if p.db != nil {
			_ = p.db.Delete("polls", key)
		}
	}
}

func (p *Poll) persist(key string) {
	if p.db != nil {
		_ = p.db.Set("polls", key, p.active[key])
	}
}

func mustList(db *storage.DB, bucket string) []string {
	keys, _ := db.List(bucket)
	return keys
}

func formatPoll(current *poll) string {
	counts := make([]int, len(current.Options))
	for _, option := range current.Votes {
		counts[option-1]++
	}
	parts := make([]string, len(current.Options))
	for i, option := range current.Options {
		parts[i] = fmt.Sprintf("%d) %s [%d]", i+1, option, counts[i])
	}
	return fmt.Sprintf("Poll: %s — %s (%d votes)", current.Question, strings.Join(parts, "; "), len(current.Votes))
}
