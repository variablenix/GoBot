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

const (
	duckHuntBucket        = "duckhunt_scores"
	duckHuntPlayersBucket = "duckhunt_players"
)

type duckHuntConfig struct {
	minimumMessages int
	minimumUsers    int
	minDelay        time.Duration
	maxDelay        time.Duration
	timeout         time.Duration
	flavorEnabled   bool
	flavorMinLead   time.Duration
	befriendEnabled bool
	minReaction     time.Duration
	retryCooldown   time.Duration
	minHP           int
	maxHP           int
	damagePerShot   int
	trustAttempts   int
	goldenChance    float64
	firearmEnabled  bool
	magazineSize    int
	startingAmmo    int
	startingPoints  int64
	magazineCost    int64
	gunCost         int64
}

type duckHuntState struct {
	channel    string
	messages   int
	users      map[string]struct{}
	nextSpawn  time.Time
	spawnedAt  time.Time
	flavorAt   time.Time
	flavorSent bool
	active     bool
	stopped    bool
	hp         int
	maxHP      int
	golden     bool
	trust      map[string]int
}

type duckScore struct {
	Ducks   uint64 `json:"ducks"`
	Friends uint64 `json:"friends"`
	Points  int64  `json:"points"`
}

type duckPlayer struct {
	Initialized    bool  `json:"initialized"`
	HasGun         bool  `json:"has_gun"`
	Ammo           int   `json:"ammo"`
	SpareMagazines int   `json:"spare_magazines"`
	Points         int64 `json:"points"`
}

// DuckHunt is an optional channel activity event. It has no real-money or
// wagering mechanics: a duck appears after enough activity, and players use
// fictional points and arcade gear to interact with it.
type DuckHunt struct {
	mu       sync.Mutex
	db       *storage.DB
	cfg      duckHuntConfig
	states   map[string]*duckHuntState
	attempts map[string]time.Time
}

func (p *DuckHunt) Name() string { return "duckhunt" }
func (p *DuckHunt) Commands() []string {
	return []string{"duckhunt", "dh", "bang", "befriend", "bef", "ducks", "buy", "reload", "ammo"}
}
func (p *DuckHunt) Help() string {
	return "!bang shoots an active duck; !bef befriends it; !ammo, !buy magazine, and !reload manage the fictional arcade gear; !ducks [nick] shows scores; !dh status|start|stop controls activity"
}

