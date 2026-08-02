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
