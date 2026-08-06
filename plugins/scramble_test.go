package plugins

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
	"go.uber.org/zap"
)

func TestBundledScrambleCatalogIsBroadAndValid(t *testing.T) {
	words := readScrambleWords(filepath.Join("..", "data", "scramble.txt"))
	if len(words) < 150 {
		t.Fatalf("bundled scramble catalog has only %d words", len(words))
	}
	for _, word := range words {
		if !validScrambleWord(word) {
			t.Fatalf("bundled catalog contains invalid word %q", word)
		}
	}
}

func TestScrambleWordChangesTheWord(t *testing.T) {
	for i := 0; i < 20; i++ {
		got := scrambleWord("network")
		if got == "network" || len(got) != len("network") {
			t.Fatalf("scrambleWord = %q, want a different permutation", got)
		}
	}
}

func TestScrambleAnswerAwardsPersistentKarma(t *testing.T) {
	db, err := storage.Open(t.TempDir() + "/bot.db")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	plugin := &Scramble{db: db, games: map[string]scrambleGame{}, timeout: 5 * time.Minute}
	plugin.games["network\x00#chat"] = scrambleGame{Word: "network", Scrambled: "tworkne", StartedAt: time.Now()}
	b := bot.New(bot.Config{NetworkName: "network", CommandPrefix: "!"}, db, nil, zap.NewNop())
	m := bot.Message{Nick: "Alice", Target: "#chat", Text: "network", IsChannel: true}
	if !plugin.answer(b, m) {
		t.Fatal("correct answer was not consumed")
	}
	karma := &Karma{db: db}
	channel, global := karma.readTotals("network", "#chat", "alice")
	if channel != 1 || global != 1 {
		t.Fatalf("karma totals = %d/%d, want 1/1", channel, global)
	}
	raw, err := db.Get(scrambleScoreBucket, duckHuntScoreKey("network", "#chat", "alice"))
	if err != nil {
		t.Fatalf("scramble score not persisted: %v", err)
	}
	var score scrambleScore
	if err := storage.Decode(raw, &score); err != nil || score.Wins != 1 {
		t.Fatalf("scramble score = %+v, %v", score, err)
	}
	if _, exists := plugin.games["network\x00#chat"]; exists {
		t.Fatal("solved scramble remained active")
	}
}
