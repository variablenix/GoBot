package plugins

import (
	"context"
	"net/http"
	"reflect"
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

func TestAskSearchAssistQueryVariants(t *testing.T) {
	if got := askSearchAssistQueryVariants("Why should users not use Cloudflare?"); !reflect.DeepEqual(got, []string{
		"Why should users not use Cloudflare?",
		"what are the disadvantages of Cloudflare?",
	}) {
		t.Fatalf("askSearchAssistQueryVariants() = %#v, want an intent-preserving variant", got)
	}
	if got := askSearchAssistQueryVariants("What is Cloudflare?"); !reflect.DeepEqual(got, []string{"What is Cloudflare?"}) {
		t.Fatalf("askSearchAssistQueryVariants() = %#v, want only the original query", got)
	}
	if got := askSearchAssistQueryVariants(`What genre is the band "ObZen"?`); !reflect.DeepEqual(got, []string{
		`What genre is the band "ObZen"?`,
		"what genres does ObZen have?",
	}) {
		t.Fatalf("askSearchAssistQueryVariants() = %#v, want an entity-focused genre variant", got)
	}
	if got := askSearchAssistQueryVariants(`What music genre is the band "ObZen"?`); !reflect.DeepEqual(got, []string{
		`What music genre is the band "ObZen"?`,
		"what genres does ObZen have?",
	}) {
		t.Fatalf("askSearchAssistQueryVariants() = %#v, want a music-genre variant", got)
	}
}

func TestAskFocusedTermDisambiguatesNamedPeople(t *testing.T) {
	tests := map[string]string{
		"What is Mark Normand's comedy?":           "mark normand",
		"tell me about the Linux kernel":           "linux kernel",
		"What is the programming language Go?":     "go",
		"what year was Linux created?":             "linux",
		"what year did Arch Linux come out?":       "arch linux",
		"what year was Arch Linux released?":       "arch linux",
		"what year was Linux first released?":      "linux",
		"when did Ubuntu first appear?":            "ubuntu",
		"what was the release date of Debian?":     "debian",
		"how long ago was Firefox published?":      "firefox",
		"what year did Linux come into existence?": "linux",
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

func TestAskDuckDuckGoRetriesTransientEmptyResponse(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	requests := 0
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return newPluginResponse(http.StatusOK, `{}`), nil
		}
		return newPluginResponse(http.StatusOK, `{"Heading":"Debian","AbstractText":"Debian is a Unix-like operating system."}`), nil
	})}
	source, ok := askDuckDuckGoWithRetry(context.Background(), "What is Debian?")
	if !ok || source.Summary != "Debian is a Unix-like operating system." || requests != 2 {
		t.Fatalf("askDuckDuckGoWithRetry() = %#v, %v after %d requests; want second-attempt answer", source, ok, requests)
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

func TestAskWikidataRelationshipUsesDeveloperClaims(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("action") == "wbsearchentities" {
			return newPluginResponse(http.StatusOK, `{"search":[{"id":"Q185576","label":"Arch Linux","description":"Linux distribution","match":{"text":"Arch Linux"}}]}`), nil
		}
		if r.URL.Query().Get("props") == "claims" {
			return newPluginResponse(http.StatusOK, `{"entities":{"Q185576":{"claims":{"P178":[{"mainsnak":{"snaktype":"value","datavalue":{"value":{"id":"Q1"}}}},{"mainsnak":{"snaktype":"value","datavalue":{"value":{"id":"Q2"}}}}]}}}}`), nil
		}
		return newPluginResponse(http.StatusOK, `{"entities":{"Q1":{"labels":{"en":{"language":"en","value":"Arch Linux Developers"}}},"Q2":{"labels":{"en":{"language":"en","value":"Judd Vinet"}}}}}`), nil
	})}
	source, ok := askWikidataRelationship(context.Background(), "arch linux", "Who created Arch Linux?")
	if !ok || source.Summary != "Arch Linux was developed by Arch Linux Developers, Judd Vinet." || source.URL != "https://www.wikidata.org/wiki/Q185576" {
		t.Fatalf("askWikidataRelationship() = %#v, %v; want developer-claim answer", source, ok)
	}
}

