package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type XKCD struct{}

func (p *XKCD) Name() string                                 { return "xkcd" }
func (p *XKCD) Commands() []string                           { return []string{"xkcd"} }
func (p *XKCD) Help() string                                 { return "!xkcd [number|latest] — show an XKCD title, date, and link" }
func (p *XKCD) Init(_ bot.PluginConfig, _ *storage.DB) error { return nil }

func (p *XKCD) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "xkcd" {
		return false
	}
	arg = strings.TrimSpace(arg)
	endpoint := "https://xkcd.com/info.0.json"
	if arg != "" && !strings.EqualFold(arg, "latest") {
		n, err := strconv.Atoi(arg)
		if err != nil || n < 1 || n > 100000 {
			b.Send(m.ReplyTarget(), "usage: !xkcd [number|latest]")
			return true
		}
		endpoint = fmt.Sprintf("https://xkcd.com/%d/info.0.json", n)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot)")
	res, err := apiHTTPClient.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		if res != nil {
			res.Body.Close()
		}
		b.Send(m.ReplyTarget(), "XKCD is temporarily unavailable")
		return true
	}
	defer res.Body.Close()
	var comic struct {
		Num                     int `json:"num"`
		Title, Year, Month, Day string
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 128<<10)).Decode(&comic); err != nil || comic.Num < 1 || comic.Title == "" {
		b.Send(m.ReplyTarget(), "XKCD data could not be parsed")
		return true
	}
	b.Send(m.ReplyTarget(), fmt.Sprintf("🎨 xkcd #%d: %s (%s-%s-%s) — https://xkcd.com/%d/", comic.Num, cleanExternalText(comic.Title), cleanExternalText(comic.Year), cleanExternalText(comic.Month), cleanExternalText(comic.Day), comic.Num))
	return true
}