func (p *DuckHunt) Init(c bot.PluginConfig, db *storage.DB) error {
	minimumMessages := c.Int("minimum_messages", 25)
	minimumUsers := c.Int("minimum_users", 2)
	minDelay := c.Int("min_delay_seconds", 60)
	maxDelay := c.Int("max_delay_seconds", 300)
	timeout := c.Int("timeout_seconds", 60)
	flavorEnabled := c.Bool("flavor_enabled", true)
	flavorMinLead := c.Int("flavor_min_lead_seconds", 15)
	befriendEnabled := c.Bool("befriend_enabled", true)
	minReaction := c.Int("min_reaction_seconds", 1)
	retryCooldown := c.Int("retry_cooldown_seconds", 7)
	minHP := c.Int("minimum_hp", 1)
	maxHP := c.Int("maximum_hp", 5)
	damagePerShot := c.Int("damage_per_shot", 1)
	trustAttempts := c.Int("befriend_attempts", 3)
	goldenChance := c.Float("golden_duck_probability", 0.15)
	firearmEnabled := c.Bool("firearm_enabled", true)
	magazineSize := c.Int("magazine_size", 6)
	startingAmmo := c.Int("starting_ammo", magazineSize)
	startingPoints := int64(c.Int("starting_points", 25))
	magazineCost := int64(c.Int("magazine_cost", 15))
	gunCost := int64(c.Int("gun_cost", 25))

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
	if flavorMinLead < 1 {
		flavorMinLead = 1
	}
	if minReaction < 0 {
		minReaction = 0
	}
	if retryCooldown < 1 {
		retryCooldown = 1
	}
	if minHP < 1 {
		minHP = 1
	}
	if maxHP < minHP {
		maxHP = minHP
	}
	if damagePerShot < 1 {
		damagePerShot = 1
	}
	if trustAttempts < 1 {
		trustAttempts = 1
	}
	if goldenChance < 0 {
		goldenChance = 0
	}
	if goldenChance > 1 {
		goldenChance = 1
	}
	if magazineSize < 1 {
		magazineSize = 1
	}
	if startingAmmo < 0 {
		startingAmmo = 0
	}
	if startingAmmo > magazineSize {
		startingAmmo = magazineSize
	}
	if startingPoints < 0 {
		startingPoints = 0
	}
	if magazineCost < 0 {
		magazineCost = 0
	}
	if gunCost < 0 {
		gunCost = 0
	}

	p.db = db
	p.cfg = duckHuntConfig{
		minimumMessages: minimumMessages,
		minimumUsers:    minimumUsers,
		minDelay:        time.Duration(minDelay) * time.Second,
		maxDelay:        time.Duration(maxDelay) * time.Second,
		timeout:         time.Duration(timeout) * time.Second,
		flavorEnabled:   flavorEnabled,
		flavorMinLead:   time.Duration(flavorMinLead) * time.Second,
		befriendEnabled: befriendEnabled,
		minReaction:     time.Duration(minReaction) * time.Second,
		retryCooldown:   time.Duration(retryCooldown) * time.Second,
		minHP:           minHP,
		maxHP:           maxHP,
		damagePerShot:   damagePerShot,
		trustAttempts:   trustAttempts,
		goldenChance:    goldenChance,
		firearmEnabled:  firearmEnabled,
		magazineSize:    magazineSize,
		startingAmmo:    startingAmmo,
		startingPoints:  startingPoints,
		magazineCost:    magazineCost,
		gunCost:         gunCost,
	}
	p.states = make(map[string]*duckHuntState)
	p.attempts = make(map[string]time.Time)
	return nil
}

// Start checks channel states in the background. Active duck state is kept in
// memory; scores, fictional points, and player gear are persisted.
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
	case "duckhunt", "dh":
		p.control(b, m, arg)
		return true
	case "bang":
		p.interact(b, m, false)
		return true
	case "befriend", "bef":
		if p.cfg.befriendEnabled {
			p.interact(b, m, true)
		} else {
			b.Send(m.ReplyTarget(), "befriending is disabled")
		}
		return true
	case "ducks":
		p.ducks(b, m, arg)
		return true
	case "buy":
		p.buy(b, m, arg)
		return true
	case "reload":
		p.reload(b, m)
		return true
	case "ammo":
		p.ammo(b, m)
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
	if state.stopped || state.active {
		return
	}
	state.messages++
	state.users[strings.ToLower(nick)] = struct{}{}
	if state.nextSpawn.IsZero() && state.messages >= p.cfg.minimumMessages && len(state.users) >= p.cfg.minimumUsers {
		state.nextSpawn = now.Add(randomDuckDelay(p.cfg))
		state.flavorAt = randomDuckFlavorTime(now, state.nextSpawn, p.cfg)
		state.flavorSent = false
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
		if !b.PluginEnabledForChannel(p.Name(), state.channel) {
			// Drop pending activity when the channel override disables this
			// plugin. New activity can schedule a fresh hunt if it is enabled
			// again later.
			state.active = false
			state.spawnedAt = time.Time{}
			state.nextSpawn = time.Time{}
			state.flavorAt = time.Time{}
			state.flavorSent = false
			state.messages = 0
			state.users = make(map[string]struct{})
			state.hp = 0
			state.maxHP = 0
			state.golden = false
			state.trust = make(map[string]int)
			continue
		}
		if state.stopped {
			continue
		}
		if state.active {
			if now.Sub(state.spawnedAt) < p.cfg.timeout {
				continue
			}
			p.resetCycleLocked(state)
			messages = append(messages, outgoing{
				target: state.channel,
				text:   randomDuckEscape(),
			})
			continue
		}
		if !state.flavorSent && !state.flavorAt.IsZero() && !now.Before(state.flavorAt) && now.Before(state.nextSpawn) {
			state.flavorSent = true
			messages = append(messages, outgoing{
				target: state.channel,
				text:   randomDuckFlavor(),
			})
		}
		if state.nextSpawn.IsZero() || now.Before(state.nextSpawn) {
			continue
		}
		state.active = true
		state.spawnedAt = now
		state.nextSpawn = time.Time{}
		state.flavorAt = time.Time{}
		state.flavorSent = false
		state.messages = 0
		state.users = make(map[string]struct{})
		state.hp = 0
		state.maxHP = 0
		state.golden = false
		state.trust = make(map[string]int)
		state.maxHP = randomDuckHP(p.cfg)
		state.hp = state.maxHP
		state.golden = rand.Float64() < p.cfg.goldenChance
		state.trust = make(map[string]int)
		messages = append(messages, outgoing{
			target: state.channel,
			text:   randomDuckAnnouncementForState(state),
		})
	}
	p.mu.Unlock()

	for _, message := range messages {
		b.Send(message.target, message.text)
	}
}

