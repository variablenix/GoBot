package plugins

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

// Ask provides a small, source-grounded question lookup. It intentionally has
// DuckDuckGo Instant Answers are primary and exact Wikidata entities are the
// fallback.
type Ask struct {
	cfg         bot.PluginConfig
	cfgMu       sync.RWMutex
	mu          sync.Mutex
	last        map[string]time.Time
	lastWarning map[string]time.Time
}

func (p *Ask) Name() string       { return "ask" }
func (p *Ask) Commands() []string { return []string{"ask", "question", "q"} }
func (p *Ask) Help() string {
	return "!ask <question> — DuckDuckGo Instant Answer with Wikidata fallback (aliases: !question, !q)"
}

func (p *Ask) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.setConfig(c)
	p.last = make(map[string]time.Time)
	p.lastWarning = make(map[string]time.Time)
	return nil
}

// Reload applies ask configuration without resetting sender cooldown state.
func (p *Ask) Reload(c bot.PluginConfig) error {
	p.setConfig(c)
	p.mu.Lock()
	if p.last == nil {
		p.last = make(map[string]time.Time)
	}
	if p.lastWarning == nil {
		p.lastWarning = make(map[string]time.Time)
	}
	p.mu.Unlock()
	return nil
}

func (p *Ask) setConfig(c bot.PluginConfig) {
	p.cfgMu.Lock()
	p.cfg = c
	p.cfgMu.Unlock()
}

func (p *Ask) configSnapshot() bot.PluginConfig {
	p.cfgMu.RLock()
	defer p.cfgMu.RUnlock()
	cfg := make(bot.PluginConfig, len(p.cfg))
	for key, value := range p.cfg {
		cfg[key] = value
	}
	return cfg
}

func (p *Ask) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || !isAskCommand(cmd) {
		return false
	}
	target := m.ReplyTarget()
	question := strings.TrimSpace(arg)
	if question == "" {
		b.Send(target, ircColor(ircYellow, "usage: !ask <question>"))
		return true
	}
	if strings.ContainsAny(question, "\r\n") {
		b.Send(target, ircColor(ircYellow, "ask needs one question on one line"))
		return true
	}
	question = cleanExternalText(question)
	if question == "" {
		b.Send(target, ircColor(ircYellow, "ask needs one question on one line"))
		return true
	}
	if len([]rune(question)) > 200 {
		b.Send(target, ircColor(ircYellow, "ask questions are limited to 200 characters"))
		return true
	}

	key := askSenderKey(m)
	cfg := p.configSnapshot()
	if !p.allow(key, cfg.Int("cooldown_seconds", 15)) {
		if p.allowWarning(key) {
			b.Send(target, "ask is cooling down — please wait a moment")
		}
		return true
	}

	if localAnswer, ok := askLocalAnswer(question); ok {
		b.Send(target, formatAskResponse(m.Nick, localAnswer, "", cfg.Int("max_length", 360), cfg.Int("max_response_chars", 240)))
		return true
	}

	timeout := cfg.Int("timeout_seconds", 8)
	if timeout < 1 {
		timeout = 1
	}
	if timeout > 30 {
		timeout = 30
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	source, found := p.findSource(ctx, question, cfg)
	if !found {
		b.Send(target, "I couldn't find a reliable answer for that question.")
		return true
	}
	b.Send(target, formatAskResponse(m.Nick, source.Summary, source.URL, cfg.Int("max_length", 360), cfg.Int("max_response_chars", 240)))
	return true
}

func isAskCommand(command string) bool {
	return command == "ask" || command == "question" || command == "q"
}

func askSenderKey(m bot.Message) string {
	if account := strings.TrimSpace(m.Account); account != "" {
		return "account:" + strings.ToLower(account)
	}
	return "sender:" + strings.ToLower(strings.Join([]string{m.Nick, m.User, m.Host}, "\x00"))
}

func (p *Ask) allow(key string, cooldown int) bool {
	if cooldown <= 0 {
		return true
	}
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if previous, ok := p.last[key]; ok && now.Sub(previous) < time.Duration(cooldown)*time.Second {
		return false
	}
	p.last[key] = now
	return true
}

func (p *Ask) allowWarning(key string) bool {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if previous, ok := p.lastWarning[key]; ok && now.Sub(previous) < 10*time.Second {
		return false
	}
	p.lastWarning[key] = now
	return true
}

type askSource struct {
	Title   string
	Summary string
	URL     string
}

func (p *Ask) findSource(ctx context.Context, question string, cfg bot.PluginConfig) (askSource, bool) {
	focused := askFocusedTerm(question)
	if cfg.Bool("duckduckgo_enabled", true) {
		if source, ok := askDuckDuckGo(ctx, question); ok {
			return source, true
		}
		if focused != "" && !strings.EqualFold(focused, strings.TrimSpace(question)) {
			if source, ok := askDuckDuckGo(ctx, focused); ok {
				return source, true
			}
		}
	}
	if cfg.Bool("wikidata_fallback", true) && focused != "" && !askNeedsRelationshipAnswer(question) {
		if source, ok := askWikidata(ctx, focused); ok {
			return source, true
		}
	}
	return askSource{}, false
}

