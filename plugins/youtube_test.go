package plugins

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

func TestYouTubeSearchRoutesUseIdenticalStatisticsFormatting(t *testing.T) {
	oldClient := youtubeHTTPClient
	t.Cleanup(func() { youtubeHTTPClient = oldClient })
	for _, route := range []string{"api", "page", "index"} {
		t.Run(route, func(t *testing.T) {
			statisticsRequests := 0
			requests := 0
			youtubeHTTPClient = &http.Client{Transport: youtubeRoundTripper(func(r *http.Request) (*http.Response, error) {
				requests++
				switch r.URL.Host + r.URL.Path {
				case "www.googleapis.com/youtube/v3/search":
					if route == "api" {
						return youtubeTestResponse(200, "application/json", `{"items":[{"id":{"videoId":"first123456"},"snippet":{"title":"Example music","channelTitle":"Example artist"}}]}`), nil
					}
					return youtubeTestResponse(200, "application/json", `{"items":[]}`), nil
				case "www.youtube.com/results":
					if route == "page" {
						return youtubeTestResponse(200, "text/html", `var ytInitialData={"videoRenderer":{"videoId":"first123456","title":{"simpleText":"Example music"},"ownerText":{"simpleText":"Example artist"}}};`), nil
					}
					return youtubeTestResponse(200, "text/html", `<html>Verify your age</html>`), nil
				case "www.bing.com/search":
					return youtubeTestResponse(200, "text/html", `<li class="b_algo"><h2><a href="https://www.youtube.com/watch?v=first123456">Example music - YouTube</a></h2></li>`), nil
				case "www.youtube.com/oembed":
					return youtubeTestResponse(200, "application/json", `{"title":"Example music","author_name":"Example artist"}`), nil
				case "www.googleapis.com/youtube/v3/videos":
					statisticsRequests++
					if r.URL.Query().Get("id") != "first123456" || r.URL.Query().Get("part") != "statistics" || r.URL.Query().Get("key") != "test-key" {
						t.Fatal("statistics lookup lost the selected video or API configuration")
					}
					return youtubeTestResponse(200, "application/json", `{"items":[{"statistics":{"viewCount":"1234567","likeCount":"42000"}}]}`), nil
				default:
					t.Fatalf("unexpected request: %s%s", r.URL.Host, r.URL.Path)
					return nil, nil
				}
			})}
			result, err := (&YouTube{apiKey: "test-key"}).search(t.Context(), "example music")
			if err != nil || statisticsRequests != 1 || requests != map[string]int{"api": 2, "page": 3, "index": 5}[route] {
				t.Fatalf("route=%s error=%v statistics=%d requests=%d", route, err, statisticsRequests, requests)
			}
			want := ircColor(ircRed, "[YouTube]") + " " + ircColor(ircYellow, "Example artist") + " — " +
				ircColor(ircCyan, "Example music") + " | " + ircColor(ircYellow, "👁 1,234,567 views") + " | " +
				ircColor(ircGreen, "👍 42,000 likes") + " | " + ircColor(ircCyan, "https://youtu.be/first123456")
			got := formatYouTubeSearchResultForTarget(result, 320, "#test")
			if got != want || strings.ContainsAny(got, "\uFE0F\uFE0E") {
				t.Fatalf("route %s has inconsistent colors/emojis: %q", route, got)
			}
		})
	}
}