func (p *DuckHunt) control(b *bot.Bot, m bot.Message, arg string) {
	action := strings.ToLower(strings.TrimSpace(arg))
	key := duckHuntStateKey(b.Config.NetworkName, m.Target)

	switch action {
	case "", "status":
		p.mu.Lock()
		state := p.states[key]
		active, stopped := false, false
		if state != nil {
			active, stopped = state.active, state.stopped
		}
		p.mu.Unlock()
		status := "enabled"
		if stopped {
			status = "stopped"
		}
		duckStatus := "no duck is active"
		if active {
			duckStatus = "a duck is active"
		}
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s is %s in %s; %s.", ircColor(ircGreen, "Duck Hunt"), status, m.Target, duckStatus))
	case "start":
		if !b.IsOwner(m) {
			b.Send(m.ReplyTarget(), ircColor(ircRed, "only a configured owner can start or stop Duck Hunt"))
			return
		}
		p.mu.Lock()
		state := p.stateLocked(b.Config.NetworkName, m.Target)
		state.stopped = false
		state.nextSpawn = time.Time{}
		state.flavorAt = time.Time{}
		state.flavorSent = false
		state.messages = 0
		state.users = make(map[string]struct{})
		state.hp = 0
		state.maxHP = 0
		state.golden = false
		state.trust = make(map[string]int)
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), ircColor(ircGreen, "Duck Hunt enabled in "+m.Target+"."))
	case "stop":
		if !b.IsOwner(m) {
			b.Send(m.ReplyTarget(), ircColor(ircRed, "only a configured owner can start or stop Duck Hunt"))
			return
		}
		p.mu.Lock()
		state := p.stateLocked(b.Config.NetworkName, m.Target)
		state.stopped = true
		state.active = false
		state.spawnedAt = time.Time{}
		state.nextSpawn = time.Time{}
		state.flavorAt = time.Time{}
		state.flavorSent = false
		state.messages = 0
		state.users = make(map[string]struct{})
		state.hp = 0
		state.maxHP = 0
		state.golden = false
		state.trust = make(map[string]int)
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "Duck Hunt stopped in "+m.Target+"."))
	default:
		b.Send(m.ReplyTarget(), ircColor(ircCyan, "usage: !dh [start|stop|status]"))
	}
}

