package compare

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type seen struct {
	method, uri, inm, cookie, rsc string
}

// fakeApp answers documents with an ETag, notes what it was sent, and sets a cookie on the first
// document of a visit.
func fakeApp(t *testing.T) (*httptest.Server, func() []seen) {
	var mu sync.Mutex
	var log []seen
	var inflight, peak atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inflight.Add(1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		defer inflight.Add(-1)
		mu.Lock()
		log = append(log, seen{r.Method, r.URL.RequestURI(), r.Header.Get("If-None-Match"), r.Header.Get("Cookie"), r.Header.Get("rsc")})
		mu.Unlock()
		if strings.Contains(r.URL.Path, "slow") {
			time.Sleep(30 * time.Millisecond)
		}
		if r.Method == "GET" && r.Header.Get("rsc") == "" {
			w.Header().Set("ETag", `W/"x"`)
			if r.Header.Get("If-None-Match") == `W/"x"` {
				w.WriteHeader(304)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "s", Value: "1", Path: "/"})
			w.Header().Set("X-Tifa-Render", "cache")
		}
		w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []seen { mu.Lock(); defer mu.Unlock(); return append([]seen(nil), log...) }
}

func newTestReplayer(base string, sc BrowseScenario, traces ...Trace) *replayer {
	r := &replayer{base: base, write: nodePath, sc: sc, seed: 1, traces: map[int]Trace{}, cpu: func() float64 { return 0 }, rss: func() int64 { return 0 }}
	for i, tr := range traces {
		tr.Plan = i
		r.traces[i] = tr
		r.plans = append(r.plans, Plan{ID: i, Steps: []PlanStep{{StepLoad, "/projects/studio"}}})
	}
	r.res = LoadResult{Renders: map[string]int{}, Responses: map[string]int{}, lat: map[string][]float64{}, Types: map[string]TypeStat{}}
	r.t0.Store(1)
	return r
}

func TestReplaySendsIfNoneMatchOnlyWithTheETagOfThisVisit(t *testing.T) {
	srv, log := fakeApp(t)
	doc := func(inm bool) TraceReq {
		return TraceReq{Method: "GET", URL: "/projects/studio", Class: "doc", INM: inm, End: 5}
	}
	tr := Trace{Steps: []TraceStep{
		{Kind: StepReload, Page: "/projects/studio", Reqs: []TraceReq{doc(true)}}, // no ETag yet: a plain GET
		{Kind: StepLoad, Page: "/projects/studio", Reqs: []TraceReq{doc(false)}},
		{Kind: StepReload, Page: "/projects/studio", Reqs: []TraceReq{doc(true)}}, // now it has one
	}}
	r := newTestReplayer(srv.URL, BrowseScenario{Name: "x", Mean: 4}, tr)
	if err := r.visit(context.Background(), http.DefaultTransport.(*http.Transport), tr, r.plans[0]); err != nil {
		t.Fatal(err)
	}
	got := log()
	if len(got) != 3 || got[0].inm != "" || got[1].inm != "" || got[2].inm != `W/"x"` {
		t.Fatalf("requests %+v", got)
	}
	if r.res.N304 != 1 || r.res.Visits != 1 || r.res.Views != 3 || r.res.Reloads != 2 {
		t.Errorf("result %+v", r.res)
	}
	// A second visit starts without the ETag and without the cookie.
	if err := r.visit(context.Background(), http.DefaultTransport.(*http.Transport), tr, r.plans[0]); err != nil {
		t.Fatal(err)
	}
	got = log()
	if got[3].inm != "" || got[3].cookie != "" {
		t.Errorf("the next visit started with %+v", got[3])
	}
	if got[0].cookie != "" || got[1].cookie == "" {
		t.Errorf("cookies within a visit: %+v %+v", got[1], got[2])
	}
}

func TestReplayKeepsTheParallelismOfTheBrowser(t *testing.T) {
	srv, log := fakeApp(t)
	// Three requests: a and b start together, c starts after a has finished.
	step := TraceStep{Kind: StepClick, Page: "/p", Reqs: []TraceReq{
		{Method: "GET", URL: "/slow-a", Class: "nav", Start: 0, End: 10, Headers: map[string]string{"rsc": "1"}},
		{Method: "GET", URL: "/slow-b", Class: "prefetch", Start: 1, End: 50, Headers: map[string]string{"rsc": "1"}},
		{Method: "GET", URL: "/c", Class: "nav", Start: 20, End: 30, Headers: map[string]string{"rsc": "1"}},
	}}
	tr := Trace{Steps: []TraceStep{step}}
	r := newTestReplayer(srv.URL, BrowseScenario{Name: "x", Mean: 4}, tr)
	start := time.Now()
	if err := r.visit(context.Background(), http.DefaultTransport.(*http.Transport), tr, r.plans[0]); err != nil {
		t.Fatal(err)
	}
	if got := log(); len(got) != 3 || got[0].uri != "/slow-a" && got[1].uri != "/slow-a" {
		t.Fatalf("requests %+v", got)
	}
	if d := time.Since(start); d > 150*time.Millisecond {
		t.Errorf("took %v: the requests ran one after another", d)
	}
	nav := r.res.lat["nav"]
	if len(nav) != 1 || nav[0] < 30 { // a (30 ms), then c: one navigation
		t.Errorf("navigation latencies %v", nav)
	}
	if len(r.res.lat["prefetch"]) != 1 {
		t.Errorf("prefetch latencies %v", r.res.lat["prefetch"])
	}
}

