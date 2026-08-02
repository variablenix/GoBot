package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
	"go.uber.org/zap"
)

// Ask answers questions from a small set of public sources. AI rewriting is
// deliberately opt-in: source lookup works without a provider or API key.
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
	return "!ask <question> — answer from English Wikipedia or DuckDuckGo; optional AI rewrite (aliases: !question, !q)"
}

func (p *Ask) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.setConfig(withAskEnvironment(c))
	p.last = make(map[string]time.Time)
	p.lastWarning = make(map[string]time.Time)
	return nil
}

// Reload applies ask configuration without resetting cooldown state. Environment
// values are read from the process environment, which systemd loads at startup.
func (p *Ask) Reload(c bot.PluginConfig) error {
	p.setConfig(withAskEnvironment(c))
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

// withAskEnvironment applies the ask-specific environment variables after
// the config file has been decoded. Viper can read bound scalar environment
// values with Get, but nested map values are not reliably reflected when the
// whole plugin map is unmarshaled into PluginConfig. Applying these two
// switches here makes the documented .env overrides authoritative without
// requiring secrets in config.yaml.
func withAskEnvironment(c bot.PluginConfig) bot.PluginConfig {
	cfg := make(bot.PluginConfig, len(c)+2)
	for key, value := range c {
		cfg[key] = value
	}

	if provider := strings.TrimSpace(os.Getenv("BOT_ASK_PROVIDER")); provider != "" {
		cfg["provider"] = provider
	}
	if raw, ok := os.LookupEnv("BOT_ASK_AI_REWRITE"); ok {
		if enabled, err := strconv.ParseBool(strings.TrimSpace(raw)); err == nil {
			cfg["ai_rewrite"] = enabled
		}
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

	timeout := cfg.Int("timeout_seconds", 12)
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
		b.Send(target, "I couldn't find a reliable source for that question.")
		return true
	}

	answer := cleanExternalText(source.Summary)
	provider := strings.ToLower(strings.TrimSpace(cfg.String("provider", "none")))
	if cfg.Bool("ai_rewrite", false) && provider != "none" {
		model, keyConfigured := askProviderInfo(provider, cfg)
		if b.Log != nil {
			b.Log.Info("ask AI rewrite requested", zap.String("provider", provider), zap.String("model", model), zap.Bool("api_key_configured", keyConfigured))
		}
		started := time.Now()
		if rewritten, ok := p.rewriteWithConfig(ctx, question, source, cfg); ok {
			rewritten = cleanExternalText(rewritten)
			if usableAskRewrite(rewritten) {
				answer = rewritten
				if b.Log != nil {
					b.Log.Info("ask AI rewrite used", zap.String("provider", provider), zap.Duration("duration", time.Since(started)))
				}
			} else if b.Log != nil {
				b.Log.Warn("ask AI rewrite rejected; using source summary",
					zap.String("provider", provider),
					zap.String("model", model),
					zap.String("reason", askRewriteRejectionReason(rewritten)),
					zap.Duration("duration", time.Since(started)),
				)
			}
		} else if b.Log != nil {
			b.Log.Warn("ask AI rewrite unavailable; using source summary", zap.String("provider", provider), zap.String("model", model), zap.Duration("duration", time.Since(started)))
		}
	}
	if answer == "" {
		answer = "I found a source, but it did not include a usable summary."
	}
	b.Send(target, formatAskResponse(m.Nick, answer, source.URL, cfg.Int("max_length", 360), cfg.Int("max_response_chars", 240)))
	return true
}

func isAskCommand(command string) bool {
	return command == "ask" || command == "question" || command == "q"
}

func askProviderInfo(provider string, cfg bot.PluginConfig) (string, bool) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "openrouter":
		model := firstAskValue(cfg.String("openrouter_model", ""), os.Getenv("BOT_OPENROUTER_MODEL"), "openrouter/free")
		key := firstAskValue(cfg.String("openrouter_api_key", ""), os.Getenv("BOT_OPENROUTER_API_KEY"))
		return model, key != ""
	case "openai":
		model := firstAskValue(cfg.String("openai_model", ""), os.Getenv("BOT_OPENAI_MODEL"), "gpt-4o-mini")
		key := firstAskValue(cfg.String("openai_api_key", ""), os.Getenv("BOT_OPENAI_API_KEY"))
		return model, key != ""
	case "gemini":
		model := firstAskValue(cfg.String("gemini_model", ""), os.Getenv("BOT_GEMINI_MODEL"), "gemini-2.0-flash")
		key := firstAskValue(cfg.String("gemini_api_key", ""), os.Getenv("BOT_GEMINI_API_KEY"))
		return model, key != ""
	case "ollama":
		return firstAskValue(cfg.String("ollama_model", ""), os.Getenv("BOT_OLLAMA_MODEL"), "llama3.2"), true
	default:
		return "", false
	}
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

