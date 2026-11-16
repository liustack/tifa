package compare

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// The work of a calibration is fixed: the same loop and the same walk give the same checksum on every
// run and every machine, so that a speed is work done a second and nothing else.
func TestTheCalibrationWorkIsTheSameEveryTime(t *testing.T) {
	if a, b := computeWork(1_000_000), computeWork(1_000_000); a != b || a == 0 {
		t.Errorf("the compute loop gave %d and %d", a, b)
	}
	ring := newWalkRing(1 << 12)
	if a, b := ring.walk(100_000), ring.walk(100_000); a != b {
		t.Errorf("the walk gave %d and %d", a, b)
	}
	// The ring is one cycle through every line: a walk of as many steps as lines comes back to the start
	// having seen each line once.
	seen := map[uint32]bool{}
	at := uint32(0)
	for range ring.lines {
		if seen[at] {
			t.Fatalf("line %d seen twice before the cycle ended", at)
		}
		seen[at] = true
		at = ring.next(at)
	}
	if at != 0 || len(seen) != ring.lines {
		t.Errorf("the walk saw %d of %d lines and ended at %d", len(seen), ring.lines, at)
	}
}

// A calibration is the median of its repeats of each kind of work, as work done a second.
func TestACalibrationIsTheMedianOfItsRepeats(t *testing.T) {
	c := runCalibration(calibrationWork{Compute: 200_000, Steps: 20_000, Lines: 1 << 12, Repeats: 3})
	if len(c.ComputeRuns) != 3 || len(c.MemoryRuns) != 3 {
		t.Fatalf("%d and %d repeats, want 3 of each", len(c.ComputeRuns), len(c.MemoryRuns))
	}
	if c.Compute != Median(c.ComputeRuns) || c.Memory != Median(c.MemoryRuns) || c.Compute <= 0 || c.Memory <= 0 {
		t.Errorf("the calibration %+v", c)
	}
}

// calibrated is a browse of the fake shop whose machine has a base of 100 M/s of compute and 10 of
// memory, and whose calibrations give the compute speeds of speeds in turn, from the first again once
// they run out. events lists the probes, and the hold taken and given back.
func calibrated(t *testing.T, speeds ...float64) (*browse, func() []*launched, *[]string) {
	t.Helper()
	tifa, _, _ := warmSpecs()
	sc := mustScenario("shop")
	b, servers := warmBrowse(t, sc, tifa)
	b.res.Calibration = &CalibrationBase{Compute: 100, Memory: 10}
	var events []string
	var mu sync.Mutex
	b.hold = func(context.Context) (func(), error) {
		mu.Lock()
		events = append(events, "take")
		mu.Unlock()
		return func() {
			mu.Lock()
			events = append(events, "give")
			mu.Unlock()
		}, nil
	}
	calls := 0
	b.o.Calibrate = func(context.Context) (Calibration, error) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, "probe")
		v := speeds[calls%len(speeds)]
		calls++
		return Calibration{Compute: v, Memory: 10}, nil
	}
	return b, servers, &events
}

// warmCell plays the first round of the cell of Tifa at 2 sessions with no calibration, so that the
// sizing and the data of the cell are done and a later round is the round alone.
func warmCell(t *testing.T, b *browse) {
	t.Helper()
	calibrate := b.o.Calibrate
	b.o.Calibrate = nil
	if _, err := b.closedRound(t.Context(), "tifa", mustScenario("shop"), 2, 1); err != nil {
		t.Fatal(err)
	}
	b.o.Calibrate = calibrate
}

