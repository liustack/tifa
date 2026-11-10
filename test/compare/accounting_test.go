package compare

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
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
	sc := BrowseScenario{}
	r := plannedRound()
	r.Visits, r.VisitsFailed, r.PageViews, r.Clicks, r.Writes, r.Failures, r.FirstErr = 6, 4, 26, 20, 0, 4, "GET /p: status 503"
	r.Judge(sc)
	if !r.Valid || r.Pass {
		t.Errorf("valid %v (%s), pass %v (%s): want a valid round that failed", r.Valid, r.WhyInvalid, r.Pass, r.Why)
	}
	if !strings.Contains(r.Why, "503") {
		t.Errorf("why %q", r.Why)
	}
	r = plannedRound()
	r.VisitsPlanned = 0
	if r.Judge(sc); r.Valid {
		t.Error("a round that planned nothing is valid")
	}
}

// The mix of the site is checked on what the visits of a round plan: a round of whole blocks has it
// exactly, and one off in any part of it (the staff share, the page views of a shopper visit, the
// returning visitors, the adds, the checkouts, the edits, the ships) is not valid. A read-only
// scenario checks no write.
func TestTheMixOfTheSiteIsChecked(t *testing.T) {
	w := siteWorkload(t, 2, 1, "shop")
	var whole Round
	w.plan(&whole, 0, 3*w.blockVisits())
	if why := whole.Mix.why(w.Scenario); why != "" {
		t.Fatalf("three blocks: %s", why)
	}
	for name, mod := range map[string]func(*PlannedMix){
		"staff":          func(m *PlannedMix) { m.Staff += m.Staff/2 + 1 },
		"page views":     func(m *PlannedMix) { m.ShopperViews += m.ShopperViews / 50 },
		"returning":      func(m *PlannedMix) { m.Returning += m.Shoppers / 100 },
		"adds":           func(m *PlannedMix) { m.Adds += m.Shoppers / 200 },
		"checkouts":      func(m *PlannedMix) { m.Checkouts -= m.Checkouts / 2 },
		"edits":          func(m *PlannedMix) { m.Edits += m.Staff / 10 },
		"ships":          func(m *PlannedMix) { m.Ships = 0 },
		"shopper visits": func(m *PlannedMix) { m.Shoppers = 0 },
		"crawlers":       func(m *PlannedMix) { m.Crawlers += m.Crawlers / 4 },
		"documents":      func(m *PlannedMix) { m.Documents -= m.Documents / 5 },
	} {
		m := *whole.Mix
		mod(&m)
		if m.why(w.Scenario) == "" {
			t.Errorf("a plan off in its %s has the mix", name)
		}
		r := plannedRound()
		r.Mix = &m
		if r.Judge(w.Scenario); r.Valid {
			t.Errorf("a round off in its %s is valid", name)
		}
	}
	reads := w.Scenario
	reads.ReadOnly = true
	m := *whole.Mix
	m.Adds, m.Checkouts, m.Edits, m.Ships = 0, 0, 0, 0
	if why := m.why(reads); why != "" {
		t.Errorf("a read-only round: %s", why)
	}
}

