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
	gets atomic.Int64 // the GETs, the requests that can render a page
	seq  seq          // in order, the checks apart from the traffic
}

// warmBrowse is a browse run against fake servers: every launch starts a new one and notes it. The
// plans are one block of the site, each page view one request to its page, in the scenario sc.
func warmBrowse(t *testing.T, sc BrowseScenario, specs ...Spec) (*browse, func() []*launched) {
	t.Helper()
	var mu sync.Mutex
	var all []*launched
	set := GenerateSitePlanSet(1, 1)
	traces := map[int]Trace{}
	for _, p := range set.Plans {
		tr := Trace{Plan: p.ID}
		for _, s := range p.Steps {
			class := "nav"
			if s.Kind == StepLoad {
				class = "doc"
			}
			tr.Steps = append(tr.Steps, TraceStep{Kind: s.Kind, Page: s.Page, Reqs: []TraceReq{{Method: "GET", URL: s.Page, Class: class, End: 1, Status: 200}}})
		}
		traces[p.ID] = tr
	}
	b := &browse{
		o:       BrowseOptions{Log: io.Discard, Sessions: 2, Rounds: 1, Warm: 150 * time.Millisecond, Target: 200 * time.Millisecond, Scenarios: []BrowseScenario{sc}, Seed: 1},
		specs:   map[string]Spec{},
		servers: map[string][]string{},
		set:     &set,
		trace:   map[string]map[int]Trace{},
		res:     &BrowseResult{Blocks: map[string]int{}, Sustain: map[string]*Sustain{}},
	}
	for _, s := range specs {
		b.specs[s.Name] = s
		b.servers[sc.Name] = append(b.servers[sc.Name], s.Name)
		b.trace[traceName(s)] = traces
	}
	// The fake shows no page that a write changes: the check of the writes is the test's to set.
	b.verify = func(context.Context, *runner) error { return nil }
	b.launch = func(ctx context.Context, spec Spec) (*liveServer, error) {
		l := &launched{spec: spec.Name}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(verifyHeader) != "" {
				l.seq.add('V')
				w.Write([]byte("ok"))
				return
			}
			// The restock before a run with writes: the list of the products of a shop and an edit of
			// each, as the shop answers them, with no render.
			if r.URL.Path == "/compare-write/studio/products" || r.URL.Path == "/compare-write/weekend/products" {
				l.seq.add('R')
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"routes":{"app/projects/project+/products/route":{"status":200,"data":[{"slug":"oak-tray","priceCents":58000,"stock":6,"nameEn":"Oak Tray","nameZh":"原木托盘","descriptionEn":"Oak.","descriptionZh":"橡木。"}]}}}`))
				return
			}
			if r.Method == "PATCH" && (strings.HasPrefix(r.URL.Path, "/compare-write/studio/products/") || strings.HasPrefix(r.URL.Path, "/compare-write/weekend/products/")) {
				l.seq.add('R')
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{}`))
				return
			}
			l.hits.Add(1)
			if r.Method == "POST" && strings.Contains(r.URL.Path, "/store/products/") {
				l.seq.add('A')
			} else {
				l.seq.add('T')
			}
			if r.Method == "GET" {
				l.gets.Add(1)
			}
			// A write is answered as the shop answers it: a checkout with a 303, a created order with its id.
			switch {
			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/checkout"):
				w.Header().Set("Location", "/store/checkout?placed=SH-9001")
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

// busy are the servers that got a request: a server of the check of the writes gets none while the
// check is the stub of warmBrowse.
func busy(list []*launched) []*launched {
	return slices.DeleteFunc(list, func(l *launched) bool { return l.hits.Load() == 0 && !l.seq.has('V') })
}

func warmSpecs() (tifa, nocache, node Spec) {
	mk := func(name, fw string) Spec {
		return Spec{Target: Target{Name: name}, Framework: fw, Write: nodeWrite, Create: nodeCreate, StoreWrite: SameStorePath, EditProduct: nodeEditProduct, Products: nodeProducts}
	}
	return mk("tifa", "tifa"), mk("tifa-nocache", "tifa"), mk("nextjs", "nextjs")
}

// Tifa has no JIT and starts every measured round with an empty render memo, so a warm-up would put
// what the memo holds there before the clock starts. Before a measured round of Tifa, with or
// without the cache, nothing that renders reaches its server: it gets the GETs of the round and no
// other, and besides the writes of the round only the writes that make the carts of the returning
// visitors. Both kinds of setup happen, in this order: the restock (Restock, which renders nothing), then
// the carts of the returning visitors, then the round. The Node servers keep their warm-up (their JIT
// needs it).
func TestNothingThatRendersPrecedesTheTrafficOfTifa(t *testing.T) {
	tifa, nocache, node := warmSpecs()
	sc := mustScenario("shop")
	// own says whether a server got the requests of its round and no other: the GETs of the round, and
	// its writes and those of its setup. The fake counts the requests of the product list and its
	// edits apart ("R"), and an edit of a round is two of them, the list and the edit.
	own := func(l *launched, out *Round) bool {
		edits := out.WriteKinds[WriteEdit]
		return int(l.gets.Load()) == out.Requests-out.Writes-edits && int(l.hits.Load()) == out.Requests+out.SetupRequests-2*edits
	}
	// setUp checks that a server of a round was restocked, then got the carts of the setup, then the
	// round, with nothing between (Tifa), or the warm-up between the restock and the carts (Node).
	setUp := func(t *testing.T, l *launched, out *Round, warmed bool) {
		t.Helper()
		o := l.seq.order()
		rest := strings.TrimLeft(o, "R")
		if len(o)-len(rest) < 2 || strings.Count(rest, "R") != 2*out.WriteKinds[WriteEdit] {
			t.Errorf("the server of the round got %.40s... with %d edits of products in the round, want the restock (a list and its edits) before anything else", o, out.WriteKinds[WriteEdit])
		}
		if warmed {
			return
		}
		carts := strings.Repeat("A", out.SetupRequests)
		if out.SetupRequests == 0 || !strings.HasPrefix(rest, carts+"T") {
			t.Errorf("after the restock the server got %.40s..., want the %d adds of the carts, then the round", rest, out.SetupRequests)
		}
	}
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
			list := busy(servers())
			// The first server is the sizing run, thrown away. Then one server for each try of the round
			// (a round of this fake is never valid, so it is tried again).
			if len(list) != 1+len(b.res.Closed) || out.Requests == 0 {
				t.Fatalf("%d servers for %d rounds, %d requests in the last", len(list), len(b.res.Closed), out.Requests)
			}
			out = &b.res.Closed[0]
			if out.SetupRequests == 0 {
				t.Fatalf("the round made no cart for a returning visitor")
			}
			if own(list[1], out) == tc.warmed {
				t.Errorf("the server of the round got %d requests (%d GETs), the round counted %d (%d writes, %d of setup): warmed %v", list[1].hits.Load(), list[1].gets.Load(), out.Requests, out.Writes, out.SetupRequests, tc.warmed)
			}
			setUp(t, list[1], out, tc.warmed)
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
			list := busy(servers())
			if len(list) != 1 || out.Requests == 0 {
				t.Fatalf("%d servers, %d requests in the round", len(list), out.Requests)
			}
			if own(list[0], out) == tc.warmed {
				t.Errorf("the server of the round got %d requests (%d GETs), the round counted %d (%d writes, %d of setup): warmed %v", list[0].hits.Load(), list[0].gets.Load(), out.Requests, out.Writes, out.SetupRequests, tc.warmed)
			}
			if out.SetupRequests == 0 {
				t.Fatalf("the round made no cart for a returning visitor")
			}
			setUp(t, list[0], out, tc.warmed)
		})
	}
	// The read-only control is Tifa: its server gets the round and nothing else.
	b, servers := warmBrowse(t, sc, tifa)
	if err := b.control(context.Background(), sc); err != nil {
		t.Fatal(err)
	}
	list := busy(servers())
	if len(b.res.Control) != 1 || len(list) != 2 {
		t.Fatalf("%d control rounds on %d servers", len(b.res.Control), len(list))
	}
	if c := &b.res.Control[0]; !own(list[1], c) || c.Requests == 0 || c.Writes != 0 {
		t.Errorf("the control server got %d requests, the round counted %d (%d writes, %d of setup)", list[1].hits.Load(), c.Requests, c.Writes, c.SetupRequests)
	}
}

