package plugins

import (
	"testing"

	"github.com/variablenix/GoBot/bot"
)

func TestBanterProbabilityIsBounded(t *testing.T) {
	p := &Banter{}
	if err := p.Init(map[string]interface{}{"probability": 4.0}, nil); err != nil {
		t.Fatal(err)
	}
	if p.probability != 1 {
		t.Fatalf("got probability %v", p.probability)
	}
	if err := p.Init(map[string]interface{}{"probability": -1.0}, nil); err != nil {
		t.Fatal(err)
	}
	if p.probability != 0 {
		t.Fatalf("got probability %v", p.probability)
	}
}

func TestSplitIRCTextKeepsLongQuotesInOrder(t *testing.T) {
	got := splitIRCText("one two three four", 9)
	want := []string{"one two", "three", "four"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunk %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSplitIRCTextDoesNotSplitUTF8(t *testing.T) {
	got := splitIRCText("café déjà vu", 6)
	for _, chunk := range got {
		if len([]byte(chunk)) > 6 {
			t.Fatalf("chunk exceeds byte limit: %q", chunk)
		}
	}
}

func TestBanterIgnoresCommands(t *testing.T) {
	p := &Banter{probability: 1, quotes: []string{"test quote"}}
	b := &bot.Bot{Config: bot.Config{CommandPrefix: "!"}}
	if p.Handle(b, bot.Message{Nick: "ak", Target: "#test", IsChannel: true, Text: "!karma Echo"}) {
		t.Fatal("banter should not consume commands")
	}
}
