package plugins

import (
	"net/http"
	"testing"
)

func TestDockerLookupRepositoryAndLatestTag(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v2/repositories/library/nginx/":
			return newPluginResponse(http.StatusOK, `{"pull_count":1200000000,"star_count":19400,"description":"Nginx HTTP server"}`), nil
		case "/v2/repositories/library/nginx/tags/":
			if r.URL.Query().Get("page_size") != "1" {
				t.Fatalf("missing tag page size: %s", r.URL)
			}
			return newPluginResponse(http.StatusOK, `{"results":[{"name":"1.25.3"}]}`), nil
		default:
			t.Fatalf("unexpected Docker endpoint: %s", r.URL)
			return nil, nil
		}
	})}
	repository, err := lookupDockerRepository(t.Context(), "library", "nginx")
	if err != nil || humanCount(repository.PullCount) != "1.2B" {
		t.Fatalf("repository = %+v, %v", repository, err)
	}
	tags, err := lookupDockerTags(t.Context(), "library", "nginx")
	if err != nil || tags.Results[0].Name != "1.25.3" {
		t.Fatalf("tags = %+v, %v", tags, err)
	}
}