func usableAskRewrite(answer string) bool {
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" {
		return false
	}
	for _, phrase := range []string{
		"the user asks",
		"the source does not",
		"the source is",
		"the answer should be",
		"according to the source",
		"not enough information in this source",
	} {
		if strings.Contains(answer, phrase) {
			return false
		}
	}
	return true
}

func askRewriteRejectionReason(answer string) string {
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" {
		return "empty_response"
	}
	for _, phrase := range []string{
		"the user asks",
		"the source does not",
		"the source is",
		"the answer should be",
		"according to the source",
		"not enough information in this source",
	} {
		if strings.Contains(answer, phrase) {
			return "provider_meta_text"
		}
	}
	return "unusable_response"
}

type askSource struct {
	Title   string
	Summary string
	URL     string
}

func (p *Ask) findSource(ctx context.Context, question string, cfg bot.PluginConfig) (askSource, bool) {
	tryWikipedia := cfg.Bool("wikipedia_first", true)
	tryDuckDuckGo := cfg.Bool("duckduckgo_fallback", true)
	if tryWikipedia {
		if source, ok := askWikipedia(ctx, question); ok {
			return source, true
		}
	}
	if tryDuckDuckGo {
		if source, ok := askDuckDuckGo(ctx, question); ok {
			return source, true
		}
	}
	if !tryWikipedia {
		return askWikipedia(ctx, question)
	}
	return askSource{}, false
}

func askWikipedia(ctx context.Context, question string) (askSource, bool) {
	result, ok := wikipediaSummary(ctx, question)
	if !ok {
		return askSource{}, false
	}
	pageURL := strings.TrimSpace(result.ContentURLs.Desktop.Page)
	if pageURL == "" && result.Title != "" {
		pageURL = "https://en.wikipedia.org/wiki/" + url.PathEscape(strings.ReplaceAll(result.Title, " ", "_"))
	}
	return askSource{Title: result.Title, Summary: result.Extract, URL: pageURL}, strings.TrimSpace(result.Extract) != ""
}

