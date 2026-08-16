package plugins

import "testing"

func TestRPSChoiceTable(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"rock", "rock"}, {"R", "rock"}, {"paper", "paper"}, {"p", "paper"}, {"scissors", "scissors"}, {"S", "scissors"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, ok := parseRPSChoice(test.input)
			if !ok || got != test.want {
				t.Fatalf("parseRPSChoice(%q) = %q, %v; want %q, true", test.input, got, ok, test.want)
			}
		})
	}
}

func TestRPSOutcomeTable(t *testing.T) {
	tests := []struct{ player, computer, want string }{
		{"rock", "scissors", "You win!"}, {"paper", "rock", "You win!"}, {"scissors", "paper", "You win!"},
		{"rock", "paper", "I win!"}, {"paper", "scissors", "I win!"}, {"scissors", "rock", "I win!"},
		{"rock", "rock", "Draw!"},
	}
	for _, test := range tests {
		if got := rpsOutcome(test.player, test.computer); got != test.want {
			t.Errorf("rpsOutcome(%q, %q) = %q, want %q", test.player, test.computer, got, test.want)
		}
	}
}
