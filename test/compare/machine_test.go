package compare

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"
)

// checkBrowse is a continued run of the shop whose result keeps the three closed rounds of each
// main server at 8 sessions, start at views a second, the others at 300, and one of a reference.
// play stands for a round of the check: it returns the page views a second of each try of each
// round, and whether it is valid.
func checkBrowse(views []float64, play func(fw string, round, try int) (float64, bool)) (*browse, *[]string) {
	sc := mustScenario("shop")
	var played []string
	b := &browse{o: BrowseOptions{Log: io.Discard, Sessions: 8, Rounds: 3, Scenarios: []BrowseScenario{sc}},
		servers: map[string][]string{sc.Name: {"tifa", "nextjs", "start", "tifa-nocache"}},
		res: &BrowseResult{ServerStamps: map[string]ServerStamp{
			"tifa": {Stamp: "t1", Trace: "a", Date: "2026-11-15"}, "nextjs": {Stamp: "n1", Trace: "b", Date: "2026-11-15"},
			"start": {Stamp: "s1", Trace: "c", Date: "2026-11-15"}, "tifa-nocache": {Stamp: "t1", Trace: "a", Date: "2026-11-15"}}}}
	for round := 1; round <= 3; round++ {
		b.res.Closed = append(b.res.Closed, closedRound("start", sc.Name, 8, round, int(100*views[round-1]), 100, true),
			closedRound("tifa", sc.Name, 8, round, 30000, 100, true), closedRound("nextjs", sc.Name, 8, round, 30000, 100, true))
	}
	b.res.Closed = append(b.res.Closed, closedRound("tifa-nocache", sc.Name, 8, 1, 30000, 100, true))
	b.playCheck = func(fw string, sc BrowseScenario, round, try int) (*Round, error) {
		played = append(played, fmt.Sprintf("%s round %d try %d", fw, round, try))
		v, valid := play(fw, round, try)
		x := closedRound(fw, sc.Name, 8, round, int(100*v), 100, true)
		x.Valid, x.Attempt = valid, try
		if !valid {
			x.WhyInvalid = "the load generator used 1.80 cores of 2"
		}
		return &x, nil
	}
	return b, &played
}

// rounds plays the given page views a second in the rounds of a check, every try valid.
func rounds(views ...float64) func(string, int, int) (float64, bool) {
	return func(_ string, round, _ int) (float64, bool) { return views[round-1], true }
}

// A continued run that measures a server again plays, before anything else, as many closed rounds of
// TanStack Start, which it keeps, as its cell has. The median of their page views a second within 3%
// of the median of the kept rounds passes: the check is kept in the result with the values of its
// rounds, both medians, the difference and the stamps it passed for, and its rounds are no result.
// The rounds of a cell differ by 7 to 8% on a machine that did not change: one round far out does not
// trip it.
func TestTheMachineCheckPassesWhenTheMediansAgree(t *testing.T) {
	for _, tc := range []struct {
		check []float64
		diff  float64
	}{
		{[]float64{81.9, 76.3, 76.5}, -2.9}, // the medians 76.5 against 78.8
		{[]float64{88, 81, 70}, 2.8},
		{[]float64{78.8, 78.8, 78.8}, 0},
	} {
		b, played := checkBrowse([]float64{81.9, 78.8, 76.5}, rounds(tc.check...))
		closed := len(b.res.Closed)
		if err := b.checkMachine(t.Context(), []string{"nextjs"}); err != nil {
			t.Fatalf("%v: %v", tc.check, err)
		}
		if want := []string{"start round 1 try 1", "start round 2 try 1", "start round 3 try 1"}; !slices.Equal(*played, want) || len(b.res.Closed) != closed {
			t.Errorf("%v: played %v, %d closed rounds kept of %d", tc.check, *played, len(b.res.Closed), closed)
		}
		if len(b.res.MachineChecks) != 1 {
			t.Fatalf("%d checks kept", len(b.res.MachineChecks))
		}
		c := b.res.MachineChecks[0]
		if c.Server != "start" || c.Scenario != "shop" || c.Sessions != 8 || !slices.Equal(c.Values, tc.check) || c.Median != Median(tc.check) || c.KeptMedian != 78.8 ||
			fmt.Sprintf("%.1f", c.Diff) != fmt.Sprintf("%.1f", tc.diff) || c.Date == "" || !maps.Equal(c.Stamps, checkStamps(b.res.ServerStamps)) {
			t.Errorf("%v: the check kept %+v", tc.check, c)
		}
	}
}

