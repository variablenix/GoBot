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
	if foods.aliases["java"] != "coffee" || foods.aliases["dh"] != "" || foods.aliases["de"] != "german" || foods.aliases["soda"] != "soda" {
		t.Fatal("unexpected food aliases")
	}
}

func TestFoodsFallbacksKeepPluginUsable(t *testing.T) {
	var foods Foods
	if err := foods.Init(bot.PluginConfig{"data_dir": filepath.Join(t.TempDir(), "missing")}, nil); err != nil {
		t.Fatal(err)
	}
	for _, category := range []string{
		"beer", "korean", "japanese", "sushi", "ramen", "burrito", "vietnamese",
		"filipino", "french", "spanish", "turkish", "ethiopian", "brazilian",
		"caribbean", "indonesian", "persian", "middle-eastern",
		"german", "british", "irish", "scottish", "welsh", "portuguese", "greek", "polish",
		"ukrainian", "russian", "swedish", "norwegian", "danish", "finnish", "dutch",
		"belgian", "austrian", "swiss", "czech", "hungarian", "romanian", "georgian",
		"moroccan", "nigerian", "south-african", "peruvian", "argentinian", "chilean",
		"colombian", "venezuelan", "cuban", "canadian", "australian", "new-zealand",
		"malaysian", "pakistani", "bangladeshi", "sri-lankan", "nepalese", "drinks", "soda",
		"juice", "water", "smoothie", "milkshake", "lemonade", "mocktail", "energy-drink",
		"sports-drink", "hot-chocolate", "kombucha", "bubble-tea",
	} {
		if len(foods.items[category]) == 0 {
			t.Fatalf("category %q has no fallback items", category)
		}
	}
}

func TestFoodsCommandListIncludesCuisineAliases(t *testing.T) {
	var foods Foods
	commands := foods.Commands()
	joined := strings.Join(commands, " ")
	for _, command := range []string{
		"food", "foods", "beer", "korean", "japanese", "sushi", "ramen", "burrito",
		"vietnamese", "filipino", "french", "spanish", "turkish", "ethiopian",
		"brazilian", "caribbean", "indonesian", "persian", "middle-eastern",
		"german", "british", "irish", "scottish", "welsh", "portuguese", "greek", "polish",
		"ukrainian", "russian", "swedish", "norwegian", "danish", "finnish", "dutch", "belgian",
		"austrian", "swiss", "czech", "hungarian", "romanian", "georgian", "moroccan", "nigerian",
		"south-african", "peruvian", "argentinian", "chilean", "colombian", "venezuelan", "cuban",
		"canadian", "australian", "new-zealand", "malaysian", "pakistani", "bangladeshi", "sri-lankan",
		"nepalese", "drinks", "soda", "juice", "water", "smoothie", "milkshake", "lemonade", "mocktail",
		"energy-drink", "sports-drink", "hot-chocolate", "kombucha", "bubble-tea",
	} {
		if !strings.Contains(joined, command) {
			t.Fatalf("command %q missing from %q", command, joined)
		}
	}
}
