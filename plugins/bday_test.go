package plugins

import (
	"testing"
	"time"
)

func TestValidBirthdayTable(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{"01-01", true}, {"02-29", true}, {"04-31", false}, {"02-30", false},
		{"13-01", false}, {"1-01", false}, {"01-1", false}, {"2024-01-01", false},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			if got := validBirthday(test.value); got != test.valid {
				t.Fatalf("validBirthday(%q) = %v, want %v", test.value, got, test.valid)
			}
		})
	}
}

func TestBirthdayNextDateIncludesToday(t *testing.T) {
	today := time.Now().UTC().Format("01-02")
	if !validBirthday(today) {
		t.Fatalf("current date %q should be valid", today)
	}
}
