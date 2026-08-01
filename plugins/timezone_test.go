package plugins

import "testing"

func TestResolveTimezoneAlias(t *testing.T) {
	tests := map[string]string{
		"Seoul":               "Asia/Seoul",
		"Bangkok":             "Asia/Bangkok",
		"New York":            "America/New_York",
		"America/Los_Angeles": "America/Los_Angeles",
	}
	for input, want := range tests {
		if got := resolveTimezoneAlias(input); got != want {
			t.Fatalf("resolveTimezoneAlias(%q) = %q, want %q", input, got, want)
		}
	}
}
