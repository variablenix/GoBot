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
	} {
		if validGeniusSongURL(raw) {
			t.Errorf("validGeniusSongURL(%q) = true", raw)
		}
	}
}

func TestLookupGeniusSongUsesTokenAndSelectsSong(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/search" || r.URL.Query().Get("q") != `artist - "song"` {
			t.Fatalf("unexpected Genius request: %s", r.URL)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q", got)
		}
		return newPluginResponse(http.StatusOK, `{"response":{"hits":[
			{"result":{"type":"artist","title":"Artist","url":"https://genius.com/artists/artist"}},
			{"result":{"type":"song","title":"Song","artist_names":"Artist","url":"https://genius.com/Artist-song-lyrics"}}
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

func TestLookupGeniusSongRejectsInvalidResults(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
		return newPluginResponse(http.StatusOK, `{"response":{"hits":[
			{"result":{"type":"artist","title":"Artist","url":"https://genius.com/artists/artist"}},
			{"result":{"type":"song","title":"Bad host","url":"https://evil.example/song"}}
		]}}`), nil
	})}
	if _, err := lookupGeniusSong(t.Context(), "song", "token"); !errors.Is(err, errGeniusNotFound) {
		t.Fatalf("lookupGeniusSong() error = %v, want not found", err)
	}
}

func TestFormatLyricsResultCleansMetadata(t *testing.T) {
	got := formatLyricsResult(geniusSong{
		Title:       "Song\ufe0f\u200d\u202e",
		ArtistNames: "Artist\x03 Name",
		URL:         "https://genius.com/Artist-song-lyrics",
	})
	if got != "[lyrics] Artist Name - Song | https://genius.com/Artist-song-lyrics" {
		t.Fatalf("formatLyricsResult() = %q", got)
	}
	if strings.ContainsAny(got, "\r\n") || strings.ContainsAny(got, "\ufe0f\u200d\u202e") {
		t.Fatalf("formatted result contains unsafe text: %q", got)
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
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	t.Setenv("BOT_GENIUS_ACCESS_TOKEN", "test-token")
	apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
		return newPluginResponse(http.StatusOK, `{"response":{"hits":[{"result":{"type":"song","title":"Song\ufe0f","artist_names":"Artist\u200d","url":"https://genius.com/Artist-song-lyrics"}}]}}`), nil
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
