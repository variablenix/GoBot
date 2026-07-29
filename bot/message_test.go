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
