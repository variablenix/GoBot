package plugins

import "testing"

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
	for _, raw := range []string{"linux", "r/", "r/linux/extra", "r/linux?sort=new", "r/linux!"} {
		if _, _, ok := redditSubredditEndpoint(raw); ok {
			t.Fatalf("invalid subreddit accepted: %q", raw)
		}
	}
}

func TestRedditLookupEndpointAcceptsPostsAndSubreddits(t *testing.T) {
	if _, _, ok := redditLookupEndpoint("r/golang"); !ok {
		t.Fatal("subreddit lookup was rejected")
	}
	if _, _, ok := redditLookupEndpoint("https://www.reddit.com/r/golang/comments/abc123/example"); !ok {
		t.Fatal("post lookup was rejected")
	}
}

func TestRedditCommands(t *testing.T) {
	for _, command := range []string{"reddit", "r"} {
		if !isRedditCommand(command) {
			t.Fatalf("expected %q to be a Reddit command", command)
		}
	}
}
