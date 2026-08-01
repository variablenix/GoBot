package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Urban struct{}

func (p *Urban) Name() string       { return "urban" }
func (p *Urban) Commands() []string { return []string{"urban", "u", "ud"} }
func (p *Urban) Help() string {
	return "!urban <term> [number] — show one Urban Dictionary definition"
}
func (p *Urban) Init(_ bot.PluginConfig, _ *storage.DB) error { return nil }

func (p *Urban) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "urban" && cmd != "u" && cmd != "ud") {
		return false
	}
	term, index := urbanArgs(arg)
	if term == "" {
		b.Send(m.ReplyTarget(), "usage: !urban <term> [number]")
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	endpoint := "https://api.urbandictionary.com/v0/define?term=" + url.QueryEscape(term)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot)")
	res, err := apiHTTPClient.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		if res != nil {
			res.Body.Close()
		}
		b.Send(m.ReplyTarget(), "Urban Dictionary is temporarily unavailable")
		return true
	}
	defer res.Body.Close()
	var data struct {
		List []struct {
			Word       string `json:"word"`
			Definition string `json:"definition"`
			Permalink  string `json:"permalink"`
		} `json:"list"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil || len(data.List) == 0 || index < 1 || index > len(data.List) {
		b.Send(m.ReplyTarget(), "no Urban Dictionary definition found")
		return true
	}
	entry := data.List[index-1]
	definition := truncateRunes(cleanExternalText(entry.Definition), 220)
	if definition == "" {
		definition = "(empty definition)"
	}
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s [%d/%d]: %s — %s", cleanExternalText(entry.Word), index, len(data.List), definition, cleanExternalText(entry.Permalink)))
	return true
}

func urbanArgs(arg string) (string, int) {
	parts := strings.Fields(arg)
	if len(parts) == 0 {
		return "", 1
	}
	index := 1
	if len(parts) > 1 {
		if n, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			index = n
			parts = parts[:len(parts)-1]
		}
	}
	return strings.Join(parts, " "), index
}