func TestReplayWritesAreTheirOwnRequestTypeAndTheTifaBuildIsSubstituted(t *testing.T) {
	srv, log := fakeApp(t)
	tr := Trace{Steps: []TraceStep{{Kind: StepClick, Page: "/projects/studio", Reqs: []TraceReq{
		{Method: "GET", URL: "/projects/studio?_tifa=deadbeef.001", Class: "nav", End: 5, Headers: map[string]string{"rsc": "1"}},
	}}}}
	r := newTestReplayer(srv.URL, BrowseScenario{Name: "x", Mean: 4, WriteRate: 1, FanOut: true}, tr)
	r.buildID = "cafe01"
	if err := r.visit(context.Background(), http.DefaultTransport.(*http.Transport), tr, r.plans[0]); err != nil {
		t.Fatal(err)
	}
	got := log()
	if len(got) != 2 || got[0].uri != "/projects/studio?_tifa=cafe01.001" || got[1].method != "PATCH" {
		t.Fatalf("requests %+v", got)
	}
	if len(r.res.lat["write"]) != 1 || len(r.res.lat["nav"]) != 1 {
		t.Errorf("latencies %v", r.res.lat)
	}
}

func TestSearchStopsAtAPlateauAndChoosesTheBestStepThatPasses(t *testing.T) {
	// A server that answers at the same speed however many ask: visits/s rises with the users until
	// they all wait on one lock.
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		time.Sleep(time.Millisecond)
		mu.Unlock()
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	tr := Trace{Steps: []TraceStep{{Kind: StepLoad, Page: "/p", Reqs: []TraceReq{{Method: "GET", URL: "/p", Class: "doc", End: 1}}}}}
	var trs []Trace
	for range 200 {
		trs = append(trs, tr)
	}
	r := newTestReplayer(srv.URL, BrowseScenario{Name: "x", Mean: 4}, trs...)
	curve, chosen, err := r.search(context.Background(), BrowseOptions{Log: io.Discard, SearchWarm: 50 * time.Millisecond, SearchWindow: 300 * time.Millisecond, MaxUsers: 64})
	if err != nil {
		t.Fatal(err)
	}
	if len(curve) < 3 || len(curve) >= 7 {
		t.Errorf("%d steps: %+v", len(curve), curve)
	}
	if chosen != ChooseStep(curve) {
		t.Errorf("chose %d out of %+v", chosen, curve)
	}
}

func TestReplayDoesNotDeadlockOnRequestsOfTheSameMillisecond(t *testing.T) {
	srv, _ := fakeApp(t)
	step := TraceStep{Kind: StepClick, Page: "/p", Reqs: []TraceReq{
		{Method: "GET", URL: "/a", Class: "nav", Start: 5, End: 5, Headers: map[string]string{"rsc": "1"}},
		{Method: "GET", URL: "/b", Class: "prefetch", Start: 5, End: 5, Headers: map[string]string{"rsc": "1"}},
		{Method: "GET", URL: "/c", Class: "nav", Start: 5, End: 9, Headers: map[string]string{"rsc": "1"}},
	}}
	tr := Trace{Steps: []TraceStep{step}}
	r := newTestReplayer(srv.URL, BrowseScenario{Name: "x", Mean: 4}, tr)
	done := make(chan error, 1)
	go func() { done <- r.visit(context.Background(), http.DefaultTransport.(*http.Transport), tr, r.plans[0]) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the visit never ended: requests of the same millisecond wait for each other")
	}
}

