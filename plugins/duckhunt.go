package plugins

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
	"unicode"

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
	jamChance       float64
	magazineSize    int
	startingAmmo    int
	startingPoints  int64
	magazineCost    int64
	gunCost         int64
	breadCost       int64
	xpPerHit        int64
	xpPerKill       int64
	xpPerBefriend   int64
	flockMin        int
	flockMax        int
}

type duckHuntState struct {
	channel          string
	messages         int
	users            map[string]struct{}
	nextSpawn        time.Time
	spawnedAt        time.Time
	flavorAt         time.Time
	flavorSent       bool
	active           bool
	stopped          bool
	hp               int
	maxHP            int
	golden           bool
	trust            map[string]int
	flockRemaining   int
	spawnMultiplier  float64
	spawnBoostUntil  time.Time
	goldenBoost      float64
	goldenBoostUntil time.Time
	nextFlock        bool
}

type duckScore struct {
	Ducks   uint64 `json:"ducks"`
	Friends uint64 `json:"friends"`
}

type duckPlayer struct {
	Initialized    bool   `json:"initialized"`
	HasGun         bool   `json:"has_gun"`
	Weapon         string `json:"weapon"`
	Ammo           int    `json:"ammo"`
	SpareMagazines int    `json:"spare_magazines"`
	Points         int64  `json:"points"`
	XP             int64  `json:"xp"`
	Shots          uint64 `json:"shots"`
	Hits           uint64 `json:"hits"`
	Bread          uint64 `json:"bread"`
	LuckyFeathers  uint64 `json:"lucky_feathers"`
	DuckWhistles   uint64 `json:"duck_whistles"`
	Decoys         uint64 `json:"decoys"`
	GunBrushes     uint64 `json:"gun_brushes"`
	GoldenSeeds    uint64 `json:"golden_seeds"`
	PondMaps       uint64 `json:"pond_maps"`
	FocusedShot    bool   `json:"focused_shot"`
	GoldenBounty   bool   `json:"golden_bounty"`
	Jammed         bool   `json:"jammed"`
}

type duckWeapon struct {
	Key          string
	Name         string
	Cost         int64
	Damage       int
	MagazineSize int
}

type duckConsumable struct {
	ID          string
	Key         string
	Name        string
	Cost        int64
	Description string
}

// DuckHunt is an optional channel activity event. It has no real-money or
// wagering mechanics: a duck appears after enough activity, and players use
// local points and arcade gear to interact with it.
type DuckHunt struct {
	mu         sync.Mutex
	db         *storage.DB
	cfg        duckHuntConfig
	states     map[string]*duckHuntState
	attempts   map[string]time.Time
	tickerDone chan struct{}
}

func (p *DuckHunt) Name() string { return "duckhunt" }
func (p *DuckHunt) Commands() []string {
	return []string{"duckhunt", "dh", "ducklaunch", "bang", "befriend", "bef", "ducks", "duckstats", "level", "xp", "profile", "shop", "store", "buy", "use", "reload", "unjam", "ammo"}
}
func (p *DuckHunt) Help() string {
	return "!bang shoots an active duck; !bef befriends it; !ducklaunch flock starts a random flock; !shop lists gear and items; !buy <item> (weapons, magazine/mag/ammo, or items 1-7); !use <1-7|item> activates a Duck Hunt item; !ammo, !reload, and !unjam manage gear; !ducks [nick] shows scores; !duckstats [nick] shows a detailed title, accuracy, gear, and inventory profile; !level [nick] shows XP and level; !dh status|start|stop controls activity"
}

// shopWeapons keeps the arsenal deliberately arcade-like. The names are game
// items, not real-world firearm recommendations or instructions.
func (p *DuckHunt) shopWeapons() []duckWeapon {
	baseDamage := p.cfg.damagePerShot
	return []duckWeapon{
		{
			Key:          "peashooter",
			Name:         "Peashooter",
			Cost:         p.cfg.gunCost,
			Damage:       baseDamage,
			MagazineSize: p.cfg.magazineSize,
		},
		{
			Key:          "quacker",
			Name:         "Quacker Blaster",
			Cost:         p.cfg.gunCost + 15,
			Damage:       baseDamage,
			MagazineSize: p.cfg.magazineSize + 2,
		},
		{
			Key:          "golden",
			Name:         "Golden Wing",
			Cost:         p.cfg.gunCost + 50,
			Damage:       baseDamage + 1,
			MagazineSize: maxInt(2, p.cfg.magazineSize-2),
		},
	}
}

func (p *DuckHunt) consumables() []duckConsumable {
	return []duckConsumable{
		{ID: "1", Key: "lucky_feather", Name: "Lucky Feather", Cost: 12, Description: "boosts the next duck's golden chance"},
		{ID: "2", Key: "duck_whistle", Name: "Duck Whistle", Cost: 18, Description: "calls the next automatic duck sooner"},
		{ID: "3", Key: "decoy", Name: "Decoy Duck", Cost: 10, Description: "extends an active hunt by 20 seconds"},
		{ID: "4", Key: "gun_brush", Name: "Gun Brush", Cost: 12, Description: "guarantees your next armed shot"},
		{ID: "5", Key: "bread", Name: "Bread", Cost: p.cfg.breadCost, Description: "accelerates automatic spawns for 20 minutes"},
		{ID: "6", Key: "golden_seed", Name: "Golden Seed", Cost: 20, Description: "adds a bonus to your next completed kill"},
		{ID: "7", Key: "pond_map", Name: "Pond Map", Cost: 15, Description: "turns the next automatic visit into a flock"},
	}
}

