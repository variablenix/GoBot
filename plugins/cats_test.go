package plugins

import "testing"

func TestCatsCommands(t *testing.T) {
	p := &Cats{}
	if got := p.Commands(); len(got) != 2 || got[0] != "cats" || got[1] != "cat" {
		t.Fatalf("unexpected commands: %v", got)
	}
}
