package plugins

import (
	"regexp"
	"testing"
)

func TestKarmaRegex(t *testing.T) {
	r := regexp.MustCompile(`(?i)([a-z0-9_][a-z0-9_-]{0,30})(\+\+|--)`)
	if !r.MatchString("go++") {
		t.Fatal("expected match")
	}
	if r.MatchString("++go") {
		t.Fatal("unexpected match")
	}
	if !r.MatchString("hello-world--") {
		t.Fatal("expected hyphenated match")
	}
}
