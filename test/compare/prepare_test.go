package compare

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Every server of a scenario starts its closed rounds with the same data, whatever its own sizing
// said: the restock, the orders of the ships and the carts of the returning visitors are made for the
// visits of the scenario, not of the round, so a server sized to three blocks and one sized to one get
// the same requests before the clock, one by one, and the writes the plan sends in the visits both
// play (an edit of the stock is worked out from what the restock gave) are the same too.
func TestEveryServerOfAScenarioGetsTheSameDataBeforeTheClock(t *testing.T) {
	sc := mustScenario("shop")
	_, _, next := warmSpecs()
	start := next
	start.Target.Name, start.Framework = "start", "start"
	b, _ := warmBrowse(t, sc, next, start)
	logs := map[string]*setupLog{}
	b.launch = func(ctx context.Context, spec Spec) (*liveServer, error) {
		l := &setupLog{}
		logs[spec.Name] = l
		srv := httptest.NewServer(l)
		return &liveServer{base: srv.URL, cpu: func() float64 { return 0 }, rss: func() int64 { return 0 }, stop: srv.Close}, nil
	}
	// Two different sizings: nextjs played one block in its window, start three.
	bv := sc.blockVisits()
	b.res.Blocks[fmt.Sprintf("%s/%d/%s", sc.Name, b.o.Sessions, "nextjs")] = 1
	b.res.Blocks[fmt.Sprintf("%s/%d/%s", sc.Name, b.o.Sessions, "start")] = 3
	runners := map[string]*runner{}
	for _, fw := range []string{"nextjs", "start"} {
		prepared, err := b.prepared(context.Background(), sc)
		if err != nil {
			t.Fatal(err)
		}
		visits := b.res.Blocks[fmt.Sprintf("%s/%d/%s", sc.Name, b.o.Sessions, fw)] * bv
		if _, err := b.with(context.Background(), fw, sc, visits, prepared, func(r *runner) *Round {
			round := &Round{}
			if !r.setup(context.Background(), round, 0, visits) {
				t.Errorf("%s: the setup failed: %s", fw, round.SetupError)
			}
			runners[fw] = r
			return round
		}); err != nil {
			t.Fatal(err)
		}
	}
	a, c := logs["nextjs"], logs["start"]
	if len(a.inOrder) == 0 || len(a.carts) == 0 {
		t.Fatalf("the setup sent %d restock and order requests and %d cart requests", len(a.inOrder), len(a.carts))
	}
	if !slices.Equal(a.inOrder, c.inOrder) {
		t.Errorf("the restock and the orders differ: nextjs sent %d requests, start %d, first difference at %d", len(a.inOrder), len(c.inOrder), firstDiff(a.inOrder, c.inOrder))
	}
	slices.Sort(a.carts)
	slices.Sort(c.carts)
	if !slices.Equal(a.carts, c.carts) {
		t.Errorf("the carts differ: nextjs sent %d requests, start %d", len(a.carts), len(c.carts))
	}
	// The writes of the visits both servers play, as each runner would send them.
	sent := func(r *runner) []string {
		var out []string
		for s := range bv {
			writes := r.wl.writesOf(s)
			for _, after := range slices.Sorted(maps.Keys(writes)) {
				method, path, body, _, err := r.writeRequest(writes[after])
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, method+" "+path+" "+string(body))
			}
		}
		return out
	}
	wa, wc := sent(runners["nextjs"]), sent(runners["start"])
	if !slices.ContainsFunc(wa, func(s string) bool { return strings.Contains(s, `"stock"`) }) {
		t.Fatalf("no edit of the stock among the writes of a block: %v", wa)
	}
	if !slices.Equal(wa, wc) {
		i := firstDiff(wa, wc)
		t.Errorf("the planned writes differ at %d: nextjs %q, start %q", i, at(wa, i), at(wc, i))
	}
}

func firstDiff(a, b []string) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

func at(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return "(none)"
}

// setupLog is a fake server that answers the setup and the restock as the shop does, and keeps the
// requests that write: the restock and the orders in the order they came (they are sent one at a
// time), the carts apart (they are made at once).
type setupLog struct {
	mu      sync.Mutex
	inOrder []string
	carts   []string
	orders  int
}

