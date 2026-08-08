package plugins

import (
	"io"
	"net/http"
	"strings"
)

type newPluginRoundTripper func(*http.Request) (*http.Response, error)

func (f newPluginRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newPluginResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
