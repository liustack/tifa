package compare

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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
	r.res = LoadResult{Renders: map[string]int{}, lat: map[string][]float64{}, Types: map[string]TypeStat{}}
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

func TestSearchStopsAtAPlateauAndKeepsTheHighestStepThatPasses(t *testing.T) {
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
	if chosen != curve[len(curve)-1].Users {
		t.Errorf("chose %d out of %+v", chosen, curve)
	}
}
