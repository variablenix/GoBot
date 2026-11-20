package main

import (
	"strings"

	"github.com/variablenix/GoBot/bot"
)

// Keep existing installations' enable/disable choices without retaining a
// Last.fm credential or assuming usernames identify the same person elsewhere.
// Called after every configuration load, including live reloads.
func migrateListenBrainzConfig(cfg *bot.Config) {
	if legacy, ok := cfg.Plugins["lastfm"]; ok {
		if _, explicit := cfg.Plugins["listenbrainz"]; !explicit {
			settings := bot.PluginConfig{}
			for _, key := range []string{"enabled", "timeout_seconds"} {
				if value, present := legacy[key]; present {
					settings[key] = value
				}
			}
			cfg.Plugins["listenbrainz"] = settings
		}
		delete(cfg.Plugins, "lastfm")
	}
	migrateListenBrainzOverrides(cfg.PluginOverrides)
	for i := range cfg.Networks {
		migrateListenBrainzOverrides(cfg.Networks[i].PluginOverrides)
	}
}

func migrateListenBrainzOverrides(channels map[string]map[string]bool) {
	for _, overrides := range channels {
		canonical := false
		for name := range overrides {
			if strings.EqualFold(name, "listenbrainz") {
				canonical = true
				break
			}
		}
		for name, enabled := range overrides {
			if strings.EqualFold(name, "lastfm") {
				if !canonical {
					overrides["listenbrainz"] = enabled
				}
				delete(overrides, name)
			}
		}
	}
}
