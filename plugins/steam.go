package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const (
	steamSearchPageURL = "https://store.steampowered.com/search/"
	steamGenrePageURL  = "https://store.steampowered.com/tags/en/"
	steamStorePageURL  = "https://store.steampowered.com/app/"
	steamChartsPageURL = "https://store.steampowered.com/charts/mostplayed/"
	steamDefaultMaxLen = 360
)

var (
	steamSearchAPIURL  = "https://store.steampowered.com/api/storesearch/"
	steamAppDetailsURL = "https://store.steampowered.com/api/appdetails"
	steamChartsAPIURL  = "https://api.steampowered.com/ISteamChartsService/GetMostPlayedGames/v1/"
	steamHTTPClient    = &http.Client{Timeout: 10 * time.Second}
	steamHTMLTagRegex  = regexp.MustCompile(`<[^>]*>`)
)

type Steam struct {
	maxLength int
	timeout   time.Duration
}

type steamSearchResponse struct {
	Total int                 `json:"total"`
	Items []steamSearchResult `json:"items"`
}

type steamSearchResult struct {
	Type string `json:"type"`
	Name string `json:"name"`
	ID   int    `json:"id"`
}

type steamGame struct {
	Name             string `json:"name"`
	SteamAppID       int    `json:"steam_appid"`
	ShortDescription string `json:"short_description"`
	DetailedDesc     string `json:"detailed_description"`
	IsFree           bool   `json:"is_free"`
	Genres           []struct {
		Description string `json:"description"`
	} `json:"genres"`
	ReleaseDate struct {
		ComingSoon bool   `json:"coming_soon"`
		Date       string `json:"date"`
	} `json:"release_date"`
	PriceOverview *struct {
		Initial int `json:"initial"`
		Final   int `json:"final"`
	} `json:"price_overview"`
}

type steamChartsResponse struct {
	Response struct {
		Ranks []struct {
			Rank       int `json:"rank"`
			AppID      int `json:"appid"`
			PeakInGame int `json:"peak_in_game"`
		} `json:"ranks"`
	} `json:"response"`
}

func (p *Steam) Name() string { return "steam" }

func (p *Steam) Commands() []string {
	return []string{"steam", "game", "steamtop"}
}

func (p *Steam) Help() string {
	return "!steam <title> — find one Steam game; !steam info <app id or store URL>; !steam genre <tag>; !steam top — #1 most-played game (aliases: !game, !steamtop; no API key required)"
}

func (p *Steam) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.maxLength = c.Int("max_length", steamDefaultMaxLen)
	if p.maxLength < 180 || p.maxLength > 500 {
		p.maxLength = steamDefaultMaxLen
	}
	timeoutSeconds := c.Int("timeout_seconds", 10)
	if timeoutSeconds < 3 || timeoutSeconds > 20 {
		timeoutSeconds = 10
	}
	p.timeout = time.Duration(timeoutSeconds) * time.Second
	return nil
}

func (p *Steam) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || !isSteamCommand(cmd) {
		return false
	}
	request := parseSteamRequest(cmd, arg)
	switch request.kind {
	case steamGenreRequest:
		p.handleGenre(b, m, request.value)
		return true
	case steamTopRequest:
		p.handleTop(b, m)
		return true
	case steamInfoRequest:
		p.handleInfo(b, m, request.value)
		return true
	case steamSearchRequest:
		p.handleSearch(b, m, request.value)
		return true
	default:
		b.Send(m.ReplyTarget(), steamUsage())
		return true
	}
}

type steamRequestKind int

const (
	steamInvalidRequest steamRequestKind = iota
	steamSearchRequest
	steamInfoRequest
	steamGenreRequest
	steamTopRequest
)

type steamRequest struct {
	kind  steamRequestKind
	value string
}

func parseSteamRequest(command, arg string) steamRequest {
	arg = strings.TrimSpace(arg)
	if strings.EqualFold(command, "steamtop") || strings.EqualFold(arg, "top") || strings.EqualFold(arg, "mostplayed") {
		return steamRequest{kind: steamTopRequest}
	}
	if arg == "" {
		return steamRequest{kind: steamInvalidRequest}
	}
	fields := strings.Fields(arg)
	first := strings.ToLower(fields[0])
	rest := strings.TrimSpace(arg[len(fields[0]):])
	switch first {
	case "genre", "tag":
		if rest == "" {
			return steamRequest{kind: steamInvalidRequest}
		}
		return steamRequest{kind: steamGenreRequest, value: rest}
	case "info", "app", "details":
		if rest == "" {
			return steamRequest{kind: steamInvalidRequest}
		}
		return steamRequest{kind: steamInfoRequest, value: rest}
	case "top", "mostplayed":
		return steamRequest{kind: steamTopRequest}
	case "search":
		if rest == "" {
			return steamRequest{kind: steamInvalidRequest}
		}
		return steamRequest{kind: steamSearchRequest, value: rest}
	default:
		return steamRequest{kind: steamSearchRequest, value: arg}
	}
}

