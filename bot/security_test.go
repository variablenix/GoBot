package bot

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"gopkg.in/irc.v3"
)

func TestOwnerPrivilegesAreNetworkScoped(t *testing.T) {
	cfg := Config{Networks: []NetworkConfig{{Name: "alpha"}, {Name: "beta"}}, OwnerAccounts: []string{"alice", "beta/bob"}}
	for _, test := range []struct {
		network, account string
		want             bool
	}{
		{"alpha", "ALICE", true}, {"beta", "alice", false}, {"beta", "bob", true}, {"alpha", "bob", false}, {"beta", "*", false},
	} {
		cfg.NetworkName = test.network
		b := &Bot{Config: cfg}
		if got := b.IsOwner(Message{Account: test.account, Nick: "alice"}); got != test.want {
			t.Errorf("owner check on %s/%s = %t, want %t", test.network, test.account, got, test.want)
		}
	}
	cfg.NetworkName = "beta"
	called := false
	b := &Bot{Config: cfg, reloadHandler: func(Message) { called = true }}
	if b.handlePrivateReload(Message{Command: "PRIVMSG", Account: "alice", Text: "reload"}) || called {
		t.Fatal("same-named account on a secondary network triggered owner reload")
	}
	cfg.Networks[0] = NetworkConfig{Server: ServerConfig{Host: "irc.example"}}
	cfg.NetworkName = "irc.example"
	if !(&Bot{Config: cfg}).IsOwner(Message{Account: "alice"}) {
		t.Fatal("unnamed primary network lost its legacy owner")
	}
}

func TestCommandIdentityHistoriesAreBoundedAndExpire(t *testing.T) {
	b := &Bot{lastCommands: make(map[string]time.Time), lastWarnings: make(map[string]time.Time)}
	now := time.Now()
	for i := 0; i < maxCommandIdentities; i++ {
		b.lastCommands[fmt.Sprint(i)] = now
	}
	message := Message{Command: "PRIVMSG", Nick: "new-user", Text: "!status"}
	b.Config.CommandPrefix = "!"
	if b.AllowCommand(message) || len(b.lastCommands) > maxCommandIdentities {
		t.Fatal("new identities bypassed the command-history bound")
	}
	for key := range b.lastCommands {
		b.lastCommands[key] = now.Add(-time.Hour)
	}
	if !b.AllowCommand(message) || len(b.lastCommands) != 1 {
		t.Fatal("expired command identities did not free capacity")
	}
	for i := 0; i < maxCommandIdentities; i++ {
		b.lastWarnings[fmt.Sprint(i)] = now
	}
	if b.AllowCommand(message) || len(b.lastWarnings) > maxCommandIdentities {
		t.Fatal("warning history exceeded its bound")
	}
}

func TestFailedIRCWriteCountsAsDroppedNotSent(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	right.Close()
	stats := NewStats()
	b := &Bot{Stats: stats, client: irc.NewClient(left, irc.ClientConfig{})}
	b.networkStats = stats.registerNetwork("test", 0, nil)
	b.sendNow("#test", "synthetic test message")
	if stats.sent.Load() != 0 || stats.dropped.Load() != 1 || b.networkStats.sent.Load() != 0 || b.networkStats.dropped.Load() != 1 {
		t.Fatal("failed IRC write was counted as a successful send")
	}
}

func TestIRCConnectHonorsCancellation(t *testing.T) {
	for _, useTLS := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		b := &Bot{Config: Config{Server: ServerConfig{Host: "example.invalid", Port: 6697, TLS: useTLS, VerifyCert: true}}}
		started := time.Now()
		if err := b.connect(ctx); !errors.Is(err, context.Canceled) || time.Since(started) > time.Second {
			t.Fatalf("IRC connection ignored cancellation: %v", err)
		}
	}
}
