package plugins

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/variablenix/GoBot/bot"
)

func TestAskCommandAliases(t *testing.T) {
	for _, command := range []string{"ask", "question", "q"} {
		if !isAskCommand(command) {
			t.Errorf("expected %q to be an ask command", command)
		}
	}
	if isAskCommand("asker") {
		t.Error("unexpectedly accepted invalid ask command")
	}
}

func TestAskFocusedTermDisambiguatesNamedPeople(t *testing.T) {
	tests := map[string]string{
		"What is Mark Normand's comedy?":       "mark normand",
		"tell me about the Linux kernel":       "linux kernel",
		"What is the programming language Go?": "go",
	}
	for input, want := range tests {
		if got := askFocusedTerm(input); got != want {
			t.Errorf("askFocusedTerm(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAskDoesNotUseWikipediaFallback(t *testing.T) {
	t.Setenv("BOT_WOLFRAM_APPID", "")
	t.Setenv("BOT_ASK_WOLFRAM_APPID", "")
	t.Setenv("BOT_WOLFRAM_LLM_APPID", "")
	t.Setenv("BOT_ASK_WOLFRAM_LLM_APPID", "")
	oldAskClient, oldAPIClient := askHTTPClient, apiHTTPClient
	t.Cleanup(func() {
		askHTTPClient = oldAskClient
		apiHTTPClient = oldAPIClient
	})
	wikipediaResponse := `{"Heading":"Mark Normand","AbstractText":"wrong source","AbstractURL":"https://en.wikipedia.org/wiki/Mark_Normand","AbstractSource":"Wikipedia"}`
	wikidataResponse := `{"search":[{"id":"Q58062048","label":"Mark Normand","description":"American comedian and actor","match":{"text":"Mark Normand"}}]}`
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.duckduckgo.com" {
			return newPluginResponse(http.StatusOK, wikipediaResponse), nil
		}
		if r.URL.Host == "www.wikidata.org" {
			return newPluginResponse(http.StatusOK, wikidataResponse), nil
		}
		t.Fatalf("unexpected ask source request: %s", r.URL)
		return nil, nil
	})}
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("ask unexpectedly called Wikipedia: %s", r.URL)
		return nil, nil
	})}

	p := &Ask{}
	source, ok := p.findSource(context.Background(), "What is Mark Normand's comedy?", bot.PluginConfig{
		"duckduckgo_enabled": true,
		"wikidata_fallback":  true,
	})
	if !ok || source.Title != "Mark Normand" || source.URL != "https://www.wikidata.org/wiki/Q58062048" {
		t.Fatalf("findSource() = %#v, %v; want Wikidata Mark Normand", source, ok)
	}
}

func TestAskWolframShortUsesEnvironmentAppID(t *testing.T) {
	t.Setenv("BOT_WOLFRAM_APPID", "test-app-id")
	t.Setenv("BOT_ASK_WOLFRAM_APPID", "")
	t.Setenv("BOT_WOLFRAM_LLM_APPID", "")
	t.Setenv("BOT_ASK_WOLFRAM_LLM_APPID", "")
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.wolframalpha.com" || r.URL.Path != "/v1/result" {
			t.Fatalf("unexpected Wolfram endpoint: %s", r.URL)
		}
		if got := r.URL.Query().Get("appid"); got != "test-app-id" {
			t.Fatalf("appid query = %q, want test-app-id", got)
		}
		if got := r.URL.Query().Get("i"); got != "What is 2+2?" {
			t.Fatalf("input query = %q, want question", got)
		}
		if got := r.Header.Get("Accept"); got != "text/plain" {
			t.Fatalf("Accept = %q, want text/plain", got)
		}
		return newPluginResponse(http.StatusOK, "4"), nil
	})}

	source, ok := askWolframShort(context.Background(), "What is 2+2?")
	if !ok || source.Summary != "4" {
		t.Fatalf("askWolframShort() = %#v, %v; want summary 4", source, ok)
	}
	if source.URL != "" {
		t.Fatalf("Short Answers source unexpectedly included a URL: %q", source.URL)
	}
}

func TestAskWolframShortSkipsWithoutAppID(t *testing.T) {
	t.Setenv("BOT_WOLFRAM_APPID", "")
	t.Setenv("BOT_ASK_WOLFRAM_APPID", "")
	t.Setenv("BOT_WOLFRAM_LLM_APPID", "")
	t.Setenv("BOT_ASK_WOLFRAM_LLM_APPID", "")
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("Wolfram request made without an AppID")
		return nil, nil
	})}
	if source, ok := askWolframShort(context.Background(), "What is 2+2?"); ok || source != (askSource{}) {
		t.Fatalf("askWolframShort() = %#v, %v without AppID; want no result", source, ok)
	}
}

