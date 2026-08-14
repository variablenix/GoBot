package plugins

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/variablenix/GoBot/bot"
)

const blackjackStatsBucket = "blackjack_stats"

type blackjackStats struct {
	Name          string `json:"name"`
	Hands         uint64 `json:"hands"`
	Wins          uint64 `json:"wins"`
	Losses        uint64 `json:"losses"`
	Pushes        uint64 `json:"pushes"`
	Blackjacks    uint64 `json:"blackjacks"`
	Busts         uint64 `json:"busts"`
	CurrentStreak int    `json:"current_streak"`
	BestStreak    uint64 `json:"best_streak"`
}

func (p *Blackjack) recordResult(m bot.Message, game *blackjackGame) {
	if p.db == nil || game == nil {
		return
	}
	key := blackjackStatsKey(m)
	stats := blackjackStats{Name: blackjackStatsName(m)}
	if raw, err := p.db.Get(blackjackStatsBucket, key); err == nil {
		_ = json.Unmarshal(raw, &stats)
	}
	// Treat malformed persisted data defensively; a negative streak must not
	// be converted to uint64 and become an enormous leaderboard value.
	if stats.CurrentStreak < 0 {
		stats.CurrentStreak = 0
	}
	stats.Name = blackjackStatsName(m)
	stats.Hands++
	playerValue, _ := blackjackHandValue(game.player)
	dealerValue, _ := blackjackHandValue(game.dealer)
	if len(game.player) == 2 && playerValue == 21 {
		stats.Blackjacks++
	}
	switch {
	case playerValue > 21:
		stats.Losses++
		stats.Busts++
		stats.CurrentStreak = 0
	case dealerValue > 21 || playerValue > dealerValue:
		stats.Wins++
		stats.CurrentStreak++
		if stats.CurrentStreak > 0 {
			currentStreak := uint64(stats.CurrentStreak) // #nosec G115 -- negative streaks are normalized above.
			if currentStreak > stats.BestStreak {
				stats.BestStreak = currentStreak
			}
		}
	case playerValue == dealerValue:
		stats.Pushes++
		stats.CurrentStreak = 0
	default:
		stats.Losses++
		stats.CurrentStreak = 0
	}
	_ = p.db.Set(blackjackStatsBucket, key, stats)
}

func (p *Blackjack) playerStats(m bot.Message) string {
	name := blackjackStatsName(m)
	if p.db == nil {
		return ircColor(ircYellow, "Blackjack statistics are unavailable")
	}
	var stats blackjackStats
	if raw, err := p.db.Get(blackjackStatsBucket, blackjackStatsKey(m)); err != nil || json.Unmarshal(raw, &stats) != nil || stats.Hands == 0 {
		return fmt.Sprintf("No Blackjack stats for %s yet.", name)
	}
	return formatBlackjackStats(name, stats)
}

func (p *Blackjack) leaderboard() string {
	if p.db == nil {
		return ircColor(ircYellow, "Blackjack leaderboard is unavailable")
	}
	keys, _ := p.db.List(blackjackStatsBucket)
	type entry struct {
		name  string
		stats blackjackStats
	}
	entries := make([]entry, 0, len(keys))
	for _, key := range keys {
		raw, err := p.db.Get(blackjackStatsBucket, key)
		if err != nil {
			continue
		}
		var stats blackjackStats
		if json.Unmarshal(raw, &stats) == nil && stats.Hands > 0 {
			entries = append(entries, entry{name: stats.Name, stats: stats})
		}
	}
	if len(entries) == 0 {
		return "No Blackjack scores yet."
	}
	sort.Slice(entries, func(i, j int) bool {
		left, right := entries[i].stats, entries[j].stats
		if left.Wins != right.Wins {
			return left.Wins > right.Wins
		}
		if left.Hands != right.Hands {
			return left.Hands > right.Hands
		}
		return strings.ToLower(entries[i].name) < strings.ToLower(entries[j].name)
	})
	if len(entries) > 5 {
		entries = entries[:5]
	}
	parts := make([]string, len(entries))
	for i, item := range entries {
		parts[i] = fmt.Sprintf("%d. %s %dW-%dL-%dP (%s)", i+1, item.name, item.stats.Wins, item.stats.Losses, item.stats.Pushes, blackjackWinRate(item.stats))
	}
	return ircBold + "Blackjack leaderboard:" + ircReset + " " + strings.Join(parts, " | ")
}

func formatBlackjackStats(name string, stats blackjackStats) string {
	return fmt.Sprintf("%s: %d hands | %dW-%dL-%dP | %d blackjack%s | win rate %s | streak %d (best %d)", name, stats.Hands, stats.Wins, stats.Losses, stats.Pushes, stats.Blackjacks, plural(stats.Blackjacks), blackjackWinRate(stats), stats.CurrentStreak, stats.BestStreak)
}

func blackjackWinRate(stats blackjackStats) string {
	if stats.Hands == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.1f%%", float64(stats.Wins)/float64(stats.Hands)*100)
}

func plural(count uint64) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func blackjackStatsKey(m bot.Message) string {
	identity := strings.TrimSpace(m.Account)
	if identity == "" {
		identity = strings.TrimSpace(m.Nick)
	}
	return strings.ToLower(identity)
}

func blackjackStatsName(m bot.Message) string {
	if strings.TrimSpace(m.Account) != "" {
		return strings.TrimSpace(m.Account)
	}
	return strings.TrimSpace(m.Nick)
}
