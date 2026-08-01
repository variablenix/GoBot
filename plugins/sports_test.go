package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/variablenix/GoBot/bot"
)

func TestSportsLoadsConfiguredList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sports.txt")
	if err := os.WriteFile(path, []byte("football\n\n# ignored\ncricket\n"), 0600); err != nil {
		t.Fatal(err)
	}

	var sports Sports
	if err := sports.Init(bot.PluginConfig{"data_file": path}, nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sports.items, ","); got != "football,cricket" {
		t.Fatalf("unexpected sports list: %q", got)
	}
}

func TestSportsFallback(t *testing.T) {
	var sports Sports
	if err := sports.Init(bot.PluginConfig{"data_file": filepath.Join(t.TempDir(), "missing.txt")}, nil); err != nil {
		t.Fatal(err)
	}
	if len(sports.items) == 0 {
		t.Fatal("expected fallback sports")
	}
}

func TestSportsCommands(t *testing.T) {
	var sports Sports
	commands := strings.Join(sports.Commands(), " ")
	for _, command := range []string{"sports", "sport"} {
		if !strings.Contains(commands, command) {
			t.Fatalf("command %q missing from %q", command, commands)
		}
	}
}
