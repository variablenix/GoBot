package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Reddit struct{ cfg bot.PluginConfig }

func (p *Reddit) Name() string       { return "reddit" }
func (p *Reddit) Commands() []string { return []string{"reddit", "r"} }
func (p *Reddit) Help() string {
	return "!reddit <Reddit post URL|r/subreddit> — show one compact Reddit result (alias: !r)"
}
func (p *Reddit) Init(c bot.PluginConfig, _ *storage.DB) error { p.cfg = c; return nil }

func (p *Reddit) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || !isRedditCommand(cmd) {
		return false
	}
	postURL, endpoint, ok := redditLookupEndpoint(strings.TrimSpace(arg))
	if !ok {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !reddit <Reddit post URL|r/subreddit>"))
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), redditTimeout(p.cfg))
	defer cancel()
	post, ok := fetchRedditPost(ctx, endpoint)
	if !ok {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "Reddit lookup is temporarily unavailable"))
		return true
	}
	maxLength := p.cfg.Int("max_length", 360)
	if maxLength < 120 {
		maxLength = 120
	}
	result := fmt.Sprintf("[Reddit] %s | u/%s | r/%s | %d points | %d comments — %s", cleanExternalText(post.Title), cleanExternalText(post.Author), cleanExternalText(post.Subreddit), post.Score, post.Comments, postURL)
	b.Send(m.ReplyTarget(), truncateRunes(result, maxLength))
	return true
}

type redditPost struct {
	Title     string
	Author    string
	Subreddit string
	Score     int
	Comments  int
}

func fetchRedditPost(ctx context.Context, endpoint string) (redditPost, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return redditPost{}, false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; Reddit lookup)")
	res, err := apiHTTPClient.Do(req)
	if err != nil {
		return redditPost{}, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return redditPost{}, false
	}
	var payload []struct {
		Data struct {
			Children []struct {
				Data struct {
					Title                 string `json:"title"`
					Author                string `json:"author"`
					Subreddit             string `json:"subreddit"`
					Score                 int    `json:"score"`
					Comments              int    `json:"num_comments"`
					SubredditNamePrefixed string `json:"subreddit_name_prefixed"`
				} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&payload); err != nil || len(payload) == 0 || len(payload[0].Data.Children) == 0 {
		return redditPost{}, false
	}
	data := payload[0].Data.Children[0].Data
	if strings.TrimSpace(data.Title) == "" {
		return redditPost{}, false
	}
	subreddit := data.SubredditNamePrefixed
	if strings.HasPrefix(strings.ToLower(subreddit), "r/") {
		subreddit = subreddit[2:]
	}
	return redditPost{Title: data.Title, Author: data.Author, Subreddit: subreddit, Score: data.Score, Comments: data.Comments}, true
}

func redditPostEndpoint(raw string) (string, string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", "", false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host != "reddit.com" && host != "www.reddit.com" && host != "old.reddit.com" && host != "new.reddit.com" {
		return "", "", false
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	commentsIndex := -1
	for i, segment := range segments {
		if strings.EqualFold(segment, "comments") {
			commentsIndex = i
			break
		}
	}
	if commentsIndex < 0 || commentsIndex+1 >= len(segments) || !validRedditID(segments[commentsIndex+1]) {
		return "", "", false
	}
	canonical := &url.URL{Scheme: "https", Host: "www.reddit.com", Path: parsed.Path}
	postURL := strings.TrimSuffix(canonical.String(), "/") + "/"
	endpointURL := &url.URL{Scheme: "https", Host: "www.reddit.com", Path: strings.TrimSuffix(parsed.Path, "/") + ".json", RawQuery: "raw_json=1"}
	return postURL, endpointURL.String(), true
}

func redditLookupEndpoint(raw string) (string, string, bool) {
	if postURL, endpoint, ok := redditPostEndpoint(raw); ok {
		return postURL, endpoint, true
	}
	return redditSubredditEndpoint(raw)
}

func redditSubredditEndpoint(raw string) (string, string, bool) {
	value := strings.TrimSpace(raw)
	if len(value) < 4 || !strings.HasPrefix(strings.ToLower(value), "r/") {
		return "", "", false
	}
	name := value[2:]
	if strings.HasSuffix(name, "/") {
		name = strings.TrimSuffix(name, "/")
	}
	if !validRedditSubreddit(name) {
		return "", "", false
	}

	postURL := "https://www.reddit.com/r/" + name + "/"
	endpointURL := &url.URL{
		Scheme:   "https",
		Host:     "www.reddit.com",
		Path:     "/r/" + name + "/new.json",
		RawQuery: "raw_json=1&limit=1",
	}
	return postURL, endpointURL.String(), true
}

func validRedditSubreddit(name string) bool {
	if len(name) < 2 || len(name) > 21 {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func validRedditID(id string) bool {
	if id == "" || len(id) > 32 {
		return false
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func isRedditCommand(command string) bool {
	switch strings.ToLower(command) {
	case "reddit", "r":
		return true
	default:
		return false
	}
}

func redditTimeout(c bot.PluginConfig) time.Duration {
	seconds := c.Int("timeout_seconds", 8)
	if seconds < 1 || seconds > 30 {
		seconds = 8
	}
	return time.Duration(seconds) * time.Second
}
