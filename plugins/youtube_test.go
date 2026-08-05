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
	want := "[YouTube] Snoop Dogg — Smoke Weed Everyday [HQ] | https://youtu.be/xnEYzp6IpqQ"
	if plain := stripYouTubeIRC(got); plain != want {
		t.Fatalf("formatYouTubeSearchResult = %q, want %q", plain, want)
	}
	for _, color := range []string{ircRed, ircYellow, ircCyan} {
		if !strings.Contains(got, color) {
			t.Errorf("formatted result %q does not contain IRC color %q", got, color)
		}
	}

	withStats := result
	withStats.ViewCount = 1234567
	withStats.LikeCount = 42000
	withStats.HasViewCount = true
	withStats.HasLikeCount = true
	got = formatYouTubeSearchResult(withStats, 320)
	want = "[YouTube] Snoop Dogg — Smoke Weed Everyday [HQ] | 👁 1,234,567 views | 👍 42,000 likes | https://youtu.be/xnEYzp6IpqQ"
	if plain := stripYouTubeIRC(got); plain != want {
		t.Fatalf("formatted result with stats = %q, want %q", plain, want)
	}
	if !strings.Contains(got, ircGreen) {
		t.Errorf("formatted result with stats %q does not contain green likes color", got)
	}

	short := formatYouTubeSearchResult(result, 55)
	if !strings.HasSuffix(stripYouTubeIRC(short), "https://youtu.be/xnEYzp6IpqQ") {
		t.Fatalf("short result lost URL: %q", short)
	}
}

func stripYouTubeIRC(value string) string {
	return strings.NewReplacer(
		ircRed, "",
		ircYellow, "",
		ircCyan, "",
		ircGreen, "",
		ircReset, "",
	).Replace(value)
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
		if r.URL.Host != "www.googleapis.com" {
			t.Fatalf("unexpected API host: %s", r.URL.String())
		}
		switch r.URL.Path {
		case "/youtube/v3/search":
			if r.URL.Query().Get("type") != "video" {
				t.Fatalf("YouTube search did not request videos: %s", r.URL.String())
			}
			return youtubeTestResponse(http.StatusOK, "application/json", `{"items":[{"id":{"videoId":"xnEYzp6IpqQ"},"snippet":{"title":"Smoke Weed Everyday [HQ]","channelTitle":"Snoop Dogg"}}]}`), nil
		case "/youtube/v3/videos":
			if r.URL.Query().Get("part") != "statistics" || r.URL.Query().Get("id") != "xnEYzp6IpqQ" {
				t.Fatalf("unexpected YouTube statistics request: %s", r.URL.String())
			}
			return youtubeTestResponse(http.StatusOK, "application/json", `{"items":[{"statistics":{"viewCount":"1234567","likeCount":"42000"}}]}`), nil
		default:
			t.Fatalf("unexpected YouTube API path: %s", r.URL.String())
			return nil, nil
		}
	})}
	youtubeHTTPClient = apiClient
	plugin := &YouTube{}
	if err := plugin.Init(bot.PluginConfig{"api_key": "test-key"}, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	got, err := plugin.search(t.Context(), "SMOKE WEED EVERYDAY")
	if err != nil || got.VideoID != "xnEYzp6IpqQ" || !got.HasViewCount || got.ViewCount != 1234567 || !got.HasLikeCount || got.LikeCount != 42000 {
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

func TestYouTubeStatisticsAreBestEffort(t *testing.T) {
	oldClient := youtubeHTTPClient
	t.Cleanup(func() { youtubeHTTPClient = oldClient })

	youtubeHTTPClient = &http.Client{Transport: youtubeRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/youtube/v3/search":
			return youtubeTestResponse(http.StatusOK, "application/json", `{"items":[{"id":{"videoId":"xnEYzp6IpqQ"},"snippet":{"title":"Smoke Weed Everyday [HQ]","channelTitle":"Snoop Dogg"}}]}`), nil
		case "/youtube/v3/videos":
			return youtubeTestResponse(http.StatusServiceUnavailable, "application/json", `{"error":"temporary failure"}`), nil
		default:
			t.Fatalf("unexpected YouTube API path: %s", r.URL.String())
			return nil, nil
		}
	})}

	plugin := &YouTube{}
	if err := plugin.Init(bot.PluginConfig{"api_key": "test-key"}, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	got, err := plugin.search(t.Context(), "SMOKE WEED EVERYDAY")
	if err != nil || got.VideoID != "xnEYzp6IpqQ" || got.HasViewCount || got.HasLikeCount {
		t.Fatalf("search with unavailable statistics = %+v, %v", got, err)
	}
}

func TestFormatYouTubeCount(t *testing.T) {
	for input, want := range map[int64]string{
		0:       "0",
		999:     "999",
		1000:    "1,000",
		1234567: "1,234,567",
	} {
		if got := formatYouTubeCount(input); got != want {
			t.Errorf("formatYouTubeCount(%d) = %q, want %q", input, got, want)
		}
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