func TestYouTubeFallbackStatisticsNeverDiscardVideo(t *testing.T) {
	oldClient := youtubeHTTPClient
	t.Cleanup(func() { youtubeHTTPClient = oldClient })
	for _, tc := range []struct {
		name   string
		status int
		body   string
		views  bool
		likes  bool
	}{
		{"unavailable", 503, `{}`, false, false},
		{"quota", 403, `{}`, false, false},
		{"deleted", 200, `{"items":[]}`, false, false},
		{"malformed-json", 200, `{`, false, false},
		{"missing-counts", 200, `{"items":[{"statistics":{}}]}`, false, false},
		{"missing-likes", 200, `{"items":[{"statistics":{"viewCount":"1234"}}]}`, true, false},
		{"invalid-counts", 200, `{"items":[{"statistics":{"viewCount":"-1","likeCount":"unknown"}}]}`, false, false},
		{"overflow", 200, `{"items":[{"statistics":{"viewCount":"99999999999999999999","likeCount":"-42"}}]}`, false, false},
		{"real-zero", 200, `{"items":[{"statistics":{"viewCount":"0","likeCount":"0"}}]}`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			youtubeHTTPClient = &http.Client{Transport: youtubeRoundTripper(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Host + r.URL.Path {
				case "www.googleapis.com/youtube/v3/search":
					return youtubeTestResponse(200, "application/json", `{"items":[]}`), nil
				case "www.youtube.com/results":
					return youtubeTestResponse(200, "text/html", `var ytInitialData={"videoRenderer":{"videoId":"first123456","title":{"simpleText":"Example music"}}};`), nil
				case "www.googleapis.com/youtube/v3/videos":
					return youtubeTestResponse(tc.status, "application/json", tc.body), nil
				default:
					t.Fatal("unexpected provider request")
					return nil, nil
				}
			})}
			result, err := (&YouTube{apiKey: "test-key"}).search(t.Context(), "example music")
			if err != nil || result.VideoID != "first123456" || result.Title != "Example music" || result.HasViewCount != tc.views || result.HasLikeCount != tc.likes {
				t.Fatalf("statistics failure altered a usable result: %+v, %v", result, err)
			}
			got := stripYouTubeIRC(formatYouTubeSearchResult(result, 320))
			if !strings.HasSuffix(got, "https://youtu.be/first123456") || strings.Contains(got, "👁") != tc.views || strings.Contains(got, "👍") != tc.likes {
				t.Fatalf("missing counts were invented or valid counts/link lost: %q", got)
			}
		})
	}
}

func TestYouTubeStatisticsTimeoutPreservesSuccessfulSearch(t *testing.T) {
	oldClient := youtubeHTTPClient
	t.Cleanup(func() { youtubeHTTPClient = oldClient })
	for _, budget := range []time.Duration{80 * time.Millisecond, 3 * time.Second} {
		t.Run(budget.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()
			statisticsRequests := 0
			youtubeHTTPClient = &http.Client{Transport: youtubeRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/youtube/v3/search" {
					return youtubeTestResponse(200, "application/json", `{"items":[{"id":{"videoId":"first123456"},"snippet":{"title":"Example music"}}]}`), nil
				}
				statisticsRequests++
				deadline, ok := r.Context().Deadline()
				parentDeadline, _ := ctx.Deadline()
				if !ok || deadline.After(parentDeadline) || time.Until(deadline) > time.Second {
					t.Fatal("statistics request exceeded its one-second or parent budget")
				}
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}
			result, err := (&YouTube{apiKey: "test-key"}).search(ctx, "example music")
			if err != nil || !validYouTubeSearchResult(result) || result.HasViewCount || result.HasLikeCount || statisticsRequests != 1 {
				t.Fatalf("timed-out statistics discarded video: %+v, %v", result, err)
			}
			if budget == 3*time.Second && ctx.Err() != nil {
				t.Fatal("statistics timeout canceled the whole command")
			}
		})
	}
}

func TestYouTubeStatisticsSkippedWithoutKeyOrSuccessfulSearch(t *testing.T) {
	oldClient := youtubeHTTPClient
	t.Cleanup(func() { youtubeHTTPClient = oldClient })
	for _, apiKey := range []string{"", "test-key"} {
		t.Run(map[bool]string{true: "no-key", false: "no-video"}[apiKey == ""], func(t *testing.T) {
			youtubeHTTPClient = &http.Client{Transport: youtubeRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/youtube/v3/videos" || (apiKey == "" && r.URL.Host == "www.googleapis.com") {
					t.Fatal("unnecessary statistics/API request")
				}
				if apiKey == "" {
					return youtubeTestResponse(200, "text/html", `var ytInitialData={"videoRenderer":{"videoId":"first123456","title":{"simpleText":"Example music"}}};`), nil
				}
				return youtubeTestResponse(503, "application/json", `{}`), nil
			})}
			result, err := (&YouTube{apiKey: apiKey}).search(t.Context(), "example music")
			if apiKey == "" && (err != nil || !validYouTubeSearchResult(result)) {
				t.Fatal("no-key search regressed")
			}
			if apiKey != "" && err == nil {
				t.Fatal("unavailable providers unexpectedly succeeded")
			}
		})
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

