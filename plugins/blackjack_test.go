package plugins

import (
	"strings"
	"testing"
)

func TestBlackjackHandValue(t *testing.T) {
	tests := []struct {
		name string
		hand []blackjackCard
		want int
		soft bool
	}{
		{name: "blackjack", hand: []blackjackCard{{rank: "A"}, {rank: "K"}}, want: 21, soft: true},
		{name: "ace adjusts", hand: []blackjackCard{{rank: "A"}, {rank: "9"}, {rank: "5"}}, want: 15, soft: false},
		{name: "two aces", hand: []blackjackCard{{rank: "A"}, {rank: "A"}, {rank: "9"}}, want: 21, soft: true},
		{name: "face cards", hand: []blackjackCard{{rank: "K"}, {rank: "Q"}, {rank: "J"}}, want: 30, soft: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, soft := blackjackHandValue(tt.hand)
			if got != tt.want || soft != tt.soft {
				t.Fatalf("got %d soft=%v, want %d soft=%v", got, soft, tt.want, tt.soft)
			}
		})
	}
}

func TestBlackjackDeck(t *testing.T) {
	deck := newBlackjackDeck()
	if len(deck) != 52 {
		t.Fatalf("got %d cards, want 52", len(deck))
	}
	seen := make(map[string]bool, len(deck))
	for _, card := range deck {
		key := card.rank + card.suit
		if seen[key] {
			t.Fatalf("duplicate card %q", key)
		}
		seen[key] = true
	}
}

func TestBlackjackShufflePreservesCards(t *testing.T) {
	deck := newBlackjackDeck()
	if err := shuffleBlackjackDeck(deck); err != nil {
		t.Fatal(err)
	}
	if len(deck) != 52 {
		t.Fatalf("got %d cards after shuffle, want 52", len(deck))
	}
}

func TestBlackjackCardUsesReadableUnicodeSuits(t *testing.T) {
	got := formatBlackjackCard(blackjackCard{rank: "A", suit: "spades"})
	if !strings.Contains(got, "[A♠]") {
		t.Fatalf("got %q, want a card face containing [A♠]", got)
	}
	if !strings.Contains(got, "\x0301,00") {
		t.Fatalf("got %q, want a white card background", got)
	}
}

func TestBlackjackFinishedDoesNotEndAfterHitStatus(t *testing.T) {
	status := "Your hand: 8♠, 4♥, 2♦ = 14 | Dealer shows: K♣ 🂠"
	if blackjackFinished(status) {
		t.Fatal("a normal hit status must leave the game active")
	}
	if !blackjackFinished("You win! | You: A♠, K♥ = 21 | Dealer: 10♣, 7♦ = 17") {
		t.Fatal("a completed result must end the game")
	}
	if !strings.Contains(status, "🂠") {
		t.Fatal("test status should include the hidden-card marker")
	}
}
