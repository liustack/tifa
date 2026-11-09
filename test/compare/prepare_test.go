package compare

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
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
