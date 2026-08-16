package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	osexec "os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/chromedp/chromedp"
	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
	xhtml "golang.org/x/net/html"
)

// Ask provides a small, source-grounded question lookup. DuckDuckGo Search
// Assist is primary, with the rendered browser path, Instant Answers, and
// exact Wikidata entities as fallbacks.
type Ask struct {
	cfg         bot.PluginConfig
	cfgMu       sync.RWMutex
	mu          sync.Mutex
	last        map[string]time.Time
	lastWarning map[string]time.Time
	cache       map[string]askCachedSource
}

type askCachedSource struct {
	source  askSource
	expires time.Time
}

func (p *Ask) Name() string       { return "ask" }
func (p *Ask) Commands() []string { return []string{"ask", "question", "q"} }
func (p *Ask) Help() string {
	return "!ask <question> — DuckDuckGo Search Assist with sourced result excerpts and Instant Answer/Wikidata fallbacks (aliases: !question, !q)"
}

func (p *Ask) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.setConfig(c)
	p.last = make(map[string]time.Time)
	p.lastWarning = make(map[string]time.Time)
	p.cache = make(map[string]askCachedSource)
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
	if p.cache == nil {
		p.cache = make(map[string]askCachedSource)
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

	cacheSeconds := cfg.Int("cache_seconds", 300)
	if source, ok := p.cached(question, cacheSeconds); ok {
		b.Send(target, formatAskResponse(m.Nick, source.Summary, source.URL, cfg.Int("max_length", 360), cfg.Int("max_response_chars", 240)))
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
		b.Send(target, formatAskNoAnswer(question, cfg.Int("max_length", 360)))
		return true
	}
	p.cacheSource(question, source, cacheSeconds)
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

func normalizeAskCacheKey(question string) string {
	question = cleanExternalText(question)
	question = strings.Trim(question, " \t\r\n?!.,;:")
	return strings.ToLower(strings.Join(strings.Fields(question), " "))
}

func (p *Ask) cached(question string, cacheSeconds int) (askSource, bool) {
	if cacheSeconds <= 0 {
		return askSource{}, false
	}
	if cacheSeconds > 3600 {
		cacheSeconds = 3600
	}
	key := normalizeAskCacheKey(question)
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.cache[key]
	if !ok || now.After(entry.expires) {
		if ok {
			delete(p.cache, key)
		}
		return askSource{}, false
	}
	return entry.source, true
}

func (p *Ask) cacheSource(question string, source askSource, cacheSeconds int) {
	if cacheSeconds <= 0 {
		return
	}
	if cacheSeconds > 3600 {
		cacheSeconds = 3600
	}
	key := normalizeAskCacheKey(question)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cache == nil {
		p.cache = make(map[string]askCachedSource)
	}
	p.cache[key] = askCachedSource{source: source, expires: time.Now().Add(time.Duration(cacheSeconds) * time.Second)}
}

type askSource struct {
	Title    string
	Summary  string
	URL      string
	Provider string
}

func (p *Ask) findSource(ctx context.Context, question string, cfg bot.PluginConfig) (askSource, bool) {
	focused := askFocusedTerm(question)
	webResultTried := false
	var deferredWebResult askSource
	deferredWebResultFound := false
	if cfg.Bool("search_assist_enabled", true) {
		stepCtx, cancel := askStepContext(ctx, 1500*time.Millisecond)
		if source, ok := askDuckDuckGoSearchAssist(stepCtx, question); ok {
			cancel()
			return source, true
		}
		cancel()
		if cfg.Bool("search_assist_browser_enabled", true) {
			fetchResults := cfg.Bool("search_results_enabled", true)
			webResultTried = fetchResults
			stepCtx, cancel = askStepContext(ctx, 3200*time.Millisecond)
			if source, ok := askDuckDuckGoRenderedSearchAssist(stepCtx, question, cfg.String("browser_path", ""), fetchResults); ok {
				cancel()
				if source.Provider == "search_result" {
					deferredWebResult = source
					deferredWebResultFound = true
				} else {
					return source, true
				}
			} else {
				cancel()
			}
		}
	}
	if cfg.Bool("duckduckgo_enabled", true) {
		stepCtx, cancel := askStepContext(ctx, 1600*time.Millisecond)
		if source, ok := askDuckDuckGoWithRetry(stepCtx, question); ok {
			if refined, ok := refineFocusedAskSource(question, source); ok {
				cancel()
				return refined, true
			}
			if !askNeedsRelationshipAnswer(question) && !askNeedsTemporalAnswer(question) {
				cancel()
				return source, true
			}
		}
		if focused != "" && !strings.EqualFold(focused, strings.TrimSpace(question)) {
			if source, ok := askDuckDuckGoWithRetry(stepCtx, focused); ok {
				if source, ok = refineFocusedAskSource(question, source); ok {
					cancel()
					return source, true
				}
				if !askNeedsRelationshipAnswer(question) && !askNeedsTemporalAnswer(question) {
					cancel()
					return source, true
				}
			}
		}
		cancel()
	}
	// Open-ended opinion and explanation questions are poorly served by
	// entity descriptions. Prefer a bounded, attributed web-result excerpt
	// before falling back to Wikidata for those queries.
	if deferredWebResultFound && askNeedsWebResultAnswer(question) {
		return deferredWebResult, true
	}
	if cfg.Bool("wikidata_fallback", true) && focused != "" {
		stepCtx, cancel := askStepContext(ctx, 2500*time.Millisecond)
		switch {
		case askNeedsRelationshipAnswer(question):
			if source, ok := askWikidataRelationship(stepCtx, focused, question); ok {
				cancel()
				return source, true
			}
		case askNeedsTemporalAnswer(question):
			if source, ok := askWikidataTemporal(stepCtx, focused, question); ok {
				cancel()
				return source, true
			}
		default:
			if source, ok := askWikidata(stepCtx, focused); ok {
				cancel()
				return source, true
			}
		}
		cancel()
	}
	if deferredWebResultFound {
		return deferredWebResult, true
	}
	if cfg.Bool("search_results_enabled", true) && !webResultTried && cfg.Bool("search_assist_browser_enabled", true) {
		stepCtx, cancel := askStepContext(ctx, 2800*time.Millisecond)
		if source, ok := askDuckDuckGoRenderedSearchAssist(stepCtx, question, cfg.String("browser_path", ""), true); ok {
			cancel()
			return source, true
		}
		cancel()
	}
	return askSource{}, false
}

func askStepContext(parent context.Context, max time.Duration) (context.Context, context.CancelFunc) {
	if max <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, max)
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
	normalized := strings.ToLower(strings.TrimSpace(query))
	normalized = strings.ReplaceAll(normalized, "who's", "who is")
	for _, phrase := range askQueryFramingPhrases {
		normalized = strings.ReplaceAll(normalized, phrase, " ")
	}
	words := strings.FieldsFunc(normalized, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	stop := map[string]struct{}{
		"a": {}, "about": {}, "after": {}, "ago": {}, "an": {}, "and": {}, "are": {}, "around": {}, "at": {}, "before": {}, "been": {}, "began": {}, "born": {}, "by": {}, "can": {}, "came": {}, "come": {}, "comes": {},
		"could": {}, "current": {}, "currently": {}, "define": {}, "did": {},
		"date": {}, "debut": {}, "debuted": {}, "do": {}, "does": {}, "during": {}, "era": {}, "established": {}, "event": {}, "exactly": {}, "explain": {}, "for": {},
		"from": {}, "happening": {}, "has": {}, "have": {}, "how": {}, "in": {}, "is": {}, "it": {},
		"into": {}, "introduced": {}, "latest": {}, "me": {}, "more": {}, "month": {}, "now": {}, "of": {}, "on": {},
		"please": {}, "recent": {}, "tell": {}, "the": {}, "to": {},
		"today": {}, "was": {}, "what": {}, "when": {}, "where": {},
		"who": {}, "why": {}, "would": {}, "you": {}, "your": {}, "know": {},
		"actor": {}, "bio": {}, "biography": {}, "career": {}, "comedy": {},
		"comedian": {}, "details": {}, "facts": {}, "history": {},
		"author": {}, "designer": {}, "founder": {}, "inventor": {}, "owner": {},
		"information": {}, "language": {}, "launch": {}, "launched": {}, "learn": {}, "life": {}, "made": {}, "meaning": {},
		"age": {}, "old": {}, "origin": {}, "originated": {}, "out": {}, "period": {}, "premiere": {}, "premiered": {}, "profile": {}, "programming": {}, "published": {}, "publication": {}, "release": {}, "released": {}, "s": {}, "since": {}, "start": {}, "started": {},
		"teach": {}, "tutorial": {}, "use": {}, "using": {}, "want": {},
		"which": {}, "with": {}, "work": {}, "write": {}, "build": {}, "begin": {}, "beginner": {}, "exist": {}, "existed": {},
		"code": {}, "coding": {}, "created": {}, "creator": {}, "first": {}, "appear": {}, "appeared": {}, "appearance": {}, "announced": {}, "available": {}, "broadcast": {}, "deployed": {}, "emerge": {}, "emerged": {}, "existence": {}, "form": {}, "formed": {}, "get": {}, "help": {}, "originally": {}, "public": {}, "publish": {},
		"time": {}, "year": {}, "years": {},
	}
	focused := make([]string, 0, len(words))
	for _, word := range words {
		if _, ignored := stop[word]; !ignored {
			focused = append(focused, word)
		}
	}
	return strings.Join(focused, " ")
}

// askQueryFramingPhrases is a local synonym lexicon for question framing. It
// is deliberately data-driven and bounded: runtime dictionary or thesaurus
// calls would add latency and another external failure point to every lookup.
var askQueryFramingPhrases = []string{
	"how many years ago", "how long has", "how long ago", "what is the age of",
	"what was the age of", "what year did", "what year was", "what year were",
	"which year did", "which year was", "which year were", "what date did", "what date was", "what date were",
	"come into existence", "came into existence", "comes into existence", "first came out",
	"first come out", "first appeared", "first appear", "first debuted", "first premiered", "was first released", "were first released",
	"was released", "were released", "release date", "publication date", "date of", "year of",
}

func askNeedsRelationshipAnswer(question string) bool {
	lowerQuestion := strings.ToLower(question)
	lowerQuestion = strings.ReplaceAll(lowerQuestion, "who's", "who is")
	words := strings.FieldsFunc(lowerQuestion, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	hasPersonQuestion := strings.Contains(lowerQuestion, "which person") || strings.Contains(lowerQuestion, "what person") || strings.Contains(lowerQuestion, "what company") || strings.Contains(lowerQuestion, "which company") || strings.Contains(lowerQuestion, "what organization") || strings.Contains(lowerQuestion, "which organization") || strings.Contains(lowerQuestion, "what group") || strings.Contains(lowerQuestion, "which group")
	for _, word := range words {
		if word == "who" || word == "whom" || word == "whose" {
			hasPersonQuestion = true
		}
	}
	if !hasPersonQuestion {
		return false
	}
	for _, word := range words {
		switch word {
		case "author", "authored", "created", "creator", "developed", "designed", "discovered", "founded", "founder", "invented", "inventor", "made", "owns", "owner", "wrote", "written":
			return true
		}
	}
	return false
}

func askNeedsTemporalAnswer(question string) bool {
	if askNeedsRelationshipAnswer(question) {
		return false
	}
	lowerQuestion := strings.ToLower(question)
	for _, phrase := range askTemporalPhrases {
		if strings.Contains(lowerQuestion, phrase) {
			return true
		}
	}
	words := strings.FieldsFunc(lowerQuestion, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	for _, word := range words {
		switch word {
		case "birth", "born", "created", "creation", "date", "debut", "debuted", "established", "founded", "formed", "launched", "launch", "originated", "premiere", "premiered", "published", "publication", "released", "release", "start", "started", "begin", "began", "unveiled", "when", "year", "years":
			return true
		}
	}
	return false
}

func askNeedsWebResultAnswer(question string) bool {
	lower := strings.ToLower(strings.TrimSpace(question))
	for _, phrase := range []string{
		"why ", "why is ", "why are ", "why was ", "why were ",
		"how does ", "how do ", "how can ", "how should ",
		"what makes ", "what do people think", "why do people ", "why do some ",
		"what are the disadvantages", "what are the benefits", "what are the alternatives",
		"is it true", "is it worth", "is it safe", "should i ", "should we ",
		"considered", "controversial", "opinion", "review", "experience",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

var askTemporalPhrases = []string{
	"come out", "came out", "comes out", "come into existence", "came into existence", "comes into existence",
	"first appear", "first appeared", "first debuted", "first premiered", "first released", "release date", "publication date",
	"when did", "when was", "when were", "since when", "as of when", "what date", "which date", "what month",
	"which month", "what year", "which year", "how long ago", "how long has", "how many years ago", "how old is", "how old was", "how old were", "age of", "what age",
}

var (
	askByNamePattern   = regexp.MustCompile(`\bby\s+([A-Z][A-Za-z.'-]*(?:\s+[A-Z][A-Za-z.'-]*){0,4})`)
	askDatePattern     = regexp.MustCompile(`\b(?:[0-9]{1,2}\s+(?:January|February|March|April|May|June|July|August|September|October|November|December)\s+[0-9]{4}|(?:January|February|March|April|May|June|July|August|September|October|November|December)\s+[0-9]{1,2},?\s+[0-9]{4}|[0-9]{4})\b`)
	askYearOnlyPattern = regexp.MustCompile(`^[0-9]{4}$`)
)

func refineFocusedAskSource(question string, source askSource) (askSource, bool) {
	if askNeedsRelationshipAnswer(question) {
		match := askByNamePattern.FindStringSubmatch(source.Summary)
		if len(match) < 2 {
			return askSource{}, false
		}
		name := cleanExternalText(strings.TrimRight(strings.TrimSpace(match[1]), ".,;:!?"))
		title := cleanExternalText(source.Title)
		if name == "" || title == "" {
			return askSource{}, false
		}
		return askSource{
			Title:   title,
			Summary: fmt.Sprintf("%s is credited with %s %s.", name, askRelationshipGerund(question), title),
			URL:     source.URL,
		}, true
	}
	if askNeedsTemporalAnswer(question) {
		date := askDatePattern.FindString(source.Summary)
		title := cleanExternalText(source.Title)
		if date == "" || title == "" {
			return askSource{}, false
		}
		return askSource{
			Title:   title,
			Summary: formatAskTemporalSummary(title, question, date),
			URL:     source.URL,
		}, true
	}
	return source, true
}

func askRelationshipGerund(question string) string {
	words := strings.FieldsFunc(strings.ToLower(question), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	for _, word := range words {
		switch word {
		case "author":
			return "writing"
		case "developed":
			return "developing"
		case "designed":
			return "designing"
		case "discovered":
			return "discovering"
		case "founded", "founder":
			return "founding"
		case "invented", "inventor":
			return "inventing"
		case "made":
			return "making"
		case "owns", "owner":
			return "owning"
		case "wrote":
			return "writing"
		case "directed":
			return "directing"
		}
	}
	return "creating"
}

func askTemporalPhrase(question string) string {
	lowerQuestion := strings.ToLower(question)
	if strings.Contains(lowerQuestion, "how long ago") || strings.Contains(lowerQuestion, "how long has") || strings.Contains(lowerQuestion, "how many years ago") || strings.Contains(lowerQuestion, "how old is") || strings.Contains(lowerQuestion, "how old was") || strings.Contains(lowerQuestion, "how old were") || strings.Contains(lowerQuestion, "what age") || strings.Contains(lowerQuestion, "age of") {
		return "dates to"
	}
	if strings.Contains(lowerQuestion, "come out") || strings.Contains(lowerQuestion, "release") {
		return "was first released"
	}
	if strings.Contains(lowerQuestion, "debut") || strings.Contains(lowerQuestion, "premier") || strings.Contains(lowerQuestion, "first appeared") {
		return "first appeared"
	}
	if strings.Contains(lowerQuestion, "publish") {
		return "was published"
	}
	if strings.Contains(lowerQuestion, "originat") {
		return "originated"
	}
	if strings.Contains(lowerQuestion, "unveil") || strings.Contains(lowerQuestion, "introduc") {
		return "was introduced"
	}
	words := strings.FieldsFunc(lowerQuestion, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	for _, word := range words {
		switch word {
		case "birth", "born":
			return "was born"
		case "created":
			return "was created"
		case "established":
			return "was established"
		case "founded":
			return "was founded"
		case "launched", "launch":
			return "was launched"
		case "formed":
			return "was formed"
		case "began", "begin":
			return "began"
		case "start", "started":
			return "started"
		}
	}
	return "was first released"
}

func formatAskTemporalSummary(title, question, date string) string {
	phrase := askTemporalPhrase(question)
	if phrase == "dates to" {
		return fmt.Sprintf("%s dates to %s.", title, date)
	}
	preposition := "in"
	if strings.Contains(date, " ") && !askYearOnlyPattern.MatchString(date) && (date[0] >= '0' && date[0] <= '9' || strings.Contains(date, ",")) {
		preposition = "on"
	}
	return fmt.Sprintf("%s %s %s %s.", title, phrase, preposition, date)
}

type duckDuckGoAnswer struct {
	Heading      string `json:"Heading"`
	AbstractText string `json:"AbstractText"`
	Answer       string `json:"Answer"`
	Definition   string `json:"Definition"`
}

var askHTTPClient = &http.Client{Timeout: 15 * time.Second}

// askWebHTTPClient fetches only public web pages selected from DuckDuckGo
// results. Its resolver rejects loopback, private, link-local, multicast, and
// unspecified addresses so a search result cannot turn !ask into an SSRF
// primitive. Redirects are validated again before following them.
var askWebHTTPClient = newAskWebHTTPClient()

// DuckDuckGo only includes its Search Assist preload on browser-shaped HTML
// responses. Search Assist is a web feature rather than a documented API, so
// keep this request isolated from the transparent user agent used by the
// public Instant Answer endpoint below.
const askSearchAssistUserAgent = "Mozilla/5.0"

type duckDuckGoSearchAssistPayload struct {
	InstantAnswers []struct {
		Data struct {
			Answer  string `json:"answer"`
			Sources []struct {
				Article struct {
					Link string `json:"link"`
					Site string `json:"site"`
					Text string `json:"text"`
				} `json:"article"`
			} `json:"sources"`
		} `json:"data"`
	} `json:"instantAnswers"`
}

var askSearchAssistScriptPattern = regexp.MustCompile(`(?s)<script[^>]*id=["']deep_preload_script["'][^>]*src=["']([^"']+)["']`)
var askSearchAssistOpinionPattern = regexp.MustCompile(`(?i)^why\s+should\s+(?:someone|somebody|people|users?|we|you|they)\s+not\s+use\s+(.+?)\s*[?!.]*$`)
var askSearchAssistGenrePattern = regexp.MustCompile(`(?i)^what\s+(?:(?:music|musical)\s+)?genre\s+is\s+(?:the\s+)?(?:band|artist|group)\s+(.+?)\s*[?!.]*$`)

type askRenderedSearchAssistData struct {
	Text  string   `json:"text"`
	Links []string `json:"links"`
}

type askRenderedSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

const askRenderedSearchAssistScript = `() => {
  const textOf = (element) => (element.innerText || element.textContent || "").trim();
  const headers = Array.from(document.querySelectorAll("body *")).filter((element) => {
    const text = textOf(element);
    return text === "Search Assist" || text.startsWith("Search Assist\n");
  });
  if (!headers.length) return null;
  let card = headers.sort((a, b) => textOf(a).length - textOf(b).length)[0];
  while (card.parentElement && textOf(card.parentElement).length < 3500) card = card.parentElement;
  const links = Array.from(card.querySelectorAll("a[href]"))
    .map((anchor) => anchor.href)
    .filter((href) => /^https?:\/\//i.test(href));
  const clone = card.cloneNode(true);
  clone.querySelectorAll("a, button, [role=button]").forEach((element) => element.remove());
  return {text: textOf(clone), links};
}`

// DuckDuckGo changes its result-card markup periodically. The selector list
// intentionally covers the current data-testid markup and the older HTML
// result classes, while requiring a title link and an absolute HTTP(S) URL.
const askRenderedSearchResultsScript = `() => {
  const textOf = (element) => (element.innerText || element.textContent || "").trim();
  const unwrap = (href) => {
    try {
      const parsed = new URL(href, location.href);
      const isDuckDuckGo = parsed.hostname === "duckduckgo.com" || parsed.hostname.endsWith(".duckduckgo.com");
      if (isDuckDuckGo && parsed.pathname.startsWith("/l/")) {
        const target = parsed.searchParams.get("uddg");
        if (target) return decodeURIComponent(target);
      }
      return parsed.href;
    } catch (_) { return ""; }
  };
  const selectors = [
    '[data-testid="result"]',
    'article[data-testid="result"]',
    '.result',
    'article.result'
  ];
  const cards = [];
  const seen = new Set();
  selectors.forEach((selector) => document.querySelectorAll(selector).forEach((card) => {
    if (!seen.has(card)) { seen.add(card); cards.push(card); }
  }));
  const results = [];
  cards.forEach((card) => {
    const link = card.querySelector('a[data-testid="result-title-a"], a.result__a, h2 a');
    if (!link) return;
    const url = unwrap(link.href);
    if (!/^https?:\/\//i.test(url)) return;
    const title = textOf(link);
    const snippetNode = card.querySelector('[data-testid="result-snippet"], [data-result="snippet"], .result__snippet');
    const snippet = snippetNode ? textOf(snippetNode) : "";
    if (!title && !snippet) return;
    results.push({title, url, snippet});
  });
  return results.length ? results.slice(0, 8) : null;
}`

func askDuckDuckGoSearchAssist(ctx context.Context, question string) (askSource, bool) {
	// Search Assist can return its web container before the generated answer is
	// available. Try one intent-preserving framing variant when the wording is
	// indirect, then use the normal fallbacks while keeping the sender cooldown
	// and request context limits.
	queries := askSearchAssistQueryVariants(question)
	for attempt := 0; attempt < 2; attempt++ {
		query := queries[0]
		if attempt < len(queries) {
			query = queries[attempt]
		}
		if source, ok := askDuckDuckGoSearchAssistOnce(ctx, query, duckDuckGoSearchURL(question)); ok {
			return source, true
		}
		if err := ctx.Err(); err != nil {
			break
		}
	}
	return askSource{}, false
}

func askSearchAssistQueryVariants(question string) []string {
	original := strings.TrimSpace(question)
	queries := []string{original}
	match := askSearchAssistOpinionPattern.FindStringSubmatch(strings.TrimRight(original, " \t\r\n"))
	if len(match) == 2 {
		subject := strings.TrimSpace(match[1])
		if subject != "" {
			queries = append(queries, "what are the disadvantages of "+subject+"?")
		}
	}
	match = askSearchAssistGenrePattern.FindStringSubmatch(original)
	if len(match) == 2 {
		subject := strings.Trim(strings.TrimSpace(match[1]), `"'`)
		if subject != "" {
			queries = append(queries, "what genres does "+subject+" have?")
		}
	}
	return queries
}

func askDuckDuckGoSearchAssistOnce(ctx context.Context, question, fallbackURL string) (askSource, bool) {
	pageURL := duckDuckGoSearchURL(question)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return askSource{}, false
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("User-Agent", askSearchAssistUserAgent)
	res, err := askHTTPClient.Do(req)
	if err != nil {
		return askSource{}, false
	}
	defer res.Body.Close()
	if !askHTTPSuccess(res.StatusCode) {
		return askSource{}, false
	}
	page, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return askSource{}, false
	}
	matches := askSearchAssistScriptPattern.FindSubmatch(page)
	if len(matches) < 2 {
		return askSource{}, false
	}
	assistURL := html.UnescapeString(string(matches[1]))
	if !validHTTPURL(assistURL) {
		return askSource{}, false
	}
	assistReq, err := http.NewRequestWithContext(ctx, http.MethodGet, assistURL, nil)
	if err != nil {
		return askSource{}, false
	}
	assistReq.Header.Set("Accept", "application/javascript, application/json")
	assistReq.Header.Set("Cache-Control", "no-cache")
	assistReq.Header.Set("Referer", pageURL)
	assistReq.Header.Set("User-Agent", askSearchAssistUserAgent)
	assistRes, err := askHTTPClient.Do(assistReq)
	if err != nil {
		return askSource{}, false
	}
	defer assistRes.Body.Close()
	if !askHTTPSuccess(assistRes.StatusCode) {
		return askSource{}, false
	}
	assistBody, err := io.ReadAll(io.LimitReader(assistRes.Body, 2<<20))
	if err != nil {
		return askSource{}, false
	}
	return parseDuckDuckGoSearchAssist(string(assistBody), fallbackURL)
}

func askDuckDuckGoRenderedSearchAssist(ctx context.Context, question, browserPath string, fetchResults bool) (askSource, bool) {
	executable := strings.TrimSpace(browserPath)
	if executable != "" {
		if _, err := osexec.LookPath(executable); err != nil {
			return askSource{}, false
		}
	} else {
		for _, candidate := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
			if path, err := osexec.LookPath(candidate); err == nil {
				executable = path
				break
			}
		}
		if executable == "" {
			return askSource{}, false
		}
	}

	queries := askSearchAssistQueryVariants(question)
	for index := 0; index < 2; index++ {
		query := queries[0]
		if index < len(queries) {
			query = queries[index]
		}
		if source, ok := askDuckDuckGoRenderedSearchAssistOnce(ctx, query, duckDuckGoSearchURL(question), executable, fetchResults); ok {
			return source, true
		}
		if err := ctx.Err(); err != nil {
			break
		}
	}
	return askSource{}, false
}

func askDuckDuckGoRenderedSearchAssistOnce(ctx context.Context, question, fallbackURL, executable string, fetchResults bool) (askSource, bool) {
	browserTimeout := 4 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return askSource{}, false
		}
		if remaining < browserTimeout {
			browserTimeout = remaining
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, browserTimeout)
	defer cancel()

	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.ExecPath(executable),
		chromedp.Flag("headless", "new"),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.WindowSize(1280, 900),
	)
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(runCtx, opts...)
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()

	var result askRenderedSearchAssistData
	if err := chromedp.Run(browserCtx, chromedp.Navigate(duckDuckGoSearchURL(question))); err != nil {
		return askSource{}, false
	}
	assistTimeout := 2 * time.Second
	if fetchResults {
		assistTimeout = 1500 * time.Millisecond
	}
	if browserTimeout < assistTimeout {
		assistTimeout = browserTimeout
	}
	if err := chromedp.Run(browserCtx, chromedp.PollFunction(askRenderedSearchAssistScript, &result,
		chromedp.WithPollingInterval(100*time.Millisecond),
		chromedp.WithPollingTimeout(assistTimeout))); err == nil {
		if source, ok := parseRenderedSearchAssist(result, fallbackURL); ok {
			return source, true
		}
	}

	// Search Assist is not guaranteed to render for every query. The normal
	// result cards are still useful: select the most relevant public result,
	// then fetch a bounded excerpt from that source (with a Reddit JSON path
	// where available). If fetching fails, the DuckDuckGo snippet remains a
	// useful attributed fallback.
	if !fetchResults {
		return askSource{}, false
	}
	var results []askRenderedSearchResult
	resultTimeout := browserTimeout - assistTimeout
	if resultTimeout <= 0 {
		return askSource{}, false
	}
	if err := chromedp.Run(browserCtx, chromedp.PollFunction(askRenderedSearchResultsScript, &results,
		chromedp.WithPollingInterval(100*time.Millisecond),
		chromedp.WithPollingTimeout(resultTimeout))); err != nil {
		return askSource{}, false
	}
	selected, ok := selectAskSearchResult(question, results)
	if !ok {
		return askSource{}, false
	}
	return fetchAskSearchResult(runCtx, selected, question)
}

func selectAskSearchResult(question string, results []askRenderedSearchResult) (askRenderedSearchResult, bool) {
	terms := strings.Fields(askFocusedTerm(question))
	if len(terms) == 0 {
		terms = strings.Fields(strings.ToLower(question))
	}
	bestScore := -1
	var best askRenderedSearchResult
	for index, result := range results {
		result.Title = cleanExternalText(result.Title)
		result.Snippet = cleanExternalText(result.Snippet)
		result.URL = strings.TrimSpace(result.URL)
		if result.Title == "" || !validPublicHTTPURL(result.URL) {
			continue
		}
		parsedURL, _ := url.Parse(result.URL)
		if parsedURL == nil || isDuckDuckGoHost(parsedURL.Hostname()) {
			continue
		}
		lowerTitle := strings.ToLower(result.Title)
		lowerSnippet := strings.ToLower(result.Snippet)
		score := 100 - index
		for _, term := range terms {
			if strings.Contains(lowerTitle, term) {
				score += 5
			}
			if strings.Contains(lowerSnippet, term) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = result
		}
	}
	return best, bestScore >= 0
}

func fetchAskSearchResult(ctx context.Context, result askRenderedSearchResult, question string) (askSource, bool) {
	if !validPublicHTTPURL(result.URL) {
		return askSource{}, false
	}
	parsedResultURL, _ := url.Parse(result.URL)
	if parsedResultURL != nil && isRedditHost(parsedResultURL.Hostname()) {
		if source, ok := fetchAskRedditResult(ctx, result); ok {
			return source, true
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, result.URL, nil)
	if err != nil {
		return askSource{}, false
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.8")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; source excerpt lookup)")
	res, err := askWebHTTPClient.Do(req)
	if err == nil {
		defer res.Body.Close()
		if askHTTPSuccess(res.StatusCode) && askHTMLContentType(res.Header.Get("Content-Type")) {
			body, readErr := io.ReadAll(io.LimitReader(res.Body, 512<<10))
			if readErr == nil {
				if excerpt := extractAskHTMLExcerpt(body, result.Snippet, result.Title, strings.Fields(askFocusedTerm(question))); excerpt != "" {
					return askSource{Title: result.Title, Summary: formatAskSearchResultSummary(result.Title, excerpt), URL: result.URL, Provider: "search_result"}, true
				}
			}
		}
	}
	if result.Snippet != "" {
		return askSource{Title: result.Title, Summary: formatAskSearchResultSummary(result.Title, result.Snippet), URL: result.URL, Provider: "search_result"}, true
	}
	return askSource{}, false
}

func formatAskSearchResultSummary(title, excerpt string) string {
	title = cleanExternalText(title)
	excerpt = cleanExternalText(excerpt)
	if title == "" {
		return excerpt
	}
	if excerpt == "" || strings.Contains(strings.ToLower(excerpt), strings.ToLower(title)) {
		return title + ": " + excerpt
	}
	return title + " — " + excerpt
}

func askHTMLContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return contentType == "" || strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml+xml") || strings.Contains(contentType, "text/plain")
}

func extractAskHTMLExcerpt(body []byte, fallback, title string, terms []string) string {
	metaDescription := ""
	paragraphs := make([]string, 0, 16)
	var titleText, paragraph strings.Builder
	inTitle, inParagraph := false, false
	skipDepth := 0
	z := xhtml.NewTokenizer(bytes.NewReader(body))
	for {
		tokenType := z.Next()
		if tokenType == xhtml.ErrorToken {
			break
		}
		switch tokenType {
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			nameBytes, hasAttr := z.TagName()
			name := strings.ToLower(string(nameBytes))
			if name == "script" || name == "style" || name == "noscript" || name == "svg" {
				if tokenType == xhtml.StartTagToken {
					skipDepth++
				}
				continue
			}
			if skipDepth > 0 {
				continue
			}
			attrs := map[string]string{}
			if hasAttr {
				for {
					key, value, more := z.TagAttr()
					attrs[strings.ToLower(string(key))] = string(value)
					if !more {
						break
					}
				}
			}
			if name == "meta" {
				property := strings.ToLower(attrs["property"])
				if property == "" {
					property = strings.ToLower(attrs["name"])
				}
				if (property == "description" || property == "og:description" || property == "twitter:description") && metaDescription == "" {
					metaDescription = cleanExternalText(html.UnescapeString(attrs["content"]))
				}
			}
			if name == "title" {
				inTitle = true
			}
			if (name == "p" || name == "li") && !inParagraph {
				inParagraph = true
				paragraph.Reset()
			}
		case xhtml.TextToken:
			if skipDepth > 0 {
				continue
			}
			text := string(z.Text())
			if inTitle {
				titleText.WriteString(text)
			}
			if inParagraph {
				paragraph.WriteByte(' ')
				paragraph.WriteString(text)
			}
		case xhtml.EndTagToken:
			nameBytes, _ := z.TagName()
			name := strings.ToLower(string(nameBytes))
			if skipDepth > 0 {
				if name == "script" || name == "style" || name == "noscript" || name == "svg" {
					skipDepth--
				}
				continue
			}
			if name == "title" {
				inTitle = false
			}
			if (name == "p" || name == "li") && inParagraph {
				if text := cleanExternalText(paragraph.String()); text != "" {
					paragraphs = append(paragraphs, text)
				}
				inParagraph = false
			}
		}
	}
	if metaDescription != "" {
		return metaDescription
	}
	if fallback != "" {
		return cleanExternalText(fallback)
	}
	best, bestScore := "", -1
	for index, candidate := range paragraphs {
		score := 1 - index
		lower := strings.ToLower(candidate)
		for _, term := range terms {
			if term != "" && strings.Contains(lower, term) {
				score++
			}
		}
		if score > bestScore {
			best, bestScore = candidate, score
		}
	}
	if best != "" {
		return best
	}
	return cleanExternalText(titleText.String())
}

func parseRenderedSearchAssist(result askRenderedSearchAssistData, fallbackURL string) (askSource, bool) {
	lines := strings.Split(strings.ReplaceAll(result.Text, "\r\n", "\n"), "\n")
	answerLines := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(cleanExternalText(line))
		if line == "" || strings.EqualFold(line, "Search Assist") || strings.EqualFold(line, "More") || strings.HasPrefix(strings.ToLower(line), "auto-generated based on") || strings.HasPrefix(strings.ToLower(line), "was this helpful") {
			continue
		}
		answerLines = append(answerLines, line)
	}
	answer := cleanExternalText(strings.Join(answerLines, " "))
	if answer == "" {
		return askSource{}, false
	}
	sourceURL := fallbackURL
	for _, link := range result.Links {
		if validHTTPURL(link) {
			sourceURL = link
			break
		}
	}
	return askSource{Title: "DuckDuckGo Search Assist", Summary: answer, URL: sourceURL, Provider: "search_assist"}, true
}

func parseDuckDuckGoSearchAssist(body, fallbackURL string) (askSource, bool) {
	const prefix = "DDG.deep.deepPayload = "
	const suffix = ";DDG.deep.bn="
	start := strings.Index(body, prefix)
	if start < 0 {
		return askSource{}, false
	}
	start += len(prefix)
	end := strings.Index(body[start:], suffix)
	if end < 0 {
		return askSource{}, false
	}
	var payload duckDuckGoSearchAssistPayload
	if err := json.Unmarshal([]byte(body[start:start+end]), &payload); err != nil {
		return askSource{}, false
	}
	for _, instantAnswer := range payload.InstantAnswers {
		answer := strings.ReplaceAll(cleanExternalText(instantAnswer.Data.Answer), "**", "")
		if answer == "" {
			continue
		}
		sourceURL := fallbackURL
		for _, source := range instantAnswer.Data.Sources {
			if validHTTPURL(source.Article.Link) {
				sourceURL = source.Article.Link
				break
			}
		}
		return askSource{Title: "DuckDuckGo Search Assist", Summary: answer, URL: sourceURL, Provider: "search_assist"}, true
	}
	return askSource{}, false
}

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
	if !askHTTPSuccess(res.StatusCode) {
		return askSource{}, false
	}
	var payload duckDuckGoAnswer
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&payload); err != nil {
		return askSource{}, false
	}
	searchURL := duckDuckGoSearchURL(question)
	if summary := cleanExternalText(payload.Answer); summary != "" {
		return askSource{Title: cleanExternalText(payload.Heading), Summary: summary, URL: searchURL}, true
	}
	if summary := cleanExternalText(payload.Definition); summary != "" {
		return askSource{Title: cleanExternalText(payload.Heading), Summary: summary, URL: searchURL}, true
	}
	if summary := cleanExternalText(payload.AbstractText); summary != "" {
		return askSource{Title: cleanExternalText(payload.Heading), Summary: summary, URL: searchURL}, true
	}
	return askSource{}, false
}

