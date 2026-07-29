package plugins

import (
	"testing"
	"time"
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
