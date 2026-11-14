package bot

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
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

func TestIRCRegistrationTimeoutDoesNotReportConnected(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			// Accept the client's registration writes without sending 001.
		}
		serverDone <- scanner.Err()
	}()
	cfg := Config{Server: ServerConfig{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port}, Identity: IdentityConfig{Nick: "echo", User: "echo"}}
	b := New(cfg, nil, nil, zap.NewNop())
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	defer b.Queue.Drain(ctx)
	err = b.connectWithRegistrationTimeout(ctx, 150*time.Millisecond)
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() || ctx.Err() != nil {
		t.Fatalf("expected registration timeout, got %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if b.Stats.connected.Load() != 0 || b.client != nil {
		t.Fatal("unregistered connection was retained")
	}
}

func TestIRCReaderAcceptsTaggedLinesAndRejectsOversizedLine(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	go func() {
		defer right.Close()
		_, _ = fmt.Fprint(right, "@example="+strings.Repeat("x", 8000)+" :server NOTICE echo :hello\r\n"+
			"PING :token\r\n"+strings.Repeat("x", maxIRCWireLine+1))
	}()
	_ = left.SetDeadline(time.Now().Add(3 * time.Second))
	reader := irc.NewReader(&boundedIRCConn{Conn: left})
	for _, want := range []string{"NOTICE", "PING"} {
		message, err := reader.ReadMessage()
		if err != nil || message.Command != want {
			t.Fatalf("wanted %s, got %v (%v)", want, message, err)
		}
	}
	if _, err := reader.ReadMessage(); !errors.Is(err, errIRCLineTooLong) {
		t.Fatalf("wanted size limit error, got %v", err)
	}
}

func TestIRCPasswordsRequireCertificateVerification(t *testing.T) {
	for _, id := range []IdentityConfig{
		{SASLUser: "example", SASLPass: "synthetic-password"},
		{SASLMechanism: "EXTERNAL", SASLUser: "example", SASLPass: "synthetic-password", NickServFallback: true},
	} {
		cfg := Config{Server: ServerConfig{Host: "example.invalid", TLS: true, ClientCert: "unused.pem"}, Identity: id}
		if id.SASLMechanism == "" {
			cfg.Server.ClientCert = ""
		}
		b := &Bot{Config: cfg}
		if err := b.connect(t.Context()); err == nil || !strings.Contains(err.Error(), "without server certificate verification") {
			t.Fatalf("wanted refusal before dialing, got %v", err)
		}
	}
}
