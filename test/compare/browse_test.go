package compare

import (
	"context"
	"io"
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

// A scenario where Next.js has no sustainable rate in range has no open run: that is its result, and
// the latency phase goes on with the other scenarios.
func TestNoSustainableRateSkipsTheOpenRunOfThatScenarioOnly(t *testing.T) {
	sc, _ := BrowseScenarioByName("office-w5")
	b := &browse{o: BrowseOptions{Log: io.Discard, Rounds: 3}, servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, res: &BrowseResult{
		Sustain:     map[string]*Sustain{sustainKey(sc.Name, openBase): {Done: true, Note: "no rate passed its confirmation rounds"}},
		LatencyRate: map[string]float64{}, LatencyBase: map[string]string{},
	}}
	if err := b.latency(context.Background(), sc); err != nil {
		t.Fatalf("the latency phase stopped: %v", err)
	}
	if len(b.res.Latency) != 0 {
		t.Fatalf("open rounds were run: %d", len(b.res.Latency))
	}
	r := b.res
	r.Scenarios, r.Servers, r.Version = []BrowseScenario{sc}, b.servers, map[string]string{}
	if md := r.Markdown(); !strings.Contains(md, "No open run: Next.js has no sustainable rate in range on this scenario (no rate passed its confirmation rounds)") {
		t.Errorf("the report does not say why there is no open run:\n%s", md)
	}
	// Without a search done it is still an error: the sustain phase did not run.
	b.res.Sustain = map[string]*Sustain{}
	if err := b.latency(context.Background(), sc); err == nil {
		t.Error("no search and no error")
	}
}