func (p *DuckHunt) interact(b *bot.Bot, m bot.Message, befriend bool) {
	key := duckHuntStateKey(b.Config.NetworkName, m.Target)
	now := time.Now()
	p.mu.Lock()
	state := p.states[key]
	if state == nil || !state.active {
		p.mu.Unlock()
		if befriend {
			b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("%s: there is no duck to befriend in the area...", m.Nick)))
		} else {
			p.noDuckBang(b, m)
		}
		return
	}
	attemptKey := key + "\x00" + strings.ToLower(m.Nick)
	if last, ok := p.attempts[attemptKey]; ok && now.Sub(last) < p.cfg.retryCooldown {
		p.mu.Unlock()
		return
	}
	p.attempts[attemptKey] = now
	player := p.loadPlayerLocked(b.Config.NetworkName, m.Target, m.Nick)
	if !befriend && p.cfg.firearmEnabled {
		if !player.HasGun {
			p.mu.Unlock()
			b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("%s: click... you need a duck gun; use !buy gun", m.Nick)))
			return
		}
		if player.Ammo < 1 {
			p.mu.Unlock()
			b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("%s: *click* you're out of ammo; use !reload", m.Nick)))
			return
		}
		player.Ammo--
		p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
	}

	elapsed := now.Sub(state.spawnedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed < p.cfg.minReaction || (elapsed < 7*time.Second && rand.Float64() > hitChance(elapsed)) {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s %s missed the duck! %s", ircColor(ircRed, "*BANG*"), m.Nick, ircColor(ircCyan, fmt.Sprintf("Try again in %d seconds.", int(p.cfg.retryCooldown/time.Second)))))
		return
	}
	if befriend {
		if state.trust == nil {
			state.trust = make(map[string]int)
		}
		progress := state.trust[strings.ToLower(m.Nick)] + 1
		state.trust[strings.ToLower(m.Nick)] = progress
		if progress < p.cfg.trustAttempts {
			remaining := state.maxHP
			p.mu.Unlock()
			b.Send(m.ReplyTarget(), fmt.Sprintf("%s %s *gentle approach* The duck warms up to you, but isn't tamed yet. It has %d HP left; trust %d/%d.", ircColor(ircCyan, "*BEFRIEND*"), m.Nick, remaining, progress, p.cfg.trustAttempts))
			return
		}
		friends := p.incrementFriends(b.Config.NetworkName, m.Target, m.Nick)
		bonus := int64(5)
		if state.golden {
			bonus *= 2
		}
		player.Points += bonus
		p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
		golden := duckName(state)
		p.resetCycleLocked(state)
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s %s befriended %s! %s [%s points] You have befriended %d duck%s in %s.", ircColor(ircGreen, "*FRIEND*"), m.Nick, golden, ircColor(ircGreen, "QUACK!"), ircColor(ircGreen, fmt.Sprintf("+%d", bonus)), friends, duckPlural(friends), m.Target))
		return
	}
	damage := p.cfg.damagePerShot
	state.hp -= damage
	if state.hp > 0 {
		remaining := state.hp
		name := duckName(state)
		points := shotPoints(state)
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s %s hit %s for %d damage! It has %d HP left. %s", ircColor(ircCyan, "*BANG*"), m.Nick, name, damage, remaining, ircColor(ircGreen, fmt.Sprintf("+%d points", points))))
		return
	}
	kills := p.incrementScore(b.Config.NetworkName, m.Target, m.Nick)
	bonus := int64(10)
	if state.golden {
		bonus *= 2
	}
	player.Points += bonus
	p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
	name := duckName(state)
	p.resetCycleLocked(state)
	p.mu.Unlock()

	b.Send(m.ReplyTarget(), fmt.Sprintf("%s %s hit %s for %d damage! It has 0 HP left. %s [%s] You have killed %d duck%s in %s.", ircColor(ircGreen, "*BANG*"), m.Nick, name, damage, ircColor(ircGreen, "KWAK!"), ircColor(ircGreen, fmt.Sprintf("+%d points", bonus)), kills, duckPlural(kills), m.Target))
}

func (p *DuckHunt) ducks(b *bot.Bot, m bot.Message, arg string) {
	nick := strings.TrimSpace(arg)
	if nick == "" {
		nick = m.Nick
	}
	if strings.ContainsAny(nick, " \r\n\t") || len([]rune(nick)) > 64 {
		b.Send(m.ReplyTarget(), ircColor(ircCyan, "usage: !ducks [nick]"))
		return
	}

	score := p.readScore(b.Config.NetworkName, m.Target, nick)
	p.mu.Lock()
	player := p.loadPlayerLocked(b.Config.NetworkName, m.Target, nick)
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s has killed %d duck%s, befriended %d duck%s, and has %d fictional points in %s.", nick, score.Ducks, duckPlural(score.Ducks), score.Friends, duckPlural(score.Friends), player.Points, m.Target))
}

