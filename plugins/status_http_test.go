package plugins

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/variablenix/GoBot/bot"
)

func TestStatusTargetParsing(t *testing.T) {
	for raw, want := range map[string]string{
		"example.org": "https://example.org", "https://EXAMPLE.org/health?q=1#section": "https://example.org/health?q=1",
		"http://example.org:80/a": "http://example.org:80/a", "example.org.:443": "https://example.org:443",
		"8.8.8.8": "https://8.8.8.8", "[2606:4700:4700::1111]": "https://[2606:4700:4700::1111]",
		"https://bücher.de/": "https://xn--bcher-kva.de/",
	} {
		parsed, err := parseStatusTarget(raw)
		if err != nil || parsed.String() != want {
			t.Errorf("parseStatusTarget(%q) = %v, %v; want %s", raw, parsed, err, want)
		}
	}
	for _, raw := range []string{
		"", "localhost", "http://localhost./", "host.local", "host.internal", "home.arpa", "host.home.arpa",
		"http://user:secret@example.org", "ftp://example.org", "file:///etc/passwd", "https://example.org:22", "example.org:",
		"https://example.org other.org", "https://example.org/\r\nInjected: yes", "https://example.org/\x00", "https://example.org/%zz",
		"https://-invalid.example", "https://invalid_.example", "https://example..org", "https://" + strings.Repeat("a", 64) + ".org",
		"2130706433", "127.1", "0177.0.0.1", "0x7f000001", "http://[fe80::1%25eth0]/", strings.Repeat("a", 2049),
	} {
		if _, err := parseStatusTarget(raw); err == nil {
			t.Errorf("unsafe target accepted: %q", raw)
		}
	}
}

func TestStatusPublicIP(t *testing.T) {
	for _, raw := range []string{
		"0.0.0.0", "0.1.2.3", "127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254",
		"100.64.0.1", "100.100.100.200", "192.0.0.1", "192.0.2.1", "192.88.99.1", "198.18.0.1", "198.51.100.1", "203.0.113.1",
		"224.0.0.1", "240.0.0.1", "255.255.255.255", "::", "::1", "fc00::1", "fe80::1", "ff02::1", "::ffff:127.0.0.1",
		"::ffff:10.0.0.1", "64:ff9b::a00:1", "2001::1", "2001:db8::1", "2002:7f00:1::1", "3fff::1",
	} {
		ip := netip.MustParseAddr(raw)
		if publicInternetIP(ip) {
			t.Errorf("non-public address accepted: %s", raw)
		}
		if _, err := parseStatusTarget("https://" + net.JoinHostPort(raw, "443")); err == nil {
			t.Errorf("non-public literal accepted: %s", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111", "::ffff:8.8.8.8"} {
		if !publicInternetIP(netip.MustParseAddr(raw)) {
			t.Errorf("public address rejected: %s", raw)
		}
	}
}

func TestStatusDNSRejectsAllMixedAndPrivateAnswersBeforeDial(t *testing.T) {
	for _, addresses := range [][]netip.Addr{
		{netip.MustParseAddr("127.0.0.1")}, {netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("10.0.0.1")},
		{netip.MustParseAddr("::ffff:192.168.1.1")}, {netip.MustParseAddr("100.100.100.200")},
	} {
		_, err := dialStatusPublic(t.Context(), "tcp", "example.org:443", func(context.Context, string, string) ([]netip.Addr, error) {
			return addresses, nil
		}, func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("private or mixed DNS answer was dialed")
			return nil, nil
		})
		if !errors.Is(err, errStatusTargetBlocked) {
			t.Fatalf("private DNS answer was not blocked: %v", err)
		}
	}
}

func TestStatusDNSPinnedAndPublicAddressFailover(t *testing.T) {
	lookups, dials := 0, 0
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	conn, err := dialStatusPublic(t.Context(), "tcp", "example.org:443", func(context.Context, string, string) ([]netip.Addr, error) {
		lookups++
		if lookups > 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("8.8.8.8")}, nil
	}, func(_ context.Context, network, address string) (net.Conn, error) {
		dials++
		if network != "tcp" || address != []string{"1.1.1.1:443", "8.8.8.8:443"}[dials-1] {
			t.Fatalf("connection not pinned to a validated address: %s", address)
		}
		if dials == 1 {
			return nil, errors.New("synthetic connection failure")
		}
		return left, nil
	})
	if err != nil || conn != left || lookups != 1 || dials != 2 {
		t.Fatalf("public DNS pinning/failover failed: lookups=%d dials=%d err=%v", lookups, dials, err)
	}
}

