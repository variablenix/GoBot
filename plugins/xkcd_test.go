package plugins

import "testing"

func TestXKCDCommands(t *testing.T) {
	plugin := &XKCD{}
	if len(plugin.Commands()) != 1 || plugin.Commands()[0] != "xkcd" {
		t.Fatalf("unexpected commands: %v", plugin.Commands())
	}
}
