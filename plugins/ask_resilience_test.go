package plugins

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/variablenix/GoBot/bot"
)

func TestAskCostComparisonsNeedWebAnswers(t *testing.T) {
	for _, question := range []string{
		"how much do dirt bikes cost vs GoKarts?", "how much does a piano cost?",
		"compare buses and trains", "electric cars versus petrol cars",
		"What is the difference between Linux and BSD?", "price of bikes vs. karts",
	} {
		if !askNeedsWebResultAnswer(question) {
			t.Errorf("not classified as web question: %q", question)
		}
	}
	for _, question := range []string{"what is Linux?", "who created Arch Linux?", "when was Debian released?"} {
		if askNeedsWebResultAnswer(question) {
			t.Errorf("entity question misclassified: %q", question)
		}
	}
	got := askSearchAssistQueryVariants("how much do dirt bikes cost vs GoKarts?")
	want := []string{"how much do dirt bikes cost vs GoKarts?", "how much do dirt bikes cost versus go karts?"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("variants=%q", got)
	}
}

func TestSearchAssistPreloadMarkupVariants(t *testing.T) {
	for _, markup := range []string{
		`<script src="/assist.js?q=a&amp;x=b" defer id="deep_preload_script"></script>`,
		"<script\n id='deep_preload_script'\n src='https://duckduckgo.com/assist.js?q=a&amp;x=b'></script>",
	} {
		if got := askSearchAssistPreloadURL([]byte(markup), "https://duckduckgo.com/?q=test"); got != "https://duckduckgo.com/assist.js?q=a&x=b" {
			t.Fatalf("preload=%q", got)
		}
	}
	if got := askSearchAssistPreloadURL([]byte(`<script src="/other.js"></script>`), "https://duckduckgo.com/"); got != "" {
		t.Fatal("unrelated script accepted")
	}
}

func TestSearchAssistPayloadWhitespaceAndTrailingCode(t *testing.T) {
	for _, body := range []string{
		`DDG.deep.deepPayload={"instantAnswers":[{"data":{"answer":"A sourced comparison.","sources":[]}}]};differentCallback();`,
		"DDG.deep.deepPayload\n=\n{\"instantAnswers\":[{\"data\":{\"answer\":\"A sourced comparison.\"}}]};",
	} {
		got, ok := parseDuckDuckGoSearchAssist(body, "https://duckduckgo.com/?q=test")
		if !ok || got.Summary != "A sourced comparison." {
			t.Fatalf("%+v %v", got, ok)
		}
	}
	if _, ok := parseDuckDuckGoSearchAssist(`DDG.deep.deepPayload={broken`, ""); ok {
		t.Fatal("malformed JSON accepted")
	}
}

func TestAskComparisonRecoversOnNormalizedRetry(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	var queries []string
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/assist.js" {
			return newPluginResponse(200, `DDG.deep.deepPayload={"instantAnswers":[{"data":{"answer":"Compare equivalent dirt bikes and go-karts, including maintenance costs.","sources":[{"article":{"link":"https://example.org/comparison"}}]}}]};callback();`), nil
		}
		if r.URL.Query().Get("assiston") != "1" {
			t.Fatal("Search Assist was not explicitly requested")
		}
		queries = append(queries, r.URL.Query().Get("q"))
		if len(queries) == 1 {
			return newPluginResponse(200, `<html><body>No answer yet</body></html>`), nil
		}
		return newPluginResponse(200, `<script src="/assist.js" id="deep_preload_script"></script>`), nil
	})}
	got, ok := (&Ask{}).findSource(t.Context(), "how much do dirt bikes cost vs GoKarts?", bot.PluginConfig{})
	if !ok || got.Provider != "search_assist" || got.URL != "https://example.org/comparison" {
		t.Fatalf("retry failed: %+v %v", got, ok)
	}
	if !reflect.DeepEqual(queries, []string{"how much do dirt bikes cost vs GoKarts?", "how much do dirt bikes cost versus go karts?"}) {
		t.Fatalf("unexpected requests: %q", queries)
	}
}

func TestAskExplicitAssistURL(t *testing.T) {
	for _, question := range []string{"how much do dirt bikes cost vs GoKarts?", "R&D vs C++? &assiston=0", "日本語の質問"} {
		u, err := url.Parse(duckDuckGoSearchAssistURL(question))
		if err != nil || u.Host != "duckduckgo.com" || u.Scheme != "https" || u.Query().Get("q") != question || !reflect.DeepEqual(u.Query()["assiston"], []string{"1"}) {
			t.Fatalf("invalid explicit route: %v %v", u, err)
		}
	}
	if !strings.Contains(formatAskNoAnswer("a question", 360), "?assiston=1&q=a+question") {
		t.Fatal("fallback link does not request Search Assist")
	}
}

