package bot

import (
	"context"
	"testing"

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