func (p *DuckHunt) ammo(b *bot.Bot, m bot.Message) {
	p.mu.Lock()
	player := p.loadPlayerLocked(b.Config.NetworkName, m.Target, m.Nick)
	p.mu.Unlock()
	if !p.cfg.firearmEnabled {
		b.Send(m.ReplyTarget(), ircColor(ircCyan, "Duck Hunt arcade gear is disabled."))
		return
	}
	if !player.HasGun {
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: you do not have a duck gun; use !buy gun. You have %d fictional points.", m.Nick, player.Points))
		return
	}
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s: ammo %d/%d, spare magazines %d, fictional points %d.", m.Nick, player.Ammo, p.cfg.magazineSize, player.SpareMagazines, player.Points))
}

func (p *DuckHunt) buy(b *bot.Bot, m bot.Message, arg string) {
	item := strings.ToLower(strings.TrimSpace(arg))
	if item != "gun" && item != "duckgun" && item != "magazine" && item != "mag" && item != "ammo" {
		b.Send(m.ReplyTarget(), ircColor(ircCyan, "usage: !buy gun|magazine (fictional arcade gear)"))
		return
	}
	if !p.cfg.firearmEnabled {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "Duck Hunt arcade gear is disabled."))
		return
	}
	p.mu.Lock()
	player := p.loadPlayerLocked(b.Config.NetworkName, m.Target, m.Nick)
	if item == "gun" || item == "duckgun" {
		if player.HasGun {
			p.mu.Unlock()
			b.Send(m.ReplyTarget(), fmt.Sprintf("%s: you already have a duck gun.", m.Nick))
			return
		}
		if player.Points < p.cfg.gunCost {
			points := player.Points
			p.mu.Unlock()
			b.Send(m.ReplyTarget(), fmt.Sprintf("%s: the duck gun costs %d fictional points; you have %d.", m.Nick, p.cfg.gunCost, points))
			return
		}
		player.Points -= p.cfg.gunCost
		player.HasGun = true
		player.Ammo = p.cfg.startingAmmo
		p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: duck gun acquired for %d fictional points; ammo %d/%d. Use !bang during a hunt.", m.Nick, p.cfg.gunCost, player.Ammo, p.cfg.magazineSize))
		return
	}
	if !player.HasGun {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: buy a duck gun first with !buy gun.", m.Nick))
		return
	}
	if player.Points < p.cfg.magazineCost {
		points := player.Points
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: a spare magazine costs %d fictional points; you have %d.", m.Nick, p.cfg.magazineCost, points))
		return
	}
	player.Points -= p.cfg.magazineCost
	player.SpareMagazines++
	p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s: spare magazine purchased for %d fictional points. Spare magazines: %d.", m.Nick, p.cfg.magazineCost, player.SpareMagazines))
}

func (p *DuckHunt) reload(b *bot.Bot, m bot.Message) {
	if !p.cfg.firearmEnabled {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "Duck Hunt arcade gear is disabled."))
		return
	}
	p.mu.Lock()
	player := p.loadPlayerLocked(b.Config.NetworkName, m.Target, m.Nick)
	if !player.HasGun {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: you need a duck gun first; use !buy gun.", m.Nick))
		return
	}
	if player.Ammo >= p.cfg.magazineSize {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: your magazine is already full at %d/%d.", m.Nick, player.Ammo, p.cfg.magazineSize))
		return
	}
	if player.SpareMagazines < 1 {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: no spare magazines. Use !buy magazine.", m.Nick))
		return
	}
	player.SpareMagazines--
	player.Ammo = p.cfg.magazineSize
	p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s: *click* new magazine loaded! Ammo: %d/%d; spare magazines: %d.", m.Nick, player.Ammo, p.cfg.magazineSize, player.SpareMagazines))
}

