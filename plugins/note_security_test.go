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

func noteTestCommand(t *testing.T, p *Note, network, account, command string, channel bool) string {
	t.Helper()
	sent := make(chan bot.Outgoing, 10)
	b := &bot.Bot{Config: bot.Config{NetworkName: network, CommandPrefix: "!"}, Queue: bot.NewQueue(1000, 10, func(out bot.Outgoing) { sent <- out })}
	defer b.Queue.Drain(context.Background())
	m := bot.Message{Command: "PRIVMSG", Nick: "tester", Target: "GoBot", Account: account, Text: command, IsChannel: channel}
	if channel {
		m.Target = "#test"
	}
	if !p.Handle(b, m) {
		t.Fatal("note command not handled")
	}
	b.Queue.Drain(context.Background())
	var replies []string
	for len(sent) > 0 {
		out := <-sent
		if out.Target != "tester" {
			t.Fatal("private note output sent to a channel")
		}
		replies = append(replies, stripPluginIRC(out.Text))
	}
	return strings.Join(replies, " ")
}

func TestNotesRejectGuestAndChannelAccessAndIsolateNetworks(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p := &Note{}
	if err := p.Init(nil, db); err != nil {
		t.Fatal(err)
	}
	if got := noteTestCommand(t, p, "alpha", "alice", "!note add sample synthetic-private-value", false); !strings.Contains(got, "saved") {
		t.Fatal(got)
	}
	for _, account := range []string{"", "*", "bob"} {
		if got := noteTestCommand(t, p, "alpha", account, "!note sample", false); strings.Contains(got, "synthetic-private-value") {
			t.Fatal("guest or another account read private notes")
		}
	}
	if got := noteTestCommand(t, p, "beta", "alice", "!note sample", false); strings.Contains(got, "synthetic-private-value") {
		t.Fatal("same-named account on another network read private notes")
	}
	if got := noteTestCommand(t, p, "alpha", "alice", "!note sample", true); strings.Contains(got, "synthetic-private-value") || strings.Contains(got, "sample:") {
		t.Fatal("channel command revealed note content")
	}
	if got := noteTestCommand(t, p, "alpha", "alice", "!note delete sample", true); !strings.Contains(got, "private message") {
		t.Fatal("channel mutation was not rejected")
	}
	if got := noteTestCommand(t, p, "alpha", "alice", "!note sample", false); !strings.Contains(got, "synthetic-private-value") {
		t.Fatal("legitimate private note was changed or inaccessible")
	}
	if err := p.Init(bot.PluginConfig{"expiry_days": 0}, db); err != nil || p.expiry != 0 {
		t.Fatal("reinitialization did not disable expiry")
	}
}

func TestLegacyNotesPreservedWithoutAutomaticOwnershipClaim(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key := noteStorageKey("account:alice")
	old := time.Now().Add(-365 * 24 * time.Hour)
	if err := db.Set("notes", key, map[string]noteRecord{"legacy": {Name: "legacy", Text: "synthetic-legacy-value", CreatedAt: old}}); err != nil {
		t.Fatal(err)
	}
	p := &Note{}
	if err := p.Init(nil, db); err != nil {
		t.Fatal(err)
	}
	if got := noteTestCommand(t, p, "alpha", "alice", "!note legacy", false); strings.Contains(got, "synthetic-legacy-value") {
		t.Fatal("legacy records were claimed by an unverified network identity")
	}
	if raw, err := db.Get("notes", key); err != nil || !strings.Contains(string(raw), "synthetic-legacy-value") {
		t.Fatal("legacy data was removed or modified")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if got := noteTestCommand(t, p, "alpha", "alice", "!note add sample text", false); !strings.Contains(got, "couldn't save") {
		t.Fatal("storage failure was reported as success")
	}
}
