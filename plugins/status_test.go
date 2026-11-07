package plugins

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/variablenix/GoBot/bot"
)

func TestStatusCommands(t *testing.T) {
	plugin := &Status{}
	if got := plugin.Commands(); len(got) != 3 || got[0] != "status" || got[1] != "uptime" || got[2] != "ping" {
		t.Fatalf("unexpected commands: %v", got)
	}
	for _, command := range []string{"status", "uptime", "ping"} {
		if !isStatusCommand(command) {
			t.Fatalf("expected %q to be a status command", command)
		}
	}
}

func statusTestReply(t *testing.T, p *Status, command, network string, stats *bot.Stats) string {
	t.Helper()
	sent := make(chan string, 4)
	b := &bot.Bot{Config: bot.Config{CommandPrefix: "!", NetworkName: network}, Stats: stats,
		Queue: bot.NewQueue(1000, 4, func(out bot.Outgoing) {
			if out.Target != "#test" {
				t.Error("status reply sent to the wrong target")
			}
			sent <- out.Text
		})}
	t.Cleanup(func() { b.Queue.Drain(context.Background()) })
	if !p.Handle(b, bot.Message{Command: "PRIVMSG", Nick: "tester", Target: "#test", IsChannel: true, Text: command}) {
		t.Fatal("status command was not handled")
	}
	b.Queue.Drain(context.Background())
	if len(sent) != 1 {
		t.Fatalf("wanted one reply, got %d", len(sent))
	}
	reply := <-sent
	if !utf8.ValidString(reply) || strings.ContainsAny(reply, "\r\n\x00\uFE0F") || len("PRIVMSG #test :"+reply+"\r\n") > 512 {
		t.Fatal("unsafe or oversized IRC reply")
	}
	return stripPluginIRC(reply)
}

func TestStatusTargetCommandsUseWebsiteNotIRCNetwork(t *testing.T) {
	for _, command := range []string{"status", "ping", "uptime"} {
		for _, network := range []string{"alpha", "beta"} {
			for _, target := range []string{"example.org", "https://example.org/health?token=synthetic-secret", "http://example.org/"} {
				t.Run(command+"/"+network+"/"+target, func(t *testing.T) {
					calls := 0
					p := &Status{client: &http.Client{Transport: newPluginRoundTripper(func(req *http.Request) (*http.Response, error) {
						calls++
						if req.URL.Host != "example.org" || req.Method != http.MethodGet || req.Context().Err() != nil {
							t.Fatal("website request did not use the supplied target")
						}
						return newPluginResponse(http.StatusOK, ""), nil
					})}}
					// Target checks must work even when local bot stats are absent.
					reply := statusTestReply(t, p, "!"+command+" "+target, network, nil)
					if calls != 1 || !strings.Contains(reply, "example.org") || !strings.Contains(reply, "HTTP 200 OK") || !strings.Contains(reply, "HTTP response") || strings.Contains(reply, "GoBot:") || strings.Contains(reply, "synthetic-secret") {
						t.Fatalf("target was ignored or sensitive query echoed: %s", reply)
					}
				})
			}
		}
	}
}

func TestStatusNoTargetPreservesLocalBehavior(t *testing.T) {
	p := &Status{client: &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("no-target command made an external request")
		return nil, context.Canceled
	})}}
	for _, command := range []string{"!status", "!ping", "!uptime"} {
		for _, network := range []string{"alpha", "beta"} {
			reply := statusTestReply(t, p, command, network, bot.NewStats())
			if !strings.Contains(reply, "GoBot:") || !strings.Contains(reply, "network "+network) || !strings.Contains(reply, "uptime") {
				t.Fatalf("local status regressed: %s", reply)
			}
		}
	}
	if reply := statusTestReply(t, p, "!status", "alpha", nil); reply != "GoBot status is unavailable" {
		t.Fatalf("missing local stats behavior changed: %s", reply)
	}
}

func TestStatusTargetInvalidNeverFallsBackToBotStats(t *testing.T) {
	p := &Status{client: &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid target reached the network")
		return nil, context.Canceled
	})}}
	for _, target := range []string{"127.0.0.1", "http://localhost", "example.org other.example", "http://user:secret@example.org/", "example.org:22"} {
		reply := statusTestReply(t, p, "!status "+target, "alpha", bot.NewStats())
		if !strings.Contains(reply, "public hostname") || strings.Contains(reply, "GoBot:") || strings.Contains(reply, "secret") {
			t.Fatalf("invalid target silently became local status: %s", reply)
		}
	}
}

func TestStatusBusyDoesNotBlockLocalStatus(t *testing.T) {
	for i := 0; i < cap(statusCheckSlots); i++ {
		statusCheckSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < cap(statusCheckSlots); i++ {
			<-statusCheckSlots
		}
	})
	if reply := statusTestReply(t, &Status{}, "!ping example.org", "alpha", nil); !strings.Contains(reply, "busy") {
		t.Fatalf("saturated checks were not bounded: %s", reply)
	}
	if reply := statusTestReply(t, &Status{}, "!status", "alpha", bot.NewStats()); !strings.Contains(reply, "GoBot:") {
		t.Fatalf("website saturation blocked local status: %s", reply)
	}
}

func TestStatusHelpAndTimeout(t *testing.T) {
	for _, seconds := range []int{-1, 0, 1, 6, 15, 16} {
		p := &Status{}
		if err := p.Init(bot.PluginConfig{"timeout_seconds": seconds}, nil); err != nil {
			t.Fatal(err)
		}
		want := seconds
		if want < 1 || want > 15 {
			want = 6
		}
		if p.timeout != time.Duration(want)*time.Second || p.client.Timeout != p.timeout {
			t.Fatal("status timeout was not bounded")
		}
		for _, text := range []string{"host", "URL", "HTTP", "without a target", "!uptime", "!ping", "not ICMP"} {
			if !strings.Contains(p.Help(), text) {
				t.Fatalf("help does not explain %q", text)
			}
		}
	}
}
