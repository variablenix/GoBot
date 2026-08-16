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

var fallbackDadJokes = []string{
	"I only know 25 letters of the alphabet. I don't know y.",
	"What do you call fake spaghetti? An impasta.",
	"I used to hate facial hair, but then it grew on me.",
	"Why did the scarecrow win an award? Because he was outstanding in his field.",
	"I am reading a book about anti-gravity. It is impossible to put down.",
}

type Dadjoke struct {
	timeout  time.Duration
	cooldown scopedCooldown
	maxLen   int
}

func (p *Dadjoke) Name() string       { return "dadjoke" }
func (p *Dadjoke) Commands() []string { return []string{"dadjoke", "dad", "punchline"} }
func (p *Dadjoke) Help() string {
	return "!dadjoke — fetch a random dad joke (aliases: !dad, !punchline)"
}
func (p *Dadjoke) Init(c bot.PluginConfig, _ *storage.DB) error {
	seconds := c.Int("timeout_seconds", 8)
	if seconds < 1 || seconds > 60 {
		seconds = 8
	}
	p.timeout = time.Duration(seconds) * time.Second
	p.cooldown.configure(c.Int("cooldown_seconds", 10), 10)
	p.maxLen = c.Int("max_length", 360)
	if p.maxLen < 120 || p.maxLen > 400 {
		p.maxLen = 360
	}
	return nil
}
func (p *Dadjoke) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, _, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "dadjoke" && cmd != "dad" && cmd != "punchline") {
		return false
	}
	if !p.cooldown.allow(strings.ToLower(b.Config.NetworkName + "\x00" + m.Target)) {
		return true
	}
	joke := p.fetch()
	b.Send(m.ReplyTarget(), truncateRunes(cleanExternalText(joke), p.maxLen))
	return true
}

func (p *Dadjoke) fetch() string {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://icanhazdadjoke.com/", nil)
	if err == nil {
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot)")
		res, requestErr := apiHTTPClient.Do(req)
		if requestErr == nil {
			defer res.Body.Close()
			if res.StatusCode == http.StatusOK {
				var payload struct {
					Joke string `json:"joke"`
				}
				if json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&payload) == nil && strings.TrimSpace(payload.Joke) != "" {
					return strings.TrimSpace(payload.Joke)
				}
			}
		}
	}
	index, randomErr := secureRandomInt(int64(len(fallbackDadJokes)))
	if randomErr != nil {
		return fallbackDadJokes[0]
	}
	return fallbackDadJokes[index]
}

func formatDadJokeResponse(joke string, maxLength int) string {
	if strings.TrimSpace(joke) == "" {
		return "dad jokes are temporarily unavailable"
	}
	return truncateRunes(cleanExternalText(fmt.Sprint(strings.TrimSpace(joke))), maxLength)
}