func TestRoundsFailByTheRuleOfTheSearchAndStepDownToTheNextPassingStep(t *testing.T) {
	run := func(doc, nav float64, errs int) BrowseRun {
		return BrowseRun{Load: &LoadResult{Visits: 1, Errors: errs, Types: map[string]TypeStat{"doc": {P99: doc}, "nav": {P99: nav}}}}
	}
	if got := RoundsFail([]BrowseRun{run(900, 950, 0), run(100, 100, 0)}); got != "" {
		t.Errorf("passing rounds fail: %q", got)
	}
	got := RoundsFail([]BrowseRun{run(900, 1079, 0), run(1006, 951, 0), run(1, 1, 2)})
	for _, want := range []string{"2 errors", "full load P99 up to 1006", "navigation P99 up to 1079"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lacks %q", got, want)
		}
	}
	curve := []CurvePoint{{Users: 1, Pass: true}, {Users: 2, Pass: true}, {Users: 4, Pass: false}, {Users: 8, Pass: true}, {Users: 16, Pass: true}}
	if StepDown(curve, 16) != 8 || StepDown(curve, 8) != 2 || StepDown(curve, 1) != 0 {
		t.Errorf("step down: %d %d %d", StepDown(curve, 16), StepDown(curve, 8), StepDown(curve, 1))
	}
}

func curveOf(v ...float64) []CurvePoint {
	var c []CurvePoint
	for i, x := range v {
		c = append(c, CurvePoint{Users: 1 << i, VisitsPerS: x, Pass: true})
	}
	return c
}

func TestOneDipDoesNotStopTheSearchButTwoSlowStepsDo(t *testing.T) {
	// 1: 639, 2: 892, 4: 853 is a dip: the search goes on to 8.
	if PlateauAfter(curveOf(639, 892, 853)) {
		t.Fatal("a single dip stopped the search")
	}
	// 8 is also under 3% above 892 (918.8): it stops, and the rounds run at 2 users, the best step.
	c := curveOf(639, 892, 853, 880)
	if !PlateauAfter(c) || ChooseStep(c) != 2 {
		t.Errorf("plateau %v, chosen %d", PlateauAfter(c), ChooseStep(c))
	}
	// 8 brings 950: the search goes on and the rounds would run at 8.
	c = curveOf(639, 892, 853, 950)
	if PlateauAfter(c) || ChooseStep(c) != 8 {
		t.Errorf("plateau %v, chosen %d", PlateauAfter(c), ChooseStep(c))
	}
	// Two falls in a row stop it too.
	if !PlateauAfter(curveOf(100, 200, 190, 180)) {
		t.Error("two falls did not stop the search")
	}
	if got := CurveEnd(curveOf(639, 892, 853, 880), 128); !strings.Contains(got, "Chosen: 2 users") || !strings.Contains(got, "under 3%") {
		t.Errorf("%q", got)
	}
}

func TestChooseStepTakesTheMostVisitsAndTiesGoToFewerUsers(t *testing.T) {
	c := curveOf(100, 200, 200, 150)
	if ChooseStep(c) != 2 {
		t.Errorf("chose %d", ChooseStep(c))
	}
	c[1].Pass = false
	if ChooseStep(c) != 4 {
		t.Errorf("a failing step was chosen: %d", ChooseStep(c))
	}
	if got := CurveEnd(curveOf(10, 20, 40), 4); !strings.Contains(got, "cap reached") || !strings.Contains(got, "Chosen: 4") {
		t.Errorf("%q", got)
	}
	failing := curveOf(10, 20)
	failing = append(failing, CurvePoint{Users: 4, VisitsPerS: 1, Why: "no visit completed"})
	if got := CurveEnd(failing, 128); !strings.Contains(got, "fails (no visit completed)") || !strings.Contains(got, "Chosen: 2") {
		t.Errorf("%q", got)
	}
}

func TestTraceCheckCountsHTMLToDataRequestsAndReloadHeaders(t *testing.T) {
	c := NewTraceCheck("Next.js", "baseline", &LoadResult{Users: 8, Requests: 10, Responses: map[string]int{
		"doc 200 text/html": 3, "nav 200 text/x-component": 5, "prefetch 200 text/html": 1, "nav 307 -": 1, "reload-document header": 2,
	}})
	if c.HTMLToData != 1 || c.NotOK != 1 || c.ReloadHeaders != 2 {
		t.Errorf("%+v", c)
	}
	if s := c.Summary(); !strings.Contains(s, "nav 200 text/x-component x5") || !strings.Contains(s, "HTML answers to data requests: 1") {
		t.Errorf("%s", s)
	}
}