func askLocalAnswer(question string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(question))
	normalized = strings.Trim(normalized, " \t\r\n?%!.")
	switch normalized {
	case "hello", "hi", "hey", "hello bot", "hi bot", "hey bot":
		return "Hello!", true
	default:
		return "", false
	}
}

func askFocusedTerm(query string) string {
	words := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(query)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	stop := map[string]struct{}{
		"a": {}, "about": {}, "an": {}, "and": {}, "are": {}, "can": {},
		"could": {}, "current": {}, "currently": {}, "define": {}, "did": {},
		"do": {}, "does": {}, "exactly": {}, "explain": {}, "for": {},
		"from": {}, "happening": {}, "how": {}, "in": {}, "is": {}, "it": {},
		"latest": {}, "me": {}, "more": {}, "now": {}, "of": {}, "on": {},
		"please": {}, "recent": {}, "tell": {}, "the": {}, "to": {},
		"today": {}, "was": {}, "what": {}, "when": {}, "where": {},
		"who": {}, "why": {}, "would": {}, "you": {}, "your": {}, "know": {},
		"actor": {}, "bio": {}, "biography": {}, "career": {}, "comedy": {},
		"comedian": {}, "details": {}, "facts": {}, "history": {},
		"information": {}, "language": {}, "learn": {}, "life": {}, "meaning": {},
		"programming": {}, "profile": {}, "s": {}, "start": {}, "started": {},
		"teach": {}, "tutorial": {}, "use": {}, "using": {}, "want": {},
		"work": {}, "write": {}, "build": {}, "begin": {}, "beginner": {},
		"code": {}, "coding": {}, "created": {}, "creator": {}, "get": {}, "help": {},
	}
	focused := make([]string, 0, len(words))
	for _, word := range words {
		if _, ignored := stop[word]; !ignored {
			focused = append(focused, word)
		}
	}
	return strings.Join(focused, " ")
}

func askNeedsRelationshipAnswer(question string) bool {
	words := strings.FieldsFunc(strings.ToLower(question), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	hasWho := false
	for _, word := range words {
		if word == "who" {
			hasWho = true
			continue
		}
		switch word {
		case "created", "creator", "developed", "discovered", "founded", "invented", "made", "wrote", "directed", "designed", "owns":
			if hasWho {
				return true
			}
		}
	}
	return false
}

type duckDuckGoAnswer struct {
	Heading          string `json:"Heading"`
	AbstractText     string `json:"AbstractText"`
	AbstractURL      string `json:"AbstractURL"`
	AbstractSource   string `json:"AbstractSource"`
	Answer           string `json:"Answer"`
	Definition       string `json:"Definition"`
	DefinitionURL    string `json:"DefinitionURL"`
	DefinitionSource string `json:"DefinitionSource"`
}

var askHTTPClient = &http.Client{Timeout: 15 * time.Second}

func askDuckDuckGo(ctx context.Context, question string) (askSource, bool) {
	endpoint := "https://api.duckduckgo.com/?q=" + url.QueryEscape(question) + "&format=json&no_html=1&skip_disambig=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return askSource{}, false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; DuckDuckGo Instant Answer lookup)")
	res, err := askHTTPClient.Do(req)
	if err != nil {
		return askSource{}, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return askSource{}, false
	}
	var payload duckDuckGoAnswer
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&payload); err != nil {
		return askSource{}, false
	}
	if summary := cleanExternalText(payload.Answer); summary != "" {
		return askSource{Title: cleanExternalText(payload.Heading), Summary: summary, URL: duckDuckGoSearchURL(question)}, true
	}
	if summary := cleanExternalText(payload.Definition); summary != "" && !wikipediaBackedSource(payload.DefinitionSource, payload.DefinitionURL) {
		return askSource{Title: cleanExternalText(payload.Heading), Summary: summary, URL: payload.DefinitionURL}, true
	}
	if summary := cleanExternalText(payload.AbstractText); summary != "" && validHTTPURL(payload.AbstractURL) && !wikipediaBackedSource(payload.AbstractSource, payload.AbstractURL) {
		return askSource{Title: cleanExternalText(payload.Heading), Summary: summary, URL: payload.AbstractURL}, true
	}
	return askSource{}, false
}

func duckDuckGoSearchURL(query string) string {
	return "https://duckduckgo.com/?q=" + url.QueryEscape(strings.TrimSpace(query))
}

