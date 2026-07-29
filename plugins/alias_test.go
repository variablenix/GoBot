package plugins

import (
	"strings"
	"testing"
)

func TestFormatAliasGroups(t *testing.T) {
	groups := formatAliasGroups(All())
	for _, want := range []string{"!dice <- !roll", "!wikipedia <- !wiki", "!channelstats <- !chanstats, !stats", "!time <- !tz"} {
		if !strings.Contains(groups, want) {
			t.Fatalf("alias list %q does not contain %q", groups, want)
		}
	}
}
