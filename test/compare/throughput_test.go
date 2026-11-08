package compare

import (
	"errors"
	"os"
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

// The sessions go up about 25% a step from 8, and down to 6 and 4 from 8.
func TestTheSessionsStepAboutAQuarter(t *testing.T) {
	var got []int
	for s := 8; s <= 40; s = stepUp(s) {
		got = append(got, s)
	}
	if want := []int{8, 10, 13, 16, 20, 25, 32, 40}; !reflect.DeepEqual(got, want) {
		t.Errorf("steps up %v, want %v", got, want)
	}
	if got := stepsDown(8); !reflect.DeepEqual(got, []int{6, 4}) {
		t.Errorf("steps down %v", got)
	}
}

// A server is stepped up only where it used under 0.95 of its cpus, with the cpus known and its
// CPU measured.
func TestCoresAreFreeOnlyWhenMeasuredUnderTheLimit(t *testing.T) {
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
		want   string
	}{
		{at(0.80, 0.85, 0.90), 1, "free"},
		{at(0.96, 0.97, 0.99), 1, "used"},
		{at(0.96, 0.90, 0.99), 1, "used"}, // the median used the core
		{at(1.5, 1.6, 1.7), 2, "free"}, {at(1.92, 1.95, 1.99), 2, "used"},
		{at(0.80), 0, "unknown"}, // not pinned: its cores are not known
		{at(0, 0), 1, "unknown"}, // no CPU measured
	} {
		if got := coreUse(tc.rounds, tc.cpus); got != tc.want {
			t.Errorf("%v on %d cpus: %s, want %s", tc.rounds, tc.cpus, got, tc.want)
		}
	}
}

