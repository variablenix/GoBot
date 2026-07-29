package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
	"go.uber.org/zap"
	"golang.org/x/net/html"
)

type URLTitle struct {
	cfg    bot.PluginConfig
	rx     *regexp.Regexp
	client *http.Client
}

func (p *URLTitle) Name() string       { return "urltitle" }
func (p *URLTitle) Commands() []string { return nil }
func (p *URLTitle) Help() string       { return "posts the title of URLs shared in a channel" }
func (p *URLTitle) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.cfg = c
	p.rx = regexp.MustCompile(`https?://[^\s<>]+`)
	p.client = &http.Client{
		Timeout: time.Duration(c.Int("timeout_seconds", 5)) * time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if !publicHTTPURL(req.URL) {
				return fmt.Errorf("redirect target is not a public HTTP URL")
			}
			return nil
		},
	}
	return nil
}

func (p *URLTitle) Handle(b *bot.Bot, m bot.Message) bool {
	if !m.IsChannel {
		return false
	}
	u := p.rx.FindString(m.Text)
	if u == "" {
		return false
	}
	parsed, err := url.Parse(u)
	if err != nil || !publicHTTPURL(parsed) {
		return false
	}
	go func() {
		requestCtx, cancel := context.WithTimeout(context.Background(), p.client.Timeout)
		defer cancel()
		req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return
		}
		req.Header.Set("User-Agent", "GoBot/1.0 (+IRC URL title fetcher)")
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		start := time.Now()
		res, err := p.client.Do(req)
		if err != nil {
			if title, ok := redditTitle(requestCtx, p.client, parsed); ok {
				b.Send(m.Target, formatURLTitle(title, parsed, p.cfg.Int("max_title_length", 120)))
			}
			return
		}
		defer res.Body.Close()
		if time.Since(start) > 2*time.Second {
			b.Log.Warn("slow URL fetch", zap.String("url", u), zap.Duration("latency", time.Since(start)))
		}
		body, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
		if err != nil {
			return
		}
		title := pageTitle(body)
		var youtubeChannel, youtubeDuration string
		// Prefer Reddit's metadata endpoint for Reddit URLs. It returns the
		// actual post title even when the normal HTML page is a shell, a
		// bot-check page, or an access-denied document.
		if isRedditHost(parsed.Hostname()) {
			if reddit, ok := redditTitle(requestCtx, p.client, parsed); ok {
				title = reddit
			}
		}
		if title == "" || titleLooksLikeError(title) {
			return
		}
		if isYouTubeHost(parsed.Hostname()) {
			// Keep metadata lookup independent from the page-fetch timeout. Some
			// YouTube pages are slow enough that oEmbed/player requests would
			// otherwise inherit an already-expired context after the title fetch.
			metadataCtx, metadataCancel := context.WithTimeout(context.Background(), p.client.Timeout)
			metadata, ok := youtubeMetadata(metadataCtx, p.client, parsed, body)
			metadataCancel()
			if ok {
				title = metadata.title
				youtubeChannel = metadata.channel
				youtubeDuration = metadata.duration
			}
		}
		if title == "" {
			return
		}
		if isYouTubeHost(parsed.Hostname()) {
			b.Send(m.Target, formatYouTubeTitle(title, youtubeChannel, youtubeDuration, p.cfg.Int("max_title_length", 120)))
			return
		}
		b.Send(m.Target, formatURLTitle(title, parsed, p.cfg.Int("max_title_length", 120)))
	}()
	return false
}

func formatURLTitle(title string, parsed *url.URL, maxLength int) string {
	displaySource := parsed.Host
	if isYouTubeHost(parsed.Hostname()) {
		if shortURL, ok := shortYouTubeDisplayURL(parsed); ok {
			displaySource = shortURL
		}
	}
	return fmt.Sprintf("[ %s — %s ]", truncateRunes(title, maxLength), displaySource)
}

func formatYouTubeTitle(title, channel, duration string, maxLength int) string {
	result := "[YouTube] " + truncateRunes(title, maxLength)
	if channel != "" {
		result += " | Channel: " + truncateRunes(channel, 80)
	}
	if duration != "" {
		result += " | " + duration
	}
	return result
}

