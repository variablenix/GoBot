package plugins

import "testing"

func TestCoinflipSecureResultsAreBothReachable(t *testing.T) {
	seen := map[int64]bool{}
	for i := 0; i < 100; i++ {
		value, err := secureRandomInt(2)
		if err != nil {
			t.Fatal(err)
		}
		seen[value] = true
	}
	if len(seen) != 2 {
		t.Fatalf("secure coin selection produced %v; expected both outcomes in 100 trials", seen)
	}
}
