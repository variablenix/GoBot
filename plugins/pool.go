package plugins

import (
	cryptorand "crypto/rand"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const (
	poolStatsBucket       = "pool_stats"
	poolChallengeLifetime = 2 * time.Minute
	poolDefaultGameTTL    = 30 * time.Minute
	poolDefaultTurnTTL    = 2 * time.Minute
	poolDefaultShotChance = 65
)

type poolMode string

const (
	poolEightBall poolMode = "8-ball"
	poolNineBall  poolMode = "9-ball"
)

type poolPlayer struct {
	Key  string
	Name string
}

type poolGame struct {
	Mode        poolMode
	Players     [2]poolPlayer
	Turn        int
	Pending     bool
	Created     time.Time
	LastAction  time.Time
	ChallengeBy int
	Balls       map[int]bool
	Groups      [2]string
}

type poolConfig struct {
	GameTTL    time.Duration
	TurnTTL    time.Duration
	ShotChance int
}

type poolStats struct {
	EightWins   uint64 `json:"eight_wins"`
	EightLosses uint64 `json:"eight_losses"`
	NineWins    uint64 `json:"nine_wins"`
	NineLosses  uint64 `json:"nine_losses"`
}

// Pool is a lightweight, turn-based pool simulation. It models legal target
// selection and pocket/miss outcomes rather than attempting graphical physics.
// Active tables are intentionally in memory; completed records are persisted.
type Pool struct {
	mu    sync.Mutex
	db    *storage.DB
	cfg   poolConfig
	games map[string]*poolGame
}

func (p *Pool) Name() string { return "pool" }

func (p *Pool) Commands() []string {
	return []string{"pool", "pool8", "8pool", "pool9", "nineball", "9ball", "9", "shoot", "forfeit", "poolstats", "poolleaderboard"}
}

func (p *Pool) Help() string {
	return "!pool 8 <nick> or !pool 9 <nick> — challenge someone; !pool accept|status|shoot [ball]|forfeit; !poolstats"
}

func (p *Pool) Init(c bot.PluginConfig, db *storage.DB) error {
	gameMinutes := c.Int("game_timeout_minutes", 30)
	turnSeconds := c.Int("turn_timeout_seconds", 120)
	shotChance := c.Int("shot_success_percent", poolDefaultShotChance)
	if gameMinutes < 1 || gameMinutes > 240 {
		gameMinutes = 30
	}
	if turnSeconds < 15 || turnSeconds > 3600 {
		turnSeconds = 120
	}
	if shotChance < 1 || shotChance > 100 {
		shotChance = poolDefaultShotChance
	}
	p.db = db
	p.cfg = poolConfig{
		GameTTL:    time.Duration(gameMinutes) * time.Minute,
		TurnTTL:    time.Duration(turnSeconds) * time.Second,
		ShotChance: shotChance,
	}
	p.games = make(map[string]*poolGame)
	return nil
}

func (p *Pool) Handle(b *bot.Bot, m bot.Message) bool {
	if !m.IsChannel {
		return false
	}
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || !poolCommand(cmd) {
		return false
	}

	cmd = strings.ToLower(cmd)
	if cmd == "poolstats" || cmd == "poolleaderboard" {
		if cmd == "poolstats" {
			b.Send(m.ReplyTarget(), p.statsResponse(m, strings.TrimSpace(arg)))
		} else {
			b.Send(m.ReplyTarget(), p.leaderboardResponse())
		}
		return true
	}

	key := poolTableKey(b.Config.NetworkName, m.Target)
	var response string
	p.mu.Lock()
	game := p.games[key]
	if game != nil {
		if expired := p.expireLocked(key, game); expired != "" {
			response = expired
			game = nil
		}
	}
	if response == "" {
		response = p.handleLocked(b, m, key, game, cmd, strings.TrimSpace(arg))
	}
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), response)
	return true
}

func poolCommand(command string) bool {
	switch strings.ToLower(command) {
	case "pool", "pool8", "8pool", "pool9", "nineball", "9ball", "9", "shoot", "forfeit", "poolstats", "poolleaderboard":
		return true
	default:
		return false
	}
}