func TestYouTubeAssignmentVariantsAndPrimaryOrder(t *testing.T) {
	data := `{"contents":{"twoColumnSearchResultsRenderer":{"primaryContents":{"sectionListRenderer":{"contents":[{"adSlotRenderer":{"videoRenderer":{"videoId":"advert12345","title":{"simpleText":"Advertisement"}}}},{"videoRenderer":{"videoId":"first123456","title":{"simpleText":"First video"}}},{"videoRenderer":{"videoId":"later123456","title":{"simpleText":"Later video"}}}]}},"secondaryContents":{"videoRenderer":{"videoId":"other123456","title":{"simpleText":"Sidebar video"}}}}}}`
	for _, assignment := range []string{"var ytInitialData = ", "let ytInitialData=", "const ytInitialData\n =\n", `window["ytInitialData"] = `, `window['ytInitialData']=`} {
		t.Run(assignment, func(t *testing.T) {
			for range 25 {
				got, err := parseYouTubeInitialData([]byte(assignment + data + ";"))
				if err != nil || got.VideoID != "first123456" {
					t.Fatalf("wrong primary result: %+v, %v", got, err)
				}
			}
		})
	}
}

func TestYouTubeModernRenderersAndInvalidData(t *testing.T) {
	for _, body := range []string{
		`{"videoWithContextRenderer":{"videoId":"first123456","title":{"simpleText":"Example video"},"longBylineText":{"simpleText":"Example channel"}}}`,
		`{"lockupViewModel":{"contentId":"first123456","contentType":"LOCKUP_CONTENT_TYPE_VIDEO","metadata":{"lockupMetadataViewModel":{"title":{"content":"Example video"}}}}}`,
	} {
		got, err := parseYouTubeInitialData([]byte("var ytInitialData=" + body + ";"))
		if err != nil || got.VideoID != "first123456" || got.Title != "Example video" {
			t.Fatalf("renderer result: %+v, %v", got, err)
		}
	}
	for _, body := range []string{
		`<html>Confirm your age</html>`, `var ytInitialData = broken;`,
		`var ytInitialData={"videoRenderer":{"videoId":"../invalid","title":{"simpleText":"Unsafe"}}};`,
		`var ytInitialData={"lockupViewModel":{"contentType":"LOCKUP_CONTENT_TYPE_PLAYLIST","videoRenderer":{"videoId":"first123456","title":{"simpleText":"Playlist thumbnail"}}}};`,
	} {
		if _, err := parseYouTubeInitialData([]byte(body)); err == nil {
			t.Fatalf("accepted invalid video data: %s", body)
		}
	}
}

func TestYouTubeIndexURLValidation(t *testing.T) {
	for _, raw := range []string{"https://www.youtube.com/watch?v=first123456", "https://music.youtube.com/watch?v=first123456", "https://youtu.be/first123456"} {
		if got := youtubeIndexedVideoID(raw); got != "first123456" {
			t.Errorf("valid URL %q rejected", raw)
		}
	}
	for _, raw := range []string{
		"https://youtube.com.evil.example/watch?v=first123456", "https://www.youtube.com@evil.example/watch?v=first123456",
		"http://youtube.com/watch?v=first123456", "https://youtube.com:8443/watch?v=first123456",
		"https://youtube.com/watch?v=short", "https://youtube.com/watch?v=first123456&v=later123456",
		"https://youtube.com/channel/first123456", "https://youtu.be/first123456/extra", "http://127.0.0.1/private",
	} {
		if got := youtubeIndexedVideoID(raw); got != "" {
			t.Errorf("unsafe/non-video URL %q accepted: %q", raw, got)
		}
	}
}

