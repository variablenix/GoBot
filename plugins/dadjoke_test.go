package plugins

import (
	"strings"
	"testing"
)

func TestDadJokeFallbackBankTable(t *testing.T) {
	if len(fallbackDadJokes) < 5 {
		t.Fatalf("fallback bank has %d jokes, want at least five", len(fallbackDadJokes))
	}
	for _, joke := range fallbackDadJokes {
		if strings.TrimSpace(formatDadJokeResponse(joke, 360)) != strings.TrimSpace(joke) {
			t.Errorf("fallback joke was changed: %q", joke)
		}
	}
}

func TestDadJokeResponseSanitizesAndBounds(t *testing.T) {
	got := formatDadJokeResponse("\x0304hello\r\nworld", 8)
	if strings.ContainsAny(got, "\r\n\x03") || len([]rune(got)) > 8 {
		t.Fatalf("unsafe or oversized dad joke response: %q", got)
	}
}
