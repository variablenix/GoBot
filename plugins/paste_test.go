package plugins

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestPasteCreateHonorsUnlistedVisibilityAndAuth(t *testing.T) {
	t.Setenv("BOT_PASTE_BASE_URL", "https://paste.example.test")
	t.Setenv("BOT_PASTE_TOKEN", "secret")
	p := &Paste{}
	if err := p.Init(nil, nil); err != nil {
		t.Fatal(err)
	}
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.String() != "https://paste.example.test/api/gists" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		}
		if got := r.Header.Get("Authorization"); got != "token secret" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		var body struct {
			Visibility string `json:"visibility"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Visibility != "unlisted" {
			t.Fatalf("unexpected visibility: %+v", body)
		}
		return newPluginResponse(http.StatusCreated, `{"html_url":"https://paste.example.test/abc"}`), nil
	})}
	got, err := p.createPaste(t.Context(), "hello")
	if err != nil || got != "https://paste.example.test/abc" {
		t.Fatalf("createPaste() = %q, %v", got, err)
	}
}

func TestFetchPasteURLRejectsPrivateTargets(t *testing.T) {
	for _, rawURL := range []string{
		"http://127.0.0.1/secret",
		"http://10.0.0.1/metadata",
		"http://localhost/",
	} {
		if _, _, err := fetchPasteURL(t.Context(), rawURL, 100); err == nil {
			t.Fatalf("fetchPasteURL accepted private target %q", rawURL)
		}
	}
}

func TestPasteOutputIsOneSanitizedLine(t *testing.T) {
	got := cleanExternalText("[paste] https://example.test/a\r\n")
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("paste output contains line breaks: %q", got)
	}
}

func TestHardWrapPasteTextWrapsWordsAndPreservesParagraphs(t *testing.T) {
	got := hardWrapPasteText("one two three four\nnext paragraph", 10)
	want := "one two\nthree four\nnext\nparagraph"
	if got != want {
		t.Fatalf("hardWrapPasteText() = %q, want %q", got, want)
	}
}
