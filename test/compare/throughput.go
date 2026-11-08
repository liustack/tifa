package compare

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
)

// The throughput of a scenario starts with every server at the same number of concurrent sessions
// (8 by default, no think time), three rounds each in a rotating order. From there each server is
// stepped on its own: up about 25% a step (10, 13, 16 and so on) while its median server cores stay
// under 0.95 of its cpus and every round passes, or down to 6 and 4 sessions when it fails at 8. A
// step with too few valid rounds is not decided: it is not a failure, and nothing steps down from it.
// What it found is typed (Throughput.Result), and every row says the sessions it was measured at.
// A cell is used only when all its rounds passed: a failed round is never capacity.

// The results of the throughput of a server.
const (
	// ThroughputCoreUsed is throughput with the core used: every round passed and the median server
	// cores reached 0.95 of the cpus.
	ThroughputCoreUsed = "core used"
	// ThroughputPlateau is a plateau with the core not full: one step up gained less than the
	// spread of the rounds.
	ThroughputPlateau = "plateau, core not full"
	// ThroughputHighestPassing is the highest step that passed, the next one having failed the rule in
	// a valid round (the next one up, or 8 sessions when it stepped down).
	ThroughputHighestPassing = "highest passing step"
	// ThroughputCap is a server still under 0.95 of its cpus and still gaining at the last step.
	ThroughputCap = "highest step tried"
	// ThroughputCoresUnknown is a server whose cores are not known at a step that passed (not pinned,
	// or no CPU measured): the steps stop there, and that step is not a capacity.
	ThroughputCoresUnknown = "cores not known"
	// ThroughputNone is a server with no step that passed.
	ThroughputNone = "no passing step"
	// ThroughputUndecided is a server with no step that passed and one that could not be decided (too
	// few valid rounds, none failed): at 8 sessions, or on the way down.
	ThroughputUndecided = "not decided"
	// ThroughputNextUndecided is the highest step that passed, the next one up not decided: it had too
	// few valid rounds, none failed.
	ThroughputNextUndecided = "next step not decided"
)

// Throughput is what the steps found for one server in a scenario: the result, the sessions of the
// cell it reports (0 when none passed), why the steps stopped, and every number of sessions they
// played, in order.
type Throughput struct {
	Result   string `json:"result"`
	Sessions int    `json:"sessions"`
	Why      string `json:"why"`
	Steps    []int  `json:"steps"`
}

// maxSessions is where the steps up stop.
const maxSessions = 640

// stepUp is the next number of sessions of a step up: the next of 10, 12.5, 16, 20, 25, 32, 40, 50,
// 64, 80, 100 and so on, about 25% a step.
func stepUp(sessions int) int {
	for decade := 1.0; ; decade *= 10 {
		for _, m := range []float64{1, 1.25, 1.6, 2, 2.5, 3.2, 4, 5, 6.4, 8} {
			if n := int(math.Round(m * decade)); n > sessions {
				return n
			}
		}
	}
}

