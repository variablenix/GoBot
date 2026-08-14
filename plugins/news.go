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

type News struct{ cfg bot.PluginConfig }

func (p *News) Name() string       { return "news" }
func (p *News) Commands() []string { return []string{"news"} }
func (p *News) Help() string {
	return "!news [query] — show current headlines (requires a NewsAPI key)"
}
func (p *News) Init(c bot.PluginConfig, _ *storage.DB) error { p.cfg = c; return nil }
func (p *News) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "news" {
		return false
	}
	if p.cfg.String("api_key", "") == "" {
		b.Send(m.ReplyTarget(), "news is not configured")
		return true
	}
	max := p.cfg.Int("max_results", 3)
	if max < 1 {
		max = 1
	}
	endpoint := newsEndpoint(arg, max)
	requestCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		b.Send(m.ReplyTarget(), "news lookup is temporarily unavailable")
		return true
	}
	req.Header.Set("X-Api-Key", p.cfg.String("api_key", ""))
	res, err := apiHTTPClient.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		if res != nil {
			res.Body.Close()
		}
		b.Send(m.ReplyTarget(), "news lookup is temporarily unavailable")
		return true
	}
	defer res.Body.Close()
	var data struct {
		Articles []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"articles"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&data); err != nil || len(data.Articles) == 0 {
		b.Send(m.ReplyTarget(), "no news found")
		return true
	}
	if len(data.Articles) > max {
		data.Articles = data.Articles[:max]
	}
	items := make([]string, 0, len(data.Articles))
	for _, article := range data.Articles {
		if title := cleanTitle(article.Title); title != "" && article.URL != "" {
			items = append(items, fmt.Sprintf("%s — %s", title, cleanExternalText(article.URL)))
		}
	}
	if len(items) == 0 {
		b.Send(m.ReplyTarget(), "no news found")
		return true
	}
	maxLength := p.cfg.Int("max_length", 360)
	if maxLength < 160 || maxLength > 600 {
		maxLength = 360
	}
	b.Send(m.ReplyTarget(), formatNewsItems(items, maxLength))
	return true
}

func formatNewsItems(items []string, maxLength int) string {
	return truncateIRCMessage(strings.Join(items, " | "), maxLength)
}

func newsEndpoint(query string, maxResults int) string {
	if maxResults < 1 {
		maxResults = 1
	}
	// Ask for extra results so filtering or unavailable articles do not
	// unnecessarily reduce the number of English headlines returned.
	pageSize := maxResults * 3
	if pageSize > 100 {
		pageSize = 100
	}
	if strings.TrimSpace(query) == "" {
		return "https://newsapi.org/v2/top-headlines?country=us&pageSize=" + url.QueryEscape(fmt.Sprintf("%d", pageSize))
	}
	return "https://newsapi.org/v2/everything?q=" + url.QueryEscape(strings.TrimSpace(query)) + "&language=en&sortBy=publishedAt&pageSize=" + url.QueryEscape(fmt.Sprintf("%d", pageSize))
}
