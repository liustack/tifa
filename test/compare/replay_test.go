package compare

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServer answers every GET at once, except /slow-* (30 ms), /hold-* (until the request is
// cancelled or 5 s) and /fail-* (500), and takes the PATCH of a write.
func fakeServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	var held atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/hold-"):
			held.Add(1)
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			held.Add(-1)
		case strings.HasPrefix(r.URL.Path, "/slow-"):
			time.Sleep(30 * time.Millisecond)
		case strings.HasPrefix(r.URL.Path, "/fail-"):
			http.Error(w, "boom", 500)
			return
		}
		w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return srv, &held
}

func req(url, class string, s, e float64) TraceReq {
	return TraceReq{Method: "GET", URL: url, Class: class, Start: s, End: e, Status: 200, Headers: map[string]string{"rsc": "1"}}
}

func cancelled(url, class string, s, e float64) TraceReq {
	r := req(url, class, s, e)
	r.Status, r.Cancel = 0, "net::ERR_ABORTED"
	return r
}

func testRunner(srv *httptest.Server, plans []Plan, traces map[int]Trace, writes map[[2]int]WriteOrder) *runner {
	return &runner{base: srv.URL, write: nodePath, cpu: func() float64 { return 0 }, rss: func() int64 { return 0 },
		wl: &Workload{Plans: plans, Traces: traces, Writes: writes}}
}

func onePlan(steps ...TraceStep) ([]Plan, map[int]Trace) {
	var ps []PlanStep
	for _, s := range steps {
		ps = append(ps, PlanStep{s.Kind, s.Page})
	}
	return []Plan{{ID: 0, Steps: ps}}, map[int]Trace{0: {Plan: 0, Steps: steps}}
}

func TestCancelledRequestsAreCancelledAtTheirOffsetAndTheirAnswerIsNotRead(t *testing.T) {
	srv, held := fakeServer(t)
	// A prefetch the browser cancelled 60 ms in, to a server that holds it for 5 s.
	plans, traces := onePlan(TraceStep{Kind: StepLoad, Page: "/projects/studio", Reqs: []TraceReq{
		req("/doc", "doc", 0, 5), cancelled("/hold-1", "prefetch", 10, 60),
	}})
	r := testRunner(srv, plans, traces, nil)
	start := time.Now()
	round := r.RunClosed(context.Background(), 1, 1, 10*time.Second)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("took %v: the replay waited for a cancelled request", d)
	}
	if round.Visits != 1 || round.Failures != 0 || round.PageViews != 1 {
		t.Fatalf("%+v", round)
	}
	if held.Load() != 0 {
		t.Errorf("%d requests still held at the server: the connection was not released", held.Load())
	}
	if d := time.Since(start); d < 55*time.Millisecond {
		t.Errorf("visit ended after %v, before the cancel offset of 60 ms", d)
	}
}

func TestACancelledForegroundRequestAloneIsNoSuccessfulPageView(t *testing.T) {
	srv, _ := fakeServer(t)
	plans, traces := onePlan(TraceStep{Kind: StepLoad, Page: "/p", Reqs: []TraceReq{cancelled("/hold-1", "doc", 0, 40)}})
	round := testRunner(srv, plans, traces, nil).RunClosed(context.Background(), 1, 1, 10*time.Second)
	if round.PageViews != 0 || round.Failures != 1 || round.Visits != 0 {
		t.Fatalf("%+v", round)
	}
	// With another foreground request that finished, the page view succeeds.
	plans, traces = onePlan(TraceStep{Kind: StepClick, Page: "/p", Reqs: []TraceReq{cancelled("/hold-1", "nav", 0, 40), req("/nav", "nav", 5, 20)}})
	round = testRunner(srv, plans, traces, nil).RunClosed(context.Background(), 1, 1, 10*time.Second)
	if round.PageViews != 1 || round.Failures != 0 || round.Cancelled != 1 {
		t.Fatalf("%+v", round)
	}
}

func TestAFailedPrefetchFailsTheVisitAndANavigationCountsTheWaitForItsDependencies(t *testing.T) {
	srv, _ := fakeServer(t)
	plans, traces := onePlan(TraceStep{Kind: StepLoad, Page: "/p", Reqs: []TraceReq{req("/doc", "doc", 0, 2), req("/fail-1", "prefetch", 3, 5)}})
	round := testRunner(srv, plans, traces, nil).RunClosed(context.Background(), 1, 1, 10*time.Second)
	if round.Visits != 0 || round.Failures != 1 {
		t.Errorf("a visit whose prefetch answered 500: %+v", round)
	}
	// The click's request starts after a slow prefetch has finished, so it waits for it: the click
	// is timed from the start of the page view, not from its first request.
	plans, traces = onePlan(TraceStep{Kind: StepClick, Page: "/p", Reqs: []TraceReq{req("/slow-pf", "prefetch", 0, 10), req("/n", "nav", 20, 25)}})
	round = testRunner(srv, plans, traces, nil).RunClosed(context.Background(), 1, 1, 10*time.Second)
	if round.Types["click"].N != 1 || round.Types["click"].P50 < 30 {
		t.Errorf("click latency %+v: the 30 ms wait for the prefetch is missing", round.Types["click"])
	}
}

