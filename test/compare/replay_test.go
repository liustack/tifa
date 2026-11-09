package compare

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

func testRunner(srv *httptest.Server, plans []Plan, traces map[int]Trace, writes map[[2]int]PlannedWrite) *runner {
	return &runner{base: srv.URL, write: nodePath, create: nodeCreate, storeWrite: samePath, products: nodeProducts, editProduct: nodeEditProduct, cpu: func() float64 { return 0 }, rss: func() int64 { return 0 }, seed: 1,
		wl: &Workload{Plans: plans, Traces: traces, Writes: writes, Scenario: BrowseScenario{Name: "x"}}}
}

// samePath sends a write of the storefront to the path of the plan.
func samePath(path string) string { return path }

// testWrite is a write of a workload made by hand: an add to the cart, which the fake servers answer
// with a 200.
func testWrite() PlannedWrite {
	return PlannedWrite{Kind: WriteAdd, Store: &StoreRequest{Kind: WriteAdd, Method: "POST", Path: "/store/products/oak-tray", Body: []byte(`{"color":"oak","quantity":1}`), Status: 200}}
}

// newTestRunner is a runner of a scenario whose plans are the traces given, in order.
func newTestRunner(base string, sc BrowseScenario, traces ...Trace) *runner {
	wl := &Workload{Traces: map[int]Trace{}, Writes: map[[2]int]PlannedWrite{}, Scenario: sc}
	for i, tr := range traces {
		tr.Plan = i
		wl.Traces[i] = tr
		p := Plan{ID: i}
		for _, s := range tr.Steps {
			p.Steps = append(p.Steps, PlanStep{s.Kind, s.Page})
		}
		wl.Plans = append(wl.Plans, p)
	}
	return &runner{base: base, write: nodePath, create: nodeCreate, storeWrite: samePath, products: nodeProducts, editProduct: nodeEditProduct, wl: wl, seed: 1, cpu: func() float64 { return 0 }, rss: func() int64 { return 0 }}
}

// visitOnce plays visit seq of the runner's sequence and returns what it saw, with the error of the
// visit if it failed.
func visitOnce(r *runner, a *acc, seq int) error {
	if !r.runVisit(context.Background(), http.DefaultTransport.(*http.Transport), a, seq, pacer{}) {
		return fmt.Errorf("the visit failed: %s", a.firstErr)
	}
	return nil
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
	r := testRunner(srv, plans, traces, map[[2]int]PlannedWrite{{0, 0}: testWrite()})
	round := r.RunClosed(context.Background(), 2, 6, 10*time.Second)
	if round.Visits != 6 || round.PageViews != 24 || round.Writes != 6 || round.FullLoads != 6 || round.Clicks != 18 {
		t.Fatalf("%+v", round)
	}
	round.Judge(BrowseScenario{})
	if !round.Valid || !round.Pass {
		t.Errorf("valid %v (%s), pass %v (%s)", round.Valid, round.WhyInvalid, round.Pass, round.Why)
	}
	if round.Types["write"].N != 6 || round.Types["load"].N != 6 || round.Types["click"].N != 18 {
		t.Errorf("types %+v", round.Types)
	}
}

