package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const (
	defaultLyricsMaxLength = 320
	lyricsIRCMaxLineBytes  = 512
	geniusSearchEndpoint   = "https://api.genius.com/search"
)

var lyricsHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	// The bearer token is only intended for api.genius.com. A redirect is an
	// upstream failure here, not permission to forward credentials elsewhere.
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type Lyrics struct {
	cfg      bot.PluginConfig
	cooldown scopedCooldown
}

type geniusSearchResponse struct {
	Response struct {
		Hits []geniusSearchHit `json:"hits"`
	} `json:"response"`
}

type geniusSearchHit struct {
	// Genius returns the hit type beside the nested result object. Keep this
	// at the hit level so song results are not mistaken for empty results.
	Type   string     `json:"type"`
	Result geniusSong `json:"result"`
}

type geniusSong struct {
	ID            int64  `json:"id"`
	Title         string `json:"title"`
	FullTitle     string `json:"full_title"`
	ArtistNames   string `json:"artist_names"`
	URL           string `json:"url"`
	PrimaryArtist struct {
		Name string `json:"name"`
	} `json:"primary_artist"`
}

func (p *Lyrics) Name() string       { return "lyrics" }
func (p *Lyrics) Commands() []string { return []string{"lyrics", "lyric", "genius"} }
func (p *Lyrics) Help() string {
	return "!lyrics <song or artist> — find a Genius lyrics page; aliases: !lyric, !genius"
}

func (p *Lyrics) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.cfg = c
	p.cooldown.configure(c.Int("cooldown_seconds", 5), 5)
	return nil
}

func (p *Lyrics) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || !isLyricsCommand(cmd) {
		return false
	}

	query := strings.TrimSpace(arg)
	if !validLyricsQuery(query) {
		p.send(b, m.ReplyTarget(), "usage: !lyrics <song or artist>")
		return true
	}

	key := scopedKey(b.Config.NetworkName, m.ReplyTarget(), pluginIdentity(m))
	if !p.cooldown.allow(key) {
		p.send(b, m.ReplyTarget(), "lyrics search is cooling down — please wait a moment")
		return true
	}

	token := strings.TrimSpace(os.Getenv("BOT_GENIUS_ACCESS_TOKEN"))
	if !validGeniusToken(token) {
		p.send(b, m.ReplyTarget(), "lyrics search is not configured (set BOT_GENIUS_ACCESS_TOKEN)")
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), lyricsTimeout(p.cfg))
	defer cancel()
	song, err := lookupGeniusSong(ctx, query, token)
	if err != nil {
		switch {
		case errors.Is(err, errGeniusNotFound):
			p.send(b, m.ReplyTarget(), fmt.Sprintf("no Genius lyrics found for %q", query))
		case errors.Is(err, errGeniusUnauthorized):
			p.send(b, m.ReplyTarget(), "lyrics search authentication failed; check BOT_GENIUS_ACCESS_TOKEN")
		default:
			p.send(b, m.ReplyTarget(), "lyrics search is temporarily unavailable")
		}
		return true
	}

	reply, ok := formatLyricsResult(song, m.ReplyTarget(), lyricsMaxLength(p.cfg))
	if !ok {
		p.send(b, m.ReplyTarget(), "lyrics result could not be formatted safely")
		return true
	}
	b.Send(m.ReplyTarget(), reply)
	return true
}

func (p *Lyrics) send(b *bot.Bot, target, text string) {
	b.Send(target, boundLyricsReply(target, text, lyricsMaxLength(p.cfg)))
}

var (
	errGeniusNotFound     = errors.New("genius song not found")
	errGeniusUnauthorized = errors.New("genius authentication failed")
)

func isLyricsCommand(command string) bool {
	switch strings.ToLower(command) {
	case "lyrics", "lyric", "genius":
		return true
	default:
		return false
	}
}

func validLyricsQuery(query string) bool {
	if query == "" || !utf8.ValidString(query) || len([]rune(query)) > 160 {
		return false
	}
	for _, r := range query {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) ||
			(r >= 0xFE00 && r <= 0xFE0F) ||
			(r >= 0xE0100 && r <= 0xE01EF) {
			return false
		}
	}
	return true
}

