package plugins

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type cveRoundTripper func(*http.Request) (*http.Response, error)

func (f cveRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func cveTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestLookupCVEParsesScoreAndAffectedProducts(t *testing.T) {
	oldClient := cveHTTPClient
	t.Cleanup(func() { cveHTTPClient = oldClient })
	cveHTTPClient = &http.Client{Transport: cveRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("cveId") != "CVE-2024-1234" {
			t.Fatalf("unexpected CVE query: %s", r.URL.String())
		}
		return cveTestResponse(http.StatusOK, `{"vulnerabilities":[{"cve":{"id":"CVE-2024-1234","descriptions":[{"lang":"en","value":"Example vulnerability\u000a"}],"metrics":{"cvssMetricV31":[{"cvssData":{"baseScore":9.8,"baseSeverity":"CRITICAL"}}]},"configurations":[{"nodes":[{"cpeMatch":[{"criteria":"cpe:2.3:a:example:product:1.2.3:*:*:*:*:*:*:*"},{"criteria":"cpe:2.3:a:example:product:1.2.3:*:*:*:*:*:*:*"},{"criteria":"cpe:2.3:o:vendor:os:10:*:*:*:*:*:*:*"}]}]}]}}]}`), nil
	})}

	result, err := lookupCVE(t.Context(), "CVE-2024-1234")
	if err != nil {
		t.Fatalf("lookupCVE returned error: %v", err)
	}
	if !result.HasScore || result.Score != 9.8 || result.Severity != "CRITICAL" {
		t.Fatalf("unexpected score: %+v", result)
	}
	if len(result.Affected) != 2 || result.Affected[0] != "example/product 1.2.3" || result.Affected[1] != "vendor/os 10" {
		t.Fatalf("unexpected affected products: %+v", result.Affected)
	}
	if strings.ContainsAny(result.Description, "\r\n") {
		t.Fatalf("description contains control characters: %q", result.Description)
	}
}

func TestFormatCVEResultIncludesBoundedUsefulFields(t *testing.T) {
	message := formatCVEResult(cveResult{
		ID:       "CVE-2024-1234",
		Score:    9.8,
		Severity: "CRITICAL",
		HasScore: true,
		Affected: []string{"example/product 1.2.3"},
	}, 360)
	plain := stripPluginIRC(message)
	for _, want := range []string{"[CVE]", "CVE-2024-1234", "CVSS 9.8 CRITICAL", "affected: example/product 1.2.3", "https://nvd.nist.gov/vuln/detail/CVE-2024-1234"} {
		if !strings.Contains(plain, want) {
			t.Errorf("formatted CVE %q does not contain %q", plain, want)
		}
	}
}

func TestCVEValidation(t *testing.T) {
	for _, value := range []string{"CVE-2024-1234", "cve-1999-0001"} {
		if !cveIDPattern.MatchString(value) {
			t.Errorf("valid CVE rejected: %q", value)
		}
	}
	for _, value := range []string{"CVE-24-1234", "CVE-2024", "CVE-2024-abc", "https://nvd.nist.gov/"} {
		if cveIDPattern.MatchString(value) {
			t.Errorf("invalid CVE accepted: %q", value)
		}
	}
}

func TestFormatCVEResultDoesNotTrustProviderID(t *testing.T) {
	message := stripPluginIRC(formatCVEResult(cveResult{ID: "CVE-2024-1234 BAD\r\n"}, 360))
	if strings.ContainsAny(message, "\x03\r\n") {
		t.Fatalf("formatted CVE contains IRC controls or line breaks: %q", message)
	}
	if strings.Contains(message, "nvd.nist.gov/vuln/detail") {
		t.Fatalf("invalid provider ID produced a link: %q", message)
	}
}
