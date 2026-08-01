package plugins

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const duckHuntBucket = "duckhunt_scores"

type duckHuntConfig struct {
	minimumMessages int
	minimumUsers    int
	minDelay        time.Duration
	maxDelay        time.Duration
	timeout         time.Duration
}

type duckHuntState struct {
	channel   string
	messages  int
	users     map[string]struct{}
	nextSpawn time.Time
	spawnedAt time.Time
	active    bool
}

type duckScore struct {
	Ducks uint64 `json:"ducks"`
}

// DuckHunt is an optional channel activity event. It does not have rounds or
// wagers: a duck appears after enough activity, and the first !bang records a
// persistent per-channel score for the shooter.
type DuckHunt struct {
	mu     sync.Mutex
	db     *storage.DB
	cfg    duckHuntConfig
	states map[string]*duckHuntState
}

func (p *DuckHunt) Name() string       { return "duckhunt" }
func (p *DuckHunt) Commands() []string { return []string{"bang", "ducks"} }
func (p *DuckHunt) Help() string {
	return "!bang — shoot a duck when one appears; !ducks [nick] — show persistent duck kills"
}

func (p *DuckHunt) Init(c bot.PluginConfig, db *storage.DB) error {
	minimumMessages := c.Int("minimum_messages", 25)
	minimumUsers := c.Int("minimum_users", 2)
	minDelay := c.Int("min_delay_seconds", 60)
	maxDelay := c.Int("max_delay_seconds", 300)
	timeout := c.Int("timeout_seconds", 30)

	if minimumMessages < 1 {
		minimumMessages = 1
	}
	if minimumUsers < 1 {
		minimumUsers = 1
	}
	if minDelay < 1 {
		minDelay = 1
	}
	if maxDelay < minDelay {
		maxDelay = minDelay
	}
	if timeout < 1 {
		timeout = 1
	}

	p.db = db
	p.cfg = duckHuntConfig{
		minimumMessages: minimumMessages,
		minimumUsers:    minimumUsers,
		minDelay:        time.Duration(minDelay) * time.Second,
		maxDelay:        time.Duration(maxDelay) * time.Second,
		timeout:         time.Duration(timeout) * time.Second,
	}
	p.states = make(map[string]*duckHuntState)
	return nil
}

// Start checks channel states in the background. GoBot intentionally keeps
// this event in memory; only scores are persisted, so a restart cannot create
// a surprise duck in a channel that was quiet before the restart.
func (p *DuckHunt) Start(b *bot.Bot) {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			p.tick(b)
		}
	}()
}

func (p *DuckHunt) Handle(b *bot.Bot, m bot.Message) bool {
	if m.Nick == "" || !m.IsChannel {
		return false
	}

	cmd, arg, isCommand := bot.IsCommand(m, b.Config.CommandPrefix)
	if !isCommand {
		p.recordActivity(b.Config.NetworkName, m.Target, m.Nick)
		return false
	}

	switch strings.ToLower(cmd) {
	case "bang":
		p.bang(b, m)
		return true
	case "ducks":
		p.ducks(b, m, arg)
		return true
	default:
		return false
	}
}

func (p *DuckHunt) recordActivity(network, channel, nick string) {
	now := time.Now()
	key := duckHuntStateKey(network, channel)

	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.states[key]
	if state == nil {
		state = &duckHuntState{channel: channel, users: make(map[string]struct{})}
		p.states[key] = state
	}
	if state.active {
		return
	}
	state.messages++
	state.users[strings.ToLower(nick)] = struct{}{}
	if state.nextSpawn.IsZero() && state.messages >= p.cfg.minimumMessages && len(state.users) >= p.cfg.minimumUsers {
		state.nextSpawn = now.Add(randomDuckDelay(p.cfg))
	}
}

