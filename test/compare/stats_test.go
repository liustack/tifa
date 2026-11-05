package compare

import (
	"slices"
	"testing"
)

func TestMedianAndRange(t *testing.T) {
	if got := Median([]float64{3, 1, 2}); got != 2 {
		t.Errorf("median of 3 values = %v, want 2", got)
	}
	if got := Median([]float64{4, 1, 3, 2}); got != 2.5 {
		t.Errorf("median of 4 values = %v, want 2.5", got)
	}
	if got := Median(nil); got != 0 {
		t.Errorf("median of nothing = %v, want 0", got)
	}
	lo, hi := Range([]float64{5, 2, 9})
	if lo != 2 || hi != 9 {
		t.Errorf("range = %v to %v, want 2 to 9", lo, hi)
	}
}

func TestPercentileIsNearestRank(t *testing.T) {
	v := make([]float64, 100)
	for i := range v {
		v[i] = float64(i + 1)
	}
	for p, want := range map[float64]float64{50: 50, 99: 99, 100: 100, 1: 1} {
		if got := Percentile(v, p); got != want {
			t.Errorf("P%v = %v, want %v", p, got, want)
		}
	}
}

func TestRotateShiftsTheStartAndKeepsTheInput(t *testing.T) {
	in := []string{"a", "b", "c"}
	for round, want := range [][]string{{"a", "b", "c"}, {"b", "c", "a"}, {"c", "a", "b"}, {"a", "b", "c"}} {
		if got := Rotate(in, round); !slices.Equal(got, want) {
			t.Errorf("round %d = %v, want %v", round, got, want)
		}
	}
	if !slices.Equal(in, []string{"a", "b", "c"}) {
		t.Errorf("Rotate changed its input: %v", in)
	}
	if got := Rotate([]string(nil), 3); len(got) != 0 {
		t.Errorf("rotating nothing gave %v", got)
	}
}

// series builds latencies for pages that settle at 10, 20 ms, with the first slow requests.
func series(n, slow int, factor float64) ([]float64, []float64) {
	steady := []float64{10, 20}
	lat := make([]float64, n)
	for i := range lat {
		lat[i] = steady[i%2]
		if i < slow {
			lat[i] *= factor
		}
	}
	return lat, steady
}

func TestSettle(t *testing.T) {
	cases := []struct {
		name    string
		n, slow int
		factor  float64
		want    int
	}{
		{"fast from the start", 200, 0, 1, 1},
		// A window of 30 has a slow median once 15 of its requests are slow. With 50 slow
		// requests the window that starts at index 36 has 14, so it is the first to be within 10%.
		{"slow start", 200, 50, 5, 37},
		{"within 10% counts as settled", 200, 50, 1.09, 1},
		{"just over 10% does not", 200, 50, 1.2, 37},
		{"slow to the end never settles", 200, 200, 3, 0},
	}
	for _, c := range cases {
		lat, steady := series(c.n, c.slow, c.factor)
		if got := Settle(lat, steady); got != c.want {
			t.Errorf("%s: settles at %d, want %d", c.name, got, c.want)
		}
	}
}

func TestSettleNeedsEveryLaterWindowToStay(t *testing.T) {
	lat, steady := series(300, 0, 1)
	for i := 100; i < 140; i++ { // a stall in the middle
		lat[i] *= 4
	}
	// Windows from index 71 (29 requests before the stall plus one) to 139 hold a slow median or
	// not; the first window after all of the stall is the one at 125 (15 or fewer slow requests).
	got := Settle(lat, steady)
	if got <= 100 || got > 140 {
		t.Errorf("settles at %d, want a request inside or just after the stall", got)
	}
}

func TestSpreadOfIdenticalRuns(t *testing.T) {
	mk := func(req float64) Run { return Run{AA: true, Metrics: Metrics{Requests: req, P50: 10}} }
	runs := []Run{mk(100), mk(102), mk(98), mk(100), {Spec: "other", Metrics: Metrics{Requests: 1}}}
	got := AASpread(runs)
	if v := got["requests"]; v < 0.0399 || v > 0.0401 {
		t.Errorf("spread of requests = %v, want 0.04", v)
	}
	if v := got["p50"]; v != 0 {
		t.Errorf("spread of P50 = %v, want 0", v)
	}
	if got := AASpread([]Run{mk(100)}); len(got) != 0 {
		t.Errorf("one sample has no spread, got %v", got)
	}
}
