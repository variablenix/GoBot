package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/variablenix/GoBot/bot"
)

func TestWeaponsLoadsCategoriesAndAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "weapons.txt")
	content := "pistol|.22 rimfire pistol\nrifle|sporting rifle\ngrenade|smoke grenade\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	var weapons Weapons
	if err := weapons.Init(bot.PluginConfig{"data_file": path}, nil); err != nil {
		t.Fatal(err)
	}
	if len(weapons.items) != 3 || len(weapons.byCategory["pistol"]) != 1 || len(weapons.byCategory["all"]) != 3 {
		t.Fatalf("unexpected weapon catalog: %#v", weapons)
	}
	for _, command := range []string{"firearm", "guns", "weapons", "pistol", "rifle", "grenade"} {
		if !isWeaponCommand(command) {
			t.Fatalf("expected %q to be a weapon command", command)
		}
	}
	if got := normalizeWeaponCategory("shotguns"); got != "shotgun" {
		t.Fatalf("unexpected shotgun category %q", got)
	}
	if !strings.Contains(weapons.Help(), "!guns") {
		t.Fatal("help should mention the short alias")
	}
}

func TestWeaponsFallback(t *testing.T) {
	var weapons Weapons
	if err := weapons.Init(bot.PluginConfig{"data_file": filepath.Join(t.TempDir(), "missing.txt")}, nil); err != nil {
		t.Fatal(err)
	}
	if len(weapons.items) == 0 || len(weapons.byCategory["all"]) == 0 {
		t.Fatal("expected weapon fallbacks")
	}
}
