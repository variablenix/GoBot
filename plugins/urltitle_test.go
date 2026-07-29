package plugins

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
)

func TestURLExtraction(t *testing.T) {
	r := regexp.MustCompile(`https?://[^\s<>]+`)
	got := r.FindAllString("see https://example.com/a and not ftp://bad", -1)
	if len(got) != 1 || got[0] != "https://example.com/a" {
		t.Fatalf("got %v", got)
	}
}

func TestPageTitlePrefersOpenGraph(t *testing.T) {
	body := []byte(`<html><head>
<meta property="og:title" content="LEFT TO DIE - Initium Mortis">
<meta name="twitter:title" content="Twitter fallback">
<title>- YouTube</title>
</head></html>`)
	if got := pageTitle(body); got != "LEFT TO DIE - Initium Mortis" {
		t.Fatalf("got %q", got)
	}
}

func TestPageTitleSupportsAttributeOrderAndEntities(t *testing.T) {
	body := []byte(`<html><head><meta content="A &amp; B" name="twitter:title"><title>Fallback</title></head></html>`)
	if got := pageTitle(body); got != "A & B" {
		t.Fatalf("got %q", got)
	}
}

func TestPageTitleFallsBackToHTMLTitle(t *testing.T) {
	if got := pageTitle([]byte(`<html><head><title>  Example &amp; page </title></head></html>`)); got != "Example & page" {
		t.Fatalf("got %q", got)
	}
}

func TestTitleLooksLikeError(t *testing.T) {
	for _, title := range []string{"Access Denied", "403 Forbidden", "Security Verification"} {
		if !titleLooksLikeError(title) {
			t.Errorf("expected %q to be rejected", title)
		}
	}
	if titleLooksLikeError("CNN International — Breaking News") {
		t.Fatal("legitimate title was rejected")
	}
}

func TestYouTubeHostDetection(t *testing.T) {
	for _, host := range []string{"youtube.com", "www.youtube.com", "youtu.be"} {
		if !isYouTubeHost(host) {
			t.Errorf("expected %q to be recognized", host)
		}
	}
	if isYouTubeHost("notyoutube.example") {
		t.Fatal("unexpected YouTube host match")
	}
}

func TestRedditHostDetection(t *testing.T) {
	for _, host := range []string{"reddit.com", "www.reddit.com", "old.reddit.com", "redd.it"} {
		if !isRedditHost(host) {
			t.Errorf("expected %q to be recognized", host)
		}
	}
	if isRedditHost("notreddit.example") {
		t.Fatal("unexpected Reddit host match")
	}
}

func TestOembedTitle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title":"A Reddit post title"}`))
	}))
	defer server.Close()

	title, ok := oembedTitle(t.Context(), server.Client(), server.URL)
	if !ok || title != "A Reddit post title" {
		t.Fatalf("got title %q, ok=%v", title, ok)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("abcdef", 4); got != "abc…" {
		t.Fatalf("got %q", got)
	}
	if got := truncateRunes("cafe\u0301", 10); got != "cafe\u0301" {
		t.Fatalf("got %q", got)
	}
	if got := truncateRunes("abcdef", 1); got != "…" {
		t.Fatalf("got %q", got)
	}
}

func TestTitleLooksLikeURLIsAllowed(t *testing.T) {
	title := "Example https://example.com"
	if titleLooksLikeError(title) {
		t.Fatalf("title %q should not be treated as an error", title)
	}
}

func TestShortYouTubeDisplayURL(t *testing.T) {
	tests := map[string]string{
		"https://www.youtube.com/watch?v=abc123": "youtu.be/abc123",
		"https://youtu.be/xyz789":                "youtu.be/xyz789",
		"https://youtube.com/shorts/qwerty":      "youtu.be/qwerty",
		"https://youtube.com/embed/zxcvb":        "youtu.be/zxcvb",
	}
	for raw, want := range tests {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		got, ok := shortYouTubeDisplayURL(u)
		if !ok {
			t.Fatalf("expected display URL for %q", raw)
		}
		if got != want {
			t.Fatalf("for %q got %q want %q", raw, got, want)
		}
	}
}
