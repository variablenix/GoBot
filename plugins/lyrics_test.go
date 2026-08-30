package plugins

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/variablenix/GoBot/bot"
)

func TestLyricsCommandsAndHelp(t *testing.T) {
	plugin := &Lyrics{}
	if got := plugin.Commands(); len(got) != 3 || got[0] != "lyrics" || got[1] != "lyric" || got[2] != "genius" {
		t.Fatalf("commands = %#v, want [lyrics lyric genius]", got)
	}
	if !strings.Contains(plugin.Help(), "!lyrics <song or artist>") {
		t.Fatalf("help = %q", plugin.Help())
	}
}

func TestValidLyricsQuery(t *testing.T) {
	for _, query := range []string{"Paranoid Android", `artist - "song title"`, "Beyoncé — Halo"} {
		if !validLyricsQuery(query) {
			t.Fatalf("validLyricsQuery(%q) = false", query)
		}
	}
	for _, query := range []string{"", "hello\nworld", "hidden\u200djoiner", "emoji\ufe0f", strings.Repeat("x", 161)} {
		if validLyricsQuery(query) {
			t.Fatalf("validLyricsQuery(%q) = true", query)
		}
	}
}

func TestValidGeniusToken(t *testing.T) {
	for _, token := range []string{"client-access-token", strings.Repeat("x", 4096)} {
		if !validGeniusToken(token) {
			t.Errorf("validGeniusToken(%q) = false", token)
		}
	}
	for _, token := range []string{"", "token\nheader", strings.Repeat("x", 4097), string([]byte{0xff})} {
		if validGeniusToken(token) {
			t.Errorf("validGeniusToken(%q) = true", token)
		}
	}
}

func TestLyricsHTTPClientRejectsRedirects(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "https://redirect.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := lyricsHTTPClient.CheckRedirect(request, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect() error = %v, want http.ErrUseLastResponse", err)
	}
}

func TestValidGeniusSongURL(t *testing.T) {
	for _, raw := range []string{
		"https://genius.com/Radiohead-paranoid-android-lyrics",
		"https://www.genius.com/songs/example",
	} {
		if !validGeniusSongURL(raw) {
			t.Errorf("validGeniusSongURL(%q) = false", raw)
		}
	}
	for _, raw := range []string{
		"http://genius.com/song",
		"https://evil.example/genius.com/song",
		"https://genius.com.evil.example/song",
		"https://user:pass@genius.com/song",
		"https://genius.com",
		"https://genius.com/",
		"https://genius.com:8443/song",
		"https://genius.com/song?tracking=1",
		"https://genius.com/song#lyrics",
		"https://genius.com/song\u202e",
		"https://genius.com/song\ufe0f",
	} {
		if validGeniusSongURL(raw) {
			t.Errorf("validGeniusSongURL(%q) = true", raw)
		}
	}
}

func TestLookupGeniusSongUsesTokenAndSelectsSong(t *testing.T) {
	old := lyricsHTTPClient
	t.Cleanup(func() { lyricsHTTPClient = old })
	lyricsHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/search" || r.URL.Query().Get("q") != `artist - "song"` {
			t.Fatalf("unexpected Genius request: %s", r.URL)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q", got)
		}
		return newPluginResponse(http.StatusOK, `{"response":{"hits":[
			{"type":"artist","result":{"title":"Artist","url":"https://genius.com/artists/artist"}},
			{"type":"song","result":{"id":123,"title":"Song","artist_names":"Artist","url":"https://genius.com/Artist-song-lyrics"}}
		]}}`), nil
	})}

	song, err := lookupGeniusSong(t.Context(), `artist - "song"`, "test-token")
	if err != nil {
		t.Fatalf("lookupGeniusSong() error = %v", err)
	}
	if song.Title != "Song" || song.URL != "https://genius.com/Artist-song-lyrics" {
		t.Fatalf("song = %#v", song)
	}
}

