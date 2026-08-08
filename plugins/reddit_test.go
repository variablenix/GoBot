package plugins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRedditPostEndpoint(t *testing.T) {
	postURL, endpoint, ok := redditPostEndpoint("https://www.reddit.com/r/golang/comments/abc123/example/?utm_source=test")
	if !ok || postURL != "https://www.reddit.com/r/golang/comments/abc123/example/" || endpoint != "https://www.reddit.com/r/golang/comments/abc123/example.json?raw_json=1" {
		t.Fatalf("unexpected Reddit URLs: %q, %q, %v", postURL, endpoint, ok)
	}
	for _, raw := range []string{
		"https://example.com/r/golang/comments/abc123/example",
		"https://www.reddit.com/r/golang/about",
		"https://www.reddit.com/r/golang/comments/not valid/example",
	} {
		if _, _, ok := redditPostEndpoint(raw); ok {
			t.Fatalf("invalid Reddit URL accepted: %q", raw)
		}
	}
}

func TestRedditSubredditEndpoint(t *testing.T) {
	postURL, endpoint, ok := redditSubredditEndpoint("r/linux")
	wantURL := "https://www.reddit.com/r/linux/"
	wantEndpoint := "https://www.reddit.com/r/linux/new.json?raw_json=1&limit=1"
	if !ok || postURL != wantURL || endpoint != wantEndpoint {
		t.Fatalf("unexpected subreddit URLs: %q, %q, %v", postURL, endpoint, ok)
	}
	postURL, endpoint, ok = redditSubredditEndpoint("https://www.reddit.com/r/linux/")
	if !ok || postURL != wantURL || endpoint != wantEndpoint {
		t.Fatalf("unexpected full subreddit URLs: %q, %q, %v", postURL, endpoint, ok)
	}
	for _, raw := range []string{"linux", "r/", "r/linux/extra", "r/linux?sort=new", "r/linux!"} {
		if _, _, ok := redditSubredditEndpoint(raw); ok {
			t.Fatalf("invalid subreddit accepted: %q", raw)
		}
	}
}

func TestRedditSortedSubredditEndpoint(t *testing.T) {
	postURL, endpoint, ok := redditSubredditEndpointWithSort("r/linux", "top")
	wantURL := "https://www.reddit.com/r/linux/"
	wantEndpoint := "https://www.reddit.com/r/linux/top.json?raw_json=1&limit=1"
	if !ok || postURL != wantURL || endpoint != wantEndpoint {
		t.Fatalf("unexpected sorted subreddit URLs: %q, %q, %v", postURL, endpoint, ok)
	}
	if _, _, ok := redditSubredditEndpointWithSort("r/linux", "controversial"); ok {
		t.Fatal("unsupported Reddit sort accepted")
	}
}

func TestParseRedditLookupArg(t *testing.T) {
	tests := []struct {
		arg, target, sort string
		ok                bool
	}{
		{arg: "r/linuxmemes", target: "r/linuxmemes", sort: "new", ok: true},
		{arg: "top r/linuxmemes", target: "r/linuxmemes", sort: "top", ok: true},
		{arg: "r/linuxmemes rising", target: "r/linuxmemes", sort: "rising", ok: true},
		{arg: "hot https://www.reddit.com/r/linux/", target: "https://www.reddit.com/r/linux/", sort: "hot", ok: true},
		{arg: "top", target: "", sort: "", ok: false},
		{arg: "top r/linuxmemes week", target: "", sort: "", ok: false},
	}
	for _, test := range tests {
		target, sort, ok := parseRedditLookupArg(test.arg)
		if target != test.target || sort != test.sort || ok != test.ok {
			t.Errorf("parseRedditLookupArg(%q) = %q, %q, %v; want %q, %q, %v", test.arg, target, sort, ok, test.target, test.sort, test.ok)
		}
	}
}

func TestRedditSortedRSSEndpoint(t *testing.T) {
	got, ok := redditRSSEndpointWithSort("r/linux", "hot")
	want := "https://www.reddit.com/r/linux/hot.rss?limit=1"
	if !ok || got != want {
		t.Fatalf("unexpected sorted RSS endpoint: %q, %v", got, ok)
	}
}

