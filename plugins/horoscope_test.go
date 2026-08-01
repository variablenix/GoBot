package plugins

import "testing"

func TestHoroscopeSigns(t *testing.T) {
	if got := horoscopeSigns["aries"]; got != "Aries" {
		t.Fatalf("aries mapped to %q", got)
	}
	if _, ok := horoscopeSigns["not-a-sign"]; ok {
		t.Fatal("invalid sign was accepted")
	}
}
