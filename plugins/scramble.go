package plugins

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const scrambleScoreBucket = "scramble_scores"

type Scramble struct {
	mu        sync.Mutex
	db        *storage.DB
	words     []string
	games     map[string]scrambleGame
	timeout   time.Duration
	maxLength int
}

type scrambleGame struct {
	Word      string
	Scrambled string
	StartedAt time.Time
}

type scrambleScore struct {
	Wins uint64 `json:"wins"`
}

func (p *Scramble) Name() string       { return "scramble" }
func (p *Scramble) Commands() []string { return []string{"scramble", "scrambleword"} }
func (p *Scramble) Help() string {
	return "!scramble — start a local word scramble; first correct channel answer wins +1 karma; !scramble status shows the active round"
}

func (p *Scramble) Init(c bot.PluginConfig, db *storage.DB) error {
	p.db = db
	p.timeout = time.Duration(c.Int("timeout_minutes", 5)) * time.Minute
	if p.timeout < time.Minute || p.timeout > time.Hour {
		p.timeout = 5 * time.Minute
	}
	p.maxLength = c.Int("max_length", 240)
	if p.maxLength < 100 || p.maxLength > 400 {
		p.maxLength = 240
	}
	path := c.String("data_file", filepath.Join("data", "scramble.txt"))
	p.words = readScrambleWords(path)
	if len(p.words) == 0 {
		p.words = append([]string(nil), scrambleFallbackWords...)
	}
	p.mu.Lock()
	p.games = make(map[string]scrambleGame)
	p.mu.Unlock()
	return nil
}

func (p *Scramble) Handle(b *bot.Bot, m bot.Message) bool {
	if !m.IsChannel || strings.TrimSpace(m.Nick) == "" {
		return false
	}
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if ok && (strings.EqualFold(cmd, "scramble") || strings.EqualFold(cmd, "scrambleword")) {
		p.command(b, m, strings.TrimSpace(arg))
		return true
	}
	if ok {
		return false
	}
	return p.answer(b, m)
}

func (p *Scramble) command(b *bot.Bot, m bot.Message, arg string) {
	key := duckHuntStateKey(b.Config.NetworkName, m.Target)
	p.mu.Lock()
	if game, exists := p.games[key]; exists && time.Since(game.StartedAt) >= p.timeout {
		delete(p.games, key)
	}
	if game, exists := p.games[key]; exists {
		p.mu.Unlock()
		if strings.EqualFold(arg, "status") {
			b.Send(m.ReplyTarget(), fmt.Sprintf("%s is active: %s", ircColor(ircGreen, "[Scramble]"), ircColor(ircYellow, game.Scrambled)))
		} else {
			b.Send(m.ReplyTarget(), fmt.Sprintf("%s round already active: %s", ircColor(ircYellow, "[Scramble]"), ircColor(ircCyan, game.Scrambled)))
		}
		return
	}
	if strings.EqualFold(arg, "status") {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "no scramble is active; use !scramble to start one"))
		return
	}
	word := p.words[rand.Intn(len(p.words))]
	p.games[key] = scrambleGame{Word: word, Scrambled: scrambleWord(word), StartedAt: time.Now()}
	game := p.games[key]
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), truncateIRCMessage(fmt.Sprintf("%s Unscramble %s — first correct answer wins +1 karma!", ircColor(ircGreen, "[Scramble]"), ircColor(ircYellow, game.Scrambled)), p.maxLength))
}

func (p *Scramble) answer(b *bot.Bot, m bot.Message) bool {
	answer := strings.ToLower(strings.TrimSpace(m.Text))
	key := duckHuntStateKey(b.Config.NetworkName, m.Target)
	p.mu.Lock()
	game, exists := p.games[key]
	if !exists || time.Since(game.StartedAt) >= p.timeout || answer != strings.ToLower(game.Word) {
		if exists && time.Since(game.StartedAt) >= p.timeout {
			delete(p.games, key)
		}
		p.mu.Unlock()
		return false
	}
	delete(p.games, key)
	karma := &Karma{db: p.db}
	channelValue, globalValue, err := karma.changeScoped(b.Config.NetworkName, m.Target, strings.ToLower(m.Nick), 1)
	if err == nil {
		p.incrementScoreLocked(b.Config.NetworkName, m.Target, m.Nick)
	}
	p.mu.Unlock()
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "scramble was solved, but the karma point could not be saved"))
		return true
	}
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s %s solved it! +1 karma (🎯 %d in %s | 🌐 %d global)", ircColor(ircGreen, "[Scramble]"), m.Nick, channelValue, m.Target, globalValue))
	return true
}

func (p *Scramble) incrementScoreLocked(network, channel, nick string) {
	if p.db == nil {
		return
	}
	key := duckHuntScoreKey(network, channel, nick)
	score := scrambleScore{}
	if raw, err := p.db.Get(scrambleScoreBucket, key); err == nil {
		_ = storage.Decode(raw, &score)
	}
	score.Wins++
	_ = p.db.Set(scrambleScoreBucket, key, score)
}

func readScrambleWords(path string) []string {
	lines := readFoodList(path)
	seen := make(map[string]struct{})
	words := make([]string, 0, len(lines))
	for _, line := range lines {
		word := strings.ToLower(strings.TrimSpace(line))
		if !validScrambleWord(word) {
			continue
		}
		if _, exists := seen[word]; exists {
			continue
		}
		seen[word] = struct{}{}
		words = append(words, word)
	}
	return words
}

func validScrambleWord(word string) bool {
	if word == "" || len([]rune(word)) < 3 || len([]rune(word)) > 32 {
		return false
	}
	for _, r := range word {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

func scrambleWord(word string) string {
	runes := []rune(word)
	for i := len(runes) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		runes[i], runes[j] = runes[j], runes[i]
	}
	result := string(runes)
	if result == word && len(runes) > 1 {
		runes[0], runes[1] = runes[1], runes[0]
		result = string(runes)
	}
	return result
}

var scrambleFallbackWords = []string{"network", "resilient", "terminal", "database", "firewall", "channel", "operator", "package", "monitor", "service"}
