package plugins

import (
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
	if plugin.cfg.minDelay != time.Minute || plugin.cfg.maxDelay != 5*time.Minute || plugin.cfg.timeout != 30*time.Second {
		t.Fatalf("unexpected timing defaults: %+v", plugin.cfg)
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
	if got := plugin.incrementScore("network", "#test", "alice"); got != 2 {
		t.Fatalf("second score = %d, want 2", got)
	}
	if got := plugin.score("network", "#other", "Alice"); got != 0 {
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
	if got := reloaded.score("network", "#test", "alice"); got != 0 {
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
	if got := reloaded.score("network", "#TEST", "alice"); got != 1 {
		t.Fatalf("reopened score = %d, want 1", got)
	}
}
