package plugins

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

func TestGrabCommands(t *testing.T) {
	p := &Grab{}
	if got := p.Name(); got != "grab" {
		t.Fatalf("Name() = %q", got)
	}
	commands := strings.Join(p.Commands(), " ")
	for _, want := range []string{"grab", "lgrab", "grabr", "grabs"} {
		if !strings.Contains(commands, want) {
			t.Fatalf("command %q missing from %q", want, commands)
		}
	}
}

func TestGrabPersistsAndLimitsQuotes(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	p := &Grab{}
	if err := p.Init(bot.PluginConfig{"max_quotes_per_user": 2}, db); err != nil {
		t.Fatal(err)
	}
	key := grabKey("network", "#channel", "alice")
	for i := 1; i <= 3; i++ {
		p.mu.Lock()
		quotes := append(p.load(key), grabbedLine{Nick: "Alice", Text: string(rune('0' + i)), At: time.Now()})
		if len(quotes) > p.maxQuotesPerUser {
			quotes = quotes[len(quotes)-p.maxQuotesPerUser:]
		}
		if err := p.db.Set(grabBucket, key, quotes); err != nil {
			p.mu.Unlock()
			t.Fatal(err)
		}
		p.mu.Unlock()
	}
	quotes := p.quotesFor("network", "#channel", "alice")
	if len(quotes) != 2 || quotes[0].Text != "2" || quotes[1].Text != "3" {
		t.Fatalf("unexpected persisted quotes: %#v", quotes)
	}
}

func TestNormalizeAndFormatGrab(t *testing.T) {
	if got := normalizeGrabText("hello\r\nworld"); got != "helloworld" {
		t.Fatalf("normalized text = %q", got)
	}
	if got := normalizeGrabText("\x01ACTION waves\x01"); got != "* waves" {
		t.Fatalf("normalized action = %q", got)
	}
	formatted := formatGrab("Alice", "hello")
	if formatted != "<A\u200blice> hello" {
		t.Fatalf("formatted grab = %q", formatted)
	}
}
