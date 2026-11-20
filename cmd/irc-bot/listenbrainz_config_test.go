package main

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/variablenix/GoBot/bot"
)

func TestListenBrainzConfigMigration(t *testing.T) {
	for _, test := range []struct {
		name, yaml string
		enabled    bool
		timeout    int
	}{
		{"legacy enabled", "plugins:\n  lastfm: {enabled: true, api_key: synthetic-unused-key}", true, 8},
		{"legacy disabled", "plugins:\n  lastfm: {enabled: false}", false, 8},
		{"canonical wins", "plugins:\n  lastfm: {enabled: true}\n  listenbrainz: {enabled: false, timeout_seconds: 12}", false, 12},
		{"canonical enables", "plugins:\n  lastfm: {enabled: false}\n  listenbrainz: {enabled: true}", true, 8},
		{"default enabled", "command_prefix: '!'", true, 8},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := viper.New()
			v.SetConfigType("yaml")
			if err := v.ReadConfig(strings.NewReader(test.yaml)); err != nil {
				t.Fatal(err)
			}
			var cfg bot.Config
			if err := v.Unmarshal(&cfg); err != nil {
				t.Fatal(err)
			}
			migrateListenBrainzConfig(&cfg)
			migrateListenBrainzConfig(&cfg) // safe for repeated reload normalization
			if _, ok := cfg.Plugins["lastfm"]; ok {
				t.Fatal("legacy config still active")
			}
			settings := cfg.Plugins["listenbrainz"]
			if settings.Bool("enabled", true) != test.enabled || settings.Int("timeout_seconds", 8) != test.timeout || settings.String("api_key", "") != "" {
				t.Fatalf("migration lost enable/disable choice or retained a key: %+v", settings)
			}
		})
	}
}

func TestListenBrainzChannelOptOutMigration(t *testing.T) {
	cfg := bot.Config{
		PluginOverrides: map[string]map[string]bool{"#legacy": {"LASTFM": false, "youtube": false}},
		Networks: []bot.NetworkConfig{{PluginOverrides: map[string]map[string]bool{
			"#legacy":   {"lastfm": false},
			"#explicit": {"lastfm": false, "ListenBrainz": true},
			"#nil":      nil,
		}}},
	}
	migrateListenBrainzConfig(&cfg)
	if cfg.PluginOverrides["#legacy"]["listenbrainz"] || cfg.PluginOverrides["#legacy"]["youtube"] {
		t.Fatal("global opt-outs changed")
	}
	for _, overrides := range []map[string]map[string]bool{cfg.PluginOverrides, cfg.Networks[0].PluginOverrides} {
		value, exists := overrides["#legacy"]["listenbrainz"]
		if !exists || value {
			t.Fatal("legacy channel opt-out was not preserved")
		}
		for _, channel := range overrides {
			for key := range channel {
				if strings.EqualFold(key, "lastfm") {
					t.Fatal("legacy override still active")
				}
			}
		}
	}
	if !cfg.Networks[0].PluginOverrides["#explicit"]["ListenBrainz"] {
		t.Fatal("explicit channel setting did not take precedence")
	}
}
