package plugins

import (
	"net/http"
	"strings"
	"testing"
)

func TestLookupPackageMetadataNPM(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://registry.npmjs.org/lodash/latest" {
			t.Fatalf("unexpected package endpoint: %s", r.URL)
		}
		return newPluginResponse(http.StatusOK, `{"version":"4.17.21","description":"utility\u000aline"}`), nil
	})}
	metadata, err := lookupPackageMetadata(t.Context(), "npm", "lodash", "")
	if err != nil {
		t.Fatal(err)
	}
	got := cleanExternalText(formatPackageMetadata(metadata))
	for _, want := range []string{"[npm] lodash 4.17.21", "utilityline", "https://npmjs.com/package/lodash"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatted package %q does not contain %q", got, want)
		}
	}
}

func TestLookupPackageMetadataGoProxyVersion(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://proxy.golang.org/gopkg.in/irc.v3/@latest" {
			t.Fatalf("unexpected Go module endpoint: %s", r.URL)
		}
		return newPluginResponse(http.StatusOK, `{"Version":"v3.1.4","Time":"2021-01-19T17:45:41Z"}`), nil
	})}
	metadata, err := lookupPackageMetadata(t.Context(), "Go", "gopkg.in/irc.v3", "")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Version != "v3.1.4" {
		t.Fatalf("Go module version = %q, want v3.1.4", metadata.Version)
	}
}

func TestSearchNPMPackageCandidates(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://registry.npmjs.org/-/v1/search?text=libirc&size=5" {
			t.Fatalf("unexpected npm search endpoint: %s", r.URL)
		}
		return newPluginResponse(http.StatusOK, `{"objects":[{"package":{"name":"libirc-client","version":"0.0.3","links":{"npm":"https://www.npmjs.com/package/libirc-client"}}}]}`), nil
	})}
	candidates, err := searchPackageCandidates(t.Context(), "npm", "libirc")
	if err != nil || len(candidates) != 1 || candidates[0].Name != "libirc-client" {
		t.Fatalf("npm candidates = %+v, %v", candidates, err)
	}
}

func TestSearchGoPackageCandidates(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://pkg.go.dev/search?m=package&limit=5&q=irc" {
			t.Fatalf("unexpected Go search endpoint: %s", r.URL)
		}
		return newPluginResponse(http.StatusOK, `<a data-test-id="snippet-title" href="/github.com/go-irc/irc">irc</a>`), nil
	})}
	candidates, err := searchPackageCandidates(t.Context(), "Go", "irc")
	if err != nil || len(candidates) != 1 || candidates[0].Name != "github.com/go-irc/irc" {
		t.Fatalf("Go candidates = %+v, %v", candidates, err)
	}
}

func TestFormatPackageSuggestionsUsesSearchResults(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		return newPluginResponse(http.StatusOK, `{"objects":[{"package":{"name":"libirc-client","version":"0.0.3"}}]}`), nil
	})}
	got := formatPackageSuggestions(t.Context(), "npm", "libirc")
	if !strings.Contains(got, "possible matches: libirc-client 0.0.3") {
		t.Fatalf("suggestion output = %q", got)
	}
}