func isSteamCommand(command string) bool {
	switch strings.ToLower(command) {
	case "steam", "game", "steamtop":
		return true
	default:
		return false
	}
}

func steamUsage() string {
	return ircColor(ircYellow, "usage: !steam <title> | !steam info <app id or store URL> | !steam genre <tag> | !steam top")
}

func (p *Steam) handleSearch(b *bot.Bot, m bot.Message, query string) {
	query = strings.TrimSpace(query)
	if query == "" || len([]rune(query)) > 100 {
		b.Send(m.ReplyTarget(), steamUsage())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	search, err := p.search(ctx, query)
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "Steam search is temporarily unavailable"))
		return
	}
	result, ok := firstSteamApp(search.Items)
	if !ok {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "I couldn't find a Steam game matching that search"))
		return
	}
	game, err := p.appDetails(ctx, result.ID)
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "Steam found a result, but its game details are temporarily unavailable"))
		return
	}
	searchURL := steamSearchURL(query)
	p.sendGameInfo(b, m, formatSteamGame(game, searchURL, false), formatSteamGame(game, searchURL, true))
}

func (p *Steam) handleInfo(b *bot.Bot, m bot.Message, value string) {
	appID, ok := steamAppID(value)
	if !ok {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !steam info <numeric app id or Steam store URL>"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	game, err := p.appDetails(ctx, appID)
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "Steam game details are temporarily unavailable"))
		return
	}
	detail := formatSteamGame(game, "", true)
	p.sendGameInfo(b, m, detail, detail)
}

func (p *Steam) handleGenre(b *bot.Bot, m bot.Message, genre string) {
	genre = strings.TrimSpace(genre)
	if genre == "" || len([]rune(genre)) > 60 {
		b.Send(m.ReplyTarget(), steamUsage())
		return
	}
	label := cleanExternalText(genre)
	if label == "" {
		b.Send(m.ReplyTarget(), steamUsage())
		return
	}
	link := steamGenreURL(label)
	b.Send(m.ReplyTarget(), fmt.Sprintf("%s Steam games: %s", ircColor(ircCyan, label), link))
}

func (p *Steam) handleTop(b *bot.Bot, m bot.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	chart, err := p.mostPlayed(ctx)
	if err != nil || len(chart.Response.Ranks) == 0 {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "Steam's most-played chart is temporarily unavailable"))
		return
	}
	top := chart.Response.Ranks[0]
	game, err := p.appDetails(ctx, top.AppID)
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "Steam's #1 game was found, but its details are temporarily unavailable"))
		return
	}
	peak := ""
	if top.PeakInGame > 0 {
		peak = fmt.Sprintf(" | recent peak %s players", formatSteamNumber(top.PeakInGame))
	}
	message := fmt.Sprintf("#1 most-played on Steam: %s%s — %s | full charts: %s", game.Name, peak, steamStoreURL(game.SteamAppID), steamChartsPageURL)
	b.Send(m.ReplyTarget(), message)
}

func (p *Steam) sendGameInfo(b *bot.Bot, m bot.Message, summary, detail string) {
	if len([]byte(summary)) <= p.maxLength {
		b.Send(m.ReplyTarget(), summary)
		return
	}
	if !steamNeedsPrivateMessage(m, summary, p.maxLength) {
		for _, part := range splitIRCText(detail, p.maxLength) {
			b.Send(m.ReplyTarget(), part)
		}
		return
	}
	for _, part := range splitIRCText(detail, p.maxLength) {
		b.Send(m.Nick, part)
	}
	b.Send(m.ReplyTarget(), fmt.Sprintf("I'm messaging you the game info, %s.", m.Nick))
}

func steamNeedsPrivateMessage(m bot.Message, text string, maxLength int) bool {
	return len([]byte(text)) > maxLength && m.IsChannel && strings.TrimSpace(m.Nick) != ""
}

func (p *Steam) search(ctx context.Context, query string) (steamSearchResponse, error) {
	endpoint, err := url.Parse(steamSearchAPIURL)
	if err != nil {
		return steamSearchResponse{}, err
	}
	params := endpoint.Query()
	params.Set("term", query)
	params.Set("l", "english")
	params.Set("cc", "us")
	endpoint.RawQuery = params.Encode()
	var response steamSearchResponse
	if err := p.getJSON(ctx, endpoint.String(), &response); err != nil {
		return steamSearchResponse{}, err
	}
	return response, nil
}