func (p *DuckHunt) findConsumable(arg string) (duckConsumable, bool) {
	item := strings.ToLower(strings.TrimSpace(arg))
	item = strings.Join(strings.Fields(item), "-")
	aliases := map[string]string{
		"1": "lucky_feather", "feather": "lucky_feather", "luckyfeather": "lucky_feather", "lucky-feather": "lucky_feather",
		"2": "duck_whistle", "whistle": "duck_whistle", "duckwhistle": "duck_whistle", "duck-whistle": "duck_whistle",
		"3": "decoy", "decoyduck": "decoy", "decoy-duck": "decoy",
		"4": "gun_brush", "brush": "gun_brush", "gunbrush": "gun_brush", "gun-brush": "gun_brush",
		"5": "bread",
		"6": "golden_seed", "seed": "golden_seed", "goldenseed": "golden_seed", "golden-seed": "golden_seed",
		"7": "pond_map", "map": "pond_map", "pondmap": "pond_map", "pond-map": "pond_map",
	}
	if canonical, ok := aliases[item]; ok {
		item = canonical
	}
	for _, consumable := range p.consumables() {
		if consumable.Key == item {
			return consumable, true
		}
	}
	return duckConsumable{}, false
}

func duckItemCount(player duckPlayer, key string) uint64 {
	switch key {
	case "lucky_feather":
		return player.LuckyFeathers
	case "duck_whistle":
		return player.DuckWhistles
	case "decoy":
		return player.Decoys
	case "gun_brush":
		return player.GunBrushes
	case "bread":
		return player.Bread
	case "golden_seed":
		return player.GoldenSeeds
	case "pond_map":
		return player.PondMaps
	default:
		return 0
	}
}

func setDuckItemCount(player *duckPlayer, key string, count uint64) {
	switch key {
	case "lucky_feather":
		player.LuckyFeathers = count
	case "duck_whistle":
		player.DuckWhistles = count
	case "decoy":
		player.Decoys = count
	case "gun_brush":
		player.GunBrushes = count
	case "bread":
		player.Bread = count
	case "golden_seed":
		player.GoldenSeeds = count
	case "pond_map":
		player.PondMaps = count
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (p *DuckHunt) weaponForPlayer(player duckPlayer) duckWeapon {
	weapons := p.shopWeapons()
	key := strings.ToLower(strings.TrimSpace(player.Weapon))
	for _, weapon := range weapons {
		if weapon.Key == key {
			return weapon
		}
	}
	return weapons[0]
}

func (p *DuckHunt) findWeapon(key string) (duckWeapon, bool) {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "gun" || key == "duckgun" || key == "pea" {
		key = "peashooter"
	}
	for _, weapon := range p.shopWeapons() {
		if weapon.Key == key {
			return weapon, true
		}
	}
	return duckWeapon{}, false
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
	jamChance := c.Float("weapon_jam_probability", 0.03)
	magazineSize := c.Int("magazine_size", 6)
	startingAmmo := c.Int("starting_ammo", magazineSize)
	startingPoints := int64(c.Int("starting_points", 25))
	magazineCost := int64(c.Int("magazine_cost", 15))
	gunCost := int64(c.Int("gun_cost", 25))
	breadCost := int64(c.Int("bread_cost", 8))
	xpPerHit := int64(c.Int("xp_per_hit", 5))
	xpPerKill := int64(c.Int("xp_per_kill", 25))
	xpPerBefriend := int64(c.Int("xp_per_befriend", 10))
	flockMin := c.Int("flock_min", 2)
	flockMax := c.Int("flock_max", 4)

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
	if jamChance < 0 {
		jamChance = 0
	}
	if jamChance > 0.25 {
		jamChance = 0.25
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
	if breadCost < 0 {
		breadCost = 0
	}
	if xpPerHit < 0 {
		xpPerHit = 0
	}
	if xpPerKill < 0 {
		xpPerKill = 0
	}
	if xpPerBefriend < 0 {
		xpPerBefriend = 0
	}
	if flockMin < 2 {
		flockMin = 2
	}
	if flockMax < flockMin {
		flockMax = flockMin
	}
	if flockMax > 12 {
		flockMax = 12
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
		jamChance:       jamChance,
		magazineSize:    magazineSize,
		startingAmmo:    startingAmmo,
		startingPoints:  startingPoints,
		magazineCost:    magazineCost,
		gunCost:         gunCost,
		breadCost:       breadCost,
		xpPerHit:        xpPerHit,
		xpPerKill:       xpPerKill,
		xpPerBefriend:   xpPerBefriend,
		flockMin:        flockMin,
		flockMax:        flockMax,
	}
	p.states = make(map[string]*duckHuntState)
	p.attempts = make(map[string]time.Time)
	return nil
}

// Start checks channel states in the background. Active duck state is kept in
// memory; scores, points, and player gear are persisted.
func (p *DuckHunt) Start(b *bot.Bot) {
	p.mu.Lock()
	if p.tickerDone != nil {
		p.mu.Unlock()
		return
	}
	done := make(chan struct{})
	p.tickerDone = done
	p.mu.Unlock()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.tick(b)
			case <-done:
				return
			}
		}
	}()
}

// Stop terminates the activity ticker. State is rebuilt by Init if the plugin
// is enabled again, so a disabled Duck Hunt cannot emit stale activity.
func (p *DuckHunt) Stop(_ *bot.Bot) {
	p.mu.Lock()
	if p.tickerDone != nil {
		close(p.tickerDone)
		p.tickerDone = nil
	}
	p.mu.Unlock()
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
	case "ducklaunch":
		p.launch(b, m, arg)
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
	case "duckstats":
		p.duckStats(b, m, arg)
		return true
	case "level", "xp", "profile":
		p.profile(b, m, arg)
		return true
	case "shop", "store":
		p.shop(b, m)
		return true
	case "buy":
		p.buy(b, m, arg)
		return true
	case "use":
		p.use(b, m, arg)
		return true
	case "reload":
		p.reload(b, m)
		return true
	case "unjam":
		p.unjam(b, m)
		return true
	case "ammo":
		p.ammo(b, m)
		return true
	default:
		return false
	}
}