func TestRuleCoversEveryClassAndABacklog(t *testing.T) {
	base := func() *Round {
		return &Round{Model: "open", VisitsPlanned: 10, VisitsStarted: 10, PageViewsPlanned: 40, ClicksPlanned: 30, WritesPlanned: 2, Visits: 10, PageViews: 40, FullLoads: 10, Clicks: 30,
			Types: map[string]TypeStat{"load": {N: 10, P99: 100}, "click": {N: 30, P99: 100}, "write": {N: 3, P99: 100}}}
	}
	r := base()
	r.Judge(BrowseScenario{})
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
		r.Judge(BrowseScenario{})
		if r.Pass {
			t.Errorf("%s: the rule passes", name)
		}
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
	round := r.RunOpen(context.Background(), 8, 8, 5*time.Second, 0, false)
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
	round = r.RunOpen(context.Background(), 4, 4, 300*time.Millisecond, 0, false)
	if !round.TimedOut || round.Visits != 0 {
		t.Errorf("a hung server: %+v", round)
	}
	round.Judge(BrowseScenario{})
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
	if _, traces, err := LoadTraces(dir, []string{"a"}); err != nil || len(traces["a"]) != SiteBlockPlans {
		t.Fatalf("a complete set: %v", err)
	}
	if _, _, err := LoadTraces(dir, []string{"a", "b"}); err == nil || !strings.Contains(err.Error(), "b.jsonl") {
		t.Errorf("a set with a missing plan: %v", err)
	}
	if _, _, err := LoadTraces(dir, []string{"c"}); err == nil || !strings.Contains(err.Error(), "failed to record") {
		t.Errorf("a set with a failed visit: %v", err)
	}
	// Plans that are not those of the site are refused: too few for their blocks, or of another site.
	os.WriteFile(PlansPath(dir), []byte(`{"seed":1,"blocks":1,"site":"shop","plans":[]}`), 0o644)
	if _, err := ReadPlans(dir); err == nil {
		t.Error("a file of no plans was read")
	}
	data, _ := json.Marshal(PlanSet{Seed: 1, Blocks: set.Blocks, Site: "store", Plans: set.Plans})
	os.WriteFile(PlansPath(dir), data, 0o644)
	if _, err := ReadPlans(dir); err == nil || !strings.Contains(err.Error(), "site") {
		t.Errorf("the plans of another site were read: %v", err)
	}
}

func TestPlanAndTraceFilesOfTheSite(t *testing.T) {
	if PlansPath("d") != "d/plans.json" || TracePath("d/", "tifa") != "d/tifa.jsonl" || TracePath("d", "nextjs") != "d/nextjs.jsonl" {
		t.Error("the files of the site changed their names")
	}
}

func TestReplaySendsIfNoneMatchOnlyWithTheETagOfThisVisit(t *testing.T) {
	srv, log := fakeApp(t)
	tr := Trace{Steps: []TraceStep{
		{Kind: StepLoad, Page: "/projects/studio", Reqs: []TraceReq{{Method: "GET", URL: "/projects/studio", Class: "doc", INM: true, End: 5, Status: 200}}},  // no ETag yet: a plain GET
		{Kind: StepClick, Page: "/projects/studio", Reqs: []TraceReq{{Method: "GET", URL: "/projects/studio", Class: "nav", INM: true, End: 5, Status: 200}}}, // now it has one
	}}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, tr)
	a := newAcc()
	if err := visitOnce(r, a, 0); err != nil {
		t.Fatal(err)
	}
	got := log()
	if len(got) != 2 || got[0].inm != "" || got[1].inm != `W/"x"` {
		t.Fatalf("requests %+v", got)
	}
	if a.n304 != 1 || a.visits != 1 || a.views != 2 {
		t.Errorf("%d answers 304, %d visits, %d page views", a.n304, a.visits, a.views)
	}
	// A second visit starts without the ETag and without the cookie.
	if err := visitOnce(r, a, 0); err != nil {
		t.Fatal(err)
	}
	got = log()
	if got[2].inm != "" || got[2].cookie != "" {
		t.Errorf("the next visit started with %+v", got[2])
	}
	if got[0].cookie != "" || got[1].cookie == "" {
		t.Errorf("cookies within a visit: %+v %+v", got[0], got[1])
	}
}

type seen struct {
	method, uri, inm, cookie, rsc string
}

// fakeApp answers GETs with an ETag (and a 304 to a request that has it), notes what it was sent,
// and sets a cookie.
func fakeApp(t *testing.T) (*httptest.Server, func() []seen) {
	var mu sync.Mutex
	var log []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		log = append(log, seen{r.Method, r.URL.RequestURI(), r.Header.Get("If-None-Match"), r.Header.Get("Cookie"), r.Header.Get("rsc")})
		mu.Unlock()
		if r.Method == "GET" {
			w.Header().Set("ETag", `W/"x"`)
			if r.Header.Get("If-None-Match") == `W/"x"` {
				w.WriteHeader(304)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "s", Value: "1", Path: "/"})
		}
		w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []seen { mu.Lock(); defer mu.Unlock(); return append([]seen(nil), log...) }
}

