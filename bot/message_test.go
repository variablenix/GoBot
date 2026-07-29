package bot

import (
	"gopkg.in/irc.v3"
	"testing"
)

func TestParseMessage(t *testing.T) {
	m := ParseMessage(irc.MustParseMessage(":alice!u@h PRIVMSG #test :hello world"))
	if m.Nick != "alice" || m.User != "u" || m.Host != "h" || m.Target != "#test" || m.Text != "hello world" || !m.IsChannel {
		t.Fatalf("unexpected message: %+v", m)
	}
}