func (p *DuckHunt) launch(b *bot.Bot, m bot.Message, arg string) {
	if !strings.EqualFold(strings.TrimSpace(arg), "flock") {
		b.Send(m.ReplyTarget(), ircColor(ircCyan, "usage: !ducklaunch flock — launch a random flock of ducks"))
		return
	}
	now := time.Now()
	p.mu.Lock()
	state := p.stateLocked(b.Config.NetworkName, m.Target)
	if state.stopped {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "Duck Hunt is stopped in this channel; an owner must use !dh start first"))
		return
	}
	if state.active {
		remaining := state.flockRemaining
		if remaining < 1 {
			remaining = 1
		}
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("there is already an active duck hunt with %d duck%s remaining", remaining, duckPlural(uint64(remaining)))))
		return
	}
	flockSize := p.randomFlockSize()
	state.active = true
	state.spawnedAt = now
	state.nextSpawn = time.Time{}
	state.flavorAt = time.Time{}
	state.flavorSent = false
	state.messages = 0
	state.users = make(map[string]struct{})
	state.maxHP = randomDuckHP(p.cfg)
	state.hp = state.maxHP
	state.golden = rand.Float64() < p.cfg.goldenChance
	state.trust = make(map[string]int)
	state.flockRemaining = flockSize
	announcement := randomDuckAnnouncementForState(state)
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), announcement)
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
		state.nextSpawn = now.Add(p.duckSpawnDelayLocked(state, now))
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
		if !state.spawnBoostUntil.IsZero() && !now.Before(state.spawnBoostUntil) {
			state.spawnMultiplier = 0
			state.spawnBoostUntil = time.Time{}
		}
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
			state.flockRemaining = 0
			state.spawnMultiplier = 0
			state.spawnBoostUntil = time.Time{}
			state.goldenBoost = 0
			state.goldenBoostUntil = time.Time{}
			state.nextFlock = false
			continue
		}
		if state.stopped {
			continue
		}
		if state.active {
			if now.Sub(state.spawnedAt) < p.cfg.timeout {
				continue
			}
			escape := randomDuckEscapeForState(state)
			p.resetCycleLocked(state)
			messages = append(messages, outgoing{
				target: state.channel,
				text:   escape,
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
		state.golden = rand.Float64() < p.goldenChanceLocked(state, now)
		if !state.goldenBoostUntil.IsZero() && now.Before(state.goldenBoostUntil) {
			state.goldenBoost = 0
			state.goldenBoostUntil = time.Time{}
		}
		state.trust = make(map[string]int)
		state.flockRemaining = 1
		if state.nextFlock {
			state.flockRemaining = p.randomFlockSize()
			state.nextFlock = false
		}
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
			p.mu.Lock()
			if state := p.states[key]; state != nil && state.flockRemaining > 1 {
				duckStatus = fmt.Sprintf("a flock is active (%d ducks remaining)", state.flockRemaining)
			}
			p.mu.Unlock()
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
		state.flockRemaining = 0
		state.spawnMultiplier = 0
		state.spawnBoostUntil = time.Time{}
		state.goldenBoost = 0
		state.goldenBoostUntil = time.Time{}
		state.nextFlock = false
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
		state.flockRemaining = 0
		state.spawnMultiplier = 0
		state.spawnBoostUntil = time.Time{}
		state.goldenBoost = 0
		state.goldenBoostUntil = time.Time{}
		state.nextFlock = false
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
	focusedShot := !befriend && player.FocusedShot
	if !befriend && p.cfg.firearmEnabled {
		if !player.HasGun {
			p.mu.Unlock()
			b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("%s: click... visit !shop and buy some arcade gear first", m.Nick)))
			return
		}
		if player.Jammed {
			weaponName := p.weaponForPlayer(player).Name
			p.mu.Unlock()
			b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("%s: *click* your %s is jammed; use !unjam before firing again", m.Nick, weaponName)))
			return
		}
		if player.Ammo < 1 {
			p.mu.Unlock()
			b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("%s: *click* you're out of ammo; use !reload if you have a spare magazine, or !buy magazine for more ammo", m.Nick)))
			return
		}
		player.Ammo--
	}
	if !befriend {
		player.Shots++
		if p.cfg.firearmEnabled && !focusedShot && rand.Float64() < p.cfg.jamChance {
			player.Jammed = true
			p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
			ammoHint := duckAmmoHint(player)
			weaponName := p.weaponForPlayer(player).Name
			p.mu.Unlock()
			b.Send(m.ReplyTarget(), fmt.Sprintf("%s %s's %s jammed! No damage dealt. Use %s to clear it.%s", ircColor(ircRed, "*KRRK*"), m.Nick, weaponName, ircColor(ircBold, "!unjam"), ammoHint))
			return
		}
		p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
	}
	if focusedShot {
		player.FocusedShot = false
		p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
	}

	elapsed := now.Sub(state.spawnedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	if !focusedShot && (elapsed < p.cfg.minReaction || (elapsed < 7*time.Second && rand.Float64() > hitChance(elapsed))) {
		ammoHint := duckAmmoHint(player)
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s %s missed the duck! %s%s", ircColor(ircRed, "*BANG*"), m.Nick, ircColor(ircCyan, fmt.Sprintf("Try again in %d seconds.", int(p.cfg.retryCooldown/time.Second))), ammoHint))
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
		xp := xpReward(p.cfg.xpPerBefriend, state.golden)
		player.Points += bonus
		player.XP += xp
		p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
		golden := duckName(state)
		p.resetCycleLocked(state)
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s %s befriended %s! %s [%s points, %s XP] You have befriended %d duck%s in %s.", ircColor(ircGreen, "*FRIEND*"), m.Nick, golden, ircColor(ircGreen, "QUACK!"), ircColor(ircGreen, fmt.Sprintf("+%d", bonus)), ircColor(ircGreen, fmt.Sprintf("+%d", xp)), friends, duckPlural(friends), m.Target))
		return
	}
	weapon := p.weaponForPlayer(player)
	damage := weapon.Damage
	player.Hits++
	state.hp -= damage
	if state.hp > 0 {
		remaining := state.hp
		name := duckName(state)
		points := shotPoints(state)
		xp := xpReward(p.cfg.xpPerHit, state.golden)
		player.Points += points
		player.XP += xp
		p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
		ammoHint := duckAmmoHint(player)
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s %s hit %s for %d damage! It has %d HP left. %s%s", ircColor(ircCyan, "*BANG*"), m.Nick, name, damage, remaining, ircColor(ircGreen, fmt.Sprintf("+%d points, +%d XP", points, xp)), ammoHint))
		return
	}
	kills := p.incrementScore(b.Config.NetworkName, m.Target, m.Nick)
	bonus := int64(10)
	if state.golden {
		bonus *= 2
	}
	xp := xpReward(p.cfg.xpPerKill, state.golden)
	if player.GoldenBounty {
		bonus += 25
		xp += 25
		player.GoldenBounty = false
	}
	player.Points += bonus
	player.XP += xp
	p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
	ammoHint := duckAmmoHint(player)
	if state.flockRemaining > 1 {
		state.flockRemaining--
		state.maxHP = randomDuckHP(p.cfg)
		state.hp = state.maxHP
		state.trust = make(map[string]int)
		remainingFlock := state.flockRemaining
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s %s hit %s for %d damage! It has 0 HP left. %s %d duck%s still in the flock!%s", ircColor(ircGreen, "*BANG*"), m.Nick, ircColor(ircCyan, "a duck in the flock"), damage, ircColor(ircGreen, fmt.Sprintf("+%d points, +%d XP", bonus, xp)), remainingFlock, duckPlural(uint64(remainingFlock)), ammoHint))
		return
	}
	name := duckName(state)
	p.resetCycleLocked(state)
	p.mu.Unlock()

	b.Send(m.ReplyTarget(), fmt.Sprintf("%s %s hit %s for %d damage! It has 0 HP left. %s [%s points, %s XP] You have killed %d duck%s in %s.%s", ircColor(ircGreen, "*BANG*"), m.Nick, name, damage, ircColor(ircGreen, "KWAK!"), ircColor(ircGreen, fmt.Sprintf("+%d", bonus)), ircColor(ircGreen, fmt.Sprintf("+%d", xp)), kills, duckPlural(kills), m.Target, ammoHint))
}

