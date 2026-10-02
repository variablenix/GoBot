package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const (
	youtubeSearchAPIURL  = "https://www.googleapis.com/youtube/v3/search"
	youtubeResultsURL    = "https://www.youtube.com/results"
	youtubeDefaultLength = 320
	youtubeMaxQuery      = 120
)

var youtubeHTTPClient = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

var youtubeDataAssignment = regexp.MustCompile(`(?:\b(?:var|let|const)\s+ytInitialData|window\s*\[\s*["']ytInitialData["']\s*\]|\bytInitialData)\s*=\s*`)

type YouTube struct {
	apiKey    string
	maxLength int
	timeout   time.Duration
}

type youtubeSearchResult struct {
	VideoID      string
	Title        string
	ChannelName  string
	ViewCount    int64
	LikeCount    int64
	HasViewCount bool
	HasLikeCount bool
}

func (p *YouTube) Name() string       { return "youtube" }
func (p *YouTube) Commands() []string { return []string{"yt", "youtube"} }
func (p *YouTube) Help() string {
	return "!yt <search terms> — find one YouTube video or music video and return a short youtu.be link (alias: !youtube; no API key required, API key improves reliability)"
}

func (p *YouTube) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.apiKey = strings.TrimSpace(c.String("api_key", ""))
	p.maxLength = c.Int("max_length", youtubeDefaultLength)
	if p.maxLength < 160 || p.maxLength > 500 {
		p.maxLength = youtubeDefaultLength
	}
	timeoutSeconds := c.Int("timeout_seconds", 10)
	if timeoutSeconds < 3 || timeoutSeconds > 20 {
		timeoutSeconds = 10
	}
	p.timeout = time.Duration(timeoutSeconds) * time.Second
	return nil
}

func (p *YouTube) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "yt" && cmd != "youtube") {
		return false
	}
	query := strings.TrimSpace(arg)
	if query == "" || len([]rune(query)) > youtubeMaxQuery {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !yt <search terms>"))
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	result, err := p.search(ctx, query)
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "YouTube search is temporarily unavailable"))
		return true
	}
	b.Send(m.ReplyTarget(), formatYouTubeSearchResultForTarget(result, p.maxLength, m.ReplyTarget()))
	return true
}

func (p *YouTube) search(ctx context.Context, query string) (youtubeSearchResult, error) {
	if p.apiKey != "" {
		step, cancel := youtubeStepContext(ctx, 2*time.Second)
		result, err := p.searchAPI(step, query)
		cancel()
		if err == nil {
			return result, nil
		}
	}
	step, cancel := youtubeStepContext(ctx, 5*time.Second)
	result, err := p.searchPage(step, query)
	cancel()
	if err == nil {
		return result, nil
	}
	// Public search can return a consent/age-confirmation page with HTTP 200.
	// Use indexed video metadata, not login or age-check circumvention.
	return p.searchIndex(ctx, query)
}

func youtubeStepContext(ctx context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline)/2 < limit {
		limit = time.Until(deadline) / 2
	}
	return context.WithTimeout(ctx, limit)
}