func (p *DuckHunt) tick(b *bot.Bot) {
	now := time.Now()
	type outgoing struct {
		target string
		text   string
	}
	var messages []outgoing

	p.mu.Lock()
	for _, state := range p.states {
		if state.active {
			if now.Sub(state.spawnedAt) < p.cfg.timeout {
				continue
			}
			state.active = false
			state.spawnedAt = time.Time{}
			state.messages = 0
			state.users = make(map[string]struct{})
			messages = append(messages, outgoing{
				target: state.channel,
				text:   "The duck flew away—too slow!",
			})
			continue
		}
		if state.nextSpawn.IsZero() || now.Before(state.nextSpawn) {
			continue
		}
		state.active = true
		state.spawnedAt = now
		state.nextSpawn = time.Time{}
		state.messages = 0
		state.users = make(map[string]struct{})
		messages = append(messages, outgoing{
			target: state.channel,
			text:   randomDuckAnnouncement(),
		})
	}
	p.mu.Unlock()

	for _, message := range messages {
		b.Send(message.target, message.text)
	}
}

func (p *DuckHunt) bang(b *bot.Bot, m bot.Message) {
	key := duckHuntStateKey(b.Config.NetworkName, m.Target)
	now := time.Now()
	p.mu.Lock()
	state := p.states[key]
	if state == nil || !state.active {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), "There is no duck right now.")
		return
	}

	elapsed := now.Sub(state.spawnedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	state.active = false
	state.spawnedAt = time.Time{}
	state.nextSpawn = time.Time{}
	state.messages = 0
	state.users = make(map[string]struct{})
	kills := p.incrementScore(b.Config.NetworkName, m.Target, m.Nick)
	p.mu.Unlock()

	b.Send(m.ReplyTarget(), fmt.Sprintf("%s shot a duck in %.3f seconds! You have killed %d duck%s in %s.", m.Nick, elapsed.Seconds(), kills, duckPlural(kills), m.Target))
}

func (p *DuckHunt) ducks(b *bot.Bot, m bot.Message, arg string) {
	nick := strings.TrimSpace(arg)
	if nick == "" {
		nick = m.Nick
	}
	if strings.ContainsAny(nick, " \r\n\t") || len([]rune(nick)) > 64 {
		b.Send(m.ReplyTarget(), "usage: !ducks [nick]")
		return
	}

	kills := p.score(b.Config.NetworkName, m.Target, nick)
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s has killed %d duck%s in %s.", nick, kills, duckPlural(kills), m.Target))
}

func (p *DuckHunt) incrementScore(network, channel, nick string) uint64 {
	score := p.score(network, channel, nick)
	score++
	if p.db != nil {
		_ = p.db.Set(duckHuntBucket, duckHuntScoreKey(network, channel, nick), duckScore{Ducks: score})
	}
	return score
}

func (p *DuckHunt) score(network, channel, nick string) uint64 {
	if p.db == nil {
		return 0
	}
	raw, err := p.db.Get(duckHuntBucket, duckHuntScoreKey(network, channel, nick))
	if err != nil {
		return 0
	}
	var saved duckScore
	if storage.Decode(raw, &saved) != nil {
		return 0
	}
	return saved.Ducks
}

func randomDuckDelay(c duckHuntConfig) time.Duration {
	if c.maxDelay <= c.minDelay {
		return c.minDelay
	}
	span := int((c.maxDelay - c.minDelay) / time.Second)
	return c.minDelay + time.Duration(rand.Intn(span+1))*time.Second
}

func randomDuckAnnouncement() string {
	ducks := []string{`\_o<`, `\_O<`, `\_0<`, `\_ö<`}
	noises := []string{"quack!", "QUACK!", "flap flap!"}
	return fmt.Sprintf("A wild duck appeared: %s %s Type !bang to shoot it!", ducks[rand.Intn(len(ducks))], noises[rand.Intn(len(noises))])
}

func duckPlural(count uint64) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func duckHuntStateKey(network, channel string) string {
	return strings.ToLower(network) + "\x00" + strings.ToLower(channel)
}

func duckHuntScoreKey(network, channel, nick string) string {
	return duckHuntStateKey(network, channel) + "\x00" + strings.ToLower(nick)
}
