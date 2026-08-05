package plugins

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/variablenix/GoBot/bot"
)

func TestParseSteamRequest(t *testing.T) {
	tests := []struct {
		command string
		arg     string
		kind    steamRequestKind
		value   string
	}{
		{command: "steam", arg: "Portal 2", kind: steamSearchRequest, value: "Portal 2"},
		{command: "game", arg: "Baldur's Gate 3", kind: steamSearchRequest, value: "Baldur's Gate 3"},
		{command: "steam", arg: "search Half-Life", kind: steamSearchRequest, value: "Half-Life"},
		{command: "steam", arg: "genre FPS", kind: steamGenreRequest, value: "FPS"},
		{command: "steam", arg: "info https://store.steampowered.com/app/620/", kind: steamInfoRequest, value: "https://store.steampowered.com/app/620/"},
		{command: "steam", arg: "top", kind: steamTopRequest},
		{command: "steamtop", arg: "", kind: steamTopRequest},
		{command: "steam", arg: "", kind: steamInvalidRequest},
	}
	for _, test := range tests {
		got := parseSteamRequest(test.command, test.arg)
		if got.kind != test.kind || got.value != test.value {
			t.Errorf("parseSteamRequest(%q, %q) = %+v; want kind=%d value=%q", test.command, test.arg, got, test.kind, test.value)
		}
	}
}

func TestSteamAppIDAndLinks(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
		ok    bool
	}{
		{value: "620", want: 620, ok: true},
		{value: "https://store.steampowered.com/app/620/", want: 620, ok: true},
		{value: "https://store.steampowered.com/app/620/Portal_2/", want: 620, ok: true},
		{value: "not-an-app", ok: false},
	} {
		got, ok := steamAppID(test.value)
		if got != test.want || ok != test.ok {
			t.Errorf("steamAppID(%q) = %d, %v; want %d, %v", test.value, got, ok, test.want, test.ok)
		}
	}
	if got := steamStoreURL(620); got != "https://store.steampowered.com/app/620/" {
		t.Fatalf("steamStoreURL = %q", got)
	}
	if got := steamGenreURL("FPS"); got != "https://store.steampowered.com/tags/en/FPS/" {
		t.Fatalf("steamGenreURL = %q", got)
	}
}

func TestSteamFormatting(t *testing.T) {
	game := steamGame{Name: "Portal 2", SteamAppID: 620, IsFree: false, DetailedDesc: "A puzzle game with <b>portals</b>.", ShortDescription: "Short description"}
	game.Genres = append(game.Genres, struct {
		Description string `json:"description"`
	}{Description: "Action"})
	game.PriceOverview = &struct {
		Initial int `json:"initial"`
		Final   int `json:"final"`
	}{Initial: 999, Final: 499}
	formatted := formatSteamGame(game, steamSearchURL("Portal"), true)
	for _, want := range []string{"Portal 2", "Action", "$4.99 (was $9.99)", "https://store.steampowered.com/app/620/", "more matches:", "about: A puzzle game with portals."} {
		if !strings.Contains(formatted, want) {
			t.Errorf("formatted game %q does not contain %q", formatted, want)
		}
	}
	if got := steamPrice(steamGame{IsFree: true}); got != "free" {
		t.Fatalf("free steamPrice = %q", got)
	}
	if got := formatSteamNumber(1250000); got != "1.2m" {
		t.Fatalf("formatSteamNumber = %q", got)
	}
}

func TestSteamHTTPParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			_, _ = w.Write([]byte(`{"total":2,"items":[{"type":"dlc","name":"Portal 2 Soundtrack","id":999},{"type":"app","name":"Portal 2","id":620}]}`))
		case "/details":
			_, _ = w.Write([]byte(`{"620":{"success":true,"data":{"name":"Portal 2","steam_appid":620,"is_free":false,"short_description":"Puzzle game","genres":[{"description":"Action"}],"release_date":{"coming_soon":false,"date":"Apr 18, 2011"},"price_overview":{"initial":999,"final":499}}}}`))
		case "/charts":
			_, _ = w.Write([]byte(`{"response":{"ranks":[{"rank":2,"appid":570,"peak_in_game":100},{"rank":1,"appid":620,"peak_in_game":200}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldSearch, oldDetails, oldCharts, oldClient := steamSearchAPIURL, steamAppDetailsURL, steamChartsAPIURL, steamHTTPClient
	steamSearchAPIURL = server.URL + "/search"
	steamAppDetailsURL = server.URL + "/details"
	steamChartsAPIURL = server.URL + "/charts"
	steamHTTPClient = server.Client()
	t.Cleanup(func() {
		steamSearchAPIURL, steamAppDetailsURL, steamChartsAPIURL, steamHTTPClient = oldSearch, oldDetails, oldCharts, oldClient
	})

	plugin := &Steam{}
	if err := plugin.Init(bot.PluginConfig{}, nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	search, err := plugin.search(t.Context(), "Portal 2")
	if err != nil {
		t.Fatalf("search returned error: %v", err)
	}
	result, ok := firstSteamApp(search.Items)
	if !ok || result.ID != 620 {
		t.Fatalf("firstSteamApp = %+v, %v; want app 620", result, ok)
	}
	game, err := plugin.appDetails(t.Context(), result.ID)
	if err != nil || game.Name != "Portal 2" {
		t.Fatalf("appDetails = %+v, %v", game, err)
	}
	charts, err := plugin.mostPlayed(t.Context())
	if err != nil || charts.Response.Ranks[0].Rank != 1 || charts.Response.Ranks[0].AppID != 620 {
		t.Fatalf("mostPlayed = %+v, %v", charts, err)
	}
}

func TestSteamHelpDocumentsFeatures(t *testing.T) {
	help := (&Steam{}).Help()
	for _, want := range []string{"!steam <title>", "!steam info", "!steam genre", "!steam top", "no API key"} {
		if !strings.Contains(help, want) {
			t.Errorf("help %q does not contain %q", help, want)
		}
	}
}