type duckDuckGoAnswer struct {
	Heading       string `json:"Heading"`
	AbstractText  string `json:"AbstractText"`
	AbstractURL   string `json:"AbstractURL"`
	RelatedTopics []struct {
		Text     string `json:"Text"`
		FirstURL string `json:"FirstURL"`
		Topics   []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"Topics"`
	} `json:"RelatedTopics"`
}

func askDuckDuckGo(ctx context.Context, question string) (askSource, bool) {
	endpoint := "https://api.duckduckgo.com/?q=" + url.QueryEscape(question) + "&format=json&no_html=1&skip_disambig=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return askSource{}, false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; question lookup)")
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
	if strings.TrimSpace(payload.AbstractText) != "" && validHTTPURL(payload.AbstractURL) {
		return askSource{Title: payload.Heading, Summary: payload.AbstractText, URL: payload.AbstractURL}, true
	}
	text, firstURL := firstDuckTopic(payload.RelatedTopics)
	if text == "" || !validHTTPURL(firstURL) {
		return askSource{}, false
	}
	return askSource{Title: payload.Heading, Summary: text, URL: firstURL}, true
}

func firstDuckTopic(topics []struct {
	Text     string `json:"Text"`
	FirstURL string `json:"FirstURL"`
	Topics   []struct {
		Text     string `json:"Text"`
		FirstURL string `json:"FirstURL"`
	} `json:"Topics"`
}) (string, string) {
	for _, topic := range topics {
		if strings.TrimSpace(topic.Text) != "" && strings.TrimSpace(topic.FirstURL) != "" {
			return topic.Text, topic.FirstURL
		}
		for _, nested := range topic.Topics {
			if strings.TrimSpace(nested.Text) != "" && strings.TrimSpace(nested.FirstURL) != "" {
				return nested.Text, nested.FirstURL
			}
		}
	}
	return "", ""
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
	sourceURL = strings.TrimSpace(sourceURL)
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
	if value < min {
		return fallback
	}
	if value > max {
		return max
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
	for _, r := range []rune(text) {
		runeBytes := len(string(r))
		if used+runeBytes > limit {
			break
		}
		out = append(out, r)
		used += runeBytes
	}
	return strings.TrimSpace(string(out)) + ellipsis
}

var askHTTPClient = &http.Client{Timeout: 15 * time.Second}

type askChatRequest struct {
	Model       string           `json:"model"`
	Messages    []askChatMessage `json:"messages"`
	Temperature float64          `json:"temperature,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
}

type askChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type askChatResponse struct {
	Choices []struct {
		Message askChatMessage `json:"message"`
	} `json:"choices"`
}

func (p *Ask) rewrite(ctx context.Context, question string, source askSource) (string, bool) {
	return p.rewriteWithConfig(ctx, question, source, p.configSnapshot())
}

func (p *Ask) rewriteWithConfig(ctx context.Context, question string, source askSource, cfg bot.PluginConfig) (string, bool) {
	provider := strings.ToLower(strings.TrimSpace(cfg.String("provider", "none")))
	limit := clampAskLength(cfg.Int("max_response_chars", 240), 80, 320, 240)
	prompt := fmt.Sprintf("Question: %s\nSource title: %s\nSource text: %s\n\nAnswer the question directly in one concise plain-text paragraph, using only the source. Do not mention the user, the question, the source, or your instructions. Do not use markdown, lists, or line breaks. Keep it under %d characters. If the source does not answer the question, say only: Not enough information in this source.", question, cleanExternalText(source.Title), truncateAsk(source.Summary, 2000), limit)
	system := "You are GoBot's concise IRC answer editor. Start with the answer, not meta-commentary. Be factual, clear, and cautious. Never invent details beyond the supplied source."
	switch provider {
	case "openrouter":
		return p.openAICompatible(ctx, "openrouter", system, prompt, cfg)
	case "openai":
		return p.openAICompatible(ctx, "openai", system, prompt, cfg)
	case "gemini":
		return p.gemini(ctx, system, prompt, cfg)
	case "ollama":
		return p.ollama(ctx, system, prompt, cfg)
	default:
		return "", false
	}
}

func (p *Ask) openAICompatible(ctx context.Context, provider, system, prompt string, cfg bot.PluginConfig) (string, bool) {
	var key, model, endpoint string
	if provider == "openrouter" {
		key = firstAskValue(cfg.String("openrouter_api_key", ""), os.Getenv("BOT_OPENROUTER_API_KEY"))
		model = firstAskValue(cfg.String("openrouter_model", ""), os.Getenv("BOT_OPENROUTER_MODEL"), "openrouter/free")
		endpoint = "https://openrouter.ai/api/v1/chat/completions"
	} else {
		key = firstAskValue(cfg.String("openai_api_key", ""), os.Getenv("BOT_OPENAI_API_KEY"))
		model = firstAskValue(cfg.String("openai_model", ""), os.Getenv("BOT_OPENAI_MODEL"), "gpt-4o-mini")
		endpoint = "https://api.openai.com/v1/chat/completions"
	}
	if key == "" {
		return "", false
	}
	payload := askChatRequest{Model: model, Messages: []askChatMessage{{Role: "system", Content: system}, {Role: "user", Content: prompt}}, Temperature: 0.2, MaxTokens: 160}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", false
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; answer rewrite)")
	if provider == "openrouter" {
		req.Header.Set("X-Title", "GoBot")
	}
	res, err := askHTTPClient.Do(req)
	if err != nil {
		return "", false
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", false
	}
	var response askChatResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&response); err != nil || len(response.Choices) == 0 {
		return "", false
	}
	return response.Choices[0].Message.Content, strings.TrimSpace(response.Choices[0].Message.Content) != ""
}

