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
	Messages uint64            `json:"messages"`
	Users    map[string]uint64 `json:"users"`
}

type ChannelStats struct {
	mu       sync.Mutex
	channels map[string]*channelStat
	db       *storage.DB
}

func (p *ChannelStats) Name() string       { return "channelstats" }
func (p *ChannelStats) Commands() []string { return []string{"stats", "chanstats", "channelstats"} }
func (p *ChannelStats) Help() string {
	return "!stats — show persistent message and user statistics for this channel"
}
func (p *ChannelStats) Init(_ bot.PluginConfig, db *storage.DB) error {
	p.channels = make(map[string]*channelStat)
	p.db = db
	if db != nil {
		for _, key := range mustChannelStatsList(db) {
			if raw, err := db.Get("channelstats", key); err == nil {
				var saved channelStat
				if storage.Decode(raw, &saved) == nil && saved.Users != nil {
					p.channels[key] = &saved
				}
			}
		}
	}
	return nil
}
func (p *ChannelStats) Handle(b *bot.Bot, m bot.Message) bool {
	if m.Command == "PRIVMSG" && m.IsChannel && m.Nick != "" {
		p.mu.Lock()
		key := channelStatsKey(b.Config.NetworkName, m.Target)
		stats := p.channels[key]
		if stats == nil {
			stats = &channelStat{Users: make(map[string]uint64)}
			p.channels[key] = stats
		}
		stats.Messages++
		stats.Users[m.Nick]++
		if p.db != nil {
			_ = p.db.Set("channelstats", key, stats)
		}
		p.mu.Unlock()
	}
	cmd, _, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "stats" && cmd != "channelstats") || !m.IsChannel {
		return false
	}
	p.mu.Lock()
	stats := p.channels[channelStatsKey(b.Config.NetworkName, m.Target)]
	response := formatChannelStats(m.Target, stats)
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), ircColor(ircCyan, response))
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
	users := make([]userCount, 0, len(stats.Users))
	for nick, count := range stats.Users {
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
	return fmt.Sprintf("%s: %d messages, %d users; top: %s", channel, stats.Messages, len(stats.Users), strings.Join(top, ", "))
}

func channelStatsKey(network, channel string) string {
	return strings.ToLower(network) + "\x00" + strings.ToLower(channel)
}

func mustChannelStatsList(db *storage.DB) []string {
	keys, _ := db.List("channelstats")
	return keys
}