func (p *Pool) handleLocked(b *bot.Bot, m bot.Message, key string, game *poolGame, command, arg string) string {
	switch command {
	case "pool8", "8pool":
		return p.challengeLocked(key, m, poolEightBall, arg)
	case "pool9", "nineball", "9ball", "9":
		return p.challengeLocked(key, m, poolNineBall, arg)
	case "shoot":
		return p.shootLocked(m, key, game, arg)
	case "forfeit":
		return p.forfeitLocked(m, key, game)
	case "pool":
		return p.poolCommandLocked(key, m, game, arg)
	default:
		return "unknown pool command; use !pool status or !help pool"
	}
}

func (p *Pool) poolCommandLocked(key string, m bot.Message, game *poolGame, arg string) string {
	parts := strings.Fields(arg)
	if len(parts) == 0 {
		return p.statusLocked(m, game)
	}
	action := strings.ToLower(parts[0])
	rest := strings.TrimSpace(strings.TrimPrefix(arg, parts[0]))
	switch action {
	case "8", "8ball":
		return p.challengeLocked(key, m, poolEightBall, rest)
	case "9", "9ball", "nineball":
		return p.challengeLocked(key, m, poolNineBall, rest)
	case "accept":
		return p.acceptLocked(m, key, game)
	case "decline", "reject":
		return p.declineLocked(m, key, game)
	case "status":
		return p.statusLocked(m, game)
	case "shoot", "break":
		if action == "break" && rest == "" && game != nil && game.Mode == poolNineBall {
			rest = "1"
		}
		return p.shootLocked(m, key, game, rest)
	case "forfeit", "quit", "leave":
		return p.forfeitLocked(m, key, game)
	default:
		return "usage: !pool 8 <nick> | !pool 9 <nick> | !pool accept|status|shoot [ball]|forfeit"
	}
}

func (p *Pool) challengeLocked(key string, m bot.Message, mode poolMode, rawNick string) string {
	if strings.TrimSpace(rawNick) == "" {
		return fmt.Sprintf("usage: !%s <nick>", map[poolMode]string{poolEightBall: "pool 8", poolNineBall: "pool 9"}[mode])
	}
	nick := strings.TrimSpace(strings.TrimPrefix(rawNick, "@"))
	if !validPoolNick(nick) {
		return "please provide one IRC nickname without spaces"
	}
	if strings.EqualFold(nick, m.Nick) || strings.EqualFold(nick, m.Account) {
		return "you need another player for pool"
	}
	if game := p.games[key]; game != nil {
		return p.statusLocked(m, game)
	}
	challenger := poolPlayer{Key: poolIdentity(m), Name: safePoolName(m.Nick)}
	opponent := poolPlayer{Key: strings.ToLower(nick), Name: safePoolName(nick)}
	p.games[key] = &poolGame{
		Mode:        mode,
		Players:     [2]poolPlayer{challenger, opponent},
		Pending:     true,
		Created:     time.Now(),
		LastAction:  time.Now(),
		ChallengeBy: 0,
	}
	return fmt.Sprintf("%s challenges %s to %s. %s, type !pool accept within 2 minutes.", challenger.Name, opponent.Name, mode, opponent.Name)
}

func (p *Pool) acceptLocked(m bot.Message, key string, game *poolGame) string {
	if game == nil || !game.Pending {
		return "there is no pending pool challenge for this channel"
	}
	if !strings.EqualFold(m.Nick, game.Players[1].Name) && !strings.EqualFold(m.Account, game.Players[1].Name) {
		return fmt.Sprintf("only %s can accept this challenge", game.Players[1].Name)
	}
	game.Players[1].Key = poolIdentity(m)
	game.Pending = false
	game.Created = time.Now()
	game.LastAction = game.Created
	game.Turn = 0
	game.Balls = newPoolBalls(game.Mode)
	if game.Mode == poolEightBall {
		return fmt.Sprintf("%s started: %s vs %s. %s breaks. Use !pool shoot <ball>.", game.Mode, game.Players[0].Name, game.Players[1].Name, game.Players[0].Name)
	}
	return fmt.Sprintf("%s started: %s vs %s. %s breaks; the lowest ball must be hit first. Use !pool shoot <ball>.", game.Mode, game.Players[0].Name, game.Players[1].Name, game.Players[0].Name)
}

func (p *Pool) declineLocked(m bot.Message, key string, game *poolGame) string {
	if game == nil || !game.Pending {
		return "there is no pending pool challenge for this channel"
	}
	if !strings.EqualFold(m.Nick, game.Players[1].Name) && !strings.EqualFold(m.Account, game.Players[1].Name) {
		return fmt.Sprintf("only %s can decline this challenge", game.Players[1].Name)
	}
	delete(p.games, key)
	return fmt.Sprintf("%s declined the %s challenge.", game.Players[1].Name, game.Mode)
}