func TestAskFindSourceFallsBackToWikidataRelationshipClaims(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.duckduckgo.com" {
			return newPluginResponse(http.StatusOK, `{"Heading":"Arch Linux","AbstractText":"Arch Linux is an open source Linux distribution."}`), nil
		}
		if r.URL.Query().Get("action") == "wbsearchentities" {
			return newPluginResponse(http.StatusOK, `{"search":[{"id":"Q185576","label":"Arch Linux","description":"Linux distribution","match":{"text":"Arch Linux"}}]}`), nil
		}
		if r.URL.Query().Get("props") == "claims" {
			return newPluginResponse(http.StatusOK, `{"entities":{"Q185576":{"claims":{"P178":[{"mainsnak":{"snaktype":"value","datavalue":{"value":{"id":"Q1"}}}}]}}}}`), nil
		}
		return newPluginResponse(http.StatusOK, `{"entities":{"Q1":{"labels":{"en":{"language":"en","value":"Arch Linux Developers"}}}}}`), nil
	})}
	source, ok := (&Ask{}).findSource(context.Background(), "Who created Arch Linux?", bot.PluginConfig{"search_assist_enabled": false, "duckduckgo_enabled": true, "wikidata_fallback": true})
	if !ok || source.Summary != "Arch Linux Developers is credited with developing Arch Linux." {
		t.Fatalf("findSource() = %#v, %v; want structured relationship fallback", source, ok)
	}
}

