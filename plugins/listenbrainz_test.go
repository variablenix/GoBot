package plugins

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
	"unicode/utf8"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const lbCurrentFixture = `{"payload":{"playing_now":true,"listens":[{"track_metadata":{"track_name":"Example Song","artist_name":"Example Artist","additional_info":{"origin_url":"http://127.0.0.1/private"}}}]}}`
const lbEmptyFixture = `{"payload":{"listens":[]}}`

func newTestListenBrainz(t *testing.T, db *storage.DB, handle func(*http.Request) (*http.Response, error)) *ListenBrainz {
	t.Helper()
	p := &ListenBrainz{}
	if err := p.Init(nil, db); err != nil {
		t.Fatal(err)
	}
	p.client = newListenBrainzClient()
	p.client.http = &http.Client{Transport: newPluginRoundTripper(handle)}
	return p
}

func listenBrainzReply(t *testing.T, p *ListenBrainz, network, account, nick, command string) string {
	t.Helper()
	sent := make(chan bot.Outgoing, 5)
	b := &bot.Bot{Config: bot.Config{CommandPrefix: "!", NetworkName: network}, Queue: bot.NewQueue(1000, 20, func(m bot.Outgoing) { sent <- m })}
	handled := p.Handle(b, bot.Message{Nick: nick, Account: account, Target: "#music", IsChannel: true, Text: command})
	b.Queue.Drain(context.Background())
	if !handled || len(sent) != 1 {
		t.Fatalf("handled=%v, replies=%d", handled, len(sent))
	}
	return (<-sent).Text
}

func TestListenBrainzCommandsAndRequestSafety(t *testing.T) {
	for _, command := range (&ListenBrainz{}).Commands() {
		t.Run(command, func(t *testing.T) {
			calls := 0
			p := newTestListenBrainz(t, nil, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.URL.Scheme != "https" || r.URL.Host != "api.listenbrainz.org" ||
					r.URL.EscapedPath() != "/1/user/music%20fan/playing-now" || r.URL.RawQuery != "" {
					t.Fatalf("unexpected API request: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "" || !strings.Contains(r.UserAgent(), "https://github.com/variablenix/GoBot") {
					t.Fatal("public lookup must identify GoBot without sending credentials")
				}
				return newPluginResponse(200, lbCurrentFixture), nil
			})
			reply := listenBrainzReply(t, p, "primary", "listener", "Alice", "!"+command+" music fan")
			if calls != 1 || !strings.Contains(reply, `is listening to "Example Song" by Example Artist`) ||
				!strings.Contains(reply, "https://listenbrainz.org/user/music%20fan/") || strings.Contains(reply, "127.0.0.1") {
				t.Fatalf("wrong current-track reply: %s (%d calls)", reply, calls)
			}
		})
	}
}

func TestListenBrainzRecentAndEmptyHistory(t *testing.T) {
	for _, test := range []struct{ history, want string }{
		{lbCurrentFixture, "last played"},
		{lbEmptyFixture, "has no recent listens"},
	} {
		t.Run(test.want, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var calls []time.Time
				p := newTestListenBrainz(t, nil, func(r *http.Request) (*http.Response, error) {
					calls = append(calls, time.Now())
					if len(calls) == 1 && r.URL.Path == "/1/user/music_fan/playing-now" {
						return newPluginResponse(200, lbEmptyFixture), nil
					}
					if len(calls) != 2 || r.URL.Path != "/1/user/music_fan/listens" || r.URL.RawQuery != "count=1" {
						t.Fatalf("unexpected history request: %s", r.URL)
					}
					return newPluginResponse(200, test.history), nil
				})
				reply := listenBrainzReply(t, p, "primary", "listener", "Alice", "!np music_fan")
				if !strings.Contains(reply, test.want) || len(calls) != 2 || calls[1].Sub(calls[0]) < time.Second {
					t.Fatalf("fallback or request spacing failed: %s, calls=%v", reply, calls)
				}
			})
		})
	}
}

