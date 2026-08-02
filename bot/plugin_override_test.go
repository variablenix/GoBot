package bot

import (
	"context"
	"testing"

	"github.com/variablenix/GoBot/storage"
	"go.uber.org/zap"
)

func TestPluginEnabledForChannelUsesCaseInsensitiveOptOuts(t *testing.T) {
	b := New(Config{PluginOverrides: map[string]map[string]bool{
		"#Noisy": {"Banter": false, "weather": true},
	}}, nil, nil, zap.NewNop())

	if b.PluginEnabledForChannel("banter", "#noisy") {
		t.Fatal("expected banter to be disabled in the overridden channel")
	}
	if !b.PluginEnabledForChannel("weather", "#NOISY") {
		t.Fatal("expected explicitly enabled weather override to remain enabled")
	}
	if !b.PluginEnabledForChannel("banter", "#other") {
		t.Fatal("expected an unlisted channel to keep the plugin enabled")
	}
	b.Queue.Drain(context.Background())
}

func TestReloadPluginOverridesReplacesLiveOverrides(t *testing.T) {
	b := New(Config{PluginOverrides: map[string]map[string]bool{
		"#quiet": {"banter": false},
	}}, nil, nil, zap.NewNop())

	b.ReloadPluginOverrides(map[string]map[string]bool{
		"#quiet": {"duckhunt": false},
	})

	if !b.PluginEnabledForChannel("banter", "#quiet") {
		t.Fatal("expected removed override to restore the global plugin setting")
	}
	if b.PluginEnabledForChannel("duckhunt", "#quiet") {
		t.Fatal("expected reloaded override to disable duckhunt")
	}
	b.Queue.Drain(context.Background())
}

func TestReloadPluginOverridesCopiesInput(t *testing.T) {
	overrides := map[string]map[string]bool{
		"#quiet": {"banter": false},
	}
	b := New(Config{}, nil, nil, zap.NewNop())
	b.ReloadPluginOverrides(overrides)
	overrides["#quiet"]["banter"] = true

	if b.PluginEnabledForChannel("banter", "#quiet") {
		t.Fatal("expected live overrides to be independent of the input map")
	}
	b.Queue.Drain(context.Background())
}

type reloadTogglePlugin struct {
	initCalls   int
	handleCalls int
}

func (p *reloadTogglePlugin) Name() string                         { return "choose" }
func (p *reloadTogglePlugin) Commands() []string                   { return []string{"choose"} }
func (p *reloadTogglePlugin) Help() string                         { return "choose" }
func (p *reloadTogglePlugin) Init(PluginConfig, *storage.DB) error { p.initCalls++; return nil }
func (p *reloadTogglePlugin) Handle(*Bot, Message) bool            { p.handleCalls++; return true }

func TestReloadPluginsTogglesGlobalEnablement(t *testing.T) {
	plugin := &reloadTogglePlugin{}
	b := New(Config{}, nil, []Plugin{plugin}, zap.NewNop())
	b.SetPluginEnabled("choose", true)

	b.dispatch(Message{Command: "PRIVMSG", Target: "#test", Text: "!choose a | b", IsChannel: true})
	if plugin.handleCalls != 1 {
		t.Fatalf("expected enabled plugin to handle message once, got %d", plugin.handleCalls)
	}

	if _, err := b.ReloadPlugins(map[string]PluginConfig{"choose": {"enabled": false}}); err != nil {
		t.Fatalf("disable reload failed: %v", err)
	}
	b.dispatch(Message{Command: "PRIVMSG", Target: "#test", Text: "!choose a | b", IsChannel: true})
	if plugin.handleCalls != 1 {
		t.Fatalf("disabled plugin handled message; calls=%d", plugin.handleCalls)
	}

	if _, err := b.ReloadPlugins(map[string]PluginConfig{"choose": {"enabled": true}}); err != nil {
		t.Fatalf("enable reload failed: %v", err)
	}
	b.dispatch(Message{Command: "PRIVMSG", Target: "#test", Text: "!choose a | b", IsChannel: true})
	if plugin.handleCalls != 2 || plugin.initCalls != 1 {
		t.Fatalf("expected re-enabled plugin to initialize and handle once, init=%d handles=%d", plugin.initCalls, plugin.handleCalls)
	}
	b.Queue.Drain(context.Background())
}
