package plugins

import (
	"strings"
	"testing"
)

func TestTriviaFallbackBankTable(t *testing.T) {
	if len(triviaFallbacks) < 10 {
		t.Fatalf("fallback bank has %d questions, want at least ten", len(triviaFallbacks))
	}
	for _, question := range triviaFallbacks {
		if len(question.Options) != 4 || question.Correct < 0 || question.Correct >= len(question.Options) {
			t.Fatalf("invalid fallback question: %+v", question)
		}
	}
}

func TestTriviaQuestionKeepsAllOptionsWithinBound(t *testing.T) {
	question := triviaQuestion{Category: "Test", Text: strings.Repeat("long ", 100), Options: []string{"one", "two", "three", "four"}}
	got := formatTriviaQuestion(question, 220)
	for _, option := range []string{"A)", "B)", "C)", "D)"} {
		if !strings.Contains(got, option) {
			t.Fatalf("formatted question %q omitted %s", got, option)
		}
	}
	if len([]rune(got)) > 220 {
		t.Fatalf("formatted question has %d runes, want at most 220", len([]rune(got)))
	}
}

func TestValidTriviaQuestionRejectsMalformedOptions(t *testing.T) {
	if validTriviaQuestion("Science", "Question", []string{"one", "two", "two", "four"}) {
		t.Fatal("duplicate trivia options were accepted")
	}
	if validTriviaQuestion("Science", "Question", []string{"one", "two", "three"}) {
		t.Fatal("wrong option count was accepted")
	}
}
