package plugins

import "testing"

func TestIRCColorUsesStandardControls(t *testing.T) {
	got := ircColor(ircGreen, "win")
	want := "\x0303win\x0f"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