func validGeniusToken(token string) bool {
	if token == "" || len(token) > 4096 || !utf8.ValidString(token) {
		return false
	}
	for _, r := range token {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func lyricsTimeout(c bot.PluginConfig) time.Duration {
	seconds := c.Int("timeout_seconds", 8)
	if seconds < 1 || seconds > 30 {
		seconds = 8
	}
	return time.Duration(seconds) * time.Second
}

func lyricsMaxLength(c bot.PluginConfig) int {
	max := c.Int("max_length", defaultLyricsMaxLength)
	if max < 120 || max > 500 {
		max = defaultLyricsMaxLength
	}
	return max
}

func boundLyricsReply(target, text string, configuredMax int) string {
	text = cleanLyricsText(text)
	return truncateUTF8Bytes(text, lyricsReplyByteLimit(target, configuredMax))
}

func lyricsReplyByteLimit(target string, configuredMax int) int {
	wireLimit := lyricsIRCMaxLineBytes - len("PRIVMSG ") - len([]byte(target)) - len(" :") - len("\r\n")
	if wireLimit < 1 {
		wireLimit = 1
	}
	if configuredMax < 1 || configuredMax > wireLimit {
		configuredMax = wireLimit
	}
	return configuredMax
}

func cleanLyricsText(text string) string {
	text = cleanExternalText(text)
	var cleaned strings.Builder
	cleaned.Grow(len(text))
	for _, r := range text {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) ||
			(r >= 0xFE00 && r <= 0xFE0F) ||
			(r >= 0xE0100 && r <= 0xE01EF) {
			continue
		}
		cleaned.WriteRune(r)
	}
	return strings.Join(strings.Fields(cleaned.String()), " ")
}

func lookupGeniusSong(ctx context.Context, query, token string) (geniusSong, error) {
	if !validLyricsQuery(query) {
		return geniusSong{}, errGeniusNotFound
	}
	endpoint, err := url.Parse(geniusSearchEndpoint)
	if err != nil {
		return geniusSong{}, err
	}
	values := endpoint.Query()
	values.Set("q", query)
	endpoint.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return geniusSong{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; Genius lyrics link lookup)")

	res, err := lyricsHTTPClient.Do(req)
	if err != nil {
		return geniusSong{}, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return geniusSong{}, errGeniusUnauthorized
	}
	if res.StatusCode == http.StatusNotFound {
		return geniusSong{}, errGeniusNotFound
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return geniusSong{}, fmt.Errorf("genius returned HTTP %d", res.StatusCode)
	}

	var payload geniusSearchResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&payload); err != nil {
		return geniusSong{}, err
	}
	for _, hit := range payload.Response.Hits {
		if !strings.EqualFold(strings.TrimSpace(hit.Type), "song") {
			continue
		}
		if !validGeniusSongURL(hit.Result.URL) {
			continue
		}
		return hit.Result, nil
	}
	return geniusSong{}, errGeniusNotFound
}

func validGeniusSongURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || cleanLyricsText(raw) != raw {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.User != nil ||
		parsed.Path == "" || parsed.Path == "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	host := strings.ToLower(parsed.Host)
	return host == "genius.com" || host == "www.genius.com"
}

func formatLyricsResult(song geniusSong, target string, configuredMax int) (string, bool) {
	link := strings.TrimSpace(song.URL)
	if !validGeniusSongURL(link) {
		return "", false
	}
	limit := lyricsReplyByteLimit(target, configuredMax)
	prefix := "[lyrics] "
	if len(prefix)+len(link) > limit && song.ID > 0 {
		link = "https://genius.com/songs/" + strconv.FormatInt(song.ID, 10)
	}
	if len(prefix)+len(link) > limit {
		return "", false
	}

	artist := cleanLyricsLabel(song.ArtistNames)
	if artist == "" {
		artist = cleanLyricsLabel(song.PrimaryArtist.Name)
	}
	title := cleanLyricsLabel(song.Title)
	if title == "" {
		title = cleanLyricsLabel(song.FullTitle)
	}
	label := "Genius song"
	if artist != "" && title != "" {
		label = artist + " - " + title
	} else if title != "" {
		label = title
	}

	suffix := " | " + link
	available := limit - len(prefix) - len(suffix)
	if available < 1 {
		return prefix + link, true
	}
	label = truncateUTF8Bytes(label, available)
	return prefix + label + suffix, true
}

func cleanLyricsLabel(text string) string {
	cleaned := cleanLyricsText(text)
	lower := strings.ToLower(cleaned)
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") ||
		strings.Contains(lower, "www.") {
		return ""
	}
	return cleaned
}
