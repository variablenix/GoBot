package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type LastFM struct {
	cfg bot.PluginConfig
	db  *storage.DB
}

func (p *LastFM) Name() string       { return "lastfm" }
func (p *LastFM) Commands() []string { return []string{"lastfm", "last", "np"} }
func (p *LastFM) Help() string {
	return "!lastfm [username] — show a user's now-playing or last-played track (requires an API key)"
}
func (p *LastFM) Init(c bot.PluginConfig, db *storage.DB) error { p.cfg, p.db = c, db; return nil }

func (p *LastFM) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "lastfm" && cmd != "last" && cmd != "np") {
		return false
	}
	apiKey := strings.TrimSpace(p.cfg.String("api_key", ""))
	if apiKey == "" {
		b.Send(m.ReplyTarget(), "last.fm is not configured; add plugins.lastfm.api_key")
		return true
	}
	username := strings.TrimSpace(arg)
	if username == "" {
		username = p.savedUser(m.Nick)
	} else if validLastFMUser(username) {
		p.saveUser(m.Nick, username)
	}
	if !validLastFMUser(username) {
		b.Send(m.ReplyTarget(), "usage: !lastfm <username> (or save one by using the command once with a username)")
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	values := url.Values{"method": {"user.getrecenttracks"}, "user": {username}, "api_key": {apiKey}, "format": {"json"}, "limit": {"1"}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://ws.audioscrobbler.com/2.0/?"+values.Encode(), nil)
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot)")
	res, err := apiHTTPClient.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		if res != nil {
			res.Body.Close()
		}
		b.Send(m.ReplyTarget(), "Last.fm is temporarily unavailable")
		return true
	}
	defer res.Body.Close()
	var data struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
		Recent  struct {
			Track []struct {
				Name   string `json:"name"`
				URL    string `json:"url"`
				Artist struct {
					Text string `json:"#text"`
				} `json:"artist"`
				Attr struct {
					NowPlaying string `json:"nowplaying"`
				} `json:"@attr"`
				Date struct {
					UTS string `json:"uts"`
				} `json:"date"`
			} `json:"track"`
		} `json:"recenttracks"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 512<<10)).Decode(&data); err != nil || data.Error != 0 {
		b.Send(m.ReplyTarget(), "no Last.fm track found for "+username)
		return true
	}
	if len(data.Recent.Track) == 0 {
		b.Send(m.ReplyTarget(), "no recent Last.fm tracks found for "+username)
		return true
	}
	track := data.Recent.Track[0]
	status := "last played"
	if strings.EqualFold(track.Attr.NowPlaying, "true") {
		status = "is listening to"
	}
	text := fmt.Sprintf("%s %s %q by %s", cleanExternalText(username), status, cleanExternalText(track.Name), cleanExternalText(track.Artist.Text))
	if track.URL != "" {
		text += " — " + cleanExternalText(track.URL)
	}
	b.Send(m.ReplyTarget(), truncateRunes(text, 380))
	return true
}

func validLastFMUser(user string) bool {
	if user == "" || len([]rune(user)) > 64 || strings.ContainsAny(user, " \r\n\t") {
		return false
	}
	return true
}

func (p *LastFM) savedUser(nick string) string {
	if p.db == nil {
		return ""
	}
	var user string
	if raw, err := p.db.Get("lastfm", strings.ToLower(nick)); err == nil && storage.Decode(raw, &user) == nil {
		return user
	}
	return ""
}

func (p *LastFM) saveUser(nick, user string) {
	if p.db != nil {
		_ = p.db.Set("lastfm", strings.ToLower(nick), user)
	}
}