func TestReplayWritesAreTheirOwnRequestTypeAndTheTifaBuildIsSubstituted(t *testing.T) {
	srv, log := fakeApp(t)
	tr := Trace{Steps: []TraceStep{{Kind: StepLoad, Page: "/projects/studio", Reqs: []TraceReq{
		{Method: "GET", URL: "/projects/studio?_tifa=deadbeef.001", Class: "doc", End: 5, Status: 200},
	}}}}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, tr)
	r.wl.Writes[[2]int{0, 0}] = testWrite()
	r.buildID = "cafe01"
	a := newAcc()
	if err := visitOnce(r, a, 0); err != nil {
		t.Fatal(err)
	}
	got := log()
	if len(got) != 2 || got[0].uri != "/projects/studio?_tifa=cafe01.001" || got[1].method != "POST" || got[1].uri != "/store/products/oak-tray" {
		t.Fatalf("requests %+v", got)
	}
	if len(a.lat["write"]) != 1 || len(a.lat["write add"]) != 1 || a.kinds[WriteAdd] != 1 || a.responses["write add 200 text/plain"] != 1 {
		t.Errorf("latencies %v, responses %v", a.lat, a.responses)
	}
}

// A warm-up sends no writes, so every round starts with the data that a fresh server has.
func TestAWarmUpSendsNoWrites(t *testing.T) {
	srv, log := fakeApp(t)
	tr := Trace{Steps: []TraceStep{{Kind: StepLoad, Page: "/projects/studio", Reqs: []TraceReq{{Method: "GET", URL: "/projects/studio", Class: "doc", End: 1, Status: 200}}}}}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, tr)
	r.wl.Writes[[2]int{0, 0}] = testWrite()
	if rate := r.Warm(context.Background(), 2, 100*time.Millisecond); rate <= 0 {
		t.Fatalf("the warm-up completed no visit")
	}
	r.RunOpen(context.Background(), 50, 5, time.Second, 0, true)
	for _, s := range log() {
		if s.method != "GET" {
			t.Fatalf("a warm-up sent %+v", s)
		}
	}
	if round := r.RunClosed(context.Background(), 1, 2, 10*time.Second); round.Writes != 2 {
		t.Errorf("the round after it sent %d writes, want 2", round.Writes)
	}
}

