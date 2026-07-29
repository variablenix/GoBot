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