// stepsDown are the sessions a server that fails at base tries next: three quarters and half of
// base (6 and 4 from 8).
func stepsDown(base int) []int {
	var out []int
	for _, n := range []int{int(math.Round(0.75 * float64(base))), base / 2} {
		if n >= 1 && n < base && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// coreUse says whether a server used its cpus in its rounds: "used" when the median of its server
// cores reached 0.95 of them, "free" when it stayed under, and "unknown" when the cpus are not known
// or the CPU was not measured (it is read from /proc, on Linux).
func coreUse(rounds []Round, cpus int) string {
	if cpus == 0 || len(rounds) == 0 {
		return "unknown"
	}
	for _, x := range rounds {
		if x.CPUSeconds <= 0 {
			return "unknown"
		}
	}
	if cores(rounds) < 0.95*float64(cpus) {
		return "free"
	}
	return "used"
}

// cores is the median of the server cores a cell used.
func cores(rounds []Round) float64 {
	var v []float64
	for _, x := range rounds {
		if x.Seconds <= 0 {
			return 0
		}
		v = append(v, x.CPUSeconds/x.Seconds)
	}
	return Median(v)
}

// beats says whether more sessions gave more page views a second than the spread of the rounds: the
// slowest round with more sessions is faster than the fastest round with fewer.
func beats(more, fewer []Round) bool {
	if len(more) == 0 || len(fewer) == 0 {
		return false
	}
	return slices.Min(each(more, pageViewsPerSecond)) > slices.Max(each(fewer, pageViewsPerSecond))
}

// joinSessions says a list of sessions as "8", "8 and 6" or "8, 6 and 4".
func joinSessions(list []int) string {
	var s []string
	for _, n := range list {
		s = append(s, fmt.Sprint(n))
	}
	if len(s) < 2 {
		return strings.Join(s, "")
	}
	return strings.Join(s[:len(s)-1], ", ") + " and " + s[len(s)-1]
}

// stepSessions steps one server from base sessions and types what it found. run plays the rounds
// of a cell (or reads back the ones played) and returns its valid rounds. cpus is the cpus of the
// server, 0 when not known. want is the rounds a cell needs. A step passes, fails (a valid round
// failed the rule) or is not decided (too few valid rounds): only a failure steps down, and a step
// that is not decided ends the steps without a claim about it. After every step that passed, its
// cores are decided first (used, or not known, which both end the steps), and only then whether it
// gained over the step below.
func stepSessions(base, cpus, want int, run func(sessions int) ([]Round, error)) (Throughput, error) {
	t := Throughput{}
	play := func(sessions int) ([]Round, string, string, error) {
		t.Steps = append(t.Steps, sessions)
		rs, err := run(sessions)
		if err != nil {
			return nil, "", "", err
		}
		got, why := stepOf(rs, want)
		return rs, got, why, nil
	}
	undecided := func(sessions int, why string) string {
		return fmt.Sprintf("%d sessions could not be decided: %s", sessions, why)
	}
	at, got, why, err := play(base)
	if err != nil {
		return t, err
	}
	switch got {
	case stepUndecided:
		t.Result, t.Why = ThroughputUndecided, undecided(base, why)
		return t, nil
	case trialFail:
		failed := []int{base}
		for _, s := range stepsDown(base) {
			_, down, downWhy, err := play(s)
			if err != nil {
				return t, err
			}
			switch down {
			case trialPass:
				t.Result, t.Sessions, t.Why = ThroughputHighestPassing, s, fmt.Sprintf("failed at %s sessions: %s", joinSessions(failed), why)
				return t, nil
			case stepUndecided:
				t.Result, t.Why = ThroughputUndecided, fmt.Sprintf("failed at %s sessions: %s; %s", joinSessions(failed), why, undecided(s, downWhy))
				return t, nil
			}
			failed = append(failed, s)
		}
		t.Result, t.Why = ThroughputNone, fmt.Sprintf("failed at %s sessions: %s", joinSessions(failed), why)
		return t, nil
	}
	t.Sessions = base
	// cored ends the steps at a step that passed when its cores are used or not known.
	cored := func(rs []Round, sessions int) bool {
		switch coreUse(rs, cpus) {
		case "unknown":
			t.Result, t.Sessions = ThroughputCoresUnknown, sessions
			t.Why = fmt.Sprintf("the cores of the server were not known at %d sessions (it needs -server-cpus and its CPU from /proc, on Linux): the steps stopped", sessions)
			if sessions == base {
				t.Why = "the cores of the server are not known (it needs -server-cpus and its CPU from /proc, on Linux): no steps"
			}
			return true
		case "used":
			t.Result, t.Sessions, t.Why = ThroughputCoreUsed, sessions, fmt.Sprintf("used %.2f of %d cpus at %d sessions", cores(rs), cpus, sessions)
			return true
		}
		return false
	}
	if cored(at, base) {
		return t, nil
	}
	for next := stepUp(base); ; next = stepUp(next) {
		if next > maxSessions {
			t.Result, t.Why = ThroughputCap, fmt.Sprintf("still under 0.95 of %d cpus and gaining at %d sessions, where the steps stop", cpus, t.Sessions)
			return t, nil
		}
		rs, got, why, err := play(next)
		if err != nil {
			return t, err
		}
		switch got {
		case trialFail:
			t.Result, t.Why = ThroughputHighestPassing, fmt.Sprintf("failed at %d sessions: %s", next, why)
			return t, nil
		case stepUndecided:
			t.Result, t.Why = ThroughputNextUndecided, undecided(next, why)
			return t, nil
		}
		if cored(rs, next) {
			return t, nil
		}
		if !beats(rs, at) {
			t.Result, t.Why = ThroughputPlateau, fmt.Sprintf("at %d sessions the page views a second rose by less than the spread of the rounds, with %.2f of %d cpus used at %d", next, cores(at), cpus, t.Sessions)
			return t, nil
		}
		t.Sessions, at = next, rs
	}
}

// throughput is the phase of the closed tables of a scenario: every server at the base sessions,
// the rounds in a rotating order with fresh servers, then each server stepped on its own.
func (b *browse) throughput(ctx context.Context, sc BrowseScenario) error {
	if !sc.Throughput {
		return nil
	}
	base := b.o.Sessions
	for round := 1; round <= b.o.Rounds; round++ {
		for _, fw := range Rotate(b.servers[sc.Name], round-1) {
			if b.res.closedDone(fw, sc.Name, base, round) || b.res.cellFailed(fw, sc.Name, base) {
				continue
			}
			if _, err := b.closedRound(ctx, fw, sc, base, round); err != nil {
				return err
			}
		}
	}
	if b.res.Throughput == nil {
		b.res.Throughput = map[string]*Throughput{}
	}
	for _, fw := range b.servers[sc.Name] {
		key := sustainKey(sc.Name, fw)
		if b.res.Throughput[key] != nil {
			continue
		}
		t, err := stepSessions(base, b.o.ServerCPUCount, b.o.Rounds, func(sessions int) ([]Round, error) { return b.cell(ctx, fw, sc, sessions) })
		if err != nil {
			return err
		}
		b.logf("throughput %-10s %-12s %s at %d sessions: %s (steps %v)\n", sc.Name, fw, t.Result, t.Sessions, t.Why, t.Steps)
		b.res.Throughput[key] = &t
		if err := b.commit(); err != nil {
			return err
		}
	}
	return nil
}

// cell plays the rounds of one server at a number of sessions that are not done yet, up to the
// first one that fails the rule (which decides the cell), and returns the valid rounds of the cell.
func (b *browse) cell(ctx context.Context, fw string, sc BrowseScenario, sessions int) ([]Round, error) {
	for round := 1; round <= b.o.Rounds; round++ {
		if b.res.closedDone(fw, sc.Name, sessions, round) {
			continue
		}
		if b.res.cellFailed(fw, sc.Name, sessions) {
			break
		}
		if _, err := b.closedRound(ctx, fw, sc, sessions, round); err != nil {
			return nil, err
		}
	}
	return b.res.cellRounds(fw, sc.Name, sessions), nil
}

// closedDone says whether a round of a cell is done: it has a valid try, or the three tries it gets.
func (r *BrowseResult) closedDone(fw, sc string, sessions, round int) bool {
	tries := 0
	for _, x := range r.Closed {
		if x.Framework == fw && x.Scenario == sc && x.Sessions == sessions && x.Round == round {
			if x.Valid {
				return true
			}
			tries++
		}
	}
	return tries >= maxTries
}

// cellFailed says whether a cell has a valid round that failed the rule, which decides the cell.
func (r *BrowseResult) cellFailed(fw, sc string, sessions int) bool {
	return slices.ContainsFunc(r.Closed, func(x Round) bool {
		return x.Framework == fw && x.Scenario == sc && x.Sessions == sessions && x.Valid && !x.Pass
	})
}

// cellRounds are the valid rounds of a spec in one cell of the closed table.
func (r *BrowseResult) cellRounds(fw, sc string, sessions int) []Round {
	var out []Round
	for _, x := range r.Closed {
		if x.Framework == fw && x.Scenario == sc && x.Sessions == sessions && x.Valid {
			out = append(out, x)
		}
	}
	return out
}

func each(rounds []Round, f func(*Round) float64) []float64 {
	var out []float64
	for i := range rounds {
		out = append(out, f(&rounds[i]))
	}
	return out
}

func pageViewsPerSecond(x *Round) float64 { return float64(x.PageViews) / x.Seconds }

// stepUndecided is the outcome of a cell with fewer valid rounds than it needs and none that failed:
// the experiment did not decide it (its other rounds stayed invalid).
const stepUndecided = "undecided"

// stepOf is the outcome of a cell from its valid rounds, with why: trialFail when a round failed the
// rule (a failed round decides the cell, however many rounds it has), stepUndecided when there are
// fewer than want, and trialPass otherwise.
func stepOf(rounds []Round, want int) (string, string) {
	for _, x := range rounds {
		if !x.Pass {
			return trialFail, x.Why
		}
	}
	if len(rounds) < want {
		return stepUndecided, fmt.Sprintf("%d of %d rounds were valid", len(rounds), want)
	}
	return trialPass, ""
}
