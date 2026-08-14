package plugins

import (
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestEvaluatePuzzleExpression(t *testing.T) {
	attempt, ok := evaluatePuzzleExpression("50*(9-6)-8-8-1", []int{50, 9, 8, 8, 1, 6}, 131)
	if !ok {
		t.Fatal("valid numbers expression was rejected")
	}
	if attempt.Value.Cmp(big.NewRat(133, 1)) != 0 || attempt.Distance.Cmp(big.NewRat(2, 1)) != 0 {
		t.Fatalf("attempt = value %s, distance %s; want 133 and 2", attempt.Value, attempt.Distance)
	}
	if attempt.Expression != "50*(9-6)-8-8-1" {
		t.Fatalf("normalized expression = %q", attempt.Expression)
	}
}

func TestEvaluatePuzzleExpressionRejectsInvalidInput(t *testing.T) {
	numbers := []int{50, 9, 8, 8, 1, 6}
	for _, expression := range []string{
		"50+9+8+8+8", // one too many 8s
		"50+7",       // 7 was not supplied
		"50/(9-9)",   // division by zero
		"50++9",      // unary operators are not allowed
		"50+os.Exit(1)",
	} {
		if _, ok := evaluatePuzzleExpression(expression, numbers, 100); ok {
			t.Errorf("invalid expression accepted: %q", expression)
		}
	}
}

func TestPuzzleNumbersAndTargetAreBounded(t *testing.T) {
	for i := 0; i < 100; i++ {
		round := newPuzzleGame("#test", time.Now(), 45*time.Second)
		if round.Target < puzzleMinTarget || round.Target > puzzleMaxTarget {
			t.Fatalf("target %d outside configured range", round.Target)
		}
		if len(round.Numbers) != puzzleNumberCount {
			t.Fatalf("got %d numbers, want %d", len(round.Numbers), puzzleNumberCount)
		}
		for _, number := range round.Numbers {
			if number < 1 || number > 100 {
				t.Fatalf("generated number %d outside range", number)
			}
		}
	}
}

func TestPuzzleMessagesDescribeTheRound(t *testing.T) {
	round := &puzzleGame{Target: 131, Numbers: []int{50, 9, 8, 8, 1, 6}}
	message := formatPuzzleStart(round, 45)
	for _, want := range []string{"⏱", "Numbers", "131", "50 9 8 8 1 6", "45 seconds"} {
		if !strings.Contains(message, want) {
			t.Errorf("start message %q does not contain %q", message, want)
		}
	}
}

func TestPuzzleExactMatchFinishesTheRound(t *testing.T) {
	plugin := &Puzzle{games: map[string]*puzzleGame{}}
	game := &puzzleGame{
		Target:    131,
		TargetIRC: "#test",
		Numbers:   []int{50, 9, 8, 8, 1, 6},
	}
	plugin.games["network\x00#test"] = game
	game.Best = &puzzleAttempt{
		Nick:       "Alice",
		Expression: "50*(9-6)-8-8-1-2+0",
		Value:      big.NewRat(131, 1),
		Distance:   big.NewRat(0, 1),
	}
	response := plugin.finishLocked("network\x00#test", game)
	if !strings.Contains(response, "Time's up") || !strings.Contains(response, "Alice") || len(plugin.games) != 0 {
		t.Fatalf("finish response = %q, games = %d", response, len(plugin.games))
	}
}

func TestPuzzleCommandAliases(t *testing.T) {
	for _, command := range []string{"puzzle", "puzzles", "PUZZLE"} {
		if !isPuzzleCommand(command) {
			t.Fatalf("expected %q to be a puzzle command", command)
		}
	}
	if isPuzzleCommand("scramble") {
		t.Fatal("scramble incorrectly recognized as a puzzle command")
	}
}
