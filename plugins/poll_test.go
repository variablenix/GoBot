package plugins

import "testing"

func TestPollCreateAndVote(t *testing.T) {
	p := &Poll{}
	if err := p.Init(nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := p.create("#test", "Lunch? | Pizza | Tacos"); got == "" {
		t.Fatal("expected poll creation response")
	}
	if got := p.vote("#test", "Alice", "2"); got != "vote recorded for option 2" {
		t.Fatalf("got %q", got)
	}
	if got := p.vote("#test", "Alice", "1"); got != "vote recorded for option 1" {
		t.Fatalf("got %q", got)
	}
	if got := p.results("#test"); got == "" {
		t.Fatal("expected poll results")
	}
}

func TestPollRejectsInvalidOptions(t *testing.T) {
	p := &Poll{}
	_ = p.Init(nil, nil)
	if got := p.create("#test", "Question | only"); got == "" {
		t.Fatal("expected usage response")
	}
	if got := p.vote("#missing", "Alice", "1"); got != "no poll is active in this channel" {
		t.Fatalf("got %q", got)
	}
}