func (p *DuckHunt) noDuckBang(b *bot.Bot, m bot.Message) {
	if !p.cfg.firearmEnabled {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("%s: BANG... what did you shoot at? There is no duck in the area.", m.Nick)))
		return
	}
	p.mu.Lock()
	player := p.loadPlayerLocked(b.Config.NetworkName, m.Target, m.Nick)
	hadGun := player.HasGun
	if hadGun {
		player.HasGun = false
		player.Ammo = 0
		player.SpareMagazines = 0
		p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
	}
	p.mu.Unlock()
	if !hadGun {
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s BANG... what did you shoot at? There is no duck in the area. %s", m.Nick, ircColor(ircCyan, "You need a duck gun first; use !buy gun.")))
		return
	}
	messages := []string{
		"BANG... what did you shoot at? There is no duck in the area... [GUN CONFISCATED]",
		"That was a very brave shot at absolutely nothing. [GUN CONFISCATED]",
		"The reeds file a complaint. No duck spotted. [GUN CONFISCATED]",
	}
	b.Send(m.ReplyTarget(), ircColor(ircRed, messages[rand.Intn(len(messages))]))
}

func (p *DuckHunt) incrementScore(network, channel, nick string) uint64 {
	score := p.readScore(network, channel, nick)
	score.Ducks++
	if p.db != nil {
		_ = p.db.Set(duckHuntBucket, duckHuntScoreKey(network, channel, nick), score)
	}
	return score.Ducks
}

func (p *DuckHunt) incrementFriends(network, channel, nick string) uint64 {
	score := p.readScore(network, channel, nick)
	score.Friends++
	if p.db != nil {
		_ = p.db.Set(duckHuntBucket, duckHuntScoreKey(network, channel, nick), score)
	}
	return score.Friends
}

func (p *DuckHunt) readScore(network, channel, nick string) duckScore {
	if p.db == nil {
		return duckScore{}
	}
	raw, err := p.db.Get(duckHuntBucket, duckHuntScoreKey(network, channel, nick))
	if err != nil {
		return duckScore{}
	}
	var saved duckScore
	if storage.Decode(raw, &saved) != nil {
		return duckScore{}
	}
	return saved
}

func (p *DuckHunt) loadPlayerLocked(network, channel, nick string) duckPlayer {
	player := duckPlayer{
		Initialized: true,
		HasGun:      false,
		Ammo:        0,
		Points:      p.cfg.startingPoints,
	}
	if p.db == nil {
		return player
	}
	raw, err := p.db.Get(duckHuntPlayersBucket, duckHuntPlayerKey(network, channel, nick))
	if err == nil {
		var saved duckPlayer
		if storage.Decode(raw, &saved) == nil {
			player = saved
		}
	}
	if !player.Initialized {
		player.Initialized = true
		if player.Points == 0 {
			player.Points = p.cfg.startingPoints
		}
	}
	if !p.cfg.firearmEnabled {
		player.HasGun = false
		player.Ammo = 0
		player.SpareMagazines = 0
	}
	if player.Points < 0 {
		player.Points = 0
	}
	if player.Ammo < 0 {
		player.Ammo = 0
	}
	if player.Ammo > p.cfg.magazineSize {
		player.Ammo = p.cfg.magazineSize
	}
	if player.SpareMagazines < 0 {
		player.SpareMagazines = 0
	}
	return player
}

func (p *DuckHunt) savePlayerLocked(network, channel, nick string, player duckPlayer) {
	if p.db != nil {
		_ = p.db.Set(duckHuntPlayersBucket, duckHuntPlayerKey(network, channel, nick), player)
	}
}

func (p *DuckHunt) stateLocked(network, channel string) *duckHuntState {
	key := duckHuntStateKey(network, channel)
	state := p.states[key]
	if state == nil {
		state = &duckHuntState{channel: channel, users: make(map[string]struct{})}
		p.states[key] = state
	}
	return state
}

func (p *DuckHunt) resetCycleLocked(state *duckHuntState) {
	state.active = false
	state.spawnedAt = time.Time{}
	state.nextSpawn = time.Time{}
	state.flavorAt = time.Time{}
	state.flavorSent = false
	state.messages = 0
	state.users = make(map[string]struct{})
	state.hp = 0
	state.maxHP = 0
	state.golden = false
	state.trust = make(map[string]int)
}

func hitChance(elapsed time.Duration) float64 {
	if elapsed < time.Second {
		return 0
	}
	if elapsed < 7*time.Second {
		return 0.60 + rand.Float64()*0.15
	}
	return 1
}