func TestAskWolframLLMUsesInputAndParsesResult(t *testing.T) {
	t.Setenv("BOT_WOLFRAM_LLM_APPID", "test-llm-app-id")
	t.Setenv("BOT_ASK_WOLFRAM_LLM_APPID", "")
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "www.wolframalpha.com" || r.URL.Path != "/api/v1/llm-api" {
			t.Fatalf("unexpected Wolfram LLM endpoint: %s", r.URL)
		}
		if got := r.URL.Query().Get("appid"); got != "" {
			t.Fatalf("appid query = %q, want AppID omitted from URL", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-llm-app-id" {
			t.Fatalf("Authorization = %q, want bearer AppID", got)
		}
		if got := r.URL.Query().Get("input"); got != "Who created Linux?" {
			t.Fatalf("input query = %q, want question", got)
		}
		return newPluginResponse(http.StatusOK, "Query: Who created Linux?\nInput interpretation: creator of Linux\nResult:\nLinus Torvalds\nImages:\nimage: https://example.test/image.png"), nil
	})}

	source, ok := askWolframLLM(context.Background(), "Who created Linux?")
	if !ok || source.Summary != "Linus Torvalds" {
		t.Fatalf("askWolframLLM() = %#v, %v; want parsed result", source, ok)
	}
	if source.URL != "" || strings.Contains(source.Summary, "test-llm-app-id") {
		t.Fatalf("Wolfram LLM output leaked link or AppID: %#v", source)
	}
}

func TestAskFindSourcePrefersLLMThenShort(t *testing.T) {
	t.Setenv("BOT_WOLFRAM_LLM_APPID", "test-llm-app-id")
	t.Setenv("BOT_ASK_WOLFRAM_LLM_APPID", "")
	t.Setenv("BOT_WOLFRAM_APPID", "test-short-app-id")
	t.Setenv("BOT_ASK_WOLFRAM_APPID", "")
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	requests := 0
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Path != "/api/v1/llm-api" {
			t.Fatalf("Short Answers or another fallback ran before LLM: %s", r.URL)
		}
		return newPluginResponse(http.StatusOK, "Result: Linus Torvalds"), nil
	})}

	source, ok := (&Ask{}).findSource(context.Background(), "Who created Linux?", bot.PluginConfig{
		"wolfram_enabled":       true,
		"wolfram_llm_enabled":   true,
		"wolfram_short_enabled": true,
		"duckduckgo_enabled":    true,
		"wikidata_fallback":     true,
	})
	if !ok || source.Title != "Wolfram|Alpha LLM" || source.Summary != "Linus Torvalds" || requests != 1 {
		t.Fatalf("findSource() = %#v, %v with %d requests; want LLM first", source, ok, requests)
	}
}

func TestAskFindSourceFallsThroughFailedWolframStages(t *testing.T) {
	t.Setenv("BOT_WOLFRAM_LLM_APPID", "test-llm-app-id")
	t.Setenv("BOT_ASK_WOLFRAM_LLM_APPID", "")
	t.Setenv("BOT_WOLFRAM_APPID", "test-short-app-id")
	t.Setenv("BOT_ASK_WOLFRAM_APPID", "")
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	requests := 0
	// With both Wolfram stages unusable, the keyless sources are next. The
	// fake response supplies a safe Wikidata result to prove the chain does not
	// stop at Wolfram's failure.
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests++
		switch r.URL.Host {
		case "www.wikidata.org":
			return newPluginResponse(http.StatusOK, `{"search":[{"id":"Q388","label":"Linux","description":"family of Unix-like operating systems","match":{"text":"Linux"}}]}`), nil
		case "www.wolframalpha.com":
			return newPluginResponse(http.StatusNotImplemented, "no result"), nil
		case "api.wolframalpha.com":
			return newPluginResponse(http.StatusNotImplemented, "did not understand your input"), nil
		default:
			t.Fatalf("unexpected fallback request: %s", r.URL)
			return nil, nil
		}
	})}

	source, ok := (&Ask{}).findSource(context.Background(), "Who created Linux?", bot.PluginConfig{
		"wolfram_enabled":       true,
		"wolfram_llm_enabled":   true,
		"wolfram_short_enabled": true,
		"duckduckgo_enabled":    false,
		"wikidata_fallback":     true,
	})
	if !ok || source.Title != "Linux" || requests != 3 {
		t.Fatalf("findSource() = %#v, %v with %d requests; want Wikidata after Wolfram failures", source, ok, requests)
	}
}

