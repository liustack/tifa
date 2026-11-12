// Package metrics writes the state of the program for Prometheus, as docs/deploy.md of Tifa
// teaches: the VM pool and the render memo from tifa.Stats, and the memo of each route. The route
// renders nothing. The comparison of test/compare reads it to check that the render memo is empty
// when a round starts its clock, and how the memo answered in the round.
package metrics

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/liustack/tifa"
)

func GET(c *tifa.Ctx) error {
	var b strings.Builder
	write(&b, tifa.Stats())
	c.SetHeader("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, err := io.WriteString(c.Writer(), b.String())
	return err
}

func write(b *strings.Builder, s tifa.ServerStats) {
	metric := func(name, kind, help string, value float64) {
		fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n%s %g\n", name, help, name, kind, name, value)
	}
	metric("tifa_vms", "gauge", "VMs loaded.", float64(s.SSR.VMs))
	metric("tifa_vms_busy", "gauge", "VMs rendering.", float64(s.SSR.Busy))
	metric("tifa_renders_queued", "gauge", "Renders waiting for a VM.", float64(s.SSR.Queued))
	metric("tifa_renders_total", "counter", "Renders that ran in a VM.", float64(s.SSR.Renders))
	metric("tifa_renders_failed_total", "counter", "Renders that failed.", float64(s.SSR.Failed))
	metric("tifa_render_seconds_total", "counter", "Time the renders took.", s.SSR.RenderTime.Seconds())
	metric("tifa_renders_rejected_total", "counter", "Renders answered 503 as busy.", float64(s.SSR.Rejected))
	metric("tifa_memo_entries", "gauge", "Entries kept.", float64(s.Memo.Entries))
	metric("tifa_memo_hits_total", "counter", "Answered from an entry.", float64(s.Memo.Hits))
	metric("tifa_memo_misses_total", "counter", "Rendered by the request.", float64(s.Memo.Misses))
	metric("tifa_memo_shared_total", "counter", "Waited for another request.", float64(s.Memo.Shared))
	metric("tifa_memo_unchanged_total", "counter", "Rendered again with the same page as before.", float64(s.Memo.Unchanged))
	metric("tifa_memo_evictions_total", "counter", "Dropped to make room for another.", float64(s.Memo.Evictions))
	metric("tifa_memo_bytes", "gauge", "Bytes the render memo holds.", float64(s.MemoBytes))
	metric("tifa_memo_size_bytes", "gauge", "Bytes the render memo may hold.", float64(s.MemoSize))
	// The route is the pattern of the page, such as /store/products/{product}, and "" for the URLs
	// that match no page.
	routes := slices.Sorted(maps.Keys(s.Memo.Routes))
	byRoute := func(name, help string, value func(tifa.MemoRouteStats) uint64) {
		fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s counter\n", name, help, name)
		for _, route := range routes {
			fmt.Fprintf(b, "%s{route=%q} %d\n", name, route, value(s.Memo.Routes[route]))
		}
	}
	byRoute("tifa_memo_route_hits_total", "Answered from an entry, by route.", func(r tifa.MemoRouteStats) uint64 { return r.Hits })
	byRoute("tifa_memo_route_misses_total", "Rendered by the request, by route.", func(r tifa.MemoRouteStats) uint64 { return r.Misses })
	byRoute("tifa_memo_route_shared_total", "Waited for another request, by route.", func(r tifa.MemoRouteStats) uint64 { return r.Shared })
	byRoute("tifa_memo_route_unchanged_total", "Rendered again with the same page as before, by route.", func(r tifa.MemoRouteStats) uint64 { return r.Unchanged })
}
