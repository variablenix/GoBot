package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/variablenix/GoBot/bot"
)

func TestFoodsLoadsListsAndAliases(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "beer.txt"), []byte("lager\n\n# comment\nIPA\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var foods Foods
	if err := foods.Init(bot.PluginConfig{"data_dir": dir}, nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(foods.items["beer"], ","); got != "lager,IPA" {
		t.Fatalf("unexpected beer list: %q", got)
	}
	if foods.aliases["java"] != "coffee" || foods.aliases["dh"] != "" {
		t.Fatal("unexpected food aliases")
	}
}

func TestFoodsFallbacksKeepPluginUsable(t *testing.T) {
	var foods Foods
	if err := foods.Init(bot.PluginConfig{"data_dir": filepath.Join(t.TempDir(), "missing")}, nil); err != nil {
		t.Fatal(err)
	}
	for _, category := range []string{"beer", "korean", "japanese", "sushi", "ramen"} {
		if len(foods.items[category]) == 0 {
			t.Fatalf("category %q has no fallback items", category)
		}
	}
}

func TestFoodsCommandListIncludesCuisineAliases(t *testing.T) {
	var foods Foods
	commands := foods.Commands()
	joined := strings.Join(commands, " ")
	for _, command := range []string{"food", "foods", "beer", "korean", "japanese", "sushi", "ramen"} {
		if !strings.Contains(joined, command) {
			t.Fatalf("command %q missing from %q", command, joined)
		}
	}
}
