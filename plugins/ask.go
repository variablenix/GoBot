package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

// Ask provides a small, source-grounded question lookup. DuckDuckGo Instant
// Answers are primary and exact Wikidata entities are the fallback.
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
		b.Send(target, formatAskNoAnswer(question, cfg.Int("max_length", 360)))
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
			if refined, ok := refineFocusedAskSource(question, source); ok {
				return refined, true
			}
			if !askNeedsRelationshipAnswer(question) && !askNeedsTemporalAnswer(question) {
				return source, true
			}
		}
		if focused != "" && !strings.EqualFold(focused, strings.TrimSpace(question)) {
			if source, ok := askDuckDuckGo(ctx, focused); ok {
				if source, ok = refineFocusedAskSource(question, source); ok {
					return source, true
				}
				if !askNeedsRelationshipAnswer(question) && !askNeedsTemporalAnswer(question) {
					return source, true
				}
			}
		}
	}
	if cfg.Bool("wikidata_fallback", true) && focused != "" {
		switch {
		case askNeedsRelationshipAnswer(question):
			if source, ok := askWikidataRelationship(ctx, focused, question); ok {
				return source, true
			}
		case askNeedsTemporalAnswer(question):
			if source, ok := askWikidataTemporal(ctx, focused, question); ok {
				return source, true
			}
			return askSource{}, false
		default:
			if source, ok := askWikidata(ctx, focused); ok {
				return source, true
			}
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