func TestBlocksAndVisitsAreWholeBlocks(t *testing.T) {
	if got := BlocksFor(800, 75*time.Second, 4, 95); got%4 != 0 || got < 4 || float64(got*95)/800 < 70 {
		t.Errorf("blocks for 800 visits/s: %d", got)
	}
	if got := BlocksFor(0.5, 75*time.Second, 4, 95); got != 4 {
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

// A cycle of the site plays, for each block of plans, its 1000 shopper visits and the staff visits
// of the scenario, placed among them. Every block of the cycle has the mix of the scenario exactly:
// the staff share and their edits and ships by quota, and the shopper mix of the plans. The staff
// visits go through all the staff plans over the cycles, and a read-only scenario plans no write.
func TestEveryBlockOfTheCycleHasTheMixOfItsScenario(t *testing.T) {
	for _, tc := range []struct {
		name               string
		staff, edits, ship int
	}{{"shop", 20, 4, 6}, {"shop-staff-x2", 42, 17, 13}} {
		w := siteWorkload(t, 5, 2, tc.name)
		bv := w.blockVisits()
		if bv != ShopperBlockVisits+tc.staff || w.cycleLen() != 2*bv {
			t.Fatalf("%s: %d visits a block, a cycle of %d", tc.name, bv, w.cycleLen())
		}
		staffPlans := map[int]bool{}
		for b := range 6 {
			var r Round
			w.plan(&r, b*bv, bv, false)
			m := r.Mix
			want := PlannedMix{Shoppers: ShopperBlockVisits, Staff: tc.staff, ShopperViews: 2600, Returning: 200, Adds: 66, Checkouts: 13, Edits: tc.edits, Ships: tc.ship}
			if m == nil || *m != want {
				t.Fatalf("%s block %d of the run plans %+v, want %+v", tc.name, b, m, want)
			}
			if why := m.why(w.Scenario); why != "" {
				t.Errorf("%s block %d: %s", tc.name, b, why)
			}
			for s := b * bv; s < (b+1)*bv; s++ {
				if p := w.planFor(s); p.Role == RoleStaff {
					staffPlans[p.ID] = true
				}
			}
		}
		if len(staffPlans) != 2*StaffBlockPlans {
			t.Errorf("%s: the staff visits played %d of the %d staff plans", tc.name, len(staffPlans), 2*StaffBlockPlans)
		}
		if w.planFor(w.cycleLen()+3).ID != w.planFor(3).ID && w.planFor(3).Role == RoleShopper {
			t.Errorf("%s: the shopper visits do not wrap with the cycle", tc.name)
		}
		reads := w.Scenario
		reads.ReadOnly = true
		w.Scenario = reads
		var r Round
		w.plan(&r, 0, bv, false)
		if r.WritesPlanned != 0 || r.Mix.Adds+r.Mix.Edits+r.Mix.Ships+r.Mix.Checkouts != 0 {
			t.Errorf("%s read-only: %d writes planned", tc.name, r.WritesPlanned)
		}
	}
}

func TestConfirmRatePublishesOnlyARateThatPassedAllItsRoundsAndKeepsEveryRound(t *testing.T) {
	var ran []float64
	run := func(passAt func(rate float64, round int) bool) func(float64, int) (*Round, error) {
		ran = nil
		return func(rate float64, round int) (*Round, error) {
			ran = append(ran, rate)
			return &Round{Rate: rate, Pass: passAt(rate, round), Valid: true}, nil
		}
	}
	// 100 fails, 90 fails in its second round, 81 passes all three.
	rate, rounds, _, err := ConfirmRate(100, 4, 3, run(func(r float64, round int) bool {
		if r == 90 {
			return round == 1
		}
		return r < 85
	}), 0)
	if err != nil || rate != 81 {
		t.Fatalf("rate %v, %v", rate, err)
	}
	if len(rounds) != 1+2+3 || rounds[0].Attempt != 1 || rounds[5].Attempt != 3 || rounds[5].Round != 3 {
		t.Errorf("%d rounds kept: %+v", len(rounds), rounds)
	}
	// The first try passes: one step 10% above it is tried, and published if it passes too.
	rate, rounds, _, _ = ConfirmRate(100, 4, 3, run(func(r float64, _ int) bool { return r <= 110 }), 0)
	if rate != 110 || len(rounds) != 6 || rounds[3].Attempt != 2 {
		t.Errorf("a passing step up: rate %v, %d rounds", rate, len(rounds))
	}
	rate, rounds, _, _ = ConfirmRate(100, 4, 3, run(func(r float64, _ int) bool { return r <= 100 }), 0)
	if rate != 100 || len(rounds) != 4 {
		t.Errorf("a failing step up leaves the first rate: %v, %d rounds", rate, len(rounds))
	}
	// Four tries that all fail: no rate, and no untested rate is made up.
	rate, rounds, _, _ = ConfirmRate(100, 4, 3, run(func(float64, int) bool { return false }), 0)
	if rate != 0 || len(rounds) != 4 {
		t.Errorf("rate %v after %d rounds", rate, len(rounds))
	}
	for _, r := range ran {
		if r == 65.61 {
			t.Error("the rate after the last try was run or published")
		}
	}
	// The step up stays within the cap of the search.
	rate, rounds, _, _ = ConfirmRate(100, 4, 3, run(func(r float64, _ int) bool { return true }), 105)
	if rate != 100 || len(rounds) != 3 {
		t.Errorf("a step up over the cap: rate %v, %d rounds", rate, len(rounds))
	}
}

func TestGrowingTellsALevelSeriesFromARunawayOne(t *testing.T) {
	level, climb, runaway := []float64{}, []float64{}, []float64{}
	for i := 0; i < 40; i++ {
		level = append(level, 100)
		climb = append(climb, min(100, float64(i)*10))
		runaway = append(runaway, 50+float64(i)*10)
	}
	if Growing(level, 10) || Growing(climb, 10) {
		t.Error("a series that levels off is growing")
	}
	if !Growing(runaway, 10) {
		t.Error("a series that keeps growing is not")
	}
	// A backlog that grows by one a second for a minute, sampled twice a second: 100 + 0.5 i.
	slow, jitter := []float64{}, []float64{}
	for i := 0; i < 120; i++ {
		slow = append(slow, 100+0.5*float64(i))
		jitter = append(jitter, 100+float64(i%7)*3-9)
	}
	if !Growing(slow, 24) {
		t.Error("a steady slow growth of 1 a second over 60 s is not caught")
	}
	if Growing(jitter, 24) {
		t.Error("jitter around a level is growing")
	}
	if Growing([]float64{1, 2, 3}, 0) || Growing(runaway[:30], 10) {
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

func TestReplayKeepsTheParallelismOfTheBrowser(t *testing.T) {
	srv, _ := fakeServer(t)
	// Three requests: a and b start together, c starts after a has finished.
	tr := Trace{Steps: []TraceStep{{Kind: StepLoad, Page: "/p", Reqs: []TraceReq{req("/doc", "doc", 0, 1)}}, {Kind: StepClick, Page: "/q", Reqs: []TraceReq{
		req("/slow-a", "nav", 0, 30), req("/slow-b", "prefetch", 1, 50), req("/c", "nav", 40, 45),
	}}}}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, tr)
	a := newAcc()
	start := time.Now()
	if err := visitOnce(r, a, 0); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 150*time.Millisecond {
		t.Errorf("took %v: the requests ran one after another", d)
	}
	if click := a.lat["click"]; len(click) != 1 || click[0] < 30 { // a (30 ms), then c: one click
		t.Errorf("click latencies %v", click)
	}
	if len(a.lat["prefetch"]) != 1 {
		t.Errorf("prefetch latencies %v", a.lat["prefetch"])
	}
}

func TestReplayDoesNotDeadlockOnRequestsOfTheSameMillisecond(t *testing.T) {
	srv, _ := fakeServer(t)
	tr := Trace{Steps: []TraceStep{{Kind: StepLoad, Page: "/p", Reqs: []TraceReq{
		req("/a", "doc", 5, 5), req("/b", "prefetch", 5, 5), req("/c", "nav", 5, 9),
	}}}}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, tr)
	done := make(chan error, 1)
	go func() { done <- visitOnce(r, newAcc(), 0) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the visit never ended: requests of the same millisecond wait for each other")
	}
}

func TestLoadingTracesFailsWhenARouterRequestLostItsHeaders(t *testing.T) {
	dir := t.TempDir()
	write := func(name, headers, url string) string {
		path := filepath.Join(dir, name)
		line := `{"plan":0,"steps":[{"kind":"load","page":"/p","reqs":[{"method":"GET","url":"/p","headers":{},"s":0,"e":1,"status":200,"class":"doc"}]},{"kind":"click","page":"/q","reqs":[{"method":"GET","url":"` + url + `","headers":` + headers + `,"s":0,"e":5,"status":200,"class":"nav"}]}]}` + "\n"
		os.WriteFile(path, []byte(line), 0o644)
		return path
	}
	if _, _, err := ReadTraces(write("ok.jsonl", `{"rsc":"1"}`, "/projects/studio?_rsc=abc")); err != nil {
		t.Errorf("a request with its header: %v", err)
	}
	if _, _, err := ReadTraces(write("lost.jsonl", `{}`, "/projects/studio?_rsc=abc")); err == nil || !strings.Contains(err.Error(), "rsc") {
		t.Errorf("a _rsc request without rsc: %v", err)
	}
	if _, _, err := ReadTraces(write("fn.jsonl", `{"accept":"x"}`, "/_serverFn/abc?payload=1")); err == nil || !strings.Contains(err.Error(), "x-tsr-serverfn") {
		t.Errorf("a server function call without its header: %v", err)
	}
	if _, _, err := ReadTraces(write("tifa.jsonl", `{}`, "/projects/studio?_tifa=ab.001")); err != nil {
		t.Errorf("a Tifa request needs no router header: %v", err)
	}
}

func TestAClosedRoundTakesSixtyToOneHundredTwentySecondsByDefault(t *testing.T) {
	if lo, hi := roundLength(75 * time.Second); lo != 60 || hi != 120 {
		t.Errorf("a round of the default target takes %v to %v s", lo, hi)
	}
	if lo, hi := roundLength(5 * time.Second); lo != 4 || hi != 8 {
		t.Errorf("a short trial takes %v to %v s", lo, hi)
	}
}

// The render of each full load and of each navigation data answer is counted by the kind of page of
// its page view, from X-Tifa-Render, so the report can tell which pages the writes keep from the
// render memo. A data answer that renders the page in a VM costs what a document does.
func TestRendersAreCountedByKindOfPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/data":
			w.Header().Set("X-Tifa-Render", "vm")
		case r.URL.Path == "/projects/studio":
			w.Header().Set("X-Tifa-Render", "memo")
		case strings.HasPrefix(r.URL.Path, "/projects/studio/orders/"):
			w.Header().Set("X-Tifa-Render", "vm")
		default:
			w.Header().Set("X-Tifa-Render", "none")
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	load := func(page string) Trace {
		return Trace{Steps: []TraceStep{{Kind: StepLoad, Page: page, Reqs: []TraceReq{req(page, "doc", 0, 1), req("/data", "nav", 2, 3)}}}}
	}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, load("/projects/studio"), load("/projects/studio/orders/SH-1041"), load("/projects/studio"))
	a := newAcc()
	for seq := range 3 {
		if err := visitOnce(r, a, seq); err != nil {
			t.Fatal(err)
		}
	}
	var round Round
	a.round(&round, 0, 0, 0, 0, time.Now(), time.Now(), 0)
	if got := fmt.Sprint(round.PageRenders); got != "map[staff list:map[memo:2 vm:2] staff order:map[vm:2]]" {
		t.Errorf("renders by page %s", got)
	}
	if got := fmt.Sprint(round.Renders); got != "map[memo:2 vm:4]" {
		t.Errorf("renders %s", got)
	}
}

