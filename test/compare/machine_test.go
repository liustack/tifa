package compare

import (
	"context"
	"fmt"
	"io"
	"maps"
	"strings"
	"testing"
)

// checkBrowse is a continued run of the shop whose result keeps the three closed rounds of each
// main server at 8 sessions, start at views a second, the others at 300, and one of a reference.
// play stands for the round of the check: it returns the page views a second of each try, and
// whether it is valid.
func checkBrowse(views []float64, play func(fw string, try int) (float64, bool)) (*browse, *[]string) {
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
	b.playCheck = func(fw string, sc BrowseScenario, try int) (*Round, error) {
		played = append(played, fmt.Sprintf("%s try %d", fw, try))
		v, valid := play(fw, try)
		x := closedRound(fw, sc.Name, 8, 0, int(100*v), 100, true)
		x.Valid, x.Attempt = valid, try
		if !valid {
			x.WhyInvalid = "the load generator used 1.80 cores of 2"
		}
		return &x, nil
	}
	return b, &played
}

// A continued run that measures a server again plays a closed round of TanStack Start, which it keeps,
// before anything else. Its page views a second within the kept rounds of its cell, less 2% of the
// slowest and 2% over the fastest, pass: the check is kept in the result with the stamps it passed
// for, and the round is no result.
func TestTheMachineCheckPassesWithinTheKeptRounds(t *testing.T) {
	for _, got := range []float64{98.5, 106} {
		b, played := checkBrowse([]float64{100, 104, 102}, func(string, int) (float64, bool) { return got, true })
		closed := len(b.res.Closed)
		if err := b.checkMachine(t.Context(), []string{"nextjs"}); err != nil {
			t.Fatalf("%v page views a second: %v", got, err)
		}
		if len(*played) != 1 || (*played)[0] != "start try 1" || len(b.res.Closed) != closed {
			t.Errorf("played %v, %d closed rounds kept of %d", *played, len(b.res.Closed), closed)
		}
		if len(b.res.MachineChecks) != 1 {
			t.Fatalf("%d checks kept", len(b.res.MachineChecks))
		}
		c := b.res.MachineChecks[0]
		if c.Server != "start" || c.Scenario != "shop" || c.Sessions != 8 || c.Value != got || c.Min != 98 || c.Max != 104*1.02 || c.Date == "" || !maps.Equal(c.Stamps, checkStamps(b.res.ServerStamps)) {
			t.Errorf("the check kept %+v", c)
		}
	}
}

// A check round outside that range stops the run before it measures anything: the rounds it keeps
// were measured on a machine that is faster or slower now. Nothing is kept of it.
func TestTheMachineCheckStopsARunOnAMachineThatChanged(t *testing.T) {
	for _, got := range []float64{97.9, 106.2} {
		b, _ := checkBrowse([]float64{100, 104, 102}, func(string, int) (float64, bool) { return got, true })
		err := b.checkMachine(t.Context(), []string{"nextjs"})
		if err == nil || !strings.Contains(err.Error(), "faster or slower") || !strings.Contains(err.Error(), "must start again") ||
			!strings.Contains(err.Error(), fmt.Sprintf("%.1f page views a second", got)) || !strings.Contains(err.Error(), "98.0 to 106.1") {
			t.Errorf("%v page views a second: %v", got, err)
		}
		if len(b.res.MachineChecks) != 0 {
			t.Errorf("%v page views a second: a check was kept", got)
		}
	}
}

