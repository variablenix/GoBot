package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
	"go.uber.org/zap"
)

const listenBrainzBucket = "listenbrainz"

var (
	errListenBrainzUser = errors.New("ListenBrainz user not found")
	errListenBrainzRate = errors.New("ListenBrainz rate limited")
	errListenBrainzData = errors.New("invalid ListenBrainz response")
	// One application-wide limiter covers every IRC network and both endpoints.
	sharedListenBrainz = newListenBrainzClient()
)

type ListenBrainz struct {
	db      *storage.DB
	timeout time.Duration
	client  *listenBrainzClient
}

func (p *ListenBrainz) Name() string { return "listenbrainz" }
func (p *ListenBrainz) Commands() []string {
	return []string{"listenbrainz", "lb", "np", "last", "lastfm"}
}
func (p *ListenBrainz) Help() string {
	return "!listenbrainz [username] — current or last-played ListenBrainz track; aliases: !lb, !np, !last, !lastfm (no API key)"
}
func (p *ListenBrainz) Init(c bot.PluginConfig, db *storage.DB) error {
	p.db = db
	seconds := c.Int("timeout_seconds", 15)
	if seconds < 1 || seconds > 30 {
		seconds = 15
	}
	p.timeout = time.Duration(seconds) * time.Second
	p.client = sharedListenBrainz
	return nil
}

func (p *ListenBrainz) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "listenbrainz" && cmd != "lb" && cmd != "np" && cmd != "last" && cmd != "lastfm") {
		return false
	}
	username := strings.TrimSpace(arg)
	key := scopedKey(b.Config.NetworkName, "", pluginIdentity(m))
	if username == "" && p.db != nil {
		raw, err := p.db.Get(listenBrainzBucket, key)
		if err == nil {
			if storage.Decode(raw, &username) != nil {
				b.Send(m.ReplyTarget(), "cannot read saved ListenBrainz username; supply !listenbrainz <username>")
				return true
			}
		} else if !errors.Is(err, storage.ErrNotFound) {
			b.Send(m.ReplyTarget(), "cannot read saved ListenBrainz username; supply !listenbrainz <username>")
			return true
		}
	}
	if !validListenBrainzUser(username) {
		b.Send(m.ReplyTarget(), "usage: !listenbrainz <ListenBrainz username> (sets your default; old Last.fm defaults are not reused)")
		return true
	}
	// Bound queued work as well as the per-request deadline during command floods.
	select {
	case p.client.slots <- struct{}{}:
		defer func() { <-p.client.slots }()
	default:
		b.Send(m.ReplyTarget(), "ListenBrainz is busy; please try again shortly")
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	listens, playing, currentUnavailable, err := p.client.lookup(ctx, username)
	if err != nil {
		reply := "ListenBrainz is temporarily unavailable; please try again later"
		if errors.Is(err, errListenBrainzUser) {
			reply = "ListenBrainz user not found; check the username on listenbrainz.org"
		} else if errors.Is(err, errListenBrainzRate) {
			reply = "ListenBrainz rate limit reached; please try again later"
		} else if errors.Is(err, context.DeadlineExceeded) {
			reply = "ListenBrainz API timed out; listening history could not be checked"
		}
		if b.Log != nil {
			// Do not log usernames, URLs, upstream bodies, or raw transport errors.
			b.Log.Warn("ListenBrainz lookup failed", zap.String("reason", listenBrainzFailureReason(err)))
		}
		if !errors.Is(err, errListenBrainzUser) {
			reply = boundListenBrainzReply(m.ReplyTarget(), username, reply, "")
		}
		b.Send(m.ReplyTarget(), reply)
		return true
	}
	// Persist only after a successful lookup, never for a typo or upstream error.
	suffix := ""
	if currentUnavailable {
		suffix = " (now playing unavailable)"
		if b.Log != nil {
			b.Log.Debug("ListenBrainz used recent-listen fallback")
		}
	}
	if strings.TrimSpace(arg) != "" && p.db != nil {
		if err := p.db.Set(listenBrainzBucket, key, username); err != nil {
			suffix += " (default not saved)"
		}
	}
	b.Send(m.ReplyTarget(), formatListenBrainz(m.ReplyTarget(), username, listens, playing, suffix))
	return true
}

