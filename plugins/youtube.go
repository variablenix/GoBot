package plugins

import (
	"bytes"
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

const (
	youtubeSearchAPIURL  = "https://www.googleapis.com/youtube/v3/search"
	youtubeResultsURL    = "https://www.youtube.com/results"
	youtubeDefaultLength = 320
	youtubeMaxQuery      = 120
)

var youtubeHTTPClient = &http.Client{Timeout: 10 * time.Second}

type YouTube struct {
	apiKey    string
	maxLength int
	timeout   time.Duration
}

type youtubeSearchResult struct {
	VideoID     string
	Title       string
	ChannelName string
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
	b.Send(m.ReplyTarget(), formatYouTubeSearchResult(result, p.maxLength))
	return true
}

func (p *YouTube) search(ctx context.Context, query string) (youtubeSearchResult, error) {
	if p.apiKey != "" {
		if result, err := p.searchAPI(ctx, query); err == nil {
			return result, nil
		}
	}
	return p.searchPage(ctx, query)
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
		result := youtubeSearchResult{VideoID: item.ID.VideoID, Title: cleanTitle(item.Snippet.Title), ChannelName: cleanTitle(item.Snippet.ChannelName)}
		if validYouTubeSearchResult(result) {
			return result, nil
		}
	}
	return youtubeSearchResult{}, fmt.Errorf("no YouTube video found")
}

func (p *YouTube) searchPage(ctx context.Context, query string) (youtubeSearchResult, error) {
	endpoint, err := url.Parse(youtubeResultsURL)
	if err != nil {
		return youtubeSearchResult{}, err
	}
	params := endpoint.Query()
	params.Set("search_query", query)
	endpoint.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return youtubeSearchResult{}, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; GoBot YouTube search)")
	res, err := youtubeHTTPClient.Do(req)
	if err != nil {
		return youtubeSearchResult{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return youtubeSearchResult{}, fmt.Errorf("YouTube returned HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return youtubeSearchResult{}, err
	}
	return parseYouTubeInitialData(body)
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
	marker := []byte("var ytInitialData = ")
	start := bytes.Index(body, marker)
	if start < 0 {
		return youtubeSearchResult{}, fmt.Errorf("YouTube search data not found")
	}
	jsonStart := start + len(marker)
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
		if renderer, ok := node["videoRenderer"].(map[string]interface{}); ok {
			result := youtubeSearchResult{
				VideoID:     stringValue(renderer["videoId"]),
				Title:       cleanTitle(youtubeText(renderer["title"])),
				ChannelName: cleanTitle(youtubeText(renderer["ownerText"])),
			}
			if result.ChannelName == "" {
				result.ChannelName = cleanTitle(youtubeText(renderer["longBylineText"]))
			}
			if validYouTubeSearchResult(result) {
				return result, true
			}
		}
		for _, child := range node {
			if result, ok := findYouTubeVideo(child); ok {
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

func youtubeText(value interface{}) string {
	node, ok := value.(map[string]interface{})
	if !ok {
		return ""
	}
	if text := stringValue(node["simpleText"]); text != "" {
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
	if result.Title == "" || result.VideoID == "" || len(result.VideoID) > 32 {
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
	link := "https://youtu.be/" + result.VideoID
	prefix := "YouTube "
	if result.ChannelName != "" {
		prefix += result.ChannelName + " - "
	}
	suffix := " URL " + link
	text := prefix + result.Title
	if maxLength > 0 && len([]rune(text+suffix)) > maxLength {
		available := maxLength - len([]rune(prefix+suffix))
		if available > 1 {
			text = prefix + truncateRunes(result.Title, available)
		} else {
			text = prefix
		}
	}
	return text + suffix
}