func TestLookupGeniusSongAcceptsScreenshotQueries(t *testing.T) {
	queries := []string{
		"electric wizard L.S.D.",
		"electric wizard lsd",
		"electric wizard",
		"electric wizard Dopethrone",
		"Snoop Dogg - Smoke Weed Everyday",
		"Smoke Weed Everyday",
	}
	old := lyricsHTTPClient
	t.Cleanup(func() { lyricsHTTPClient = old })
	lyricsHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		query := r.URL.Query().Get("q")
		found := false
		for _, candidate := range queries {
			if query == candidate {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("unexpected screenshot query: %q", query)
		}
		return newPluginResponse(http.StatusOK, `{"response":{"hits":[{"type":"song","result":{"id":321,"title":"Matched song","artist_names":"Matched artist","url":"https://genius.com/Matched-artist-matched-song-lyrics"}}]}}`), nil
	})}

	for _, query := range queries {
		song, err := lookupGeniusSong(t.Context(), query, "test-token")
		if err != nil {
			t.Errorf("lookupGeniusSong(%q) error = %v", query, err)
			continue
		}
		if song.ID != 321 || song.URL == "" {
			t.Errorf("lookupGeniusSong(%q) = %#v", query, song)
		}
	}
}

func TestLookupGeniusSongRejectsInvalidResults(t *testing.T) {
	old := lyricsHTTPClient
	t.Cleanup(func() { lyricsHTTPClient = old })
	lyricsHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
		return newPluginResponse(http.StatusOK, `{"response":{"hits":[
			{"type":"artist","result":{"title":"Artist","url":"https://genius.com/artists/artist"}},
			{"type":"song","result":{"title":"Bad host","url":"https://evil.example/song"}}
		]}}`), nil
	})}
	if _, err := lookupGeniusSong(t.Context(), "song", "token"); !errors.Is(err, errGeniusNotFound) {
		t.Fatalf("lookupGeniusSong() error = %v, want not found", err)
	}
}

func TestLookupGeniusSongHandlesUpstreamFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{}`, want: errGeniusUnauthorized},
		{name: "forbidden", status: http.StatusForbidden, body: `{}`, want: errGeniusUnauthorized},
		{name: "not found", status: http.StatusNotFound, body: `{}`, want: errGeniusNotFound},
		{name: "no hits", status: http.StatusOK, body: `{"response":{"hits":[]}}`, want: errGeniusNotFound},
		{name: "malformed JSON", status: http.StatusOK, body: `{"response":`, want: nil},
		{name: "server error", status: http.StatusInternalServerError, body: `{}`, want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			old := lyricsHTTPClient
			t.Cleanup(func() { lyricsHTTPClient = old })
			lyricsHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
				return newPluginResponse(test.status, test.body), nil
			})}
			_, err := lookupGeniusSong(t.Context(), "song", "token")
			if test.want != nil {
				if !errors.Is(err, test.want) {
					t.Fatalf("lookupGeniusSong() error = %v, want %v", err, test.want)
				}
			} else if err == nil {
				t.Fatal("lookupGeniusSong() error = nil")
			}
		})
	}
}

func TestFormatLyricsResultCleansMetadata(t *testing.T) {
	got, ok := formatLyricsResult(geniusSong{
		ID:          123,
		Title:       "Song\ufe0f\u200d\u202e",
		ArtistNames: "Artist\x03 Name",
		URL:         "https://genius.com/Artist-song-lyrics",
	}, "#music", 320)
	if !ok {
		t.Fatal("formatLyricsResult() rejected a valid song")
	}
	if got != "[lyrics] Artist Name - Song | https://genius.com/Artist-song-lyrics" {
		t.Fatalf("formatLyricsResult() = %q", got)
	}
	if strings.ContainsAny(got, "\r\n") || strings.ContainsAny(got, "\ufe0f\u200d\u202e") {
		t.Fatalf("formatted result contains unsafe text: %q", got)
	}
}

func TestFormatLyricsResultPreservesExactlyOneCompleteLink(t *testing.T) {
	canonical := "https://genius.com/Artist-" + strings.Repeat("very-long-title-", 20) + "lyrics"
	got, ok := formatLyricsResult(geniusSong{
		ID:          456,
		Title:       strings.Repeat("界", 200),
		ArtistNames: "Artist https://untrusted.example",
		URL:         canonical,
	}, "#music", 120)
	if !ok {
		t.Fatal("formatLyricsResult() rejected a song with a usable short link")
	}
	if got != "[lyrics] 界界界界界界界界界界界界界界界界界界界界界界界界界… | https://genius.com/songs/456" {
		t.Fatalf("formatLyricsResult() = %q", got)
	}
	if strings.Count(got, "https://") != 1 || !strings.HasSuffix(got, "https://genius.com/songs/456") {
		t.Fatalf("reply does not preserve exactly one complete Genius link: %q", got)
	}
	if len([]byte(got)) > 120 {
		t.Fatalf("reply is %d bytes, want at most 120: %q", len([]byte(got)), got)
	}
}

func TestBoundLyricsReplyUsesUTF8WireByteLimit(t *testing.T) {
	target := "#international-music"
	reply := boundLyricsReply(target, "[lyrics] "+strings.Repeat("界", 300)+"\r\nsecond line", 500)
	wire := "PRIVMSG " + target + " :" + reply + "\r\n"
	if len([]byte(wire)) > lyricsIRCMaxLineBytes {
		t.Fatalf("wire line is %d bytes, want at most %d", len([]byte(wire)), lyricsIRCMaxLineBytes)
	}
	if !utf8.ValidString(reply) || strings.ContainsAny(reply, "\r\n") {
		t.Fatalf("reply is not a valid single UTF-8 line: %q", reply)
	}
}

func TestLyricsHandleReturnsOneBoundedLine(t *testing.T) {
	old := lyricsHTTPClient
	t.Cleanup(func() { lyricsHTTPClient = old })
	t.Setenv("BOT_GENIUS_ACCESS_TOKEN", "test-token")
	lyricsHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
		return newPluginResponse(http.StatusOK, `{"response":{"hits":[{"type":"song","result":{"id":123,"title":"Song\ufe0f","artist_names":"Artist\u200d","url":"https://genius.com/Artist-song-lyrics"}}]}}`), nil
	})}

	sent := make(chan bot.Outgoing, 2)
	b := &bot.Bot{
		Config: bot.Config{NetworkName: "test", CommandPrefix: "!"},
		Queue:  bot.NewQueue(1000, 20, func(message bot.Outgoing) { sent <- message }),
	}
	plugin := &Lyrics{}
	if err := plugin.Init(bot.PluginConfig{"max_length": 500, "cooldown_seconds": 1}, nil); err != nil {
		t.Fatal(err)
	}
	if !plugin.Handle(b, bot.Message{Nick: "Alice", Target: "#music", IsChannel: true, Text: "!lyric Artist - Song"}) {
		t.Fatal("lyrics command was not consumed")
	}
	b.Queue.Drain(context.Background())
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want exactly one", len(sent))
	}
	outgoing := <-sent
	if !strings.Contains(outgoing.Text, "https://genius.com/Artist-song-lyrics") {
		t.Fatalf("reply = %q", outgoing.Text)
	}
	wire := "PRIVMSG " + outgoing.Target + " :" + outgoing.Text + "\r\n"
	if len([]byte(wire)) > lyricsIRCMaxLineBytes || !utf8.ValidString(outgoing.Text) || strings.ContainsAny(outgoing.Text, "\r\n") {
		t.Fatalf("reply is not a bounded UTF-8 wire line: %q", outgoing.Text)
	}
}

func TestLyricsHandleReportsMissingConfiguration(t *testing.T) {
	t.Setenv("BOT_GENIUS_ACCESS_TOKEN", "")
	sent := make(chan bot.Outgoing, 1)
	b := &bot.Bot{
		Config: bot.Config{NetworkName: "test", CommandPrefix: "!"},
		Queue:  bot.NewQueue(1000, 20, func(message bot.Outgoing) { sent <- message }),
	}
	plugin := &Lyrics{}
	if err := plugin.Init(bot.PluginConfig{"cooldown_seconds": 1}, nil); err != nil {
		t.Fatal(err)
	}
	plugin.Handle(b, bot.Message{Nick: "Alice", Target: "#music", IsChannel: true, Text: "!lyrics Song"})
	b.Queue.Drain(context.Background())
	if got := (<-sent).Text; got != "lyrics search is not configured (set BOT_GENIUS_ACCESS_TOKEN)" {
		t.Fatalf("reply = %q", got)
	}
}

func TestLyricsSearchURLQueryEscapesInput(t *testing.T) {
	endpoint, err := url.Parse(geniusSearchEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	values := endpoint.Query()
	values.Set("q", `artist - "song"`)
	endpoint.RawQuery = values.Encode()
	if endpoint.Query().Get("q") != `artist - "song"` {
		t.Fatalf("query round trip failed: %s", endpoint)
	}
}
