package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

// Keep this marker to a single code point. In particular, do not append
// U+FE0F: some terminal stacks calculate emoji presentation sequences with
// inconsistent cell widths.
const imdbPrefix = "🎥"

const (
	defaultIMDbMaxLength = 320
	defaultIMDbResults   = 3
	imdbIRCMaxLineBytes  = 512
)

type IMDb struct {
	cfg      bot.PluginConfig
	cooldown scopedCooldown
}

type imdbTitle struct {
	ID      string `json:"id"`
	Label   string `json:"l"`
	Kind    string `json:"q"`
	KindID  string `json:"qid"`
	Stars   string `json:"s"`
	Year    int    `json:"y"`
	YearRaw string `json:"yr"`
}

type imdbSuggestionResponse struct {
	Titles []imdbTitle `json:"d"`
}

func (p *IMDb) Name() string       { return "imdb" }
func (p *IMDb) Commands() []string { return []string{"imdb"} }
func (p *IMDb) Help() string {
	return "!imdb <movie or film> — search IMDb titles; no API key required"
}

func (p *IMDb) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.cfg = c
	p.cooldown.configure(c.Int("cooldown_seconds", 5), 5)
	return nil
}

func (p *IMDb) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || !strings.EqualFold(cmd, "imdb") {
		return false
	}

	query := strings.TrimSpace(arg)
	if !validIMDbQuery(query) {
		p.send(b, m.ReplyTarget(), "usage: !imdb <movie or film>")
		return true
	}

	key := scopedKey(b.Config.NetworkName, m.ReplyTarget(), pluginIdentity(m))
	if !p.cooldown.allow(key) {
		p.send(b, m.ReplyTarget(), "IMDb search is cooling down — please wait a moment")
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), imdbTimeout(p.cfg))
	defer cancel()
	titles, err := lookupIMDbTitles(ctx, query)
	if err != nil {
		if err == errIMDbNotFound {
			p.send(b, m.ReplyTarget(), fmt.Sprintf("%s IMDb: no movie or film found for %q", imdbPrefix, query))
		} else {
			p.send(b, m.ReplyTarget(), "IMDb search is temporarily unavailable")
		}
		return true
	}

	result := formatIMDbResults(titles, imdbMaxResults(p.cfg))
	p.send(b, m.ReplyTarget(), result)
	return true
}

func (p *IMDb) send(b *bot.Bot, target, text string) {
	b.Send(target, boundIMDbReply(target, text, imdbMaxLength(p.cfg)))
}

var errIMDbNotFound = errors.New("IMDb title not found")

func validIMDbQuery(query string) bool {
	if query == "" || len([]rune(query)) > 120 {
		return false
	}
	for _, r := range query {
		if unsafeIMDbRune(r) {
			return false
		}
	}
	return true
}

func imdbTimeout(c bot.PluginConfig) time.Duration {
	seconds := c.Int("timeout_seconds", 8)
	if seconds < 1 || seconds > 30 {
		seconds = 8
	}
	return time.Duration(seconds) * time.Second
}

func imdbMaxLength(c bot.PluginConfig) int {
	max := c.Int("max_length", defaultIMDbMaxLength)
	if max < 120 || max > 500 {
		max = defaultIMDbMaxLength
	}
	return max
}

func imdbMaxResults(c bot.PluginConfig) int {
	max := c.Int("max_results", defaultIMDbResults)
	if max < 1 || max > 5 {
		max = defaultIMDbResults
	}
	return max
}

func boundIMDbReply(target, text string, configuredMax int) string {
	text = cleanIMDbText(text)
	// gopkg.in/irc.v3 writes messages as-is. Reserve the exact bytes used by
	// "PRIVMSG <target> :" and CRLF so the complete wire line stays at or
	// below IRC's 512-byte limit.
	wireLimit := imdbIRCMaxLineBytes - len("PRIVMSG ") - len([]byte(target)) - len(" :") - len("\r\n")
	if wireLimit < 1 {
		wireLimit = 1
	}
	if configuredMax < 1 || configuredMax > wireLimit {
		configuredMax = wireLimit
	}
	return truncateUTF8Bytes(text, configuredMax)
}

