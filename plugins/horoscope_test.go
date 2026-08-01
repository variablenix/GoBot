package plugins

import (
	"strings"
	"testing"
)

func TestHoroscopeSigns(t *testing.T) {
	if got := horoscopeSigns["aries"]; got != "Aries" {
		t.Fatalf("aries mapped to %q", got)
	}
	if _, ok := horoscopeSigns["not-a-sign"]; ok {
		t.Fatal("invalid sign was accepted")
	}
}

func TestHoroscopeSourceURL(t *testing.T) {
	want := "https://astrology.com.au/horoscopes/daily-horoscopes/virgo"
	if got := horoscopeSourceURL("Virgo"); got != want {
		t.Fatalf("source URL = %q, want %q", got, want)
	}
}

func TestHoroscopeSummaryLengthDefaultsAndBounds(t *testing.T) {
	plugin := &Horoscope{}
	if err := plugin.Init(nil, nil); err != nil {
		t.Fatalf("default Init returned error: %v", err)
	}
	if plugin.maxSummaryLength != defaultHoroscopeSummaryLength {
		t.Fatalf("default summary length = %d, want %d", plugin.maxSummaryLength, defaultHoroscopeSummaryLength)
	}

	plugin = &Horoscope{}
	if err := plugin.Init(map[string]interface{}{"max_summary_length": 80}, nil); err != nil {
		t.Fatalf("lower-bound Init returned error: %v", err)
	}
	if plugin.maxSummaryLength != 120 {
		t.Fatalf("lower-bound summary length = %d, want 120", plugin.maxSummaryLength)
	}

	plugin = &Horoscope{}
	if err := plugin.Init(map[string]interface{}{"max_summary_length": 500}, nil); err != nil {
		t.Fatalf("upper-bound Init returned error: %v", err)
	}
	if plugin.maxSummaryLength != 260 {
		t.Fatalf("upper-bound summary length = %d, want 260", plugin.maxSummaryLength)
	}
}

func TestHoroscopeSourceLinkIsReadableAndOneLine(t *testing.T) {
	message := "Virgo: You are focused and practical today. Read more: " + horoscopeSourceURL("virgo")
	if strings.ContainsAny(message, "\r\n") {
		t.Fatal("horoscope message contains a line break")
	}
	if !strings.Contains(message, "Read more: https://astrology.com.au/horoscopes/daily-horoscopes/virgo") {
		t.Fatal("horoscope message is missing its source link")
	}
}
