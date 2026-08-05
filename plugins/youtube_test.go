package plugins

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/variablenix/GoBot/bot"
)

type youtubeRoundTripper func(*http.Request) (*http.Response, error)

func (f youtubeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func youtubeTestResponse(status int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestFormatYouTubeSearchResultKeepsShortLink(t *testing.T) {
	result := youtubeSearchResult{VideoID: "xnEYzp6IpqQ", Title: "Smoke Weed Everyday [HQ]", ChannelName: "Snoop Dogg"}
	got := formatYouTubeSearchResult(result, 320)
	want := "YouTube Snoop Dogg - Smoke Weed Everyday [HQ] URL https://youtu.be/xnEYzp6IpqQ"
	if got != want {
		t.Fatalf("formatYouTubeSearchResult = %q, want %q", got, want)
	}

	short := formatYouTubeSearchResult(result, 55)
	if !strings.HasSuffix(short, "https://youtu.be/xnEYzp6IpqQ") {
		t.Fatalf("short result lost URL: %q", short)
	}
}

func TestParseYouTubeInitialData(t *testing.T) {
	body := []byte(`var ytInitialData = {"contents":{"twoColumnSearchResultsRenderer":{"primaryContents":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"videoRenderer":{"videoId":"xnEYzp6IpqQ","title":{"runs":[{"text":"Smoke Weed Everyday [HQ]"}]},"ownerText":{"runs":[{"text":"Snoop Dogg"}]}}}]}}]}}}}};`)
	got, err := parseYouTubeInitialData(body)
	if err != nil {
		t.Fatalf("parseYouTubeInitialData returned error: %v", err)
	}
	if got.VideoID != "xnEYzp6IpqQ" || got.Title != "Smoke Weed Everyday [HQ]" || got.ChannelName != "Snoop Dogg" {
		t.Fatalf("parsed result = %+v", got)
	}
}

func TestYouTubeSearchUsesAPIThenPageFallback(t *testing.T) {
	oldClient := youtubeHTTPClient
	t.Cleanup(func() { youtubeHTTPClient = oldClient })

	apiClient := &http.Client{Transport: youtubeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "www.googleapis.com" || r.URL.Path != "/youtube/v3/search" || r.URL.Query().Get("type") != "video" {
			t.Fatalf("unexpected API request: %s", r.URL.String())
		}
		return youtubeTestResponse(http.StatusOK, "application/json", `{"items":[{"id":{"videoId":"xnEYzp6IpqQ"},"snippet":{"title":"Smoke Weed Everyday [HQ]","channelTitle":"Snoop Dogg"}}]}`), nil
	})}
	youtubeHTTPClient = apiClient
	plugin := &YouTube{}
	if err := plugin.Init(bot.PluginConfig{"api_key": "test-key"}, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	got, err := plugin.search(t.Context(), "SMOKE WEED EVERYDAY")
	if err != nil || got.VideoID != "xnEYzp6IpqQ" {
		t.Fatalf("API search = %+v, %v", got, err)
	}

	pageClient := &http.Client{Transport: youtubeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "www.youtube.com" || r.URL.Query().Get("search_query") != "SMOKE WEED EVERYDAY" {
			t.Fatalf("unexpected page request: %s", r.URL.String())
		}
		body := `var ytInitialData = {"contents":{"videoRenderer":{"videoId":"xnEYzp6IpqQ","title":{"simpleText":"Smoke Weed Everyday [HQ]"},"ownerText":{"simpleText":"Snoop Dogg"}}}};`
		return youtubeTestResponse(http.StatusOK, "text/html", body), nil
	})}
	youtubeHTTPClient = pageClient
	plugin.apiKey = ""
	got, err = plugin.search(t.Context(), "SMOKE WEED EVERYDAY")
	if err != nil || got.ChannelName != "Snoop Dogg" {
		t.Fatalf("page search = %+v, %v", got, err)
	}
}

func TestYouTubeHelpDocumentsAliases(t *testing.T) {
	help := (&YouTube{}).Help()
	for _, want := range []string{"!yt", "!youtube", "youtu.be", "no API key required"} {
		if !strings.Contains(help, want) {
			t.Errorf("help %q does not contain %q", help, want)
		}
	}
}