func askDuckDuckGoWithRetry(ctx context.Context, question string) (askSource, bool) {
	for attempt := 0; attempt < 2; attempt++ {
		if source, ok := askDuckDuckGo(ctx, question); ok {
			return source, true
		}
		if ctx.Err() != nil {
			break
		}
	}
	return askSource{}, false
}

func duckDuckGoSearchURL(query string) string {
	return "https://duckduckgo.com/?q=" + url.QueryEscape(strings.TrimSpace(query))
}

func askHTTPSuccess(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

type wikidataSearchResponse struct {
	Search []wikidataEntity `json:"search"`
}

type wikidataClaimsResponse struct {
	Entities map[string]wikidataClaimsEntity `json:"entities"`
}

type wikidataClaimsEntity struct {
	Claims map[string][]wikidataClaim `json:"claims"`
}

type wikidataClaim struct {
	MainSnak struct {
		SnakType  string `json:"snaktype"`
		DataValue struct {
			Value struct {
				Time string `json:"time"`
				ID   string `json:"id"`
			} `json:"value"`
		} `json:"datavalue"`
	} `json:"mainsnak"`
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
	entity, ok := searchWikidataEntity(ctx, query)
	if !ok {
		return askSource{}, false
	}
	return askSource{Title: entity.Label, Summary: entity.Description, URL: "https://www.wikidata.org/wiki/" + entity.ID}, true
}

type wikidataLabelsResponse struct {
	Entities map[string]struct {
		Labels map[string]struct {
			Value string `json:"value"`
		} `json:"labels"`
	} `json:"entities"`
}

func askWikidataRelationship(ctx context.Context, query, question string) (askSource, bool) {
	entity, ok := searchWikidataEntity(ctx, query)
	if !ok {
		return askSource{}, false
	}
	claims, ok := fetchWikidataClaims(ctx, entity.ID)
	if !ok {
		return askSource{}, false
	}
	for _, property := range askRelationshipClaimProperties(question) {
		ids := make([]string, 0, 4)
		seen := make(map[string]struct{})
		for _, claim := range claims[property] {
			if claim.MainSnak.SnakType != "value" {
				continue
			}
			id := strings.TrimSpace(claim.MainSnak.DataValue.Value.ID)
			if !validWikidataID(id) {
				continue
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
			if len(ids) == 4 {
				break
			}
		}
		if len(ids) == 0 {
			continue
		}
		labels, ok := fetchWikidataLabels(ctx, ids)
		if !ok {
			continue
		}
		names := make([]string, 0, len(ids))
		for _, id := range ids {
			if label := cleanExternalText(labels[id]); label != "" {
				names = append(names, label)
			}
		}
		if len(names) == 0 {
			continue
		}
		return askSource{
			Title:   entity.Label,
			Summary: formatAskRelationshipSummary(entity.Label, question, property, names, len(ids)-len(names)),
			URL:     "https://www.wikidata.org/wiki/" + entity.ID,
		}, true
	}
	return askSource{}, false
}

func askRelationshipClaimProperties(question string) []string {
	lowerQuestion := strings.ToLower(question)
	switch {
	case strings.Contains(lowerQuestion, "invent"):
		return []string{"P61", "P170", "P178"}
	case strings.Contains(lowerQuestion, "author") || strings.Contains(lowerQuestion, "wrote") || strings.Contains(lowerQuestion, "written"):
		return []string{"P50", "P170", "P178"}
	case strings.Contains(lowerQuestion, "direct"):
		return []string{"P57", "P170"}
	case strings.Contains(lowerQuestion, "develop"):
		return []string{"P178", "P170", "P112"}
	case strings.Contains(lowerQuestion, "found"):
		return []string{"P112", "P170", "P178"}
	default:
		return []string{"P170", "P178", "P112", "P61", "P50", "P57"}
	}
}

func formatAskRelationshipSummary(title, question, property string, names []string, omitted int) string {
	if len(names) == 1 && omitted == 0 {
		return fmt.Sprintf("%s is credited with %s %s.", names[0], askRelationshipGerundForProperty(question, property), title)
	}
	if omitted > 0 {
		names = append(names, fmt.Sprintf("+ %d more", omitted))
	}
	return fmt.Sprintf("%s was %s by %s.", title, askRelationshipPastPredicate(question, property), strings.Join(names, ", "))
}

func askRelationshipGerundForProperty(question, property string) string {
	switch property {
	case "P50":
		return "writing"
	case "P57":
		return "directing"
	case "P61":
		return "inventing"
	case "P112":
		return "founding"
	case "P170":
		return "creating"
	case "P178":
		return "developing"
	default:
		return askRelationshipGerund(question)
	}
}

func askRelationshipPastPredicate(question, property string) string {
	switch property {
	case "P50":
		return "authored"
	case "P57":
		return "directed"
	case "P61":
		return "invented"
	case "P112":
		return "founded"
	case "P170":
		return "created"
	case "P178":
		return "developed"
	default:
		return askRelationshipGerund(question)
	}
}

func fetchWikidataLabels(ctx context.Context, ids []string) (map[string]string, bool) {
	if len(ids) == 0 {
		return nil, false
	}
	params := url.Values{}
	params.Set("action", "wbgetentities")
	params.Set("ids", strings.Join(ids, "|"))
	params.Set("props", "labels")
	params.Set("languages", "en")
	params.Set("format", "json")
	endpoint := "https://www.wikidata.org/w/api.php?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (https://github.com/variablenix/GoBot; IRC bot)")
	res, err := askHTTPClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer res.Body.Close()
	if !askHTTPSuccess(res.StatusCode) {
		return nil, false
	}
	var payload wikidataLabelsResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 512<<10)).Decode(&payload); err != nil {
		return nil, false
	}
	labels := make(map[string]string, len(payload.Entities))
	for id, entity := range payload.Entities {
		if label, ok := entity.Labels["en"]; ok {
			labels[id] = label.Value
		}
	}
	return labels, true
}