// A round whose calibration before its clock or after it is off the base does not count: it is played
// again on a fresh server, and the round that counts is the first try of the server, which keeps its
// calibrations and why the rounds before it did not count.
func TestARoundOffTheSpeedOfTheMachineIsPlayedAgainWithoutATry(t *testing.T) {
	// The round: before the warm-up, before the clock, after the round (4% fast), then one within 3%.
	b, servers, _ := calibrated(t, 100, 100, 104, 101, 99, 102)
	warmCell(t, b)
	before := len(busy(servers()))
	out, err := b.closedRound(t.Context(), "tifa", mustScenario("shop"), 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if out.Attempt != 1 || !out.Valid || len(out.MachineReplays) != 1 || !strings.Contains(out.MachineReplays[0], "after the round, compute at 104.0% of the base") {
		t.Errorf("try %d, valid %v (%s), played again for %q", out.Attempt, out.Valid, out.WhyInvalid, out.MachineReplays)
	}
	var when []string
	for _, c := range out.Calibrations {
		when = append(when, fmt.Sprintf("%s %.2f", c.When, c.ComputeShare))
	}
	if want := []string{"before the warm-up 1.01", "before the clock 0.99", "after the round 1.02"}; !slices.Equal(when, want) {
		t.Errorf("the round keeps the calibrations %q, want %q", when, want)
	}
	if n := len(busy(servers())) - before; n != 2 {
		t.Errorf("%d servers played the round, want 2", n)
	}
	if rounds := b.res.cellRounds("tifa", "shop", 2); len(rounds) != 2 {
		t.Errorf("the cell keeps %d rounds, want 2", len(rounds))
	}
}

// A machine that stays off its speed stops the run: after machineReplays rounds that did not count, and
// after calibrationProbes probes before a warm-up. The message says the run continues from there.
func TestTheRunStopsWhenTheMachineStaysOffItsSpeed(t *testing.T) {
	b, servers, _ := calibrated(t, 100, 95) // within before the warm-up, 5% slow before every clock
	warmCell(t, b)
	before, closed := len(busy(servers())), len(b.res.Closed)
	_, err := b.closedRound(t.Context(), "tifa", mustScenario("shop"), 2, 2)
	if !errors.Is(err, errMachineSpeed) || !strings.Contains(err.Error(), "5 rounds of Tifa in a row") || !strings.Contains(err.Error(), "continues from here when it is started again") {
		t.Errorf("the run ended with %v", err)
	}
	if n := len(busy(servers())) - before; n != 5 || len(b.res.Closed) != closed {
		t.Errorf("%d servers played, %d rounds kept, want 5 and none", n, len(b.res.Closed)-closed)
	}

	defer func(w time.Duration) { calibrationWait = w }(calibrationWait)
	calibrationWait = time.Millisecond
	b, _, events := calibrated(t, 90)
	warmCell(t, b)
	*events = nil
	_, err = b.closedRound(t.Context(), "tifa", mustScenario("shop"), 2, 2)
	if !errors.Is(err, errMachineSpeed) || !strings.Contains(err.Error(), "15 probes before the warm-up") || !strings.Contains(err.Error(), "continues from here") {
		t.Errorf("the run ended with %v", err)
	}
	if got := strings.Count(strings.Join(*events, " "), "take probe give"); got != calibrationProbes {
		t.Errorf("%d probes with the hold taken and given back, want %d: %v", got, calibrationProbes, *events)
	}
}

// The gate before the warm-up of a round probes the machine inside the hold. Off the base it gives the
// hold back, waits, takes it again and probes again, and the round starts once a probe is within.
func TestTheGateBeforeTheWarmUpWaitsWithTheHoldGivenBack(t *testing.T) {
	defer func(w time.Duration) { calibrationWait = w }(calibrationWait)
	calibrationWait = 50 * time.Millisecond
	b, _, events := calibrated(t, 90, 106, 100, 100, 100)
	warmCell(t, b)
	*events = nil
	start := time.Now()
	out, err := b.closedRound(t.Context(), "tifa", mustScenario("shop"), 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(*events, " ")
	if want := "take probe give take probe give take probe probe probe give"; got != want {
		t.Errorf("the hold and the probes went %q, want %q", got, want)
	}
	if time.Since(start) < 2*calibrationWait {
		t.Errorf("the gate did not wait %s between the probes", calibrationWait)
	}
	if len(out.MachineReplays) != 0 || len(out.Calibrations) != 3 || out.Calibrations[0].ComputeShare != 1 {
		t.Errorf("the round kept %+v, played again for %q", out.Calibrations, out.MachineReplays)
	}
}

// The base is the median of 5 calibrations inside one hold, at the start of a run that has none. A run
// that continues a result keeps the base it finds.
func TestTheBaseIsSetOnceAndKeptByARunThatContinues(t *testing.T) {
	b, _, events := calibrated(t, 101, 97, 100, 103, 99)
	b.res.Calibration = nil
	if err := b.calibrationBase(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*events, " "); got != "take probe probe probe probe probe give" {
		t.Errorf("the base was taken as %q", got)
	}
	if c := b.res.Calibration; c == nil || c.Compute != 100 || c.Memory != 10 || len(c.Runs) != 5 || c.Date == "" {
		t.Fatalf("the base %+v", c)
	}
	kept := *b.res.Calibration
	*events = nil
	if err := b.calibrationBase(t.Context()); err != nil || len(*events) != 0 || b.res.Calibration.Compute != kept.Compute {
		t.Errorf("a continued run calibrated %v and has the base %+v (%v)", *events, b.res.Calibration, err)
	}
}

// A speed is off the base when it is more than its tolerance away from it, faster or slower: 3% for
// compute, 8% for memory.
func TestASpeedIsOffTheBaseEitherWay(t *testing.T) {
	base := &CalibrationBase{Compute: 100, Memory: 10}
	for _, tc := range []struct {
		compute, memory float64
		off             bool
	}{{100, 10, false}, {103, 10, false}, {97, 10, false}, {103.1, 10, true}, {96.9, 10, true}, {100, 10.8, false}, {100, 9.2, false}, {100, 10.81, true}, {100, 9.19, true}, {103, 10.8, false}} {
		if got := offBase(base.share(calibrationBeforeClock, Calibration{Compute: tc.compute, Memory: tc.memory})) != ""; got != tc.off {
			t.Errorf("compute %v, memory %v: off %v, want %v", tc.compute, tc.memory, got, tc.off)
		}
	}
}