func randomDuckDelay(c duckHuntConfig) time.Duration {
	if c.maxDelay <= c.minDelay {
		return c.minDelay
	}
	span := int((c.maxDelay - c.minDelay) / time.Second)
	return c.minDelay + time.Duration(rand.Intn(span+1))*time.Second
}

func randomDuckHP(c duckHuntConfig) int {
	if c.maxHP <= c.minHP {
		return c.minHP
	}
	return c.minHP + rand.Intn(c.maxHP-c.minHP+1)
}

func shotPoints(state *duckHuntState) int64 {
	if state != nil && state.golden {
		return 20
	}
	return 10
}

func duckName(state *duckHuntState) string {
	if state != nil && state.golden {
		return ircColor(ircYellow, "the GOLDEN DUCK")
	}
	return ircColor(ircCyan, "the duck")
}

func randomDuckFlavorTime(now, nextSpawn time.Time, c duckHuntConfig) time.Time {
	if !c.flavorEnabled {
		return time.Time{}
	}
	remaining := nextSpawn.Sub(now)
	minLead := c.flavorMinLead
	if remaining <= minLead*2 {
		return time.Time{}
	}
	maxOffset := remaining - minLead
	span := maxOffset - minLead
	return now.Add(minLead + time.Duration(rand.Int63n(int64(span)+1)))
}

func randomDuckFlavor() string {
	trails := []string{
		"· ° · ° · °",
		"·  °   ·  °   ·",
		"° · ° · ° · °",
		"· · ° · · °",
	}
	flavors := []string{
		`\_o< QUACK!`,
		`\_O< QUACK QUACK!`,
		`\_o< FLAP FLAP!`,
		`\_ö< *flap flap*`,
	}
	return fmt.Sprintf("%s %s %s", ircColor(ircGreen, "[Duck Hunt]"), ircColor(ircYellow, trails[rand.Intn(len(trails))]), ircColor(ircCyan, flavors[rand.Intn(len(flavors))]))
}

func randomDuckAnnouncement() string {
	return randomDuckAnnouncementForState(&duckHuntState{hp: 1, maxHP: 1})
}

func randomDuckAnnouncementForState(state *duckHuntState) string {
	ducks := []string{`\_o<`, `\_O<`, `\_0<`, `\_ö<`}
	noises := []string{"quack!", "QUACK!", "QUACK! QUACK!", "QUACK! flap flap!"}
	duck := ducks[rand.Intn(len(ducks))]
	noise := noises[rand.Intn(len(noises))]
	hp := 1
	if state != nil && state.maxHP > 0 {
		hp = state.maxHP
	}
	name := "DUCK"
	color := ircCyan
	if state != nil && state.golden {
		name = "GOLDEN DUCK"
		color = ircYellow
	}
	return fmt.Sprintf("%s %s %s %s HP: %d | Type %s to shoot or %s to befriend!", ircColor(ircGreen, "[Duck Hunt]"), ircColor(ircYellow, duck), ircColor(color, name), ircColor(ircCyan, noise), hp, ircColor(ircBold, "!bang"), ircColor(ircBold, "!bef"))
}

func randomDuckEscape() string {
	actions := []string{
		`The duck escapes into the sky! °°...`,
		`The duck flaps away, living another day. °°°...`,
		`The duck waddles behind a bush and gets away! \_o<`,
		`\_o< *ZOOM* The speedy duck vanishes in a flash!`,
		`The duck takes off in a hurry. QUACK! °°...`,
		`The duck slips away through the reeds. Better luck next time!`,
		`The duck spreads its wings and soars away. \_O<`,
		`The duck makes a break for it—waddle waddle waddle!`,
		`The ninja duck drops a smoke bomb and vanishes! *poof*`,
		`The duck moonwalks into the reeds and disappears.`,
		`The duck performs an evasive barrel roll and escapes!`,
	}
	return ircColor(ircYellow, "[Duck Hunt] "+actions[rand.Intn(len(actions))])
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

func duckHuntPlayerKey(network, channel, nick string) string {
	return duckHuntScoreKey(network, channel, nick)
}