func (p *YouTube) searchAPI(ctx context.Context, query string) (youtubeSearchResult, error) {
	endpoint, err := url.Parse(youtubeSearchAPIURL)
	if err != nil {
		return youtubeSearchResult{}, err
	}
	params := endpoint.Query()
	params.Set("part", "snippet")
	params.Set("maxResults", "1")
	params.Set("q", query)
	params.Set("type", "video")
	params.Set("key", p.apiKey)
	endpoint.RawQuery = params.Encode()

	var response struct {
		Items []struct {
			ID struct {
				VideoID string `json:"videoId"`
			} `json:"id"`
			Snippet struct {
				Title       string `json:"title"`
				ChannelName string `json:"channelTitle"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := p.getJSON(ctx, endpoint.String(), &response); err != nil {
		return youtubeSearchResult{}, err
	}
	for _, item := range response.Items {
		result := youtubeSearchResult{VideoID: item.ID.VideoID, Title: cleanYouTubeText(item.Snippet.Title), ChannelName: cleanYouTubeText(item.Snippet.ChannelName)}
		if validYouTubeSearchResult(result) {
			step, cancel := context.WithTimeout(ctx, time.Second)
			p.addStatistics(step, result.VideoID, &result)
			cancel()
			return result, nil
		}
	}
	return youtubeSearchResult{}, fmt.Errorf("no YouTube video found")
}

// addStatistics enriches a successful search result when the Data API is
// configured. Statistics are deliberately best-effort: a missing statistic
// (for example, likes disabled by the creator) must not turn a useful search
// result into an error.
func (p *YouTube) addStatistics(ctx context.Context, videoID string, result *youtubeSearchResult) {
	endpoint, err := url.Parse("https://www.googleapis.com/youtube/v3/videos")
	if err != nil {
		return
	}
	params := endpoint.Query()
	params.Set("part", "statistics")
	params.Set("id", videoID)
	params.Set("key", p.apiKey)
	endpoint.RawQuery = params.Encode()

	var response struct {
		Items []struct {
			Statistics struct {
				ViewCount string `json:"viewCount"`
				LikeCount string `json:"likeCount"`
			} `json:"statistics"`
		} `json:"items"`
	}
	if err := p.getJSON(ctx, endpoint.String(), &response); err != nil || len(response.Items) == 0 {
		return
	}
	statistics := response.Items[0].Statistics
	if count, ok := parseYouTubeCount(statistics.ViewCount); ok {
		result.ViewCount = count
		result.HasViewCount = true
	}
	if count, ok := parseYouTubeCount(statistics.LikeCount); ok {
		result.LikeCount = count
		result.HasLikeCount = true
	}
}

func parseYouTubeCount(value string) (int64, bool) {
	count, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return count, err == nil && count >= 0
}

func (p *YouTube) searchPage(ctx context.Context, query string) (youtubeSearchResult, error) {
	endpoint, err := url.Parse(youtubeResultsURL)
	if err != nil {
		return youtubeSearchResult{}, err
	}
	params := endpoint.Query()
	params.Set("search_query", query)
	endpoint.RawQuery = params.Encode()
	body, err := youtubeSearchHTML(ctx, endpoint.String())
	if err != nil {
		return youtubeSearchResult{}, err
	}
	return parseYouTubeInitialData(body)
}

func youtubeSearchHTML(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; GoBot YouTube search)")
	res, err := youtubeHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("video search returned HTTP %d", res.StatusCode)
	}
	const maxBytes = 4 << 20
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBytes {
		return nil, fmt.Errorf("video search response exceeds size limit")
	}
	return body, nil
}

func (p *YouTube) searchIndex(ctx context.Context, query string) (youtubeSearchResult, error) {
	endpoint := "https://www.bing.com/search?" + url.Values{
		"q": {"site:youtube.com/watch " + query}, "count": {"8"}, "setlang": {"en-US"},
	}.Encode()
	body, err := youtubeSearchHTML(ctx, endpoint)
	if err != nil {
		return youtubeSearchResult{}, err
	}
	for _, item := range parseBingSearchResults(body) {
		id := youtubeIndexedVideoID(item.URL)
		title := strings.TrimSuffix(strings.TrimSuffix(cleanYouTubeText(item.Title), " - YouTube Music"), " - YouTube")
		result := youtubeSearchResult{VideoID: id, Title: title}
		if !validYouTubeSearchResult(result) {
			continue
		}
		// oEmbed can enrich the public result, but unavailable metadata must
		// not discard an otherwise useful indexed video title and link.
		step, cancel := context.WithTimeout(ctx, time.Second)
		var metadata struct {
			Title  string `json:"title"`
			Author string `json:"author_name"`
		}
		oembed := "https://www.youtube.com/oembed?" + url.Values{"url": {"https://www.youtube.com/watch?v=" + id}, "format": {"json"}}.Encode()
		if p.getJSON(step, oembed, &metadata) == nil && cleanYouTubeText(metadata.Title) != "" {
			result.Title = cleanYouTubeText(metadata.Title)
			result.ChannelName = cleanYouTubeText(metadata.Author)
		}
		cancel()
		return result, nil
	}
	return youtubeSearchResult{}, fmt.Errorf("no indexed YouTube video found")
}

func youtubeIndexedVideoID(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return ""
	}
	var id string
	switch strings.ToLower(u.Host) {
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com":
		if u.Path == "/watch" && len(u.Query()["v"]) == 1 {
			id = u.Query().Get("v")
		}
	case "youtu.be":
		id = strings.TrimPrefix(u.Path, "/")
	}
	if !validYouTubeSearchResult(youtubeSearchResult{VideoID: id, Title: "video"}) {
		return ""
	}
	return id
}

func (p *YouTube) getJSON(ctx context.Context, endpoint string, value interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; YouTube search)")
	res, err := youtubeHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("YouTube returned HTTP %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(value)
}

func parseYouTubeInitialData(body []byte) (youtubeSearchResult, error) {
	assignment := youtubeDataAssignment.FindIndex(body)
	if assignment == nil {
		return youtubeSearchResult{}, fmt.Errorf("YouTube search data not found")
	}
	jsonStart := assignment[1]
	decoder := json.NewDecoder(bytes.NewReader(body[jsonStart:]))
	var data interface{}
	if err := decoder.Decode(&data); err != nil {
		return youtubeSearchResult{}, err
	}
	if result, ok := findYouTubeVideo(data); ok {
		return result, nil
	}
	return youtubeSearchResult{}, fmt.Errorf("no YouTube video found")
}

func findYouTubeVideo(value interface{}) (youtubeSearchResult, bool) {
	switch node := value.(type) {
	case map[string]interface{}:
		// Only search the primary results when the full page envelope exists.
		// Sidebar recommendations must not win over the first organic result.
		if primary, ok := youtubeObject(node, "contents", "twoColumnSearchResultsRenderer", "primaryContents"); ok {
			return findYouTubeVideo(primary)
		}
		for _, kind := range []string{"videoRenderer", "videoWithContextRenderer"} {
			if renderer, ok := node[kind].(map[string]interface{}); ok {
				result := youtubeSearchResult{
					VideoID:     stringValue(renderer["videoId"]),
					Title:       cleanYouTubeText(youtubeText(renderer["title"])),
					ChannelName: cleanYouTubeText(youtubeText(renderer["ownerText"])),
				}
				if result.ChannelName == "" {
					result.ChannelName = cleanYouTubeText(youtubeText(renderer["longBylineText"]))
				}
				if validYouTubeSearchResult(result) {
					return result, true
				}
			}
		}
		if renderer, ok := node["lockupViewModel"].(map[string]interface{}); ok {
			if stringValue(renderer["contentType"]) == "LOCKUP_CONTENT_TYPE_VIDEO" {
				metadata, _ := youtubeObject(renderer, "metadata", "lockupMetadataViewModel")
				result := youtubeSearchResult{VideoID: stringValue(renderer["contentId"]), Title: cleanYouTubeText(youtubeText(metadata["title"]))}
				if validYouTubeSearchResult(result) {
					return result, true
				}
			}
			return youtubeSearchResult{}, false // Do not mine playlist thumbnails.
		}
		keys := make([]string, 0, len(node))
		for key := range node {
			if key == "adSlotRenderer" || key == "promotedSparklesWebRenderer" || key == "secondaryContents" {
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if result, ok := findYouTubeVideo(node[key]); ok {
				return result, true
			}
		}
	case []interface{}:
		for _, child := range node {
			if result, ok := findYouTubeVideo(child); ok {
				return result, true
			}
		}
	}
	return youtubeSearchResult{}, false
}

func youtubeObject(node map[string]interface{}, path ...string) (map[string]interface{}, bool) {
	for _, key := range path {
		var ok bool
		node, ok = node[key].(map[string]interface{})
		if !ok {
			return nil, false
		}
	}
	return node, true
}

func cleanYouTubeText(text string) string { return cleanIMDbText(cleanTitle(text)) }

func youtubeText(value interface{}) string {
	node, ok := value.(map[string]interface{})
	if !ok {
		return ""
	}
	if text := stringValue(node["simpleText"]); text != "" {
		return text
	}
	if text := stringValue(node["content"]); text != "" {
		return text
	}
	runs, ok := node["runs"].([]interface{})
	if !ok {
		return ""
	}
	var parts []string
	for _, run := range runs {
		if runMap, ok := run.(map[string]interface{}); ok {
			if text := stringValue(runMap["text"]); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "")
}

func stringValue(value interface{}) string {
	text, _ := value.(string)
	return text
}

func validYouTubeSearchResult(result youtubeSearchResult) bool {
	if result.Title == "" || len(result.VideoID) != 11 {
		return false
	}
	for _, r := range result.VideoID {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func formatYouTubeSearchResult(result youtubeSearchResult, maxLength int) string {
	return formatYouTubeSearchResultForTarget(result, maxLength, "")
}

func formatYouTubeSearchResultForTarget(result youtubeSearchResult, maxLength int, target string) string {
	result.Title = cleanYouTubeText(result.Title)
	result.ChannelName = cleanYouTubeText(result.ChannelName)
	// Include UTF-8 bytes, IRC color controls and the actual PRIVMSG envelope.
	wireLimit := 512 - len("PRIVMSG "+target+" :\r\n")
	if maxLength <= 0 || maxLength > wireLimit {
		maxLength = wireLimit
	}
	for {
		text := renderYouTubeSearchResult(result)
		if len(text) <= maxLength {
			return text
		}
		switch {
		case result.ChannelName != "":
			result.ChannelName = "" // Preserve title and link ahead of the byline.
		case result.HasViewCount || result.HasLikeCount:
			result.HasViewCount, result.HasLikeCount = false, false
		case result.Title != "":
			budget := len(result.Title) - (len(text) - maxLength)
			result.Title = truncateUTF8Bytes(result.Title, budget)
		default:
			return "https://youtu.be/" + result.VideoID
		}
	}
}

func renderYouTubeSearchResult(result youtubeSearchResult) string {
	link := "https://youtu.be/" + result.VideoID

	header := ircColor(ircRed, "[YouTube]")
	if result.ChannelName != "" {
		header += " " + ircColor(ircYellow, result.ChannelName) + " —"
	}
	if result.Title != "" {
		header += " " + ircColor(ircCyan, result.Title)
	}
	parts := []string{header}
	if result.HasViewCount {
		parts = append(parts, ircColor(ircYellow, "👁 "+formatYouTubeCount(result.ViewCount)+" views"))
	}
	if result.HasLikeCount {
		parts = append(parts, ircColor(ircGreen, "👍 "+formatYouTubeCount(result.LikeCount)+" likes"))
	}
	parts = append(parts, ircColor(ircCyan, link))
	return strings.Join(parts, " | ")
}

func formatYouTubeCount(count int64) string {
	value := strconv.FormatInt(count, 10)
	for i := len(value) - 3; i > 0; i -= 3 {
		value = value[:i] + "," + value[i:]
	}
	return value
}