func (l *setupLog) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	line := r.Method + " " + r.URL.Path + " " + string(body)
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/products"):
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"routes":{"app/projects/project+/products/route":{"status":200,"data":[{"slug":"oak-tray","priceCents":58000,"stock":6,"nameEn":"Oak Tray","nameZh":"原木托盘","descriptionEn":"Oak.","descriptionZh":"橡木。"}]}}}`))
	case r.Method == "PATCH":
		l.inOrder = append(l.inOrder, line)
		w.Write([]byte(`{}`))
	case r.Method == "POST" && strings.Contains(r.URL.Path, "/store/"):
		l.carts = append(l.carts, line)
		w.Write([]byte(`{}`))
	case r.Method == "POST":
		l.inOrder = append(l.inOrder, line)
		l.orders++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"SH-%d"}`, 9000+l.orders)
	default:
		w.Write([]byte("ok"))
	}
}

// playable caps the visits a closed round is sized to at the prepared ones, and a round that plays
// every prepared visit is capped: one sized to fewer is not, to as many or more is.
func TestARoundThatPlaysEveryPreparedVisitIsCapped(t *testing.T) {
	for _, tc := range []struct {
		sized, prepared, visits int
		capped                  bool
	}{
		{1020, 2040, 1020, false},
		{2040, 2040, 2040, true},
		{3060, 2040, 2040, true},
	} {
		if visits, capped := playable(tc.sized, tc.prepared); visits != tc.visits || capped != tc.capped {
			t.Errorf("sized to %d of %d prepared: %d visits, capped %v, want %d, %v", tc.sized, tc.prepared, visits, capped, tc.visits, tc.capped)
		}
	}
}

// A closed round sized to exactly the prepared visits cannot be made longer either: when it ends
// early it is kept. Here its first two tries are not valid (the setup of their servers fails), and
// the third, the last, plays every prepared visit in less than the window: it is the valid result.
func TestTheLastTryOfARoundAtThePreparedVisitsIsKeptWhenShort(t *testing.T) {
	sc := mustScenario("shop")
	_, _, node := warmSpecs()
	b, _ := warmBrowse(t, sc, node)
	b.o.Target = time.Minute
	bv := sc.blockVisits()
	b.res.Prepared = map[string]int{sc.Name: bv}
	b.res.Blocks[fmt.Sprintf("%s/%d/%s", sc.Name, b.o.Sessions, "nextjs")] = 1
	launch := b.launch
	var mu sync.Mutex
	failed := 0
	b.launch = func(ctx context.Context, spec Spec) (*liveServer, error) {
		s, err := launch(ctx, spec)
		if err != nil {
			return nil, err
		}
		target, _ := url.Parse(s.base)
		proxy := httputil.NewSingleHostReverseProxy(target)
		fail := false // this server refuses the orders of the setup
		front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" && (r.URL.Path == "/compare-write/studio" || r.URL.Path == "/compare-write/weekend") {
				mu.Lock()
				if !fail && failed < 2 {
					fail, failed = true, failed+1
				}
				refuse := fail
				mu.Unlock()
				if refuse {
					http.Error(w, "down", http.StatusInternalServerError)
					return
				}
			}
			proxy.ServeHTTP(w, r)
		}))
		stop := s.stop
		s.base, s.stop = front.URL, func() { front.Close(); stop() }
		return s, nil
	}
	out, err := b.closedRound(context.Background(), "nextjs", sc, b.o.Sessions, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.res.Closed) != maxTries || b.res.Closed[0].Valid || b.res.Closed[1].Valid {
		t.Fatalf("%d tries, want %d with the first two not valid: %+v", len(b.res.Closed), maxTries, b.res.Closed)
	}
	if out.Attempt != maxTries || !out.Capped || !out.Valid || !out.Pass {
		t.Errorf("the last try: attempt %d, capped %v, valid %v %s, pass %v, %.2f s", out.Attempt, out.Capped, out.Valid, out.WhyInvalid, out.Pass, out.Seconds)
	}
}
