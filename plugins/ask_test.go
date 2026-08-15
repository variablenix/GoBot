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

func TestAskHelpDescribesSimpleFallbacks(t *testing.T) {
	help := (&Ask{}).Help()
	if !strings.Contains(help, "DuckDuckGo") || !strings.Contains(help, "Wikidata") {
		t.Fatalf("Help() = %q, want simple source description", help)
	}
}

func TestAskFocusedTermDisambiguatesNamedPeople(t *testing.T) {
	tests := map[string]string{
		"What is Mark Normand's comedy?":       "mark normand",
		"tell me about the Linux kernel":       "linux kernel",
		"What is the programming language Go?": "go",
		"what year was Linux created?":         "linux",
		"what year did Arch Linux come out?":   "arch linux",
		"what year was Arch Linux released?":   "arch linux",
		"what year was Linux first released?":  "linux",
	}
	for input, want := range tests {
		if got := askFocusedTerm(input); got != want {
			t.Errorf("askFocusedTerm(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAskLocalAnswer(t *testing.T) {
	for question, want := range map[string]string{"hello": "Hello!"} {
		if got, ok := askLocalAnswer(question); !ok || got != want {
			t.Fatalf("askLocalAnswer(%q) = %q, %v; want %q, true", question, got, ok, want)
		}
	}
}

func TestAskDuckDuckGoParsesAnswersIncludingWikipediaBackedSummaries(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.duckduckgo.com" {
			t.Fatalf("unexpected host: %s", r.URL.Host)
		}
		return newPluginResponse(http.StatusAccepted, `{"Heading":"Go","Answer":"Go is a programming language.","AbstractText":""}`), nil
	})}
	source, ok := askDuckDuckGo(context.Background(), "What is Go?")
	if !ok || source.Summary != "Go is a programming language." || source.URL != "https://duckduckgo.com/?q=What+is+Go%3F" {
		t.Fatalf("askDuckDuckGo() = %#v, %v; want parsed answer", source, ok)
	}

	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
		return newPluginResponse(http.StatusOK, `{"Heading":"Linux","AbstractText":"Linux is a family of free and open-source Unix-like operating systems.","AbstractURL":"https://en.wikipedia.org/wiki/Linux","AbstractSource":"Wikipedia"}`), nil
	})}
	if source, ok := askDuckDuckGo(context.Background(), "What is Linux?"); !ok || source.Summary == "" || source.URL != "https://duckduckgo.com/?q=What+is+Linux%3F" {
		t.Fatalf("askDuckDuckGo discarded Wikipedia-backed summary or used the upstream URL: %#v, %v", source, ok)
	}
}

func TestAskWikidataPrefersExactLabel(t *testing.T) {
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

func TestAskWikidataPrefersPrimaryExactMatchOverLowerRankedAmbiguity(t *testing.T) {
	entities := []wikidataEntity{
		{ID: "Q698", Label: "Mozilla Firefox", Description: "free and open source web browser", Match: struct {
			Text string `json:"text"`
		}{Text: "Firefox"}},
		{ID: "Q3072842", Label: "Firefox", Description: "1984 arcade video game", Match: struct {
			Text string `json:"text"`
		}{Text: "Firefox"}},
	}
	got, ok := bestWikidataEntity("firefox", entities)
	if !ok || got.ID != "Q698" {
		t.Fatalf("bestWikidataEntity() = %#v, %v; want Mozilla Firefox", got, ok)
	}
}

func TestAskWikidataUsesExactEntityDescription(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "www.wikidata.org" {
			t.Fatalf("unexpected host: %s", r.URL.Host)
		}
		return newPluginResponse(http.StatusOK, `{"search":[{"id":"Q388","label":"Linux","description":"family of Unix-like operating systems","match":{"text":"Linux"}}]}`), nil
	})}
	source, ok := askWikidata(context.Background(), "linux")
	if !ok || source.Summary != "family of Unix-like operating systems" || source.URL != "https://www.wikidata.org/wiki/Q388" {
		t.Fatalf("askWikidata() = %#v, %v; want exact entity description", source, ok)
	}
}

func TestAskRelationshipQuestionsSkipGenericWikidata(t *testing.T) {
	if !askNeedsRelationshipAnswer("Who created Linux?") {
		t.Fatal("relationship question was not detected")
	}
	if askNeedsRelationshipAnswer("What is Linux?") {
		t.Fatal("definition question was incorrectly classified as relationship lookup")
	}
}

func TestRefineFocusedAskSourceAnswersSpecificLinuxQuestions(t *testing.T) {
	source := askSource{
		Title:   "Linux",
		Summary: "Linux is based on the Linux kernel, which was first released on 17 September 1991 by Linus Torvalds.",
		URL:     "https://duckduckgo.com/?q=linux",
	}
	created, ok := refineFocusedAskSource("Who created Linux?", source)
	if !ok || !strings.Contains(created.Summary, "Linus Torvalds") || !strings.Contains(created.Summary, "creating Linux") {
		t.Fatalf("relationship refinement = %#v, %v", created, ok)
	}
	year, ok := refineFocusedAskSource("What year did Linux start?", source)
	if !ok || year.Summary != "Linux started on 17 September 1991." {
		t.Fatalf("temporal refinement = %#v, %v", year, ok)
	}
}