// The throughput of a server is typed by how its steps ended: its core used with every round
// passing, a plateau with the core not full, the highest step that passed when the next failed, a
// step down from 8 to 6 and 4 when 8 failed, and no result when no round was valid or the cores are
// not known. The sessions it reports are always those of a cell whose rounds all passed.
func TestTheThroughputResultIsTypedByHowTheStepsEnded(t *testing.T) {
	ok := func(cores float64, views ...float64) []Round { return cellAt(views, cores, true) }
	bad := cellAt([]float64{90}, 0.8, false)
	base := ok(0.70, 99, 100, 101)
	for _, tc := range []struct {
		name     string
		cpus     int
		cells    map[int][]Round
		result   string
		sessions int
		steps    []int
		why      string
	}{
		{"its core at 8", 1, map[int][]Round{8: ok(0.97, 99, 100, 101)}, ThroughputCoreUsed, 8, []int{8}, "used 0.97 of 1 cpus at 8 sessions"},
		{"its core at 13", 1, map[int][]Round{8: base, 10: ok(0.85, 110, 112, 114), 13: ok(0.96, 118, 120, 121)}, ThroughputCoreUsed, 13, []int{8, 10, 13}, "used 0.96 of 1 cpus at 13 sessions"},
		{"a plateau", 1, map[int][]Round{8: base, 10: ok(0.75, 100, 101, 103)}, ThroughputPlateau, 8, []int{8, 10}, "at 10 sessions the page views a second rose by less than the spread of the rounds"},
		{"the next step failed", 1, map[int][]Round{8: base, 10: ok(0.85, 110, 112, 114), 13: bad}, ThroughputHighestPassing, 10, []int{8, 10, 13}, "failed at 13 sessions: click P99"},
		{"8 failed, 6 passed", 1, map[int][]Round{8: bad, 6: ok(0.9, 70, 71, 72)}, ThroughputHighestPassing, 6, []int{8, 6}, "failed at 8 sessions: click P99"},
		{"8 and 6 failed, 4 passed", 1, map[int][]Round{8: bad, 6: bad, 4: ok(0.9, 50, 51, 52)}, ThroughputHighestPassing, 4, []int{8, 6, 4}, "failed at 8 and 6 sessions: click P99"},
		{"every step down failed", 1, map[int][]Round{8: bad, 6: bad, 4: bad}, ThroughputNone, 0, []int{8, 6, 4}, "failed at 8, 6 and 4 sessions: click P99"},
		// A step with too few valid rounds, none of them failed, is not decided: it is not a failure, so
		// nothing steps down from it, and the step below it stands when there is one.
		{"no valid round", 1, map[int][]Round{8: nil}, ThroughputUndecided, 0, []int{8}, "8 sessions could not be decided: 0 of 3 rounds were valid"},
		{"two valid rounds at 8", 1, map[int][]Round{8: ok(0.7, 99, 100)}, ThroughputUndecided, 0, []int{8}, "8 sessions could not be decided: 2 of 3 rounds were valid"},
		{"8 failed, 6 not decided", 1, map[int][]Round{8: bad, 6: nil}, ThroughputUndecided, 0, []int{8, 6}, "failed at 8 sessions: click P99 1200 ms over 1000 ms; 6 sessions could not be decided"},
		{"the next step not decided", 1, map[int][]Round{8: base, 10: ok(0.8, 120)}, ThroughputNextUndecided, 8, []int{8, 10}, "10 sessions could not be decided: 1 of 3 rounds were valid"},
		// The cores are decided before the plateau: a step that passed with its core used is the result,
		// even with a gain over the step below that the spread of the rounds hides.
		{"the core used with no clear gain", 1, map[int][]Round{8: ok(0.90, 99, 100, 101), 10: ok(0.96, 100, 101, 103)}, ThroughputCoreUsed, 10, []int{8, 10}, "used 0.96 of 1 cpus at 10 sessions"},
		{"the cores lost after a step", 1, map[int][]Round{8: base, 10: ok(0, 110, 112, 114)}, ThroughputCoresUnknown, 10, []int{8, 10}, "the cores of the server were not known at 10 sessions"},
		{"cores not known", 0, map[int][]Round{8: base}, ThroughputCoresUnknown, 8, []int{8}, "the cores of the server are not known"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var played []int
			got, err := stepSessions(8, tc.cpus, 3, func(sessions int) ([]Round, error) {
				played = append(played, sessions)
				rs, ok := tc.cells[sessions]
				if !ok {
					t.Fatalf("played %d sessions", sessions)
				}
				return rs, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if got.Result != tc.result || got.Sessions != tc.sessions || !reflect.DeepEqual(got.Steps, tc.steps) || !reflect.DeepEqual(played, tc.steps) || !strings.HasPrefix(got.Why, tc.why) {
				t.Errorf("got %s at %d sessions, steps %v, played %v, why %q, want %s at %d, %v, %q", got.Result, got.Sessions, got.Steps, played, got.Why, tc.result, tc.sessions, tc.steps, tc.why)
			}
		})
	}
	// A server that never stops gaining stops at the cap, with the sessions of the last step.
	got, err := stepSessions(8, 1, 3, func(s int) ([]Round, error) {
		return ok(0.5, float64(s)*3, float64(s)*3+1, float64(s)*3+2), nil
	})
	if err != nil || got.Result != ThroughputCap || got.Sessions != maxSessions || !strings.Contains(got.Why, "where the steps stop") {
		t.Errorf("at the cap: %+v %v", got, err)
	}
	// An error of a round is returned.
	boom := errors.New("boom")
	if _, err := stepSessions(8, 1, 3, func(int) ([]Round, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Errorf("error %v", err)
	}
}

// A cell fails on its first failed round, whatever the count of rounds: the rounds after it are
// not needed for the verdict. A cell with too few valid rounds and none failed is not decided.
func TestACellFailsOnItsFirstFailedRound(t *testing.T) {
	if got, why := stepOf(cellAt([]float64{90}, 0.8, false), 3); got != trialFail || !strings.Contains(why, "click P99") {
		t.Errorf("one failed round of three: %s %q", got, why)
	}
	if got, why := stepOf(cellAt([]float64{90, 91}, 0.8, true), 3); got != stepUndecided || why != "2 of 3 rounds were valid" {
		t.Errorf("two passing rounds of three: %s %q", got, why)
	}
	if got, _ := stepOf(cellAt([]float64{90, 91, 92}, 0.8, true), 3); got != trialPass {
		t.Errorf("three passing rounds: %s", got)
	}
}

// The code that decides a verdict lives in the files of the method, so that a change to it is a
// change of the method: none of it is in the report.
func TestTheDecisionsAreInTheFilesOfTheMethod(t *testing.T) {
	data, err := os.ReadFile("report_browse.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"func stepOf(", "func pageViewsPerSecond(", "func each(", "func (r *BrowseResult) cellRounds("} {
		if strings.Contains(string(data), f) {
			t.Errorf("report_browse.go holds %s", f)
		}
	}
}
