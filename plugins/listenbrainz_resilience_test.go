package plugins

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/variablenix/GoBot/bot"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type stalledListenBrainzBody struct{ ctx context.Context }

func (b stalledListenBrainzBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (b stalledListenBrainzBody) Close() error { return nil }

type truncatedListenBrainzBody struct{}

func (truncatedListenBrainzBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (truncatedListenBrainzBody) Close() error             { return nil }

func TestListenBrainzStalledCurrentFallsBackWithinBudget(t *testing.T) {
	for _, stage := range []string{"headers", "body"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var starts []time.Time
				p := newTestListenBrainz(t, nil, func(r *http.Request) (*http.Response, error) {
					starts = append(starts, time.Now())
					if strings.HasSuffix(r.URL.Path, "/playing-now") {
						if stage == "headers" {
							<-r.Context().Done()
							return nil, r.Context().Err()
						}
						res := newPluginResponse(200, "")
						res.Body = stalledListenBrainzBody{r.Context()}
						return res, nil
					}
					if !strings.HasSuffix(r.URL.Path, "/listens") || r.URL.RawQuery != "count=1" {
						t.Fatalf("unexpected fallback endpoint: %s", r.URL)
					}
					return newPluginResponse(200, lbCurrentFixture), nil
				})
				start := time.Now()
				reply := listenBrainzReply(t, p, "primary", "listener", "Alice", "!np music_fan")
				if !strings.Contains(reply, `last played "Example Song"`) || !strings.Contains(reply, "now playing unavailable") ||
					len(starts) != 2 || time.Since(start) != 5*time.Second || starts[1].Sub(starts[0]) < time.Second {
					t.Fatalf("stalled current blocked recent-track response: %q, calls=%v, elapsed=%s", reply, starts, time.Since(start))
				}
			})
		})
	}
}

func TestListenBrainzTransientHistoryRetry(t *testing.T) {
	for _, failure := range []string{"http503", "transport", "truncated_body", "timeout"} {
		t.Run(failure, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var starts []time.Time
				p := newTestListenBrainz(t, nil, func(r *http.Request) (*http.Response, error) {
					starts = append(starts, time.Now())
					if len(starts) == 1 {
						return newPluginResponse(200, lbEmptyFixture), nil
					}
					if len(starts) == 2 {
						switch failure {
						case "http503":
							return newPluginResponse(503, ""), nil
						case "transport":
							return nil, errors.New("synthetic connection reset")
						case "truncated_body":
							res := newPluginResponse(200, "")
							res.Body = truncatedListenBrainzBody{}
							return res, nil
						case "timeout":
							<-r.Context().Done()
							return nil, r.Context().Err()
						}
					}
					return newPluginResponse(200, lbCurrentFixture), nil
				})
				start := time.Now()
				reply := listenBrainzReply(t, p, "primary", "listener", "Alice", "!lb music_fan")
				if len(starts) != 3 || !strings.Contains(reply, "last played") || strings.Contains(reply, "now playing unavailable") || time.Since(start) > p.timeout {
					t.Fatalf("transient history error was not recovered: %q, calls=%v", reply, starts)
				}
				for i := 1; i < len(starts); i++ {
					if starts[i].Sub(starts[i-1]) < time.Second {
						t.Fatal("retry bypassed shared provider rate limit")
					}
				}
			})
		})
	}
}

func TestListenBrainzFallbackHonorsRetryAfterAndPermanentFailures(t *testing.T) {
	for _, status := range []int{404, 429, 302, 200, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				p := newTestListenBrainz(t, nil, func(*http.Request) (*http.Response, error) {
					calls++
					if calls == 1 {
						return newPluginResponse(200, lbEmptyFixture), nil
					}
					res := newPluginResponse(status, `{}`)
					res.Header.Set("Retry-After", "30")
					return res, nil
				})
				reply := listenBrainzReply(t, p, "primary", "listener", "Alice", "!np music_fan")
				if calls != 2 || strings.Contains(reply, "no recent listens") {
					t.Fatalf("retry ignored permanent failure or provider cooldown: status=%d, calls=%d, reply=%q", status, calls, reply)
				}
			})
		})
	}
}

func TestListenBrainzCompleteOutageIsBoundedAndNotEmptyHistory(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		p := newTestListenBrainz(t, nil, func(r *http.Request) (*http.Response, error) {
			calls++
			<-r.Context().Done()
			return nil, r.Context().Err()
		})
		start := time.Now()
		reply := listenBrainzReply(t, p, "primary", "listener", "Alice", "!np music_fan")
		if calls != 3 || time.Since(start) != p.timeout || !strings.Contains(reply, "API timed out") ||
			!strings.Contains(reply, "https://listenbrainz.org/user/music_fan/") || strings.Contains(reply, "no recent listens") {
			t.Fatalf("outage must not become empty history or an unbounded retry: %q, calls=%d, elapsed=%s", reply, calls, time.Since(start))
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, _, _, err := p.client.lookup(ctx, "music_fan")
		if !errors.Is(err, context.Canceled) || calls != 3 {
			t.Fatal("cancelled lookup made an additional provider request")
		}
	})
}

func TestListenBrainzPartialOutageDoesNotClaimCurrentOrEmptyHistory(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newTestListenBrainz(t, nil, func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, "/playing-now") {
				return newPluginResponse(503, ""), nil
			}
			return newPluginResponse(200, lbEmptyFixture), nil
		})
		reply := listenBrainzReply(t, p, "primary", "listener", "Alice", "!np music_fan")
		if !strings.Contains(reply, "has no recent listens") || !strings.Contains(reply, "now playing unavailable") || strings.Contains(reply, "is listening to") {
			t.Fatalf("partial outage was misrepresented: %s", reply)
		}
	})
}

func TestListenBrainzFailureLogsDoNotExposeUserOrTransportDetails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newTestListenBrainz(t, nil, func(*http.Request) (*http.Response, error) {
			return nil, errors.New("private upstream body or proxy credentials")
		})
		core, logs := observer.New(zap.WarnLevel)
		b := &bot.Bot{Config: bot.Config{CommandPrefix: "!", NetworkName: "primary"}, Log: zap.New(core), Queue: bot.NewQueue(1000, 20, func(bot.Outgoing) {})}
		p.Handle(b, bot.Message{Nick: "Alice", Target: "#music", IsChannel: true, Text: "!lb music_fan"})
		b.Queue.Drain(context.Background())
		entries := logs.All()
		if len(entries) != 1 || len(entries[0].ContextMap()) != 1 || entries[0].ContextMap()["reason"] != "network_error" {
			t.Fatal("failure log must contain only a classified reason")
		}
	})
	if reason := listenBrainzFailureReason(&url.Error{Op: "Get", URL: "https://private.example/music_fan", Err: context.DeadlineExceeded}); reason != "timeout" {
		t.Fatalf("unsafe or incorrect transport classification: %s", reason)
	}
	if reason := listenBrainzFailureReason(listenBrainzHTTPError(503)); reason != "http_503" {
		t.Fatalf("HTTP status classification lost: %s", reason)
	}
}