func TestAskFindSourcePrefersWolfram(t *testing.T) {
	t.Setenv("BOT_WOLFRAM_APPID", "test-app-id")
	t.Setenv("BOT_ASK_WOLFRAM_APPID", "")
	t.Setenv("BOT_WOLFRAM_LLM_APPID", "")
	t.Setenv("BOT_ASK_WOLFRAM_LLM_APPID", "")
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.wolframalpha.com" {
			t.Fatalf("fallback source was called before Wolfram: %s", r.URL)
		}
		return newPluginResponse(http.StatusOK, "The answer"), nil
	})}

	source, ok := (&Ask{}).findSource(context.Background(), "What is the answer?", bot.PluginConfig{
		"wolfram_enabled":    true,
		"duckduckgo_enabled": true,
		"wikidata_fallback":  true,
	})
	if !ok || source.Title != "Wolfram|Alpha" || source.Summary != "The answer" {
		t.Fatalf("findSource() = %#v, %v; want Wolfram source", source, ok)
	}
}

func TestUsableWolframAnswerRejectsFailureText(t *testing.T) {
	for _, answer := range []string{"", "Wolfram|Alpha did not understand the input", "not enough information"} {
		if usableWolframAnswer(answer) {
			t.Errorf("usableWolframAnswer(%q) = true, want false", answer)
		}
	}
	if !usableWolframAnswer("The population is 68 million") {
		t.Fatal("usableWolframAnswer rejected a normal answer")
	}
}

func TestAskDuckDuckGoRejectsWikipediaAndLooseRelatedTopics(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
		return newPluginResponse(http.StatusOK, `{"AbstractText":"wrong","AbstractURL":"https://en.wikipedia.org/wiki/Wrong","AbstractSource":"Wikipedia","RelatedTopics":[{"Text":"unrelated","FirstURL":"https://example.test/unrelated"}]}`), nil
	})}
	if source, ok := askDuckDuckGo(context.Background(), "Mark Normand"); ok || source != (askSource{}) {
		t.Fatalf("askDuckDuckGo accepted an unsafe fallback: %#v, %v", source, ok)
	}
}

func TestBestWikidataEntityPrefersExactLabel(t *testing.T) {
	entities := []wikidataEntity{
		{ID: "Q999", Label: "Mark", Description: "an unrelated person"},
		{ID: "Q58062048", Label: "Mark Normand", Description: "American comedian and actor"},
	}
	got, ok := bestWikidataEntity("mark normand", entities)
	if !ok || got.ID != "Q58062048" {
		t.Fatalf("bestWikidataEntity() = %#v, %v; want Mark Normand", got, ok)
	}
	if _, ok := bestWikidataEntity("mark normand", entities[:1]); ok {
		t.Fatal("bestWikidataEntity accepted a partial multi-word match")
	}
}

func TestAskResponseIsSingleLineAndBounded(t *testing.T) {
	answer := strings.Repeat("This is a useful answer. ", 40) + "\nignore this line break"
	got := formatAskResponse("Echo", answer, "https://en.wikipedia.org/wiki/Go_(programming_language)", 180, 120)
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("response contains a line break: %q", got)
	}
	if len([]byte(got)) > 180 {
		t.Fatalf("response is %d bytes, want at most 180: %q", len([]byte(got)), got)
	}
	if !strings.Contains(got, "Read more: https://en.wikipedia.org/wiki/Go_(programming_language)") {
		t.Fatalf("response lost source link: %q", got)
	}
}

func TestAskResponseDropsInvalidSourceURL(t *testing.T) {
	got := formatAskResponse("Echo", "A short answer", "javascript:alert(1)", 360, 240)
	if strings.Contains(got, "javascript:") {
		t.Fatalf("unsafe URL was included: %q", got)
	}
}

func TestAskProviderNoneDoesNotCallAI(t *testing.T) {
	p := &Ask{}
	if rewritten, ok := p.rewrite(t.Context(), "What is Go?", askSource{Title: "Go", Summary: "A programming language."}); ok || rewritten != "" {
		t.Fatalf("provider none unexpectedly rewrote answer: %q, %v", rewritten, ok)
	}
}