func (p *DuckHunt) ducks(b *bot.Bot, m bot.Message, arg string) {
	nick := strings.TrimSpace(arg)
	if nick == "" {
		nick = m.Nick
	}
	if !validDuckProfileNick(nick) {
		b.Send(m.ReplyTarget(), ircColor(ircCyan, "usage: !ducks [nick]"))
		return
	}

	score := p.readScore(b.Config.NetworkName, m.Target, nick)
	p.mu.Lock()
	player := p.loadPlayerLocked(b.Config.NetworkName, m.Target, nick)
	p.mu.Unlock()
	level := duckLevel(player.XP)
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s: %d killed, %d befriended, %d points, level %d (%d XP; %d to level %d) in %s.", nick, score.Ducks, score.Friends, player.Points, level, player.XP, xpToNextLevel(player.XP), level+1, m.Target))
}

func (p *DuckHunt) profile(b *bot.Bot, m bot.Message, arg string) {
	nick := strings.TrimSpace(arg)
	if nick == "" {
		nick = m.Nick
	}
	if !validDuckProfileNick(nick) {
		b.Send(m.ReplyTarget(), ircColor(ircCyan, "usage: !level [nick]"))
		return
	}
	score := p.readScore(b.Config.NetworkName, m.Target, nick)
	p.mu.Lock()
	player := p.loadPlayerLocked(b.Config.NetworkName, m.Target, nick)
	p.mu.Unlock()
	level := duckLevel(player.XP)
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s is level %d %s with %d XP (%d to level %d), %d points, %d killed, and %d befriended in %s.", nick, level, duckTitle(level), player.XP, xpToNextLevel(player.XP), level+1, player.Points, score.Ducks, score.Friends, m.Target))
}

func (p *DuckHunt) duckStats(b *bot.Bot, m bot.Message, arg string) {
	nick := strings.TrimSpace(arg)
	if nick == "" {
		nick = m.Nick
	}
	if !validDuckProfileNick(nick) {
		b.Send(m.ReplyTarget(), ircColor(ircCyan, "usage: !duckstats [nick]"))
		return
	}
	score := p.readScore(b.Config.NetworkName, m.Target, nick)
	p.mu.Lock()
	player := p.loadPlayerLocked(b.Config.NetworkName, m.Target, nick)
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), p.formatDuckStats(nick, player, score))
}