func searchWikidataEntity(ctx context.Context, query string) (wikidataEntity, bool) {
	endpoint := "https://www.wikidata.org/w/api.php?action=wbsearchentities&search=" + url.QueryEscape(query) + "&language=en&uselang=en&format=json&limit=8"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return wikidataEntity{}, false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (https://github.com/variablenix/GoBot; IRC bot)")
	res, err := askHTTPClient.Do(req)
	if err != nil {
		return wikidataEntity{}, false
	}
	defer res.Body.Close()
	if !askHTTPSuccess(res.StatusCode) {
		return wikidataEntity{}, false
	}
	var payload wikidataSearchResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 512<<10)).Decode(&payload); err != nil {
		return wikidataEntity{}, false
	}
	return bestWikidataEntity(query, payload.Search)
}

func askWikidataTemporal(ctx context.Context, query, question string) (askSource, bool) {
	entity, ok := searchWikidataEntity(ctx, query)
	if !ok {
		return askSource{}, false
	}
	claims, ok := fetchWikidataClaims(ctx, entity.ID)
	if !ok {
		return askSource{}, false
	}
	properties := askTemporalClaimProperties(question)
	for _, property := range properties {
		for _, claim := range claims[property] {
			if claim.MainSnak.SnakType != "value" {
				continue
			}
			date := formatWikidataDate(claim.MainSnak.DataValue.Value.Time)
			if date == "" {
				continue
			}
			return askSource{
				Title:   entity.Label,
				Summary: formatAskTemporalSummary(entity.Label, question, date),
				URL:     "https://www.wikidata.org/wiki/" + entity.ID,
			}, true
		}
	}
	return askSource{}, false
}