func pageTitle(body []byte) string {
	titles := map[string]string{}
	z := html.NewTokenizer(strings.NewReader(string(body)))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			if string(name) != "meta" || !hasAttr {
				continue
			}
			var property, content string
			for {
				key, value, more := z.TagAttr()
				switch strings.ToLower(string(key)) {
				case "property", "name":
					property = strings.ToLower(string(value))
				case "content":
					content = string(value)
				}
				if !more {
					break
				}
			}
			if content != "" && (property == "og:title" || property == "twitter:title") {
				titles[property] = content
			}
		}
	}
	if title := cleanTitle(titles["og:title"]); title != "" {
		return title
	}
	if title := cleanTitle(titles["twitter:title"]); title != "" {
		return title
	}
	z = html.NewTokenizer(strings.NewReader(string(body)))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt == html.StartTagToken {
			name, _ := z.TagName()
			if string(name) == "title" {
				var parts []string
				for {
					tt = z.Next()
					if tt == html.TextToken {
						parts = append(parts, string(z.Text()))
					} else if tt == html.EndTagToken || tt == html.ErrorToken {
						return cleanTitle(strings.Join(parts, " "))
					}
				}
			}
		}
	}
	return ""
}

func cleanTitle(title string) string {
	return strings.Join(strings.Fields(html.UnescapeString(title)), " ")
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}

func titleLooksLikeError(title string) bool {
	title = strings.ToLower(cleanTitle(title))
	for _, phrase := range []string{
		"access denied",
		"403 forbidden",
		"404 not found",
		"security verification",
		"just a moment",
		"checking your browser",
		"enable javascript and cookies",
	} {
		if strings.Contains(title, phrase) {
			return true
		}
	}
	return false
}

func isYouTubeHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "youtube.com" || strings.HasSuffix(host, ".youtube.com") || host == "youtu.be"
}

func youtubeTitle(ctx context.Context, client *http.Client, videoURL *url.URL) (string, bool) {
	oembed := "https://www.youtube.com/oembed?url=" + url.QueryEscape(videoURL.String()) + "&format=json"
	return oembedTitle(ctx, client, oembed)
}

type youtubeMetadataResult struct {
	title, channel, duration string
}

