package compare

import (
	"context"
	"strings"
	"testing"
	"time"
)

// plannedRound is a round whose workload planned 10 visits of 4 page views (one full load, three clicks)
// and 2 writes, as at 5% writes.
func plannedRound() *Round {
	return &Round{Model: "closed", VisitsPlanned: 10, VisitsStarted: 10, PageViewsPlanned: 40, ClicksPlanned: 30, WritesPlanned: 2,
		Visits: 10, PageViews: 40, FullLoads: 10, Clicks: 30, Writes: 2,
		Types: map[string]TypeStat{"load": {N: 10, P99: 100}, "click": {N: 30, P99: 100}, "write": {N: 2, P99: 100}}}
}

// The mix is checked over the visits as planned, not over the ones that completed: a server that
// fails visits cuts them short, which changes the mix achieved, and that round is a valid round that
// failed the rule, not a bad experiment to run again.
func TestTheMixIsCheckedOverThePlannedVisits(t *testing.T) {
	sc := BrowseScenario{Share: 2.0 / 42}
	r := plannedRound()
	r.Visits, r.VisitsFailed, r.PageViews, r.Clicks, r.Writes, r.Failures, r.FirstErr = 6, 4, 26, 20, 0, 4, "GET /p: status 503"
	r.Judge(sc)
	if !r.Valid || r.Pass {
		t.Errorf("valid %v (%s), pass %v (%s): want a valid round that failed", r.Valid, r.WhyInvalid, r.Pass, r.Why)
	}
	if !strings.Contains(r.Why, "503") {
		t.Errorf("why %q", r.Why)
	}
	for name, mod := range map[string]func(*Round){
		"writes":             func(r *Round) { r.WritesPlanned = 10 },
		"clicks":             func(r *Round) { r.ClicksPlanned = 20 },
		"page views a visit": func(r *Round) { r.VisitsPlanned, r.VisitsStarted = 12, 12 },
		"nothing planned":    func(r *Round) { r.VisitsPlanned = 0 },
	} {
		r := plannedRound()
		mod(r)
		r.Judge(sc)
		if r.Valid {
			t.Errorf("a plan off in its %s is valid", name)
		}
	}
}

// Only the load generator and a stop make a round invalid: a generator that started fewer visits
// than planned without the round being cut off, or one that fell behind or ran out of cpus in the
// open model. A backlog of the server is a valid round that failed.
func TestOnlyTheLoadGeneratorMakesARoundInvalid(t *testing.T) {
	sc := BrowseScenario{Share: 2.0 / 42}
	r := plannedRound()
	r.VisitsStarted, r.Visits, r.PageViews, r.Clicks, r.FullLoads = 8, 8, 32, 24, 8
	r.Judge(sc)
	if r.Valid || !strings.Contains(r.WhyInvalid, "started 8 of the 10 visits") {
		t.Errorf("a generator that did not start every visit: valid %v %q", r.Valid, r.WhyInvalid)
	}
	open := plannedRound()
	open.Model, open.TimedOut, open.Backlog = "open", true, true
	open.VisitsStarted, open.Visits, open.VisitsUnfinished = 10, 7, 3
	open.Judge(sc)
	if !open.Valid || open.Pass {
		t.Errorf("a backlog: valid %v (%s), pass %v", open.Valid, open.WhyInvalid, open.Pass)
	}
	for _, tc := range []struct {
		lag, client float64
		want        string
	}{{300, 1, "started a visit 300 ms late"}, {10, 1.8, "used 1.80 cores of 2"}, {10, 1, ""}} {
		r := plannedRound()
		r.Model, r.LagMs, r.ClientCPU, r.Seconds = "open", tc.lag, tc.client*100, 100
		if got := generatorFault(r, 2); !strings.Contains(got, tc.want) || (tc.want == "") != (got == "") {
			t.Errorf("lag %v, client %v: %q, want %q", tc.lag, tc.client, got, tc.want)
		}
	}
}

// A closed round outside the length window is resized from what it did. It is run again only when it
// passed the rule: a round that failed it is kept as a valid failed round, so the resize, which is
// calibration, never discards an observed failure of the server.
func TestTheResizeNeverDiscardsAServerFailure(t *testing.T) {
	target := 75 * time.Second
	short := plannedRound()
	short.Seconds, short.Visits = 40, 3800
	short.Pass = true
	if blocks := calibrate(short, target, 4); blocks == 0 || short.Valid {
		t.Errorf("a short round that passed: %d blocks, valid %v", blocks, short.Valid)
	}
	failed := plannedRound()
	failed.Seconds, failed.Visits, failed.VisitsFailed, failed.Valid, failed.Why = 40, 1000, 2800, true, "2800 failures (GET /p: status 503)"
	if blocks := calibrate(failed, target, 4); blocks != BlocksFor(3800/40.0, target, 4) || !failed.Valid {
		t.Errorf("a short round that failed: %d blocks, valid %v %s", blocks, failed.Valid, failed.WhyInvalid)
	}
	in := plannedRound()
	in.Seconds, in.Pass, in.Valid = 90, true, true
	if blocks := calibrate(in, target, 4); blocks != 0 || !in.Valid {
		t.Errorf("a round in the window: %d blocks, valid %v", blocks, in.Valid)
	}
}