// Apart from a request the shop refused (TestARefusedAnswerMakesItsRoundInvalid), only the load generator
// and a stop make a round invalid: a generator that started fewer visits
// than planned without the round being cut off, or one that fell behind or ran out of cpus in the
// open model. A backlog of the server is a valid round that failed.
func TestOnlyTheLoadGeneratorMakesARoundInvalid(t *testing.T) {
	sc := BrowseScenario{}
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
	if blocks := calibrate(short, target, 4, 95); blocks == 0 || short.Valid {
		t.Errorf("a short round that passed: %d blocks, valid %v", blocks, short.Valid)
	}
	failed := plannedRound()
	failed.Seconds, failed.Visits, failed.VisitsFailed, failed.Valid, failed.Why = 40, 1000, 2800, true, "2800 failures (GET /p: status 503)"
	if blocks := calibrate(failed, target, 4, 95); blocks != BlocksFor(3800/40.0, target, 4, 95) || !failed.Valid {
		t.Errorf("a short round that failed: %d blocks, valid %v %s", blocks, failed.Valid, failed.WhyInvalid)
	}
	in := plannedRound()
	in.Seconds, in.Pass, in.Valid = 90, true, true
	if blocks := calibrate(in, target, 4, 95); blocks != 0 || !in.Valid {
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
	round := r.RunClosed(context.Background(), 1, 200, 300*time.Millisecond)
	if !round.TimedOut || round.VisitsPlanned != 200 || round.Visits == 0 || round.VisitsUnfinished == 0 || round.VisitsFailed != 0 {
		t.Fatalf("%+v", round)
	}
	if round.Visits+round.VisitsFailed+round.VisitsUnfinished != round.VisitsPlanned || round.VisitsStarted > round.VisitsPlanned || round.PageViewsPlanned != 800 || round.ClicksPlanned != 600 {
		t.Errorf("planned %d, started %d, succeeded %d, failed %d, unfinished %d, page views planned %d", round.VisitsPlanned, round.VisitsStarted, round.Visits, round.VisitsFailed, round.VisitsUnfinished, round.PageViewsPlanned)
	}
	if round.CutOff == 0 || round.CutOffLate != 0 {
		t.Errorf("cut off %d actions, %d over the rule: an action cut off within the rule is a matter of sizing", round.CutOff, round.CutOffLate)
	}
	round.Judge(BrowseScenario{})
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
	round := r.RunClosed(context.Background(), 2, 6, 10*time.Second)
	if round.Visits != 0 || round.VisitsFailed != 6 || round.VisitsStarted != 6 || round.VisitsPlanned != 6 || round.VisitsUnfinished != 0 || round.PageViewsPlanned != 24 || round.PageViews != 0 {
		t.Errorf("%+v", round)
	}
	round.Judge(BrowseScenario{})
	if !round.Valid || round.Pass {
		t.Errorf("valid %v (%s), pass %v", round.Valid, round.WhyInvalid, round.Pass)
	}
}

// The confirmation of a rate never accepts an invalid round, even one that passed the rule, and an
// invalid round never moves it: the round is played again at the same rate, up to three tries. When
// the tries run out the confirmation stops there, limited by the load generator, with every round kept.
func TestConfirmRateRetriesAnInvalidRoundAtItsRate(t *testing.T) {
	var ran []float64
	plays := map[[2]float64]int{}
	run := func(invalid func(rate float64, round, try int) bool) func(float64, int) (*Round, error) {
		ran, plays = nil, map[[2]float64]int{}
		return func(rate float64, round int) (*Round, error) {
			ran = append(ran, rate)
			k := [2]float64{rate, float64(round)}
			plays[k]++
			return &Round{Rate: rate, Pass: rate <= 100, Valid: !invalid(rate, round, plays[k])}, nil
		}
	}
	// Round 2 at 100 is invalid on its first try: it is played again at 100, and 100 is confirmed.
	rate, rounds, limited, err := ConfirmRate(100, 4, 3, run(func(r float64, round, try int) bool { return r == 100 && round == 2 && try == 1 }), 0)
	if err != nil || rate != 100 || limited || len(rounds) != 4+1 || rounds[1].Valid || rounds[1].Round != 2 || rounds[2].Round != 2 {
		t.Errorf("a retried round: rate %v, limited %v, %d rounds %+v (%v)", rate, limited, len(rounds), rounds, err)
	}
	// Every try at 100 is invalid: no rate, limited, and no step down to 90.
	rate, rounds, limited, _ = ConfirmRate(100, 4, 3, run(func(float64, int, int) bool { return true }), 0)
	if rate != 0 || !limited || len(rounds) != 3 || slices.Contains(ran, 90) {
		t.Errorf("an invalid rate: rate %v, limited %v, %d rounds, ran %v", rate, limited, len(rounds), ran)
	}
	// 100 is confirmed and every try 10%% above it is invalid: 100 stands, limited.
	rate, _, limited, _ = ConfirmRate(100, 4, 3, run(func(r float64, _, _ int) bool { return r > 100 }), 0)
	if rate != 100 || !limited {
		t.Errorf("an invalid step up: rate %v, limited %v", rate, limited)
	}
}

// The search of a rate stops at a rate whose trials stay invalid: it neither halves nor narrows on
// an experiment that did not work, and says that the load generator limited it.
func TestTheRateSearchStopsAtAnInvalidTrial(t *testing.T) {
	var tried []float64
	lo, hi, limited, err := searchRate(16384, func(rate float64) (string, error) {
		tried = append(tried, rate)
		if rate >= 16 {
			return trialInvalid, nil
		}
		return trialPass, nil
	})
	if err != nil || lo != 8 || hi != 0 || !limited || !slices.Equal(tried, []float64{4, 8, 16}) {
		t.Errorf("lo %v hi %v limited %v tried %v (%v)", lo, hi, limited, tried, err)
	}
	if _, _, limited, _ := searchRate(16384, func(float64) (string, error) { return trialInvalid, nil }); !limited {
		t.Error("a first trial that stays invalid is not limited")
	}
}

// A trial plays an invalid round again at the same rate, up to three tries, and its outcome is that
// of the last try.
func TestATrialRetriesAnInvalidRound(t *testing.T) {
	for _, tc := range []struct {
		validAt int // the try that is valid, 0 for none
		want    string
		tries   int
	}{{1, trialPass, 1}, {3, trialPass, 3}, {0, trialInvalid, 3}} {
		tries := 0
		r, err := retryInvalid(func(try int) (*Round, error) {
			tries++
			return &Round{Attempt: try, Pass: true, Valid: try == tc.validAt}, nil
		})
		if err != nil || trialOf(r) != tc.want || tries != tc.tries {
			t.Errorf("valid at try %d: %s after %d tries (%v)", tc.validAt, trialOf(r), tries, err)
		}
	}
	if got := trialOf(&Round{Valid: true}); got != trialFail {
		t.Errorf("a valid round that failed: %s", got)
	}
}

// A closed round in which nothing completed before its timeout (a hung server) is a valid round that
// failed, which decides its cell, and its length resizes nothing.
func TestAHungClosedRoundIsAValidFailedRound(t *testing.T) {
	r := plannedRound()
	r.TimedOut, r.Seconds = true, 150
	r.VisitsStarted, r.Visits, r.VisitsUnfinished, r.PageViews, r.FullLoads, r.Clicks, r.Writes = 3, 0, 10, 0, 0, 0, 0
	r.Types = map[string]TypeStat{}
	r.Judge(BrowseScenario{})
	if !r.Valid || r.Pass {
		t.Errorf("valid %v (%s), pass %v", r.Valid, r.WhyInvalid, r.Pass)
	}
	if blocks := calibrate(r, 75*time.Second, 4, 95); blocks != 0 || !r.Valid {
		t.Errorf("resized to %d blocks, valid %v", blocks, r.Valid)
	}
}

// fourViews is a visit of a full load and three clicks, all to url.
func fourViews(url string) []TraceStep {
	return []TraceStep{{Kind: StepLoad, Page: "/p", Reqs: []TraceReq{req(url, "doc", 0, 5)}}, {Kind: StepClick, Page: "/p", Reqs: []TraceReq{req(url, "nav", 0, 5)}},
		{Kind: StepClick, Page: "/p", Reqs: []TraceReq{req(url, "nav", 0, 5)}}, {Kind: StepClick, Page: "/p", Reqs: []TraceReq{req(url, "nav", 0, 5)}}}
}

// A server that answers well and then stops answering leaves actions in flight for longer than the
// rule when the round is cut off. The answers it gave pass the rule, but the round is a valid round
// that failed, and its length resizes nothing away.
func TestAClosedRoundThatIsFineAndThenStallsFails(t *testing.T) {
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) > 40 {
			<-r.Context().Done()
			return
		}
		w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	plans, traces := onePlan(fourViews("/a")...)
	r := testRunner(srv, plans, traces, nil)
	round := r.RunClosed(context.Background(), 2, 400, 1500*time.Millisecond)
	if !round.TimedOut || round.Visits == 0 || round.Types["load"].P99 > LatencyLimit || round.Types["click"].P99 > LatencyLimit {
		t.Fatalf("the test shows nothing: %+v", round)
	}
	if round.CutOff != 2 || round.CutOffLate != 2 || round.CutOffOldestMs < LatencyLimit {
		t.Errorf("cut off %d actions, %d over the rule, the oldest %.0f ms", round.CutOff, round.CutOffLate, round.CutOffOldestMs)
	}
	round.Judge(BrowseScenario{})
	if !round.Valid || round.Pass || !strings.Contains(round.Why, "cut off") {
		t.Errorf("valid %v (%s), pass %v (%s): want a valid round that failed", round.Valid, round.WhyInvalid, round.Pass, round.Why)
	}
	calibrate(round, 75*time.Second, 4, 95)
	if !round.Valid {
		t.Errorf("the resize discarded the failure: %s", round.WhyInvalid)
	}
}

