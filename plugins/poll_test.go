package plugins

import (
	"strings"
	"testing"
)

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

func TestPollInvalidStoredVotesDoNotPanic(t *testing.T) {
	current := &poll{Question: "Example?", Options: []string{"A", "B"}, Votes: map[string]int{"valid": 1, "zero": 0, "large": 999, "negative": -1}}
	if result := formatPoll(current); !strings.Contains(result, "(1 votes)") {
		t.Fatal("expected usable results despite invalid stored votes")
	}
}

func TestPollCanVoteWithMissingStoredVotes(t *testing.T) {
	p := &Poll{active: map[string]*poll{"example": {Question: "Example?", Options: []string{"A", "B"}}}}
	if response := p.vote("example", "tester", "1"); response != "vote recorded for option 1" {
		t.Fatalf("vote failed: %s", response)
	}
}