func TestAskRelationshipQuestionsSkipGenericWikidata(t *testing.T) {
	if !askNeedsRelationshipAnswer("Who created Linux?") {
		t.Fatal("relationship question was not detected")
	}
	for _, question := range []string{
		"By whom was Linux created?",
		"Who's the creator of Linux?",
		"Which person developed Go?",
		"What company created Example?",
	} {
		if !askNeedsRelationshipAnswer(question) {
			t.Fatalf("relationship variant %q was not detected", question)
		}
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
	for _, question := range []string{
		"When did Example first appear?",
		"What was the publication date of Example?",
		"How long has Example existed?",
		"How old is Example?",
	} {
		if !askNeedsTemporalAnswer(question) {
			t.Fatalf("temporal variant %q was not detected", question)
		}
	}
	if got := askTemporalPhrase("What year was Example founded?"); got != "was founded" {
		t.Fatalf("askTemporalPhrase() = %q, want was founded", got)
	}
	for question, want := range map[string]string{
		"What year did Example come out?":    "was first released",
		"What year was Example published?":   "was published",
		"When did Example first debut?":      "first appeared",
		"How long ago was Example released?": "dates to",
		"What year was Example established?": "was established",
		"What year was Example introduced?":  "was introduced",
		"What year did Example begin?":       "began",
	} {
		if got := askTemporalPhrase(question); got != want {
			t.Errorf("askTemporalPhrase(%q) = %q, want %q", question, got, want)
		}
	}
}

func TestAskNeedsWebResultAnswer(t *testing.T) {
	for _, question := range []string{
		"Why is this comedian controversial?",
		"What makes this service unpopular?",
		"Why do people criticize this product?",
	} {
		if !askNeedsWebResultAnswer(question) {
			t.Fatalf("askNeedsWebResultAnswer(%q) = false; want web-result fallback", question)
		}
	}
	if askNeedsWebResultAnswer("What is Linux?") {
		t.Fatal("definition question was incorrectly classified as open web opinion")
	}
}

func TestFormatWikidataDate(t *testing.T) {
	if got := formatWikidataDate("+2002-03-11T00:00:00Z"); got != "11 March 2002" {
		t.Fatalf("formatWikidataDate() = %q, want 11 March 2002", got)
	}
	if got := formatWikidataDate("+2002-03-00T00:00:00Z"); got != "March 2002" {
		t.Fatalf("formatWikidataDate(month precision) = %q, want March 2002", got)
	}
	if got := formatWikidataDate("+2002-00-00T00:00:00Z"); got != "2002" {
		t.Fatalf("formatWikidataDate(year precision) = %q, want 2002", got)
	}
	if got := formatWikidataDate("invalid"); got != "" {
		t.Fatalf("formatWikidataDate(invalid) = %q, want empty", got)
	}
	if got := formatAskTemporalSummary("Example", "When was Example published?", "March 2002"); got != "Example was published in March 2002." {
		t.Fatalf("formatAskTemporalSummary(month) = %q, want month-level preposition", got)
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
	source, ok := (&Ask{}).findSource(context.Background(), "Who created Linux?", bot.PluginConfig{"search_assist_enabled": false, "duckduckgo_enabled": true, "wikidata_fallback": false})
	if !ok || source.Summary != "Linus Torvalds is credited with creating Linux." {
		t.Fatalf("findSource() = %#v, %v; want refined relationship answer", source, ok)
	}
	if len(queries) != 3 || queries[0] != "Who created Linux?" || queries[1] != "Who created Linux?" || queries[2] != "linux" {
		t.Fatalf("DuckDuckGo queries = %#v; want a bounded retry followed by focused term", queries)
	}
}

func TestParseDuckDuckGoSearchAssist(t *testing.T) {
	body := `window.execDeep = function() {DDG.deep.deepPayload = {"instantAnswers":[{"data":{"answer":"Arch Linux was **created by Judd Vinet** in March 2002.","sources":[{"article":{"link":"https://en.wikipedia.org/wiki/Arch_Linux","site":"Wikipedia","text":"Arch Linux"}}]}}]};DDG.deep.bn={};}`
	source, ok := parseDuckDuckGoSearchAssist(body, "https://duckduckgo.com/?q=who+created+Arch+Linux%3F")
	if !ok || source.Summary != "Arch Linux was created by Judd Vinet in March 2002." || source.URL != "https://en.wikipedia.org/wiki/Arch_Linux" {
		t.Fatalf("parseDuckDuckGoSearchAssist() = %#v, %v; want sanitized answer and cited source", source, ok)
	}
}

func TestParseRenderedSearchAssist(t *testing.T) {
	source, ok := parseRenderedSearchAssist(askRenderedSearchAssistData{
		Text:  "Search Assist\nObZen is not a band; it is an album by Meshuggah.\nMore",
		Links: []string{"https://en.wikipedia.org/wiki/ObZen"},
	}, "https://duckduckgo.com/?q=what+genre+is+the+band+ObZen%3F")
	if !ok || source.Summary != "ObZen is not a band; it is an album by Meshuggah." || source.URL != "https://en.wikipedia.org/wiki/ObZen" {
		t.Fatalf("parseRenderedSearchAssist() = %#v, %v; want rendered answer and cited source", source, ok)
	}
}

func TestSelectAskSearchResultPrefersRelevantTitle(t *testing.T) {
	results := []askRenderedSearchResult{
		{Title: "Unrelated news", URL: "https://example.com/news", Snippet: "A general article."},
		{Title: "Why Ari Shaffir is controversial", URL: "https://www.reddit.com/r/Standup/comments/abc123/example/", Snippet: "A discussion of the comedian's public conduct."},
	}
	got, ok := selectAskSearchResult("Why is Ari Shaffir controversial?", results)
	if !ok || got.URL != results[1].URL {
		t.Fatalf("selectAskSearchResult() = %#v, %v; want relevant result", got, ok)
	}
}

func TestParseBingSearchResultsParsesResultCards(t *testing.T) {
	body := []byte(`<html><body><li class="b_algo"><h2><a href="https://example.com/article">Relevant article</a></h2><div class="b_caption"><p>A useful source excerpt.</p></div></li></body></html>`)
	results := parseBingSearchResults(body)
	if len(results) != 1 || results[0].URL != "https://example.com/article" || results[0].Snippet != "A useful source excerpt." {
		t.Fatalf("parseBingSearchResults() = %#v; want one parsed result", results)
	}
}

func TestUnwrapBingResultURL(t *testing.T) {
	raw := "https://www.bing.com/ck/a?u=a1aHR0cHM6Ly9leGFtcGxlLmNvbS9hcnRpY2xl"
	if got := unwrapBingResultURL(raw); got != "https://example.com/article" {
		t.Fatalf("unwrapBingResultURL() = %q; want direct public URL", got)
	}
}

func TestFetchAskRedditResultUsesPublicJSON(t *testing.T) {
	old := askWebHTTPClient
	t.Cleanup(func() { askWebHTTPClient = old })
	askWebHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, ".json") || r.URL.Host != "www.reddit.com" {
			t.Fatalf("unexpected Reddit request: %s", r.URL)
		}
		return newPluginResponse(http.StatusOK, `[
      {"data":{"children":[{"data":{"title":"A discussion about a comedian","selftext":"The discussion focuses on controversial public conduct.","permalink":"/r/Standup/comments/abc123/example/"}}]}},
      {"data":{"children":[]}}
    ]`), nil
	})}
	source, ok := fetchAskRedditResult(context.Background(), askRenderedSearchResult{
		Title: "A discussion about a comedian", URL: "https://www.reddit.com/r/Standup/comments/abc123/example/",
	})
	if !ok || !strings.Contains(source.Summary, "controversial public conduct") || source.URL == "" {
		t.Fatalf("fetchAskRedditResult() = %#v, %v; want attributed Reddit excerpt", source, ok)
	}
}