// A check that passed is not played again when the run is continued with the same stamps, and is
// played again when a stamp of a server changed since.
func TestTheMachineCheckIsNotPlayedAgainForTheSameStamps(t *testing.T) {
	b, played := checkBrowse([]float64{100, 104, 102}, func(string, int) (float64, bool) { return 101, true })
	if err := b.checkMachine(t.Context(), []string{"nextjs"}); err != nil {
		t.Fatal(err)
	}
	b.res.ServerStamps["nextjs"] = ServerStamp{Stamp: "n1", Trace: "b", Date: "2026-11-16"} // the same stamps, measured again later
	if err := b.checkMachine(t.Context(), []string{"nextjs"}); err != nil || len(*played) != 1 || len(b.res.MachineChecks) != 1 {
		t.Errorf("the same stamps: played %v, %d checks (%v)", *played, len(b.res.MachineChecks), err)
	}
	b.res.ServerStamps["nextjs"] = ServerStamp{Stamp: "n2", Trace: "b", Date: "2026-11-16"}
	if err := b.checkMachine(t.Context(), []string{"nextjs"}); err != nil || len(*played) != 2 || len(b.res.MachineChecks) != 2 {
		t.Errorf("another stamp: played %v, %d checks (%v)", *played, len(b.res.MachineChecks), err)
	}
}

// A check round that is not valid is played again, as a round of the cell is, up to three tries; one
// that is never valid stops the run.
func TestAnInvalidMachineCheckRoundIsPlayedAgain(t *testing.T) {
	b, played := checkBrowse([]float64{100, 104, 102}, func(_ string, try int) (float64, bool) { return 300, try < 3 && try > 1 })
	if err := b.checkMachine(t.Context(), []string{"nextjs"}); err == nil || !strings.Contains(err.Error(), "faster or slower") || len(*played) != 2 {
		t.Errorf("valid at the second try: played %v (%v)", *played, err)
	}
	b, played = checkBrowse([]float64{100, 104, 102}, func(string, int) (float64, bool) { return 101, false })
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
		b, played := checkBrowse([]float64{300, 300, 300}, func(string, int) (float64, bool) { return 300, true })
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

// The round of the check is a closed round of the cell as it is played, on a server of its own after
// the sizing it keeps, the setup and the warm-up, and none of it is kept: no closed round, no new size
// of the cell.
func TestTheMachineCheckRoundIsAClosedRoundOfTheCellAndNoResult(t *testing.T) {
	tifa, _, node := warmSpecs()
	sc := mustScenario("shop")
	b, servers := warmBrowse(t, sc, tifa, node)
	b.o.Rounds = 2
	for _, fw := range []string{"tifa", "nextjs"} {
		if _, err := b.closedRound(context.Background(), fw, sc, 2, 1); err != nil {
			t.Fatal(err)
		}
	}
	// The kept rounds of Tifa span every speed, so the check passes whatever the machine does.
	slow, fast := closedRound("tifa", sc.Name, 2, 1, 1, 100, true), closedRound("tifa", sc.Name, 2, 2, 1e9, 1, true)
	b.res.Closed = append(b.res.Closed, slow, fast)
	b.res.ServerStamps = map[string]ServerStamp{"tifa": {Stamp: "t"}, "nextjs": {Stamp: "n"}}
	closed, blocks, before := len(b.res.Closed), maps.Clone(b.res.Blocks), len(busy(servers()))
	if err := b.checkMachine(context.Background(), []string{"nextjs"}); err != nil {
		t.Fatal(err)
	}
	if len(b.res.MachineChecks) != 1 || b.res.MachineChecks[0].Server != "tifa" || b.res.MachineChecks[0].Value <= 0 {
		t.Fatalf("the checks %+v", b.res.MachineChecks)
	}
	if len(b.res.Closed) != closed || !maps.Equal(b.res.Blocks, blocks) {
		t.Errorf("the check kept a round (%d of %d) or a size (%v of %v)", len(b.res.Closed), closed, b.res.Blocks, blocks)
	}
	list := busy(servers())[before:]
	if len(list) == 0 {
		t.Fatal("the check started no server")
	}
	if l := list[len(list)-1]; l.spec != "tifa" || len(l.phase(1)) == 0 || len(l.phase(2)) == 0 {
		t.Errorf("the server of the check (%s) had no warm-up or no round: %d and %d requests", l.spec, len(l.phase(1)), len(l.phase(2)))
	}
}