func askTemporalClaimProperties(question string) []string {
	lowerQuestion := strings.ToLower(question)
	if strings.Contains(lowerQuestion, "releas") || strings.Contains(lowerQuestion, "come out") || strings.Contains(lowerQuestion, "debut") || strings.Contains(lowerQuestion, "premier") || strings.Contains(lowerQuestion, "publish") || strings.Contains(lowerQuestion, "launch") || strings.Contains(lowerQuestion, "introduc") || strings.Contains(lowerQuestion, "unveil") || strings.Contains(lowerQuestion, "first appeared") {
		return []string{"P577", "P571"}
	}
	return []string{"P571", "P577"}
}

func fetchWikidataClaims(ctx context.Context, id string) (map[string][]wikidataClaim, bool) {
	endpoint := "https://www.wikidata.org/w/api.php?action=wbgetentities&ids=" + url.QueryEscape(id) + "&props=claims&format=json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (https://github.com/variablenix/GoBot; IRC bot)")
	res, err := askHTTPClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer res.Body.Close()
	if !askHTTPSuccess(res.StatusCode) {
		return nil, false
	}
	var payload wikidataClaimsResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 512<<10)).Decode(&payload); err != nil {
		return nil, false
	}
	entity, ok := payload.Entities[id]
	if !ok {
		return nil, false
	}
	return entity.Claims, true
}

