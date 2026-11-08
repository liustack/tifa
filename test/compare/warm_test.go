package compare

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// launched is one server a fake launch started, with the requests it got.
type launched struct {
	spec string
	hits atomic.Int64
}

// warmBrowse is a browse run against fake servers: every launch starts a new one and notes it. The
// plans are one block of 95 visits of a single page view, in a scenario with no writes.
func warmBrowse(t *testing.T, sc BrowseScenario, specs ...Spec) (*browse, func() []*launched) {
	t.Helper()
	var mu sync.Mutex
	var all []*launched
	set := &PlanSet{Blocks: 1}
	traces := map[int]Trace{}
	for i := range BlockVisits {
		set.Plans = append(set.Plans, Plan{ID: i, Steps: []PlanStep{{StepLoad, "/projects/studio"}}})
		traces[i] = Trace{Plan: i, Steps: []TraceStep{{Kind: StepLoad, Page: "/projects/studio", Reqs: []TraceReq{{Method: "GET", URL: "/projects/studio", Class: "doc", End: 1, Status: 200}}}}}
	}
	b := &browse{
		o:       BrowseOptions{Log: io.Discard, Sessions: 2, Rounds: 1, Warm: 150 * time.Millisecond, Target: time.Second, Scenarios: []BrowseScenario{sc}, Seed: 1},
		specs:   map[string]Spec{},
		servers: map[string][]string{},
		sets:    map[string]*PlanSet{sc.site(): set},
		trace:   map[string]map[string]map[int]Trace{sc.site(): {}},
		res:     &BrowseResult{Blocks: map[string]int{}, Sustain: map[string]*Sustain{}},
	}
	for _, s := range specs {
		b.specs[s.Name] = s
		b.servers[sc.Name] = append(b.servers[sc.Name], s.Name)
		b.trace[sc.site()][traceName(s)] = traces
	}
	b.launch = func(ctx context.Context, spec Spec) (*liveServer, error) {
		l := &launched{spec: spec.Name}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			l.hits.Add(1)
			w.Write([]byte("ok"))
		}))
		mu.Lock()
		all = append(all, l)
		mu.Unlock()
		return &liveServer{base: srv.URL, cpu: func() float64 { return 0 }, rss: func() int64 { return 0 }, stop: srv.Close}, nil
	}
	return b, func() []*launched {
		mu.Lock()
		defer mu.Unlock()
		return append([]*launched(nil), all...)
	}
}

func warmSpecs() (tifa, nocache, node Spec) {
	return Spec{Target: Target{Name: "tifa"}, Framework: "tifa"}, Spec{Target: Target{Name: "tifa-nocache"}, Framework: "tifa"}, Spec{Target: Target{Name: "nextjs"}, Framework: "nextjs"}
}

// Tifa has no JIT and starts every measured round with an empty render cache, so a warm-up would put
// what the cache holds there before the clock starts. A measured round of Tifa, with or without the
// cache, gets no request but its own, and the Node servers keep their warm-up (their JIT needs it).
func TestOnlyTheNodeServersGetAWarmUp(t *testing.T) {
	tifa, nocache, node := warmSpecs()
	sc := BrowseScenario{Name: "reads", Share: 0, Mix: MixRename}
	for _, tc := range []struct {
		spec   Spec
		warmed bool
	}{{tifa, false}, {nocache, false}, {node, true}} {
		t.Run("closed "+tc.spec.Name, func(t *testing.T) {
			b, servers := warmBrowse(t, sc, tc.spec)
			out, err := b.closedRound(context.Background(), tc.spec.Name, sc, 2, 1)
			if err != nil {
				t.Fatal(err)
			}
			list := servers()
			// The first server is the sizing run, thrown away. Then one server for each try of the round
			// (a round of this fake is never valid, so it is tried again).
			if len(list) != 1+len(b.res.Closed) || out.Requests == 0 {
				t.Fatalf("%d servers for %d rounds, %d requests in the last", len(list), len(b.res.Closed), out.Requests)
			}
			out = &b.res.Closed[0]
			if got := int(list[1].hits.Load()); (got > out.Requests) != tc.warmed || (!tc.warmed && got != out.Requests) {
				t.Errorf("the server of the round got %d requests, the round counted %d: warmed %v, want %v", got, out.Requests, got > out.Requests, tc.warmed)
			}
			if list[0].hits.Load() == 0 {
				t.Errorf("the sizing run sent nothing")
			}
		})
		t.Run("open "+tc.spec.Name, func(t *testing.T) {
			b, servers := warmBrowse(t, sc, tc.spec)
			out, err := b.openRound(context.Background(), tc.spec.Name, sc, 100, time.Second, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			list := servers()
			if len(list) != 1 || out.Requests == 0 {
				t.Fatalf("%d servers, %d requests in the round", len(list), out.Requests)
			}
			if got := int(list[0].hits.Load()); (got > out.Requests) != tc.warmed || (!tc.warmed && got != out.Requests) {
				t.Errorf("the server of the round got %d requests, the round counted %d: warmed %v, want %v", got, out.Requests, got > out.Requests, tc.warmed)
			}
		})
	}
	// The read-only control is Tifa: its server gets the round and nothing else.
	b, servers := warmBrowse(t, sc, tifa)
	if err := b.control(context.Background(), sc); err != nil {
		t.Fatal(err)
	}
	list := servers()
	if len(b.res.Control) != 1 || len(list) != 2 {
		t.Fatalf("%d control rounds on %d servers", len(b.res.Control), len(list))
	}
	if got, want := int(list[1].hits.Load()), b.res.Control[0].Requests; got != want || want == 0 {
		t.Errorf("the control server got %d requests, the round counted %d", got, want)
	}
}

// The sizing of a cell runs on a server of its own, thrown away: no request of it reaches the
// server of a measured round, for Tifa as for the Node servers.
func TestSizingNeverReachesAMeasuredServer(t *testing.T) {
	tifa, _, node := warmSpecs()
	sc := BrowseScenario{Name: "reads", Share: 0, Mix: MixRename}
	for _, spec := range []Spec{tifa, node} {
		b, servers := warmBrowse(t, sc, spec)
		if _, err := b.closedRound(context.Background(), spec.Name, sc, 2, 1); err != nil {
			t.Fatal(err)
		}
		list := servers()
		if len(list) < 2 {
			t.Fatalf("%s: %d servers", spec.Name, len(list))
		}
		if len(list) != 1+len(b.res.Closed) {
			t.Errorf("%s: %d servers for %d rounds: want one for the sizing and one a round", spec.Name, len(list), len(b.res.Closed))
		}
	}
}