// Allow Unicode and internal spaces, but never path segments, controls, hidden
// formatting, or unbounded input. The provider decides whether the user exists.
func validListenBrainzUser(user string) bool {
	if !utf8.ValidString(user) || user == "" || utf8.RuneCountInString(user) > 64 ||
		user != strings.TrimSpace(user) || user == "." || user == ".." {
		return false
	}
	for _, r := range user {
		if unsafeTerminalRune(r) || strings.ContainsRune("/\\?#%", r) || (unicode.IsSpace(r) && r != ' ') {
			return false
		}
	}
	return true
}

type listenBrainzListen struct {
	Metadata struct {
		Track  string `json:"track_name"`
		Artist string `json:"artist_name"`
	} `json:"track_metadata"`
}

func formatListenBrainz(target, username string, listens []listenBrainzListen, playing bool, suffix string) string {
	// The existing terminal-safe sanitizer also removes variation selectors and
	// bidi formatting from untrusted music metadata. Never render supplied URLs.
	text := cleanTerminalText(username) + " has no recent listens"
	if len(listens) > 0 {
		status := "last played"
		if playing {
			status = "is listening to"
		}
		text = fmt.Sprintf("%s %s %q by %s", cleanTerminalText(username), status,
			cleanTerminalText(listens[0].Metadata.Track), cleanTerminalText(listens[0].Metadata.Artist))
	}
	return boundListenBrainzReply(target, username, "🎵 "+text, suffix)
}

func boundListenBrainzReply(target, username, text, suffix string) string {
	profile := "https://listenbrainz.org/user/" + url.PathEscape(username) + "/"
	// Reserve the canonical profile link and warning; keep the complete reply
	// within one IRC line even for multibyte names and long channel targets.
	limit := min(450, 512-len("PRIVMSG "+target+" :\r\n"))
	tail := " — " + profile + suffix
	if len(tail) > limit-80 {
		// Some valid Unicode usernames have very large percent-encoded URLs.
		// Omit that link rather than emit a broken/truncated URL or extra lines.
		tail = suffix
	}
	return truncateUTF8Bytes(truncateUTF8Bytes(text, limit-len(tail))+tail, limit)
}

type listenBrainzClient struct {
	http  *http.Client
	gate  chan struct{}
	slots chan struct{}
	next  time.Time // accessed only while holding gate
}

func newListenBrainzClient() *listenBrainzClient {
	return &listenBrainzClient{gate: make(chan struct{}, 1), slots: make(chan struct{}, 4)}
}

// A slow optional current-track endpoint must not consume the whole command
// budget and prevent the independent recent-listens endpoint from answering.
func (c *listenBrainzClient) lookup(ctx context.Context, username string) ([]listenBrainzListen, bool, bool, error) {
	currentBudget := 5 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		currentBudget = min(currentBudget, time.Until(deadline)/3)
	}
	currentCtx, cancelCurrent := context.WithTimeout(ctx, currentBudget)
	listens, currentErr := c.get(currentCtx, username, "playing-now")
	cancelCurrent()
	if currentErr == nil && len(listens) > 0 {
		return listens, true, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, false, err
	}
	if errors.Is(currentErr, errListenBrainzUser) || errors.Is(currentErr, errListenBrainzRate) {
		return nil, false, false, currentErr
	}
	// Reserve up to three seconds for one transient-error retry, but allow a
	// normal recent-history response considerably longer than the optional call.
	historyCtx := ctx
	cancelHistory := func() {}
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 6*time.Second {
			historyCtx, cancelHistory = context.WithTimeout(ctx, remaining-3*time.Second)
		}
	}
	listens, err := c.get(historyCtx, username, "listens?count=1")
	cancelHistory()
	if err != nil && ctx.Err() == nil && retryableListenBrainzError(err) {
		// get still enforces the global one-request-per-second limit and any
		// provider cooldown. No immediate retry of 429, 404, or invalid JSON.
		listens, err = c.get(ctx, username, "listens?count=1")
	}
	return listens, false, currentErr != nil, err
}

