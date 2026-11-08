package compare

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// The search of the sustainable rate starts within its cap, so a cap under 4 visits a second still
// measures, and halves below that.
func TestTheRateSearchStartsWithinTheCap(t *testing.T) {
	for _, tc := range []struct {
		max    float64
		passes float64 // the highest rate that passes
		tried  []float64
		lo, hi float64
	}{
		{2, 1, []float64{2, 1}, 1, 2},
		{3, 100, []float64{3}, 3, 0},
		{16384, 40, []float64{4, 8, 16, 32, 64}, 32, 64},
		{16384, 1, []float64{4, 2, 1}, 1, 2},
	} {
		var tried []float64
		lo, hi, err := searchRate(tc.max, func(rate float64) (bool, error) {
			tried = append(tried, rate)
			return rate <= tc.passes, nil
		})
		if err != nil || lo != tc.lo || hi != tc.hi || !reflect.DeepEqual(tried, tc.tried) {
			t.Errorf("cap %v, passing up to %v: lo %v hi %v tried %v (%v), want %v %v %v", tc.max, tc.passes, lo, hi, tried, err, tc.lo, tc.hi, tc.tried)
		}
	}
	if _, err := (Env{}).Browse(context.Background(), BrowseOptions{MaxRate: 0.25}); err == nil || !strings.Contains(err.Error(), "-max-rate") {
		t.Errorf("a cap under half a visit a second: %v", err)
	}
}
