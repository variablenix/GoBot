package plugins

import (
	"math/big"
	"os"
	"path/filepath"
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

func TestReadPuzzleClues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clues.txt")
	content := "# ignored\nWhat is 2 + 2?|4; four | FOUR\nmalformed\n|missing prompt\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	clues := readPuzzleClues(path)
	if len(clues) != 1 {
		t.Fatalf("got %d clues, want 1", len(clues))
	}
	if clues[0].Prompt != "What is 2 + 2?" {
		t.Fatalf("prompt = %q", clues[0].Prompt)
	}
	if !puzzleTextAnswerMatches(" four ", clues[0].Answers) {
		t.Fatalf("answers = %#v", clues[0].Answers)
	}
}

func TestPuzzleTextAnswerNormalization(t *testing.T) {
	if got := normalizePuzzleTextAnswer("  Thirty-two!\r\n"); got != "thirty two" {
		t.Fatalf("normalized answer = %q", got)
	}
	if !puzzleTextAnswerMatches("  KEYBOARD!", []string{"keyboard"}) {
		t.Fatal("expected normalized answer to match")
	}
	if puzzleTextAnswerMatches("wrong", []string{"keyboard"}) {
		t.Fatal("incorrect answer matched")
	}
}

func TestPuzzleFallbacksAndCategorySelection(t *testing.T) {
	clues := map[puzzleCategory][]puzzleClue{}
	addPuzzleFallbacks(clues)
	for _, category := range []puzzleCategory{puzzleTrivia, puzzleWord, puzzleLogic, puzzleCrossword} {
		if len(clues[category]) == 0 {
			t.Fatalf("no fallback clues for %s", category)
		}
	}

	plugin := &Puzzle{
		clues:    clues,
		anagrams: []string{"network"},
		timeout:  time.Minute,
	}
	for _, category := range []puzzleCategory{puzzleTrivia, puzzleWord, puzzleLogic, puzzleAnagram, puzzleCrossword, puzzleNumbers} {
		game := plugin.newGame(category, "#test", time.Now())
		if game.Category != category {
			t.Fatalf("category = %s, want %s", game.Category, category)
		}
		if category == puzzleNumbers {
			if game.Target == 0 || len(game.Numbers) != puzzleNumberCount {
				t.Fatalf("invalid numbers game: %#v", game)
			}
			continue
		}
		if game.Prompt == "" || len(game.Answers) == 0 {
			t.Fatalf("invalid %s game: %#v", category, game)
		}
	}

	plugin.anagrams = []string{"network", "terminal"}
	seenCategories := make(map[puzzleCategory]struct{})
	for i := 0; i < 6; i++ {
		game := plugin.newGame(puzzleRandom, "#variety", time.Now())
		seenCategories[game.Category] = struct{}{}
	}
	if len(seenCategories) != 6 {
		t.Fatalf("random category cycle = %#v, want all six categories", seenCategories)
	}

	first := plugin.newGame(puzzleTrivia, "#repeat", time.Now())
	second := plugin.newGame(puzzleTrivia, "#repeat", time.Now())
	if first.Prompt == second.Prompt {
		t.Fatalf("repeated trivia prompt before catalog exhausted: %q", first.Prompt)
	}
	firstAnagram := plugin.newGame(puzzleAnagram, "#anagrams", time.Now())
	secondAnagram := plugin.newGame(puzzleAnagram, "#anagrams", time.Now())
	if firstAnagram.Answers[0] == secondAnagram.Answers[0] {
		t.Fatalf("repeated anagram before word list exhausted: %q", firstAnagram.Answers[0])
	}
}
