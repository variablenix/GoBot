package plugins

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

func TestFormatSeenAge(t *testing.T) {
	now := time.Date(2026, time.July, 29, 2, 43, 0, 0, time.UTC)
	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{name: "seconds", at: now.Add(-12 * time.Second), want: "12s"},
		{name: "minute floors", at: now.Add(-1*time.Minute - 59*time.Second), want: "1m"},
		{name: "hours", at: now.Add(-2 * time.Hour), want: "2h"},
		{name: "days", at: now.Add(-3 * 24 * time.Hour), want: "3d"},
		{name: "future timestamp", at: now.Add(time.Minute), want: "0s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatSeenAge(tt.at, now); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSeenDoesNotExposePrivateOrCrossNetworkRecords(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "seen.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sent := make(chan string, 10)
	b := &bot.Bot{Config: bot.Config{CommandPrefix: "!", NetworkName: "first"}, Queue: bot.NewQueue(1, 1, func(message bot.Outgoing) { sent <- message.Text })}
	defer b.Queue.Drain(context.Background())
	p := &Seen{}
	p.Init(nil, db)
	p.Handle(b, bot.Message{Command: "PRIVMSG", Nick: "Alice", Target: "Echo", Text: "private content", Timestamp: time.Now()})
	if _, err := db.Get("seen", seenKey("first", "Echo", "Alice")); err != storage.ErrNotFound {
		t.Fatal("private message was persisted")
	}
	p.Handle(b, bot.Message{Command: "PRIVMSG", Nick: "Alice", Target: "#test", IsChannel: true, Text: "public greeting", Timestamp: time.Now()})
	if !p.Handle(b, bot.Message{Nick: "tester", Target: "#test", IsChannel: true, Text: "!seen Alice"}) {
		t.Fatal("seen command was not handled")
	}
	b.Queue.Drain(context.Background())
	if len(sent) != 1 || !strings.Contains(<-sent, "public greeting") {
		t.Fatal("same-network public record was not returned")
	}
	if _, err := db.Get("seen", seenKey("second", "#test", "Alice")); err != storage.ErrNotFound {
		t.Fatal("network isolation failed")
	}
	// Legacy records cannot distinguish networks or prove a public origin.
	db.Set("seen", "bob", record{Nick: "Bob", Channel: "Echo", Text: "legacy private content"})
	p.Handle(b, bot.Message{Nick: "tester", Target: "#test", IsChannel: true, Text: "!seen Bob"})
	p.Handle(b, bot.Message{Nick: "tester", Target: "#other", IsChannel: true, Text: "!seen Alice"})
	p.Handle(b, bot.Message{Nick: "tester", Target: "Echo", Text: "!seen Alice"})
	b.Config.NetworkName = "second"
	p.Handle(b, bot.Message{Nick: "tester", Target: "#test", IsChannel: true, Text: "!seen Alice"})
	b.Queue.Drain(context.Background())
	if len(sent) != 4 {
		t.Fatalf("expected four lookup replies, got %d", len(sent))
	}
	for len(sent) > 0 {
		if reply := <-sent; strings.Contains(reply, "private content") || strings.Contains(reply, "public greeting") {
			t.Fatalf("private or cross-network data leaked: %q", reply)
		}
	}
}

func TestNormalizeSeenTextForCTCPAction(t *testing.T) {
	tests := []struct {
		name string
		nick string
		text string
		want string
	}{
		{name: "action", nick: "netstat", text: "\x01ACTION wants to spank nsa\x01", want: "netstat wants to spank nsa"},
		{name: "lowercase action", nick: "netstat", text: "\x01action waves\x01", want: "netstat waves"},
		{name: "empty action", nick: "netstat", text: "\x01ACTION\x01", want: "netstat"},
		{name: "ordinary message", nick: "netstat", text: "hello there", want: "hello there"},
		{name: "action prefix is not enough", nick: "netstat", text: "\x01ACTIONable\x01", want: "\x01ACTIONable\x01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeSeenText(tt.nick, tt.text); got != tt.want {
				t.Fatalf("normalizeSeenText(%q, %q) = %q, want %q", tt.nick, tt.text, got, tt.want)
			}
		})
	}
}
