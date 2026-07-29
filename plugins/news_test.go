package plugins

import (
	"net/url"
	"strconv"
	"testing"
)

func TestNewsEndpointRequestsEnglish(t *testing.T) {
	tests := []struct {
		query string
		want  string
	}{
		{query: "", want: "top-headlines"},
		{query: "linux", want: "everything"},
	}
	for _, tt := range tests {
		endpoint := newsEndpoint(tt.query, 3)
		parsed, err := url.Parse(endpoint)
		if err != nil {
			t.Fatalf("parse endpoint %q: %v", endpoint, err)
		}
		if parsed.Path != "/v2/"+tt.want {
			t.Errorf("query %q used path %q", tt.query, parsed.Path)
		}
		if tt.query == "linux" && parsed.Query().Get("language") != "en" {
			t.Errorf("query %q did not request English results", tt.query)
		}
		if got := parsed.Query().Get("pageSize"); got != strconv.Itoa(9) {
			t.Errorf("query %q used pageSize %q, want 9", tt.query, got)
		}
	}
}

func TestNewsEndpointCapsPageSize(t *testing.T) {
	parsed, err := url.Parse(newsEndpoint("linux", 101))
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("pageSize"); got != "100" {
		t.Fatalf("got pageSize %q, want 100", got)
	}
}
