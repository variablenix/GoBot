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

func TestRedditCommands(t *testing.T) {
	for _, command := range []string{"reddit", "r"} {
		if !isRedditCommand(command) {
			t.Fatalf("expected %q to be a Reddit command", command)
		}
	}
}