func TestAskRecoversDelayedAnswerInOriginalCommand(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	for _, question := range []string{"what kind of GoKart does Mario use?", "what kind of GoKart does Toad use?"} {
		t.Run(question, func(t *testing.T) {
			started := time.Now()
			pages := 0
			askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/assist.js" {
					return newPluginResponse(200, `DDG.deep.deepPayload={"instantAnswers":[{"data":{"answer":"The available kart depends on the game.","sources":[{"article":{"link":"https://example.org/karts"}}]}}]};`), nil
				}
				pages++
				if r.URL.Query().Get("q") != question {
					t.Fatal("recovery changed the original question")
				}
				if pages <= 2 || time.Since(started) < 750*time.Millisecond {
					return newPluginResponse(200, `<html>No generated answer yet</html>`), nil
				}
				return newPluginResponse(200, `<script id="deep_preload_script" src="/assist.js"></script>`), nil
			})}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			cfg := bot.PluginConfig{"search_assist_browser_enabled": false, "duckduckgo_enabled": false, "wikidata_fallback": false, "search_results_enabled": false}
			source, ok := (&Ask{}).findSource(ctx, question, cfg)
			if !ok || source.Provider != "search_assist" || pages != 3 {
				t.Fatalf("original command did not recover: source=%+v ok=%v requests=%d", source, ok, pages)
			}
		})
	}
}

func TestAskRecoveryIsBoundedAndCancelable(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	for _, timeout := range []time.Duration{50 * time.Millisecond, 2 * time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			pages := 0
			askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Context().Err() != nil {
					t.Fatal("request made after cancellation")
				}
				pages++
				return newPluginResponse(200, `<html>No answer</html>`), nil
			})}
			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			defer cancel()
			cfg := bot.PluginConfig{"search_assist_browser_enabled": false, "duckduckgo_enabled": false, "wikidata_fallback": false, "search_results_enabled": false}
			if _, ok := (&Ask{}).findSource(ctx, "what kind of kart?", cfg); ok {
				t.Fatal("fabricated an answer from an empty response")
			}
			want := 3
			if timeout < time.Second {
				want = 1
			}
			if pages != want {
				t.Fatalf("requests=%d want=%d", pages, want)
			}
		})
	}
}

func TestAskSuccessDoesNotTriggerRecovery(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	requests := 0
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Path == "/assist.js" {
			return newPluginResponse(200, `DDG.deep.deepPayload={"instantAnswers":[{"data":{"answer":"An existing successful answer."}}]};`), nil
		}
		return newPluginResponse(200, `<script id="deep_preload_script" src="/assist.js"></script>`), nil
	})}
	if _, ok := (&Ask{}).findSource(t.Context(), "a question", bot.PluginConfig{}); !ok || requests != 2 {
		t.Fatalf("successful path changed: ok=%v requests=%d", ok, requests)
	}
}

func TestAskReservesRecoveryBudgetWithoutExtendingDeadline(t *testing.T) {
	for _, budget := range []time.Duration{8 * time.Second, 20 * time.Second} {
		parent, cancel := context.WithTimeout(t.Context(), budget)
		ctx, done := askInitialLookupContext(parent)
		parentDeadline, _ := parent.Deadline()
		deadline, _ := ctx.Deadline()
		want := time.Duration(0)
		if budget == 20*time.Second {
			want = 3 * time.Second
		}
		if parentDeadline.Sub(deadline) != want {
			t.Errorf("budget=%s reserved=%s want=%s", budget, parentDeadline.Sub(deadline), want)
		}
		done()
		if parent.Err() != nil {
			t.Fatal("canceling initial lookup canceled recovery parent")
		}
		cancel()
	}
}

func TestLiveAskKartQuestions(t *testing.T) {
	if os.Getenv("GOBOT_LIVE_LOOKUPS") != "1" {
		t.Skip("opt-in live provider smoke test")
	}
	for _, question := range []string{"what kind of GoKart does Mario use?", "what kind of GoKart does Toad use?"} {
		t.Run(question, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			source, ok := (&Ask{}).findSource(ctx, question, bot.PluginConfig{})
			if !ok {
				t.Fatal("no answer within the original request")
			}
			t.Logf("provider=%s source=%s", source.Provider, source.URL)
		})
	}
}

