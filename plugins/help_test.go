package plugins

import (
	"strings"
	"testing"
)

func TestHelpOutputCanBeSplitForIRC(t *testing.T) {
	text := strings.Repeat("plugin — command usage | ", 40)
	parts := splitIRCText(text, 350)
	if len(parts) < 2 {
		t.Fatal("expected long help output to be split")
	}
	for _, part := range parts {
		if len([]byte(part)) > 350 {
			t.Fatalf("help part is too long: %d bytes", len([]byte(part)))
		}
	}
}