func validDuckProfileNick(nick string) bool {
	if nick == "" || len([]rune(nick)) > 64 {
		return false
	}
	for _, r := range nick {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (p *DuckHunt) formatDuckStats(nick string, player duckPlayer, score duckScore) string {
	level := duckLevel(player.XP)
	title := duckTitle(level)
	nextRankLevel, nextTitle, hasNextRank := nextDuckRank(level)
	next := "top title reached"
	if hasNextRank {
		nextXP := xpForLevel(nextRankLevel) - player.XP
		if nextXP < 0 {
			nextXP = 0
		}
		next = fmt.Sprintf("Need %d XP for %s", nextXP, nextTitle)
	}
	accuracy := duckPercent(player.Hits, player.Shots, 0)
	hitRate := duckPercent(score.Ducks, player.Hits, 1)
	armed := "Unarmed"
	ammo := "0/0"
	jamChance := "0%"
	if player.HasGun {
		armed = "Armed"
		if player.Jammed {
			armed = "Armed (Jammed)"
		}
		weapon := p.weaponForPlayer(player)
		ammo = fmt.Sprintf("%d/%d", player.Ammo, weapon.MagazineSize)
		jamChance = fmt.Sprintf("%.0f%%", p.cfg.jamChance*100)
	}
	items := duckItems(player)
	effects := duckEffects(player)
	return fmt.Sprintf("%s: Lv%d %s | %d XP (%s) | %d shots | %d killed | %d befriended | %s accuracy | %s hit rate | %s | %s ammo | %d spare magazine%s | %s jam chance | Items: %s | Effects: %s", nick, level, title, player.XP, next, player.Shots, score.Ducks, score.Friends, accuracy, hitRate, armed, ammo, player.SpareMagazines, duckPlural(uint64(player.SpareMagazines)), jamChance, items, effects)
}

func duckEffects(player duckPlayer) string {
	effects := make([]string, 0, 2)
	if player.FocusedShot {
		effects = append(effects, "guaranteed shot ready")
	}
	if player.GoldenBounty {
		effects = append(effects, "golden bounty ready")
	}
	if len(effects) == 0 {
		return "none"
	}
	return strings.Join(effects, ", ")
}

func duckItems(player duckPlayer) string {
	items := make([]string, 0, 8)
	for _, item := range []struct {
		name  string
		count uint64
	}{
		{name: "Lucky Feather", count: player.LuckyFeathers},
		{name: "Duck Whistle", count: player.DuckWhistles},
		{name: "Decoy Duck", count: player.Decoys},
		{name: "Gun Brush", count: player.GunBrushes},
		{name: "Bread", count: player.Bread},
		{name: "Golden Seed", count: player.GoldenSeeds},
		{name: "Pond Map", count: player.PondMaps},
	} {
		if item.count > 0 {
			items = append(items, fmt.Sprintf("%s x%d", item.name, item.count))
		}
	}
	if player.SpareMagazines > 0 {
		items = append(items, fmt.Sprintf("Magazine x%d", player.SpareMagazines))
	}
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

func duckPercent(numerator, denominator uint64, decimals int) string {
	if denominator == 0 {
		if decimals > 0 {
			return "0.0%"
		}
		return "0%"
	}
	value := float64(numerator) * 100 / float64(denominator)
	if decimals > 0 {
		return fmt.Sprintf("%.1f%%", value)
	}
	return fmt.Sprintf("%.0f%%", value)
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
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: you do not have arcade gear; use !shop. You have %d points.", m.Nick, player.Points))
		return
	}
	weapon := p.weaponForPlayer(player)
	jammed := ""
	if player.Jammed {
		jammed = " JAMMED—use !unjam."
	}
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s: %s ammo %d/%d, spare magazines %d, points %d.%s", m.Nick, weapon.Name, player.Ammo, weapon.MagazineSize, player.SpareMagazines, player.Points, jammed))
}

func duckAmmoHint(player duckPlayer) string {
	if player.HasGun && player.Ammo == 0 {
		return " " + ircColor(ircYellow, "Out of ammo—use !reload or !buy magazine (or !buy mag).")
	}
	return ""
}

func (p *DuckHunt) shop(b *bot.Bot, m bot.Message) {
	parts := make([]string, 0, len(p.shopWeapons())+1)
	if p.cfg.firearmEnabled {
		for _, weapon := range p.shopWeapons() {
			name := weapon.Name
			if weapon.Key == "golden" {
				name = ircColor(ircYellow, name)
			} else {
				name = ircColor(ircCyan, name)
			}
			parts = append(parts, fmt.Sprintf("!buy %s (%s): %d points, %d dmg, %d-cap mag", weapon.Key, name, weapon.Cost, weapon.Damage, weapon.MagazineSize))
		}
		parts = append(parts, fmt.Sprintf("!buy magazine (or !buy mag): %d points", p.cfg.magazineCost))
	}
	label := "Duck Hunt shop"
	if !p.cfg.firearmEnabled {
		label += " (arcade gear disabled)"
	}
	if len(parts) > 0 {
		b.Send(m.ReplyTarget(), label+": "+strings.Join(parts, " | "))
	}
	items := make([]string, 0, len(p.consumables()))
	for _, item := range p.consumables() {
		items = append(items, fmt.Sprintf("!buy %s %s: %d points (%s)", item.ID, item.Name, item.Cost, item.Description))
	}
	for start := 0; start < len(items); start += 4 {
		end := minInt(start+4, len(items))
		b.Send(m.ReplyTarget(), "Duck Hunt items: "+strings.Join(items[start:end], " | "))
	}
}

func (p *DuckHunt) buy(b *bot.Bot, m bot.Message, arg string) {
	item := normalizeDuckShopItem(arg)
	consumable, isConsumable := p.findConsumable(item)
	if isConsumable {
		item = consumable.Key
	}
	if item != "magazine" && !isConsumable {
		if _, ok := p.findWeapon(item); !ok {
			b.Send(m.ReplyTarget(), ircColor(ircCyan, "usage: !shop or !buy peashooter|quacker|golden|1-7|magazine|mag"))
			return
		}
	}
	if !p.cfg.firearmEnabled && !isConsumable {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "Duck Hunt arcade gear is disabled."))
		return
	}
	p.mu.Lock()
	player := p.loadPlayerLocked(b.Config.NetworkName, m.Target, m.Nick)
	if isConsumable {
		if player.Points < consumable.Cost {
			points := player.Points
			p.mu.Unlock()
			b.Send(m.ReplyTarget(), fmt.Sprintf("%s: %s costs %d points; you have %d.", m.Nick, consumable.Name, consumable.Cost, points))
			return
		}
		player.Points -= consumable.Cost
		setDuckItemCount(&player, consumable.Key, duckItemCount(player, consumable.Key)+1)
		p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: %s purchased for %d points. Inventory: %d. Use !use %s.", m.Nick, consumable.Name, consumable.Cost, duckItemCount(player, consumable.Key), consumable.ID))
		return
	}
	if item != "magazine" {
		weapon, _ := p.findWeapon(item)
		if player.HasGun && player.Weapon == weapon.Key {
			p.mu.Unlock()
			b.Send(m.ReplyTarget(), fmt.Sprintf("%s: you already have the %s.", m.Nick, weapon.Name))
			return
		}
		if player.Points < weapon.Cost {
			points := player.Points
			p.mu.Unlock()
			b.Send(m.ReplyTarget(), fmt.Sprintf("%s: the %s costs %d points; you have %d.", m.Nick, weapon.Name, weapon.Cost, points))
			return
		}
		player.Points -= weapon.Cost
		player.HasGun = true
		player.Weapon = weapon.Key
		player.Ammo = minInt(p.cfg.startingAmmo, weapon.MagazineSize)
		player.SpareMagazines = 0
		p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: %s acquired for %d points; ammo %d/%d. Use !bang during a hunt.", m.Nick, weapon.Name, weapon.Cost, player.Ammo, weapon.MagazineSize))
		return
	}
	if !player.HasGun {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: buy arcade gear first with !shop.", m.Nick))
		return
	}
	if player.Points < p.cfg.magazineCost {
		points := player.Points
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: a spare magazine costs %d points; you have %d.", m.Nick, p.cfg.magazineCost, points))
		return
	}
	player.Points -= p.cfg.magazineCost
	player.SpareMagazines++
	p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s: spare magazine purchased for %d points. Spare magazines: %d.", m.Nick, p.cfg.magazineCost, player.SpareMagazines))
}

