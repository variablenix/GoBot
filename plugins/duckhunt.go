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
	xpPerHit        int64
	xpPerKill       int64
	xpPerBefriend   int64
	flockMin        int
	flockMax        int
}

type duckHuntState struct {
	channel        string
	messages       int
	users          map[string]struct{}
	nextSpawn      time.Time
	spawnedAt      time.Time
	flavorAt       time.Time
	flavorSent     bool
	active         bool
	stopped        bool
	hp             int
	maxHP          int
	golden         bool
	trust          map[string]int
	flockRemaining int
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
}

type duckWeapon struct {
	Key          string
	Name         string
	Cost         int64
	Damage       int
	MagazineSize int
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
	return []string{"duckhunt", "dh", "ducklaunch", "bang", "befriend", "bef", "ducks", "level", "xp", "profile", "shop", "store", "buy", "reload", "ammo"}
}
func (p *DuckHunt) Help() string {
	return "!bang shoots an active duck; !bef befriends it; !ducklaunch flock starts a random flock; !shop lists arcade gear; !buy <item> (magazine aliases: mag, ammo), !ammo, and !reload manage it; !ducks [nick] shows points and scores; !level [nick] shows XP and level; !dh status|start|stop controls activity"
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
	magazineSize := c.Int("magazine_size", 6)
	startingAmmo := c.Int("starting_ammo", magazineSize)
	startingPoints := int64(c.Int("starting_points", 25))
	magazineCost := int64(c.Int("magazine_cost", 15))
	gunCost := int64(c.Int("gun_cost", 25))
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
		magazineSize:    magazineSize,
		startingAmmo:    startingAmmo,
		startingPoints:  startingPoints,
		magazineCost:    magazineCost,
		gunCost:         gunCost,
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
	case "level", "xp", "profile":
		p.profile(b, m, arg)
		return true
	case "shop", "store":
		p.shop(b, m)
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
	flockSize := p.cfg.flockMin
	if p.cfg.flockMax > p.cfg.flockMin {
		flockSize += rand.Intn(p.cfg.flockMax - p.cfg.flockMin + 1)
	}
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
			state.flockRemaining = 0
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
		state.golden = rand.Float64() < p.cfg.goldenChance
		state.trust = make(map[string]int)
		state.flockRemaining = 1
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
			b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("%s: click... visit !shop and buy some arcade gear first", m.Nick)))
			return
		}
		if player.Ammo < 1 {
			p.mu.Unlock()
			b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("%s: *click* you're out of ammo; use !reload if you have a spare magazine, or !buy magazine for more ammo", m.Nick)))
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
	if strings.ContainsAny(nick, " \r\n\t") || len([]rune(nick)) > 64 {
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
	if strings.ContainsAny(nick, " \r\n\t") || len([]rune(nick)) > 64 {
		b.Send(m.ReplyTarget(), ircColor(ircCyan, "usage: !level [nick]"))
		return
	}
	score := p.readScore(b.Config.NetworkName, m.Target, nick)
	p.mu.Lock()
	player := p.loadPlayerLocked(b.Config.NetworkName, m.Target, nick)
	p.mu.Unlock()
	level := duckLevel(player.XP)
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s is level %d with %d XP (%d to level %d), %d points, %d killed, and %d befriended in %s.", nick, level, player.XP, xpToNextLevel(player.XP), level+1, player.Points, score.Ducks, score.Friends, m.Target))
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
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s: %s ammo %d/%d, spare magazines %d, points %d.", m.Nick, weapon.Name, player.Ammo, weapon.MagazineSize, player.SpareMagazines, player.Points))
}

func duckAmmoHint(player duckPlayer) string {
	if player.HasGun && player.Ammo == 0 {
		return " " + ircColor(ircYellow, "Out of ammo—use !reload or !buy magazine (or !buy mag).")
	}
	return ""
}

func (p *DuckHunt) shop(b *bot.Bot, m bot.Message) {
	if !p.cfg.firearmEnabled {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "Duck Hunt arcade gear is disabled."))
		return
	}
	parts := make([]string, 0, len(p.shopWeapons())+1)
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
	b.Send(m.ReplyTarget(), "Duck Hunt shop: "+strings.Join(parts, " | "))
}

func (p *DuckHunt) buy(b *bot.Bot, m bot.Message, arg string) {
	item := normalizeDuckShopItem(arg)
	if item != "magazine" {
		if _, ok := p.findWeapon(item); !ok {
			b.Send(m.ReplyTarget(), ircColor(ircCyan, "usage: !shop or !buy peashooter|quacker|golden|magazine|mag"))
			return
		}
	}
	if !p.cfg.firearmEnabled {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "Duck Hunt arcade gear is disabled."))
		return
	}
	p.mu.Lock()
	player := p.loadPlayerLocked(b.Config.NetworkName, m.Target, m.Nick)
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