func TestStatusClientProxyTLSAndRedirectPolicy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9999")
	client := newStatusHTTPClient(time.Second)
	transport := client.Transport.(*http.Transport)
	if transport.Proxy != nil || transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify || !transport.DisableKeepAlives || transport.MaxResponseHeaderBytes != 64<<10 {
		t.Fatal("website client bypasses protections or resource bounds")
	}
	for _, destination := range []string{"http://127.0.0.1/", "https://[::ffff:10.0.0.1]/", "https://example.org:22/", "http://user:secret@example.org/"} {
		calls := 0
		copy := *client
		copy.Transport = newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{destination}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		})
		target, _ := parseStatusTarget("example.org")
		_, err := checkStatusSite(t.Context(), &copy, target)
		if !errors.Is(err, errStatusTargetBlocked) || calls != 1 {
			t.Fatalf("unsafe redirect followed: calls=%d err=%v", calls, err)
		}
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.org/", nil)
	if !errors.Is(client.CheckRedirect(request, make([]*http.Request, 5)), errStatusRedirectLimit) {
		t.Fatal("redirect loop was not bounded")
	}
}

type statusUnreadBody struct{ closed bool }

func (*statusUnreadBody) Read([]byte) (int, error) { panic("website check read an unnecessary body") }
func (b *statusUnreadBody) Close() error           { b.closed = true; return nil }

func TestStatusHTTPResponsesAndBodyBounds(t *testing.T) {
	for _, code := range []int{200, 204, 301, 401, 403, 404, 429, 500, 503} {
		body := &statusUnreadBody{}
		client := &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: code, Header: make(http.Header), Body: body}, nil
		})}
		target, _ := parseStatusTarget("example.org/health?secret=do-not-echo")
		result, err := checkStatusSite(t.Context(), client, target)
		text := stripPluginIRC(formatStatusSite(target, result, err))
		if err != nil || result.code != code || !body.closed || !strings.Contains(text, "reachable") || !strings.Contains(text, http.StatusText(code)) || strings.Contains(text, "do-not-echo") {
			t.Fatalf("HTTP %d was hidden or mislabeled: %s err=%v", code, text, err)
		}
	}
}

func TestStatusTimeoutAndSafeErrors(t *testing.T) {
	client := &http.Client{Transport: newPluginRoundTripper(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	p := &Status{client: client, timeout: 30 * time.Millisecond}
	started := time.Now()
	reply := statusTestReply(t, p, "!ping example.org", "alpha", nil)
	if !strings.Contains(reply, "timed out") || time.Since(started) > time.Second {
		t.Fatalf("slow website check was not bounded: %s", reply)
	}
	for _, test := range []struct {
		err  error
		want string
	}{
		{&net.DNSError{Err: "private resolver diagnostic", Name: "secret.internal"}, "DNS lookup failed"},
		{x509.UnknownAuthorityError{}, "TLS certificate verification failed"},
		{errStatusTargetBlocked, "blocked non-public target or redirect"},
		{errStatusRedirectLimit, "too many redirects"},
		{errors.New("secret internal connection details"), "HTTP connection failed"},
	} {
		if got := statusSiteError(&url.Error{Op: "Get", URL: "https://example.org/?secret", Err: test.err}); got != test.want {
			t.Errorf("unsafe or incorrect error: got %q want %q", got, test.want)
		}
	}
}

func TestStatusLongRedirectReplyIsBounded(t *testing.T) {
	initial, _ := parseStatusTarget("https://example.org/" + strings.Repeat("%E6%97%A5", 120) + "?secret=initial")
	final, _ := parseStatusTarget("https://other.example/" + strings.Repeat("a", 500) + "?secret=final")
	p := &Status{client: &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
		response := newPluginResponse(http.StatusServiceUnavailable, "")
		response.Request = &http.Request{URL: final}
		return response, nil
	})}}
	reply := statusTestReply(t, p, "!status "+initial.String(), "alpha", bot.NewStats())
	if !strings.Contains(reply, "HTTP 503") || !strings.Contains(reply, "redirected to") || strings.Contains(reply, "secret=") {
		t.Fatalf("long redirected response lost its status or leaked query: %s", reply)
	}
}

// Opt-in deployment-host check through the command handlers, without an IRC
// connection, API credentials, or logging target URLs/query parameters.
func TestLiveStatusSites(t *testing.T) {
	if os.Getenv("GOBOT_LIVE_STATUS") != "1" {
		t.Skip("opt-in public website status check")
	}
	targets := []string{"example.org", "https://example.org/", "https://www.wikipedia.org/"}
	if raw := os.Getenv("GOBOT_LIVE_STATUS_TARGETS"); raw != "" {
		if json.Unmarshal([]byte(raw), &targets) != nil || len(targets) == 0 || len(targets) > 8 {
			t.Fatal("invalid live target list")
		}
	}
	p := &Status{}
	if err := p.Init(bot.PluginConfig{}, nil); err != nil {
		t.Fatal(err)
	}
	for i, target := range targets {
		t.Run("target-"+strconv.Itoa(i+1), func(t *testing.T) {
			started := time.Now()
			command := []string{"status", "ping", "uptime"}[i%3]
			reply := statusTestReply(t, p, "!"+command+" "+target, "test-network", nil)
			if !strings.Contains(reply, "reachable | HTTP ") || !strings.Contains(reply, "HTTP response") || strings.Contains(reply, "GoBot:") {
				t.Fatal("website check did not report an HTTP response")
			}
			t.Logf("command=%s returned_http_response=true elapsed_ms=%d", command, time.Since(started).Milliseconds())
		})
	}
}