func TestAskIntentVariants(t *testing.T) {
	if !askNeedsRelationshipAnswer("Who is the founder of Example?") {
		t.Fatal("founder relationship was not detected")
	}
	if !askNeedsRelationshipAnswer("Who invented Example?") {
		t.Fatal("inventor relationship was not detected")
	}
	if !askNeedsTemporalAnswer("When was Example founded?") {
		t.Fatal("founded temporal question was not detected")
	}
	if got := askTemporalPhrase("What year was Example founded?"); got != "was founded" {
		t.Fatalf("askTemporalPhrase() = %q, want was founded", got)
	}
}

func TestFormatWikidataDate(t *testing.T) {
	if got := formatWikidataDate("+2002-03-11T00:00:00Z"); got != "11 March 2002" {
		t.Fatalf("formatWikidataDate() = %q, want 11 March 2002", got)
	}
	if got := formatWikidataDate("invalid"); got != "" {
		t.Fatalf("formatWikidataDate(invalid) = %q, want empty", got)
	}
}

func TestAskWikidataTemporalUsesReleaseClaims(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("action") == "wbsearchentities" {
			return newPluginResponse(http.StatusOK, `{"search":[{"id":"Q185576","label":"Arch Linux","description":"Linux distribution","match":{"text":"Arch Linux"}}]}`), nil
		}
		return newPluginResponse(http.StatusOK, `{"entities":{"Q185576":{"claims":{"P577":[{"mainsnak":{"snaktype":"value","datavalue":{"value":{"time":"+2002-03-12T00:00:00Z"}}}}]}}}}`), nil
	})}
	source, ok := askWikidataTemporal(context.Background(), "arch linux", "What year was Arch Linux released?")
	if !ok || source.Summary != "Arch Linux was first released on 12 March 2002." || source.URL != "https://www.wikidata.org/wiki/Q185576" {
		t.Fatalf("askWikidataTemporal() = %#v, %v; want release claim answer", source, ok)
	}
}

func TestRefineFocusedAskSourceRejectsUnrelatedSpecificQuestion(t *testing.T) {
	source := askSource{Title: "Linux", Summary: "Linux is a family of Unix-like operating systems.", URL: "https://duckduckgo.com/?q=linux"}
	if _, ok := refineFocusedAskSource("Who created Linux?", source); ok {
		t.Fatal("generic focused summary was accepted as a relationship answer")
	}
}

func TestAskFindSourceRefinesFocusedDuckDuckGoAnswer(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	var queries []string
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		query := r.URL.Query().Get("q")
		queries = append(queries, query)
		if strings.EqualFold(query, "who created linux?") {
			return newPluginResponse(http.StatusAccepted, `{}`), nil
		}
		return newPluginResponse(http.StatusAccepted, `{"Heading":"Linux","AbstractText":"Linux is based on the Linux kernel, which was first released on 17 September 1991 by Linus Torvalds."}`), nil
	})}
	source, ok := (&Ask{}).findSource(context.Background(), "Who created Linux?", bot.PluginConfig{"duckduckgo_enabled": true, "wikidata_fallback": false})
	if !ok || source.Summary != "Linus Torvalds is credited with creating Linux." {
		t.Fatalf("findSource() = %#v, %v; want refined relationship answer", source, ok)
	}
	if len(queries) != 2 || queries[1] != "linux" {
		t.Fatalf("DuckDuckGo queries = %#v; want full question followed by focused term", queries)
	}
}

func TestAskResponseIsSingleLineAndBounded(t *testing.T) {
	answer := strings.Repeat("This is a useful answer. ", 40) + "\nignore this line break"
	got := formatAskResponse("Echo", answer, "https://example.test/source", 180, 120)
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("response contains a line break: %q", got)
	}
	if len([]byte(got)) > 180 {
		t.Fatalf("response is %d bytes, want at most 180: %q", len([]byte(got)), got)
	}
	if !strings.Contains(got, "Read more: https://example.test/source") {
		t.Fatalf("response lost source link: %q", got)
	}
}

func TestAskResponseDropsInvalidSourceURL(t *testing.T) {
	got := formatAskResponse("Echo", "A short answer", "javascript:alert(1)", 360, 240)
	if strings.Contains(got, "javascript:") {
		t.Fatalf("unsafe URL was included: %q", got)
	}
}

func TestAskReloadPreservesCooldownState(t *testing.T) {
	p := &Ask{}
	if err := p.Init(bot.PluginConfig{"cooldown_seconds": 15}, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	p.last["account:test"] = time.Now()
	if err := p.Reload(bot.PluginConfig{"cooldown_seconds": 5}); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if _, ok := p.last["account:test"]; !ok {
		t.Fatal("reload discarded cooldown state")
	}
}
