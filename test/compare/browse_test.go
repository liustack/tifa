package compare

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A result is continued only under the settings it was started with: a probe asked for again, or
// other server cpus, must not reuse the rounds of another run, nor a probe the rate of a run without one.
func TestAResultIsContinuedOnlyUnderItsSettings(t *testing.T) {
	o := BrowseOptions{Sessions: 8, Rounds: 3, Warm: 15 * time.Second, Target: 75 * time.Second, ServerCPUs: "0", ServerCPUCount: 1}
	res := &BrowseResult{Config: browseConfig(o)}
	if err := canContinue(res, o); err != nil {
		t.Fatalf("the same settings: %v", err)
	}
	probe := o
	probe.Probe = true
	cpus := o
	cpus.ServerCPUs, cpus.ServerCPUCount = "0,2", 2
	target := o
	target.Target = 30 * time.Second
	for name, other := range map[string]BrowseOptions{"-probe": probe, "-server-cpus": cpus, "-target": target} {
		err := canContinue(res, other)
		if err == nil || !strings.Contains(err.Error(), "remove it to start again") {
			t.Errorf("%s: continued a result of other settings: %v", name, err)
		}
	}
}

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
	sc, _ := BrowseScenarioByName("store-w5")
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

// A closed round outside the length window resizes its cell from what it did, a round stopped at
// twice the target too: it played only part of its blocks, and the next round must not get the same
// blocks again.
func TestARoundOutsideTheWindowResizesItsCell(t *testing.T) {
	target := 75 * time.Second
	for _, tc := range []struct {
		name string
		out  Round
		want bool
	}{
		{"stopped at twice the target", Round{Seconds: 150, Visits: 3555, TimedOut: true}, true},
		{"too short", Round{Seconds: 40, Visits: 3800}, true},
		{"in the window", Round{Seconds: 90, Visits: 3800}, false},
		{"nothing ended", Round{Seconds: 150, TimedOut: true}, false},
	} {
		tc.out.Pass, tc.out.Valid = true, true
		blocks := calibrate(&tc.out, target, 4)
		if (blocks > 0) != tc.want || tc.out.Valid == tc.want {
			t.Errorf("%s: %d blocks, valid %v", tc.name, blocks, tc.out.Valid)
		}
		if want := BlocksFor(float64(tc.out.Visits)/tc.out.Seconds, target, 4); tc.want && blocks != want {
			t.Errorf("%s: %d blocks, want %d", tc.name, blocks, want)
		}
	}
}

// A cell with no valid round says why its rounds failed, not only how many there were.
func TestACellWithNoValidRoundSaysWhyItsRoundsFailed(t *testing.T) {
	sc, _ := BrowseScenarioByName("store-w5")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Version: map[string]string{}, Sustain: map[string]*Sustain{}}
	for round := 1; round <= 3; round++ {
		r.Closed = append(r.Closed, closedRound("nextjs", sc.Name, 32, round, 400, 10, true))
		bad := closedRound("tifa", sc.Name, 32, 1, 4000, 59, false)
		bad.Valid, bad.Attempt = false, round
		bad.Why, bad.WhyInvalid = "630 failures (GET /zh-CN/store/cart: status 503)", "the round took 59 s, outside 60 to 120 s: resized"
		r.Closed = append(r.Closed, bad)
	}
	md := r.Markdown()
	if want := "failed (no valid round of 3: 630 failures (GET /zh-CN/store/cart: status 503); the round took 59 s, outside 60 to 120 s: resized) |"; !strings.Contains(md, want) {
		t.Errorf("the report lacks %q:\n%s", want, md)
	}
}

// A result of the old format, which counted users and had no planned visits, is refused with a clear
// error, by the report and by a run that would continue it: there is no reading of it.
func TestAResultOfTheOldFormatIsRefused(t *testing.T) {
	cur := &BrowseResult{Config: browseConfig(BrowseOptions{Sessions: 8}), Closed: []Round{{VisitsPlanned: 4}}, Sustain: map[string]*Sustain{}}
	data, err := cur.json()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBrowseResult(data); err != nil {
		t.Fatalf("the current format: %v", err)
	}
	for name, old := range map[string]string{
		"users in the settings": `{"settings":{"users":"32","rounds":"3"},"closed":[],"sustain":{}}`,
		"users in a round":      `{"settings":{"sessions":"8"},"closed":[{"users":32,"visits":10}],"sustain":{}}`,
		"a saturation file":     `{"settings":{"sessions":"8"},"closed":[],"sustain":{},"saturated":{}}`,
	} {
		if _, err := ReadBrowseResult([]byte(old)); err == nil || !strings.Contains(err.Error(), "older version") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
