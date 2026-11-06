package compare

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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
		case strings.HasPrefix(r.URL.Path, "/stream-"):
			// The headers and the start of the answer go out at once, and the rest is held.
			w.Write([]byte("start"))
			w.(http.Flusher).Flush()
			held.Add(1)
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			held.Add(-1)
			return
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
	for i := 0; i < 50 && held.Load() != 0; i++ {
		time.Sleep(10 * time.Millisecond)
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

func TestLoadTracesRejectsAnIncompleteSet(t *testing.T) {
	dir := t.TempDir()
	if err := WritePlans(dir, 1, 1); err != nil {
		t.Fatal(err)
	}
	set, err := ReadPlans(dir)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, skip int, bad bool) {
		var b strings.Builder
		for _, p := range set.Plans {
			if p.ID == skip {
				continue
			}
			steps := ""
			for i, s := range p.Steps {
				if i > 0 {
					steps += ","
				}
				steps += `{"kind":"` + s.Kind + `","page":"` + s.Page + `","reqs":[{"method":"GET","url":"` + s.Page + `","headers":{},"s":0,"e":1,"status":200,"class":"doc"}]}`
			}
			line := `{"plan":` + strconv.Itoa(p.ID) + `,"steps":[` + steps + `]}`
			if bad && p.ID == 3 {
				line = `{"plan":3,"error":"timeout"}`
			}
			b.WriteString(line + "\n")
		}
		os.WriteFile(filepath.Join(dir, name+".jsonl"), []byte(b.String()), 0o644)
	}
	write("a", -1, false)
	write("b", 7, false)
	write("c", -1, true)
	if _, traces, err := LoadTraces(dir, []string{"a"}); err != nil || len(traces["a"]) != BlockVisits {
		t.Fatalf("a complete set: %v", err)
	}
	if _, _, err := LoadTraces(dir, []string{"a", "b"}); err == nil || !strings.Contains(err.Error(), "b.jsonl") {
		t.Errorf("a set with a missing plan: %v", err)
	}
	if _, _, err := LoadTraces(dir, []string{"c"}); err == nil || !strings.Contains(err.Error(), "failed to record") {
		t.Errorf("a set with a failed visit: %v", err)
	}
}

func TestBlocksAndVisitsAreWholeBlocks(t *testing.T) {
	if got := BlocksFor(800, 75*time.Second, 4); got%4 != 0 || got < 4 || float64(got*BlockVisits)/800 < 70 {
		t.Errorf("blocks for 800 visits/s: %d", got)
	}
	if got := BlocksFor(0.5, 75*time.Second, 4); got != 4 {
		t.Errorf("a slow framework still plays whole passes of the plans: %d", got)
	}
	// Every open run plays whole cycles of all 380 plans: a part of a cycle has another page mix.
	if n := visitsFor(8, 30*time.Second, 380); n%380 != 0 || n < 240 {
		t.Errorf("visits for 8 visits/s: %d", n)
	}
	if visitsFor(0.1, time.Second, 380) != 380 || visitsFor(2000, time.Minute, 380)%380 != 0 {
		t.Error("an open run is whole cycles")
	}
}

func TestWholeCyclesHaveTheMixAndPartsOfOneDoNot(t *testing.T) {
	set := GeneratePlanSet(1, 4)
	w := &Workload{Plans: set.Plans}
	views := func(n int) float64 {
		v := 0
		for s := 0; s < n; s++ {
			v += len(w.planFor(s).Steps)
		}
		return float64(v) / float64(n)
	}
	if got := views(380); got != 4 {
		t.Errorf("a cycle has %v page views a visit", got)
	}
	if got := views(760); got != 4 {
		t.Errorf("two cycles have %v page views a visit", got)
	}
	// Parts of a cycle stray from 4 by more than the 1% a round may: the reason trials play whole cycles.
	strays := 0
	for _, n := range []int{95, 120, 190, 240, 285} {
		if g := views(n); g < 3.96 || g > 4.04 {
			strays++
		}
	}
	if strays == 0 {
		t.Error("every part of a cycle has 4 page views a visit within 1%: the test shows nothing")
	}
	if w.planFor(380).ID != w.planFor(0).ID {
		t.Error("the sequence does not wrap")
	}
}

func TestConfirmRatePublishesOnlyARateThatPassedAllItsRoundsAndKeepsEveryRound(t *testing.T) {
	var ran []float64
	run := func(passAt func(rate float64, round int) bool) func(float64, int) (*Round, error) {
		ran = nil
		return func(rate float64, round int) (*Round, error) {
			ran = append(ran, rate)
			return &Round{Rate: rate, Pass: passAt(rate, round)}, nil
		}
	}
	// 100 fails, 90 fails in its second round, 81 passes all three.
	rate, rounds, err := ConfirmRate(100, 4, 3, run(func(r float64, round int) bool {
		if r == 90 {
			return round == 1
		}
		return r < 85
	}))
	if err != nil || rate != 81 {
		t.Fatalf("rate %v, %v", rate, err)
	}
	if len(rounds) != 1+2+3 || rounds[0].Attempt != 1 || rounds[5].Attempt != 3 || rounds[5].Round != 3 {
		t.Errorf("%d rounds kept: %+v", len(rounds), rounds)
	}
	// The first try passes: one step 10% above it is tried, and published if it passes too.
	rate, rounds, _ = ConfirmRate(100, 4, 3, run(func(r float64, _ int) bool { return r <= 110 }))
	if rate != 110 || len(rounds) != 6 || rounds[3].Attempt != 2 {
		t.Errorf("a passing step up: rate %v, %d rounds", rate, len(rounds))
	}
	rate, rounds, _ = ConfirmRate(100, 4, 3, run(func(r float64, _ int) bool { return r <= 100 }))
	if rate != 100 || len(rounds) != 4 {
		t.Errorf("a failing step up leaves the first rate: %v, %d rounds", rate, len(rounds))
	}
	// Four tries that all fail: no rate, and no untested rate is made up.
	rate, rounds, _ = ConfirmRate(100, 4, 3, run(func(float64, int) bool { return false }))
	if rate != 0 || len(rounds) != 4 {
		t.Errorf("rate %v after %d rounds", rate, len(rounds))
	}
	for _, r := range ran {
		if r == 65.61 {
			t.Error("the rate after the last try was run or published")
		}
	}
}

