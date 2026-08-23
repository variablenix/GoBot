package plugins

import (
	"net/http"
	"strings"
	"testing"
)

func TestIMDbCommandsAndHelp(t *testing.T) {
	plugin := &IMDb{}
	if got := plugin.Commands(); len(got) != 1 || got[0] != "imdb" {
		t.Fatalf("commands = %#v, want [imdb]", got)
	}
	if !strings.Contains(plugin.Help(), "!imdb <movie or film>") {
		t.Fatalf("help = %q", plugin.Help())
	}
}

func TestIMDbPrefixHasNoVariationSelector(t *testing.T) {
	if imdbPrefix != "\U0001F3A5" {
		t.Fatalf("imdbPrefix = %q, want only U+1F3A5 without U+FE0F", imdbPrefix)
	}
}

func TestValidIMDbQuery(t *testing.T) {
	if !validIMDbQuery("The Matrix") {
		t.Fatal("expected a normal title query to be valid")
	}
	for _, query := range []string{"", "hello\nworld", strings.Repeat("x", 121)} {
		if validIMDbQuery(query) {
			t.Fatalf("query %q should be invalid", query)
		}
	}
}

func TestLookupIMDbTitlesFiltersPeople(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://v3.sg.media-imdb.com/suggestion/x/inception.json" {
			t.Fatalf("unexpected IMDb endpoint: %s", r.URL)
		}
		return newPluginResponse(http.StatusOK, `{"d":[{"id":"nm0000138","l":"Tom Hanks","q":"actor","qid":"name"},{"id":"tt1375666","l":"Inception","q":"feature","qid":"movie","s":"Leonardo DiCaprio, Joseph Gordon-Levitt","y":2010},{"id":"not-an-imdb-id","l":"Bad result","q":"feature","qid":"movie"}]}`), nil
	})}
	titles, err := lookupIMDbTitles(t.Context(), "inception")
	if err != nil || len(titles) != 1 || titles[0].ID != "tt1375666" {
		t.Fatalf("titles = %#v, error = %v", titles, err)
	}
}

func TestValidIMDbID(t *testing.T) {
	for _, id := range []string{"tt1375666", "tt0000001"} {
		if !validIMDbID(id) {
			t.Errorf("validIMDbID(%q) = false", id)
		}
	}
	for _, id := range []string{"", "nm0000138", "tt", "tt12x", "tt/123"} {
		if validIMDbID(id) {
			t.Errorf("validIMDbID(%q) = true", id)
		}
	}
}

func TestFormatIMDbResultsIsBoundedAndIncludesMoreCount(t *testing.T) {
	titles := []imdbTitle{
		{ID: "tt1375666", Label: "Inception", KindID: "movie", Year: 2010, Stars: "Leonardo DiCaprio"},
		{ID: "tt0133093", Label: "The Matrix", KindID: "movie", Year: 1999},
		{ID: "tt0816692", Label: "Interstellar", KindID: "movie", Year: 2014},
		{ID: "tt0110912", Label: "Pulp Fiction", KindID: "movie", Year: 1994},
	}
	got := formatIMDbResults(titles, 3)
	for _, want := range []string{"🎥 IMDb:", "Inception (2010; movie; Leonardo DiCaprio)", "https://www.imdb.com/title/tt1375666/", "+ 1 more"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatted result %q does not contain %q", got, want)
		}
	}
}
