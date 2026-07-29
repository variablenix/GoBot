package plugins

import (
	"context"
	"encoding/json"
	"fmt"
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
	endpoint := "https://newsapi.org/v2/top-headlines?country=us&pageSize=" + url.QueryEscape(fmt.Sprintf("%d", p.cfg.Int("max_results", 3)))
	if strings.TrimSpace(arg) != "" {
		endpoint = "https://newsapi.org/v2/everything?q=" + url.QueryEscape(strings.TrimSpace(arg)) + "&sortBy=publishedAt&pageSize=" + url.QueryEscape(fmt.Sprintf("%d", p.cfg.Int("max_results", 3)))
	}
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
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil || len(data.Articles) == 0 {
		b.Send(m.ReplyTarget(), "no news found")
		return true
	}
	max := p.cfg.Int("max_results", 3)
	if max < 1 {
		max = 1
	}
	if len(data.Articles) > max {
		data.Articles = data.Articles[:max]
	}
	for _, article := range data.Articles {
		if title := cleanTitle(article.Title); title != "" && article.URL != "" {
			b.Send(m.ReplyTarget(), fmt.Sprintf("%s — %s", title, article.URL))
		}
	}
	return true
}