func TestAskComparisonDoesNotReturnEntityDescription(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("entity provider should not be called for a cost comparison: %s", r.URL.Host)
		return nil, context.Canceled
	})}
	cfg := bot.PluginConfig{"search_assist_enabled": false, "search_assist_browser_enabled": false, "search_results_enabled": false}
	if _, ok := (&Ask{}).findSource(t.Context(), "how much do dirt bikes cost vs GoKarts?", cfg); ok {
		t.Fatal("generic answer substituted for comparison")
	}
}

func TestAskBrowserBudgetLeavesFallbackTime(t *testing.T) {
	parent, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	ctx, done := askBrowserStepContext(parent)
	defer done()
	deadline, _ := ctx.Deadline()
	remaining := time.Until(deadline)
	if remaining < 5*time.Second || remaining > 6*time.Second {
		t.Fatalf("browser budget=%s", remaining)
	}
}

// Run with GOBOT_TEST_BROWSER=/path/to/chromium. This fixture runs entirely
// offline and verifies the DOM code against a delayed, expanded answer card.
func TestAskRenderedCardWaitsForAnswer(t *testing.T) {
	executable := os.Getenv("GOBOT_TEST_BROWSER")
	if executable == "" {
		t.Skip("set GOBOT_TEST_BROWSER for offline browser fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	allocator, stop := chromedp.NewExecAllocator(ctx, append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(executable))...)
	defer stop()
	page, closePage := chromedp.NewContext(allocator)
	defer closePage()
	var before *askRenderedSearchAssistData
	var answer askRenderedSearchAssistData
	const fixture = `document.body.innerHTML='<nav><span>Search Assist</span></nav><section id="card"><header><span>Search Assist</span></header><p id="answer"></p><div id="sources"></div><button>More</button></section><article data-testid="result"><a data-testid="result-title-a" href="https://example.org/other">Unrelated search result</a></article>'`
	if err := chromedp.Run(page, chromedp.Navigate("about:blank"), chromedp.Evaluate(fixture, nil), chromedp.Evaluate("("+askRenderedSearchAssistScript+")()", &before)); err != nil {
		t.Fatal(err)
	}
	if before != nil {
		t.Fatalf("empty card accepted: %+v", before)
	}
	const delayed = `setTimeout(() => {document.getElementById('answer').textContent='Dirt bikes and go-karts have different purchase and ownership costs. Compare equivalent models before deciding.';document.getElementById('sources').innerHTML='<a href="https://example.org/comparison">Source</a>';const details=document.createElement('div');details.textContent='Additional comparison details. '.repeat(180);document.getElementById('card').append(details);}, 1800)`
	if err := chromedp.Run(page, chromedp.Evaluate(delayed, nil), chromedp.PollFunction(askRenderedSearchAssistScript, &answer, chromedp.WithPollingInterval(100*time.Millisecond), chromedp.WithPollingTimeout(4*time.Second))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer.Text, "purchase and ownership") || strings.Contains(answer.Text, "Unrelated") || len(answer.Links) != 1 {
		t.Fatalf("wrong card: %+v", answer)
	}
	// A short, cited answer is valid too: do not impose a prose-length floor.
	if err := chromedp.Run(page, chromedp.Evaluate(`document.getElementById('card').innerHTML='<header><svg></svg>Search Assist</header><p>2002</p><a href="https://example.org/date">Source</a>'`, nil), chromedp.Evaluate("("+askRenderedSearchAssistScript+")()", &answer)); err != nil {
		t.Fatal(err)
	}
	if answer.Text != "2002" {
		t.Fatalf("short answer lost: %+v", answer)
	}
}

func TestLiveAskCostComparison(t *testing.T) {
	if os.Getenv("GOBOT_LIVE_LOOKUPS") != "1" {
		t.Skip("opt-in live provider smoke test")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	source, ok := (&Ask{}).findSource(ctx, "how much do dirt bikes cost vs GoKarts?", bot.PluginConfig{})
	if !ok {
		t.Fatal("no comparison answer")
	}
	t.Logf("provider=%s source=%s answer=%s", source.Provider, source.URL, source.Summary)
	if source.Provider != "search_assist" && source.Provider != "search_result" {
		t.Fatalf("wrong provider for comparison: %s", source.Provider)
	}
}
