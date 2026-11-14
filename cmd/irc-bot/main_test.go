package main

import (
	"testing"

	"github.com/variablenix/GoBot/bot"
)

func TestValidateNetworkNamesRejectsSharedNamespaces(t *testing.T) {
	for _, networks := range [][]bot.NetworkConfig{
		{{Name: "example"}, {Name: " EXAMPLE "}},
		{{Server: bot.ServerConfig{Host: "irc.example"}}, {Name: "IRC.EXAMPLE"}},
		{{}, {Name: "network-1"}},
		{{Name: " "}},
	} {
		if err := validateNetworkNames(networks); err == nil {
			t.Errorf("accepted ambiguous network configuration: %+v", networks)
		}
	}
	if err := validateNetworkNames([]bot.NetworkConfig{{Name: "primary"}, {Name: "secondary"}}); err != nil {
		t.Fatalf("distinct network names rejected: %v", err)
	}
}