func TestAServerThatHangsFailsTheStepInsteadOfPassingWithNothing(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	tr := Trace{Steps: []TraceStep{{Kind: StepLoad, Page: "/p", Reqs: []TraceReq{{Method: "GET", URL: "/p", Class: "doc", End: 1}}}}}
	r := newTestReplayer(srv.URL, BrowseScenario{Name: "x", Mean: 4}, tr, tr)
	res, err := r.RunLoad(context.Background(), 2, 50*time.Millisecond, 1200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if res.Visits != 0 || res.Errors != 0 || res.OpenRequests != 2 || res.OpenOldestMs < 1000 {
		t.Fatalf("visits %d, errors %d, %d requests open for %.0f ms", res.Visits, res.Errors, res.OpenRequests, res.OpenOldestMs)
	}
	p := CurvePoint{Users: 2, Visits: res.Visits, OpenOldest: res.OpenOldestMs}
	if p.why() == "" {
		t.Error("a step in which nothing completed passes")
	}
	if got := RoundsFail([]BrowseRun{{Load: res}}); !strings.Contains(got, "no visit") || !strings.Contains(got, "waited") {
		t.Errorf("rounds: %q", got)
	}
	// Open requests alone fail a step too: visits completed, but one has waited over a second.
	if (CurvePoint{Visits: 5, OpenOldest: 1500}).why() == "" {
		t.Error("a request open for 1.5 s passes")
	}
}

func TestAFailedPrefetchFailsTheVisit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/pf") {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	tr := Trace{Steps: []TraceStep{{Kind: StepLoad, Page: "/p", Reqs: []TraceReq{
		{Method: "GET", URL: "/p", Class: "doc", Start: 0, End: 2},
		{Method: "GET", URL: "/pf", Class: "prefetch", Start: 3, End: 5, Headers: map[string]string{"rsc": "1"}},
	}}}}
	r := newTestReplayer(srv.URL, BrowseScenario{Name: "x", Mean: 4}, tr)
	if err := r.visit(context.Background(), http.DefaultTransport.(*http.Transport), tr, r.plans[0]); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("a visit whose prefetch answered 500 ended with %v", err)
	}
	if r.res.Visits != 0 {
		t.Error("the visit counted")
	}
}

func TestNavigationTimeIncludesTheWaitBeforeItsFirstRequest(t *testing.T) {
	srv, _ := fakeApp(t)
	// The navigation request starts in the browser after a slow prefetch has finished, so it waits for it.
	step := TraceStep{Kind: StepClick, Page: "/p", Reqs: []TraceReq{
		{Method: "GET", URL: "/slow-pf", Class: "prefetch", Start: 0, End: 10, Headers: map[string]string{"rsc": "1"}},
		{Method: "GET", URL: "/n", Class: "nav", Start: 20, End: 25, Headers: map[string]string{"rsc": "1"}},
	}}
	tr := Trace{Steps: []TraceStep{step}}
	r := newTestReplayer(srv.URL, BrowseScenario{Name: "x", Mean: 4}, tr)
	if err := r.visit(context.Background(), http.DefaultTransport.(*http.Transport), tr, r.plans[0]); err != nil {
		t.Fatal(err)
	}
	if nav := r.res.lat["nav"]; len(nav) != 1 || nav[0] < 30 {
		t.Errorf("navigation latencies %v: the 30 ms wait for the prefetch is missing", nav)
	}
}

func TestOnlyPlansWithATraceForEveryServerAreReplayed(t *testing.T) {
	dir := t.TempDir()
	var plans []Plan
	for i := range 150 {
		plans = append(plans, Plan{ID: i, Steps: []PlanStep{{StepLoad, "/projects/studio"}}})
	}
	data, _ := json.Marshal(plans)
	os.WriteFile(PlansPath(dir, 4), data, 0o644)
	write := func(spec string, skip map[int]bool) {
		var b strings.Builder
		for i := range 150 {
			if skip[i] {
				continue
			}
			fmt.Fprintf(&b, `{"plan":%d,"steps":[{"kind":"load","page":"/projects/studio","reqs":[{"method":"GET","url":"/projects/studio","s":0,"e":1,"status":200,"class":"doc"}]}]}`+"\n", i)
		}
		os.WriteFile(filepath.Join(dir, spec+"-m4.jsonl"), []byte(b.String()), 0o644)
	}
	write("a", map[int]bool{3: true})
	write("b", map[int]bool{7: true, 9: true})
	specs := []Spec{{Target: Target{Name: "a"}}, {Target: Target{Name: "b"}}}
	sets, common, err := loadSets(dir, specs, 4)
	if err != nil {
		t.Fatal(err)
	}
	if common != 147 || len(sets["a"].traces) != 147 || len(sets["b"].traces) != 147 {
		t.Errorf("common %d, a %d, b %d", common, len(sets["a"].traces), len(sets["b"].traces))
	}
	for _, id := range []int{3, 7, 9} {
		if _, ok := sets["a"].traces[id]; ok {
			t.Errorf("a still replays plan %d, which b has no trace for", id)
		}
	}
}

func TestLoadingTracesFailsWhenARouterRequestLostItsHeaders(t *testing.T) {
	dir := t.TempDir()
	write := func(name, headers, url string) string {
		path := filepath.Join(dir, name)
		line := `{"plan":0,"steps":[{"kind":"click","page":"/p","reqs":[{"method":"GET","url":"` + url + `","headers":` + headers + `,"s":0,"e":5,"status":200,"class":"nav"}]}]}` + "\n"
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
