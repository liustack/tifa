package seed

import (
	"errors"
	"testing"

	"github.com/liustack/tifa"
)

func TestSizeTakesWholeNumbersInRange(t *testing.T) {
	for _, tc := range []struct {
		param string
		want  int
		ok    bool
	}{{"500", 500, true}, {"1", 1, true}, {"1000", 1000, true}, {"0", 0, false}, {"1001", 0, false}, {"05", 0, false}, {"+5", 0, false}, {"5k", 0, false}, {"", 0, false}} {
		n, err := Size(tc.param, 1, 1000)
		if tc.ok && (err != nil || n != tc.want) {
			t.Errorf("Size(%q) = %d, %v, want %d", tc.param, n, err, tc.want)
		}
		if !tc.ok && !errors.Is(err, tifa.ErrNotFound) {
			t.Errorf("Size(%q) = %d, %v, want ErrNotFound", tc.param, n, err)
		}
	}
}

func TestNewIsTheSameForTheSameScenarioAndSize(t *testing.T) {
	a, b, c := New("dom", 10), New("dom", 10), New("dom", 11)
	x, y, z := a.Uint64(), b.Uint64(), c.Uint64()
	if x != y || x == z {
		t.Errorf("New gave %d, %d and %d", x, y, z)
	}
}
