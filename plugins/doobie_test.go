package plugins

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/variablenix/GoBot/bot"
	"go.uber.org/zap"
)

func TestDoobieCommandsAndHelp(t *testing.T) {
	p := &Doobie{}
	if err := p.Init(nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"doobie", "420", "DOOBIE", "420"} {
		if !isDoobieCommand(command) {
			t.Fatalf("expected %q to be a doobie command", command)
		}
	}
	if isDoobieCommand("doobies") {
		t.Fatal("unexpectedly accepted invalid command")
	}
	for _, want := range []string{"!doobie", "!420", "$doobie", "$420"} {
		if !strings.Contains(p.Help(), want) {
			t.Fatalf("help %q does not contain %q", p.Help(), want)
		}
	}
}

func TestDoobieInlineTriggerBoundaries(t *testing.T) {
	for _, text := range []string{
		"Who wants to smoke a $doobie ?",
		"pass the $420 around",
		"$dooblie",
		"please, $DOOBIE!",
	} {
		if !doobieInlineRX.MatchString(text) {
			t.Fatalf("expected inline trigger in %q", text)
		}
	}
	for _, text := range []string{"$doobies", "$4200", "not$doobie", "doobie"} {
		if doobieInlineRX.MatchString(text) {
			t.Fatalf("unexpected inline trigger in %q", text)
		}
	}
}

func TestDoobieCooldownIsPerSender(t *testing.T) {
	p := &Doobie{}
	if err := p.Init(bot.PluginConfig{"cooldown_seconds": 15}, nil); err != nil {
		t.Fatal(err)
	}
	if !p.allow("sender:alice") {
		t.Fatal("first trigger should be allowed")
	}
	if p.allow("sender:alice") {
		t.Fatal("repeat trigger from the same sender should be rate-limited")
	}
	if !p.allow("sender:bob") {
		t.Fatal("a different sender should have an independent cooldown")
	}
}

func TestDoobieLoadsConfiguredClosingQuotes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doobie.txt")
	if err := os.WriteFile(path, []byte("First puff, best puff.\n\nKeep the circle kind.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &Doobie{}
	if err := p.Init(bot.PluginConfig{"quotes_file": path}, nil); err != nil {
		t.Fatal(err)
	}
	if len(p.quotes) != 2 || p.quotes[0] != "First puff, best puff." {
		t.Fatalf("loaded quotes = %#v", p.quotes)
	}
	if got := p.randomClosingLine(); !strings.Contains(got, ircLightGreen) || !strings.Contains(got, ircReset) {
		t.Fatalf("closing line is not light-green IRC text: %q", got)
	}
}

func TestDoobieCatalogIncludesHotboxLineAndPunchlines(t *testing.T) {
	quotes := readQuotes(filepath.Join("..", "quotes", "doobie.txt"))
	if len(quotes) < 15 {
		t.Fatalf("doobie quote catalog has %d entries, want at least 15", len(quotes))
	}
	wanted := "Hotboxing with this doobie 🚗💨"
	for _, quote := range quotes {
		if quote == wanted {
			return
		}
	}
	t.Fatalf("doobie quote catalog is missing %q", wanted)
}

func TestDoobieSequenceSendsFourSingleLineMessages(t *testing.T) {
	sent := make(chan string, len(doobieCountdownSteps)+1)
	cfg := bot.Config{NetworkName: "test", CommandPrefix: "!"}
	b := bot.New(cfg, nil, nil, zap.NewNop())
	// Replace the queue with a fast capture queue so the test does not depend on
	// an IRC connection or the production pacing rate.
	b.Queue = bot.NewQueue(1000, 20, func(message bot.Outgoing) { sent <- message.Text })
	p := &Doobie{}
	if err := p.Init(bot.PluginConfig{"countdown_delay_seconds": 0, "cooldown_seconds": 1}, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		p.Stop(b)
		b.Queue.Drain(context.Background())
	})

	if !p.Handle(b, bot.Message{Nick: "Alice", Target: "#chat", IsChannel: true, Text: "Who wants a $doobie?"}) {
		t.Fatal("inline trigger was not consumed")
	}

	got := make([]string, 0, len(doobieCountdownSteps)+1)
	deadline := time.After(2 * time.Second)
	for len(got) < len(doobieCountdownSteps)+1 {
		select {
		case line := <-sent:
			got = append(got, line)
		case <-deadline:
			t.Fatalf("received %d/%d countdown messages: %q", len(got), len(doobieCountdownSteps)+1, got)
		}
	}
	for i, want := range doobieCountdownSteps {
		if got[i] != want {
			t.Fatalf("message %d = %q, want %q", i, got[i], want)
		}
		if strings.ContainsAny(got[i], "\r\n") {
			t.Fatalf("message %d contains a line break: %q", i, got[i])
		}
	}
	if !strings.Contains(got[0], ircGreen) {
		t.Fatalf("grinding message is not dark-green IRC text: %q", got[0])
	}
	if strings.Contains(got[1], "\x03") || strings.Contains(got[1], ircReset) {
		t.Fatalf("rolling message should be plain text: %q", got[1])
	}
	if !strings.Contains(got[2], ircCyan) {
		t.Fatalf("sparking message is not cyan IRC text: %q", got[2])
	}
	if !strings.Contains(got[len(got)-1], ircLightGreen) || !strings.Contains(got[len(got)-1], ircReset) {
		t.Fatalf("closing message is not light-green IRC text: %q", got[len(got)-1])
	}
}

func TestDoobieStopCancelsActiveCountdown(t *testing.T) {
	p := &Doobie{}
	if err := p.Init(bot.PluginConfig{"countdown_delay_seconds": 60}, nil); err != nil {
		t.Fatal(err)
	}
	b := bot.New(bot.Config{NetworkName: "test", CommandPrefix: "!"}, nil, nil, zap.NewNop())
	p.start(b, "#chat", "test\x00#chat")
	p.mu.Lock()
	if len(p.active) != 1 {
		t.Fatalf("active sequences = %d, want 1", len(p.active))
	}
	p.mu.Unlock()
	p.Stop(b)
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.active) != 0 {
		t.Fatalf("active sequences after stop = %d, want 0", len(p.active))
	}
}
