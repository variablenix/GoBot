package plugins

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

// Exercise every registered plugin's initialization and command boundary.
// Provider-specific and stateful behavior remains covered by its own tests.
func TestAllPluginsInitializeAndKeepUnknownCommandAvailable(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	b := &bot.Bot{Config: bot.Config{CommandPrefix: "!", NetworkName: "test"}, Queue: bot.NewQueue(1, 1, func(bot.Outgoing) {})}
	defer b.Queue.Drain(context.Background())
	names, commands := map[string]bool{}, map[string]string{}
	for _, p := range All() {
		t.Run(p.Name(), func(t *testing.T) {
			if names[p.Name()] || p.Name() == "" || p.Help() == "" {
				t.Fatal("missing/duplicate plugin identity or help")
			}
			names[p.Name()] = true
			for _, command := range p.Commands() {
				if owner := commands[command]; owner != "" && !(command == "vuln" && owner == "cve" && p.Name() == "audit") {
					t.Fatalf("command %q conflicts with %s", command, owner)
				}
				commands[command] = p.Name()
			}
			var config bot.PluginConfig
			if p.Name() == "wordle" {
				config = bot.PluginConfig{"words_file": "../data/wordle/words.txt"}
			}
			if err := p.Init(config, db); err != nil {
				t.Fatal(err)
			}
			if p.Handle(b, bot.Message{Command: "PRIVMSG", Nick: "tester", Target: "#test", IsChannel: true, Text: "!__unknown_command__", Timestamp: time.Now()}) {
				t.Fatal("plugin consumed another command")
			}
			if stopper, ok := p.(bot.Stopper); ok {
				stopper.Stop(b)
			}
		})
	}
	t.Logf("audited %d registered plugins and %d command aliases", len(names), len(commands))
}

func TestAuthenticatedAPIRedirectsKeepCredentialsOnOrigin(t *testing.T) {
	old := apiHTTPClient
	t.Cleanup(func() { apiHTTPClient = old })
	for _, destination := range []string{"https://other.example/private", "https://sub.api.example/private", "http://api.example/private", "https://api.example/next"} {
		requests := 0
		apiHTTPClient = &http.Client{Transport: newPluginRoundTripper(func(r *http.Request) (*http.Response, error) {
			requests++
			if requests == 1 {
				res := newPluginResponse(302, "")
				res.Header.Set("Location", destination)
				return res, nil
			}
			return newPluginResponse(200, `{}`), nil
		})}
		req, _ := http.NewRequestWithContext(t.Context(), "GET", "https://api.example/start", nil)
		req.Header.Set("X-Api-Key", "synthetic-key")
		res, err := authenticatedAPIRequest(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		want := 1
		if strings.HasPrefix(destination, "https://api.example/") {
			want = 2
		}
		if requests != want {
			t.Fatalf("redirect to %s issued %d requests, want %d", destination, requests, want)
		}
	}
}
