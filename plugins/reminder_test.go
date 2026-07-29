package plugins

import (
	"testing"
	"time"
)

func TestFormatReminderDuration(t *testing.T) {
	if got := formatReminderDuration(30 * time.Second); got != "30 seconds" {
		t.Fatalf("got %q", got)
	}
	if got := formatReminderDuration(2 * time.Hour); got != "2 hours" {
		t.Fatalf("got %q", got)
	}
}
