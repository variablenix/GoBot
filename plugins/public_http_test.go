package plugins

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/variablenix/GoBot/bot"
)

func TestPublicFetchersShareStrictAddressPolicy(t *testing.T) {
	for _, raw := range []string{"100.64.0.1", "100.100.100.200", "0.1.2.3", "240.0.0.1", "198.18.0.1", "2001:db8::1", "::ffff:127.0.0.1"} {
		ip := netip.MustParseAddr(raw)
		if publicIP(net.ParseIP(raw)) || isPublicIP(ip) || publicInternetIP(ip) {
			t.Fatalf("a user-controlled fetcher still accepts %s", raw)
		}
		if _, err := askPublicDialContext(t.Context(), "tcp", net.JoinHostPort(raw, "443")); err == nil {
			t.Fatal("Ask dialer accepted a special-purpose literal")
		}
		if _, err := safePublicDialContext(t.Context(), "tcp", net.JoinHostPort(raw, "443")); err == nil {
			t.Fatal("shared URL/paste dialer accepted a special-purpose literal")
		}
	}
	if publicIP(nil) {
		t.Fatal("empty address accepted")
	}
	for _, client := range []*http.Client{newAskWebHTTPClient(), newPasteFetchHTTPClient()} {
		transport := client.Transport.(*http.Transport)
		if transport.Proxy != nil || transport.MaxResponseHeaderBytes != 64<<10 {
			t.Fatal("public fetcher lacks proxy/header protections")
		}
		req, _ := http.NewRequest(http.MethodGet, "https://1.1.1.1/", nil)
		if client.CheckRedirect(req, make([]*http.Request, 10)) == nil {
			t.Fatal("public fetcher follows unbounded redirects")
		}
	}
}

func TestURLTitleBoundsWorkersAcrossNetworksAndSnapshotsSettings(t *testing.T) {
	started := make(chan struct{}, 20)
	release := make(chan struct{})
	replies := make(chan string, 20)
	client := &http.Client{Timeout: time.Second, Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-release
		return newPluginResponse(200, "<html><title>Synthetic original title</title></html>"), nil
	})}
	p := &URLTitle{}
	if err := p.Init(nil, nil); err != nil {
		t.Fatal(err)
	}
	p.client = client
	b := &bot.Bot{Config: bot.Config{NetworkName: "alpha"}, Queue: bot.NewQueue(1000, 20, func(out bot.Outgoing) { replies <- out.Text })}
	defer b.Queue.Drain(context.Background())
	for i := 0; i < 20; i++ {
		b.Config.NetworkName = []string{"alpha", "beta"}[i%2]
		p.Handle(b, bot.Message{IsChannel: true, Target: "#test", Text: "https://1.1.1.1/"})
	}
	deadline := time.After(2 * time.Second)
	for i := 0; i < cap(urlTitleSlots); i++ {
		select {
		case <-started:
		case <-deadline:
			t.Fatal("bounded URL workers did not start")
		}
	}
	// The router allows reinitialization only after Handle returns, but old
	// asynchronous workers can still be running and must use captured settings.
	if err := p.Init(bot.PluginConfig{"max_title_length": 1}, nil); err != nil {
		t.Fatal(err)
	}
	close(release)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for len(urlTitleSlots) != 0 {
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatal("URL workers did not release their capacity")
		}
	}
	b.Queue.Drain(context.Background())
	if len(started) != 0 || len(replies) != cap(urlTitleSlots) {
		t.Fatal("URL worker limit exceeded or results were lost")
	}
	for len(replies) > 0 {
		if text := <-replies; !strings.Contains(text, "Synthetic original title") {
			t.Fatal("old URL fetch used reloaded settings")
		}
	}
}