func youtubeMetadata(ctx context.Context, client *http.Client, videoURL *url.URL, body []byte) (youtubeMetadataResult, bool) {
	oembed := "https://www.youtube.com/oembed?url=" + url.QueryEscape(videoURL.String()) + "&format=json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, oembed, nil)
	if err != nil {
		return youtubeMetadataResult{}, false
	}
	req.Header.Set("User-Agent", "GoBot/1.0 (+IRC URL title fetcher)")
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return youtubeMetadataResult{}, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return youtubeMetadataResult{}, false
	}
	var data struct {
		Title      string `json:"title"`
		AuthorName string `json:"author_name"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return youtubeMetadataResult{}, false
	}
	result := youtubeMetadataResult{
		title:    cleanTitle(data.Title),
		channel:  cleanTitle(data.AuthorName),
		duration: youtubeDuration(body),
	}
	if result.duration == "" {
		result.duration = youtubePlayerDuration(ctx, client, videoURL)
	}
	if result.channel == "" {
		result.channel = cleanTitle(youtubeJSONStringField(body, "ownerChannelName"))
	}
	if result.channel == "" {
		result.channel = cleanTitle(youtubeJSONStringField(body, "author"))
	}
	return result, result.title != ""
}

func youtubeDuration(body []byte) string {
	seconds := youtubeJSONField(body, "lengthSeconds")
	if seconds == "" {
		return youtubeMetaDuration(body)
	}
	value, err := strconv.Atoi(seconds)
	if err != nil || value < 1 {
		return ""
	}
	hours := value / 3600
	minutes := (value % 3600) / 60
	remaining := value % 60
	if hours > 0 {
		if remaining > 0 {
			return fmt.Sprintf("%dh %dm %ds", hours, minutes, remaining)
		}
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, remaining)
	}
	return fmt.Sprintf("%ds", remaining)
}

// youtubePlayerDuration uses YouTube's public player endpoint when the HTML
// page does not include lengthSeconds. This is common for consent, Music,
// Shorts, and other pages whose metadata is rendered dynamically.
func youtubePlayerDuration(ctx context.Context, client *http.Client, videoURL *url.URL) string {
	videoID, ok := youtubeVideoID(videoURL)
	if !ok {
		return ""
	}
	requestBody := fmt.Sprintf(`{"context":{"client":{"clientName":"ANDROID","clientVersion":"20.10.38"}},"videoId":%q}`, videoID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://www.youtube.com/youtubei/v1/player?prettyPrint=false", strings.NewReader(requestBody))
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "com.google.android.youtube/20.10.38 (Linux; U; Android 14)")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 512<<10))
	if err != nil {
		return ""
	}
	return youtubeDuration(body)
}

func youtubeVideoID(u *url.URL) (string, bool) {
	if u == nil || !isYouTubeHost(u.Hostname()) {
		return "", false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "youtu.be" {
		id := strings.Trim(strings.TrimSpace(u.Path), "/")
		return id, id != ""
	}
	if id := strings.TrimSpace(u.Query().Get("v")); id != "" {
		return id, true
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 && (parts[0] == "shorts" || parts[0] == "embed" || parts[0] == "live") {
		id := strings.TrimSpace(parts[1])
		return id, id != ""
	}
	return "", false
}

// YouTube has used several valid HTML/JSON layouts over time. Keep duration
// extraction tolerant of whitespace, numeric-vs-string JSON values, and meta
// attribute order so it works consistently for regular videos, Music videos,
// Shorts, and other YouTube-hosted video pages.
func youtubeMetaDuration(body []byte) string {
	z := html.NewTokenizer(strings.NewReader(string(body)))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return ""
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		name, hasAttr := z.TagName()
		if string(name) != "meta" || !hasAttr {
			continue
		}
		var itemprop, content string
		for {
			key, value, more := z.TagAttr()
			switch strings.ToLower(string(key)) {
			case "itemprop":
				itemprop = strings.ToLower(strings.TrimSpace(string(value)))
			case "content":
				content = strings.TrimSpace(string(value))
			}
			if !more {
				break
			}
		}
		if itemprop != "duration" || !strings.HasPrefix(strings.ToUpper(content), "PT") {
			continue
		}
		if duration := formatISO8601Duration(strings.TrimPrefix(strings.ToUpper(content), "PT")); duration != "" {
			return duration
		}
	}
}

func formatISO8601Duration(value string) string {
	match := regexp.MustCompile(`^(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?$`).FindStringSubmatch(value)
	if len(match) == 0 {
		return ""
	}
	hours, minutes, seconds := 0, 0, 0
	if match[1] != "" {
		hours, _ = strconv.Atoi(match[1])
	}
	if match[2] != "" {
		minutes, _ = strconv.Atoi(match[2])
	}
	if match[3] != "" {
		seconds, _ = strconv.Atoi(match[3])
	}
	return formatYouTubeDurationParts(hours, minutes, seconds)
}

func formatYouTubeDurationParts(hours, minutes, seconds int) string {
	if hours > 0 {
		if seconds > 0 {
			return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
		}
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	if seconds > 0 {
		return fmt.Sprintf("%ds", seconds)
	}
	return ""
}

func youtubeJSONField(body []byte, field string) string {
	pattern := regexp.MustCompile(`"` + regexp.QuoteMeta(field) + `"\s*:\s*"?([0-9]+)"?`)
	match := pattern.FindSubmatch(body)
	if len(match) == 2 {
		return string(match[1])
	}
	return ""
}

func youtubeJSONStringField(body []byte, field string) string {
	pattern := regexp.MustCompile(`"` + regexp.QuoteMeta(field) + `":"((?:\\.|[^"\\])*)"`)
	match := pattern.FindSubmatch(body)
	if len(match) != 2 {
		return ""
	}
	var value string
	if err := json.Unmarshal([]byte(`"`+string(match[1])+`"`), &value); err != nil {
		return ""
	}
	return value
}

func redditTitle(ctx context.Context, client *http.Client, postURL *url.URL) (string, bool) {
	if !isRedditHost(postURL.Hostname()) {
		return "", false
	}
	oembed := "https://www.reddit.com/oembed?url=" + url.QueryEscape(postURL.String()) + "&format=json"
	return oembedTitle(ctx, client, oembed)
}

func oembedTitle(ctx context.Context, client *http.Client, endpoint string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("User-Agent", "GoBot/1.0 (+IRC URL title fetcher)")
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return "", false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", false
	}
	var data struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return "", false
	}
	title := cleanTitle(data.Title)
	return title, title != ""
}

func isRedditHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "reddit.com" || strings.HasSuffix(host, ".reddit.com") || host == "redd.it"
}

func shortYouTubeDisplayURL(u *url.URL) (string, bool) {
	if u == nil {
		return "", false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	switch {
	case host == "youtu.be":
		id := strings.Trim(strings.TrimSpace(u.Path), "/")
		if id == "" {
			return "", false
		}
		return "youtu.be/" + id, true
	case host == "youtube.com", strings.HasSuffix(host, ".youtube.com"):
		queryID := strings.TrimSpace(u.Query().Get("v"))
		if queryID != "" {
			return "youtu.be/" + queryID, true
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 {
			switch parts[0] {
			case "shorts", "embed", "live":
				if parts[1] != "" {
					return "youtu.be/" + parts[1], true
				}
			}
		}
	}
	return "", false
}

func publicHTTPURL(u *url.URL) bool {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return publicIP(ip)
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return false
		}
	}
	return true
}

func publicIP(ip net.IP) bool {
	return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !ip.IsMulticast()
}