// Tifa starts every round with an empty render memo, so the first answer of each kind of page is a
// render: a round whose first answer of a kind came from the memo is not valid (something filled it
// before the round). The answers of the private pages, the cart and the checkout, that came from the
// memo are counted apart.
func TestTheFirstAnswerOfEachKindOfPageMustBeARender(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer func(path string, n int) string // how the n-th answer of a path is made
		valid  bool
	}{
		{"renders first", func(_ string, n int) string { return map[bool]string{true: RenderVM, false: RenderHit}[n == 1] }, true},
		{"shared first", func(_ string, n int) string { return map[bool]string{true: RenderShared, false: RenderHit}[n == 1] }, true},
		{"a hit first", func(path string, n int) string {
			if path == "/store/cart" {
				return RenderHit
			}
			return RenderVM
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			seen := map[string]int{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				seen[r.URL.Path]++
				n := seen[r.URL.Path]
				mu.Unlock()
				w.Header().Set(RenderHeader, tc.answer(r.URL.Path, n))
				w.Write([]byte("ok"))
			}))
			defer srv.Close()
			load := func(page string) Trace {
				return Trace{Steps: []TraceStep{{Kind: StepLoad, Page: page, Reqs: []TraceReq{req(page, "doc", 0, 1)}}, {Kind: StepClick, Page: page, Reqs: []TraceReq{req(page, "nav", 0, 1)}}}}
			}
			r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, load("/store/cart"), load("/store/products/oak-tray"), load("/store/checkout"))
			round := r.RunClosed(context.Background(), 1, 3, 10*time.Second)
			round.Judge(BrowseScenario{})
			if round.Valid != tc.valid || !tc.valid && !strings.Contains(round.WhyInvalid, "first answer of a cart page") {
				t.Errorf("first answers %v: valid %v (%s)", round.FirstRenders, round.Valid, round.WhyInvalid)
			}
			// The second answers of the cart and of the checkout, or both answers of the cart.
			if round.PrivateHits != 2 {
				t.Errorf("%d answers of the cart and the checkout came from the memo, want 2: %v", round.PrivateHits, round.PageRenders)
			}
		})
	}
}

