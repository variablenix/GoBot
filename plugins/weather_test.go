package plugins

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

func TestWeatherDescription(t *testing.T) {
	if got := weatherDescription(0); got != "clear sky" {
		t.Fatalf("got %q", got)
	}
	if got := weatherDescription(63); got != "rain" {
		t.Fatalf("got %q", got)
	}
}

func TestCompassDirection(t *testing.T) {
	for degrees, want := range map[float64]string{0: "N", 90: "E", 180: "S", 270: "W"} {
		if got := compassDirection(degrees); got != want {
			t.Errorf("%.0f degrees: got %q, want %q", degrees, got, want)
		}
	}
}

func TestWeatherCommandAliases(t *testing.T) {
	for _, command := range []string{"weather", "wx", "forecast", "temp", "WX"} {
		if !isWeatherCommand(command) {
			t.Errorf("expected %q to be a weather command", command)
		}
	}
	if isWeatherCommand("weatherize") {
		t.Error("unexpectedly accepted invalid weather command")
	}
}

func TestParseWeatherRequest(t *testing.T) {
	tests := []struct {
		arg   string
		want  weatherRequest
		valid bool
	}{
		{arg: "", want: weatherRequest{}, valid: true},
		{arg: "Las Vegas", want: weatherRequest{location: "Las Vegas"}, valid: true},
		{arg: "'Las Vegas'", want: weatherRequest{location: "Las Vegas"}, valid: true},
		{arg: "set Las Vegas", want: weatherRequest{location: "Las Vegas", setDefault: true}, valid: true},
		{arg: "set 'Las Vegas'", want: weatherRequest{location: "Las Vegas", setDefault: true}, valid: true},
		{arg: "default \"Las Vegas\"", want: weatherRequest{location: "Las Vegas", setDefault: true}, valid: true},
		{arg: "clear", want: weatherRequest{clear: true}, valid: true},
		{arg: "unset now", want: weatherRequest{}, valid: false},
		{arg: "set", want: weatherRequest{}, valid: false},
	}
	for _, test := range tests {
		got, valid := parseWeatherRequest(test.arg)
		if got != test.want || valid != test.valid {
			t.Errorf("parseWeatherRequest(%q) = %+v, %v; want %+v, %v", test.arg, got, valid, test.want, test.valid)
		}
	}
}

func TestWeatherDefaultsPersistByIdentity(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "weather.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	plugin := &Weather{}
	if err := plugin.Init(nil, db); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if err := plugin.saveWeatherDefault("account:ak", "Las Vegas"); err != nil {
		t.Fatalf("saveWeatherDefault returned error: %v", err)
	}
	if got := plugin.savedWeatherDefault("account:ak"); got != "Las Vegas" {
		t.Fatalf("savedWeatherDefault = %q, want Las Vegas", got)
	}

	reloaded := &Weather{}
	if err := reloaded.Init(bot.PluginConfig{}, db); err != nil {
		t.Fatalf("reloaded Init returned error: %v", err)
	}
	if got := reloaded.savedWeatherDefault("account:ak"); got != "Las Vegas" {
		t.Fatalf("persisted default = %q, want Las Vegas", got)
	}
	if got := reloaded.savedWeatherDefault("account:other"); got != "" {
		t.Fatalf("other user's default = %q, want empty", got)
	}
	if err := reloaded.clearWeatherDefault("account:ak"); err != nil {
		t.Fatalf("clearWeatherDefault returned error: %v", err)
	}
	if got := reloaded.savedWeatherDefault("account:ak"); got != "" {
		t.Fatalf("cleared default = %q, want empty", got)
	}
}

func TestWeatherHelpDocumentsDefaults(t *testing.T) {
	help := (&Weather{}).Help()
	for _, want := range []string{"!weather [city]", "!weather set <city>", "!weather clear", "!wx"} {
		if !strings.Contains(help, want) {
			t.Errorf("weather help %q does not contain %q", help, want)
		}
	}
}
