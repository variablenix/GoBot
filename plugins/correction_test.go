package plugins

import "testing"

func TestParseSubstitution(t *testing.T) {
	old, replacement, global, ok := ParseSubstitution(`s/wiiifee/wife`)
	if !ok || old != "wiiifee" || replacement != "wife" || global {
		t.Fatalf("unexpected substitution: %q %q %v %v", old, replacement, global, ok)
	}
	old, replacement, global, ok = ParseSubstitution(`s/foo\/bar/baz/g`)
	if !ok || old != "foo/bar" || replacement != "baz" || !global {
		t.Fatalf("unexpected escaped substitution: %q %q %v %v", old, replacement, global, ok)
	}
}

func TestParseSubstitutionRejectsInvalidInput(t *testing.T) {
	for _, input := range []string{"s//new", "replace old new", "s/old/new/extra"} {
		if _, _, _, ok := ParseSubstitution(input); ok {
			t.Fatalf("expected %q to be rejected", input)
		}
	}
}