func TestListenBrainzProviderFailures(t *testing.T) {
	for _, test := range []struct {
		name, body, want string
		status, calls    int
	}{
		{"unknown user", `{}`, "user not found", 404, 1},
		{"throttled", `{}`, "rate limit", 429, 1},
		{"outage", `{}`, "temporarily unavailable", 503, 3},
		{"bad JSON", `{`, "temporarily unavailable", 200, 2},
		{"missing payload", `{}`, "temporarily unavailable", 200, 2},
		{"null listens", `{"payload":{"listens":null}}`, "temporarily unavailable", 200, 2},
		{"missing metadata", `{"payload":{"listens":[{}]}}`, "temporarily unavailable", 200, 2},
		{"hidden title", `{"payload":{"listens":[{"track_metadata":{"track_name":"\u202e","artist_name":"artist"}}]}}`, "temporarily unavailable", 200, 2},
		{"multiple tracks", `{"payload":{"listens":[{},{}]}}`, "temporarily unavailable", 200, 2},
		{"trailing data", lbCurrentFixture + `{}`, "temporarily unavailable", 200, 2},
		{"oversized", lbCurrentFixture + strings.Repeat(" ", 512<<10), "temporarily unavailable", 200, 2},
		{"redirect", ``, "temporarily unavailable", 302, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				p := newTestListenBrainz(t, nil, func(*http.Request) (*http.Response, error) {
					calls++
					res := newPluginResponse(test.status, test.body)
					res.Header.Set("Location", "http://127.0.0.1/private")
					return res, nil
				})
				reply := listenBrainzReply(t, p, "primary", "listener", "Alice", "!lb music_fan")
				if calls != test.calls || !strings.Contains(reply, test.want) {
					t.Fatalf("wrong error handling: %s (%d calls)", reply, calls)
				}
			})
		})
	}
	synctest.Test(t, func(t *testing.T) {
		p := newTestListenBrainz(t, nil, func(*http.Request) (*http.Response, error) { return nil, errors.New("synthetic transport failure") })
		if reply := listenBrainzReply(t, p, "primary", "listener", "Alice", "!lb music_fan"); !strings.Contains(reply, "temporarily unavailable") || strings.Contains(reply, "synthetic") {
			t.Fatalf("transport error was leaked or mishandled: %s", reply)
		}
	})
}

func TestListenBrainzSavedDefaultsAreScopedAndLegacyIsPreserved(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db, err := storage.Open(filepath.Join(t.TempDir(), "music.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if err := db.Set("lastfm", "alice", "old_service_user"); err != nil {
			t.Fatal(err)
		}
		p := newTestListenBrainz(t, db, func(r *http.Request) (*http.Response, error) {
			if strings.Contains(r.URL.Path, "/unknown/") {
				return newPluginResponse(404, `{}`), nil
			}
			return newPluginResponse(200, lbCurrentFixture), nil
		})
		for _, call := range []struct{ network, account, nick, command, want string }{
			{"primary", "listener", "Alice", "!np", "usage:"},
			{"primary", "listener", "Alice", "!np music_fan", "is listening to"},
			{"primary", "listener", "Renamed", "!np", "music_fan is listening to"},
			{"secondary", "listener", "Alice", "!np", "usage:"},
			{"primary", "other", "Alice", "!np", "usage:"},
			{"primary", "*", "Alice", "!np", "usage:"},
			{"primary", "listener", "Alice", "!np unknown", "user not found"},
			{"primary", "listener", "Alice", "!np", "music_fan is listening to"},
			{"primary", "*", "Alice", "!np guest_profile", "is listening to"},
			{"primary", "*", "ALICE", "!np", "guest_profile is listening to"},
			{"secondary", "*", "Alice", "!np", "usage:"},
		} {
			if reply := listenBrainzReply(t, p, call.network, call.account, call.nick, call.command); !strings.Contains(reply, call.want) {
				t.Fatalf("%+v: %s", call, reply)
			}
		}
		raw, err := db.Get("lastfm", "alice")
		if err != nil || string(raw) != `"old_service_user"` {
			t.Fatal("legacy Last.fm preference was modified")
		}
		// A failed persistence write must not hide the fetched track or pretend
		// the preference was saved. Reads must not silently mask storage failures.
		db.Close()
		if reply := listenBrainzReply(t, p, "primary", "listener", "Alice", "!np music_fan"); !strings.Contains(reply, "default not saved") || !strings.Contains(reply, "Example Song") {
			t.Fatalf("storage write failure not surfaced: %s", reply)
		}
		if reply := listenBrainzReply(t, p, "primary", "listener", "Alice", "!np"); !strings.Contains(reply, "cannot read saved") {
			t.Fatalf("storage read failure not surfaced: %s", reply)
		}
	})
}

func TestListenBrainzInputAndOutputSafety(t *testing.T) {
	for _, user := range []string{"music_fan", "two words", "音楽", "O'Connor"} {
		if !validListenBrainzUser(user) {
			t.Errorf("valid username rejected: %q", user)
		}
	}
	for _, user := range []string{"", ".", "..", "a/b", "a\\b", "%2f", "a?b", "a#b", "bad\nname", "bad\x03name", "a\u202eb", "a\ufe0f", "bad\tname", " name ", string([]byte{0xff}), strings.Repeat("a", 65)} {
		if validListenBrainzUser(user) {
			t.Errorf("unsafe username accepted: %q", user)
		}
	}
	var listens []listenBrainzListen
	listens = append(listens, listenBrainzListen{})
	listens[0].Metadata.Track = strings.Repeat("曲\ufe0f\u200d\u202e\x03", 500)
	listens[0].Metadata.Artist = "artist\r\nINJECT"
	for _, username := range []string{"music_fan", strings.Repeat("音", 64)} {
		for _, target := range []string{"#music", "#" + strings.Repeat("x", 180)} {
			reply := formatListenBrainz(target, username, listens, true, " (default not saved)")
			if !utf8.ValidString(reply) || len("PRIVMSG "+target+" :"+reply+"\r\n") > 512 || strings.ContainsAny(reply, "\r\n\x00\x03\ufe0f\u200d\u202e") {
				t.Fatalf("unsafe or oversized response: %q", reply)
			}
			if username == "music_fan" && !strings.Contains(reply, "https://listenbrainz.org/user/music_fan/") {
				t.Fatal("canonical profile link lost")
			}
		}
	}
}

func TestListenBrainzSharedLimiterAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := newListenBrainzClient()
		var starts []time.Time
		client.http = &http.Client{Transport: newPluginRoundTripper(func(*http.Request) (*http.Response, error) {
			starts = append(starts, time.Now())
			return newPluginResponse(200, lbCurrentFixture), nil
		})}
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				if _, err := client.get(t.Context(), "music_fan", "playing-now"); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		for i := 1; i < len(starts); i++ {
			if starts[i].Sub(starts[i-1]) < time.Second {
				t.Fatal("concurrent requests violated provider rate limit")
			}
		}
		client.next = time.Now().Add(time.Hour)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if _, err := client.get(ctx, "music_fan", "playing-now"); !errors.Is(err, context.DeadlineExceeded) || len(starts) != 4 {
			t.Fatalf("rate wait did not respect deadline: %v (%d requests)", err, len(starts))
		}
		client.gate <- struct{}{}
		ctx2, cancel2 := context.WithCancel(t.Context())
		cancel2()
		if _, err := client.get(ctx2, "music_fan", "playing-now"); !errors.Is(err, context.Canceled) {
			t.Fatalf("queued request ignored cancellation: %v", err)
		}
		<-client.gate
	})
	p1, p2 := &ListenBrainz{}, &ListenBrainz{}
	p1.Init(bot.PluginConfig{"timeout_seconds": -1}, nil)
	p2.Init(bot.PluginConfig{"timeout_seconds": 900}, nil)
	if p1.client != p2.client || p1.timeout != 15*time.Second || p2.timeout != 15*time.Second {
		t.Fatal("networks must share one limiter and clamp timeouts")
	}
	p1.client = newListenBrainzClient()
	for range cap(p1.client.slots) {
		p1.client.slots <- struct{}{}
	}
	if reply := listenBrainzReply(t, p1, "primary", "listener", "Alice", "!np music_fan"); !strings.Contains(reply, "busy") {
		t.Fatalf("unbounded work during overload: %s", reply)
	}
}

func TestListenBrainzRateHeaders(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		headers map[string]string
		status  int
		seconds int
	}{
		{map[string]string{"X-RateLimit-Remaining": "1", "X-RateLimit-Reset-In": "30"}, 200, 1},
		{map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset-In": "30"}, 200, 30},
		{map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset-In": "30", "X-RateLimit-Reset": "999999999999"}, 200, 30},
		{map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": fmt.Sprint(now.Unix() + 40)}, 200, 40},
		{map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset-In": "-1", "X-RateLimit-Reset": fmt.Sprint(now.Unix() + 40)}, 200, 40},
		{map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset-In": "999999999999"}, 200, 365 * 24 * 60 * 60},
		{map[string]string{"Retry-After": "60"}, 429, 60},
		{map[string]string{"Retry-After": "20"}, 503, 20},
		{map[string]string{"Retry-After": now.Add(time.Minute).Format(http.TimeFormat)}, 429, 61},
		{map[string]string{"Retry-After": "-1", "X-RateLimit-Reset-In": "invalid"}, 429, 1},
	} {
		client := newListenBrainzClient()
		client.next = now.Add(time.Second)
		res := newPluginResponse(test.status, "")
		for key, value := range test.headers {
			res.Header.Set(key, value)
		}
		client.observeRate(res, now)
		res.Body.Close()
		if got := client.next.Sub(now); got != time.Duration(test.seconds)*time.Second {
			t.Errorf("%v: cooldown=%s, want %ds", test.headers, got, test.seconds)
		}
	}
}

// Public endpoint smoke test, deliberately opt-in: no API token or IRC traffic.
func TestLiveListenBrainz(t *testing.T) {
	if testing.Short() {
		t.Skip("live smoke test disabled in short mode")
	}
	// The environment value names a public profile, never a credential.
	username := strings.TrimSpace(os.Getenv("GOBOT_LIVE_LISTENBRAINZ_USER"))
	if username == "" {
		t.Skip("set GOBOT_LIVE_LISTENBRAINZ_USER for an opt-in public lookup")
	}
	p := &ListenBrainz{}
	p.Init(nil, nil)
	reply := listenBrainzReply(t, p, "test", "listener", "tester", "!listenbrainz "+username)
	if !strings.Contains(reply, "https://listenbrainz.org/user/") || !successfulListenBrainzReply(reply) {
		t.Fatalf("public lookup did not return a profile: %s", reply)
	}
	t.Logf("public lookup succeeded (recent-listen fallback: %t)", strings.Contains(reply, "now playing unavailable"))
}

func successfulListenBrainzReply(reply string) bool {
	return strings.HasPrefix(reply, "🎵 ") &&
		(strings.Contains(reply, " is listening to ") || strings.Contains(reply, " last played ") || strings.Contains(reply, " has no recent listens"))
}