func TestRedditLookupEndpointAcceptsPostsAndSubreddits(t *testing.T) {
	if _, _, ok := redditLookupEndpoint("r/golang"); !ok {
		t.Fatal("subreddit lookup was rejected")
	}
	if _, _, ok := redditLookupEndpoint("https://www.reddit.com/r/golang/comments/abc123/example"); !ok {
		t.Fatal("post lookup was rejected")
	}
	if _, _, ok := redditLookupEndpoint("https://www.reddit.com/r/golang/"); !ok {
		t.Fatal("full subreddit URL lookup was rejected")
	}
}

func TestRedditRSSEndpoint(t *testing.T) {
	if got, ok := redditRSSEndpoint("https://www.reddit.com/r/linux/"); !ok || got != "https://www.reddit.com/r/linux.rss?limit=1" {
		t.Fatalf("unexpected subreddit RSS endpoint: %q, %v", got, ok)
	}
	if got, ok := redditRSSEndpoint("https://www.reddit.com/r/golang/comments/abc123/example"); !ok || got != "https://www.reddit.com/r/golang/comments/abc123/example.rss" {
		t.Fatalf("unexpected post RSS endpoint: %q, %v", got, ok)
	}
}

func TestRedditOEmbedEndpoint(t *testing.T) {
	got, ok := redditOEmbedEndpoint("https://www.reddit.com/r/linux/comments/abc123/example")
	if !ok || got != "https://www.reddit.com/oembed?url=https%3A%2F%2Fwww.reddit.com%2Fr%2Flinux%2Fcomments%2Fabc123%2Fexample%2F&format=json" {
		t.Fatalf("unexpected oEmbed endpoint: %q, %v", got, ok)
	}
	if _, ok := redditOEmbedEndpoint("https://www.reddit.com/r/linux/"); ok {
		t.Fatal("subreddit URL incorrectly accepted as a post oEmbed lookup")
	}
}

func TestFormatRedditResultOmitsUnavailableStats(t *testing.T) {
	got := formatRedditResult(redditPost{Title: "A title", Author: "alice", Subreddit: "linux"}, "https://www.reddit.com/r/linux/")
	want := "[Reddit] A title | u/alice | r/linux — https://www.reddit.com/r/linux/"
	if got != want {
		t.Fatalf("unexpected RSS result: %q", got)
	}
}

func TestFormatRedditResultIncludesExplicitSort(t *testing.T) {
	got := formatRedditResultWithSort(redditPost{Title: "A title"}, "https://www.reddit.com/r/linux/", "top")
	want := "[Reddit top] A title — https://www.reddit.com/r/linux/"
	if got != want {
		t.Fatalf("unexpected sorted Reddit result: %q", got)
	}
}

func TestFetchRedditRSS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<rss><channel><item><title>Linux news</title><link>https://www.reddit.com/r/linux/comments/abc123/post/</link><dc:creator xmlns:dc="http://purl.org/dc/elements/1.1/">u/alice</dc:creator><category>r/linux</category></item></channel></rss>`))
	}))
	defer server.Close()
	originalClient := apiHTTPClient
	apiHTTPClient = server.Client()
	defer func() { apiHTTPClient = originalClient }()

	post, ok := fetchRedditRSS(context.Background(), server.URL)
	if !ok || post.Title != "Linux news" || post.Author != "alice" || post.Subreddit != "linux" || post.HasStats {
		t.Fatalf("unexpected RSS post: %#v, %v", post, ok)
	}
}

func TestFetchRedditAtom(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"><entry><author><name>/u/alice</name></author><category term="linux" label="r/linux"/><link href="https://www.reddit.com/r/linux/comments/abc123/post/"/><title>Linux news</title></entry></feed>`))
	}))
	defer server.Close()
	originalClient := apiHTTPClient
	apiHTTPClient = server.Client()
	defer func() { apiHTTPClient = originalClient }()

	post, ok := fetchRedditRSS(context.Background(), server.URL)
	if !ok || post.Title != "Linux news" || post.Author != "alice" || post.Subreddit != "linux" {
		t.Fatalf("unexpected Atom post: %#v, %v", post, ok)
	}
}

func TestRedditCommands(t *testing.T) {
	for _, command := range []string{"reddit", "r"} {
		if !isRedditCommand(command) {
			t.Fatalf("expected %q to be a Reddit command", command)
		}
	}
}
