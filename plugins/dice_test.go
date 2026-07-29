package plugins

import "testing"

func TestParseDice(t *testing.T) {
	for _, x := range []struct {
		s    string
		n, d int
	}{{"2d6", 2, 6}, {"d20", 1, 20}, {"20", 1, 20}} {
		n, d, e := ParseDice(x.s)
		if e != nil || n != x.n || d != x.d {
			t.Errorf("%s => %d,%d,%v", x.s, n, d, e)
		}
	}
	for _, s := range []string{"bad", "101d6", "2d10001", "0d6"} {
		if _, _, e := ParseDice(s); e == nil {
			t.Errorf("expected error for %s", s)
		}
	}
}
