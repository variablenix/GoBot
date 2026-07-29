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
