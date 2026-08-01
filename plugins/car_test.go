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

func TestCarCatalogIncludesMajorSegmentsWithoutDuplicates(t *testing.T) {
	items := readFoodList(filepath.Join("..", "data", "cars.txt"))
	if len(items) < 600 {
		t.Fatalf("expected expanded car catalog, got %d entries", len(items))
	}

	wanted := []string{
		"Bugatti Chiron (2016-2024)",
		"Lamborghini Urus (2018-present)",
		"Range Rover", // checked below by prefix because the catalog includes multiple variants.
		"Lucid Gravity (2024-present)",
		"Toyota Sequoia (2000-present)",
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			t.Fatalf("duplicate catalog entry: %q", item)
		}
		seen[item] = struct{}{}
	}
	for _, want := range wanted {
		if want == "Range Rover" {
			found := false
			for item := range seen {
				if strings.HasPrefix(item, "Land Rover Range Rover ") || item == "Land Rover Range Rover (1970-present)" {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("catalog missing %q family", want)
			}
			continue
		}
		if _, ok := seen[want]; !ok {
			t.Fatalf("catalog missing %q", want)
		}
	}
}
