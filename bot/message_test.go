package bot

import (
	"net"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"gopkg.in/irc.v3"
)

func TestParseMessage(t *testing.T) {
	m := ParseMessage(irc.MustParseMessage("@account=alice :alice!u@h PRIVMSG #test :hello world"))
	if m.Nick != "alice" || m.User != "u" || m.Host != "h" || m.Account != "alice" || m.Target != "#test" || m.Text != "hello world" || !m.IsChannel {
		t.Fatalf("unexpected message: %+v", m)
	}
}

func TestParseMessageUsesServerTimeWhenAvailable(t *testing.T) {
	m := ParseMessage(irc.MustParseMessage("@account=alice;time=2026-08-02T19:00:00.123Z :alice!u@h PRIVMSG #test :hello"))
	want := time.Date(2026, time.August, 2, 19, 0, 0, 123000000, time.UTC)
	if !m.Timestamp.Equal(want) {
		t.Fatalf("timestamp = %s, want %s", m.Timestamp, want)
	}
}

func TestParseMessageFallsBackToLocalTimeForInvalidServerTime(t *testing.T) {
	before := time.Now()
	m := ParseMessage(irc.MustParseMessage("@time=not-a-timestamp :alice!u@h PRIVMSG #test :hello"))
	after := time.Now()
	if m.Timestamp.Before(before) || m.Timestamp.After(after) {
		t.Fatalf("timestamp = %s, want parse time between %s and %s", m.Timestamp, before, after)
	}
}

func TestIsCommandRejectsEmptyPrefix(t *testing.T) {
	m := Message{Target: "#test", IsChannel: true, Text: "ordinary text"}
	if _, _, ok := IsCommand(m, ""); ok {
		t.Fatal("empty command prefix must not classify ordinary text as a command")
	}
}

func TestValidChannelName(t *testing.T) {
	for _, channel := range []string{"#test", "&local"} {
		if !validChannelName(channel) {
			t.Errorf("expected valid channel %q", channel)
		}
	}
	for _, channel := range []string{"test", "#bad channel", "#bad,channel", "#"} {
		if validChannelName(channel) {
			t.Errorf("expected invalid channel %q", channel)
		}
	}
}

func TestIsOwnerRequiresAuthenticatedAccount(t *testing.T) {
	b := &Bot{Config: Config{OwnerAccounts: []string{"Alice"}}}

	if !b.IsOwner(Message{Account: "alice"}) {
		t.Fatal("expected matching authenticated account to be an owner")
	}
	if b.IsOwner(Message{Nick: "alice"}) {
		t.Fatal("nickname alone must not prove ownership")
	}
	if b.IsOwner(Message{Account: "*"}) {
		t.Fatal("unidentified account must not prove ownership")
	}
}

func TestPrivateReloadIsOwnerOnly(t *testing.T) {
	called := 0
	b := &Bot{
		Config:        Config{OwnerAccounts: []string{"alice"}},
		reloadHandler: func(Message) { called++ },
	}

	if !b.handlePrivateReload(Message{Command: "PRIVMSG", Account: "alice", Text: "reload"}) {
		t.Fatal("expected an authenticated owner's private reload to be handled")
	}
	if called != 1 {
		t.Fatalf("expected reload handler once, got %d calls", called)
	}
	if b.handlePrivateReload(Message{Command: "PRIVMSG", Account: "guest", Text: "reload"}) {
		t.Fatal("unauthenticated account must not trigger reload")
	}
	if b.handlePrivateReload(Message{Command: "PRIVMSG", Account: "alice", IsChannel: true, Text: "reload"}) {
		t.Fatal("channel messages must not trigger private reload")
	}
	if b.handlePrivateReload(Message{Command: "PRIVMSG", Account: "alice", Text: "reload now"}) {
		t.Fatal("reload must require an exact command")
	}
	if called != 1 {
		t.Fatalf("unexpected reload handler calls: %d", called)
	}
}

func TestCapabilityRequestIncludesAccountTag(t *testing.T) {
	if got := capabilityRequest("multi-prefix sasl=ANONYMOUS,EXTERNAL,PLAIN account-tag server-time away", "PLAIN"); got != "sasl account-tag server-time" {
		t.Fatalf("capabilityRequest with SASL = %q, want %q", got, "sasl account-tag server-time")
	}
	if got := capabilityRequest("account-tag server-time", ""); got != "account-tag server-time" {
		t.Fatalf("capabilityRequest without SASL = %q, want %q", got, "account-tag server-time")
	}
	if got := capabilityRequest("sasl=PLAIN account-tag", "EXTERNAL"); got != "account-tag" {
		t.Fatalf("capabilityRequest with unsupported SASL mechanism = %q, want account-tag", got)
	}
	if !hasSASLMechanism("sasl=ANONYMOUS,EXTERNAL,PLAIN", "external") {
		t.Fatal("expected EXTERNAL SASL mechanism to be detected")
	}
}

