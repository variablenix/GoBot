package plugins

import "testing"

func TestStatusCommands(t *testing.T) {
	plugin := &Status{}
	if got := plugin.Commands(); len(got) != 3 || got[0] != "status" || got[1] != "uptime" || got[2] != "ping" {
		t.Fatalf("unexpected commands: %v", got)
	}
	for _, command := range []string{"status", "uptime", "ping"} {
		if !isStatusCommand(command) {
			t.Fatalf("expected %q to be a status command", command)
		}
	}
}
