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
