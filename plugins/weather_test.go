package plugins

import "testing"

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
