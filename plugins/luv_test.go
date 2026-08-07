package plugins

import (
	"strings"
	"testing"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

func TestLuvNickValidation(t *testing.T) {
	for _, nick := range []string{"Alice", "ak[Relay]", "bot_2", "{Echo}"} {
		if !validLuvNick(nick) {
			t.Errorf("validLuvNick(%q) = false, want true", nick)
		}
	}
	for _, nick := range []string{"", "Alice Smith", "@Alice", "Alice\n", strings.Repeat("A", 31)} {
		if validLuvNick(nick) {
			t.Errorf("validLuvNick(%q) = true, want false", nick)
		}
	}
}

func TestLuvAwardsPersistentCaseInsensitivePoints(t *testing.T) {
	db, err := storage.Open(t.TempDir() + "/bot.db")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	plugin := &Luv{db: db}
	first, err := plugin.award("Network", "Alice")
	if err != nil || first != 1 {
		t.Fatalf("first LUV award = %d, %v; want 1", first, err)
	}
	second, err := plugin.award("network", "alice")
	if err != nil || second != 2 {
		t.Fatalf("second LUV award = %d, %v; want 2", second, err)
	}
	otherNetwork, err := plugin.award("other", "alice")
	if err != nil || otherNetwork != 1 {
		t.Fatalf("other-network LUV award = %d, %v; want 1", otherNetwork, err)
	}
}

func TestLuvHelpAndCommand(t *testing.T) {
	plugin := &Luv{}
	if plugin.Name() != "luv" {
		t.Fatalf("plugin name = %q, want luv", plugin.Name())
	}
	if len(plugin.Commands()) != 1 || plugin.Commands()[0] != "luv" {
		t.Fatalf("plugin commands = %v, want [luv]", plugin.Commands())
	}
	if !strings.Contains(plugin.Help(), "!luv <nick>") {
		t.Fatalf("help does not describe !luv: %q", plugin.Help())
	}
}

func TestLuvCommandIsCaseInsensitive(t *testing.T) {
	command, arg, ok := bot.IsCommand(bot.Message{Text: "!LUV Alice", Target: "#chat", IsChannel: true}, "!")
	if !ok || command != "luv" || arg != "Alice" {
		t.Fatalf("parsed LUV command = %q, %q, %v; want luv, Alice, true", command, arg, ok)
	}
}
