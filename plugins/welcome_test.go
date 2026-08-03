package plugins

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/variablenix/GoBot/bot"
	"go.uber.org/zap"
)

func TestWelcomeDefaults(t *testing.T) {
	plugin := &Welcome{}
	if err := plugin.Init(nil, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if plugin.probability != 0.15 || plugin.cooldown.Seconds() != 120 {
		t.Fatalf("unexpected defaults: probability=%v cooldown=%v", plugin.probability, plugin.cooldown)
	}
	if len(plugin.lines) == 0 {
		t.Fatal("expected fallback welcome lines")
	}
}

func TestWelcomeLoadsConfiguredLines(t *testing.T) {
	path := t.TempDir() + "/welcome.txt"
	if err := os.WriteFile(path, []byte("hello {nick}\n\nkeep it short\n"), 0600); err != nil {
		t.Fatalf("write test catalog: %v", err)
	}
	plugin := &Welcome{}
	if err := plugin.Init(bot.PluginConfig{
		"probability":      1.0,
		"cooldown_seconds": 1,
		"messages_file":    path,
	}, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if len(plugin.lines) != 2 || plugin.lines[0] != "hello {nick}" {
		t.Fatalf("unexpected loaded lines: %#v", plugin.lines)
	}
	if got := strings.ReplaceAll(plugin.lines[0], "{nick}", "Alice"); got != "hello Alice" {
		t.Fatalf("placeholder replacement = %q", got)
	}
}

func TestWelcomeSkipsSelfAndAppliesChannelCooldown(t *testing.T) {
	plugin := &Welcome{}
	if err := plugin.Init(bot.PluginConfig{
		"probability":      1.0,
		"cooldown_seconds": 120,
	}, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	b := bot.New(bot.Config{}, nil, nil, zap.NewNop())
	b.Config.NetworkName = "test"
	b.Config.Identity.Nick = "GoBot"
	join := bot.Message{Command: "JOIN", Target: "#chat", IsChannel: true, Nick: "Alice"}
	if !plugin.HandleEvent(b, join) {
		t.Fatal("expected first join to produce a greeting")
	}
	if plugin.HandleEvent(b, bot.Message{Command: "JOIN", Target: "#chat", IsChannel: true, Nick: "Bob"}) {
		t.Fatal("expected channel cooldown to suppress the next greeting")
	}
	if plugin.HandleEvent(b, bot.Message{Command: "JOIN", Target: "#chat", IsChannel: true, Nick: "GoBot"}) {
		t.Fatal("expected the bot's own join to be ignored")
	}
	b.Queue.Drain(context.Background())
}

func TestWelcomeUsesMIRCColor(t *testing.T) {
	message := ircColor(ircCyan, "hello")
	if !strings.Contains(message, "\x03") {
		t.Fatal("expected standard mIRC color control code")
	}
	if strings.Contains(message, "[Welcome]") {
		t.Fatalf("welcome output should contain only the configured sentence: %q", message)
	}
}
