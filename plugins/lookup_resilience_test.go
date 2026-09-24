package plugins

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/variablenix/GoBot/bot"
	"go.uber.org/zap"
)

func TestDefinitionFallback(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	for _, status := range []int{http.StatusNotFound, http.StatusServiceUnavailable, http.StatusTooManyRequests} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "api.dictionaryapi.dev" {
					return newPluginResponse(status, `{}`), nil
				}
				if r.URL.EscapedPath() != "/api/rest_v1/page/definition/Linux" {
					t.Fatalf("unexpected path %s", r.URL.EscapedPath())
				}
				return newPluginResponse(200, `{"fr":[{"definitions":[{"definition":"wrong language"}]}],"en":[{"partOfSpeech":"Proper noun","definitions":[{"definition":""},{"definition":"A <b>Unix-like</b> operating system."}]}]}`), nil
			})}
			got, ok := lookupDefinition(t.Context(), "Linux")
			if !ok || got.Definition != "A Unix-like operating system." || !strings.HasSuffix(got.URL, "Linux#English") {
				t.Fatalf("%+v %v", got, ok)
			}
		})
	}
}

func TestDefinitionTimeoutLeavesFallbackBudget(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.dictionaryapi.dev" {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		if r.Context().Err() != nil {
			t.Fatal("fallback inherited expired primary context")
		}
		return newPluginResponse(200, `{"en":[{"definitions":[{"definition":"A greeting."}]}]}`), nil
	})}
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	if _, ok := lookupDefinition(ctx, "hello"); !ok {
		t.Fatal("timeout prevented fallback")
	}
}

func TestDefinitionKeepsPrimarySuccess(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.dictionaryapi.dev" {
			t.Fatal("unnecessary fallback")
		}
		return newPluginResponse(200, `[{"word":"hello","meanings":[{"partOfSpeech":"interjection","definitions":[{"definition":"A greeting."}]}]}]`), nil
	})}
	got, ok := lookupDefinition(t.Context(), "hello")
	if !ok || got.Definition != "A greeting." || got.URL != "" {
		t.Fatalf("%+v %v", got, ok)
	}
}

func TestWiktionaryRejectsInvalidPayload(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	for _, body := range []string{`not json`, `{}`, `{"fr":[{"definitions":[{"definition":"bonjour"}]}]}`, `{"en":[{"definitions":[{"definition":"<b></b>"}]}]}`} {
		apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) { return newPluginResponse(200, body), nil })}
		if _, ok := wiktionaryEntry(t.Context(), "hello"); ok {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestSearchAssistRejectsForeignPreload(t *testing.T) {
	old := askHTTPClient
	t.Cleanup(func() { askHTTPClient = old })
	calls := 0
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		return newPluginResponse(200, `<script id="deep_preload_script" src="http://127.0.0.1/private"></script>`), nil
	})}
	if _, ok := askDuckDuckGoSearchAssistOnce(t.Context(), "hello", "https://duckduckgo.com/"); ok || calls != 1 {
		t.Fatal("untrusted preload was followed")
	}
}

func TestAskStepReservesTime(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	step, done := askStepContext(ctx, 5*time.Second)
	defer done()
	deadline, _ := step.Deadline()
	if time.Until(deadline) > 600*time.Millisecond {
		t.Fatal("step can exhaust entire parent budget")
	}
}

func TestWikipediaNegativeLengthDoesNotPanic(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		return newPluginResponse(200, `{"title":"Linux","extract":"Linux is an operating system.","content_urls":{"desktop":{"page":"https://en.wikipedia.org/wiki/Linux"}}}`), nil
	})}
	p := &Wikipedia{}
	p.Init(bot.PluginConfig{"max_summary_length": -1}, nil)
	b := bot.New(bot.Config{CommandPrefix: "!"}, nil, nil, zap.NewNop())
	if !p.Handle(b, bot.Message{Text: "!wiki Linux", Nick: "tester", Target: "#test", IsChannel: true}) {
		t.Fatal("command not handled")
	}
}

func TestAskWikipediaFallback(t *testing.T) {
	oldAPI, oldAsk := apiHTTPClient, askHTTPClient
	t.Cleanup(func() { apiHTTPClient, askHTTPClient = oldAPI, oldAsk })
	askHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) { return newPluginResponse(503, `{}`), nil })}
	calls := 0
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		return newPluginResponse(200, `{"title":"Linux","extract":"Linux is an operating system.","content_urls":{"desktop":{"page":"https://en.wikipedia.org/wiki/Linux"}}}`), nil
	})}
	cfg := bot.PluginConfig{"search_assist_enabled": false, "search_assist_browser_enabled": false, "search_results_enabled": false, "duckduckgo_enabled": false}
	got, ok := (&Ask{}).findSource(t.Context(), "what is Linux?", cfg)
	if !ok || got.Provider != "wikipedia" {
		t.Fatalf("unexpected fallback: %+v, %v", got, ok)
	}
	calls = 0
	if _, ok := (&Ask{}).findSource(t.Context(), "why is Linux popular?", cfg); ok || calls != 0 {
		t.Fatal("entity summary used for opinion question")
	}
}

// Explicitly opt in on the deployment host. Normal CI is deterministic and
// does not depend on third-party availability or rate limits.
func TestLiveLookupProviders(t *testing.T) {
	if os.Getenv("GOBOT_LIVE_LOOKUPS") != "1" {
		t.Skip("opt-in live provider smoke test")
	}
	for _, term := range []string{"Linux", "hello", "resilient"} {
		t.Run("define/"+term, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			result, ok := lookupDefinition(ctx, term)
			if !ok {
				t.Fatal("no definition")
			}
			t.Logf("%s: %s", term, result.Definition)
		})
	}
	for _, q := range []string{"what is Linux?", "who created Arch Linux?", "when was Debian first released?", "why is the sky blue?"} {
		t.Run("ask/"+q, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			result, ok := (&Ask{}).findSource(ctx, q, bot.PluginConfig{})
			if !ok {
				t.Fatal("no answer")
			}
			t.Logf("provider=%s answer=%s", result.Provider, result.Summary)
		})
	}
}
