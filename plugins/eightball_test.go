package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEightBallResponses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "responses.txt")
	if err := os.WriteFile(path, []byte("green|Yes\n// comment\nyellow|Maybe\nred|No\nplain\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got := loadEightBallResponses(path)
	if len(got) != 4 || got[0].Color != ircGreen || got[1].Text != "Maybe" || got[3].Color != "" {
		t.Fatalf("unexpected responses: %#v", got)
	}
}

func TestEightBallCommands(t *testing.T) {
	if got := (&EightBall{}).Commands(); len(got) != 3 {
		t.Fatalf("unexpected commands: %v", got)
	}
}
