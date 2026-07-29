package plugins

import "testing"

func TestFormatChannelStats(t *testing.T) {
	stats := &channelStat{Messages: 3, Users: map[string]uint64{"Alice": 2, "Bob": 1}}
	got := formatChannelStats("#test", stats)
	want := "#test: 3 messages, 2 users; top: Alice 2, Bob 1"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
