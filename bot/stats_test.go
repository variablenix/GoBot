package bot

import "testing"

func TestStatsListenAddress(t *testing.T) {
	tests := []struct {
		address string
		want    string
	}{
		{address: "127.0.0.1", want: "127.0.0.1:8080"},
		{address: "127.0.0.1:9090", want: "127.0.0.1:9090"},
		{address: "0.0.0.0", want: "0.0.0.0:8080"},
	}
	for _, tt := range tests {
		if got := statsListenAddress(tt.address, 8080); got != tt.want {
			t.Errorf("address %q: got %q, want %q", tt.address, got, tt.want)
		}
	}
}

func TestMetricsSnapshotIncludesUptimeAndDroppedMessages(t *testing.T) {
	stats := NewStats()
	stats.dropped.Store(3)

	snapshot := stats.MetricsSnapshot()
	if _, ok := snapshot["uptime_seconds"]; !ok {
		t.Fatal("metrics snapshot does not include uptime_seconds")
	}
	if got := snapshot["messages_dropped"]; got != uint64(3) {
		t.Fatalf("messages_dropped: got %v, want 3", got)
	}
	if _, ok := snapshot["uptime"]; ok {
		t.Fatal("metrics snapshot should not include human-readable uptime")
	}
}