func formatWikidataDate(raw string) string {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "+")
	if len(raw) < len("2002-00-00") {
		return ""
	}
	datePart := raw[:len("2002-00-00")]
	parts := strings.Split(datePart, "-")
	if len(parts) != 3 || len(parts[0]) != 4 {
		return ""
	}
	month, err := strconv.Atoi(parts[1])
	if err != nil || month < 0 || month > 12 {
		return ""
	}
	day, err := strconv.Atoi(parts[2])
	if err != nil || day < 0 || day > 31 {
		return ""
	}
	if month == 0 {
		return parts[0]
	}
	if day == 0 {
		return fmt.Sprintf("%s %s", time.Month(month), parts[0])
	}
	parsed, err := time.Parse("2006-01-02", datePart)
	if err != nil {
		return ""
	}
	return parsed.Format("2 January 2006")
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

		// Wikidata often returns exact matches for several unrelated meanings.
		// Its search ranking is a better tie-breaker than preferring an exact
		// label, which previously selected the Firefox arcade game over Mozilla
		// Firefox. Exact match text is treated as the strongest signal; otherwise
		// use label-word coverage and preserve the API's relevance order.
		var score int
		if match == query {
			score = 100000 - index
		} else {
			score = matches * 10
			if label == query {
				score += 100
			}
			if matches == len(queryWords) {
				score += 100
			}
			score -= index
		}
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

func validPublicHTTPURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !validHTTPURL(raw) || parsed.User != nil || parsed.Hostname() == "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return false
	}
	if port := parsed.Port(); port != "" && port != "80" && port != "443" {
		return false
	}
	if address, err := netip.ParseAddr(host); err == nil {
		return isPublicIP(address)
	}
	return true
}

func isDuckDuckGoHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "duckduckgo.com" || strings.HasSuffix(host, ".duckduckgo.com")
}

func isPublicIP(address netip.Addr) bool {
	return address.IsGlobalUnicast() && !address.IsPrivate() && !address.IsLoopback() && !address.IsLinkLocalUnicast() && !address.IsLinkLocalMulticast() && !address.IsMulticast() && !address.IsUnspecified()
}

func newAskWebHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = askPublicDialContext
	return &http.Client{
		Timeout:   6 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if !validPublicHTTPURL(req.URL.String()) {
				return fmt.Errorf("refusing non-public redirect")
			}
			return nil
		},
	}
}

func askPublicDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if parsed, parseErr := netip.ParseAddr(host); parseErr == nil {
		if !isPublicIP(parsed) {
			return nil, fmt.Errorf("refusing non-public address")
		}
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, address)
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	for _, ip := range ips {
		parsed, parseErr := netip.ParseAddr(ip.String())
		if parseErr != nil || !isPublicIP(parsed) {
			continue
		}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(parsed.String(), port))
		if dialErr == nil {
			return conn, nil
		}
	}
	return nil, fmt.Errorf("no public address available for %s", host)
}

