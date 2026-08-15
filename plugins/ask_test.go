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
		return newPluginResponse(http.StatusOK, `{"Heading":"Go","Answer":"Go is a programming language.","AbstractText":""}`), nil
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
