package plugins

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
	"go.uber.org/zap"
)

func TestDuckHuntDefaults(t *testing.T) {
	plugin := &DuckHunt{}
	if err := plugin.Init(nil, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if plugin.cfg.minimumMessages != 25 || plugin.cfg.minimumUsers != 2 {
		t.Fatalf("unexpected activity defaults: %+v", plugin.cfg)
	}
	if plugin.cfg.minDelay != time.Minute || plugin.cfg.maxDelay != 5*time.Minute || plugin.cfg.timeout != time.Minute {
		t.Fatalf("unexpected timing defaults: %+v", plugin.cfg)
	}
	if !plugin.cfg.befriendEnabled || plugin.cfg.minReaction != time.Second || plugin.cfg.retryCooldown != 7*time.Second {
		t.Fatalf("unexpected interaction defaults: %+v", plugin.cfg)
	}
	if !plugin.cfg.flavorEnabled || plugin.cfg.flavorMinLead != 15*time.Second {
		t.Fatalf("unexpected flavor defaults: %+v", plugin.cfg)
	}
	if plugin.cfg.minHP != 1 || plugin.cfg.maxHP != 5 || plugin.cfg.damagePerShot != 1 || plugin.cfg.trustAttempts != 3 {
		t.Fatalf("unexpected duck mechanics defaults: %+v", plugin.cfg)
	}
	if !plugin.cfg.firearmEnabled || plugin.cfg.magazineSize != 6 || plugin.cfg.startingAmmo != 6 || plugin.cfg.startingPoints != 25 || plugin.cfg.jamChance != 0.03 {
		t.Fatalf("unexpected arcade gear defaults: %+v", plugin.cfg)
	}
	if plugin.cfg.xpPerHit != 5 || plugin.cfg.xpPerKill != 25 || plugin.cfg.xpPerBefriend != 10 || plugin.cfg.breadCost != 8 || plugin.cfg.flockMin != 2 || plugin.cfg.flockMax != 4 {
		t.Fatalf("unexpected progression defaults: %+v", plugin.cfg)
	}
}

func TestDuckHuntKillRewardsExceedBefriendRewards(t *testing.T) {
	plugin := &DuckHunt{}
	if err := plugin.Init(nil, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if plugin.cfg.xpPerKill <= plugin.cfg.xpPerBefriend {
		t.Fatalf("kill XP %d should exceed befriend XP %d", plugin.cfg.xpPerKill, plugin.cfg.xpPerBefriend)
	}
	if xpReward(plugin.cfg.xpPerKill, false) <= xpReward(plugin.cfg.xpPerBefriend, false) {
		t.Fatal("kill reward should exceed befriend reward")
	}
}

func TestDuckHuntAchievementCatalogIsSubstantialAndUnique(t *testing.T) {
	if len(duckAchievementCatalog) < 15 {
		t.Fatalf("achievement catalog has %d entries, want at least 15", len(duckAchievementCatalog))
	}
	seen := make(map[string]struct{}, len(duckAchievementCatalog))
	for _, achievement := range duckAchievementCatalog {
		if achievement.Key == "" || achievement.Name == "" || achievement.Description == "" {
			t.Fatalf("incomplete achievement definition: %+v", achievement)
		}
		if _, ok := seen[achievement.Key]; ok {
			t.Fatalf("duplicate achievement key %q", achievement.Key)
		}
		seen[achievement.Key] = struct{}{}
	}
}

func TestDuckHuntGoldenAchievementColorsOnlyGoldenDuck(t *testing.T) {
	var golden duckAchievementDefinition
	for _, achievement := range duckAchievementCatalog {
		if achievement.Key == "golden_slayer" {
			golden = achievement
			break
		}
	}
	message := formatDuckAchievement("GoBot", golden)
	if !strings.Contains(message, "Killed a "+ircColor(ircYellow, "GOLDEN DUCK")) {
		t.Fatalf("achievement message = %q, want only GOLDEN DUCK highlighted", message)
	}
	if strings.Contains(message, ircYellow+"Killed a") || strings.Contains(message, ircYellow+"a ") {
		t.Fatalf("achievement article or description prefix was colored: %q", message)
	}
}

func TestDuckHuntAchievementsPersistAndUnlockOnce(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "achievements.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	plugin := &DuckHunt{}
	if err := plugin.Init(nil, db); err != nil {
		t.Fatal(err)
	}

	plugin.mu.Lock()
	player := plugin.loadPlayerLocked("network", "#chat", "GoBot")
	player.Hits = 1
	player.Shots = 1
	player.Points = 1000
	player.XP = 1000
	unlocked := unlockDuckAchievements(&player, 1, 0, duckAchievementEvent{
		Hit:           true,
		Killed:        true,
		Golden:        true,
		FlockComplete: true,
		LastShot:      true,
	})
	plugin.savePlayerLocked("network", "#chat", "GoBot", player)
	loaded := plugin.loadPlayerLocked("network", "#chat", "GoBot")
	plugin.mu.Unlock()

	if len(unlocked) < 6 {
		t.Fatalf("unlocked %d achievements, want multiple milestone achievements", len(unlocked))
	}
	if !loaded.Achievements["golden_slayer"] || !loaded.Achievements["flock_buster"] {
		t.Fatalf("persisted achievements = %+v", loaded.Achievements)
	}
	if repeat := unlockDuckAchievements(&loaded, 1, 0, duckAchievementEvent{Killed: true, Golden: true, FlockComplete: true}); len(repeat) != 0 {
		t.Fatalf("already unlocked achievements were emitted again: %+v", repeat)
	}
}

func TestDuckHuntIncludesBefriendAlias(t *testing.T) {
	plugin := &DuckHunt{}
	found := false
	for _, command := range plugin.Commands() {
		if command == "bef" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected !bef command alias")
	}
	for _, command := range []string{"ducklaunch", "launch", "shop", "store", "buy", "use", "reload", "unjam", "clearjam", "ammo", "ducks", "duckscore", "duckstats", "dstats", "achievements", "achieve", "ach", "level", "lvl", "xp", "profile", "prof"} {
		found = false
		for _, candidate := range plugin.Commands() {
			if candidate == command {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %q command", command)
		}
	}
	if !strings.Contains(plugin.Help(), "!bef") {
		t.Fatal("expected !bef alias in help")
	}
	if !strings.Contains(plugin.Help(), "!level") {
		t.Fatal("expected progression command in help")
	}
	if !strings.Contains(plugin.Help(), "items 1-7") || !strings.Contains(plugin.Help(), "!use <1-7|item>") {
		t.Fatal("expected item catalog in help")
	}
	if !strings.Contains(plugin.Help(), "!duckstats") {
		t.Fatal("expected duckstats and item usage in help")
	}
	if !strings.Contains(plugin.Help(), "!achievements") {
		t.Fatal("expected achievements command in help")
	}
	for _, alias := range []string{"!achieve", "!ach", "!launch", "!duckscore", "!dstats", "!lvl", "!prof", "!clearjam"} {
		if !strings.Contains(plugin.Help(), alias) {
			t.Fatalf("expected %s alias in help", alias)
		}
	}
}

func TestDuckHuntUseItemAliases(t *testing.T) {
	plugin := &DuckHunt{}
	if err := plugin.Init(nil, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	for _, test := range []struct {
		alias string
		key   string
	}{
		{alias: "1", key: "lucky_feather"},
		{alias: "feather", key: "lucky_feather"},
		{alias: "2", key: "duck_whistle"},
		{alias: "whistle", key: "duck_whistle"},
		{alias: "3", key: "decoy"},
		{alias: "4", key: "gun-brush"},
		{alias: "5", key: "bread"},
		{alias: " BREAD ", key: "bread"},
		{alias: "6", key: "golden_seed"},
		{alias: "7", key: "pond_map"},
	} {
		item, ok := plugin.findConsumable(test.alias)
		if !ok || item.Key != strings.ReplaceAll(test.key, "-", "_") {
			t.Fatalf("findConsumable(%q) = %+v, %v; want %s", test.alias, item, ok, test.key)
		}
	}
	if _, ok := plugin.findConsumable("8"); ok {
		t.Fatal("invalid item ID resolved to a consumable")
	}
}

func TestDuckHuntProfileNickValidationRejectsIRCControls(t *testing.T) {
	for _, nick := range []string{"Alice", "ak[Relay]", "bot_2"} {
		if !validDuckProfileNick(nick) {
			t.Errorf("validDuckProfileNick(%q) = false, want true", nick)
		}
	}
	for _, nick := range []string{"", "Alice Smith", "Alice\x03", "Alice\n", strings.Repeat("A", 65)} {
		if validDuckProfileNick(nick) {
			t.Errorf("validDuckProfileNick(%q) = true, want false", nick)
		}
	}
}

func TestDuckHuntMagazineAliases(t *testing.T) {
	for _, alias := range []string{"magazine", "mag", "ammo", " MAG "} {
		if got := normalizeDuckShopItem(alias); got != "magazine" {
			t.Fatalf("normalizeDuckShopItem(%q) = %q, want magazine", alias, got)
		}
	}
}

func TestDuckHuntShopCatalog(t *testing.T) {
	plugin := &DuckHunt{}
	if err := plugin.Init(nil, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	weapons := plugin.shopWeapons()
	if len(weapons) != 3 {
		t.Fatalf("shop has %d weapons, want 3", len(weapons))
	}
	if weapon, ok := plugin.findWeapon("gun"); !ok || weapon.Key != "peashooter" {
		t.Fatalf("generic gun alias resolved to %+v, want peashooter", weapon)
	}
	if weapons[1].MagazineSize <= weapons[0].MagazineSize {
		t.Fatalf("Quacker Blaster should have a larger magazine: %+v", weapons)
	}
	if weapons[2].Damage <= weapons[0].Damage || weapons[2].Cost <= weapons[0].Cost {
		t.Fatalf("Golden Wing should cost more and do more damage: %+v", weapons)
	}
}

func TestDuckHuntConsumableCatalogAndInventory(t *testing.T) {
	plugin := &DuckHunt{}
	if err := plugin.Init(nil, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	items := plugin.consumables()
	if len(items) != 7 {
		t.Fatalf("consumable catalog has %d items, want 7", len(items))
	}
	player := duckPlayer{}
	for _, item := range items {
		if item.ID == "" || item.Key == "" || item.Name == "" || item.Cost <= 0 || item.Description == "" {
			t.Fatalf("incomplete consumable definition: %+v", item)
		}
		setDuckItemCount(&player, item.Key, 1)
		if got := duckItemCount(player, item.Key); got != 1 {
			t.Fatalf("duckItemCount(%q) = %d, want 1", item.Key, got)
		}
	}
	if got := duckItems(player); !strings.Contains(got, "Lucky Feather x1") || !strings.Contains(got, "Pond Map x1") {
		t.Fatalf("inventory summary = %q, want all catalog items", got)
	}
	for _, alias := range []string{"1", "lucky-feather", "2", "duck whistle", "3", "decoy-duck", "4", "gun-brush", "5", "6", "golden seed", "7", "pond-map"} {
		if _, ok := plugin.findConsumable(alias); !ok {
			t.Fatalf("findConsumable(%q) did not resolve", alias)
		}
	}
}

func TestDuckHuntConsumableEffectsAreDistinctAndNonDestructive(t *testing.T) {
	db, err := storage.Open(t.TempDir() + "/bot.db")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	plugin := &DuckHunt{}
	if err := plugin.Init(bot.PluginConfig{"min_delay_seconds": 60, "max_delay_seconds": 60}, db); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	const network, channel, nick = "network", "#chat", "Alice"
	plugin.mu.Lock()
	player := plugin.loadPlayerLocked(network, channel, nick)
	player.HasGun = true
	player.Weapon = "peashooter"
	player.LuckyFeathers = 1
	player.DuckWhistles = 1
	player.Decoys = 1
	player.GunBrushes = 1
	player.Bread = 1
	player.GoldenSeeds = 1
	player.PondMaps = 1
	plugin.savePlayerLocked(network, channel, nick, player)
	plugin.mu.Unlock()

	b := bot.New(bot.Config{NetworkName: network, CommandPrefix: "!"}, db, nil, zap.NewNop())
	message := bot.Message{Nick: nick, Target: channel, IsChannel: true}
	for _, id := range []string{"1", "2", "4", "5", "6", "7"} {
		plugin.use(b, message, id)
	}
	plugin.mu.Lock()
	state := plugin.states[duckHuntStateKey(network, channel)]
	player = plugin.loadPlayerLocked(network, channel, nick)
	plugin.mu.Unlock()
	if player.LuckyFeathers != 0 || player.DuckWhistles != 0 || player.GunBrushes != 0 || player.Bread != 0 || player.GoldenSeeds != 0 || player.PondMaps != 0 {
		t.Fatalf("consumables were not consumed: %+v", player)
	}
	if !player.FocusedShot || !player.GoldenBounty || !state.nextFlock || state.goldenBoost <= 0 || state.nextSpawn.IsZero() {
		t.Fatalf("distinct effects were not applied: player=%+v state=%+v", player, state)
	}

	plugin.use(b, message, "3")
	plugin.mu.Lock()
	player = plugin.loadPlayerLocked(network, channel, nick)
	plugin.mu.Unlock()
	if player.Decoys != 1 {
		t.Fatalf("inapplicable Decoy Duck was consumed: %+v", player)
	}
	plugin.mu.Lock()
	state = plugin.states[duckHuntStateKey(network, channel)]
	state.active = true
	state.spawnedAt = time.Now().Add(-10 * time.Second)
	before := state.spawnedAt
	player.Decoys = 1
	plugin.savePlayerLocked(network, channel, nick, player)
	plugin.mu.Unlock()
	plugin.use(b, message, "3")
	plugin.mu.Lock()
	state = plugin.states[duckHuntStateKey(network, channel)]
	player = plugin.loadPlayerLocked(network, channel, nick)
	plugin.mu.Unlock()
	if player.Decoys != 0 || !state.spawnedAt.After(before) {
		t.Fatalf("applicable Decoy Duck did not extend hunt: player=%+v state=%+v", player, state)
	}
}

func TestDuckHuntGoldenSeedAddsOneKillBounty(t *testing.T) {
	db, err := storage.Open(t.TempDir() + "/bot.db")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	plugin := &DuckHunt{}
	if err := plugin.Init(bot.PluginConfig{"min_reaction_seconds": 0, "minimum_hp": 1, "maximum_hp": 1, "weapon_jam_probability": 0}, db); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	const network, channel, nick = "network", "#chat", "Alice"
	plugin.mu.Lock()
	state := plugin.stateLocked(network, channel)
	state.active = true
	state.spawnedAt = time.Now().Add(-10 * time.Second)
	state.hp = 1
	state.maxHP = 1
	player := plugin.loadPlayerLocked(network, channel, nick)
	player.HasGun = true
	player.Weapon = "peashooter"
	player.Ammo = 1
	player.GoldenBounty = true
	plugin.savePlayerLocked(network, channel, nick, player)
	plugin.mu.Unlock()

	b := bot.New(bot.Config{NetworkName: network, CommandPrefix: "!"}, db, nil, zap.NewNop())
	plugin.interact(b, bot.Message{Nick: nick, Target: channel, IsChannel: true}, false)
	plugin.mu.Lock()
	player = plugin.loadPlayerLocked(network, channel, nick)
	plugin.mu.Unlock()
	if player.XP != 50 || player.Points != 60 || player.GoldenBounty {
		t.Fatalf("golden seed reward = %+v, want 50 XP, 60 points, and consumed bounty", player)
	}
}

func TestDuckHuntAnnouncementKeepsDuckASCIICompact(t *testing.T) {
	announcement := randomDuckAnnouncementForState(&duckHuntState{golden: true, maxHP: 3})
	if !strings.Contains(announcement, "QUACK!") {
		t.Fatalf("announcement = %q, want QUACK!", announcement)
	}
	if !strings.Contains(announcement, "HP: 3") {
		t.Fatalf("announcement = %q, want HP: 3", announcement)
	}
	if strings.Contains(announcement, "GOLDEN DUCK") {
		t.Fatalf("announcement = %q, should not put GOLDEN DUCK in the duck body", announcement)
	}
	if !strings.Contains(announcement, "\\_") || !strings.Contains(announcement, "o") || !strings.Contains(announcement, "<") {
		t.Fatalf("announcement = %q, want compact duck ASCII", announcement)
	}
}

func TestDuckHuntGoldenDuckColorStartsAtLabel(t *testing.T) {
	got := duckName(&duckHuntState{golden: true})
	want := "the " + ircColor(ircYellow, "GOLDEN DUCK")
	if got != want {
		t.Fatalf("golden duck name = %q, want %q", got, want)
	}
	if strings.HasPrefix(got, ircYellow) {
		t.Fatal("the article must not be yellow")
	}
}

func TestDuckHuntSchedulesAfterActivityThreshold(t *testing.T) {
	plugin := &DuckHunt{}
	if err := plugin.Init(bot.PluginConfig{
		"minimum_messages":  2,
		"minimum_users":     2,
		"min_delay_seconds": 1,
		"max_delay_seconds": 1,
	}, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	plugin.recordActivity("network", "#Test", "Alice")
	plugin.recordActivity("network", "#Test", "Bob")

	state := plugin.states[duckHuntStateKey("network", "#test")]
	if state == nil || state.nextSpawn.IsZero() {
		t.Fatal("expected duck spawn to be scheduled after the activity threshold")
	}
	if state.channel != "#Test" {
		t.Fatalf("expected original channel spelling to be retained, got %q", state.channel)
	}
}

func TestDuckHuntScoresPersistPerChannelAndNick(t *testing.T) {
	db, err := storage.Open(t.TempDir() + "/bot.db")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	plugin := &DuckHunt{}
	if err := plugin.Init(nil, db); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if got := plugin.incrementScore("network", "#Test", "Alice"); got != 1 {
		t.Fatalf("first score = %d, want 1", got)
	}
	if got := plugin.incrementFriends("network", "#test", "Alice"); got != 1 {
		t.Fatalf("first friend score = %d, want 1", got)
	}
	if got := plugin.incrementScore("network", "#test", "alice"); got != 2 {
		t.Fatalf("second score = %d, want 2", got)
	}
	if got := plugin.readScore("network", "#other", "Alice").Ducks; got != 0 {
		t.Fatalf("different channel score = %d, want 0", got)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	reopened, err := storage.Open(t.TempDir() + "/unused.db")
	if err != nil {
		t.Fatalf("open second database: %v", err)
	}
	defer reopened.Close()
	// A fresh database intentionally starts empty; this also verifies the
	// plugin does not keep scores in process memory when the DB is replaced.
	reloaded := &DuckHunt{}
	if err := reloaded.Init(nil, reopened); err != nil {
		t.Fatalf("reloaded Init returned error: %v", err)
	}
	if got := reloaded.readScore("network", "#test", "alice").Ducks; got != 0 {
		t.Fatalf("fresh database score = %d, want 0", got)
	}
}

func TestDuckHuntScoresSurviveDatabaseReopen(t *testing.T) {
	dbPath := t.TempDir() + "/bot.db"
	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	plugin := &DuckHunt{}
	if err := plugin.Init(nil, db); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	plugin.incrementScore("network", "#test", "Alice")
	plugin.incrementFriends("network", "#test", "Alice")
	if err := db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	reopened, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer reopened.Close()
	reloaded := &DuckHunt{}
	if err := reloaded.Init(nil, reopened); err != nil {
		t.Fatalf("reloaded Init returned error: %v", err)
	}
	if got := reloaded.readScore("network", "#TEST", "alice").Ducks; got != 1 {
		t.Fatalf("reopened score = %d, want 1", got)
	}
	if got := reloaded.readScore("network", "#TEST", "alice").Friends; got != 1 {
		t.Fatalf("reopened friend score = %d, want 1", got)
	}
}

func TestDuckHuntHitChanceProtectsAgainstInstantShots(t *testing.T) {
	if got := hitChance(500 * time.Millisecond); got != 0 {
		t.Fatalf("instant shot chance = %v, want 0", got)
	}
	if got := hitChance(2 * time.Second); got < 0.60 || got > 0.75 {
		t.Fatalf("normal shot chance = %v, want 0.60-0.75", got)
	}
	if got := hitChance(7 * time.Second); got != 1 {
		t.Fatalf("late shot chance = %v, want 1", got)
	}
}

func TestDuckHuntRandomHPStaysWithinConfiguredRange(t *testing.T) {
	cfg := duckHuntConfig{minHP: 2, maxHP: 5}
	for i := 0; i < 100; i++ {
		got := randomDuckHP(cfg)
		if got < cfg.minHP || got > cfg.maxHP {
			t.Fatalf("random HP = %d, want %d..%d", got, cfg.minHP, cfg.maxHP)
		}
	}
}

func TestDuckHuntPlayerGearPersists(t *testing.T) {
	db, err := storage.Open(t.TempDir() + "/bot.db")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	plugin := &DuckHunt{}
	if err := plugin.Init(nil, db); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	plugin.mu.Lock()
	player := plugin.loadPlayerLocked("network", "#channel", "Alice")
	player.SpareMagazines = 2
	player.Ammo = 3
	player.Points = 99
	player.XP = 123
	player.Shots = 10
	player.Hits = 8
	player.Bread = 1
	player.LuckyFeathers = 2
	player.DuckWhistles = 3
	player.Decoys = 4
	player.GunBrushes = 5
	player.GoldenSeeds = 6
	player.PondMaps = 7
	player.HasGun = true
	player.Weapon = "quacker"
	plugin.savePlayerLocked("network", "#channel", "Alice", player)
	plugin.mu.Unlock()

	plugin.mu.Lock()
	loaded := plugin.loadPlayerLocked("network", "#channel", "alice")
	plugin.mu.Unlock()
	if loaded.SpareMagazines != 2 || loaded.Ammo != 3 || loaded.Points != 99 || loaded.XP != 123 || loaded.Shots != 10 || loaded.Hits != 8 || loaded.Bread != 1 || loaded.LuckyFeathers != 2 || loaded.DuckWhistles != 3 || loaded.Decoys != 4 || loaded.GunBrushes != 5 || loaded.GoldenSeeds != 6 || loaded.PondMaps != 7 || loaded.Weapon != "quacker" {
		t.Fatalf("loaded player = %+v, want persisted gear and points", loaded)
	}
}

func TestDuckHuntTitlesAndDetailedStats(t *testing.T) {
	if duckTitle(1) != "Pond Rookie" || duckTitle(6) != "Duck Whisperer" || duckTitle(25) != "Eternal Duckkeeper" || duckTitle(100) != "Eternal Duckkeeper" {
		t.Fatalf("unexpected Duck Hunt title progression")
	}
	if level, title, ok := nextDuckRank(6); !ok || level != 7 || title != "Flock Tactician" {
		t.Fatalf("next Duck Hunt rank = %d %q %v, want level 7 Flock Tactician", level, title, ok)
	}
	plugin := &DuckHunt{}
	if err := plugin.Init(nil, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	player := duckPlayer{XP: xpForLevel(6), Shots: 102, Hits: 86, HasGun: true, Weapon: "peashooter", Ammo: 1, SpareMagazines: 1, Bread: 1}
	message := stripPluginIRC(plugin.formatDuckStats("mlu", player, duckScore{Ducks: 68, Friends: 5}))
	for _, want := range []string{"Lv6 Duck Whisperer", "1500 XP (Need 600 XP for Flock Tactician)", "102 shots", "68 killed", "5 befriended", "84% accuracy", "79.1% hit rate", "Armed", "1/6 ammo", "1 spare magazine", "3% jam chance", "Bread x1, Magazine x1"} {
		if !strings.Contains(message, want) {
			t.Errorf("Duck Hunt stats %q does not contain %q", message, want)
		}
	}
}

func TestDuckHuntDetailedStatsUsesColorAndLightweightEmojis(t *testing.T) {
	plugin := &DuckHunt{}
	if err := plugin.Init(nil, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	message := plugin.formatDuckStats("Alice", duckPlayer{XP: 100, HasGun: true, Weapon: "peashooter", Ammo: 3}, duckScore{Ducks: 1, Friends: 2})
	for _, want := range []string{ircCyan, ircYellow, ircGreen, ircTan, "🦆", "✨", "🎯", "🎒"} {
		if !strings.Contains(message, want) {
			t.Errorf("Duck Hunt stats %q does not contain color or emoji marker %q", message, want)
		}
	}
}

func TestDuckHuntWeaponJamRequiresUnjam(t *testing.T) {
	db, err := storage.Open(t.TempDir() + "/bot.db")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	plugin := &DuckHunt{}
	if err := plugin.Init(bot.PluginConfig{"min_reaction_seconds": 0, "minimum_hp": 1, "maximum_hp": 1, "weapon_jam_probability": 1}, db); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	plugin.cfg.jamChance = 1
	const network, channel, nick = "network", "#chat", "Alice"
	plugin.mu.Lock()
	state := plugin.stateLocked(network, channel)
	state.active = true
	state.spawnedAt = time.Now().Add(-10 * time.Second)
	state.hp = 1
	state.maxHP = 1
	player := plugin.loadPlayerLocked(network, channel, nick)
	player.HasGun = true
	player.Weapon = "peashooter"
	player.Ammo = 2
	plugin.savePlayerLocked(network, channel, nick, player)
	plugin.mu.Unlock()

	b := bot.New(bot.Config{NetworkName: network, CommandPrefix: "!"}, db, nil, zap.NewNop())
	message := bot.Message{Nick: nick, Target: channel, IsChannel: true}
	plugin.interact(b, message, false)
	plugin.mu.Lock()
	player = plugin.loadPlayerLocked(network, channel, nick)
	plugin.mu.Unlock()
	if !player.Jammed || player.Ammo != 1 || player.Shots != 1 || player.Hits != 0 {
		t.Fatalf("jammed shot state = %+v, want jammed with one ammo, one shot, zero hits", player)
	}

	plugin.unjam(b, message)
	plugin.mu.Lock()
	player = plugin.loadPlayerLocked(network, channel, nick)
	plugin.mu.Unlock()
	if player.Jammed {
		t.Fatalf("unjam did not clear the weapon: %+v", player)
	}

	plugin.cfg.jamChance = 0
	plugin.mu.Lock()
	plugin.attempts = make(map[string]time.Time)
	plugin.mu.Unlock()
	plugin.interact(b, message, false)
	plugin.mu.Lock()
	player = plugin.loadPlayerLocked(network, channel, nick)
	plugin.mu.Unlock()
	if player.Jammed || player.Ammo != 0 || player.Shots != 2 || player.Hits != 1 {
		t.Fatalf("post-unjam shot state = %+v, want a successful shot", player)
	}
}

func TestDuckHuntBreadBoostConsumesItemAndStacks(t *testing.T) {
	db, err := storage.Open(t.TempDir() + "/bot.db")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	plugin := &DuckHunt{}
	if err := plugin.Init(bot.PluginConfig{"min_delay_seconds": 60, "max_delay_seconds": 60}, db); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	const network, channel, nick = "network", "#chat", "Alice"
	plugin.mu.Lock()
	player := plugin.loadPlayerLocked(network, channel, nick)
	player.Bread = 3
	player.Points = 20
	plugin.savePlayerLocked(network, channel, nick, player)
	state := plugin.stateLocked(network, channel)
	state.nextSpawn = time.Now().Add(60 * time.Second)
	plugin.mu.Unlock()

	b := bot.New(bot.Config{NetworkName: network, CommandPrefix: "!"}, db, nil, zap.NewNop())
	message := bot.Message{Nick: nick, Target: channel, IsChannel: true}
	plugin.use(b, message, "5")
	plugin.mu.Lock()
	state = plugin.states[duckHuntStateKey(network, channel)]
	player = plugin.loadPlayerLocked(network, channel, nick)
	firstSpawn := state.nextSpawn
	plugin.mu.Unlock()
	if player.Bread != 2 || state.spawnMultiplier != 2 || !state.spawnBoostUntil.After(time.Now()) {
		t.Fatalf("after first bread use player=%+v state=%+v", player, state)
	}
	if !firstSpawn.Before(time.Now().Add(60 * time.Second)) {
		t.Fatalf("spawn was not accelerated: %v", firstSpawn)
	}

	plugin.use(b, message, "bread")
	plugin.use(b, message, "bread")
	plugin.mu.Lock()
	state = plugin.states[duckHuntStateKey(network, channel)]
	player = plugin.loadPlayerLocked(network, channel, nick)
	plugin.mu.Unlock()
	if player.Bread != 0 || state.spawnMultiplier != 3 {
		t.Fatalf("after stacked bread uses player=%+v state=%+v, want no bread and 3x boost", player, state)
	}
	if got := plugin.duckSpawnDelayLocked(&duckHuntState{spawnMultiplier: 2, spawnBoostUntil: time.Now().Add(time.Minute)}, time.Now()); got != 30*time.Second {
		t.Fatalf("2x spawn delay = %s, want 30s", got)
	}
}

func TestDuckHuntProgression(t *testing.T) {
	tests := []struct {
		xp    int64
		level int
	}{
		{xp: 0, level: 1},
		{xp: 99, level: 1},
		{xp: 100, level: 2},
		{xp: 299, level: 2},
		{xp: 300, level: 3},
	}
	for _, test := range tests {
		if got := duckLevel(test.xp); got != test.level {
			t.Fatalf("duckLevel(%d) = %d, want %d", test.xp, got, test.level)
		}
	}
	if got := xpToNextLevel(123); got != 177 {
		t.Fatalf("xpToNextLevel(123) = %d, want 177", got)
	}
	if got := xpReward(5, false); got != 5 {
		t.Fatalf("xpReward(5, false) = %d, want 5", got)
	}
	if got := xpReward(5, true); got != 10 {
		t.Fatalf("xpReward(5, true) = %d, want 10", got)
	}
}

func TestDuckHuntAnnouncementIncludesColorDuckAndQuack(t *testing.T) {
	announcement := randomDuckAnnouncement()
	if !strings.Contains(announcement, "\x03") {
		t.Fatal("expected mIRC color formatting in Duck Hunt announcement")
	}
	if !strings.Contains(announcement, "[Duck Hunt]") {
		t.Fatal("expected Duck Hunt label in announcement")
	}
	if !strings.Contains(strings.ToLower(announcement), "quack") {
		t.Fatal("expected quack text in announcement")
	}
}

func TestDuckHuntAnnouncementIncludesHPAndActions(t *testing.T) {
	announcement := randomDuckAnnouncementForState(&duckHuntState{golden: true, hp: 4, maxHP: 4})
	if strings.Contains(announcement, "GOLDEN DUCK") || !strings.Contains(announcement, "HP: 4") {
		t.Fatalf("unexpected announcement: %q", announcement)
	}
	if !strings.Contains(announcement, "!bang") || !strings.Contains(announcement, "!bef") {
		t.Fatalf("announcement does not provide actions: %q", announcement)
	}
}

func TestDuckHuntFlockAnnouncementAndAmmoHint(t *testing.T) {
	announcement := randomDuckAnnouncementForState(&duckHuntState{flockRemaining: 2, maxHP: 3})
	plain := stripPluginIRC(announcement)
	if !strings.Contains(plain, "A flock of 2 ducks has landed!") || !strings.Contains(plain, "Type !bang to pick them off!") {
		t.Fatalf("flock announcement = %q", plain)
	}
	if hint := stripPluginIRC(duckAmmoHint(duckPlayer{HasGun: true, Ammo: 0})); !strings.Contains(hint, "Out of ammo") || !strings.Contains(hint, "!buy magazine") {
		t.Fatalf("ammo hint = %q", hint)
	}
	if hint := duckAmmoHint(duckPlayer{HasGun: true, Ammo: 1}); hint != "" {
		t.Fatalf("unexpected ammo hint with ammunition: %q", hint)
	}
}

func TestDuckHuntFlockKillLeavesTheRoundActive(t *testing.T) {
	db, err := storage.Open(t.TempDir() + "/bot.db")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	plugin := &DuckHunt{}
	if err := plugin.Init(bot.PluginConfig{"min_reaction_seconds": 0, "minimum_hp": 1, "maximum_hp": 1, "weapon_jam_probability": 0}, db); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	const network, channel, nick = "network", "#chat", "Alice"
	plugin.mu.Lock()
	state := plugin.stateLocked(network, channel)
	state.active = true
	state.spawnedAt = time.Now().Add(-10 * time.Second)
	state.hp = 1
	state.maxHP = 1
	state.flockRemaining = 2
	player := plugin.loadPlayerLocked(network, channel, nick)
	player.HasGun = true
	player.Weapon = "peashooter"
	player.Ammo = 2
	plugin.savePlayerLocked(network, channel, nick, player)
	plugin.mu.Unlock()

	b := bot.New(bot.Config{NetworkName: network, CommandPrefix: "!"}, db, nil, zap.NewNop())
	plugin.interact(b, bot.Message{Nick: nick, Target: channel, IsChannel: true}, false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	b.Queue.Drain(ctx)
	cancel()

	plugin.mu.Lock()
	state = plugin.states[duckHuntStateKey(network, channel)]
	loaded := plugin.loadPlayerLocked(network, channel, nick)
	plugin.mu.Unlock()
	if state == nil || !state.active || state.flockRemaining != 1 || state.hp != 1 {
		t.Fatalf("flock state after first kill = %+v, want active with one duck remaining", state)
	}
	if loaded.Ammo != 1 || loaded.Shots != 1 || loaded.Hits != 1 {
		t.Fatalf("player after one flock kill = %+v, want 1 ammo, 1 shot, and 1 hit", loaded)
	}
}

func TestDuckHuntFlavorIncludesColorAndMotion(t *testing.T) {
	flavor := randomDuckFlavor()
	if !strings.Contains(flavor, "\x03") {
		t.Fatal("expected mIRC color formatting in Duck Hunt flavor")
	}
	if !strings.Contains(flavor, "[Duck Hunt]") {
		t.Fatal("expected Duck Hunt label in flavor")
	}
	lower := strings.ToLower(flavor)
	if !strings.Contains(lower, "quack") && !strings.Contains(lower, "flap") {
		t.Fatal("expected quack or flap text in flavor")
	}
}

func TestDuckHuntEscapeIncludesColorAndMotion(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 50; i++ {
		escape := randomDuckEscape()
		if !strings.Contains(escape, "\x03") {
			t.Fatal("expected mIRC color formatting in Duck Hunt escape")
		}
		if !strings.Contains(escape, "[Duck Hunt]") || !strings.Contains(escape, `\_`) {
			t.Fatalf("escape = %q, want Duck Hunt label and duck ASCII", escape)
		}
		if strings.Contains(escape, "\n") || strings.Contains(escape, "\r") {
			t.Fatal("escape must remain one IRC message")
		}
		seen[stripPluginIRC(escape)] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("escape flavor did not vary across samples: %v", seen)
	}

	goldenEscape := randomDuckEscapeForState(&duckHuntState{golden: true})
	if !strings.Contains(goldenEscape, "the "+ircColor(ircYellow, "GOLDEN DUCK")) {
		t.Fatalf("golden escape = %q, want colored GOLDEN DUCK label", goldenEscape)
	}
}

func TestDuckHuntFlockEscapeVaries(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 50; i++ {
		seen[stripPluginIRC(randomDuckEscapeForState(&duckHuntState{flockRemaining: 3}))] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("flock escape flavor did not vary across samples: %v", seen)
	}
}

func TestDuckHuntFlavorTimeIsBeforeSpawn(t *testing.T) {
	now := time.Unix(1000, 0)
	nextSpawn := now.Add(60 * time.Second)
	cfg := duckHuntConfig{flavorEnabled: true, flavorMinLead: 15 * time.Second}
	flavorAt := randomDuckFlavorTime(now, nextSpawn, cfg)
	if flavorAt.Before(now.Add(15*time.Second)) || flavorAt.After(nextSpawn.Add(-15*time.Second)) {
		t.Fatalf("flavor time %v is outside the expected window", flavorAt)
	}

	cfg.flavorEnabled = false
	if flavorAt := randomDuckFlavorTime(now, nextSpawn, cfg); !flavorAt.IsZero() {
		t.Fatalf("disabled flavor scheduled at %v", flavorAt)
	}
}