func TestYouTubeRecoveryFromAPIAndPageFailures(t *testing.T) {
	old := youtubeHTTPClient
	t.Cleanup(func() { youtubeHTTPClient = old })
	for _, slow := range []bool{false, true} {
		t.Run(map[bool]string{false: "quota-and-age-gate", true: "timeouts"}[slow], func(t *testing.T) {
			var requests []string
			youtubeHTTPClient = &http.Client{Transport: youtubeRoundTripper(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.URL.Host+r.URL.Path)
				switch r.URL.Host + r.URL.Path {
				case "www.googleapis.com/youtube/v3/search", "www.youtube.com/results":
					if slow {
						<-r.Context().Done()
						return nil, r.Context().Err()
					}
					if r.URL.Host == "www.googleapis.com" {
						return youtubeTestResponse(403, "application/json", `{}`), nil
					}
					return youtubeTestResponse(200, "text/html", `var ytInitialData={"contents":{"backgroundPromoRenderer":{"title":{"simpleText":"Confirm your age"}}}};`), nil
				case "www.bing.com/search":
					if r.Context().Err() != nil || r.URL.Query().Get("q") != "site:youtube.com/watch example music" {
						t.Fatal("fallback lost query or inherited expired context")
					}
					// Ignore non-YouTube results, accept Bing's encoded destination,
					// and never fetch a URL supplied by a search result directly.
					encoded := "a1" + base64.RawURLEncoding.EncodeToString([]byte("https://www.youtube.com/watch?v=first123456"))
					return youtubeTestResponse(200, "text/html", `<li class="b_algo"><h2><a href="http://127.0.0.1/private">Wrong source</a></h2></li><li class="b_algo"><h2><a href="https://www.bing.com/ck/a?u=`+encoded+`">Example music - YouTube</a></h2></li>`), nil
				case "www.youtube.com/oembed":
					return youtubeTestResponse(200, "application/json", `{"title":"Example music","author_name":"Example artist"}`), nil
				case "www.googleapis.com/youtube/v3/videos":
					return youtubeTestResponse(403, "application/json", `{}`), nil
				default:
					t.Fatalf("unexpected fallback request host/path: %s", r.URL.Host+r.URL.Path)
					return nil, nil
				}
			})}
			ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
			defer cancel()
			got, err := (&YouTube{apiKey: "test-key"}).search(ctx, "example music")
			if err != nil || got.VideoID != "first123456" || got.ChannelName != "Example artist" || len(requests) != 5 {
				t.Fatalf("recovery: %+v, %v, requests=%v", got, err, requests)
			}
		})
	}
}

func TestYouTubeIndexedTitleSurvivesMetadataFailure(t *testing.T) {
	old := youtubeHTTPClient
	t.Cleanup(func() { youtubeHTTPClient = old })
	youtubeHTTPClient = &http.Client{Transport: youtubeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "www.bing.com" {
			return youtubeTestResponse(200, "text/html", `<li class="b_algo"><h2><a href="https://youtube.com/watch?v=first123456">Example music - YouTube</a></h2></li>`), nil
		}
		return youtubeTestResponse(403, "text/html", "Unavailable"), nil
	})}
	got, err := (&YouTube{}).searchIndex(t.Context(), "example music")
	if err != nil || got.Title != "Example music" || got.VideoID != "first123456" {
		t.Fatalf("best-effort metadata discarded result: %+v, %v", got, err)
	}
}

func TestYouTubeReplyBoundsAndTerminalSafety(t *testing.T) {
	for _, target := range []string{"#test", "#" + strings.Repeat("c", 180)} {
		for _, limit := range []int{160, 320, 500} {
			result := youtubeSearchResult{VideoID: "first123456", Title: strings.Repeat("音🎥", 200) + "\uFE0F\u202E\x03" + "04unsafe\r\n", ChannelName: strings.Repeat("界", 200), HasViewCount: true, ViewCount: 123456789}
			got := formatYouTubeSearchResultForTarget(result, limit, target)
			if len(got) > limit || len("PRIVMSG "+target+" :"+got+"\r\n") > 512 || !utf8.ValidString(got) {
				t.Fatalf("reply exceeds UTF-8/wire/config bounds: %d bytes", len(got))
			}
			if strings.ContainsAny(got, "\r\n\uFE0F\u202E") || !strings.HasSuffix(stripYouTubeIRC(got), "https://youtu.be/first123456") {
				t.Fatalf("unsafe reply or lost link: %q", got)
			}
		}
	}
}

func TestYouTubeSearchHTMLSizeLimit(t *testing.T) {
	old := youtubeHTTPClient
	t.Cleanup(func() { youtubeHTTPClient = old })
	youtubeHTTPClient = &http.Client{Transport: youtubeRoundTripper(func(r *http.Request) (*http.Response, error) {
		return youtubeTestResponse(200, "text/html", strings.Repeat("x", (4<<20)+1)), nil
	})}
	if _, err := youtubeSearchHTML(t.Context(), youtubeResultsURL); err == nil {
		t.Fatal("oversized HTML accepted")
	}
}

