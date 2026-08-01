package plugins

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

func TestPoolDefaultsAndCommands(t *testing.T) {
	plugin := &Pool{}
	if err := plugin.Init(nil, nil); err != nil {
		t.Fatal(err)
	}
	if plugin.cfg.GameTTL != poolDefaultGameTTL || plugin.cfg.TurnTTL != poolDefaultTurnTTL || plugin.cfg.ShotChance != poolDefaultShotChance {
		t.Fatalf("unexpected pool defaults: %+v", plugin.cfg)
	}
	commands := strings.Join(plugin.Commands(), " ")
	for _, command := range []string{"pool", "pool8", "8pool", "pool9", "9ball", "9", "shoot", "forfeit", "poolstats"} {
		if !strings.Contains(commands, command) {
			t.Fatalf("command %q missing from %q", command, commands)
		}
	}
	if poolCommand("8ball") || poolCommand("8") {
		t.Fatal("pool must not claim the magic 8-ball commands")
	}
	if !poolCommand("pool8") || !poolCommand("8pool") {
		t.Fatal("pool 8 aliases are not registered correctly")
	}
}

func TestPoolBallSets(t *testing.T) {
	if got := len(newPoolBalls(poolEightBall)); got != 15 {
		t.Fatalf("8-ball set has %d balls, want 15", got)
	}
	if got := len(newPoolBalls(poolNineBall)); got != 9 {
		t.Fatalf("9-ball set has %d balls, want 9", got)
	}
}

func TestPoolTargetRules(t *testing.T) {
	plugin := &Pool{}
	_ = plugin.Init(bot.PluginConfig{"shot_success_percent": 100}, nil)

	nine := &poolGame{Mode: poolNineBall, Turn: 0, Balls: newPoolBalls(poolNineBall)}
	if ok, _ := plugin.legalTarget(nine, 2); ok {
		t.Fatal("9-ball should require the lowest remaining ball")
	}
	if ok, _ := plugin.legalTarget(nine, 1); !ok {
		t.Fatal("1 should be legal on a fresh 9-ball table")
	}

	eight := &poolGame{Mode: poolEightBall, Turn: 0, Balls: newPoolBalls(poolEightBall)}
	if ok, _ := plugin.legalTarget(eight, 8); ok {
		t.Fatal("8-ball should not be legal on an open table")
	}
	eight.Groups[0] = "solids"
	eight.Groups[1] = "stripes"
	if ok, _ := plugin.legalTarget(eight, 9); ok {
		t.Fatal("player should not shoot the opponent's group")
	}
	for ball := 1; ball <= 7; ball++ {
		delete(eight.Balls, ball)
	}
	if ok, _ := plugin.legalTarget(eight, 8); !ok {
		t.Fatal("8-ball should be legal after solids are cleared")
	}
}

func TestPoolChallengeAndAccept(t *testing.T) {
	plugin := &Pool{}
	_ = plugin.Init(nil, nil)
	challenger := bot.Message{Nick: "Alice", IsChannel: true, Target: "#games"}
	if got := plugin.challengeLocked("network\x00#games", challenger, poolEightBall, "Bob"); !strings.Contains(got, "type !pool accept") {
		t.Fatalf("unexpected challenge response: %q", got)
	}
	game := plugin.games["network\x00#games"]
	if game == nil || !game.Pending {
		t.Fatal("expected pending challenge")
	}
	accepted := plugin.acceptLocked(bot.Message{Nick: "Bob"}, "network\x00#games", game)
	if !strings.Contains(accepted, "8-ball started") || game.Pending || len(game.Balls) != 15 {
		t.Fatalf("unexpected accepted game: %q, %+v", accepted, game)
	}
}

func TestPoolStatsPersist(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "pool.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	plugin := &Pool{}
	_ = plugin.Init(nil, db)
	winner := poolPlayer{Key: "nick:alice", Name: "Alice"}
	loser := poolPlayer{Key: "nick:bob", Name: "Bob"}
	plugin.recordResult(poolEightBall, winner, loser)
	plugin.recordResult(poolNineBall, winner, loser)
	if got := plugin.readStats("nick:alice"); got.EightWins != 1 || got.NineWins != 1 {
		t.Fatalf("unexpected winner stats: %+v", got)
	}
	if got := plugin.readStats("nick:bob"); got.EightLosses != 1 || got.NineLosses != 1 {
		t.Fatalf("unexpected loser stats: %+v", got)
	}
}

func TestPoolTurnExpiry(t *testing.T) {
	plugin := &Pool{}
	_ = plugin.Init(nil, nil)
	game := &poolGame{
		Mode:       poolEightBall,
		Players:    [2]poolPlayer{{Name: "Alice", Key: "nick:alice"}, {Name: "Bob", Key: "nick:bob"}},
		Turn:       0,
		LastAction: time.Now().Add(-plugin.cfg.TurnTTL - time.Second),
		Balls:      newPoolBalls(poolEightBall),
	}
	plugin.games["network\x00#games"] = game
	response := plugin.expireLocked("network\x00#games", game)
	if !strings.Contains(response, "Bob wins by timeout") {
		t.Fatalf("unexpected expiry response: %q", response)
	}
	if len(plugin.games) != 0 {
		t.Fatal("expired game was not removed")
	}
}

func TestPoolShotProbabilityBounds(t *testing.T) {
	if !poolShotSucceeds(100) {
		t.Fatal("100 percent shot should succeed")
	}
	if poolShotSucceeds(0) {
		t.Fatal("0 percent shot should fail")
	}
}
