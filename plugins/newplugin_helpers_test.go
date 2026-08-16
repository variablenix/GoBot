package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScopedKeySeparatesIdentityData(t *testing.T) {
	first := scopedKey("network", "#channel", "nick:a\x00b")
	second := scopedKey("network", "#channel", "nick:a")
	third := scopedKey("network", "#channel\x00nick:a", "")
	if first == second || first == third || second == third {
		t.Fatalf("scoped keys collided: %q, %q, %q", first, second, third)
	}
}

func TestReadBoundedRegularFileRejectsOversizedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBoundedRegularFile(path, 16); err == nil {
		t.Fatal("oversized file was accepted")
	}
}
