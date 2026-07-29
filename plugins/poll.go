package plugins

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type poll struct {
	question string
	options  []string
	votes    map[string]int
}

type Poll struct {
	mu     sync.Mutex
	active map[string]*poll
}

func (p *Poll) Name() string       { return "poll" }
func (p *Poll) Commands() []string { return []string{"poll"} }
func (p *Poll) Help() string {
	return "!poll create question | option 1 | option 2; !poll vote <number>; !poll results; !poll close"
}
func (p *Poll) Init(_ bot.PluginConfig, _ *storage.DB) error {
	p.active = make(map[string]*poll)
	return nil
}
func (p *Poll) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "poll" || !m.IsChannel {
		return false
	}
	key := strings.ToLower(m.Target)
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
	p.active[key] = &poll{question: question, options: options, votes: make(map[string]int)}
	return formatPoll(p.active[key])
}

func (p *Poll) vote(key, nick, rawOption string) string {
	current := p.active[key]
	if current == nil {
		return "no poll is active in this channel"
	}
	option, err := strconv.Atoi(rawOption)
	if err != nil || option < 1 || option > len(current.options) {
		return fmt.Sprintf("vote with a number from 1 to %d", len(current.options))
	}
	current.votes[strings.ToLower(nick)] = option
	return fmt.Sprintf("vote recorded for option %d", option)
}

func (p *Poll) results(key string) string {
	current := p.active[key]
	if current == nil {
		return "no poll is active in this channel"
	}
	return formatPoll(current)
}

func formatPoll(current *poll) string {
	counts := make([]int, len(current.options))
	for _, option := range current.votes {
		counts[option-1]++
	}
	parts := make([]string, len(current.options))
	for i, option := range current.options {
		parts[i] = fmt.Sprintf("%d) %s [%d]", i+1, option, counts[i])
	}
	return fmt.Sprintf("Poll: %s — %s (%d votes)", current.question, strings.Join(parts, "; "), len(current.votes))
}
