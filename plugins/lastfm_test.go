package plugins

import "testing"

func TestValidLastFMUser(t *testing.T) {
	if !validLastFMUser("music_fan") {
		t.Fatal("valid username rejected")
	}
	for _, user := range []string{"", "two words", "bad\nname"} {
		if validLastFMUser(user) {
			t.Errorf("invalid username accepted: %q", user)
		}
	}
}