func TestClosedRoundCountsWritesAndPageViewsAndJudgesTheMix(t *testing.T) {
	srv, _ := fakeServer(t)
	steps := []TraceStep{
		{Kind: StepLoad, Page: "/projects/studio", Reqs: []TraceReq{req("/a", "doc", 0, 2)}},
		{Kind: StepClick, Page: "/projects/studio/stats", Reqs: []TraceReq{req("/b", "nav", 0, 2)}},
		{Kind: StepClick, Page: "/projects/studio", Reqs: []TraceReq{req("/c", "nav", 0, 2)}},
		{Kind: StepClick, Page: "/projects/studio/stats", Reqs: []TraceReq{req("/d", "nav", 0, 2)}},
	}
	plans, traces := onePlan(steps...)
	// One write after the first page view: 1 write in 5 actions.
	r := testRunner(srv, plans, traces, map[[2]int]WriteOrder{{0, 0}: WritableOrders[0]})
	round := r.RunClosed(context.Background(), 2, 6, 10*time.Second)
	if round.Visits != 6 || round.PageViews != 24 || round.Writes != 6 || round.FullLoads != 6 || round.Clicks != 18 {
		t.Fatalf("%+v", round)
	}
	round.Judge(Mix{Share: 0.2})
	if !round.Valid || !round.Pass {
		t.Errorf("valid %v (%s), pass %v (%s)", round.Valid, round.WhyInvalid, round.Pass, round.Why)
	}
	round.Judge(Mix{Share: 0.5})
	if round.Valid {
		t.Error("a round with 20% writes is valid for the 50% mix")
	}
	if round.Types["write"].N != 6 || round.Types["load"].N != 6 || round.Types["click"].N != 18 {
		t.Errorf("types %+v", round.Types)
	}
}

func TestRuleCoversEveryClassAndABacklog(t *testing.T) {
	base := func() *Round {
		return &Round{Visits: 10, PageViews: 40, FullLoads: 10, Clicks: 30, WriteShare: 0.05, ClickShare: 0.75, ViewsPerVisit: 4,
			Types: map[string]TypeStat{"load": {N: 10, P99: 100}, "click": {N: 30, P99: 100}, "write": {N: 3, P99: 100}}}
	}
	r := base()
	r.Judge(Mix{Share: 0.05})
	if !r.Pass || !r.Valid {
		t.Fatalf("%s %s", r.Why, r.WhyInvalid)
	}
	for name, mod := range map[string]func(*Round){
		"load":    func(r *Round) { r.Types["load"] = TypeStat{N: 10, P99: 1200} },
		"click":   func(r *Round) { r.Types["click"] = TypeStat{N: 30, P99: 1200} },
		"write":   func(r *Round) { r.Types["write"] = TypeStat{N: 3, P99: 1200} },
		"failure": func(r *Round) { r.Failures = 1 },
		"backlog": func(r *Round) { r.TimedOut = true },
	} {
		r := base()
		mod(r)
		r.Judge(Mix{Share: 0.05})
		if r.Pass {
			t.Errorf("%s: the rule passes", name)
		}
	}
	r = base()
	r.WriteShare = 0.07
	r.Judge(Mix{Share: 0.05})
	if r.Valid {
		t.Error("a write share two points off is valid")
	}
}

func TestOpenModelScoresFromTheScheduledTimeAndFindsABacklog(t *testing.T) {
	srv, _ := fakeServer(t)
	steps := []TraceStep{
		{Kind: StepLoad, Page: "/projects/studio", Reqs: []TraceReq{req("/slow-a", "doc", 0, 30)}},
		{Kind: StepClick, Page: "/projects/studio/stats", Reqs: []TraceReq{req("/slow-b", "nav", 0, 30)}},
	}
	plans, traces := onePlan(steps...)
	r := testRunner(srv, plans, traces, nil)
	// 8 visits a second for 1 s: the two actions of a visit are 1 s apart.
	round := r.RunOpen(context.Background(), 8, 8, 5*time.Second, 0)
	if round.Visits != 8 || round.PageViews != 16 || round.TimedOut {
		t.Fatalf("%+v", round)
	}
	if round.Types["load"].P50 < 30 || round.Types["load"].P50 > 200 || round.Types["click"].P50 < 30 || round.Types["click"].P50 > 200 {
		t.Errorf("types %+v", round.Types)
	}
	if round.Seconds < 1.9 {
		t.Errorf("round took %.2f s, the second action of the last visit is planned 1 s after its start", round.Seconds)
	}
	// A server that holds everything: the visits cannot finish and the round times out.
	plans, traces = onePlan(TraceStep{Kind: StepLoad, Page: "/p", Reqs: []TraceReq{req("/hold-x", "doc", 0, 5)}})
	r = testRunner(srv, plans, traces, nil)
	round = r.RunOpen(context.Background(), 4, 4, 300*time.Millisecond, 0)
	if !round.TimedOut || round.Visits != 0 {
		t.Errorf("a hung server: %+v", round)
	}
	round.Judge(Mix{Share: 0.05})
	if round.Pass {
		t.Error("a hung server passes")
	}
}