func TestFetchAskSearchResultUsesBoundedHTMLExcerpt(t *testing.T) {
	old := askWebHTTPClient
	t.Cleanup(func() { askWebHTTPClient = old })
	askWebHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		response := newPluginResponse(http.StatusOK, `<html><head><meta property="og:description" content="The source explains the public controversy." /></head><body><p>More page text.</p></body></html>`)
		response.Header.Set("Content-Type", "text/html; charset=utf-8")
		return response, nil
	})}
	source, ok := fetchAskSearchResult(context.Background(), askRenderedSearchResult{
		Title: "Relevant article", URL: "https://example.com/article", Snippet: "Search snippet",
	}, "why is this controversial?")
	if !ok || !strings.Contains(source.Summary, "public controversy") || source.URL != "https://example.com/article" {
		t.Fatalf("fetchAskSearchResult() = %#v, %v; want bounded source excerpt", source, ok)
	}
}

func TestAskSourceURLRejectsPrivateAndUnusualDestinations(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1/secret",
		"http://localhost/secret",
		"https://[::1]/secret",
		"https://example.com:8080/secret",
		"file:///etc/passwd",
	} {
		if validPublicHTTPURL(raw) {
			t.Fatalf("validPublicHTTPURL(%q) = true; want rejected", raw)
		}
	}
	if !validPublicHTTPURL("https://example.com/path") {
		t.Fatal("validPublicHTTPURL rejected a normal HTTPS URL")
	}
}

func TestExtractAskHTMLExcerptPrefersMetadata(t *testing.T) {
	body := []byte(`<html><head><meta name="description" content="A concise source description."></head><body><p>Long page text.</p></body></html>`)
	if got := extractAskHTMLExcerpt(body, "", "Example", []string{"example"}); got != "A concise source description." {
		t.Fatalf("extractAskHTMLExcerpt() = %q, want metadata description", got)
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

func TestAskNoAnswerProvidesSearchFallback(t *testing.T) {
	got := formatAskNoAnswer("Is the Earth flat?", 180)
	if !strings.Contains(got, "I couldn't find a reliable answer") || !strings.Contains(got, "https://duckduckgo.com/?q=Is+the+Earth+flat%3F") {
		t.Fatalf("formatAskNoAnswer() = %q, want bounded search fallback", got)
	}
	if strings.ContainsAny(got, "\r\n") || len([]byte(got)) > 180 {
		t.Fatalf("formatAskNoAnswer() is not one bounded line: %q", got)
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

func TestAskCacheNormalizesRepeatedQuestions(t *testing.T) {
	p := &Ask{}
	if err := p.Init(bot.PluginConfig{"cache_seconds": 300}, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	want := askSource{Title: "Debian", Summary: "Debian is a Unix-like operating system.", URL: "https://example.com/debian"}
	p.cacheSource("What is Debian?", want, 300)
	got, ok := p.cached("  what   is debian! ", 300)
	if !ok || got != want {
		t.Fatalf("cached() = %#v, %v; want normalized cached source", got, ok)
	}
}
