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

func TestFormatReminderDuration(t *testing.T) {
	if got := formatReminderDuration(30 * time.Second); got != "30 seconds" {
		t.Fatalf("got %q", got)
	}
	if got := formatReminderDuration(2 * time.Hour); got != "2 hours" {
		t.Fatalf("got %q", got)
	}
}

func TestReminderStorageFailureDoesNotClaimSuccess(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "reminder.db"))
	if err != nil {
		t.Fatal(err)
	}
	p := &Reminder{}
	p.Init(nil, db)
	db.Close()
	sent := make(chan string, 2)
	b := &bot.Bot{Config: bot.Config{CommandPrefix: "!"}, Queue: bot.NewQueue(1, 1, func(message bot.Outgoing) { sent <- message.Text })}
	if !p.Handle(b, bot.Message{Nick: "tester", Target: "Echo", Text: "!remind 30m check logs"}) {
		t.Fatal("reminder command was not handled")
	}
	b.Queue.Drain(context.Background())
	if reply := <-sent; !strings.Contains(reply, "could not save") || len(p.items) != 0 {
		t.Fatalf("failed storage still scheduled/confirmed reminder: %q", reply)
	}
}
