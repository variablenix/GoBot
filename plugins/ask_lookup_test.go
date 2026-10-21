package plugins

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/variablenix/GoBot/bot"
)

func TestAskRaceHealthyAssistSkipsDelayedProviders(t *testing.T) {
	var delayedCalls atomic.Int32
	want := askSource{Summary: "A sourced answer.", Provider: "search_assist"}
	started := time.Now()
	source, found := askRaceSources(t.Context(), []askLookupTask{
		{lookup: func(context.Context) (askSource, bool) { return want, true }},
		{delay: time.Second, lookup: func(context.Context) (askSource, bool) {
			delayedCalls.Add(1)
			return askSource{}, false
		}},
	}, time.Second)
	if !found || source != want || delayedCalls.Load() != 0 || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("healthy answer was delayed or started unnecessary work: %+v, %t", source, found)
	}
}

func TestAskRaceFastFallbackCancelsAndJoinsSlowProvider(t *testing.T) {
	stopped := make(chan struct{})
	want := askSource{Summary: "A relevant public excerpt.", Provider: "search_result"}
	started := time.Now()
	source, found := askRaceSources(t.Context(), []askLookupTask{
		{lookup: func(ctx context.Context) (askSource, bool) {
			defer close(stopped)
			<-ctx.Done()
			return askSource{}, false
		}},
		{lookup: func(context.Context) (askSource, bool) { return want, true }},
	}, 20*time.Millisecond)
	if !found || source != want || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("slow provider held a ready answer: %+v, %t", source, found)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("losing worker outlived the lookup")
	}
}

func TestAskRacePrefersAssistWithinShortWindow(t *testing.T) {
	want := askSource{Summary: "The generated sourced answer.", Provider: "search_assist"}
	source, found := askRaceSources(t.Context(), []askLookupTask{
		{delay: 20 * time.Millisecond, lookup: func(context.Context) (askSource, bool) { return want, true }},
		{lookup: func(context.Context) (askSource, bool) {
			return askSource{Summary: "A relevant excerpt.", Provider: "search_result"}, true
		}},
	}, 500*time.Millisecond)
	if !found || source != want {
		t.Fatalf("lost the preferred answer within the grace period: %+v, %t", source, found)
	}
}

func TestAskRaceDoesNotInventAnswers(t *testing.T) {
	for _, tasks := range [][]askLookupTask{nil, {
		{lookup: func(context.Context) (askSource, bool) { return askSource{}, false }},
		{lookup: func(context.Context) (askSource, bool) { return askSource{Title: "No answer"}, true }},
	}} {
		if source, found := askRaceSources(t.Context(), tasks, time.Second); found || source.Summary != "" {
			t.Fatalf("an empty provider response became an answer: %+v", source)
		}
	}
}

func TestAskRaceProviderPanicDoesNotLoseOtherAnswer(t *testing.T) {
	want := askSource{Summary: "A sourced fallback remains available.", Provider: "search_result"}
	source, found := askRaceSources(t.Context(), []askLookupTask{
		{lookup: func(context.Context) (askSource, bool) { panic("simulated provider failure") }},
		{lookup: func(context.Context) (askSource, bool) { return want, true }},
	}, time.Second)
	if !found || source != want {
		t.Fatalf("one provider failure discarded the healthy result: %+v, %t", source, found)
	}
}

func TestAskRaceCanceledParentDoesNotStartWork(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, found := askRaceSources(ctx, []askLookupTask{{lookup: func(context.Context) (askSource, bool) {
		t.Fatal("lookup started after cancellation")
		return askSource{}, false
	}}}, time.Second)
	if found {
		t.Fatal("canceled lookup unexpectedly succeeded")
	}
}

func TestAskRaceDeadlineKeepsAlreadyAvailableAnswer(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 80*time.Millisecond)
	defer cancel()
	want := askSource{Summary: "Already available public information.", Provider: "search_result"}
	source, found := askRaceSources(ctx, []askLookupTask{
		{lookup: func(ctx context.Context) (askSource, bool) { <-ctx.Done(); return askSource{}, false }},
		{lookup: func(context.Context) (askSource, bool) { return want, true }},
	}, time.Second)
	if !found || source != want {
		t.Fatalf("deadline discarded a usable answer: %+v, %t", source, found)
	}
}

