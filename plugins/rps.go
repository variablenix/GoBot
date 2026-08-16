package plugins

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type rpsRecord struct {
	Nick string `json:"nick"`
	Wins int    `json:"wins"`
	Loss int    `json:"losses"`
	Draw int    `json:"draws"`
}

type RPS struct {
	db        *storage.DB
	cooldown  scopedCooldown
	maxLength int
	mu        sync.Mutex
}

func (p *RPS) Name() string       { return "rps" }
func (p *RPS) Commands() []string { return []string{"rps", "rockpaperscissors"} }
func (p *RPS) Help() string {
	return "!rps <rock|paper|scissors>; !rps stats [nick]; !rps leaderboard (alias: !rockpaperscissors)"
}
func (p *RPS) Init(c bot.PluginConfig, db *storage.DB) error {
	p.db = db
	p.cooldown.configure(c.Int("cooldown_seconds", 5), 5)
	p.maxLength = c.Int("max_length", 300)
	if p.maxLength < 160 || p.maxLength > 400 {
		p.maxLength = 300
	}
	return nil
}
func (p *RPS) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "rps" && cmd != "rockpaperscissors") {
		return false
	}
	action := strings.Fields(strings.TrimSpace(arg))
	if len(action) > 0 && strings.EqualFold(action[0], "stats") {
		p.sendStats(b, m, action[1:])
		return true
	}
	if len(action) > 0 && strings.EqualFold(action[0], "leaderboard") {
		p.sendLeaderboard(b, m)
		return true
	}
	if len(action) != 1 {
		b.Send(m.ReplyTarget(), "usage: !rps <rock|paper|scissors>")
		return true
	}
	choice, valid := parseRPSChoice(action[0])
	if !valid {
		b.Send(m.ReplyTarget(), "choose rock, paper, or scissors")
		return true
	}
	if !p.cooldown.allow(scopedKey(b.Config.NetworkName, m.Target, pluginIdentity(m))) {
		return true
	}
	botChoice, err := secureRandomInt(3)
	if err != nil {
		b.Send(m.ReplyTarget(), "RPS is temporarily unavailable")
		return true
	}
	computer := []string{"rock", "paper", "scissors"}[botChoice]
	outcome := rpsOutcome(choice, computer)
	if err := p.record(b.Config.NetworkName, m.Target, m, outcome); err != nil {
		b.Send(m.ReplyTarget(), "RPS results could not be saved")
		return true
	}
	icon := map[string]string{"rock": "🤜", "paper": "✋", "scissors": "✌️"}[choice]
	b.Send(m.ReplyTarget(), truncateRunes(fmt.Sprintf("%s You threw %s, I threw %s — %s", icon, choice, computer, outcome), p.maxLength))
	return true
}

func parseRPSChoice(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "rock", "r":
		return "rock", true
	case "paper", "p":
		return "paper", true
	case "scissors", "s":
		return "scissors", true
	default:
		return "", false
	}
}

func rpsOutcome(player, computer string) string {
	if player == computer {
		return "Draw!"
	}
	if (player == "rock" && computer == "scissors") || (player == "paper" && computer == "rock") || (player == "scissors" && computer == "paper") {
		return "You win!"
	}
	return "I win!"
}

func (p *RPS) record(network, channel string, m bot.Message, outcome string) error {
	if p.db == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	key := scopedKey(network, channel, pluginIdentity(m))
	record := rpsRecord{Nick: cleanExternalText(m.Nick)}
	if raw, err := p.db.Get("rps", key); err == nil {
		if err := storage.Decode(raw, &record); err != nil {
			return err
		}
		record.Wins = maxNonNegative(record.Wins)
		record.Loss = maxNonNegative(record.Loss)
		record.Draw = maxNonNegative(record.Draw)
	}
	record.Nick = cleanExternalText(m.Nick)
	switch outcome {
	case "You win!":
		record.Wins++
	case "I win!":
		record.Loss++
	default:
		record.Draw++
	}
	return p.db.Set("rps", key, record)
}

func (p *RPS) sendStats(b *bot.Bot, m bot.Message, args []string) {
	if p.db == nil {
		b.Send(m.ReplyTarget(), "RPS stats are unavailable")
		return
	}
	identity := pluginIdentity(m)
	nick := m.Nick
	if len(args) > 0 {
		if len(args) != 1 {
			b.Send(m.ReplyTarget(), "usage: !rps stats [nick]")
			return
		}
		nick = cleanExternalText(args[0])
		identity = "nick:" + strings.ToLower(args[0])
	}
	record, ok := p.findRecord(b.Config.NetworkName, m.Target, identity, nick)
	if !ok {
		b.Send(m.ReplyTarget(), truncateRunes(fmt.Sprintf("📊 No RPS record for %s in %s", cleanExternalText(nick), cleanExternalText(m.Target)), p.maxLength))
		return
	}
	label := "Your"
	if len(args) > 0 {
		label = cleanExternalText(nick)
	}
	b.Send(m.ReplyTarget(), truncateRunes(fmt.Sprintf("📊 %s RPS record in %s: %dW %dL %dD", label, cleanExternalText(m.Target), record.Wins, record.Loss, record.Draw), p.maxLength))
}

func (p *RPS) findRecord(network, channel, identity, nick string) (rpsRecord, bool) {
	prefix := scopedKey(network, channel, "")
	keys, _ := p.db.List("rps")
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		raw, err := p.db.Get("rps", key)
		if err != nil {
			continue
		}
		var record rpsRecord
		if storage.Decode(raw, &record) == nil && (key == scopedKey(network, channel, identity) || (nick != "" && strings.EqualFold(strings.TrimSpace(record.Nick), strings.TrimSpace(nick)))) {
			return record, true
		}
	}
	return rpsRecord{}, false
}

type rpsLeader struct {
	Nick string
	Wins int
}

func (p *RPS) sendLeaderboard(b *bot.Bot, m bot.Message) {
	leaders := p.leaderboard(b.Config.NetworkName, m.Target)
	if len(leaders) == 0 {
		b.Send(m.ReplyTarget(), "🏆 No RPS games have been played in this channel")
		return
	}
	parts := make([]string, len(leaders))
	for i, leader := range leaders {
		parts[i] = fmt.Sprintf("%s %dW", cleanExternalText(leader.Nick), leader.Wins)
	}
	b.Send(m.ReplyTarget(), truncateRunes("🏆 RPS leaders: "+strings.Join(parts, ", "), 300))
}

func (p *RPS) leaderboard(network, channel string) []rpsLeader {
	if p.db == nil {
		return nil
	}
	prefix := scopedKey(network, channel, "")
	keys, _ := p.db.List("rps")
	leaders := make([]rpsLeader, 0, len(keys))
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		raw, err := p.db.Get("rps", key)
		if err != nil {
			continue
		}
		var record rpsRecord
		if storage.Decode(raw, &record) == nil && record.Nick != "" {
			leaders = append(leaders, rpsLeader{Nick: record.Nick, Wins: record.Wins})
		}
	}
	sort.SliceStable(leaders, func(i, j int) bool {
		if leaders[i].Wins != leaders[j].Wins {
			return leaders[i].Wins > leaders[j].Wins
		}
		return strings.ToLower(leaders[i].Nick) < strings.ToLower(leaders[j].Nick)
	})
	if len(leaders) > 5 {
		leaders = leaders[:5]
	}
	return leaders
}
