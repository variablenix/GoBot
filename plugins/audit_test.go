package plugins

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAuditQueryOmitsVersionWhenAuditingLatest(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if _, ok := body["version"]; ok {
			t.Fatal("latest audit query unexpectedly included version")
		}
		return newPluginResponse(http.StatusOK, `{"vulns":[]}`), nil
	})}
	response, err := queryOSV(t.Context(), "npm", "demo", "")
	if err != nil || len(response.Vulns) != 0 {
		t.Fatalf("queryOSV() = %+v, %v", response, err)
	}
}

func TestAuditFormatsCVESeverityFixedAndMore(t *testing.T) {
	vulns := []osvVulnerability{
		{ID: "GHSA-demo", Aliases: []string{"CVE-2024-0001"}, DatabaseSpecific: map[string]json.RawMessage{"severity": json.RawMessage(`"HIGH"`)}, Affected: []osvAffected{{Ranges: []osvRange{{Events: []osvEvent{{Fixed: "2.0.0"}}}}}}},
		{ID: "CVE-2024-0002"},
	}
	got := cleanExternalText(formatAuditExact("demo", "1.0.0", vulns, 1))
	for _, want := range []string{"2 vulns: CVE-2024-0001 (HIGH)", "+ 1 more", "fixed in 2.0.0"} {
		if !strings.Contains(got, want) {
			t.Errorf("audit output %q does not contain %q", got, want)
		}
	}
	if !rangeAffectsVersion(osvRange{Events: []osvEvent{{Introduced: "0"}, {Fixed: "2.0.0"}}}, "1.9.0") || rangeAffectsVersion(osvRange{Events: []osvEvent{{Introduced: "0"}, {Fixed: "2.0.0"}}}, "2.0.0") {
		t.Fatal("OSV range matching is incorrect")
	}
}

func TestFormatAuditSuggestionsUsesSearchResults(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		return newPluginResponse(http.StatusOK, `{"objects":[{"package":{"name":"libirc-client","version":"0.0.3"}}]}`), nil
	})}
	got := formatAuditSuggestions(t.Context(), "npm", "libirc")
	if !strings.Contains(got, "possible packages: libirc-client 0.0.3") {
		t.Fatalf("audit suggestion output = %q", got)
	}
}