// Live checks are opt-in; normal CI uses deterministic provider fixtures.
func TestYouTubeIndexRetriesExplicitMusicTitle(t *testing.T) {
	old := youtubeHTTPClient
	t.Cleanup(func() { youtubeHTTPClient = old })
	var queries []string
	youtubeHTTPClient = &http.Client{Transport: youtubeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/oembed" {
			return youtubeTestResponse(403, "application/json", `{}`), nil
		}
		queries = append(queries, r.URL.Query().Get("q"))
		if len(queries) == 1 {
			return youtubeTestResponse(200, "text/html", `<li class="b_algo"><h2><a href="https://example.com/not-a-video">Unrelated result</a></h2></li>`), nil
		}
		return youtubeTestResponse(200, "text/html", `<li class="b_algo"><h2><a href="https://www.youtube.com/watch?v=first123456">Example music - YouTube</a></h2></li>`), nil
	})}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result, err := (&YouTube{}).searchIndex(ctx, "example artist - example music")
	if err != nil || result.VideoID != "first123456" || len(queries) != 2 || queries[0] != "site:youtube.com/watch example artist - example music" || queries[1] != "site:youtube.com/watch example music" {
		t.Fatalf("music recovery: result=%+v err=%v queries=%v", result, err, queries)
	}
}

func TestYouTubeAliasesReturnUsableIRCResult(t *testing.T) {
	old := youtubeHTTPClient
	t.Cleanup(func() { youtubeHTTPClient = old })
	youtubeHTTPClient = &http.Client{Transport: youtubeRoundTripper(func(*http.Request) (*http.Response, error) {
		return youtubeTestResponse(200, "text/html", `var ytInitialData={"videoRenderer":{"videoId":"first123456","title":{"simpleText":"Example music"}}};`), nil
	})}
	p := &YouTube{}
	p.Init(nil, nil)
	sent := make(chan bot.Outgoing, 4)
	b := &bot.Bot{Config: bot.Config{CommandPrefix: "!"}, Queue: bot.NewQueue(1, 1, func(m bot.Outgoing) { sent <- m })}
	defer b.Queue.Drain(context.Background())
	for _, alias := range []string{"!yt", "!youtube"} {
		if !p.Handle(b, bot.Message{Nick: "tester", Target: "#test", IsChannel: true, Text: alias + " example music"}) {
			t.Fatal("YouTube alias not handled")
		}
	}
	b.Queue.Drain(context.Background())
	if len(sent) != 2 {
		t.Fatal("missing YouTube alias replies")
	}
	for len(sent) > 0 {
		m := <-sent
		if m.Target != "#test" || !strings.Contains(m.Text, "https://youtu.be/first123456") || len("PRIVMSG #test :"+m.Text+"\r\n") > 512 {
			t.Fatal("invalid YouTube command reply")
		}
	}
}

func TestLiveYouTubeSearch(t *testing.T) {
	if os.Getenv("GOBOT_LIVE_YOUTUBE") != "1" {
		t.Skip("opt-in live YouTube smoke test")
	}
	loadLiveProviderCredentials(t)
	queries := []string{"Linux server setup", "classical piano music"}
	if raw := os.Getenv("GOBOT_LIVE_YOUTUBE_QUERIES"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &queries); err != nil {
			t.Fatal("invalid live query list")
		}
	}
	p := &YouTube{}
	p.Init(bot.PluginConfig{"api_key": os.Getenv("BOT_YOUTUBE_API_KEY")}, nil)
	for index, query := range queries {
		t.Run("query-"+string(rune('A'+index)), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), p.timeout)
			defer cancel()
			result, err := p.search(ctx, query)
			if err != nil || !validYouTubeSearchResult(result) {
				t.Fatalf("live lookup failed: %v", err)
			}
			if os.Getenv("GOBOT_LIVE_YOUTUBE_REQUIRE_STATS") == "1" && (!result.HasViewCount || !result.HasLikeCount) {
				t.Fatal("live lookup did not return public view and like counts")
			}
			t.Logf("video=%s has_views=%t has_likes=%t", result.VideoID, result.HasViewCount, result.HasLikeCount)
		})
	}
}
