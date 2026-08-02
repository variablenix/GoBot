package plugins

import (
	"strings"
	"testing"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
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
	if !plugin.cfg.firearmEnabled || plugin.cfg.magazineSize != 6 || plugin.cfg.startingAmmo != 6 || plugin.cfg.startingPoints != 25 {
		t.Fatalf("unexpected arcade gear defaults: %+v", plugin.cfg)
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
	for _, command := range []string{"buy", "reload", "ammo"} {
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
	plugin.savePlayerLocked("network", "#channel", "Alice", player)
	plugin.mu.Unlock()

	plugin.mu.Lock()
	loaded := plugin.loadPlayerLocked("network", "#channel", "alice")
	plugin.mu.Unlock()
	if loaded.SpareMagazines != 2 || loaded.Ammo != 3 || loaded.Points != 99 {
		t.Fatalf("loaded player = %+v, want persisted gear and points", loaded)
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

func TestDuckHuntGoldenAnnouncementIncludesHPAndActions(t *testing.T) {
	announcement := randomDuckAnnouncementForState(&duckHuntState{golden: true, hp: 4, maxHP: 4})
	if !strings.Contains(announcement, "GOLDEN DUCK") || !strings.Contains(announcement, "HP: 4") {
		t.Fatalf("unexpected golden announcement: %q", announcement)
	}
	if !strings.Contains(announcement, "!bang") || !strings.Contains(announcement, "!bef") {
		t.Fatalf("announcement does not provide actions: %q", announcement)
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
	escape := randomDuckEscape()
	if !strings.Contains(escape, "\x03") {
		t.Fatal("expected mIRC color formatting in Duck Hunt escape")
	}
	if !strings.Contains(escape, "[Duck Hunt]") {
		t.Fatal("expected Duck Hunt label in escape")
	}
	if strings.Contains(escape, "\n") || strings.Contains(escape, "\r") {
		t.Fatal("escape must remain one IRC message")
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
