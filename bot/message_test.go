package bot

import (
	"gopkg.in/irc.v3"
	"testing"
)

func TestParseMessage(t *testing.T) {
	m := ParseMessage(irc.MustParseMessage("@account=alice :alice!u@h PRIVMSG #test :hello world"))
	if m.Nick != "alice" || m.User != "u" || m.Host != "h" || m.Account != "alice" || m.Target != "#test" || m.Text != "hello world" || !m.IsChannel {
		t.Fatalf("unexpected message: %+v", m)
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
