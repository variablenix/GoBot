package plugins

import (
	"context"
	"encoding/json"
	"encoding/xml"
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
		if rssEndpoint, rssOK := redditRSSEndpoint(strings.TrimSpace(arg)); rssOK {
			post, ok = fetchRedditRSS(ctx, rssEndpoint)
		}
	}
	if !ok {
		if oembedEndpoint, oembedOK := redditOEmbedEndpoint(strings.TrimSpace(arg)); oembedOK {
			post, ok = fetchRedditOEmbed(ctx, oembedEndpoint, postURL)
		}
	}
	if !ok {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "Reddit lookup is temporarily unavailable"))
		return true
	}
	maxLength := p.cfg.Int("max_length", 360)
	if maxLength < 120 {
		maxLength = 120
	}
	result := formatRedditResult(post, postURL)
	b.Send(m.ReplyTarget(), truncateRunes(result, maxLength))
	return true
}

type redditPost struct {
	Title     string
	Author    string
	Subreddit string
	Score     int
	Comments  int
	HasStats  bool
}

func formatRedditResult(post redditPost, postURL string) string {
	result := "[Reddit] " + cleanExternalText(post.Title)
	if author := cleanExternalText(post.Author); author != "" {
		result += " | u/" + author
	}
	if subreddit := cleanExternalText(post.Subreddit); subreddit != "" {
		result += " | r/" + subreddit
	}
	if post.HasStats {
		result += fmt.Sprintf(" | %d points | %d comments", post.Score, post.Comments)
	}
	return result + " — " + postURL
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
	return redditPost{Title: data.Title, Author: data.Author, Subreddit: subreddit, Score: data.Score, Comments: data.Comments, HasStats: true}, true
}

type redditRSSFeed struct {
	Channel struct {
		Items []redditRSSItem `xml:"item"`
	} `xml:"channel"`
	Entries []redditAtomEntry `xml:"entry"`
}

type redditRSSItem struct {
	Title   string              `xml:"title"`
	Link    string              `xml:"link"`
	Author  string              `xml:"author"`
	Creator string              `xml:"creator"`
	Tags    []redditRSSCategory `xml:"category"`
}

type redditRSSCategory struct {
	Value string `xml:",chardata"`
	Term  string `xml:"term,attr"`
}

type redditAtomEntry struct {
	Title    string              `xml:"title"`
	Link     string              `xml:"link,attr"`
	Links    []redditAtomLink    `xml:"link"`
	Author   redditRSSAuthor     `xml:"author"`
	Category []redditRSSCategory `xml:"category"`
}

type redditAtomLink struct {
	Href string `xml:"href,attr"`
}

type redditRSSAuthor struct {
	Name string `xml:"name"`
}

func fetchRedditRSS(ctx context.Context, endpoint string) (redditPost, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return redditPost{}, false
	}
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; Reddit lookup)")
	res, err := apiHTTPClient.Do(req)
	if err != nil {
		return redditPost{}, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return redditPost{}, false
	}
	var feed redditRSSFeed
	if err := xml.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&feed); err != nil {
		return redditPost{}, false
	}
	var item redditRSSItem
	if len(feed.Channel.Items) > 0 {
		item = feed.Channel.Items[0]
	} else if len(feed.Entries) > 0 {
		entry := feed.Entries[0]
		item.Title = entry.Title
		item.Author = entry.Author.Name
		if len(entry.Links) > 0 {
			item.Link = entry.Links[0].Href
		}
		for _, category := range entry.Category {
			if category.Term != "" {
				item.Tags = append(item.Tags, redditRSSCategory{Value: "r/" + strings.TrimPrefix(category.Term, "r/")})
			}
		}
	}
	if item.Title == "" {
		return redditPost{}, false
	}
	title := strings.TrimSpace(item.Title)
	if title == "" {
		return redditPost{}, false
	}
	author := strings.TrimSpace(item.Creator)
	if author == "" {
		author = strings.TrimSpace(item.Author)
	}
	author = strings.TrimPrefix(strings.TrimPrefix(author, "/"), "u/")
	subreddit := ""
	for _, category := range item.Tags {
		tag := strings.TrimSpace(firstNonEmpty(category.Term, category.Value))
		if strings.HasPrefix(strings.ToLower(tag), "r/") {
			subreddit = tag[2:]
			break
		}
	}
	if subreddit == "" {
		subreddit = subredditFromRedditURL(item.Link)
	}
	return redditPost{Title: title, Author: author, Subreddit: subreddit}, true
}

func redditOEmbedEndpoint(raw string) (string, bool) {
	postURL, _, ok := redditPostEndpoint(raw)
	if !ok {
		return "", false
	}
	return "https://www.reddit.com/oembed?url=" + url.QueryEscape(postURL) + "&format=json", true
}

func fetchRedditOEmbed(ctx context.Context, endpoint, postURL string) (redditPost, bool) {
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
	var data struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&data); err != nil || strings.TrimSpace(data.Title) == "" {
		return redditPost{}, false
	}
	return redditPost{Title: data.Title, Subreddit: subredditFromRedditURL(postURL)}, true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
	name := ""
	if strings.HasPrefix(strings.ToLower(value), "r/") {
		name = value[2:]
	} else {
		parsed, err := url.Parse(value)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
			return "", "", false
		}
		host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
		if host != "reddit.com" && host != "www.reddit.com" && host != "old.reddit.com" && host != "new.reddit.com" {
			return "", "", false
		}
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(segments) != 2 || !strings.EqualFold(segments[0], "r") {
			return "", "", false
		}
		name = segments[1]
	}
	name = strings.TrimSuffix(name, "/")
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

func redditRSSEndpoint(raw string) (string, bool) {
	if _, _, ok := redditSubredditEndpoint(raw); ok {
		value := strings.TrimSpace(raw)
		name := ""
		if strings.HasPrefix(strings.ToLower(value), "r/") {
			name = value[2:]
		} else if parsed, err := url.Parse(value); err == nil {
			segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
			if len(segments) == 2 {
				name = segments[1]
			}
		}
		name = strings.TrimSuffix(name, "/")
		if validRedditSubreddit(name) {
			return "https://www.reddit.com/r/" + name + ".rss?limit=1", true
		}
	}
	if postURL, _, ok := redditPostEndpoint(raw); ok {
		parsed, err := url.Parse(postURL)
		if err != nil {
			return "", false
		}
		return "https://www.reddit.com" + strings.TrimSuffix(parsed.Path, "/") + ".rss", true
	}
	return "", false
}

func subredditFromRedditURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := 0; i+1 < len(segments); i++ {
		if strings.EqualFold(segments[i], "r") && validRedditSubreddit(segments[i+1]) {
			return segments[i+1]
		}
	}
	return ""
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
