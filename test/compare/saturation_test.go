package compare

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// cellAt is the rounds of a cell: page views a second and server cores per round.
func cellAt(views []float64, cores float64, pass bool) []Round {
	var rs []Round
	for _, v := range views {
		rs = append(rs, Round{Seconds: 100, PageViews: int(100 * v), CPUSeconds: 100 * cores, Valid: true, Pass: pass, Why: map[bool]string{false: "click P99 1200 ms over 1000 ms"}[pass]})
	}
	return rs
}

// A server is stepped up only where it used under 0.95 of its cpus, with the cpus known and its
// CPU measured.
func TestMoreUsersWhereTheServerDidNotUseItsCores(t *testing.T) {
	at := func(cores ...float64) []Round {
		var rs []Round
		for _, c := range cores {
			rs = append(rs, Round{Seconds: 100, CPUSeconds: 100 * c, Valid: true, Pass: true})
		}
		return rs
	}
	for _, tc := range []struct {
		rounds []Round
		cpus   int
		want   bool
	}{
		{at(0.80, 0.85, 0.90), 1, true},
		{at(0.96, 0.97, 0.99), 1, false},
		{at(0.96, 0.90, 0.99), 1, false}, // the median used the core
		{at(1.5, 1.6, 1.7), 2, true}, {at(1.92, 1.95, 1.99), 2, false},
		{at(0.80), 0, false}, // not pinned: its cores are not known
		{at(0, 0), 1, false}, // no CPU measured
		{nil, 1, false},
	} {
		if got := needsMoreUsers(tc.rounds, tc.cpus); got != tc.want {
			t.Errorf("%v on %d cpus: %v", tc.rounds, tc.cpus, got)
		}
	}
}

// The users go up about 25% a step.
func TestTheProbeStepsUpAboutAQuarter(t *testing.T) {
	var got []int
	for u := 32; u <= 200; u = stepUp(u) {
		got = append(got, u)
	}
	if want := []int{32, 40, 50, 64, 80, 100, 125, 160, 200}; !reflect.DeepEqual(got, want) {
		t.Errorf("steps %v, want %v", got, want)
	}
	if stepUp(8) != 10 || stepUp(10) != 13 {
		t.Errorf("below 32: %d %d", stepUp(8), stepUp(10))
	}
}