// One request that never answers, among requests that all answer at once, fails the round too.
func TestAClosedRoundWithOneRequestThatNeverAnswersFails(t *testing.T) {
	srv, _ := fakeServer(t)
	hung := fourViews("/a")
	hung[2].Reqs = []TraceReq{req("/hold-1", "nav", 0, 5)}
	traces := make([]Trace, 8000)
	for i := range traces {
		traces[i] = Trace{Steps: fourViews("/a")}
	}
	traces[1] = Trace{Steps: hung} // the second visit of the round, and no other
	r := newTestRunner(srv.URL, BrowseScenario{}, traces...)
	round := r.RunClosed(context.Background(), 4, 8000, 1500*time.Millisecond)
	if !round.TimedOut || round.Visits < 100 {
		t.Fatalf("the test shows nothing: %+v", round)
	}
	if round.CutOffLate == 0 {
		t.Errorf("cut off %d actions, none over the rule", round.CutOff)
	}
	round.Judge(BrowseScenario{})
	if !round.Valid || round.Pass {
		t.Errorf("valid %v (%s), pass %v (%s): want a valid round that failed", round.Valid, round.WhyInvalid, round.Pass, round.Why)
	}
}

// A closed round whose load generator ran out of cpus is not valid, whatever the server did: it is
// played again, and never counted as a plateau or a failure of the server. Without pinned cpus for
// the load generator its headroom cannot be checked, and the report says so.
func TestAClosedRoundChecksTheHeadroomOfTheLoadGenerator(t *testing.T) {
	for _, pass := range []bool{true, false} {
		r := plannedRound()
		r.Seconds, r.ClientCPU, r.Valid, r.Pass = 90, 90*1.7, true, pass
		judgeClosed(r, 75*time.Second, 4, 95, 2)
		if r.Valid || !r.GeneratorLimited || !strings.Contains(r.WhyInvalid, "used 1.70 cores of 2") {
			t.Errorf("pass %v: valid %v, limited %v %q", pass, r.Valid, r.GeneratorLimited, r.WhyInvalid)
		}
	}
	r := plannedRound()
	r.Seconds, r.ClientCPU, r.Valid, r.Pass = 90, 90*1.7, true, true
	if judgeClosed(r, 75*time.Second, 4, 95, 0); !r.Valid {
		t.Errorf("not pinned: %s", r.WhyInvalid)
	}
	sc := mustScenario("shop")
	res := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa"}}, Version: map[string]string{}, Sustain: map[string]*Sustain{},
		Config: map[string]string{"sessions": "8", "loadCpus": ""}, Closed: []Round{closedRound("tifa", sc.Name, 8, 1, 4000, 10, true)}}
	if md := res.Markdown(); !strings.Contains(md, "The load generator was not pinned to cpus of its own (`-load-cpus`), so whether it had cpus to spare was not checked.") {
		t.Errorf("the report does not say the headroom was not checked:\n%s", md)
	}
	res.Config["loadCpus"] = "1,5"
	if strings.Contains(res.Markdown(), "was not checked") {
		t.Error("the report says a pinned load generator was not checked")
	}
}

// A closed round that played every visit the data before the clock was made for (Capped) cannot be
// made longer: when it ends before the window it stays valid and is not resized. Too long, it is
// resized as any other.
func TestARoundCappedAtThePreparedVisitsIsKeptWhenShort(t *testing.T) {
	target := 75 * time.Second
	short := plannedRound()
	short.Seconds, short.Visits, short.Pass, short.Valid, short.Capped = 40, 3800, true, true, true
	if blocks := calibrate(short, target, 4, 95); blocks != 0 || !short.Valid {
		t.Errorf("a capped round that ended early: %d blocks, valid %v %s", blocks, short.Valid, short.WhyInvalid)
	}
	long := plannedRound()
	long.Seconds, long.Visits, long.Pass, long.Valid, long.Capped = 200, 3800, true, true, true
	if blocks := calibrate(long, target, 4, 95); blocks == 0 || long.Valid {
		t.Errorf("a capped round that ran too long: %d blocks, valid %v", blocks, long.Valid)
	}
}
