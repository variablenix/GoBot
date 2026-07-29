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
	if !ok || cmd != "wiki" {
		return false
	}
	if strings.TrimSpace(arg) == "" {
		b.Send(m.ReplyTarget(), "usage: !wiki <query>")
		return true
	}
	requestCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, "https://en.wikipedia.org/api/rest_v1/page/summary/"+url.PathEscape(arg), nil)
	res, err := apiHTTPClient.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		b.Send(m.ReplyTarget(), "I couldn't find that Wikipedia article.")
		return true
	}
	defer res.Body.Close()
	var x struct {
		Extract     string `json:"extract"`
		ContentURLs struct {
			Desktop struct {
				Page string `json:"page"`
			} `json:"desktop"`
		} `json:"content_urls"`
	}
	json.NewDecoder(res.Body).Decode(&x)
	summary := strings.Join(strings.Fields(x.Extract), " ")
	r := []rune(summary)
	max := p.cfg.Int("max_summary_length", 300)
	if len(r) > max {
		summary = string(r[:max]) + "…"
	}
	b.Send(m.ReplyTarget(), summary+" "+x.ContentURLs.Desktop.Page)
	return true
}
