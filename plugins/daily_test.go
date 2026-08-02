package plugins

import (
	"testing"
	"time"

	"github.com/variablenix/GoBot/storage"
)

func TestDailyClaimsPersistAndTrackStreaks(t *testing.T) {
	db, err := storage.Open(t.TempDir() + "/bot.db")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	plugin := &Daily{}
	if err := plugin.Init(nil, db); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	first, already, err := plugin.claim("account:alice", "2026-08-02")
	if err != nil || already || first.Streak != 1 {
		t.Fatalf("first claim = %+v, already=%v, err=%v", first, already, err)
	}
	if _, already, err := plugin.claim("account:alice", "2026-08-02"); err != nil || !already {
		t.Fatalf("same-day claim = already %v, err %v; want already=true", already, err)
	}

	dailyNow = func() time.Time { return time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { dailyNow = time.Now })
	next, already, err := plugin.claim("account:alice", "2026-08-03")
	if err != nil || already || next.Streak != 2 {
		t.Fatalf("next-day claim = %+v, already=%v, err=%v", next, already, err)
	}

	reloaded := &Daily{}
	if err := reloaded.Init(nil, db); err != nil {
		t.Fatalf("reloaded Init returned error: %v", err)
	}
	if _, already, err := reloaded.claim("account:alice", "2026-08-03"); err != nil || !already {
		t.Fatalf("persisted same-day claim = already %v, err %v; want already=true", already, err)
	}
	if _, already, err := reloaded.claim("account:bob", "2026-08-03"); err != nil || already {
		t.Fatalf("different-user claim = already %v, err %v; want already=false", already, err)
	}
}

func TestDailyAwardUsesDuckHuntXP(t *testing.T) {
	db, err := storage.Open(t.TempDir() + "/bot.db")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	duckhunt := &DuckHunt{}
	if err := duckhunt.Init(nil, db); err != nil {
		t.Fatalf("Duck Hunt Init returned error: %v", err)
	}
	if _, err := duckhunt.AwardXP("primary", "#lobby", "Alice", 25); err != nil {
		t.Fatalf("AwardXP returned error: %v", err)
	}
	duckhunt.mu.Lock()
	player := duckhunt.loadPlayerLocked("primary", "#lobby", "alice")
	duckhunt.mu.Unlock()
	if player.XP != 25 {
		t.Fatalf("player XP = %d, want 25", player.XP)
	}
}
