package plugins

import (
	"context"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/variablenix/GoBot/bot"
)

// Run from the deployment host explicitly. These call providers through the
// real command handlers but never connect to IRC or send to live channels.
// Keep normal CI independent of API keys, rate limits, and upstream outages.
func TestLivePluginCommands(t *testing.T) {
	if os.Getenv("GOBOT_LIVE_PLUGINS") != "1" {
		t.Skip("opt-in live plugin smoke test")
	}
	cases := []struct {
		plugin               bot.Plugin
		command, want, token string
	}{
		{&Wikipedia{}, "!wiki Linux", "wikipedia.org", ""},
		{&Weather{}, "!weather London", "London", ""},
		{&IMDb{}, "!imdb The Matrix", "imdb.com/title/", ""},
		{&Linux{}, "!linux", "kernel", ""},
		{&Cats{}, "!cat", "Cat fact", ""},
		{&XKCD{}, "!xkcd 1", "xkcd.com/1", ""},
		{&Urban{}, "!urban hello", "urbandictionary.com", ""},
		{&Dadjoke{}, "!dadjoke", "", ""},
		{&GitHub{}, "!github golang/go", "github.com/golang/go", ""},
		{&Pkg{}, "!pkg npm lodash", "lodash", ""},
		{&Docker{}, "!docker alpine", "hub.docker.com", ""},
		{&CVE{}, "!cve CVE-2024-3094", "CVE-2024-3094", ""},
		{&Reddit{}, "!reddit linux", "reddit.com", ""},
		{&Steam{}, "!steam Portal 2", "steampowered.com", ""},
		{&Sports{}, "!sports", "Sports pick:", ""},
		{&News{}, "!news technology", "http", "BOT_NEWS_API_KEY"},
		{&Lyrics{}, "!lyrics electric wizard Dopethrone", "genius.com", "BOT_GENIUS_ACCESS_TOKEN"},
	}
	for _, test := range cases {
		t.Run(test.plugin.Name(), func(t *testing.T) {
			if test.token != "" && strings.TrimSpace(os.Getenv(test.token)) == "" {
				t.Skip("provider credential not configured")
			}
			cfg := bot.PluginConfig{"timeout_seconds": 8}
			if test.token != "" {
				cfg["api_key"] = os.Getenv(test.token)
			}
			if err := test.plugin.Init(cfg, nil); err != nil {
				t.Fatal(err)
			}
			sent := make(chan string, 20)
			b := &bot.Bot{Config: bot.Config{CommandPrefix: "!", NetworkName: "test"}, Queue: bot.NewQueue(1, 1, func(m bot.Outgoing) { sent <- m.Text })}
			defer b.Queue.Drain(context.Background())
			if !test.plugin.Handle(b, bot.Message{Command: "PRIVMSG", Nick: "tester", Target: "#test", IsChannel: true, Text: test.command}) {
				t.Fatal("command not handled")
			}
			b.Queue.Drain(context.Background())
			var replies []string
			for len(sent) > 0 {
				text := <-sent
				if !utf8.ValidString(text) || len("PRIVMSG #test :"+text+"\r\n") > 512 || strings.ContainsAny(text, "\r\n\x00") {
					t.Fatal("invalid IRC reply")
				}
				replies = append(replies, stripPluginIRC(text))
			}
			joined := strings.Join(replies, " ")
			lower := strings.ToLower(joined)
			for _, failure := range []string{"unavailable", "not configured", "not found", "no results", "usage:", "failed"} {
				if strings.Contains(lower, failure) {
					t.Fatalf("provider did not return a result: %s", joined)
				}
			}
			if joined == "" || !strings.Contains(lower, strings.ToLower(test.want)) {
				t.Fatalf("expected result marker %q, got %s", test.want, joined)
			}
		})
	}
}
