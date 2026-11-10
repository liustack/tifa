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
	// phases are the requests the server got, as method, path and body, between two reads of its
	// metrics route (a spec with Spec.Metrics): before the first, then after each.
	mu     sync.Mutex
	phases [][]string
}

// phase is the requests the server got after its i-th read of the metrics route (0: before the
// first).
func (l *launched) phase(i int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if i >= len(l.phases) {
		return nil
	}
	return slices.Clone(l.phases[i])
}

// warmBrowse is a browse run against fake servers: every launch starts a new one and notes it. The
// plans are one block of the site, each page view one request to its page, in the scenario sc.
func warmBrowse(t *testing.T, sc BrowseScenario, specs ...Spec) (*browse, func() []*launched) {
	t.Helper()
	var mu sync.Mutex
	var all []*launched
	set := GenerateSitePlanSet(1, 1)
	traces := formTraces(set.Plans, func(p Plan) Trace {
		tr := Trace{Plan: p.ID}
		for _, s := range p.Steps {
			class := "nav"
			if s.Kind == StepLoad {
				class = "doc"
			}
			tr.Steps = append(tr.Steps, TraceStep{Kind: s.Kind, Page: s.Page, Reqs: []TraceReq{{Method: "GET", URL: s.Page, Class: class, End: 1, Status: 200}}})
		}
		return tr
	})
	b := &browse{
		o:       BrowseOptions{Log: io.Discard, Sessions: 2, Rounds: 1, Sizing: 150 * time.Millisecond, Target: 200 * time.Millisecond, Scenarios: []BrowseScenario{sc}, Seed: 1},
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
		l := &launched{spec: spec.Name, phases: [][]string{nil}}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			l.mu.Lock()
			if r.URL.Path == "/metrics" {
				l.phases = append(l.phases, nil)
			} else {
				l.phases[len(l.phases)-1] = append(l.phases[len(l.phases)-1], r.Method+" "+r.URL.RequestURI()+" "+string(body))
			}
			l.mu.Unlock()
			// The metrics route of Tifa (Spec.Metrics), with a render memo that holds nothing.
			if r.URL.Path == "/metrics" {
				l.seq.add('M')
				w.Write([]byte("tifa_memo_entries 0\ntifa_memo_hits_total 0\ntifa_memo_misses_total 0\ntifa_memo_shared_total 0\ntifa_memo_bytes 0\ntifa_memo_evictions_total 0\n"))
				return
			}
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
			switch {
			case r.Method == "POST" && (r.URL.Path == "/compare-write/studio" || r.URL.Path == "/compare-write/weekend"):
				l.seq.add('O') // an order staff place: the setup places one for each ship
			case r.Method == "POST" && strings.Contains(r.URL.Path, "/store/products/"):
				l.seq.add('A')
			default:
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
			if answerOld(w, r) {
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
	tifa, nocache = mk("tifa", "tifa"), mk("tifa-nocache", "tifa")
	tifa.Metrics, nocache.Metrics = "/metrics", "/metrics"
	return tifa, nocache, mk("nextjs", "nextjs")
}

// shipsIn counts the ships of visits 0 to n-1 of the workload of a spec in a scenario of b.
func shipsIn(t *testing.T, b *browse, sc BrowseScenario, spec string, n int) int {
	t.Helper()
	wl, err := NewWorkload(b.set, b.trace[traceName(b.specs[spec])], sc, b.o.Seed)
	if err != nil {
		t.Fatal(err)
	}
	ships := 0
	for s := range n {
		for _, w := range wl.writesOf(s) {
			if w.Kind == WriteShip {
				ships++
			}
		}
	}
	return ships
}

// Before the warm-up of a measured round nothing that renders reaches its server, for Tifa with
// and without the memo as for the Node servers: every kind of setup happens, in this order, the
// restock (Restock, which renders nothing), then an order for each ship of the visits the data is
// made for (the prepared visits and those of the warm-up after them), then the carts of their
// returning visitors, then, for Tifa, a read of its metrics route. Then come the warm-up and the
// round, and the server gets no other request than those.
func TestNothingThatRendersPrecedesTheWarmUpOfTifa(t *testing.T) {
	tifa, nocache, node := warmSpecs()
	sc := mustScenario("shop")
	// own says whether a server got the requests of its warm-up and its round and no other: their
	// GETs, and their writes and those of the setup. The fake counts the requests of the product
	// list and its edits apart ("R"), and an edit of the warm-up or of the round is one of them.
	own := func(l *launched, out *Round) bool {
		w := out.WarmUp
		if w == nil {
			return false
		}
		edits := out.WriteKinds[WriteEdit] + w.WriteKinds[WriteEdit]
		crawls := out.crawlerRequests() + w.Crawlers // the crawler requests are GETs that no count of people has
		return int(l.gets.Load()) == out.Requests-out.Writes+w.Requests-w.Writes+crawls && int(l.hits.Load()) == out.Requests+w.Requests+out.SetupRequests-edits+crawls
	}
	// setUp checks that a server of a round was restocked, then got the orders of the ships of the
	// data, then the carts, then (Tifa) the read of its metrics, then the warm-up, with nothing
	// between.
	setUp := func(t *testing.T, l *launched, out *Round, ships int, metrics bool) {
		t.Helper()
		o := l.seq.order()
		rest := strings.TrimLeft(o, "R")
		edits := out.WriteKinds[WriteEdit] + out.WarmUp.WriteKinds[WriteEdit]
		if len(o)-len(rest) < 2 || strings.Count(rest, "R") != edits {
			t.Errorf("the server of the round got %.40s... with %d edits of products in the warm-up and the round, want the restock (a list and its edits) before anything else", o, edits)
		}
		setup := strings.Repeat("O", ships) + strings.Repeat("A", out.SetupRequests-ships)
		if metrics {
			setup += "M"
		}
		if ships < max(1, out.Mix.Ships) || out.SetupRequests <= ships || !strings.HasPrefix(rest, setup+"T") {
			t.Errorf("after the restock the server got %.60s..., want the %d orders of the ships, then the %d adds of the carts, then the warm-up", rest, ships, out.SetupRequests-ships)
		}
		if reads := strings.Count(o, "M"); metrics && reads != 3 || !metrics && reads != 0 {
			t.Errorf("%d reads of the metrics, want 3 for Tifa (before and after the warm-up, after the round) and none for the others", reads)
		}
	}
	for _, spec := range []Spec{tifa, nocache, node} {
		metrics := spec.Metrics != ""
		t.Run("closed "+spec.Name, func(t *testing.T) {
			b, servers := warmBrowse(t, sc, spec)
			out, err := b.closedRound(context.Background(), spec.Name, sc, 2, 1)
			if err != nil {
				t.Fatal(err)
			}
			list := busy(servers())
			// The first server is the sizing run, thrown away. Then one server for each try of the round
			// (a try that is not valid is played again).
			if len(list) != 1+len(b.res.Closed) || out.Requests == 0 {
				t.Fatalf("%d servers for %d rounds, %d requests in the last", len(list), len(b.res.Closed), out.Requests)
			}
			// The requests of a try can be counted only when it ran whole. The sizing run is short, and when
			// it sizes the round too long for this machine the warm-up or the round is cut off at twice the
			// target: the server then got requests the client had in flight and did not count, and the try is
			// not valid and is played again with fewer visits. So the check takes the first try that no
			// timeout cut off, with the server of that try.
			try := slices.IndexFunc(b.res.Closed, func(r Round) bool { return !r.TimedOut && r.WarmUp != nil && !r.WarmUp.TimedOut })
			if try < 0 {
				t.Fatalf("every one of the %d tries of the round was cut off by its timeout: none can be counted", len(b.res.Closed))
			}
			out, server := &b.res.Closed[try], list[1+try]
			if out.SetupRequests == 0 {
				t.Fatalf("the round made no cart for a returning visitor")
			}
			if !own(server, out) {
				t.Errorf("the server of the round got %d requests (%d GETs), the warm-up and the round counted %+v and %d (%d writes, %d of setup)", server.hits.Load(), server.gets.Load(), out.WarmUp, out.Requests, out.Writes, out.SetupRequests)
			}
			setUp(t, server, out, shipsIn(t, b, sc, spec.Name, b.res.Prepared[sc.Name]+b.res.WarmVisits[sc.Name]), metrics)
			if list[0].hits.Load() == 0 {
				t.Errorf("the sizing run sent nothing")
			}
			if strings.Count(list[0].seq.order(), "M") > 1 {
				t.Errorf("the sizing run had a warm-up: %d reads of the metrics", strings.Count(list[0].seq.order(), "M"))
			}
		})
		t.Run("open "+spec.Name, func(t *testing.T) {
			b, servers := warmBrowse(t, sc, spec)
			out, err := b.openRound(context.Background(), spec.Name, sc, 100, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			list := busy(servers())
			if len(list) != 1 || out.Requests == 0 {
				t.Fatalf("%d servers, %d requests in the round", len(list), out.Requests)
			}
			if !own(list[0], out) {
				t.Errorf("the server of the round got %d requests (%d GETs), the warm-up and the round counted %+v and %d (%d writes, %d of setup)", list[0].hits.Load(), list[0].gets.Load(), out.WarmUp, out.Requests, out.Writes, out.SetupRequests)
			}
			if out.SetupRequests == 0 {
				t.Fatalf("the round made no cart for a returning visitor")
			}
			setUp(t, list[0], out, shipsIn(t, b, sc, spec.Name, 2*out.VisitsPlanned), metrics)
		})
	}
	// The read-only control is Tifa: its server gets the same warm-up with no writes, then the
	// round, and nothing else.
	// The control is one try, sized by a short sizing run of its own as a closed round is, so one that a
	// timeout cut off cannot be counted either: it is played again from the start, three times at most.
	var b *browse
	var servers func() []*launched
	for attempt := 1; ; attempt++ {
		b, servers = warmBrowse(t, sc, tifa)
		if err := b.control(context.Background(), sc); err != nil {
			t.Fatal(err)
		}
		if c := b.res.Control; attempt == 3 || len(c) != 1 || (!c[0].TimedOut && c[0].WarmUp != nil && !c[0].WarmUp.TimedOut) {
			break
		}
	}
	list := busy(servers())
	if len(b.res.Control) != 1 || len(list) < 2 {
		t.Fatalf("%d control rounds on %d servers", len(b.res.Control), len(list))
	}
	c := &b.res.Control[0]
	if !own(list[len(list)-1], c) || c.Requests == 0 || c.Writes != 0 || c.WarmUp.Writes != 0 {
		t.Errorf("the control server got %d requests, the warm-up and the round counted %+v and %d (%d writes, %d of setup)", list[len(list)-1].hits.Load(), c.WarmUp, c.Requests, c.Writes, c.SetupRequests)
	}
	if c.WarmUp.FirstVisit != b.res.Prepared[sc.Name] || c.WarmUp.VisitsPlanned != b.res.WarmVisits[sc.Name] {
		t.Errorf("the control warmed up with visits %d on, %d of them, want the warm-up of the scenario: %d on, %d", c.WarmUp.FirstVisit, c.WarmUp.VisitsPlanned, b.res.Prepared[sc.Name], b.res.WarmVisits[sc.Name])
	}
}

// Every server of a scenario gets the same warm-up before the clock of a measured round, Tifa as
// the Node servers: the visits after the prepared ones, as many as the warm-up of the scenario (a
// closed round, at any sessions) or as the round plays (an open round), with the same requests and
// the same writes. The warm-up sends the writes of its visits.
func TestEveryServerGetsTheSameWarmUp(t *testing.T) {
	tifa, nocache, node := warmSpecs()
	// The reads of the metrics route mark where the warm-up starts and ends on the fake servers, so
	// every spec has one here: what a warm-up sends does not depend on the spec.
	node.Metrics = "/metrics"
	sc := mustScenario("shop")
	checked := map[bool]int{}
	defer func() {
		if checked[false] < 3 || checked[true] != 3 {
			t.Errorf("%d warm-ups of closed rounds and %d of open rounds compared, want one for each of the three servers at least", checked[false], checked[true])
		}
	}()
	for _, open := range []bool{false, true} {
		b, servers := warmBrowse(t, sc, tifa, nocache, node)
		var first []string
		for _, spec := range []Spec{tifa, nocache, node} {
			before, tried := len(servers()), len(b.res.Closed)
			var out *Round
			var err error
			if open {
				out, err = b.openRound(context.Background(), spec.Name, sc, 100, time.Second)
			} else {
				out, err = b.closedRound(context.Background(), spec.Name, sc, 2, 1)
			}
			if err != nil {
				t.Fatal(err)
			}
			// The servers of the rounds the call started, one for each try (closed: the sizing runs come
			// before, and a try that is not valid is played again).
			var rounds []*launched
			for _, s := range servers()[before:] {
				if s.spec == spec.Name && len(s.phase(3)) == 0 && len(s.phase(2)) > 0 {
					rounds = append(rounds, s)
				}
			}
			if len(rounds) == 0 {
				t.Fatalf("open %v %s: no server of a round with a warm-up", open, spec.Name)
			}
			// The tries whose warm-up is compared, each with its server. An open round is one try. A closed
			// round is sized from a short sizing run, and one sized too long for this machine has its
			// warm-up cut off at twice the target: the server then got a part of the warm-up, and the try
			// is played again with fewer visits. Every try whose warm-up no timeout cut off is compared,
			// whatever its round did after: a round that was cut off or failed had the same warm-up.
			tries := []*Round{out}
			if !open {
				closed := b.res.Closed[tried:]
				if len(closed) != len(rounds) {
					t.Fatalf("closed %s: %d tries on %d servers", spec.Name, len(closed), len(rounds))
				}
				all := rounds
				tries, rounds = nil, nil
				for k := range closed {
					if w := closed[k].WarmUp; w != nil && !w.TimedOut {
						tries, rounds = append(tries, &closed[k]), append(rounds, all[k])
					}
				}
				if len(tries) == 0 {
					t.Fatalf("closed %s: the warm-up of every one of the %d tries was cut off by its timeout", spec.Name, len(closed))
				}
			}
			for k, out := range tries {
				warm := rounds[k].phase(1)
				slices.Sort(warm)
				first0, n := b.res.Prepared[sc.Name], b.res.WarmVisits[sc.Name]
				if open {
					first0, n = out.VisitsPlanned, out.VisitsPlanned
				}
				if out.WarmUp == nil || out.WarmUp.FirstVisit != first0 || out.WarmUp.VisitsPlanned != n || n == 0 {
					t.Errorf("open %v %s: the warm-up %+v, want %d visits from %d on", open, spec.Name, out.WarmUp, n, first0)
				}
				writes := slices.DeleteFunc(slices.Clone(warm), func(s string) bool { return strings.HasPrefix(s, "GET ") })
				if len(writes) == 0 || out.WarmUp.Writes == 0 || out.WarmUp.Writes != out.WarmUp.WritesPlanned {
					t.Errorf("open %v %s: the warm-up sent %d writes, %d of %d planned", open, spec.Name, len(writes), out.WarmUp.Writes, out.WarmUp.WritesPlanned)
				}
				if first == nil {
					first = warm
					continue
				}
				if !slices.Equal(first, warm) {
					d := firstDiff(first, warm)
					t.Errorf("open %v: the warm-up of %s (try %d) is not the first one of %s: %d and %d requests, first difference %q against %q", open, spec.Name, out.Attempt, tifa.Name, len(warm), len(first), at(warm, d), at(first, d))
				}
			}
			checked[open] += len(tries)
		}
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
						out, err = b.openRound(context.Background(), spec.Name, sc, 100, time.Second)
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
						// An add and an order, those of the setup among them, are traffic here.
						o := strings.NewReplacer("A", "T", "O", "T", "M", "").Replace(l.seq.order())
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
							if strings.Contains(o, "VT") {
								t.Errorf("server %d of %s: %s, a check before traffic", i, spec.Name, o)
							}
							if !strings.HasSuffix(strings.TrimRight(o, "V"), "T") || strings.Contains(strings.TrimRight(o, "V"), "V") {
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
