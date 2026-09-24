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

	xhtml "golang.org/x/net/html"

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
	entry, ok := lookupDefinition(ctx, term)
	if !ok {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "No definition available: the word may be missing or the dictionary services unavailable. Try !wiki "+cleanExternalText(term)))
		return true
	}
	maxLength := p.cfg.Int("max_length", 240)
	if maxLength < 80 || maxLength > 400 {
		maxLength = 240
	}
	definition := truncateRunes(cleanExternalText(entry.Definition), maxLength)
	part := cleanExternalText(entry.PartOfSpeech)
	if part != "" {
		part = " (" + part + ")"
	}
	message := fmt.Sprintf("📖 %s%s: %s", cleanExternalText(entry.Word), part, definition)
	if entry.URL != "" {
		message += " — " + entry.URL
	}
	b.Send(m.ReplyTarget(), message)
	return true
}

type dictionaryEntryResult struct {
	Word         string
	PartOfSpeech string
	Definition   string
	URL          string
}

// Reserve time for an independent provider when the primary is unreachable.
func lookupDefinition(ctx context.Context, term string) (dictionaryEntryResult, bool) {
	budget := 2 * time.Second
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline)/3 < budget {
		budget = time.Until(deadline) / 3
	}
	primary, cancel := context.WithTimeout(ctx, budget)
	entry, ok := dictionaryEntry(primary, term)
	cancel()
	if ok {
		return entry, true
	}
	return wiktionaryEntry(ctx, term)
}

func wiktionaryEntry(ctx context.Context, term string) (dictionaryEntryResult, bool) {
	endpoint := "https://en.wiktionary.org/api/rest_v1/page/definition/" + url.PathEscape(term)
	req, err := wikipediaRequest(ctx, endpoint)
	if err != nil {
		return dictionaryEntryResult{}, false
	}
	res, err := apiHTTPClient.Do(req)
	if err != nil {
		return dictionaryEntryResult{}, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return dictionaryEntryResult{}, false
	}
	var entries map[string][]struct {
		PartOfSpeech string `json:"partOfSpeech"`
		Definitions  []struct {
			Definition string `json:"definition"`
		} `json:"definitions"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 512*1024)).Decode(&entries) != nil {
		return dictionaryEntryResult{}, false
	}
	for _, entry := range entries["en"] {
		for _, definition := range entry.Definitions {
			node, err := xhtml.Parse(strings.NewReader(definition.Definition))
			if err != nil {
				continue
			}
			text := cleanExternalText(askHTMLText(node))
			if text != "" {
				return dictionaryEntryResult{Word: term, PartOfSpeech: entry.PartOfSpeech, Definition: text, URL: "https://en.wiktionary.org/wiki/" + url.PathEscape(term) + "#English"}, true
			}
		}
	}
	return dictionaryEntryResult{}, false
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