func wikipediaBackedSource(source, rawURL string) bool {
	if strings.EqualFold(strings.TrimSpace(source), "wikipedia") {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "wikipedia.org" || strings.HasSuffix(host, ".wikipedia.org")
}

type wikidataSearchResponse struct {
	Search []wikidataEntity `json:"search"`
}

type wikidataEntity struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Match       struct {
		Text string `json:"text"`
	} `json:"match"`
}

func askWikidata(ctx context.Context, query string) (askSource, bool) {
	endpoint := "https://www.wikidata.org/w/api.php?action=wbsearchentities&search=" + url.QueryEscape(query) + "&language=en&uselang=en&format=json&limit=8"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return askSource{}, false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (https://github.com/variablenix/GoBot; IRC bot)")
	res, err := askHTTPClient.Do(req)
	if err != nil {
		return askSource{}, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return askSource{}, false
	}
	var payload wikidataSearchResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 512<<10)).Decode(&payload); err != nil {
		return askSource{}, false
	}
	entity, ok := bestWikidataEntity(query, payload.Search)
	if !ok {
		return askSource{}, false
	}
	return askSource{Title: entity.Label, Summary: entity.Description, URL: "https://www.wikidata.org/wiki/" + entity.ID}, true
}

func bestWikidataEntity(query string, entities []wikidataEntity) (wikidataEntity, bool) {
	query = strings.ToLower(strings.TrimSpace(query))
	queryWords := strings.Fields(query)
	if query == "" || len(queryWords) == 0 {
		return wikidataEntity{}, false
	}
	bestScore := -1
	var best wikidataEntity
	for index, entity := range entities {
		if !validWikidataID(entity.ID) || strings.TrimSpace(entity.Description) == "" {
			continue
		}
		label := strings.ToLower(strings.TrimSpace(entity.Label))
		match := strings.ToLower(strings.TrimSpace(entity.Match.Text))
		labelWords := strings.Fields(label)
		matches := 0
		labelSet := make(map[string]struct{}, len(labelWords))
		for _, word := range labelWords {
			labelSet[word] = struct{}{}
		}
		for _, word := range queryWords {
			if _, ok := labelSet[word]; ok {
				matches++
			}
		}
		if len(queryWords) > 1 && matches < len(queryWords) {
			continue
		}
		score := matches * 10
		if label == query {
			score += 1000
		}
		if match == query {
			score += 500
		}
		if matches == len(queryWords) {
			score += 100
		}
		score -= index
		if score > bestScore {
			bestScore = score
			best = entity
		}
	}
	return best, bestScore >= 10
}

func validWikidataID(id string) bool {
	if len(id) < 2 || id[0] != 'Q' {
		return false
	}
	for _, r := range id[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validHTTPURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func formatAskResponse(nick, answer, sourceURL string, maxLength, maxResponseChars int) string {
	maxLength = clampAskLength(maxLength, 120, 450, 360)
	maxResponseChars = clampAskLength(maxResponseChars, 80, 320, 240)
	nick = cleanExternalText(nick)
	answer = truncateAsk(answer, maxResponseChars)
	sourceURL = cleanExternalText(strings.TrimSpace(sourceURL))
	if !validHTTPURL(sourceURL) {
		sourceURL = ""
	}
	prefix := ""
	if nick != "" {
		prefix = nick + ": "
	}
	suffix := ""
	if sourceURL != "" {
		suffix = " — Read more: " + sourceURL
	}
	result := prefix + answer + suffix
	if len(result) <= maxLength && utf8.RuneCountInString(result) <= maxLength && len(result) <= 450 {
		return result
	}
	available := maxLength - len([]byte(prefix)) - len([]byte(suffix))
	if available < 1 {
		return truncateAskBytes(prefix+answer, maxLength)
	}
	answer = truncateAskBytes(answer, available)
	result = prefix + answer + suffix
	if len(result) > 450 {
		result = truncateAskBytes(result, 450)
	}
	return result
}

func clampAskLength(value, min, max, fallback int) int {
	if value < min || value > max {
		return fallback
	}
	return value
}

func truncateAsk(text string, max int) string {
	text = cleanExternalText(text)
	if max < 1 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	if max == 1 {
		return "…"
	}
	return strings.TrimSpace(string(runes[:max-1])) + "…"
}

func truncateAskBytes(text string, maxBytes int) string {
	text = cleanExternalText(text)
	if maxBytes < 1 {
		return ""
	}
	if len([]byte(text)) <= maxBytes {
		return text
	}
	const ellipsis = "…"
	if maxBytes <= len([]byte(ellipsis)) {
		return string([]rune(text)[:1])
	}
	limit := maxBytes - len([]byte(ellipsis))
	var out []rune
	used := 0
	for _, r := range text {
		runeBytes := len(string(r))
		if used+runeBytes > limit {
			break
		}
		out = append(out, r)
		used += runeBytes
	}
	return strings.TrimSpace(string(out)) + ellipsis
}
