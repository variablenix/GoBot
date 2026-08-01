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
	if plugin.maxSummaryLength != 400 {
		t.Fatalf("upper-bound summary length = %d, want 400", plugin.maxSummaryLength)
	}
}

func TestHoroscopeSourceLinkIsReadableAndOneLine(t *testing.T) {
	short := formatHoroscopeReply("Virgo", "virgo", "You are focused and practical today. Progress is available.", 360)
	if strings.Contains(short, "Read more:") || !strings.Contains(short, "Progress is available.") {
		t.Fatalf("short horoscope was not preserved without an unnecessary link: %q", short)
	}

	long := formatHoroscopeReply("Virgo", "virgo", strings.Repeat("A useful horoscope sentence. ", 20), 120)
	if strings.ContainsAny(long, "\r\n") {
		t.Fatal("horoscope message contains a line break")
	}
	if !strings.Contains(long, "Read more: https://astrology.com.au/horoscopes/daily-horoscopes/virgo") {
		t.Fatalf("long horoscope is missing its source link: %q", long)
	}
	if len([]byte(long)) > maxHoroscopeMessageBytes {
		t.Fatalf("horoscope reply is %d bytes, want at most %d", len([]byte(long)), maxHoroscopeMessageBytes)
	}

	unicode := formatHoroscopeReply("Virgo", "virgo", strings.Repeat("é ", 300), 360)
	if len([]byte(unicode)) > maxHoroscopeMessageBytes {
		t.Fatalf("unicode horoscope reply is %d bytes, want at most %d", len([]byte(unicode)), maxHoroscopeMessageBytes)
	}
}
