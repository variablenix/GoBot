package plugins

import (
	"context"
	"testing"
)

func TestWikipediaRequestSetsDescriptiveUserAgent(t *testing.T) {
	req, err := wikipediaRequest(context.Background(), "https://en.wikipedia.org/api/rest_v1/page/summary/Linux")
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("User-Agent"); got == "" || got == "Go-http-client/1.1" {
		t.Fatalf("unexpected User-Agent %q", got)
	}
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("got Accept %q", got)
	}
}
