package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/tifatest"
)

// The route writes tifa.Stats in the text format of Prometheus, the render memo among it. Before
// the server starts every count is zero.
func TestGETWritesTheStatsOfTheProgram(t *testing.T) {
	c, w := tifatest.NewCtx(t, httptest.NewRequest("GET", "/metrics", nil))
	if err := GET(c); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	for _, line := range []string{"tifa_memo_entries 0\n", "tifa_memo_hits_total 0\n", "tifa_memo_misses_total 0\n", "tifa_memo_shared_total 0\n", "tifa_memo_same_page_total 0\n", "tifa_memo_evictions_total 0\n", "tifa_renders_total 0\n"} {
		if !strings.Contains(body, line) {
			t.Errorf("the metrics have no %q:\n%s", line, body)
		}
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("Content-Type %q", ct)
	}
}

// The counts of each route are a family of their own with the route as a label, the routes sorted,
// and the URLs that match no page under route="".
func TestWriteGivesTheMemoOfEachRoute(t *testing.T) {
	var s tifa.ServerStats
	s.Memo.Routes = map[string]tifa.MemoRouteStats{
		"/store/products/{product}": {Hits: 15, Misses: 50, SamePage: 40},
		"/store":                    {Hits: 120, Misses: 3, Shared: 1},
		"":                          {Misses: 2},
	}
	var b strings.Builder
	write(&b, s)
	want := `# HELP tifa_memo_route_hits_total Answered from an entry, by route.
# TYPE tifa_memo_route_hits_total counter
tifa_memo_route_hits_total{route=""} 0
tifa_memo_route_hits_total{route="/store"} 120
tifa_memo_route_hits_total{route="/store/products/{product}"} 15
`
	if !strings.Contains(b.String(), want) {
		t.Errorf("no hits by route:\n%s", b.String())
	}
	for _, line := range []string{
		`tifa_memo_route_misses_total{route="/store/products/{product}"} 50` + "\n",
		`tifa_memo_route_shared_total{route="/store"} 1` + "\n",
		`tifa_memo_route_same_page_total{route="/store/products/{product}"} 40` + "\n",
	} {
		if !strings.Contains(b.String(), line) {
			t.Errorf("the metrics have no %q:\n%s", line, b.String())
		}
	}
	if n := strings.Count(b.String(), "# TYPE tifa_memo_route_same_page_total counter\n"); n != 1 {
		t.Errorf("%d TYPE lines of the same pages by route", n)
	}
}