func (p *Pool) shootLocked(m bot.Message, key string, game *poolGame, rawBall string) string {
	if game == nil || game.Pending {
		return "no pool game is active; use !pool 8 <nick> or !pool 9 <nick>"
	}
	player := p.currentPlayer(game)
	if !samePoolIdentity(m, player) {
		return fmt.Sprintf("it is %s's turn", player.Name)
	}
	ball, err := requestedPoolBall(rawBall)
	if err != nil && strings.TrimSpace(rawBall) != "" {
		return "usage: !pool shoot [ball number]"
	}
	if err != nil {
		ball = p.defaultTarget(game)
		if ball == 0 {
			return "there is no legal target available"
		}
	}
	legal, reason := p.legalTarget(game, ball)
	if !legal {
		return reason
	}
	game.LastAction = time.Now()
	if !poolShotSucceeds(p.cfg.ShotChance) {
		game.Turn = 1 - game.Turn
		return fmt.Sprintf("%s misses; %s's turn.", player.Name, p.currentPlayer(game).Name)
	}

	if game.Mode == poolNineBall {
		delete(game.Balls, ball)
		if ball == 9 {
			return p.finishLocked(key, game, game.Turn, fmt.Sprintf("%s pockets the 9-ball and wins!", player.Name))
		}
		return fmt.Sprintf("%s pockets the %d-ball and continues. %s", player.Name, ball, p.remainingNineBall(game))
	}

	if ball == 8 {
		delete(game.Balls, ball)
		return p.finishLocked(key, game, game.Turn, fmt.Sprintf("%s pockets the 8-ball and wins!", player.Name))
	}
	group := poolGroup(ball)
	if game.Groups[game.Turn] == "" {
		game.Groups[game.Turn] = group
		game.Groups[1-game.Turn] = oppositePoolGroup(group)
	}
	delete(game.Balls, ball)
	remaining := p.remainingGroup(game, game.Turn)
	if remaining == 0 {
		return fmt.Sprintf("%s pockets the %d-ball (%s) and clears the group; continue with !pool shoot 8.", player.Name, ball, group)
	}
	return fmt.Sprintf("%s pockets the %d-ball (%s) and continues; %d group ball%s remain.", player.Name, ball, group, remaining, pluralPool(remaining))
}

func (p *Pool) forfeitLocked(m bot.Message, key string, game *poolGame) string {
	if game == nil || game.Pending {
		return "no active pool game to forfeit"
	}
	playerIndex := p.playerIndex(game, m)
	if playerIndex < 0 {
		return "you are not a player in this pool game"
	}
	winner := 1 - playerIndex
	return p.finishLocked(key, game, winner, fmt.Sprintf("%s forfeits; %s wins.", game.Players[playerIndex].Name, game.Players[winner].Name))
}

func (p *Pool) statusLocked(m bot.Message, game *poolGame) string {
	if game == nil {
		return "no pool game is active; use !pool 8 <nick> or !pool 9 <nick>"
	}
	if game.Pending {
		return fmt.Sprintf("pending %s challenge: %s invited %s; waiting for !pool accept.", game.Mode, game.Players[0].Name, game.Players[1].Name)
	}
	turn := p.currentPlayer(game)
	if game.Mode == poolNineBall {
		return fmt.Sprintf("%s: %s vs %s | turn: %s | lowest ball: %d", game.Mode, game.Players[0].Name, game.Players[1].Name, turn.Name, lowestPoolBall(game))
	}
	group := "open table"
	if game.Groups[0] != "" {
		group = fmt.Sprintf("%s: %s; %s: %s", game.Players[0].Name, game.Groups[0], game.Players[1].Name, game.Groups[1])
	}
	return fmt.Sprintf("%s: %s vs %s | turn: %s | %s | balls left: %d", game.Mode, game.Players[0].Name, game.Players[1].Name, turn.Name, group, len(game.Balls))
}