func normalizeDuckShopItem(arg string) string {
	item := strings.ToLower(strings.TrimSpace(arg))
	if item == "mag" || item == "ammo" {
		return "magazine"
	}
	return item
}

func normalizeDuckUseItem(arg string) string {
	return strings.ToLower(strings.TrimSpace(arg))
}

func (p *DuckHunt) use(b *bot.Bot, m bot.Message, arg string) {
	item, ok := p.findConsumable(normalizeDuckUseItem(arg))
	if !ok {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "invalid Duck Hunt item ID; use !use <1-7|item> or !shop"))
		return
	}
	now := time.Now()
	p.mu.Lock()
	player := p.loadPlayerLocked(b.Config.NetworkName, m.Target, m.Nick)
	count := duckItemCount(player, item.Key)
	if count < 1 {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: you do not have %s. Buy one with !buy %s.", m.Nick, item.Name, item.ID))
		return
	}
	state := p.stateLocked(b.Config.NetworkName, m.Target)
	if state.stopped {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "Duck Hunt is stopped in this channel; an owner must use !dh start first"))
		return
	}
	message := ""
	consume := true
	switch item.Key {
	case "lucky_feather":
		if state.goldenBoostUntil.After(now) {
			state.goldenBoost = minFloat(0.75, state.goldenBoost+0.25)
		} else {
			state.goldenBoost = 0.25
		}
		state.goldenBoostUntil = now.Add(20 * time.Minute)
		chance := int((p.goldenChanceLocked(state, now) * 100) + 0.5)
		message = fmt.Sprintf("%s: a Lucky Feather glitters in the reeds! The next duck has a %d%% golden chance for 20 minutes.", m.Nick, chance)
	case "duck_whistle":
		if state.active {
			consume = false
			message = fmt.Sprintf("%s: the pond already has a duck; save your Duck Whistle for the next call.", m.Nick)
		} else if !state.nextSpawn.IsZero() && state.nextSpawn.Sub(now) <= 15*time.Second {
			consume = false
			message = fmt.Sprintf("%s: a duck is already on the way; save your Duck Whistle.", m.Nick)
		} else {
			state.nextSpawn = now.Add(15 * time.Second)
			state.flavorAt = randomDuckFlavorTime(now, state.nextSpawn, p.cfg)
			state.flavorSent = false
			message = fmt.Sprintf("%s: the Duck Whistle echoes across the pond! A duck will arrive in 15 seconds.", m.Nick)
		}
	case "decoy":
		if !state.active {
			consume = false
			message = fmt.Sprintf("%s: there is no active hunt for your Decoy Duck to distract.", m.Nick)
		} else {
			state.spawnedAt = state.spawnedAt.Add(20 * time.Second)
			message = fmt.Sprintf("%s: Decoy Duck deployed! The active hunt lasts 20 seconds longer.", m.Nick)
		}
	case "gun_brush":
		if !player.HasGun {
			consume = false
			message = fmt.Sprintf("%s: you do not have arcade gear for a Gun Brush. Use !buy peashooter first.", m.Nick)
		} else if player.FocusedShot {
			consume = false
			message = fmt.Sprintf("%s: your next armed shot is already polished and guaranteed.", m.Nick)
		} else {
			player.FocusedShot = true
			message = fmt.Sprintf("%s: you polished your gear! Your next armed shot is guaranteed to hit.", m.Nick)
		}
	case "bread":
		multiplier := 2.0
		if state.spawnBoostUntil.After(now) && state.spawnMultiplier > 1 {
			multiplier = minFloat(3.0, state.spawnMultiplier+0.5)
		}
		state.spawnMultiplier = multiplier
		state.spawnBoostUntil = now.Add(20 * time.Minute)
		if !state.nextSpawn.IsZero() && state.nextSpawn.After(now) {
			state.nextSpawn = now.Add(time.Duration(float64(state.nextSpawn.Sub(now)) / multiplier))
			state.flavorAt = randomDuckFlavorTime(now, state.nextSpawn, p.cfg)
			state.flavorSent = false
		}
		message = fmt.Sprintf("%s: You scattered bread around the pond! Ducks will spawn %.1fx faster for 20 minutes.", m.Nick, multiplier)
	case "golden_seed":
		if player.GoldenBounty {
			consume = false
			message = fmt.Sprintf("%s: a Golden Seed is already planted; your next completed kill has a bonus ready.", m.Nick)
		} else {
			player.GoldenBounty = true
			message = fmt.Sprintf("%s: Golden Seed planted! Your next completed kill earns a +25 point and +25 XP bounty.", m.Nick)
		}
	case "pond_map":
		if state.nextFlock {
			consume = false
			message = fmt.Sprintf("%s: a flock route is already mapped for the next automatic visit.", m.Nick)
		} else {
			state.nextFlock = true
			message = fmt.Sprintf("%s: the Pond Map reveals a migration route! The next automatic visit will be a flock.", m.Nick)
		}
	}
	if consume {
		setDuckItemCount(&player, item.Key, count-1)
	}
	p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), message)
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
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: you need arcade gear first; use !shop.", m.Nick))
		return
	}
	if player.Jammed {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: your %s is jammed; use !unjam first. Reloading does not clear jams.", m.Nick, p.weaponForPlayer(player).Name))
		return
	}
	weapon := p.weaponForPlayer(player)
	if player.Ammo >= weapon.MagazineSize {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: your %s magazine is already full at %d/%d.", m.Nick, weapon.Name, player.Ammo, weapon.MagazineSize))
		return
	}
	if player.SpareMagazines < 1 {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: no spare magazines. Use !buy magazine or !buy mag.", m.Nick))
		return
	}
	player.SpareMagazines--
	player.Ammo = weapon.MagazineSize
	p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s: *click* new %s magazine loaded! Ammo: %d/%d; spare magazines: %d.", m.Nick, weapon.Name, player.Ammo, weapon.MagazineSize, player.SpareMagazines))
}

