package plugins

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

func TestSplitAndParseNoteCommands(t *testing.T) {
	if action, rest := splitNoteAction("add vegas https://example.com"); action != "add" || rest != "vegas https://example.com" {
		t.Fatalf("splitNoteAction = %q, %q", action, rest)
	}
	name, text, valid := parseNoteAdd("Vegas https://example.com/my-trip")
	if !valid || name != "vegas" || text != "https://example.com/my-trip" {
		t.Fatalf("parseNoteAdd = %q, %q, %v", name, text, valid)
	}
	if _, _, valid := parseNoteAdd("only-name"); valid {
		t.Fatal("parseNoteAdd accepted a note without text")
	}
	for _, name := range []string{"vegas", "project_1", "todo-now"} {
		if !validNoteName(name) {
			t.Errorf("validNoteName(%q) = false", name)
		}
	}
	for _, name := range []string{"two words", "line\nbreak", strings.Repeat("x", maxNoteNameLength+1)} {
		if validNoteName(name) {
			t.Errorf("validNoteName(%q) = true", name)
		}
	}
}

func TestNoteIdentityUsesAccountThenNetworkAndNick(t *testing.T) {
	b := &bot.Bot{Config: bot.Config{NetworkName: "Ouch"}}
	if got := noteIdentity(b, bot.Message{Account: "Alice", Nick: "Different"}); got != "account:alice" {
		t.Fatalf("account identity = %q", got)
	}
	if got := noteIdentity(b, bot.Message{Account: "*", Nick: "Alice"}); got != "nick:ouch\x00alice" {
		t.Fatalf("fallback identity = %q", got)
	}
}

func TestNotesPersistAndExpireInactiveRecords(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	plugin := &Note{}
	if err := plugin.Init(bot.PluginConfig{"expiry_days": 1}, db); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	identity := "account:alice"
	now := time.Now()
	notes := map[string]noteRecord{
		"keep": {Name: "keep", Text: "remember this", CreatedAt: now, UpdatedAt: now, LastAccessedAt: now},
		"old":  {Name: "old", Text: "remove this", CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now.Add(-48 * time.Hour), LastAccessedAt: now.Add(-48 * time.Hour)},
	}
	plugin.mu.Lock()
	if err := plugin.saveLocked(identity, notes); err != nil {
		t.Fatalf("saveLocked returned error: %v", err)
	}
	loaded, err := plugin.loadLocked(identity)
	plugin.mu.Unlock()
	if err != nil {
		t.Fatalf("loadLocked returned error: %v", err)
	}
	if len(loaded) != 1 || loaded["keep"].Text != "remember this" {
		t.Fatalf("loaded notes = %+v, want only keep note", loaded)
	}
	if _, ok := loaded["old"]; ok {
		t.Fatal("expired note was not removed")
	}

	reloaded := &Note{}
	if err := reloaded.Init(bot.PluginConfig{"expiry_days": 1}, db); err != nil {
		t.Fatalf("reloaded Init returned error: %v", err)
	}
	reloaded.mu.Lock()
	persisted, err := reloaded.loadLocked(identity)
	reloaded.mu.Unlock()
	if err != nil || len(persisted) != 1 {
		t.Fatalf("persisted notes = %+v, err=%v; want one note", persisted, err)
	}
}

func TestNoteExpiryCanBeDisabled(t *testing.T) {
	plugin := &Note{}
	if err := plugin.Init(bot.PluginConfig{"expiry_days": 0}, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if plugin.expiry != 0 {
		t.Fatalf("expiry = %s, want disabled", plugin.expiry)
	}
}

func TestNoteHelpDocumentsCommands(t *testing.T) {
	help := (&Note{}).Help()
	for _, want := range []string{"!note add", "!note list", "!note delete", "!note clear"} {
		if !strings.Contains(help, want) {
			t.Errorf("help %q does not contain %q", help, want)
		}
	}
}
