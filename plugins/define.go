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

type Define struct{ cfg bot.PluginConfig }

func (p *Define) Name() string       { return "define" }
func (p *Define) Commands() []string { return []string{"define", "def", "dictionary"} }
func (p *Define) Help() string {
	return "!define <word> — show a short English definition (aliases: !def, !dictionary; no API key required)"
}
func (p *Define) Init(c bot.PluginConfig, _ *storage.DB) error { p.cfg = c; return nil }

func (p *Define) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || !isDefineCommand(cmd) {
		return false
	}
	term := strings.TrimSpace(arg)
	if !validDefinitionTerm(term) {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !define <English word>"))
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), definitionTimeout(p.cfg))
	defer cancel()
	entry, ok := dictionaryEntry(ctx, term)
	if !ok {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "no English definition found"))
		return true
	}
	maxLength := p.cfg.Int("max_length", 240)
	if maxLength < 80 {
		maxLength = 80
	}
	definition := truncateRunes(cleanExternalText(entry.Definition), maxLength)
	part := cleanExternalText(entry.PartOfSpeech)
	if part != "" {
		part = " (" + part + ")"
	}
	b.Send(m.ReplyTarget(), fmt.Sprintf("📖 %s%s: %s", cleanExternalText(entry.Word), part, definition))
	return true
}

type dictionaryEntryResult struct {
	Word         string
	PartOfSpeech string
	Definition   string
}

func dictionaryEntry(ctx context.Context, term string) (dictionaryEntryResult, bool) {
	endpoint := "https://api.dictionaryapi.dev/api/v2/entries/en/" + url.PathEscape(strings.ToLower(term))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return dictionaryEntryResult{}, false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot)")
	res, err := apiHTTPClient.Do(req)
	if err != nil {
		return dictionaryEntryResult{}, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return dictionaryEntryResult{}, false
	}
	var entries []struct {
		Word     string `json:"word"`
		Meanings []struct {
			PartOfSpeech string `json:"partOfSpeech"`
			Definitions  []struct {
				Definition string `json:"definition"`
			} `json:"definitions"`
		} `json:"meanings"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 512*1024)).Decode(&entries); err != nil {
		return dictionaryEntryResult{}, false
	}
	for _, entry := range entries {
		for _, meaning := range entry.Meanings {
			for _, definition := range meaning.Definitions {
				if strings.TrimSpace(definition.Definition) != "" {
					word := entry.Word
					if word == "" {
						word = term
					}
					return dictionaryEntryResult{Word: word, PartOfSpeech: meaning.PartOfSpeech, Definition: definition.Definition}, true
				}
			}
		}
	}
	return dictionaryEntryResult{}, false
}

func isDefineCommand(command string) bool {
	switch strings.ToLower(command) {
	case "define", "def", "dictionary":
		return true
	default:
		return false
	}
}

func validDefinitionTerm(term string) bool {
	if term == "" || len([]rune(term)) > 64 || strings.ContainsAny(term, "\r\n\t") {
		return false
	}
	return true
}

func definitionTimeout(c bot.PluginConfig) time.Duration {
	seconds := c.Int("timeout_seconds", 8)
	if seconds < 1 || seconds > 30 {
		seconds = 8
	}
	return time.Duration(seconds) * time.Second
}
