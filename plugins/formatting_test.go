package plugins

import (
	"strings"
	"testing"
)

func TestIRCColorUsesStandardControls(t *testing.T) {
	got := ircColor(ircGreen, "win")
	want := "\x0303win\x0f"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func stripPluginIRC(value string) string {
	return strings.NewReplacer(
		ircReset, "",
		ircBold, "",
		ircGreen, "",
		ircRed, "",
		ircTan, "",
		ircCyan, "",
		ircYellow, "",
	).Replace(value)
}
