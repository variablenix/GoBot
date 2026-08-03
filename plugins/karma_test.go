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
	message := formatKarmaUpdates([]karmaUpdate{{key: "echo", delta: 1, globalValue: 4}})
	if !strings.Contains(message, "🆙 Karma boost! echo") {
		t.Fatalf("unexpected karma message: %q", message)
	}
	if !strings.Contains(message, "✨ 🌟 💫") {
		t.Fatalf("expected spaced positive karma emojis: %q", message)
	}
	if !strings.Contains(message, "\x03") {
		t.Fatal("expected IRC color formatting")
	}
}

func TestKarmaUpdateIncludesChannelAndGlobalTotals(t *testing.T) {
	message := formatKarmaUpdates([]karmaUpdate{{key: "knownsyntax", displayKey: "KnownSyntax", delta: 1, channel: "#chat", channelValue: 9, globalValue: 19}})
	if !strings.Contains(message, "KnownSyntax gained 1 karma") || !strings.Contains(message, "(🎯 9 in #chat | 🌐 19 global)") {
		t.Fatalf("message %q does not contain scoped totals", message)
	}
	if strings.Contains(message, "knownsyntax gained") {
		t.Fatalf("karma announcement lost nickname casing: %q", message)
	}
}

func TestKarmaDecorationsKeepEmojiSeparated(t *testing.T) {
	cases := []struct {
		name    string
		updates []karmaUpdate
		want    string
	}{
		{name: "positive", updates: []karmaUpdate{{key: "thing", delta: 1, globalValue: 4}}, want: "✨ 🌟 💫"},
		{name: "negative", updates: []karmaUpdate{{key: "thing", delta: -1, globalValue: -1}}, want: "📉 🌀 💥 😬"},
		{name: "mixed", updates: []karmaUpdate{{key: "thing", delta: 1, globalValue: 1}, {key: "other", delta: -1, globalValue: -1}}, want: "✨ 📊 🔄 🌟"},
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
	message := formatKarmaUpdates([]karmaUpdate{{key: "project", delta: 1, channel: "#chat", channelValue: 25, globalValue: 25}})
	if !strings.Contains(message, "project has reached +25 karma! 🏆") {
		t.Fatalf("missing milestone notice: %q", message)
	}
	if !strings.Contains(message, ircTan) {
		t.Fatalf("expected milestone color formatting: %q", message)
	}

	if message := formatKarmaUpdates([]karmaUpdate{{key: "project", delta: 1, channel: "#chat", channelValue: 26, globalValue: 26}}); strings.Contains(message, "has reached") {
		t.Fatalf("milestone repeated without crossing a threshold: %q", message)
	}
	if message := formatKarmaUpdates([]karmaUpdate{{key: "project", delta: -1, channel: "#chat", channelValue: -25, globalValue: -25}}); strings.Contains(message, "has reached") {
		t.Fatalf("negative karma unexpectedly received a positive milestone: %q", message)
	}
}

func TestKarmaTracksChannelAndGlobalTotals(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "karma.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p := &Karma{}
	if err := p.Init(bot.PluginConfig{}, db); err != nil {
		t.Fatal(err)
	}

	if updates := p.applyTextChanges("primary", "#chat", "project++ project++"); len(updates) != 2 {
		t.Fatalf("expected two channel updates, got %v", updates)
	}
	if updates := p.applyTextChanges("primary", "#other", "project++"); len(updates) != 1 {
		t.Fatalf("expected one second-channel update, got %v", updates)
	}

	channel, global := p.readTotals("primary", "#chat", "project")
	if channel != 2 || global != 3 {
		t.Fatalf("#chat totals = (%d, %d), want (2, 3)", channel, global)
	}
	channel, global = p.readTotals("primary", "#other", "project")
	if channel != 1 || global != 3 {
		t.Fatalf("#other totals = (%d, %d), want (1, 3)", channel, global)
	}
	channel, global = p.readTotals("secondary", "#chat", "project")
	if channel != 0 || global != 3 {
		t.Fatalf("secondary #chat totals = (%d, %d), want (0, 3)", channel, global)
	}
	if updates := p.applyTextChanges("primary", "#chat", "KnownSyntax++"); len(updates) != 1 || updates[0].displayKey != "KnownSyntax" {
		t.Fatalf("expected display nickname to be preserved, got %v", updates)
	}
}

func TestKarmaPreservesLegacyGlobalTotals(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "karma.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p := &Karma{}
	if err := p.Init(bot.PluginConfig{}, db); err != nil {
		t.Fatal(err)
	}
	if _, err := p.change("legacy", 5); err != nil {
		t.Fatal(err)
	}
	channel, global := p.readTotals("primary", "#chat", "legacy")
	if channel != 0 || global != 5 {
		t.Fatalf("legacy totals = (%d, %d), want (0, 5)", channel, global)
	}
	if updates := p.applyTextChanges("primary", "#chat", "legacy++"); len(updates) != 1 {
		t.Fatalf("expected one migrated update, got %v", updates)
	}
	channel, global = p.readTotals("primary", "#chat", "legacy")
	if channel != 1 || global != 6 {
		t.Fatalf("updated legacy totals = (%d, %d), want (1, 6)", channel, global)
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
	if updates := p.applyTextChanges("primary", "#test", "notgo++inside"); len(updates) != 0 {
		t.Fatalf("expected embedded update to be ignored, got %v", updates)
	}
	updates := p.applyTextChanges("primary", "#test", "ouchnet++ ouchnet++ ouchnet--")
	if len(updates) != 3 {
		t.Fatalf("expected three updates, got %v", updates)
	}
	value, err := p.change("ouchnet", 0)
	if err != nil || value != 1 {
		t.Fatalf("expected persisted karma +1, got %d, %v", value, err)
	}
	channel, global := p.readTotals("primary", "#test", "ouchnet")
	if channel != 1 || global != 1 {
		t.Fatalf("scoped totals = (%d, %d), want (1, 1)", channel, global)
	}
}