// The sizing of a cell runs on a server of its own, thrown away: no request of it reaches the
// server of a measured round, for Tifa as for the Node servers.
func TestSizingNeverReachesAMeasuredServer(t *testing.T) {
	tifa, _, node := warmSpecs()
	sc := mustScenario("shop")
	for _, spec := range []Spec{tifa, node} {
		b, servers := warmBrowse(t, sc, spec)
		if _, err := b.closedRound(context.Background(), spec.Name, sc, 2, 1); err != nil {
			t.Fatal(err)
		}
		list := busy(servers())
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

// seq is what one server got, in order: "V" for a request of the write check, "R" for one of the
// restock before a run with writes, "A" for an add to a cart (of the setup or of a round), and "T"
// for the rest of the traffic of a round (a warm-up, a sizing run or a measured round).
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

// The write check of a scenario renders pages, so it must not run on the server of a measured round
// nor on the one of a sizing run: no request that renders comes before their traffic, and the check
// runs on a server of its own. The restock comes before the traffic on both, and it renders nothing
// (Restock checks every answer for that). That holds for every attempt, closed and open, in every mix
// with writes, for Tifa with and without the cache. The check after a round stays on the server of the
// round, once its timing has ended.
func TestTheWriteCheckNeverPrecedesTheTrafficOfTifa(t *testing.T) {
	tifa, nocache, node := warmSpecs()
	for _, sc := range BrowseScenarios {
		mix := sc.Name
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
					list := busy(servers())
					if len(list) == 0 {
						t.Fatal("no server")
					}
					if !out.Pass {
						t.Fatalf("the round did not pass (%s), so the check after it was not reached", out.Why)
					}
					checks, after := 0, 0
					for i, l := range list {
						// An add, the carts of the setup among them, is traffic here.
						o := strings.ReplaceAll(l.seq.order(), "A", "T")
						// Every server of traffic is restocked before it (the edits of products of the traffic are
						// "R" too: TestNothingThatRendersPrecedesTheTrafficOfTifa counts them).
						if i := strings.IndexByte(o, 'T'); i >= 0 {
							if i == 0 || strings.Trim(o[:i], "R") != "" {
								t.Errorf("server of %s: %s, want the restock and nothing else before the traffic", spec.Name, o)
							}
						}
						o = strings.ReplaceAll(o, "R", "")
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
