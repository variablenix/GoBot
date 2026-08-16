package plugins

import "testing"

func TestWordleFeedbackTable(t *testing.T) {
	tests := []struct {
		answer, guess string
		want          []string
	}{
		{"crane", "crane", []string{"[C]", "[R]", "[A]", "[N]", "[E]"}},
		{"crane", "cigar", []string{"[C]", "_", "_", "(a)", "(r)"}},
		{"allee", "eagle", []string{"(e)", "(a)", "_", "(l)", "[E]"}},
	}
	for _, test := range tests {
		got := wordleFeedback(test.answer, test.guess)
		if len(got) != len(test.want) {
			t.Fatalf("wordleFeedback(%q, %q) length = %d, want %d", test.answer, test.guess, len(got), len(test.want))
		}
		for i := range got {
			if got[i] != test.want[i] {
				t.Errorf("wordleFeedback(%q, %q)[%d] = %q, want %q", test.answer, test.guess, i, got[i], test.want[i])
			}
		}
	}
}

func TestWordleAnswerIsDeterministicPerChannel(t *testing.T) {
	words := []string{"crane", "slate", "stare", "plant", "world"}
	if first, second := wordleAnswer("2026-08-16", "#chat", words), wordleAnswer("2026-08-16", "#chat", words); first != second {
		t.Fatalf("same date/channel produced %q and %q", first, second)
	}
	if wordleAnswer("2026-08-16", "#chat", words) == wordleAnswer("2026-08-16", "#other", words) && len(words) > 1 {
		t.Log("different channels may legitimately collide in a small test corpus")
	}
}

func TestWordleFeedbackRejectsMismatchedLengths(t *testing.T) {
	if got := wordleFeedback("crane", "cat"); got != nil {
		t.Fatalf("mismatched feedback = %v, want nil", got)
	}
}

func TestValidWordleStateRejectsCorruptGuesses(t *testing.T) {
	words := map[string]struct{}{"crane": {}, "slate": {}}
	state := wordleState{Date: "2026-08-16", Word: "crane", Guesses: []string{"notaword"}}
	if validWordleState(state, "2026-08-16", words, 6) {
		t.Fatal("corrupt Wordle state was accepted")
	}
}