// A closed round cut off by its timeout counts the visits it never ended as unfinished: planned,
// succeeded, failed and unfinished add up, and the round is cut short, not failed.
func TestAClosedRoundCutOffCountsItsUnfinishedVisits(t *testing.T) {
	srv, _ := fakeServer(t)
	plans, traces := onePlan(TraceStep{Kind: StepLoad, Page: "/p", Reqs: []TraceReq{req("/slow-a", "doc", 0, 5)}},
		TraceStep{Kind: StepClick, Page: "/p", Reqs: []TraceReq{req("/b", "nav", 0, 5)}}, TraceStep{Kind: StepClick, Page: "/p", Reqs: []TraceReq{req("/c", "nav", 0, 5)}}, TraceStep{Kind: StepClick, Page: "/p", Reqs: []TraceReq{req("/d", "nav", 0, 5)}})
	r := testRunner(srv, plans, traces, nil)
	r.wl.Scenario.Share = 0
	round := r.RunClosed(context.Background(), 1, 200, 300*time.Millisecond)
	if !round.TimedOut || round.VisitsPlanned != 200 || round.Visits == 0 || round.VisitsUnfinished == 0 || round.VisitsFailed != 0 {
		t.Fatalf("%+v", round)
	}
	if round.Visits+round.VisitsFailed+round.VisitsUnfinished != round.VisitsPlanned || round.VisitsStarted > round.VisitsPlanned || round.PageViewsPlanned != 800 || round.ClicksPlanned != 600 {
		t.Errorf("planned %d, started %d, succeeded %d, failed %d, unfinished %d, page views planned %d", round.VisitsPlanned, round.VisitsStarted, round.Visits, round.VisitsFailed, round.VisitsUnfinished, round.PageViewsPlanned)
	}
	round.Judge(BrowseScenario{Share: 0})
	if !round.Valid || !round.Pass {
		t.Errorf("a round cut off by its timeout: valid %v (%s), pass %v (%s)", round.Valid, round.WhyInvalid, round.Pass, round.Why)
	}
}

// The visits that fail are counted as failed, with the page views and writes they planned.
func TestAClosedRoundCountsTheVisitsThatFailed(t *testing.T) {
	srv, _ := fakeServer(t)
	plans, traces := onePlan(TraceStep{Kind: StepLoad, Page: "/p", Reqs: []TraceReq{req("/fail-1", "doc", 0, 5)}},
		TraceStep{Kind: StepClick, Page: "/p", Reqs: []TraceReq{req("/b", "nav", 0, 5)}}, TraceStep{Kind: StepClick, Page: "/p", Reqs: []TraceReq{req("/c", "nav", 0, 5)}}, TraceStep{Kind: StepClick, Page: "/p", Reqs: []TraceReq{req("/d", "nav", 0, 5)}})
	r := testRunner(srv, plans, traces, nil)
	r.wl.Scenario.Share = 0
	round := r.RunClosed(context.Background(), 2, 6, 10*time.Second)
	if round.Visits != 0 || round.VisitsFailed != 6 || round.VisitsStarted != 6 || round.VisitsPlanned != 6 || round.VisitsUnfinished != 0 || round.PageViewsPlanned != 24 || round.PageViews != 0 {
		t.Errorf("%+v", round)
	}
	round.Judge(BrowseScenario{Share: 0})
	if !round.Valid || round.Pass {
		t.Errorf("valid %v (%s), pass %v", round.Valid, round.WhyInvalid, round.Pass)
	}
}

// The confirmation of a rate never accepts an invalid round, even one that passed the rule.
func TestConfirmRateRefusesAnInvalidRound(t *testing.T) {
	rate, _, err := ConfirmRate(100, 2, 3, func(rate float64, round int) (*Round, error) {
		return &Round{Rate: rate, Pass: true, Valid: rate < 95}, nil
	}, 0)
	if err != nil || rate != 90 {
		t.Errorf("rate %v (%v), want 90", rate, err)
	}
}
