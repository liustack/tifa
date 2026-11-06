package compare

import (
	"fmt"
	"slices"
	"strings"
)

// minP99 is how many samples a type of request needs before its P99 is printed.
const minP99 = 100

func fmtCell(values []float64, format string) string {
	if len(values) == 0 {
		return "-"
	}
	d := &metricDef{Format: fixed(format)}
	return cell(values, d)
}

// browseMarkdown writes the section of the browse comparison: what was assumed, the curve of each
// framework and the table of each scenario.
func (r *Record) browseMarkdown() string {
	var b strings.Builder
	bd := r.Browse
	fmt.Fprintf(&b, "Placement: %s", r.Placement.Note)
	if r.Placement.Server != "" {
		fmt.Fprintf(&b, " (server cpus %s, load cpus %s)", r.Placement.Server, r.Placement.Load)
	}
	fmt.Fprintf(&b, ". %d rounds at the chosen users, fresh servers each in a rotating order, %s of lead-in and then one common window of %s for all users. The search for the users took steps of %s.\n\n", r.Settings.Rounds, bd.Warm, bd.Window, bd.SearchWindow)
	b.WriteString("### Visits\n\n")
	b.WriteString("Each user plays recorded visits one after another. A visit is a full load of a page, then clicks on links of the page on screen and reloads of it, as a real browser did it against that framework (Chromium, 1280x800, no hover, the browser's own prefetching, caching and route discovery). The traces hold dynamic traffic only: documents and the data requests of the client router, with no scripts, styles, images or fonts. Cookies are kept within a visit and not across visits, and `If-None-Match` carries only the ETag that the visit got for that document. Back and forward are not modelled, since the browser's back/forward cache cannot be measured here.\n\n")
	b.WriteString("What a visit is made of is assumed, not measured: its length (4 page views on average is 75% soft navigations, 2 is 50%), that 10% of the page views after the first are reloads, and the rate of writes (a write to one of the three orders that accept one, after a page view, with probability 1/500 or 1/20). A write gives an order a new address, and in the fan-out run a new customer name too, which the lists, the stats and the other orders of that customer show. The conditions favor reuse of rendered pages: 58 pages that every visitor shares, almost no personalization and rare writes. Times are when the server's answer reached the client, not when a browser would have shown the page. CPU is the processor time of the whole server process tree, user and system, and includes writes and prefetches.\n\n")
	for _, sc := range bd.Scenarios {
		fmt.Fprintf(&b, "#### %s: %d page views a visit on average, a write after about %s page views%s\n\n", sc.Name, sc.Mean, fmt.Sprintf("1 in %.0f", 1/sc.WriteRate), map[bool]string{true: ", changing the customer too", false: ""}[sc.FanOut])
		b.WriteString("| | users | visits/s | CPU ms/visit | page views/s | CPU ms/view | requests/visit | body KB/visit | full load P50 / P99 ms (n) | navigation P50 / P99 ms (n) | reload P50 ms (n) | prefetch P50 / P99 ms (n) | write P50 ms (n) | peak RSS MiB |\n| --- |")
		b.WriteString(strings.Repeat(" --- |", 13) + "\n")
		var specs []string
		by := map[string][]BrowseRun{}
		for _, run := range bd.Runs {
			if run.Scenario != sc.Name {
				continue
			}
			if _, ok := by[run.Spec]; !ok {
				specs = append(specs, run.Spec)
			}
			by[run.Spec] = append(by[run.Spec], run)
		}
		for _, spec := range specs {
			runs := by[spec]
			per := func(f func(l *LoadResult) float64) []float64 {
				var out []float64
				for _, run := range runs {
					out = append(out, f(run.Load))
				}
				return out
			}
			visits := func(l *LoadResult) float64 { return max(1, float64(l.Visits)) }
			typ := func(kind string, f func(TypeStat) float64) []float64 {
				return per(func(l *LoadResult) float64 { return f(l.Types[kind]) })
			}
			n := func(kind string) int {
				total := 0
				for _, run := range runs {
					total += run.Load.Types[kind].N
				}
				return total
			}
			p99 := func(kind string) string {
				if n(kind) < minP99*len(runs) {
					return "-"
				}
				return fmtCell(typ(kind, func(t TypeStat) float64 { return t.P99 }), "%.0f")
			}
			p50 := func(kind string) string {
				if n(kind) == 0 {
					return "-"
				}
				return fmtCell(typ(kind, func(t TypeStat) float64 { return t.P50 }), "%.1f")
			}
			fmt.Fprintf(&b, "| %s | %d | %s | %s | %s | %s | %s | %s | %s / %s (%d) | %s / %s (%d) | %s (%d) | %s / %s (%d) | %s (%d) | %s |\n",
				runs[0].Label, runs[0].Load.Users,
				fmtCell(per(func(l *LoadResult) float64 { return float64(l.Visits) / l.Seconds }), "%.2f"),
				fmtCell(per(func(l *LoadResult) float64 { return 1000 * l.CPU / visits(l) }), "%.1f"),
				fmtCell(per(func(l *LoadResult) float64 { return float64(l.Views) / l.Seconds }), "%.2f"),
				fmtCell(per(func(l *LoadResult) float64 { return 1000 * l.CPU / max(1, float64(l.Views)) }), "%.1f"),
				fmtCell(per(func(l *LoadResult) float64 { return float64(l.Requests) / visits(l) }), "%.1f"),
				fmtCell(per(func(l *LoadResult) float64 { return float64(l.Bytes) / 1024 / visits(l) }), "%.1f"),
				p50("doc"), p99("doc"), n("doc"), p50("nav"), p99("nav"), n("nav"), p50("reload"), n("reload"), p50("prefetch"), p99("prefetch"), n("prefetch"), p50("write"), n("write"),
				fmtCell(per(func(l *LoadResult) float64 { return l.PeakRSS }), "%.0f"))
		}
		b.WriteString("\nP99 is printed where a type has at least 100 samples in each round.\n")
		if tifa, ok := by["tifa"]; ok {
			var rows []string
			for _, kind := range []string{"vm", "cache", "shared"} {
				rows = append(rows, fmt.Sprintf("%s %s", fmtCell(perVisit(tifa, func(l *LoadResult) float64 { return float64(l.Renders[kind]) }), "%.3f"), map[string]string{"vm": "renders", "cache": "cache hits", "shared": "shared renders"}[kind]))
			}
			rows = append(rows, fmtCell(perVisit(tifa, func(l *LoadResult) float64 { return float64(l.N304) }), "%.3f")+" 304 answers")
			fmt.Fprintf(&b, "\nTifa, per visit: %s.\n", strings.Join(rows, ", "))
		}
		b.WriteString("\n")
	}
	b.WriteString("#### The search for the users\n\nUsers go 1, 2, 4, ... until visits/s rises by under 3% for two steps in a row, or a step has errors, or the P99 of a full load or of a whole navigation passes 1 s. The highest step that passes is used for the rounds (marked with *).\n\n")
	b.WriteString("| scenario / server | steps: users: visits/s (full load P99 ms, navigation P99 ms) |\n| --- | --- |\n")
	var keys []string
	for k := range bd.Curves {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		var parts []string
		for _, p := range bd.Curves[k] {
			mark := ""
			if p.Users == bd.Chosen[k] {
				mark = "*"
			}
			parts = append(parts, fmt.Sprintf("%d%s: %.2f (%.0f, %.0f)%s", p.Users, mark, p.VisitsPerS, p.DocP99, p.NavP99, map[bool]string{true: "", false: " fails"}[p.Pass]))
		}
		fmt.Fprintf(&b, "| %s | %s |\n", k, strings.Join(parts, "; "))
	}
	fmt.Fprintf(&b, "\nTraces: ")
	var files []string
	for f := range bd.Traces {
		files = append(files, f)
	}
	slices.Sort(files)
	for i, f := range files {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s %d visits", f, bd.Traces[f])
		if d := bd.Dropped[f]; d > 0 {
			fmt.Fprintf(&b, " (%d dropped)", d)
		}
	}
	b.WriteString(".\n")
	return b.String()
}

func perVisit(runs []BrowseRun, f func(*LoadResult) float64) []float64 {
	var out []float64
	for _, run := range runs {
		out = append(out, f(run.Load)/max(1, float64(run.Load.Visits)))
	}
	return out
}
