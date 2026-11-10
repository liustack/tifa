package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"

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
	for _, line := range []string{"tifa_memo_entries 0\n", "tifa_memo_hits_total 0\n", "tifa_memo_misses_total 0\n", "tifa_memo_shared_total 0\n", "tifa_memo_evictions_total 0\n", "tifa_renders_total 0\n"} {
		if !strings.Contains(body, line) {
			t.Errorf("the metrics have no %q:\n%s", line, body)
		}
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("Content-Type %q", ct)
	}
}
