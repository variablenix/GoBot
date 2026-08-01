package plugins

import (
	"context"
	"encoding/json"
	"fmt"
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

const defaultHoroscopeSummaryLength = 220

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
	if p.maxSummaryLength > 260 {
		p.maxSummaryLength = 260
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
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil || strings.TrimSpace(data.Data.Horoscope) == "" {
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
	text = truncateRunes(text, maxSummaryLength)
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s: %s Read more: %s", canonical, text, horoscopeSourceURL(sign)))
	return true
}

// horoscopeSourceURL points users to a readable daily horoscope page rather
// than exposing the JSON API endpoint. The sign is validated before this
// helper is called, but escaping keeps the URL safe if it is reused later.
func horoscopeSourceURL(sign string) string {
	return "https://astrology.com.au/horoscopes/daily-horoscopes/" + url.PathEscape(strings.ToLower(sign))
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
