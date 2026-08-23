package plugins

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/variablenix/GoBot/bot"
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
	for _, query := range []string{"", "hello\nworld", "hidden\u200djoiner", "emoji\ufe0f", strings.Repeat("x", 121)} {
		if validIMDbQuery(query) {
			t.Fatalf("query %q should be invalid", query)
		}
	}
}

func TestCleanIMDbTextRemovesInvisibleFormatting(t *testing.T) {
	got := cleanIMDbText("The\ufe0f \u200dMovie\u202e\x03\x034")
	if got != "The Movie" {
		t.Fatalf("cleanIMDbText() = %q, want %q", got, "The Movie")
	}
	for _, forbidden := range []string{"\ufe0f", "\u200d", "\u202e", "\x03"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("cleaned IMDb text still contains %q: %q", forbidden, got)
		}
	}
}

func TestBoundIMDbReplyUsesUTF8WireByteLimit(t *testing.T) {
	target := "#international-movies"
	reply := boundIMDbReply(target, imdbPrefix+" IMDb: "+strings.Repeat("界", 300)+"\r\nsecond line", 500)
	wire := "PRIVMSG " + target + " :" + reply + "\r\n"
	if len([]byte(wire)) > imdbIRCMaxLineBytes {
		t.Fatalf("wire line is %d bytes, want at most %d", len([]byte(wire)), imdbIRCMaxLineBytes)
	}
	if !utf8.ValidString(reply) {
		t.Fatalf("reply is not valid UTF-8: %q", reply)
	}
	if strings.ContainsAny(reply, "\r\n") {
		t.Fatalf("reply contains a line break: %q", reply)
	}
	if !strings.HasSuffix(reply, "…") {
		t.Fatalf("truncated reply does not end with an ellipsis: %q", reply)
	}
	configuredReply := boundIMDbReply(target, strings.Repeat("界", 100), 120)
	if len([]byte(configuredReply)) > 120 {
		t.Fatalf("configured reply is %d bytes, want at most 120", len([]byte(configuredReply)))
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

func TestFormatIMDbResultsIncludesMoreCount(t *testing.T) {
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

func TestLookupIMDbTitlesHandlesUpstreamFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "server error", status: http.StatusInternalServerError, body: `{}`},
		{name: "malformed JSON", status: http.StatusOK, body: `{"d":[`},
		{name: "no usable titles", status: http.StatusOK, body: `{"d":[{"id":"nm0000138","l":"Person","qid":"name"}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			old := apiHTTPClient
			t.Cleanup(func() { apiHTTPClient = old })
			apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
				return newPluginResponse(test.status, test.body), nil
			})}
			if _, err := lookupIMDbTitles(t.Context(), "inception"); err == nil {
				t.Fatal("lookupIMDbTitles() error = nil")
			}
		})
	}
}

func TestLookupIMDbTitlesHonorsContextCancellation(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	if _, err := lookupIMDbTitles(ctx, "inception"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lookupIMDbTitles() error = %v, want context deadline exceeded", err)
	}
}

func TestIMDbHandleAlwaysSendsOneSafeWireLine(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		status   int
		body     string
		contains string
	}{
		{
			name:     "successful lookup sanitizes third-party text",
			query:    "international",
			status:   http.StatusOK,
			body:     `{"d":[{"id":"tt1234567","l":"` + strings.Repeat("界", 180) + `\ufe0f\u200d","q":"feature","qid":"movie","s":"Actor\u202e Name","y":2026}]}`,
			contains: imdbPrefix + " IMDb:",
		},
		{
			name:     "not found bounds multibyte query",
			query:    strings.Repeat("界", 120),
			status:   http.StatusNotFound,
			body:     `{}`,
			contains: "no movie or film found",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			old := apiHTTPClient
			t.Cleanup(func() { apiHTTPClient = old })
			apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
				return newPluginResponse(test.status, test.body), nil
			})}

			sent := make(chan bot.Outgoing, 2)
			cfg := bot.Config{NetworkName: "test", CommandPrefix: "!"}
			b := &bot.Bot{Config: cfg, Queue: bot.NewQueue(1000, 20, func(message bot.Outgoing) { sent <- message })}
			plugin := &IMDb{}
			if err := plugin.Init(bot.PluginConfig{"max_length": 500, "max_results": 5, "cooldown_seconds": 1}, nil); err != nil {
				t.Fatal(err)
			}
			message := bot.Message{Nick: "Alice", Target: "#movies", IsChannel: true, Text: "!imdb " + test.query}
			if !plugin.Handle(b, message) {
				t.Fatal("IMDb command was not consumed")
			}
			b.Queue.Drain(context.Background())

			if len(sent) != 1 {
				t.Fatalf("IMDb sent %d messages, want exactly one", len(sent))
			}
			outgoing := <-sent
			if !strings.Contains(outgoing.Text, test.contains) {
				t.Fatalf("reply %q does not contain %q", outgoing.Text, test.contains)
			}
			wire := "PRIVMSG " + outgoing.Target + " :" + outgoing.Text + "\r\n"
			if len([]byte(wire)) > imdbIRCMaxLineBytes {
				t.Fatalf("wire line is %d bytes, want at most %d", len([]byte(wire)), imdbIRCMaxLineBytes)
			}
			if !utf8.ValidString(outgoing.Text) || strings.ContainsAny(outgoing.Text, "\r\n") {
				t.Fatalf("reply is not a valid single UTF-8 line: %q", outgoing.Text)
			}
			for _, forbidden := range []string{"\ufe0f", "\u200d", "\u202e"} {
				if strings.Contains(outgoing.Text, forbidden) {
					t.Fatalf("reply contains unsafe formatting rune %q: %q", forbidden, outgoing.Text)
				}
			}
		})
	}
}
