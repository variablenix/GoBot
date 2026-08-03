package plugins

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

func TestKarmaRegex(t *testing.T) {
	r := regexp.MustCompile(`(?i)(^|[^a-z0-9_-])([a-z0-9_][a-z0-9_-]{0,30})(\+\+|--)`)
	if !r.MatchString("go++") {
		t.Fatal("expected match")
	}
	if r.MatchString("++go") {
		t.Fatal("unexpected match")
	}
	if !r.MatchString("hello-world--") {
		t.Fatal("expected hyphenated match")
	}
	match := r.FindStringSubmatch("ouchnet++")
	if len(match) < 4 || match[2] != "ouchnet" || match[3] != "++" {
		t.Fatalf("unexpected karma match: %#v", match)
	}
}

func TestKarmaUpdateMessageIsColorfulAndCompact(t *testing.T) {
	message := formatKarmaUpdates([]karmaUpdate{{key: "echo", delta: 1, value: 4}})
	if !strings.Contains(message, "Karma boost! echo") {
		t.Fatalf("unexpected karma message: %q", message)
	}
	if !strings.Contains(message, "✨ 🎯 🌟 💫") {
		t.Fatalf("expected spaced positive karma emojis: %q", message)
	}
	if !strings.Contains(message, "\x03") {
		t.Fatal("expected IRC color formatting")
	}
}

func TestKarmaDecorationsKeepEmojiSeparated(t *testing.T) {
	cases := []struct {
		name    string
		updates []karmaUpdate
		want    string
	}{
		{name: "positive", updates: []karmaUpdate{{key: "thing", delta: 1, value: 4}}, want: "✨ 🎯 🌟 💫"},
		{name: "negative", updates: []karmaUpdate{{key: "thing", delta: -1, value: -1}}, want: "📉 🌀 💥 😬"},
		{name: "mixed", updates: []karmaUpdate{{key: "thing", delta: 1, value: 1}, {key: "other", delta: -1, value: -1}}, want: "✨ 📊 🔄 🌟"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if message := formatKarmaUpdates(test.updates); !strings.Contains(message, test.want) {
				t.Fatalf("message %q does not contain spaced decoration %q", message, test.want)
			}
		})
	}
}

func TestKarmaMilestoneDecoration(t *testing.T) {
	message := formatKarmaUpdates([]karmaUpdate{{key: "project", delta: 1, value: 25}})
	if !strings.Contains(message, "project has reached +25 karma! 🏆") {
		t.Fatalf("missing milestone notice: %q", message)
	}
	if !strings.Contains(message, ircTan) {
		t.Fatalf("expected milestone color formatting: %q", message)
	}

	if message := formatKarmaUpdates([]karmaUpdate{{key: "project", delta: 1, value: 26}}); strings.Contains(message, "has reached") {
		t.Fatalf("milestone repeated without crossing a threshold: %q", message)
	}
	if message := formatKarmaUpdates([]karmaUpdate{{key: "project", delta: -1, value: -25}}); strings.Contains(message, "has reached") {
		t.Fatalf("negative karma unexpectedly received a positive milestone: %q", message)
	}
}

func TestKarmaChangesPersist(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "karma.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p := &Karma{}
	if err := p.Init(bot.PluginConfig{}, db); err != nil {
		t.Fatal(err)
	}
	if updates := p.applyTextChanges("notgo++inside"); len(updates) != 0 {
		t.Fatalf("expected embedded update to be ignored, got %v", updates)
	}
	updates := p.applyTextChanges("ouchnet++ ouchnet++ ouchnet--")
	if len(updates) != 3 {
		t.Fatalf("expected three updates, got %v", updates)
	}
	value, err := p.change("ouchnet", 0)
	if err != nil || value != 1 {
		t.Fatalf("expected persisted karma +1, got %d, %v", value, err)
	}
}
