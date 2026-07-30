package plugins

import "testing"

func TestTellMessageFormat(t *testing.T) {
	got := formatTellMessage("Echo", "you are awesome")
	if got != "Echo: you are awesome" {
		t.Fatalf("got %q", got)
	}
}
