package plugins

import (
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