func truncateUTF8Bytes(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(text) <= maxBytes {
		return text
	}
	const suffix = "…"
	if maxBytes < len(suffix) {
		return strings.Repeat(".", maxBytes)
	}
	cut := maxBytes - len(suffix)
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + suffix
}

func cleanIMDbText(text string) string {
	text = cleanExternalText(text)
	var cleaned strings.Builder
	cleaned.Grow(len(text))
	for _, r := range text {
		if unsafeIMDbRune(r) {
			continue
		}
		cleaned.WriteRune(r)
	}
	return strings.Join(strings.Fields(cleaned.String()), " ")
}

func unsafeIMDbRune(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) ||
		(r >= 0xFE00 && r <= 0xFE0F) ||
		(r >= 0xE0100 && r <= 0xE01EF)
}

func lookupIMDbTitles(ctx context.Context, query string) ([]imdbTitle, error) {
	if !validIMDbQuery(query) {
		return nil, errIMDbNotFound
	}
	endpoint := "https://v3.sg.media-imdb.com/suggestion/x/" + url.PathEscape(query) + ".json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; IMDb lookup)")
	res, err := apiHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, errIMDbNotFound
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("IMDb returned HTTP %d", res.StatusCode)
	}

	var payload imdbSuggestionResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 2*1024*1024)).Decode(&payload); err != nil {
		return nil, err
	}
	titles := make([]imdbTitle, 0, len(payload.Titles))
	for _, title := range payload.Titles {
		if !isIMDbTitle(title) || !validIMDbID(title.ID) || strings.TrimSpace(title.Label) == "" {
			continue
		}
		titles = append(titles, title)
	}
	if len(titles) == 0 {
		return nil, errIMDbNotFound
	}
	return titles, nil
}

func isIMDbTitle(title imdbTitle) bool {
	// The suggestion endpoint also returns people. Keep this command focused on
	// titles while accepting movies, series, episodes, shorts, and videos.
	return !strings.EqualFold(title.KindID, "name") && !strings.EqualFold(title.Kind, "name")
}

func validIMDbID(id string) bool {
	id = strings.TrimSpace(id)
	if len(id) < 3 || !strings.HasPrefix(id, "tt") {
		return false
	}
	for _, r := range id[2:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func formatIMDbResults(titles []imdbTitle, maxResults int) string {
	if len(titles) == 0 {
		return imdbPrefix + " IMDb: no movie or film found"
	}
	if maxResults < 1 {
		maxResults = defaultIMDbResults
	}
	if len(titles) < maxResults {
		maxResults = len(titles)
	}
	items := make([]string, 0, maxResults)
	for _, title := range titles[:maxResults] {
		label := cleanIMDbText(title.Label)
		details := make([]string, 0, 3)
		if year := imdbYear(title); year != "" {
			details = append(details, year)
		}
		if kind := imdbKind(title); kind != "" {
			details = append(details, kind)
		}
		if stars := cleanIMDbText(title.Stars); stars != "" {
			details = append(details, stars)
		}
		if len(details) > 0 {
			label += " (" + strings.Join(details, "; ") + ")"
		}
		id := strings.TrimSpace(title.ID)
		items = append(items, label+" | https://www.imdb.com/title/"+id+"/")
	}
	result := imdbPrefix + " IMDb: " + strings.Join(items, " ; ")
	if remaining := len(titles) - maxResults; remaining > 0 {
		result += fmt.Sprintf(" + %d more", remaining)
	}
	return result
}

func imdbYear(title imdbTitle) string {
	if title.Year > 0 {
		return fmt.Sprintf("%d", title.Year)
	}
	return cleanIMDbText(title.YearRaw)
}

func imdbKind(title imdbTitle) string {
	switch strings.ToLower(strings.TrimSpace(title.KindID)) {
	case "movie":
		return "movie"
	case "tvmovie":
		return "TV movie"
	case "tvseries":
		return "series"
	case "tvminiseries":
		return "miniseries"
	case "tvepisode":
		return "episode"
	case "short":
		return "short"
	case "video":
		return "video"
	case "videogame":
		return "game"
	default:
		return cleanIMDbText(title.Kind)
	}
}
