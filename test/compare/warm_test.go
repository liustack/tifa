package compare

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// launched is one server a fake launch started, with the requests it got.
type launched struct {
	spec string
	hits atomic.Int64 // every request
	seq  seq          // in order, the checks apart from the traffic
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
		set.Plans = append(set.Plans, Plan{ID: i, Steps: []PlanStep{{StepLoad, "/projects/studio"}, {StepClick, "/projects/studio"}, {StepClick, "/projects/studio"}, {StepClick, "/projects/studio"}}})
		doc := TraceStep{Kind: StepLoad, Page: "/projects/studio", Reqs: []TraceReq{{Method: "GET", URL: "/projects/studio", Class: "doc", End: 1, Status: 200}}}
		nav := TraceStep{Kind: StepClick, Page: "/projects/studio", Reqs: []TraceReq{{Method: "GET", URL: "/projects/studio", Class: "nav", End: 1, Status: 200}}}
		traces[i] = Trace{Plan: i, Steps: []TraceStep{doc, nav, nav, nav}}
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
			if r.Header.Get(verifyHeader) != "" {
				l.seq.add('V')
				w.Write([]byte("ok"))
				return
			}
			l.hits.Add(1)
			l.seq.add('T')
			// A write is answered as the shop answers it: a checkout with a 303, a created order with its id.
			switch {
			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/checkout"):
				w.Header().Set("Location", "/store/orders/1")
				w.WriteHeader(303)
				return
			case r.Method == "POST" && r.URL.Path == "/store/cart":
				w.WriteHeader(204)
				return
			case r.Method == "POST" && !strings.HasPrefix(r.URL.Path, "/store/"):
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"id":"SH-9000","customer":"x"}`))
				return
			}
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
	mk := func(name, fw string) Spec {
		return Spec{Target: Target{Name: name}, Framework: fw, Write: nodeWrite, Create: nodeCreate, StoreWrite: SameStorePath}
	}
	return mk("tifa", "tifa"), mk("tifa-nocache", "tifa"), mk("nextjs", "nextjs")
}

// Tifa has no JIT and starts every measured round with an empty render memo, so a warm-up would put
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

// A request of a check that a fake server tells from the traffic of a round by this header.
const verifyHeader = "X-Test-Verify"

// seq is what one server got, in order: "V" for a request of the write check and "T" for the traffic
// of a round (a warm-up, a sizing run or a measured round).
type seq struct {
	mu   sync.Mutex
	kind []byte
}

func (s *seq) add(k byte) { s.mu.Lock(); s.kind = append(s.kind, k); s.mu.Unlock() }
func (s *seq) first() (byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.kind) == 0 {
		return 0, false
	}
	return s.kind[0], true
}

// order is the kinds of the requests as a string, such as "TTTVV".
func (s *seq) order() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.kind)
}

func (s *seq) has(k byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.kind, k)
}

// The write check of a scenario sends requests, so it must not run on the server of a measured round
// nor on the one of a sizing run: both start with the first request of their traffic, and the check
// runs on a server of its own. That holds for every attempt, closed and open, in every mix with
// writes, for Tifa with and without the cache. The check after a round stays on the server of the
// round, once its timing has ended.
func TestTheWriteCheckNeverPrecedesTheTrafficOfTifa(t *testing.T) {
	tifa, nocache, node := warmSpecs()
	for _, mix := range []string{MixRename, MixAdmin, MixStore} {
		sc := BrowseScenario{Name: "w-" + mix, Share: 0.05, Mix: mix}
		for _, spec := range []Spec{tifa, nocache, node} {
			for _, open := range []bool{false, true} {
				name := mix + "/" + spec.Name + "/closed"
				if open {
					name = mix + "/" + spec.Name + "/open"
				}
				t.Run(name, func(t *testing.T) {
					b, servers := warmBrowse(t, sc, spec)
					b.verify = func(ctx context.Context, r *runner) error {
						req, _ := http.NewRequestWithContext(ctx, "GET", r.base+"/check", nil)
						req.Header.Set(verifyHeader, "1")
						resp, err := http.DefaultClient.Do(req)
						if err == nil {
							resp.Body.Close()
						}
						return err
					}
					var out *Round
					var err error
					if open {
						out, err = b.openRound(context.Background(), spec.Name, sc, 100, time.Second, time.Second)
					} else {
						out, err = b.closedRound(context.Background(), spec.Name, sc, 2, 1)
					}
					if err != nil {
						t.Fatal(err)
					}
					list := servers()
					if len(list) == 0 {
						t.Fatal("no server")
					}
					if !out.Pass {
						t.Fatalf("the round did not pass (%s), so the check after it was not reached", out.Why)
					}
					checks, after := 0, 0
					for i, l := range list {
						o := l.seq.order()
						switch {
						case o != "" && strings.Trim(o, "V") == "":
							checks++ // a server of the check alone
						case strings.Contains(o, "V"):
							after++
							if strings.Contains(o, "VT") && !spec.WarmsUp() {
								t.Errorf("server %d of %s: %s, a check before traffic", i, spec.Name, o)
							}
							if !strings.HasSuffix(strings.TrimRight(o, "V"), "T") || strings.Contains(strings.TrimRight(o, "V"), "V") && !spec.WarmsUp() {
								t.Errorf("server %d of %s: %s, a check among the traffic", i, spec.Name, o)
							}
						}
					}
					if checks == 0 {
						t.Errorf("no server of its own got the check: %d servers", len(list))
					}
					if after == 0 {
						t.Errorf("no check after the round on its server: %d servers", len(list))
					}
				})
			}
		}
	}
}