func TestAskFastFallbackIsIndependentOfStalledAssist(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.duckduckgo.com" {
			return newPluginResponse(200, `{"Heading":"Linux","AbstractText":"Linux is a family of free and open-source operating systems."}`), nil
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	cfg := bot.PluginConfig{"search_assist_browser_enabled": false, "search_results_enabled": false, "wikidata_fallback": false}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	started := time.Now()
	source, found := (&Ask{}).findSource(ctx, "what is Linux?", cfg)
	if !found || !strings.Contains(source.Summary, "operating systems") || time.Since(started) > 2*time.Second {
		t.Fatalf("independent fallback was blocked: %+v, %t", source, found)
	}
	if cfg["search_assist_enabled"] != nil || cfg["search_assist_browser_enabled"] != false {
		t.Fatal("lookup modified the caller's configuration")
	}
}

func TestAskInlineAssistPayloadNeedsOnlyOneRequest(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	requests := 0
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
		requests++
		return newPluginResponse(200, `<html><script>DDG.deep.deepPayload={"instantAnswers":[{"data":{"answer":"A directly available sourced answer.","sources":[{"article":{"link":"https://example.org/answer"}}]}}]};</script></html>`), nil
	})}
	source, found := (&Ask{}).findSource(t.Context(), "an example question?", bot.PluginConfig{})
	if !found || source.Provider != "search_assist" || requests != 1 || source.URL != "https://example.org/answer" {
		t.Fatalf("inline answer was missed or required extra requests: %+v, %t, %d", source, found, requests)
	}
}

func TestAskSpecificWebQuestionsDoNotRaceGenericEntityAnswers(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("generic entity lookup attempted for a specific web question")
		return nil, context.Canceled
	})}
	for _, question := range []string{
		`what music genre is the band "Example Band"?`, "what genre is the band Example Band?",
		"what kind of vehicle does Example Character use?", "how to install Example OS?",
		"what is an alternative to Example Service?",
	} {
		if !askNeedsWebResultAnswer(question) {
			t.Fatalf("specific question misclassified: %q", question)
		}
		if _, found := (&Ask{}).findSource(t.Context(), question, bot.PluginConfig{
			"search_assist_enabled": false, "search_assist_browser_enabled": false, "search_results_enabled": false,
		}); found {
			t.Fatal("specific question received a generic entity description")
		}
	}
}

func TestAskSnippetSurvivesSlowOrigin(t *testing.T) {
	old := askWebHTTPClient
	t.Cleanup(func() { askWebHTTPClient = old })
	askWebHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 600*time.Millisecond {
			t.Fatal("snippet refresh lacks a bounded deadline")
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	started := time.Now()
	source, found := fetchAskSearchResult(t.Context(), askRenderedSearchResult{
		Title: "Example comparison", URL: "https://example.org/comparison", Snippet: "Compare equivalent models and ongoing maintenance costs.",
	}, "compare the costs of bikes and karts")
	if !found || source.Provider != "search_result" || !strings.Contains(source.Summary, "maintenance costs") || time.Since(started) > 1500*time.Millisecond {
		t.Fatalf("slow origin lost or delayed the snippet: %+v, %t", source, found)
	}
}

func TestAskSearchFallbackKeepsCompleteEncodedURL(t *testing.T) {
	for _, question := range []string{"a simple question?", strings.Repeat("日本語 🎥 & C++? ", 40)} {
		for _, limit := range []int{120, 360, 450} {
			text := formatAskNoAnswer(question, limit)
			_, rawURL, ok := strings.Cut(text, ": ")
			parsed, err := url.Parse(rawURL)
			if !ok || err != nil || len(text) > limit || !utf8.ValidString(text) || strings.ContainsAny(text, "\r\n\uFE0F") {
				t.Fatalf("unsafe or truncated fallback: %q, %v", text, err)
			}
			query := parsed.Query().Get("q")
			if parsed.Host != "duckduckgo.com" || parsed.Scheme != "https" || parsed.Query().Get("assiston") != "1" || query == "" || !utf8.ValidString(query) || !strings.HasPrefix(question, query) {
				t.Fatalf("fallback no longer contains a valid query: %q", text)
			}
			if !strings.HasPrefix(text, "Search Assist / web results: ") {
				t.Fatalf("fallback is not clearly labeled as search results: %q", text)
			}
		}
	}
}
