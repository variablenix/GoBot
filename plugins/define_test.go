package plugins

import "testing"

func TestDefineCommands(t *testing.T) {
	for _, command := range []string{"define", "def", "dictionary"} {
		if !isDefineCommand(command) {
			t.Fatalf("expected %q to be a define command", command)
		}
	}
	if validDefinitionTerm("hello\nworld") || validDefinitionTerm("") {
		t.Fatal("invalid definition terms were accepted")
	}
}
