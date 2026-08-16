package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

// Cats returns a short cat fact from the public catfact.ninja endpoint. It is
// deliberately a fact lookup rather than an image download so the reply stays
// small and useful in both graphical and terminal IRC clients.
type Cats struct{}

func (p *Cats) Name() string       { return "cats" }
func (p *Cats) Commands() []string { return []string{"cats", "cat"} }
func (p *Cats) Help() string {
	return "!cats — show a short cat fact (alias: !cat; no API key required)"
}
func (p *Cats) Init(_ bot.PluginConfig, _ *storage.DB) error { return nil }

func (p *Cats) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, _, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "cats" && cmd != "cat") {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://catfact.ninja/fact?max_length=180", nil)
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "cat facts are temporarily unavailable"))
		return true
	}
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot)")
	res, err := apiHTTPClient.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		if res != nil {
			res.Body.Close()
		}
		b.Send(m.ReplyTarget(), ircColor(ircRed, "cat facts are temporarily unavailable"))
		return true
	}
	defer res.Body.Close()
	var data struct {
		Fact string `json:"fact"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64*1024)).Decode(&data); err != nil || strings.TrimSpace(data.Fact) == "" {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "cat fact could not be parsed"))
		return true
	}
	fact := truncateRunes(cleanExternalText(data.Fact), 220)
	b.Send(m.ReplyTarget(), fmt.Sprintf("🐱 %s: %s", ircColor(ircCyan, "Cat fact"), fact))
	return true
}
