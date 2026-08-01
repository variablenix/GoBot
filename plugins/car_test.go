package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/variablenix/GoBot/bot"
)

func TestCarLoadsDataAndAliases(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cars.txt"), []byte("Toyota Corolla (1966-present)\n\n# comment\nPorsche 911 (1964-present)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var cars Car
	if err := cars.Init(bot.PluginConfig{"data_file": filepath.Join(dir, "cars.txt")}, nil); err != nil {
		t.Fatal(err)
	}
	if len(cars.items) != 2 || !strings.Contains(cars.items[0], "Toyota Corolla") {
		t.Fatalf("unexpected cars: %v", cars.items)
	}
	if got := cars.Commands(); len(got) != 2 || got[0] != "car" || got[1] != "cars" {
		t.Fatalf("unexpected commands: %v", got)
	}
}

func TestCarFallback(t *testing.T) {
	var cars Car
	if err := cars.Init(bot.PluginConfig{"data_file": filepath.Join(t.TempDir(), "missing.txt")}, nil); err != nil {
		t.Fatal(err)
	}
	if len(cars.items) == 0 {
		t.Fatal("expected fallback car entries")
	}
}