func TestHandleSASLExternalNegotiation(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	client := irc.NewClient(clientConn, irc.ClientConfig{})
	state := &capNegotiation{mechanism: "EXTERNAL"}

	readWrite := make(chan string, 1)
	go func() {
		buf := make([]byte, 128)
		n, _ := serverConn.Read(buf)
		readWrite <- string(buf[:n])
	}()
	handleSASL(client, irc.MustParseMessage(":server CAP * ACK :sasl account-tag"), "", "", "EXTERNAL", state, zap.NewNop())
	if got := strings.TrimSpace(<-readWrite); got != "AUTHENTICATE EXTERNAL" {
		t.Fatalf("CAP ACK response = %q, want AUTHENTICATE EXTERNAL", got)
	}

	readWrite = make(chan string, 1)
	go func() {
		buf := make([]byte, 128)
		n, _ := serverConn.Read(buf)
		readWrite <- string(buf[:n])
	}()
	handleSASL(client, irc.MustParseMessage(":server AUTHENTICATE :+"), "", "", "EXTERNAL", state, zap.NewNop())
	if got := strings.TrimSpace(<-readWrite); got != "AUTHENTICATE +" {
		t.Fatalf("AUTHENTICATE response = %q, want AUTHENTICATE +", got)
	}
}

func TestHandleSASLWaitsForFinalCapabilityListing(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	client := irc.NewClient(clientConn, irc.ClientConfig{})
	state := &capNegotiation{mechanism: "EXTERNAL"}

	serverConn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buf := make([]byte, 128)
	handleSASL(client, irc.MustParseMessage(":server CAP * LS * :account-tag"), "", "", "EXTERNAL", state, zap.NewNop())
	if _, err := serverConn.Read(buf); err == nil {
		t.Fatal("continued CAP LS response triggered a request before the final listing")
	}
	serverConn.SetReadDeadline(time.Time{})

	readWrite := make(chan string, 1)
	go func() {
		readBuf := make([]byte, 128)
		n, _ := serverConn.Read(readBuf)
		readWrite <- string(readBuf[:n])
	}()
	handleSASL(client, irc.MustParseMessage(":server CAP * LS :sasl=EXTERNAL,PLAIN account-tag"), "", "", "EXTERNAL", state, zap.NewNop())
	if got := strings.TrimSpace(<-readWrite); got != "CAP REQ :sasl account-tag" {
		t.Fatalf("final CAP LS response = %q, want CAP REQ :sasl account-tag", got)
	}
}

func TestCertFPEnrollmentWaitsForSASLAndRegistration(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	client := irc.NewClient(clientConn, irc.ClientConfig{})
	state := &capNegotiation{
		certFPEnroll:    true,
		certFingerprint: "b1f669b2237e4f5ec84118a60960c656d02e8d3c50245686b7b7b148f2e84f47",
		nickServName:    "NickServ",
	}

	serverConn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buf := make([]byte, 256)
	if _, err := serverConn.Read(buf); err == nil {
		t.Fatal("enrollment must wait for SASL success and registration")
	}
	serverConn.SetReadDeadline(time.Time{})

	state.saslSucceeded = true
	maybeSendCertFPEnrollment(client, state, zap.NewNop())
	serverConn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, err := serverConn.Read(buf); err == nil {
		t.Fatal("enrollment must wait for IRC registration")
	}
	serverConn.SetReadDeadline(time.Time{})

	state.registered = true
	readWrite := make(chan string, 1)
	go func() {
		readBuf := make([]byte, 256)
		n, _ := serverConn.Read(readBuf)
		readWrite <- string(readBuf[:n])
	}()
	maybeSendCertFPEnrollment(client, state, zap.NewNop())
	if got := strings.TrimSpace(<-readWrite); got != "PRIVMSG NickServ :CERT ADD b1f669b2237e4f5ec84118a60960c656d02e8d3c50245686b7b7b148f2e84f47" {
		t.Fatalf("enrollment command = %q", got)
	}

	serverConn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	maybeSendCertFPEnrollment(client, state, zap.NewNop())
	if _, err := serverConn.Read(buf); err == nil {
		t.Fatal("enrollment command must be sent only once per connection")
	}
}

func TestValidNickServTarget(t *testing.T) {
	for _, target := range []string{"NickServ", "NickServ-Services"} {
		if !validNickServTarget(target) {
			t.Errorf("expected valid NickServ target %q", target)
		}
	}
	for _, target := range []string{"", "Nick Serv", "NickServ\r\nPRIVMSG #ops :oops", ":NickServ", "Nick,Serv"} {
		if validNickServTarget(target) {
			t.Errorf("expected invalid NickServ target %q", target)
		}
	}
}
