package plugins

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Wikipedia struct{ cfg bot.PluginConfig }

func (p *Wikipedia) Name() string                                 { return "wikipedia" }
func (p *Wikipedia) Commands() []string                           { return []string{"wiki", "wikipedia"} }
func (p *Wikipedia) Help() string                                 { return "!wiki <query> — summarize a Wikipedia article" }
func (p *Wikipedia) Init(c bot.PluginConfig, _ *storage.DB) error { p.cfg = c; return nil }
func (p *Wikipedia) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "wiki" && cmd != "wikipedia") {
		return false
	}
	if strings.TrimSpace(arg) == "" {
		b.Send(m.ReplyTarget(), "usage: !wiki <query>")
		return true
	}
	requestCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	x, ok := wikipediaSummary(requestCtx, strings.TrimSpace(arg))
	if !ok {
		b.Send(m.ReplyTarget(), "I couldn't find that Wikipedia article.")
		return true
	}
	summary := strings.Join(strings.Fields(x.Extract), " ")
	if summary == "" {
		b.Send(m.ReplyTarget(), "I couldn't find a summary for that Wikipedia article.")
		return true
	}
	r := []rune(summary)
	max := p.cfg.Int("max_summary_length", 300)
	if len(r) > max {
		summary = string(r[:max]) + "…"
	}
	b.Send(m.ReplyTarget(), summary+" "+x.ContentURLs.Desktop.Page)
	return true
}

type wikipediaSummaryResult struct {
	Title       string `json:"title"`
	Extract     string `json:"extract"`
	ContentURLs struct {
		Desktop struct {
			Page string `json:"page"`
		} `json:"desktop"`
	} `json:"content_urls"`
}

func wikipediaSummary(ctx context.Context, query string) (wikipediaSummaryResult, bool) {
	result, ok := wikipediaSummaryPage(ctx, query)
	if ok {
		return result, true
	}

	searchURL := "https://en.wikipedia.org/w/api.php?action=query&list=search&srsearch=" + url.QueryEscape(query) + "&format=json&utf8=1&srlimit=1"
	req, err := wikipediaRequest(ctx, searchURL)
	if err != nil {
		return wikipediaSummaryResult{}, false
	}
	res, err := apiHTTPClient.Do(req)
	if err != nil {
		return wikipediaSummaryResult{}, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return wikipediaSummaryResult{}, false
	}
	var search struct {
		Query struct {
			Search []struct {
				Title string `json:"title"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := json.NewDecoder(res.Body).Decode(&search); err != nil || len(search.Query.Search) == 0 {
		return wikipediaSummaryResult{}, false
	}
	return wikipediaSummaryPage(ctx, search.Query.Search[0].Title)
}

func wikipediaSummaryPage(ctx context.Context, title string) (wikipediaSummaryResult, bool) {
	endpoint := "https://en.wikipedia.org/api/rest_v1/page/summary/" + url.PathEscape(title)
	req, err := wikipediaRequest(ctx, endpoint)
	if err != nil {
		return wikipediaSummaryResult{}, false
	}
	res, err := apiHTTPClient.Do(req)
	if err != nil {
		return wikipediaSummaryResult{}, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return wikipediaSummaryResult{}, false
	}
	var result wikipediaSummaryResult
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil || result.Extract == "" {
		return wikipediaSummaryResult{}, false
	}
	return result, true
}

func wikipediaRequest(ctx context.Context, endpoint string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "GoBot/1.0 (https://github.com/variablenix/GoBot; IRC bot)")
	req.Header.Set("Accept", "application/json")
	return req, nil
}
