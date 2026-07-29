package plugins

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type channelStat struct {
	messages uint64
	users    map[string]uint64
}

type ChannelStats struct {
	mu       sync.Mutex
	channels map[string]*channelStat
}

func (p *ChannelStats) Name() string       { return "channelstats" }
func (p *ChannelStats) Commands() []string { return []string{"stats", "chanstats", "channelstats"} }
func (p *ChannelStats) Help() string {
	return "!stats — show in-memory message and user statistics for this channel"
}
func (p *ChannelStats) Init(_ bot.PluginConfig, _ *storage.DB) error {
	p.channels = make(map[string]*channelStat)
	return nil
}
func (p *ChannelStats) Handle(b *bot.Bot, m bot.Message) bool {
	if m.Command == "PRIVMSG" && m.IsChannel && m.Nick != "" {
		p.mu.Lock()
		stats := p.channels[strings.ToLower(m.Target)]
		if stats == nil {
			stats = &channelStat{users: make(map[string]uint64)}
			p.channels[strings.ToLower(m.Target)] = stats
		}
		stats.messages++
		stats.users[m.Nick]++
		p.mu.Unlock()
	}
	cmd, _, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "stats" && cmd != "channelstats") || !m.IsChannel {
		return false
	}
	p.mu.Lock()
	stats := p.channels[strings.ToLower(m.Target)]
	response := formatChannelStats(m.Target, stats)
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), response)
	return true
}

func formatChannelStats(channel string, stats *channelStat) string {
	if stats == nil {
		return fmt.Sprintf("%s: no messages recorded yet", channel)
	}
	type userCount struct {
		nick  string
		count uint64
	}
	users := make([]userCount, 0, len(stats.users))
	for nick, count := range stats.users {
		users = append(users, userCount{nick, count})
	}
	sort.Slice(users, func(i, j int) bool {
		if users[i].count == users[j].count {
			return strings.ToLower(users[i].nick) < strings.ToLower(users[j].nick)
		}
		return users[i].count > users[j].count
	})
	if len(users) > 5 {
		users = users[:5]
	}
	top := make([]string, len(users))
	for i, user := range users {
		top[i] = fmt.Sprintf("%s %d", user.nick, user.count)
	}
	return fmt.Sprintf("%s: %d messages, %d users; top: %s", channel, stats.messages, len(stats.users), strings.Join(top, ", "))
}
