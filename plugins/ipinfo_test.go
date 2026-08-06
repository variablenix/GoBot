package plugins

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type ipRoundTripper func(*http.Request) (*http.Response, error)

func (f ipRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func ipTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestLookupIPParsesNetworkFlags(t *testing.T) {
	oldClient := ipHTTPClient
	oldLastRequest := ipLastRequest
	t.Cleanup(func() {
		ipHTTPClient = oldClient
		ipLastRequest = oldLastRequest
	})
	ipLastRequest = time.Time{}
	ipHTTPClient = &http.Client{Transport: ipRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "ip-api.com" || !strings.HasPrefix(r.URL.Path, "/json/8.8.8.8") {
			t.Fatalf("unexpected IP API request: %s", r.URL.String())
		}
		return ipTestResponse(http.StatusOK, `{"status":"success","query":"8.8.8.8","country":"United States","countryCode":"US","org":"Google LLC","as":"AS15169 Google LLC","reverse":"dns.google","proxy":false,"hosting":true,"mobile":false}`), nil
	})}

	result, err := lookupIP(t.Context(), "8.8.8.8")
	if err != nil {
		t.Fatalf("lookupIP returned error: %v", err)
	}
	message := stripPluginIRC(formatIPResult("ip", result))
	for _, want := range []string{"[IP]", "8.8.8.8", "AS15169 Google LLC", "org: Google LLC", "country: United States", "datacenter/hosting", "rDNS: dns.google"} {
		if !strings.Contains(message, want) {
			t.Errorf("formatted IP result %q does not contain %q", message, want)
		}
	}
}

func TestValidIPQueryRejectsURLLikeInput(t *testing.T) {
	for _, value := range []string{"8.8.8.8", "2001:db8::1", "router.example"} {
		if !validIPQuery(value) {
			t.Errorf("valid IP query rejected: %q", value)
		}
	}
	for _, value := range []string{"", "https://example.com", "8.8.8.8/path", "bad value", "\n8.8.8.8"} {
		if validIPQuery(value) {
			t.Errorf("unsafe IP query accepted: %q", value)
		}
	}
}
