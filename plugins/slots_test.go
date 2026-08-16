package plugins

import "testing"

func TestSlotResultTable(t *testing.T) {
	tests := []struct {
		name  string
		reels []string
		want  string
	}{
		{"jackpot", []string{"🎰", "🎰", "🎰"}, "JACKPOT!"},
		{"sevens", []string{"7️⃣", "7️⃣", "🍒"}, "Lucky sevens!"},
		{"diamonds", []string{"💎", "💎", "🍒"}, "Double diamonds!"},
		{"pair", []string{"🍒", "🍋", "🍒"}, "A pair of 🍒!"},
		{"none", []string{"🍒", "🍋", "🍊"}, "No match. Try again!"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := slotResult(test.reels); got != test.want {
				t.Fatalf("slotResult(%v) = %q, want %q", test.reels, got, test.want)
			}
		})
	}
}