func (p *Steam) appDetails(ctx context.Context, appID int) (steamGame, error) {
	endpoint, err := url.Parse(steamAppDetailsURL)
	if err != nil {
		return steamGame{}, err
	}
	params := endpoint.Query()
	params.Set("appids", strconv.Itoa(appID))
	params.Set("l", "english")
	params.Set("cc", "us")
	endpoint.RawQuery = params.Encode()
	var response map[string]struct {
		Success bool      `json:"success"`
		Data    steamGame `json:"data"`
	}
	if err := p.getJSON(ctx, endpoint.String(), &response); err != nil {
		return steamGame{}, err
	}
	entry, ok := response[strconv.Itoa(appID)]
	if !ok || !entry.Success || entry.Data.Name == "" {
		return steamGame{}, errors.New("Steam app details unavailable")
	}
	return entry.Data, nil
}

func (p *Steam) mostPlayed(ctx context.Context) (steamChartsResponse, error) {
	var response steamChartsResponse
	if err := p.getJSON(ctx, steamChartsAPIURL, &response); err != nil {
		return steamChartsResponse{}, err
	}
	sort.SliceStable(response.Response.Ranks, func(i, j int) bool {
		return response.Response.Ranks[i].Rank < response.Response.Ranks[j].Rank
	})
	return response, nil
}

func (p *Steam) getJSON(ctx context.Context, endpoint string, value interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot; Steam lookup)")
	res, err := steamHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("Steam returned HTTP %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(value)
}

func firstSteamApp(results []steamSearchResult) (steamSearchResult, bool) {
	for _, result := range results {
		if result.ID > 0 && (result.Type == "" || strings.EqualFold(result.Type, "app")) {
			return result, true
		}
	}
	return steamSearchResult{}, false
}

func steamAppID(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return 0, false
		}
		schemeOK := strings.EqualFold(parsed.Scheme, "https") || strings.EqualFold(parsed.Scheme, "http")
		if !schemeOK || !strings.EqualFold(parsed.Hostname(), "store.steampowered.com") {
			return 0, false
		}
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) < 2 || !strings.EqualFold(parts[0], "app") {
			return 0, false
		}
		value = parts[1]
	}
	appID, err := strconv.Atoi(value)
	return appID, err == nil && appID > 0
}

func formatSteamGame(game steamGame, searchURL string, includeDescription bool) string {
	name := cleanExternalText(game.Name)
	parts := []string{ircBold + name + ircReset}
	genres := make([]string, 0, len(game.Genres))
	for _, genre := range game.Genres {
		if cleaned := cleanExternalText(genre.Description); cleaned != "" {
			genres = append(genres, cleaned)
		}
	}
	if len(genres) > 0 {
		parts = append(parts, strings.Join(genres, ", "))
	}
	if game.ReleaseDate.ComingSoon && cleanExternalText(game.ReleaseDate.Date) != "" {
		parts = append(parts, "coming "+cleanExternalText(game.ReleaseDate.Date))
	} else if release := cleanExternalText(game.ReleaseDate.Date); release != "" {
		parts = append(parts, "released "+release)
	}
	if price := steamPrice(game); price != "" {
		parts = append(parts, price)
	}
	parts = append(parts, steamStoreURL(game.SteamAppID))
	if searchURL != "" {
		parts = append(parts, "more matches: "+searchURL)
	}
	if includeDescription {
		description := cleanSteamDescription(game.DetailedDesc)
		if description == "" {
			description = cleanSteamDescription(game.ShortDescription)
		}
		if description != "" {
			parts = append(parts, "about: "+truncateRunes(description, 900))
		}
	}
	return strings.Join(parts, " | ")
}

func cleanSteamDescription(description string) string {
	description = html.UnescapeString(steamHTMLTagRegex.ReplaceAllString(description, " "))
	description = cleanExternalText(description)
	return strings.NewReplacer(" .", ".", " ,", ",", " !", "!", " ?", "?", " :", ":").Replace(description)
}

func steamPrice(game steamGame) string {
	if game.IsFree {
		return "free"
	}
	if game.PriceOverview == nil {
		return ""
	}
	current := formatSteamCents(game.PriceOverview.Final)
	if game.PriceOverview.Initial > game.PriceOverview.Final {
		return fmt.Sprintf("%s (was %s)", current, formatSteamCents(game.PriceOverview.Initial))
	}
	return current
}

func formatSteamCents(cents int) string {
	return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
}

func steamStoreURL(appID int) string {
	return steamStorePageURL + strconv.Itoa(appID) + "/"
}

func steamSearchURL(query string) string {
	return steamSearchPageURL + "?term=" + url.QueryEscape(query)
}

func steamGenreURL(genre string) string {
	return steamGenrePageURL + url.PathEscape(strings.TrimSpace(genre)) + "/"
}

func formatSteamNumber(value int) string {
	if value < 1000 {
		return strconv.Itoa(value)
	}
	if value < 1000000 {
		return fmt.Sprintf("%.1fk", float64(value)/1000)
	}
	return fmt.Sprintf("%.1fm", float64(value)/1000000)
}
