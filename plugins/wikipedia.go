package plugins

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

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
	searchTerm := wikipediaSearchTerm(query)
	if searchTerm == "" {
		searchTerm = strings.TrimSpace(query)
	}

	// Try the cleaned topic first. This avoids sending a full conversational
	// question to the page endpoint, where an accidental redirect can look like
	// a successful but unrelated answer.
	if result, ok := wikipediaSummaryPage(ctx, searchTerm); ok && wikipediaTitleMatches(searchTerm, result.Title) {
		return result, true
	}

	searchURL := "https://en.wikipedia.org/w/api.php?action=query&list=search&srsearch=" + url.QueryEscape(searchTerm) + "&format=json&utf8=1&srlimit=8"
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
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&search); err != nil || len(search.Query.Search) == 0 {
		return wikipediaSummaryResult{}, false
	}
	for _, candidate := range search.Query.Search {
		if !wikipediaTitleMatches(searchTerm, candidate.Title) {
			continue
		}
		if result, ok := wikipediaSummaryPage(ctx, candidate.Title); ok {
			return result, true
		}
	}
	return wikipediaSummaryResult{}, false
}

// wikipediaSearchTerm turns common conversational question forms into a
// focused topic. Wikipedia's search endpoint otherwise tends to rank an
// incidental word in a question above the article the user meant.
func wikipediaSearchTerm(query string) string {
	words := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(query)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	stop := map[string]struct{}{
		"a": {}, "about": {}, "an": {}, "and": {}, "are": {}, "can": {},
		"could": {}, "current": {}, "currently": {}, "define": {}, "did": {},
		"do": {}, "does": {}, "exactly": {}, "explain": {}, "for": {},
		"happening": {}, "how": {}, "in": {}, "is": {}, "it": {}, "latest": {},
		"me": {}, "more": {}, "now": {}, "of": {}, "on": {}, "please": {},
		"recent": {}, "tell": {}, "the": {}, "to": {}, "today": {}, "was": {},
		"what": {}, "when": {}, "where": {}, "who": {}, "why": {}, "would": {},
		"you": {}, "know": {},
	}
	filtered := make([]string, 0, len(words))
	for _, word := range words {
		if _, ignored := stop[word]; !ignored {
			filtered = append(filtered, word)
		}
	}
	return strings.Join(filtered, " ")
}

// wikipediaTitleMatches prevents a loosely related first search result from
// being presented as an answer. One-word topics must appear in the title. For
// multi-word topics, require two matching words, or the first topic word (the
// usual subject) to match. This keeps useful lookups such as "TLS protect"
// while rejecting "UFO disclosure" -> "Disclosure Day (soundtrack)".
func wikipediaTitleMatches(topic, title string) bool {
	topicWords := strings.Fields(wikipediaSearchTerm(topic))
	titleWords := strings.Fields(wikipediaSearchTerm(title))
	if len(topicWords) == 0 || len(titleWords) == 0 {
		return false
	}
	titleSet := make(map[string]struct{}, len(titleWords))
	for _, word := range titleWords {
		titleSet[word] = struct{}{}
	}
	matches := 0
	for _, word := range topicWords {
		if _, ok := titleSet[word]; ok {
			matches++
		}
	}
	if len(topicWords) == 1 {
		return matches == 1
	}
	return matches >= 2 || func() bool {
		_, ok := titleSet[topicWords[0]]
		return ok
	}()
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
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&result); err != nil || result.Extract == "" {
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
