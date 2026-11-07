package plugins

import (
	"net/http"
	"strings"
	"testing"
)

func TestAskComparisonRejectsOneSidedSearchFallbacks(t *testing.T) {
	question := "how much do Alpha plans cost vs Beta plans?"
	for _, text := range []string{"Alpha plans cost $10.", "Beta plans start at $20.", "Plans have different prices."} {
		if askComparisonRelevant(question, text) {
			t.Fatalf("one-sided or generic excerpt accepted: %s", text)
		}
	}
	if !askComparisonRelevant(question, "Alpha plans start at $10, while Beta plans start at $20.") {
		t.Fatal("complete comparison was rejected")
	}
	results := []askRenderedSearchResult{
		{Title: "Alpha plan prices", Snippet: "Alpha plans cost $10.", URL: "https://example.org/alpha"},
		{Title: "Alpha vs Beta pricing", Snippet: "A comparison of their plans.", URL: "https://example.org/comparison"},
	}
	selected, ok := selectAskSearchResult(question, results)
	if !ok || !strings.HasSuffix(selected.URL, "/comparison") {
		t.Fatal("first one-sided result won over a comparison")
	}
	if _, ok := selectAskSearchResult(question, results[:1]); ok {
		t.Fatal("one-sided source should leave other providers available")
	}
	if !askComparisonRelevant("what is Alpha?", "Alpha is an example service.") {
		t.Fatal("non-comparison lookup changed")
	}
	if !askComparisonRelevant("Alpha vs. Beta", "Alpha and Beta compared.") {
		t.Fatal("punctuated comparison rejected")
	}
}

func TestAskComparisonKeepsRelevantSnippetWhenOriginLosesContext(t *testing.T) {
	old := askWebHTTPClient
	t.Cleanup(func() { askWebHTTPClient = old })
	askWebHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
		return newPluginResponse(200, `<html><meta name="description" content="Alpha pricing only."></html>`), nil
	})}
	source, found := fetchAskSearchResult(t.Context(), askRenderedSearchResult{
		Title: "Plan pricing", Snippet: "Alpha starts at $10, Beta at $20.", URL: "https://example.org/prices",
	}, "Alpha vs Beta prices")
	if !found || !strings.Contains(source.Summary, "Beta") {
		t.Fatal("origin refresh discarded the complete comparison snippet")
	}
}