func TestGrowingTellsALevelSeriesFromARunawayOne(t *testing.T) {
	level, climb, runaway := []float64{}, []float64{}, []float64{}
	for i := 0; i < 40; i++ {
		level = append(level, 100)
		climb = append(climb, min(100, float64(i)*10))
		runaway = append(runaway, 50+float64(i)*10)
	}
	if Growing(level) || Growing(climb) {
		t.Error("a series that levels off is growing")
	}
	if !Growing(runaway) {
		t.Error("a series that keeps growing is not")
	}
	// A backlog that grows by one a second for a minute, sampled twice a second: 100 + 0.5 i.
	slow, jitter := []float64{}, []float64{}
	for i := 0; i < 120; i++ {
		slow = append(slow, 100+0.5*float64(i))
		jitter = append(jitter, 100+float64(i%7)*3-9)
	}
	if !Growing(slow) {
		t.Error("a steady slow growth of 1 a second over 60 s is not caught")
	}
	if Growing(jitter) {
		t.Error("jitter around a level is growing")
	}
	if Growing([]float64{1, 2, 3}) {
		t.Error("too few samples")
	}
}

func TestACancelledRequestIsSentEvenWhenItsOffsetHasPassedAndLivesItsRecordedTime(t *testing.T) {
	srv, held := fakeServer(t)
	// The prefetch lived 40 ms in the browser, and its cancel is at 50 ms into the page view. The
	// document is slow (30 ms), so the prefetch, which starts after it, is sent at about 30 ms; and
	// its recorded 40 ms of life means it is cancelled at about 70 ms, not at 50.
	plans, traces := onePlan(TraceStep{Kind: StepLoad, Page: "/p", Reqs: []TraceReq{
		req("/slow-doc", "doc", 0, 10), cancelled("/hold-1", "prefetch", 15, 55),
	}})
	r := testRunner(srv, plans, traces, nil)
	start := time.Now()
	round := r.RunClosed(context.Background(), 1, 1, 10*time.Second)
	if d := time.Since(start); d < 65*time.Millisecond || d > time.Second {
		t.Errorf("visit took %v, want its prefetch to live its recorded 40 ms after being sent at 30 ms", d)
	}
	if round.Visits != 1 || round.Failures != 0 {
		t.Errorf("%+v", round)
	}
	for i := 0; i < 50 && held.Load() != 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if held.Load() != 0 {
		t.Error("the prefetch was not released")
	}
}

func TestAForegroundRequestCancelledAfterItsAnswerCameIsUsed(t *testing.T) {
	srv, held := fakeServer(t)
	// Next.js's router aborts the fetch of a click once it has what it needs: the answer began at
	// 10 ms and the cancel came at 14 ms. The page view is successful and the cancel comes 4 ms after the
	// answer begins to arrive, however late that is.
	step := TraceStep{Kind: StepClick, Page: "/p", Reqs: []TraceReq{cancelledAfterAnswer("/stream-1", "nav", 0, 10, 14)}}
	plans, traces := onePlan(step)
	round := testRunner(srv, plans, traces, nil).RunClosed(context.Background(), 1, 1, 10*time.Second)
	if round.PageViews != 1 || round.Failures != 0 || round.Cancelled != 1 || round.Responses["nav cancelled"] != 1 || round.Requests != 1 {
		t.Fatalf("%+v", round)
	}
	for i := 0; i < 50 && held.Load() != 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if held.Load() != 0 {
		t.Error("the request was not released")
	}
}

func cancelledAfterAnswer(url, class string, s, responded, e float64) TraceReq {
	r := cancelled(url, class, s, e)
	r.Responded = responded
	return r
}

func TestAnErrorStatusFailsEvenWhenTheRequestIsCancelledLater(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	// The browser cancelled it 4 ms after its answer began; the answer is a 500.
	plans, traces := onePlan(TraceStep{Kind: StepClick, Page: "/p", Reqs: []TraceReq{cancelledAfterAnswer("/x", "nav", 0, 10, 14), req("/ok", "nav", 0, 5)}})
	round := testRunner(srv, plans, traces, nil).RunClosed(context.Background(), 1, 1, 10*time.Second)
	if round.PageViews != 0 || round.Failures != 1 || !strings.Contains(round.FirstErr, "status 500") {
		t.Fatalf("a cancelled 500 counted as a good page view: %+v", round)
	}
}