// The body a request read before its cancel is counted: the client read those bytes.
func TestTheBytesReadBeforeACancelAreCounted(t *testing.T) {
	srv, _ := fakeServer(t)
	step := TraceStep{Kind: StepClick, Page: "/p", Reqs: []TraceReq{cancelledAfterAnswer("/stream-1", "nav", 0, 10, 14)}}
	plans, traces := onePlan(step)
	round := testRunner(srv, plans, traces, nil).RunClosed(context.Background(), 1, 1, 10*time.Second)
	if round.Cancelled != 1 || round.Bytes != int64(len("start")) {
		t.Fatalf("cancelled %d, bytes %d, want 1 and %d", round.Cancelled, round.Bytes, len("start"))
	}
}

// A visit whose context ends between two actions waits for its background requests before it
// returns, as on every other way out, so that a round ends after its last request.
func TestAVisitEndedBetweenActionsWaitsForItsBackgroundRequests(t *testing.T) {
	srv, _ := fakeServer(t)
	plans, traces := onePlan(
		TraceStep{Kind: StepLoad, Page: "/p", Reqs: []TraceReq{req("/doc", "doc", 0, 5), req("/hold-1", "prefetch", 1, 5000)}},
		TraceStep{Kind: StepClick, Page: "/q", Reqs: []TraceReq{req("/nav", "nav", 0, 5)}})
	for i := 0; i < 20; i++ {
		r := testRunner(srv, plans, traces, nil)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		a := newAcc()
		r.runVisit(ctx, http.DefaultTransport.(*http.Transport), a, 0, pacer{open: true, start: time.Now(), reads: true})
		left := r.inflight.Load()
		cancel()
		if left != 0 {
			t.Fatalf("visit %d returned with %d requests still in flight", i, left)
		}
	}
}

