package plugins

import "testing"

func TestUrbanArgs(t *testing.T) {
	term, index := urbanArgs("go bot 2")
	if term != "go bot" || index != 2 {
		t.Fatalf("got %q/%d, want %q/2", term, index, "go bot")
	}
	term, index = urbanArgs("slang")
	if term != "slang" || index != 1 {
		t.Fatalf("got %q/%d, want slang/1", term, index)
	}
}

func TestUrbanPrefixHasNoEmojiVariationSelector(t *testing.T) {
	if urbanPrefix != "\U0001F5E3" {
		t.Fatalf("urbanPrefix = %q, want only U+1F5E3 without U+FE0F", urbanPrefix)
	}
}

func TestCleanExternalText(t *testing.T) {
	got := cleanExternalText("hello\x02 world\r\nagain")
	if got != "hello worldagain" {
		t.Fatalf("got %q", got)
	}
}