// The probe stops at the first step that gains less than the spread of the rounds, fails the rule,
// or uses the cpus, and keeps the users of the best step that passed.
func TestTheProbeStopsWhereTheServerStopsGaining(t *testing.T) {
	base := cellAt([]float64{99, 100, 101}, 0.70, true)
	for _, tc := range []struct {
		name  string
		at    []Round
		cells map[int][]Round
		users int
		steps []int
		why   string
	}{
		{"used its core already", cellAt([]float64{99, 100, 101}, 0.97, true), nil, 32, nil, "used 0.97 of 1 cpus at 32 users"},
		{"failed at the base", cellAt([]float64{99, 100, 101}, 0.50, false), nil, 32, nil, "failed at 32 users: click P99"},
		{"the core at the second step", base, map[int][]Round{
			40: cellAt([]float64{110, 112, 114}, 0.85, true),
			50: cellAt([]float64{118, 120, 121}, 0.96, true),
		}, 50, []int{40, 50}, "used 0.96 of 1 cpus at 50 users"},
		{"less than the spread", base, map[int][]Round{
			40: cellAt([]float64{110, 112, 114}, 0.85, true),
			50: cellAt([]float64{113, 116, 118}, 0.90, true),
		}, 40, []int{40, 50}, "at 50 users the page views a second rose by less than the spread"},
		{"the rule failed", base, map[int][]Round{
			40: cellAt([]float64{110, 112, 114}, 0.85, false),
		}, 32, []int{40}, "failed at 40 users: click P99"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var played []int
			s, err := probe(32, 1, 3, tc.at, func(users int) ([]Round, error) {
				played = append(played, users)
				rs, ok := tc.cells[users]
				if !ok {
					t.Fatalf("played %d users", users)
				}
				return rs, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if s.Users != tc.users || !reflect.DeepEqual(s.Steps, tc.steps) || !reflect.DeepEqual(played, tc.steps) || !strings.HasPrefix(s.Why, tc.why) {
				t.Errorf("got %d users, steps %v, played %v, why %q, want %d, %v, %q", s.Users, s.Steps, played, s.Why, tc.users, tc.steps, tc.why)
			}
		})
	}
	// A server that never stops gaining stops at the cap.
	s, err := probe(32, 1, 3, base, func(users int) ([]Round, error) {
		return cellAt([]float64{float64(users) * 3, float64(users)*3 + 1, float64(users)*3 + 2}, 0.5, true), nil
	})
	if err != nil || s.Users != 640 || !strings.Contains(s.Why, "where the probe stops") {
		t.Errorf("at the cap: %+v %v", s, err)
	}
	// An error of a round is returned.
	boom := errors.New("boom")
	if _, err := probe(32, 1, 3, base, func(int) ([]Round, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Errorf("error %v", err)
	}
}

// The file keeps the users per scenario and server, with what measured them, and an entry written
// replaces only itself.
func TestTheSaturationFileKeepsEachEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "saturation.json")
	f, err := readSaturation(path)
	if err != nil || len(f) != 0 {
		t.Fatalf("a missing file: %v %v", f, err)
	}
	a := Saturated{Users: 50, Why: "used 0.96 of 1 cpus at 50 users", Steps: []int{40, 50}, Tifa: "abc1234", Date: "2026-11-08", ServerCPUs: 1, Probed: true}
	b := Saturated{Users: 32, Why: "used 0.99 of 1 cpus at 32 users", Tifa: "abc1234", Date: "2026-11-08", ServerCPUs: 1}
	for _, e := range []struct {
		sc, fw string
		s      Saturated
	}{{"office-w5", "tifa", a}, {"office-w5", "nextjs", b}, {"store-w5", "nextjs", b}, {"office-w5", "nextjs", a}} {
		if err := writeSaturation(path, e.sc, e.fw, e.s); err != nil {
			t.Fatal(err)
		}
	}
	f, err = readSaturation(path)
	if err != nil {
		t.Fatal(err)
	}
	a.Probed = false // the file holds what was measured, not by which run
	want := SaturationFile{"office-w5": {"tifa": a, "nextjs": a}, "store-w5": {"nextjs": b}}
	if !reflect.DeepEqual(f, want) {
		t.Errorf("file %+v, want %+v", f, want)
	}
}

// A server whose closed rounds at the users of every server were all invalid has nothing to probe
// from: the phase says so and goes on, and the file gets no entry for it.
func TestTheProbeSkipsACellWithNoValidRound(t *testing.T) {
	sc, _ := BrowseScenarioByName("store-w5")
	var log strings.Builder
	path := filepath.Join(t.TempDir(), "saturation.json")
	b := &browse{o: BrowseOptions{Log: &log, Rounds: 3, Users: 32, ServerCPUCount: 1, SaturationFile: path}, servers: map[string][]string{sc.Name: {"tifa-nopage"}}, res: &BrowseResult{}}
	for round := 1; round <= 3; round++ {
		bad := closedRound("tifa-nopage", sc.Name, 32, round, 4000, 59, false)
		bad.Valid, bad.WhyInvalid = false, "the round took 59 s, outside 60 to 120 s: resized"
		b.res.Closed = append(b.res.Closed, bad)
	}
	if err := b.saturation(context.Background(), sc); err != nil {
		t.Fatalf("the saturation phase stopped: %v", err)
	}
	if f, err := readSaturation(path); err != nil || len(f[sc.Name]) != 0 {
		t.Errorf("the file has %v (%v), want no entry", f, err)
	}
	if !strings.Contains(log.String(), "no valid round at 32 users") {
		t.Errorf("the log does not say why it was not probed:\n%s", log.String())
	}
}