// A median of the check more than 3% off the median of the kept rounds stops the run before it
// measures anything: the rounds it keeps were measured on a machine that is faster or slower now. The
// message gives both medians, and nothing is kept of the check.
func TestTheMachineCheckStopsARunOnAMachineThatChanged(t *testing.T) {
	for _, check := range [][]float64{{76.4, 70, 90}, {81.2, 81.2, 60}} {
		b, _ := checkBrowse([]float64{81.9, 78.8, 76.5}, rounds(check...))
		err := b.checkMachine(t.Context(), []string{"nextjs"})
		if err == nil || !strings.Contains(err.Error(), "faster or slower") || !strings.Contains(err.Error(), "must start again") ||
			!strings.Contains(err.Error(), fmt.Sprintf("median of %.1f page views a second", Median(check))) || !strings.Contains(err.Error(), "median of the 3 kept rounds is 78.8") {
			t.Errorf("%v: %v", check, err)
		}
		if len(b.res.MachineChecks) != 0 {
			t.Errorf("%v: a check was kept", check)
		}
	}
}

// A check that passed is not played again when the run is continued with the same stamps, and is
// played again when a stamp of a server changed since.
func TestTheMachineCheckIsNotPlayedAgainForTheSameStamps(t *testing.T) {
	b, played := checkBrowse([]float64{100, 104, 102}, rounds(101, 101, 101))
	if err := b.checkMachine(t.Context(), []string{"nextjs"}); err != nil {
		t.Fatal(err)
	}
	b.res.ServerStamps["nextjs"] = ServerStamp{Stamp: "n1", Trace: "b", Date: "2026-11-16"} // the same stamps, measured again later
	if err := b.checkMachine(t.Context(), []string{"nextjs"}); err != nil || len(*played) != 3 || len(b.res.MachineChecks) != 1 {
		t.Errorf("the same stamps: played %v, %d checks (%v)", *played, len(b.res.MachineChecks), err)
	}
	b.res.ServerStamps["nextjs"] = ServerStamp{Stamp: "n2", Trace: "b", Date: "2026-11-16"}
	if err := b.checkMachine(t.Context(), []string{"nextjs"}); err != nil || len(*played) != 6 || len(b.res.MachineChecks) != 2 {
		t.Errorf("another stamp: played %v, %d checks (%v)", *played, len(b.res.MachineChecks), err)
	}
}

// A round of the check that is not valid is played again, as a round of the cell is, up to three
// tries, and only its valid try counts. A round that is never valid stops the run.
func TestAnInvalidMachineCheckRoundIsPlayedAgain(t *testing.T) {
	b, played := checkBrowse([]float64{100, 104, 102}, func(_ string, round, try int) (float64, bool) {
		if round == 2 && try == 1 {
			return 300, false
		}
		return 101, true
	})
	if err := b.checkMachine(t.Context(), []string{"nextjs"}); err != nil || len(*played) != 4 || (*played)[2] != "start round 2 try 2" {
		t.Errorf("round 2 valid at the second try: played %v (%v)", *played, err)
	} else if c := b.res.MachineChecks[0]; !slices.Equal(c.Values, []float64{101, 101, 101}) {
		t.Errorf("the check kept the values %v", c.Values)
	}
	b, played = checkBrowse([]float64{100, 104, 102}, func(string, int, int) (float64, bool) { return 101, false })
	if err := b.checkMachine(t.Context(), []string{"nextjs"}); err == nil || !strings.Contains(err.Error(), "valid in 3 tries") || len(*played) != 3 {
		t.Errorf("never valid: played %v (%v)", *played, err)
	}
}