func (p *Pool) legalTarget(game *poolGame, ball int) (bool, string) {
	if !game.Balls[ball] {
		return false, "that ball is no longer on the table"
	}
	if game.Mode == poolNineBall {
		lowest := lowestPoolBall(game)
		if ball != lowest {
			return false, fmt.Sprintf("you must hit the lowest ball first: %d", lowest)
		}
		return true, ""
	}
	if ball == 8 {
		if game.Groups[game.Turn] == "" {
			return false, "the 8-ball is only legal after your group is assigned and cleared"
		}
		if p.remainingGroup(game, game.Turn) != 0 {
			return false, "clear your group before shooting the 8-ball"
		}
		return true, ""
	}
	if ball < 1 || ball > 15 {
		return false, "choose an 8-ball target from 1-7, 9-15, or 8 when your group is clear"
	}
	if game.Groups[game.Turn] != "" && poolGroup(ball) != game.Groups[game.Turn] {
		return false, fmt.Sprintf("your group is %s; choose one of your remaining balls", game.Groups[game.Turn])
	}
	return true, ""
}

func (p *Pool) finishLocked(key string, game *poolGame, winner int, message string) string {
	loser := 1 - winner
	p.recordResult(game.Mode, game.Players[winner], game.Players[loser])
	delete(p.games, key)
	return message
}

func (p *Pool) expireLocked(key string, game *poolGame) string {
	now := time.Now()
	lifetime := p.cfg.GameTTL
	if game.Pending {
		lifetime = poolChallengeLifetime
	}
	if !game.Pending && now.Sub(game.LastAction) > p.cfg.TurnTTL {
		winner := 1 - game.Turn
		message := fmt.Sprintf("pool turn expired; %s wins by timeout.", game.Players[winner].Name)
		return p.finishLocked(key, game, winner, message)
	}
	if now.Sub(game.LastAction) <= lifetime {
		return ""
	}
	if game.Pending {
		delete(p.games, key)
		return "pool challenge expired"
	}
	winner := 1 - game.Turn
	message := fmt.Sprintf("pool game expired; %s wins by timeout.", game.Players[winner].Name)
	return p.finishLocked(key, game, winner, message)
}

func (p *Pool) currentPlayer(game *poolGame) poolPlayer { return game.Players[game.Turn] }

func (p *Pool) playerIndex(game *poolGame, m bot.Message) int {
	identity := poolIdentity(m)
	for i, player := range game.Players {
		if identity == player.Key || strings.EqualFold(m.Nick, player.Name) || strings.EqualFold(m.Account, player.Name) {
			return i
		}
	}
	return -1
}

func (p *Pool) remainingGroup(game *poolGame, player int) int {
	group := game.Groups[player]
	if group == "" {
		return 0
	}
	count := 0
	for ball := range game.Balls {
		if ball != 8 && poolGroup(ball) == group {
			count++
		}
	}
	return count
}

func (p *Pool) remainingNineBall(game *poolGame) string {
	lowest := lowestPoolBall(game)
	if lowest == 0 {
		return "no balls remain"
	}
	return fmt.Sprintf("lowest ball is %d", lowest)
}

func (p *Pool) defaultTarget(game *poolGame) int {
	if game.Mode == poolNineBall {
		return lowestPoolBall(game)
	}
	legal := make([]int, 0, len(game.Balls))
	for ball := range game.Balls {
		if ball == 8 && (game.Groups[game.Turn] == "" || p.remainingGroup(game, game.Turn) != 0) {
			continue
		}
		if ball != 8 && game.Groups[game.Turn] != "" && poolGroup(ball) != game.Groups[game.Turn] {
			continue
		}
		legal = append(legal, ball)
	}
	if len(legal) == 0 {
		return 0
	}
	sort.Ints(legal)
	return legal[0]
}

func (p *Pool) statsResponse(m bot.Message, rawName string) string {
	name := strings.TrimSpace(rawName)
	if name == "" {
		name = m.Nick
	}
	if !validPoolNick(strings.TrimPrefix(name, "@")) {
		return "usage: !poolstats [nick]"
	}
	name = strings.TrimPrefix(name, "@")
	stats := p.readStats(poolStatsKey(name))
	return fmt.Sprintf("%s: 8-ball %dW-%dL; 9-ball %dW-%dL.", name, stats.EightWins, stats.EightLosses, stats.NineWins, stats.NineLosses)
}