func (p *DuckHunt) unjam(b *bot.Bot, m bot.Message) {
	if !p.cfg.firearmEnabled {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "Duck Hunt arcade gear is disabled."))
		return
	}
	p.mu.Lock()
	player := p.loadPlayerLocked(b.Config.NetworkName, m.Target, m.Nick)
	if !player.HasGun {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: you do not have arcade gear; use !shop first.", m.Nick))
		return
	}
	if !player.Jammed {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s: your %s is running smoothly; no jam to clear.", m.Nick, p.weaponForPlayer(player).Name))
		return
	}
	player.Jammed = false
	p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
	weapon := p.weaponForPlayer(player)
	ammo := player.Ammo
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s: *click-clack* %s cleared. Ammo: %d/%d; ready to fire.", m.Nick, weapon.Name, ammo, weapon.MagazineSize))
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
		player.Weapon = ""
		player.Ammo = 0
		player.SpareMagazines = 0
		p.savePlayerLocked(b.Config.NetworkName, m.Target, m.Nick, player)
	}
	p.mu.Unlock()
	if !hadGun {
		b.Send(m.ReplyTarget(), fmt.Sprintf("%s BANG... what did you shoot at? There is no duck in the area. %s", m.Nick, ircColor(ircCyan, "Visit !shop before taking aim.")))
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
	if p.db != nil {
		raw, err := p.db.Get(duckHuntPlayersBucket, duckHuntPlayerKey(network, channel, nick))
		if err == nil {
			var saved duckPlayer
			if storage.Decode(raw, &saved) == nil {
				player = saved
			}
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
		player.Weapon = ""
		player.Ammo = 0
		player.SpareMagazines = 0
	}
	if player.HasGun && strings.TrimSpace(player.Weapon) == "" {
		// Migrate players created by the earlier generic-gun implementation.
		player.Weapon = "peashooter"
	}
	if !player.HasGun {
		player.Weapon = ""
	}
	if player.Points < 0 {
		player.Points = 0
	}
	if player.XP < 0 {
		player.XP = 0
	}
	if player.Ammo < 0 {
		player.Ammo = 0
	}
	if player.HasGun {
		if weapon := p.weaponForPlayer(player); player.Ammo > weapon.MagazineSize {
			player.Ammo = weapon.MagazineSize
		}
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

// AwardXP adds progression XP to the player's profile for the current
// network/channel. It is used by the daily bonus plugin so daily rewards and
// Duck Hunt progression share one durable ledger.
func (p *DuckHunt) AwardXP(network, channel, nick string, amount int64) (int64, error) {
	if amount <= 0 {
		return 0, fmt.Errorf("XP award must be positive")
	}
	const maxInt64 = int64(1<<63 - 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	player := p.loadPlayerLocked(network, channel, nick)
	if player.XP > maxInt64-amount {
		return player.XP, fmt.Errorf("XP total is too large")
	}
	player.XP += amount
	if p.db != nil {
		if err := p.db.Set(duckHuntPlayersBucket, duckHuntPlayerKey(network, channel, nick), player); err != nil {
			return player.XP - amount, err
		}
	}
	return player.XP, nil
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
	state.flockRemaining = 0
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

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func (p *DuckHunt) randomFlockSize() int {
	flockSize := p.cfg.flockMin
	if p.cfg.flockMax > p.cfg.flockMin {
		flockSize += rand.Intn(p.cfg.flockMax - p.cfg.flockMin + 1)
	}
	return flockSize
}

func (p *DuckHunt) goldenChanceLocked(state *duckHuntState, now time.Time) float64 {
	chance := p.cfg.goldenChance
	if state == nil || state.goldenBoostUntil.IsZero() {
		return chance
	}
	if !now.Before(state.goldenBoostUntil) {
		state.goldenBoost = 0
		state.goldenBoostUntil = time.Time{}
		return chance
	}
	return minFloat(1, chance+state.goldenBoost)
}

func (p *DuckHunt) duckSpawnDelayLocked(state *duckHuntState, now time.Time) time.Duration {
	delay := randomDuckDelay(p.cfg)
	multiplier := 1.0
	if state != nil && state.spawnBoostUntil.After(now) && state.spawnMultiplier > 1 {
		multiplier = state.spawnMultiplier
	}
	if multiplier > 1 {
		delay = time.Duration(float64(delay) / multiplier)
		if delay < time.Second {
			delay = time.Second
		}
	}
	return delay
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

func xpReward(base int64, golden bool) int64 {
	if base <= 0 {
		return 0
	}
	if golden {
		return base * 2
	}
	return base
}

// xpForLevel uses a gentle cumulative curve: level 2 starts at 100 XP,
// level 3 at 300 XP, level 4 at 600 XP, and so on.
func xpForLevel(level int) int64 {
	if level <= 1 {
		return 0
	}
	n := int64(level - 1)
	return 100 * n * (n + 1) / 2
}

func duckLevel(xp int64) int {
	if xp < 0 {
		xp = 0
	}
	level := 1
	for level < 100000 && xp >= xpForLevel(level+1) {
		level++
	}
	return level
}

var duckRankTitles = []struct {
	level int
	title string
}{
	{level: 1, title: "Pond Rookie"},
	{level: 2, title: "Feather Finder"},
	{level: 3, title: "Waddle Scout"},
	{level: 4, title: "Duck Tracker"},
	{level: 5, title: "Quack Ranger"},
	{level: 6, title: "Duck Whisperer"},
	{level: 7, title: "Flock Tactician"},
	{level: 8, title: "Golden Wing"},
	{level: 9, title: "Pond Guardian"},
	{level: 10, title: "Legendary Hunter"},
	{level: 12, title: "Mythic Mallard"},
	{level: 15, title: "Quackmaster"},
	{level: 20, title: "Grand Duckmaster"},
	{level: 25, title: "Eternal Duckkeeper"},
}

func duckTitle(level int) string {
	if level < 1 {
		level = 1
	}
	title := duckRankTitles[0].title
	for _, rank := range duckRankTitles {
		if rank.level > level {
			break
		}
		title = rank.title
	}
	return title
}

func nextDuckRank(level int) (int, string, bool) {
	for _, rank := range duckRankTitles {
		if rank.level > level {
			return rank.level, rank.title, true
		}
	}
	return 0, "", false
}

func xpToNextLevel(xp int64) int64 {
	level := duckLevel(xp)
	remaining := xpForLevel(level+1) - xp
	if remaining < 0 {
		return 0
	}
	return remaining
}

func duckName(state *duckHuntState) string {
	if state != nil && state.golden {
		return ircColor(ircYellow, "the GOLDEN DUCK")
	}
	if state != nil && state.flockRemaining > 1 {
		return ircColor(ircCyan, "a duck in the flock")
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
	if state != nil && state.flockRemaining > 1 {
		return fmt.Sprintf("%s %s", ircColor(ircGreen, "[Duck Hunt]"), ircColor(ircYellow, fmt.Sprintf("A flock of %d ducks has landed!", state.flockRemaining))+" Type "+ircColor(ircBold, "!bang")+" to pick them off!")
	}
	// Keep the active-duck announcement compact and recognizable in both
	// graphical and terminal IRC clients. The body uses mIRC orange as a soft
	// tan approximation, the head is green, and the bill is yellow.
	duck := coloredDuckASCII()
	noise := ircColor(ircCyan, "QUACK!")
	hp := 1
	if state != nil && state.maxHP > 0 {
		hp = state.maxHP
	}
	return fmt.Sprintf("%s %s %s HP: %d | Type %s to shoot or %s to befriend!", ircColor(ircGreen, "[Duck Hunt]"), duck, noise, hp, ircColor(ircBold, "!bang"), ircColor(ircBold, "!bef"))
}

func coloredDuckASCII() string {
	return ircColor(ircTan, `\_`) + ircColor(ircGreen, "o") + ircColor(ircYellow, "<")
}

func randomDuckEscape() string {
	return randomDuckEscapeForState(nil)
}

func randomDuckEscapeForState(state *duckHuntState) string {
	if state != nil && state.flockRemaining > 1 {
		return fmt.Sprintf("%s %s", ircColor(ircGreen, "[Duck Hunt]"), ircColor(ircYellow, fmt.Sprintf("The flock of %d ducks scatters into the sky!", state.flockRemaining)))
	}
	actions := []string{
		`The duck escapes into the sky! °°...`,
		`The duck flaps away, living another day. °°°...`,
		`The duck waddles behind a bush and gets away!`,
		`*ZOOM* The speedy duck vanishes in a flash!`,
		`The duck takes off in a hurry. QUACK! °°...`,
		`The duck slips away through the reeds. Better luck next time!`,
		`The duck spreads its wings and soars away.`,
		`The duck makes a break for it—waddle waddle waddle!`,
		`The ninja duck drops a smoke bomb and vanishes! *poof*`,
		`The duck moonwalks into the reeds and disappears.`,
		`The duck performs an evasive barrel roll and escapes!`,
	}
	return fmt.Sprintf("%s %s %s", ircColor(ircGreen, "[Duck Hunt]"), coloredDuckASCII(), ircColor(ircYellow, actions[rand.Intn(len(actions))]))
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