// The check runs only when a server is measured again and a main server with complete closed rounds
// at the base sessions is kept: TanStack Start when it is, another main server when it is not. A
// reference is never the server of the check.
func TestTheMachineCheckPlaysAKeptMainServer(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stale []string
		drop  func(r *BrowseResult)
		want  string // the server of the check, "" for none
	}{
		{"nothing measured again", nil, nil, ""},
		{"Start kept", []string{"nextjs"}, nil, "start"},
		{"Start measured again", []string{"start"}, nil, "tifa"},
		{"Start with two rounds", []string{"nextjs"}, func(r *BrowseResult) { r.Closed = r.Closed[1:] }, "tifa"},
		{"the references alone kept", []string{"tifa", "nextjs", "start"}, nil, ""},
	} {
		b, played := checkBrowse([]float64{300, 300, 300}, rounds(300, 300, 300))
		if tc.drop != nil {
			tc.drop(b.res)
		}
		if err := b.checkMachine(t.Context(), tc.stale); err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
		got := ""
		if len(*played) > 0 {
			got, _, _ = strings.Cut((*played)[0], " ")
		}
		if got != tc.want {
			t.Errorf("%s: the check played %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The rounds of the check are closed rounds of the cell as they are played, each on a server of its
// own after the sizing it keeps, the setup and the warm-up, as many as the cell has, and none of them
// is kept: no closed round, no new size of the cell. Whether the fake servers of the test agree within
// 3% with the kept round is up to the machine, so the test takes either outcome.
func TestTheMachineCheckRoundsAreClosedRoundsOfTheCellAndNoResult(t *testing.T) {
	tifa, _, node := warmSpecs()
	sc := mustScenario("shop")
	b, servers := warmBrowse(t, sc, tifa, node)
	b.o.Rounds = 2
	for _, fw := range []string{"tifa", "nextjs"} {
		for round := 1; round <= 2; round++ {
			if _, err := b.closedRound(context.Background(), fw, sc, 2, round); err != nil {
				t.Fatal(err)
			}
		}
	}
	b.res.ServerStamps = map[string]ServerStamp{"tifa": {Stamp: "t"}, "nextjs": {Stamp: "n"}}
	closed, blocks, before := len(b.res.Closed), maps.Clone(b.res.Blocks), len(busy(servers()))
	err := b.checkMachine(context.Background(), []string{"nextjs"})
	switch {
	case err == nil:
		if len(b.res.MachineChecks) != 1 || b.res.MachineChecks[0].Server != "tifa" || len(b.res.MachineChecks[0].Values) != 2 || b.res.MachineChecks[0].Median <= 0 {
			t.Fatalf("the checks %+v", b.res.MachineChecks)
		}
	case strings.Contains(err.Error(), "faster or slower"):
		if len(b.res.MachineChecks) != 0 {
			t.Errorf("a check that failed was kept: %+v", b.res.MachineChecks)
		}
	default:
		t.Fatal(err)
	}
	if len(b.res.Closed) != closed || !maps.Equal(b.res.Blocks, blocks) {
		t.Errorf("the check kept a round (%d of %d) or a size (%v of %v)", len(b.res.Closed), closed, b.res.Blocks, blocks)
	}
	list := busy(servers())[before:]
	if len(list) != 2 {
		t.Fatalf("the check started %d servers, want one for each of its 2 rounds", len(list))
	}
	for _, l := range list {
		if l.spec != "tifa" || len(l.phase(1)) == 0 || len(l.phase(2)) == 0 {
			t.Errorf("a server of the check (%s) had no warm-up or no round: %d and %d requests", l.spec, len(l.phase(1)), len(l.phase(2)))
		}
	}
}