// The sizing run plays the mix of its round, writes included, so that a server whose writes make it
// slower is not sized from the reads alone.
func TestTheSizingRunSendsTheWritesOfItsMix(t *testing.T) {
	srv, log := fakeApp(t)
	tr := Trace{Steps: []TraceStep{{Kind: StepLoad, Page: "/projects/studio", Reqs: []TraceReq{{Method: "GET", URL: "/projects/studio", Class: "doc", End: 1, Status: 200}}}}}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, tr)
	r.wl.Writes[[2]int{0, 0}] = testWrite()
	if rate, _ := r.Size(context.Background(), 2, 100*time.Millisecond, 1000); rate <= 0 {
		t.Fatalf("the sizing run completed no visit")
	}
	writes := 0
	for _, s := range log() {
		if s.method != "GET" {
			writes++
		}
	}
	if writes == 0 {
		t.Error("the sizing run sent no write")
	}
}

// A closed round counts the visits it played, the ones that failed too.

// A write or a page that the shop answers with 409 or 422 counts as refused as soon as its status
// arrives, even when its body then cannot be read, and the round it is in is not valid.
func TestARefusedAnswerMakesItsRoundInvalid(t *testing.T) {
	for _, tc := range []struct{ name, refuse string }{{"a write whose body is cut", "POST"}, {"a page", "GET"}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.refuse {
					w.Write([]byte("ok"))
					return
				}
				if r.Method == "GET" {
					w.WriteHeader(http.StatusConflict)
					return
				}
				// A 422 that says it is gzip and is not: reading its body fails.
				w.Header().Set("Content-Encoding", "gzip")
				w.WriteHeader(http.StatusUnprocessableEntity)
				w.Write([]byte("not gzip"))
			}))
			defer srv.Close()
			tr := Trace{Steps: []TraceStep{{Kind: StepLoad, Page: "/projects/studio", Reqs: []TraceReq{
				{Method: "GET", URL: "/projects/studio", Class: "doc", End: 5, Status: 200},
			}}}}
			r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, tr)
			r.wl.Writes[[2]int{0, 0}] = testWrite()
			a := newAcc()
			if err := visitOnce(r, a, 0); err == nil {
				t.Fatal("the visit did not fail")
			}
			round := plannedRound()
			now := time.Now()
			a.round(round, 0, 0, 0, 0, now, now.Add(time.Second), 0)
			round.Judge(BrowseScenario{})
			if round.Refused != 1 || round.Valid || !strings.Contains(round.WhyInvalid, "refused 1 requests") {
				t.Errorf("refused %d, valid %v (%s): want one refused answer and an invalid round", round.Refused, round.Valid, round.WhyInvalid)
			}
		})
	}
}