func TestAskEnvironmentOverridesNestedConfig(t *testing.T) {
	t.Setenv("BOT_ASK_PROVIDER", "openrouter")
	t.Setenv("BOT_ASK_AI_REWRITE", "true")

	p := &Ask{}
	if err := p.Init(bot.PluginConfig{
		"provider":   "none",
		"ai_rewrite": false,
		"max_length": 360,
	}, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	cfg := p.configSnapshot()
	if got := cfg.String("provider", "none"); got != "openrouter" {
		t.Fatalf("provider = %q, want openrouter", got)
	}
	if !cfg.Bool("ai_rewrite", false) {
		t.Fatal("ai_rewrite remained disabled despite BOT_ASK_AI_REWRITE=true")
	}
}

func TestAskInvalidEnvironmentBooleanLeavesConfigUnchanged(t *testing.T) {
	t.Setenv("BOT_ASK_AI_REWRITE", "not-a-boolean")

	p := &Ask{}
	if err := p.Init(bot.PluginConfig{"ai_rewrite": true}, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if !p.configSnapshot().Bool("ai_rewrite", false) {
		t.Fatal("invalid environment boolean unexpectedly changed config")
	}
}

func TestAskProviderInfoUsesEnvironmentCredentials(t *testing.T) {
	t.Setenv("BOT_OPENROUTER_API_KEY", "secret")
	t.Setenv("BOT_OPENROUTER_MODEL", "nvidia/test:free")
	model, configured := askProviderInfo("openrouter", bot.PluginConfig{"provider": "openrouter"})
	if model != "nvidia/test:free" || !configured {
		t.Fatalf("provider info = (%q, %v), want configured test model", model, configured)
	}
}

func TestAskReloadUpdatesConfigAndPreservesCooldownState(t *testing.T) {
	t.Setenv("BOT_ASK_PROVIDER", "")
	t.Setenv("BOT_ASK_AI_REWRITE", "")
	p := &Ask{}
	if err := p.Init(bot.PluginConfig{"provider": "none", "ai_rewrite": false}, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	p.last["account:test"] = time.Now()
	if err := p.Reload(bot.PluginConfig{"provider": "openrouter", "ai_rewrite": true}); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	cfg := p.configSnapshot()
	if cfg.String("provider", "none") != "openrouter" || !cfg.Bool("ai_rewrite", false) {
		t.Fatalf("reload did not update config: %#v", cfg)
	}
	if _, ok := p.last["account:test"]; !ok {
		t.Fatal("reload discarded cooldown state")
	}
}

func TestAskSenderKeyUsesAccountWhenAvailable(t *testing.T) {
	key := askSenderKey(bot.Message{Nick: "Echo", Account: "UserAccount"})
	if key != "account:useraccount" {
		t.Fatalf("got %q", key)
	}
}

func TestUsableAskRewriteRejectsProviderMetaText(t *testing.T) {
	for _, answer := range []string{
		"The user asks: what is Linux?",
		"The source does not define that topic.",
		"According to the source, the answer is unclear.",
		"Not enough information in this source.",
	} {
		if usableAskRewrite(answer) {
			t.Errorf("usableAskRewrite(%q) = true, want false", answer)
		}
	}
	if !usableAskRewrite("Linux is a family of open-source operating systems.") {
		t.Fatal("usableAskRewrite rejected a direct factual answer")
	}
}

func TestAskRewritePromptHasDirectAnswerContract(t *testing.T) {
	initial := askRewritePrompt("What is Go?", askSource{Title: "Go", Summary: "A programming language."}, 240, false)
	if !strings.Contains(initial, "Return only one concise plain-text paragraph") || !strings.Contains(initial, "INSUFFICIENT_SOURCE") {
		t.Fatalf("initial prompt is missing answer contract: %q", initial)
	}
	correction := askRewritePrompt("What is Go?", askSource{Title: "Go", Summary: "A programming language."}, 240, true)
	if !strings.Contains(correction, "previous response was invalid") {
		t.Fatalf("correction prompt is missing retry instruction: %q", correction)
	}
}

func TestAskRewriteRejectionReasonDoesNotExposeResponse(t *testing.T) {
	tests := []struct {
		answer string
		want   string
	}{
		{answer: "", want: "empty_response"},
		{answer: "The user asks: what is Linux?", want: "provider_meta_text"},
		{answer: "INSUFFICIENT_SOURCE", want: "insufficient_source"},
		{answer: "A response with no accepted structure", want: "unusable_response"},
	}
	for _, test := range tests {
		if got := askRewriteRejectionReason(test.answer); got != test.want {
			t.Errorf("askRewriteRejectionReason(%q) = %q, want %q", test.answer, got, test.want)
		}
	}
}