func (p *Pool) leaderboardResponse() string {
	if p.db == nil {
		return "pool leaderboard is unavailable"
	}
	keys, err := p.db.List(poolStatsBucket)
	if err != nil || len(keys) == 0 {
		return "no pool scores yet"
	}
	type entry struct {
		name  string
		wins  uint64
		stats poolStats
	}
	entries := make([]entry, 0, len(keys))
	for _, key := range keys {
		stats := p.readStats(key)
		wins := stats.EightWins + stats.NineWins
		if wins > 0 {
			entries = append(entries, entry{name: key, wins: wins, stats: stats})
		}
	}
	if len(entries) == 0 {
		return "no pool scores yet"
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].wins != entries[j].wins {
			return entries[i].wins > entries[j].wins
		}
		return entries[i].name < entries[j].name
	})
	if len(entries) > 5 {
		entries = entries[:5]
	}
	parts := make([]string, len(entries))
	for i, item := range entries {
		parts[i] = fmt.Sprintf("%d. %s %dW-%dL", i+1, item.name, item.wins, item.stats.EightLosses+item.stats.NineLosses)
	}
	return "Pool leaderboard: " + strings.Join(parts, " | ")
}

func (p *Pool) recordResult(mode poolMode, winner, loser poolPlayer) {
	winnerStats := p.readStats(winner.Key)
	loserStats := p.readStats(loser.Key)
	if mode == poolEightBall {
		winnerStats.EightWins++
		loserStats.EightLosses++
	} else {
		winnerStats.NineWins++
		loserStats.NineLosses++
	}
	if p.db != nil {
		_ = p.db.Set(poolStatsBucket, poolStatsKey(winner.Key), winnerStats)
		_ = p.db.Set(poolStatsBucket, poolStatsKey(loser.Key), loserStats)
	}
}

func (p *Pool) readStats(key string) poolStats {
	if p.db == nil {
		return poolStats{}
	}
	raw, err := p.db.Get(poolStatsBucket, poolStatsKey(key))
	if err != nil {
		return poolStats{}
	}
	var stats poolStats
	if storage.Decode(raw, &stats) != nil {
		return poolStats{}
	}
	return stats
}

func newPoolBalls(mode poolMode) map[int]bool {
	balls := make(map[int]bool)
	max := 15
	if mode == poolNineBall {
		max = 9
	}
	for ball := 1; ball <= max; ball++ {
		balls[ball] = true
	}
	return balls
}

func lowestPoolBall(game *poolGame) int {
	lowest := 0
	for ball := range game.Balls {
		if lowest == 0 || ball < lowest {
			lowest = ball
		}
	}
	return lowest
}

func poolGroup(ball int) string {
	if ball >= 1 && ball <= 7 {
		return "solids"
	}
	return "stripes"
}

func oppositePoolGroup(group string) string {
	if group == "solids" {
		return "stripes"
	}
	return "solids"
}

func requestedPoolBall(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, fmt.Errorf("ball required")
	}
	parts := strings.Fields(raw)
	if len(parts) != 1 {
		return 0, fmt.Errorf("one ball at a time")
	}
	ball, err := strconv.Atoi(parts[0])
	if err != nil || ball < 1 || ball > 15 {
		return 0, fmt.Errorf("invalid ball")
	}
	return ball, nil
}

func poolShotSucceeds(percent int) bool {
	if percent >= 100 {
		return true
	}
	if percent <= 0 {
		return false
	}
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(100))
	return err == nil && n.Int64() < int64(percent)
}

func validPoolNick(nick string) bool {
	return nick != "" && len([]rune(nick)) <= 64 && !strings.ContainsAny(nick, " \r\n\t,\x00\x07")
}

func safePoolName(name string) string {
	name = cleanExternalText(name)
	if name == "" {
		return "player"
	}
	return truncateRunes(name, 64)
}

func poolIdentity(m bot.Message) string {
	if account := strings.TrimSpace(m.Account); account != "" && account != "*" {
		return "account:" + strings.ToLower(account)
	}
	return "nick:" + strings.ToLower(strings.TrimSpace(m.Nick))
}

func samePoolIdentity(m bot.Message, player poolPlayer) bool {
	return poolIdentity(m) == player.Key || strings.EqualFold(m.Nick, player.Name) || strings.EqualFold(m.Account, player.Name)
}

func poolTableKey(network, channel string) string {
	return strings.ToLower(strings.TrimSpace(network)) + "\x00" + strings.ToLower(strings.TrimSpace(channel))
}

func poolStatsKey(identity string) string {
	identity = strings.TrimSpace(strings.ToLower(identity))
	identity = strings.TrimPrefix(identity, "account:")
	identity = strings.TrimPrefix(identity, "nick:")
	return identity
}

func pluralPool(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