type askRedditListing struct {
	Data struct {
		Children []struct {
			Data struct {
				Title     string `json:"title"`
				Selftext  string `json:"selftext"`
				Body      string `json:"body"`
				Permalink string `json:"permalink"`
			} `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

func fetchAskRedditResult(ctx context.Context, result askRenderedSearchResult) (askSource, bool) {
	parsed, err := url.Parse(result.URL)
	if err != nil || !isRedditHost(parsed.Hostname()) || !strings.Contains(strings.ToLower(parsed.Path), "/comments/") {
		return askSource{}, false
	}
	jsonURL := *parsed
	jsonURL.Path = strings.TrimRight(jsonURL.Path, "/") + ".json"
	jsonURL.RawQuery = "raw_json=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jsonURL.String(), nil)
	if err != nil {
		return askSource{}, false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; public Reddit source excerpt)")
	res, err := askWebHTTPClient.Do(req)
	if err != nil {
		return askSource{}, false
	}
	defer res.Body.Close()
	if !askHTTPSuccess(res.StatusCode) {
		return askSource{}, false
	}
	var listings []askRedditListing
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&listings); err != nil || len(listings) == 0 || len(listings[0].Data.Children) == 0 {
		return askSource{}, false
	}
	post := listings[0].Data.Children[0].Data
	title := cleanExternalText(post.Title)
	body := cleanExternalText(post.Selftext)
	if body == "" && len(listings) > 1 {
		for _, child := range listings[1].Data.Children {
			if comment := cleanExternalText(child.Data.Body); comment != "" {
				body = comment
				break
			}
		}
	}
	if title == "" {
		title = result.Title
	}
	if body == "" {
		body = cleanExternalText(result.Snippet)
	}
	if title == "" || body == "" {
		return askSource{}, false
	}
	return askSource{Title: title, Summary: formatAskSearchResultSummary(title, body), URL: result.URL, Provider: "search_result"}, true
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

func formatAskNoAnswer(question string, maxLength int) string {
	maxLength = clampAskLength(maxLength, 120, 450, 360)
	return truncateAskBytes("I couldn't find a reliable answer — search: "+duckDuckGoSearchURL(question), maxLength)
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