type listenBrainzHTTPError int

func (e listenBrainzHTTPError) Error() string {
	return fmt.Sprintf("ListenBrainz HTTP %d", int(e))
}

func retryableListenBrainzError(err error) bool {
	var status listenBrainzHTTPError
	if errors.As(err, &status) {
		return status == http.StatusRequestTimeout || status >= 500
	}
	var networkError net.Error
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &networkError)
}

func listenBrainzFailureReason(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, errListenBrainzUser):
		return "user_not_found"
	case errors.Is(err, errListenBrainzRate):
		return "rate_limited"
	case errors.Is(err, errListenBrainzData):
		return "invalid_response"
	}
	var status listenBrainzHTTPError
	if errors.As(err, &status) {
		return fmt.Sprintf("http_%d", int(status))
	}
	return "network_error"
}

func (c *listenBrainzClient) get(ctx context.Context, username, endpoint string) ([]listenBrainzListen, error) {
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if delay := time.Until(c.next); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.next = time.Now().Add(time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.listenbrainz.org/1/user/"+url.PathEscape(username)+"/"+endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "GoBot/1.0 ( https://github.com/variablenix/GoBot )")
	req.Header.Set("Accept", "application/json")
	base := c.http
	if base == nil {
		base = apiHTTPClient
	}
	client := *base
	// Use the single command deadline for both API calls and rate-limit waits,
	// rather than the unrelated generic API client's shorter timeout.
	client.Timeout = 0
	// A redirect would be an uncounted request and is unnecessary for this API.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	c.observeRate(res, time.Now())
	switch res.StatusCode {
	case http.StatusNotFound:
		return nil, errListenBrainzUser
	case http.StatusTooManyRequests:
		return nil, errListenBrainzRate
	case http.StatusOK:
	default:
		return nil, listenBrainzHTTPError(res.StatusCode)
	}
	const maxBody = 512 << 10
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxBody {
		return nil, errListenBrainzData
	}
	var data struct {
		Payload *struct {
			Listens []listenBrainzListen `json:"listens"`
		} `json:"payload"`
	}
	if json.Unmarshal(raw, &data) != nil || data.Payload == nil || data.Payload.Listens == nil {
		return nil, errListenBrainzData
	}
	if len(data.Payload.Listens) > 1 {
		return nil, errListenBrainzData
	}
	for _, listen := range data.Payload.Listens {
		if cleanTerminalText(listen.Metadata.Track) == "" || cleanTerminalText(listen.Metadata.Artist) == "" {
			return nil, errListenBrainzData
		}
	}
	return data.Payload.Listens, nil
}

// Honor the provider's shared window, plus standard Retry-After on throttling.
// Long cooldowns are retained; individual callers still have short deadlines.
func (c *listenBrainzClient) observeRate(res *http.Response, now time.Time) {
	postpone := func(seconds string) {
		n, err := strconv.ParseInt(seconds, 10, 64)
		if err == nil && n >= 0 {
			// Saturate rather than overflowing an untrusted duration.
			if n > 365*24*60*60 {
				n = 365 * 24 * 60 * 60
			}
			if next := now.Add(time.Duration(n) * time.Second); next.After(c.next) {
				c.next = next
			}
		}
	}
	if res.Header.Get("X-RateLimit-Remaining") == "0" || res.StatusCode == http.StatusTooManyRequests {
		postpone(res.Header.Get("X-RateLimit-Reset-In"))
		if reset, err := strconv.ParseInt(res.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && reset > now.Unix() {
			// Prefer the relative header, which is safe against clock skew.
			if relative, err := strconv.ParseInt(res.Header.Get("X-RateLimit-Reset-In"), 10, 64); err != nil || relative < 0 {
				postpone(strconv.FormatInt(reset-now.Unix(), 10))
			}
		}
	}
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable {
		if retry, err := http.ParseTime(res.Header.Get("Retry-After")); err == nil {
			postpone(strconv.FormatInt(int64(retry.Sub(now).Seconds())+1, 10))
		} else {
			postpone(res.Header.Get("Retry-After"))
		}
	}
}