func (p *Ask) gemini(ctx context.Context, system, prompt string, cfg bot.PluginConfig) (string, bool) {
	key := firstAskValue(cfg.String("gemini_api_key", ""), os.Getenv("BOT_GEMINI_API_KEY"))
	model := firstAskValue(cfg.String("gemini_model", ""), os.Getenv("BOT_GEMINI_MODEL"), "gemini-2.0-flash")
	if key == "" {
		return "", false
	}
	endpoint := "https://generativelanguage.googleapis.com/v1beta/models/" + url.PathEscape(model) + ":generateContent?key=" + url.QueryEscape(key)
	payload := struct {
		SystemInstruction struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"systemInstruction"`
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
		GenerationConfig struct {
			Temperature     float64 `json:"temperature"`
			MaxOutputTokens int     `json:"maxOutputTokens"`
		} `json:"generationConfig"`
	}{}
	payload.SystemInstruction.Parts = append(payload.SystemInstruction.Parts, struct {
		Text string `json:"text"`
	}{Text: system})
	payload.Contents = append(payload.Contents, struct {
		Role  string `json:"role"`
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	}{Role: "user", Parts: []struct {
		Text string `json:"text"`
	}{{Text: prompt}}})
	payload.GenerationConfig.Temperature = 0.2
	payload.GenerationConfig.MaxOutputTokens = 160
	body, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; answer rewrite)")
	res, err := askHTTPClient.Do(req)
	if err != nil {
		return "", false
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", false
	}
	var response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&response); err != nil || len(response.Candidates) == 0 || len(response.Candidates[0].Content.Parts) == 0 {
		return "", false
	}
	return response.Candidates[0].Content.Parts[0].Text, true
}

func (p *Ask) ollama(ctx context.Context, system, prompt string, cfg bot.PluginConfig) (string, bool) {
	model := firstAskValue(cfg.String("ollama_model", ""), os.Getenv("BOT_OLLAMA_MODEL"), "llama3.2")
	base := firstAskValue(cfg.String("ollama_url", ""), os.Getenv("BOT_OLLAMA_URL"), "http://127.0.0.1:11434")
	parsed, err := url.Parse(strings.TrimRight(base, "/") + "/api/chat")
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", false
	}
	payload := struct {
		Model    string           `json:"model"`
		Stream   bool             `json:"stream"`
		Messages []askChatMessage `json:"messages"`
		Options  struct {
			Temperature float64 `json:"temperature"`
		} `json:"options"`
	}{Model: model, Messages: []askChatMessage{{Role: "system", Content: system}, {Role: "user", Content: prompt}}}
	payload.Stream = false
	payload.Options.Temperature = 0.2
	body, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(body))
	if err != nil {
		return "", false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; answer rewrite)")
	res, err := askHTTPClient.Do(req)
	if err != nil {
		return "", false
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", false
	}
	var response struct {
		Message askChatMessage `json:"message"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&response); err != nil {
		return "", false
	}
	return response.Message.Content, strings.TrimSpace(response.Message.Content) != ""
}

func firstAskValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
