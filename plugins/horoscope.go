package plugins

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

var horoscopeSigns = map[string]string{
	"aries": "Aries", "taurus": "Taurus", "gemini": "Gemini", "cancer": "Cancer",
	"leo": "Leo", "virgo": "Virgo", "libra": "Libra", "scorpio": "Scorpio",
	"sagittarius": "Sagittarius", "capricorn": "Capricorn", "aquarius": "Aquarius", "pisces": "Pisces",
}

const (
	defaultHoroscopeSummaryLength = 360
	maxHoroscopeMessageBytes      = 450
)

type Horoscope struct {
	db               *storage.DB
	maxSummaryLength int
}

func (p *Horoscope) Name() string       { return "horoscope" }
func (p *Horoscope) Commands() []string { return []string{"horoscope", "zodiac"} }
func (p *Horoscope) Help() string {
	return "!horoscope [sign] — show today's horoscope; specifying a sign saves it for you"
}
func (p *Horoscope) Init(c bot.PluginConfig, db *storage.DB) error {
	p.db = db
	p.maxSummaryLength = c.Int("max_summary_length", defaultHoroscopeSummaryLength)
	if p.maxSummaryLength < 120 {
		p.maxSummaryLength = 120
	}
	if p.maxSummaryLength > 400 {
		p.maxSummaryLength = 400
	}
	return nil
}

func (p *Horoscope) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "horoscope" && cmd != "zodiac") {
		return false
	}
	sign := strings.ToLower(strings.TrimSpace(arg))
	if sign == "" {
		sign = p.savedSign(m.Nick)
	}
	canonical, valid := horoscopeSigns[sign]
	if !valid {
		b.Send(m.ReplyTarget(), "usage: !horoscope <sign> (aries, taurus, gemini, cancer, leo, virgo, libra, scorpio, sagittarius, capricorn, aquarius, or pisces)")
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	endpoint := "https://horoscope-app-api.vercel.app/api/v1/get-horoscope/daily?sign=" + url.QueryEscape(canonical) + "&day=TODAY"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		b.Send(m.ReplyTarget(), "horoscope is temporarily unavailable")
		return true
	}
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot)")
	res, err := apiHTTPClient.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		if res != nil {
			res.Body.Close()
		}
		b.Send(m.ReplyTarget(), "horoscope is temporarily unavailable")
		return true
	}
	defer res.Body.Close()
	var data struct {
		Data struct {
			Horoscope string `json:"horoscope"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 256<<10)).Decode(&data); err != nil || strings.TrimSpace(data.Data.Horoscope) == "" {
		b.Send(m.ReplyTarget(), "horoscope data could not be parsed")
		return true
	}
	if strings.TrimSpace(arg) != "" {
		p.saveSign(m.Nick, sign)
	}
	text := cleanExternalText(data.Data.Horoscope)
	maxSummaryLength := p.maxSummaryLength
	if maxSummaryLength == 0 {
		maxSummaryLength = defaultHoroscopeSummaryLength
	}
	b.Send(m.ReplyTarget(), zodiacEmoji(sign)+" "+formatHoroscopeReply(canonical, sign, text, maxSummaryLength))
	return true
}

func formatHoroscopeReply(canonical, sign, text string, maxSummaryLength int) string {
	text = cleanExternalText(text)
	if maxSummaryLength <= 0 {
		maxSummaryLength = defaultHoroscopeSummaryLength
	}
	wasTruncated := len([]rune(text)) > maxSummaryLength
	text = truncateRunes(text, maxSummaryLength)
	prefix := canonical + ": "
	result := prefix + text
	if !wasTruncated && len([]byte(result)) <= maxHoroscopeMessageBytes {
		return result
	}

	suffix := " Read more: " + horoscopeSourceURL(sign)
	available := maxHoroscopeMessageBytes - len([]byte(prefix)) - len([]byte(suffix))
	if available < 2 {
		return truncateBytes(prefix+text, maxHoroscopeMessageBytes)
	}
	text = truncateBytes(text, available)
	if wasTruncated {
		return prefix + text + suffix
	}
	return prefix + text + suffix
}

func truncateBytes(text string, max int) string {
	if max <= 0 {
		return ""
	}
	if len([]byte(text)) <= max {
		return text
	}
	if max <= len([]byte("…")) {
		return "…"
	}

	var result strings.Builder
	for _, r := range text {
		next := string(r)
		if result.Len()+len([]byte(next))+len([]byte("…")) > max {
			break
		}
		result.WriteString(next)
	}
	return result.String() + "…"
}

// horoscopeSourceURL points users to a readable daily horoscope page rather
// than exposing the JSON API endpoint. The sign is validated before this
// helper is called, but escaping keeps the URL safe if it is reused later.
func horoscopeSourceURL(sign string) string {
	return "https://astrology.com.au/horoscopes/daily-horoscopes/" + url.PathEscape(strings.ToLower(sign))
}

func zodiacEmoji(sign string) string {
	switch strings.ToLower(sign) {
	case "aries":
		return "♈"
	case "taurus":
		return "♉"
	case "gemini":
		return "♊"
	case "cancer":
		return "♋"
	case "leo":
		return "♌"
	case "virgo":
		return "♍"
	case "libra":
		return "♎"
	case "scorpio":
		return "♏"
	case "sagittarius":
		return "♐"
	case "capricorn":
		return "♑"
	case "aquarius":
		return "♒"
	case "pisces":
		return "♓"
	default:
		return ""
	}
}

func (p *Horoscope) savedSign(nick string) string {
	if p.db == nil {
		return ""
	}
	var sign string
	if raw, err := p.db.Get("horoscope", strings.ToLower(nick)); err == nil && storage.Decode(raw, &sign) == nil {
		return sign
	}
	return ""
}

func (p *Horoscope) saveSign(nick, sign string) {
	if p.db != nil {
		_ = p.db.Set("horoscope", strings.ToLower(nick), sign)
	}
}
