package compare

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

func fmtCell(values []float64, format string) string {
	if len(values) == 0 {
		return "-"
	}
	return cell(values, &metricDef{Format: fixed(format)})
}

// browseLabels name the specs in the tables.
var browseLabels = map[string]string{
	"tifa":         "Tifa",
	"tifa-nocache": "Tifa (cache off)",
	"nextjs":       "Next.js",
	"start":        "TanStack Start",
	"reactrouter":  "React Router",
}

// BrowseLabel is how a spec is named in the tables.
func BrowseLabel(spec string) string {
	if l, ok := browseLabels[spec]; ok {
		return l
	}
	return spec
}

// isTifa says whether a spec is one of the Tifa servers, whose answers say what they rendered.
func isTifa(spec string) bool { return spec == "tifa" || strings.HasPrefix(spec, "tifa-") }

// tries are all the rounds of a cell of the closed table, the invalid ones too.
func (r *BrowseResult) tries(fw, sc string, sessions int) []Round {
	var out []Round
	for _, x := range r.Closed {
		if x.Framework == fw && x.Scenario == sc && x.Sessions == sessions {
			out = append(out, x)
		}
	}
	return out
}

func count(x *Round, prefix string) float64 {
	n := 0
	for k, v := range x.Responses {
		if strings.HasPrefix(k, prefix) {
			n += v
		}
	}
	return float64(n)
}

func typeCell(rounds []Round, kind string) string {
	// A round with no samples of the kind has no latency of it, which is not 0 ms.
	var p50, p99 []float64
	n := 0
	for _, x := range rounds {
		if t := x.Types[kind]; t.N > 0 {
			p50, p99, n = append(p50, t.P50), append(p99, t.P99), n+t.N
		}
	}
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("%s / %s (%d)", fmtCell(p50, "%.1f"), fmtCell(p99, "%.0f"), n)
}

// p50Cell is the median of the P50 of a type over the rounds that have one.
func p50Cell(rounds []Round, kind string) string {
	var out []float64
	for _, x := range rounds {
		if t := x.Types[kind]; t.N > 0 {
			out = append(out, t.P50)
		}
	}
	return fmtCell(out, "%.1f")
}

// Markdown writes the report of the browse comparison. The body holds the results alone: for every
// scenario its throughput table and its latency table, each with a link to the diagnostics behind
// it, then the mix with the source of every number. The appendix after it holds how the comparison
// is made and every diagnostic: the steps of the sessions and what the rounds did, the rate Next.js
// sustains, the render memo of Tifa, the read-only control, the time series and whether the traces
// replay.
func (r *BrowseResult) Markdown() string {
	var b strings.Builder
	v := r.Version
	fmt.Fprintf(&b, "Measured %s. Go %s, Node %s", r.Date, strings.TrimPrefix(v["go"], "go"), v["node"])
	for _, k := range sortedKeys(r.Version) {
		if !slices.Contains([]string{"go", "node"}, k) {
			fmt.Fprintf(&b, ", %s %s", k, r.Version[k])
		}
	}
	fmt.Fprintf(&b, ". %s, %s. Placement: %s.\n", r.Machine.CPU, r.Machine.Kernel, r.Place.Note)
	rounds := r.rounds()
	for _, sc := range r.Scenarios {
		r.resultsMarkdown(&b, sc, rounds)
	}
	r.assumptionsMarkdown(&b)
	b.WriteString("\n## Appendix\n\n### How the comparison is made\n\n")
	b.WriteString(visitsText)
	b.WriteString(siteText)
	for _, sc := range r.Scenarios {
		r.diagnosticsMarkdown(&b, sc, rounds)
	}
	if checks := r.checks(); len(checks) > 0 {
		b.WriteString("\n### Do the traces replay on this machine\n\nThe answers of one round of each server (the first valid closed round at the base sessions of the first scenario it ran), by type of request, status and content type:\n\n")
		for _, f := range r.allServers() {
			if c := checks[f]; c != nil {
				fmt.Fprintf(&b, "- %s: %s\n", BrowseLabel(f), c.Summary())
			}
		}
	}
	p := r.Plans
	fmt.Fprintf(&b, "\nPlans: %d blocks of %d shopper and %d staff visits. In all: %d shopper visits with %d page views (%d returning, %d that add to the cart, %d that check out), %d staff visits, %d full loads and %d clicks. Page views by class: %v. Every round, with the invalid ones, is in the JSON.\n",
		p.Blocks, ShopperBlockVisits, StaffBlockPlans, p.Shoppers, p.ShopperViews, p.Returning, p.Adds, p.Checkouts, p.Staff, p.Loads, p.Clicks, p.Pages)
	return b.String()
}

// diagnosticsAnchor is the id of the part of the appendix that holds the diagnostics behind the
// throughput or the latency table of a scenario.
func diagnosticsAnchor(sc BrowseScenario, table string) string {
	return "diagnostics-" + sc.Name + "-" + table
}

// resultsMarkdown writes the result tables of a scenario: its throughput and its latency, each with a
// link to its diagnostics.
func (r *BrowseResult) resultsMarkdown(b *strings.Builder, sc BrowseScenario, rounds int) {
	fmt.Fprintf(b, "\n### %s\n\n", sc.Title)
	if len(r.sessionsOf(sc.Name)) > 0 {
		r.throughputMarkdown(b, sc, rounds)
		fmt.Fprintf(b, "\nDiagnostics: [the steps of the sessions, what the rounds did and cost, the writes, the render memo of Tifa, the read-only control and the time series](#%s).\n", diagnosticsAnchor(sc, "throughput"))
	}
	if !sc.Throughput {
		b.WriteString("\nThis scenario has no throughput table: it is measured for its latency at a fixed arrival rate only.\n")
	}
	if r.latencyMarkdown(b, sc) {
		fmt.Fprintf(b, "\nDiagnostics: [the arrival rate Next.js sustains, the writes and what the rounds did](#%s).\n", diagnosticsAnchor(sc, "latency"))
	}
	if !sc.Latency {
		b.WriteString("\nThis scenario has no open run: it is measured for its throughput only.\n")
	}
}

// diagnosticsMarkdown writes, in the appendix, the diagnostics behind the result tables of a
// scenario.
func (r *BrowseResult) diagnosticsMarkdown(b *strings.Builder, sc BrowseScenario, rounds int) {
	fw := r.Servers[sc.Name]
	sessions := r.sessionsOf(sc.Name)
	fmt.Fprintf(b, "\n<a id=\"%s\"></a>\n\n### %s: what is behind the throughput\n", diagnosticsAnchor(sc, "throughput"), sc.Title)
	if len(r.sessionsOf(sc.Name)) > 0 {
		r.throughputMethodMarkdown(b, sc, rounds)
	}
	if len(sessions) > 0 {
		b.WriteString("\n#### Page views a second at each number of concurrent sessions\n\nClosed loop: N concurrent sessions, no think time, each playing the next visit as soon as it ends one, with at most 6 connections. A cell is the median of the rounds with their range, and `failed` says why a server did not meet the rule there (P99 of full loads, clicks and writes at most 1 s, no failures, no backlog). A cell stops at its first failed round, which decides it. A page view counts when it completes. Prefetches and writes are not page views. `-` is a number of sessions the server was not stepped to.\n\n| |")
		for _, n := range sessions {
			fmt.Fprintf(b, " %d sessions |", n)
		}
		b.WriteString("\n| --- |" + strings.Repeat(" --- |", len(sessions)) + "\n")
		for _, f := range fw {
			fmt.Fprintf(b, "| %s |", BrowseLabel(f))
			for _, n := range sessions {
				rs := r.cellRounds(f, sc.Name, n)
				tries := r.tries(f, sc.Name, n)
				switch got, why := stepOf(rs, rounds); {
				case len(tries) == 0:
					b.WriteString(" - |")
				case len(rs) == 0:
					// No round of the cell was valid: say why the last one failed, with what made it invalid.
					last := tries[len(tries)-1]
					fmt.Fprintf(b, " not decided (no valid round of %d: %s) |", len(tries), strings.Join(slices.DeleteFunc([]string{last.Why, last.WhyInvalid}, func(s string) bool { return s == "" }), "; "))
				case got == trialFail:
					fmt.Fprintf(b, " failed (%s) |", why)
				case got == stepUndecided:
					fmt.Fprintf(b, " not decided (%s) |", why)
				default:
					fmt.Fprintf(b, " %s |", fmtCell(each(rs, pageViewsPerSecond), "%.1f"))
				}
			}
			b.WriteString("\n")
		}
	}
	for _, n := range r.detailSessions(sc.Name) {
		r.closedDetail(b, sc, n)
	}
	r.controlMarkdown(b, sc)
	r.seriesMarkdown(b, sc)
	fmt.Fprintf(b, "\n<a id=\"%s\"></a>\n\n### %s: what is behind the latency\n", diagnosticsAnchor(sc, "latency"), sc.Title)
	r.latencyMethodMarkdown(b, sc)
	r.sustainMarkdown(b, sc)
	if rs := func(f string) []Round { return r.latencyRounds(f, sc.Name) }; slices.ContainsFunc(fw, func(f string) bool { return len(rs(f)) > 0 }) {
		b.WriteString("\n#### The rounds at the same arrival rate\n")
		r.writesMarkdown(b, sc, rs)
		r.achievedMarkdown(b, sc, rs)
	}
}

// peopleCell reads page views a second as the active people who would make them at one page every
// 5 to 10 seconds: a reading aid, not a measurement.
func peopleCell(views float64) string {
	return fmt.Sprintf("%.0f to %.0f", 5*views, 10*views)
}

// throughputMarkdown is the table of what the steps of the sessions found for each server: the
// results alone (throughputMethodMarkdown in the appendix says how the steps ran).
func (r *BrowseResult) throughputMarkdown(b *strings.Builder, sc BrowseScenario, rounds int) {
	fmt.Fprintf(b, "#### Throughput\n\nEvery server starts at %d concurrent sessions with no think time and is stepped up while it keeps up, %d rounds a step. A row is where its steps ended, at the sessions it gives, so servers are compared at their own working points, not at the same concurrency. The result says how the steps ended, and the appendix says what each result means. A failed round is never used as a capacity. Active people is the page views a second times 5 to 10, as if each person read a page for 5 to 10 seconds: a reading aid, not something this run measured.\n\n", r.baseSessions(), rounds)
	b.WriteString("| | result | sessions | page views/s | active people at one page per 5 to 10 s | full load P50 / P99 ms (n) | click P50 / P99 ms (n) | write P50 / P99 ms (n) |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, f := range r.Servers[sc.Name] {
		t := r.Throughput[sustainKey(sc.Name, f)]
		switch {
		case t == nil:
			fmt.Fprintf(b, "| %s | not measured | - | - | - | - | - | - |\n", BrowseLabel(f))
		case t.Sessions == 0:
			fmt.Fprintf(b, "| %s | %s | - | - | - | - | - | - |\n", BrowseLabel(f), t.Result)
		default:
			rs := r.cellRounds(f, sc.Name, t.Sessions)
			views := each(rs, pageViewsPerSecond)
			fmt.Fprintf(b, "| %s | %s | %d | %s | %s | %s | %s | %s |\n", BrowseLabel(f), t.Result, t.Sessions, fmtCell(views, "%.1f"), peopleCell(Median(views)),
				typeCell(rs, "load"), typeCell(rs, "click"), typeCell(rs, "write"))
		}
	}
}

// throughputMethodMarkdown is, in the appendix, how the steps of the sessions ran and why they
// stopped where the throughput table says, with the server cores of each server there.
func (r *BrowseResult) throughputMethodMarkdown(b *strings.Builder, sc BrowseScenario, rounds int) {
	base := r.baseSessions()
	fmt.Fprintf(b, "\n#### How the steps of the sessions ran\n\nEvery server starts at %d concurrent sessions, no think time, %d rounds. A server whose median server cores stay under 0.95 of its cpus, with every round passing, is stepped up about 25%% a step (%d, %d, %d and so on), and one that fails at %d is stepped down to %s. A step passes, fails (a valid round failed the rule) or is not decided (too few valid rounds, none failed): only a failure steps down. The result says how the steps ended: `%s` (every round passed and the median server cores reached 0.95, even when the gain over the step below is within the spread of the rounds), `%s` (no reliable gain was measured: the slowest round of the step up was not faster than the fastest round of the step below, which is reported), `%s` (the next step failed the rule, so this is the step below it), `%s` (the next step was not decided, so this is the step below it), `%s` (the server's cores were not measured at a step that passed, so the steps stopped there: on a Mac, or without `-server-cpus`), `%s` (every step failed), `%s` (a step was not decided and none below it passed). Active people holds no long sessions open and no memory for them. A round in which the load generator used over 0.8 of its cpus is not valid and is played again, so a limit of the generator is never read as one of the server.\n\n", base, rounds, stepUp(base), stepUp(stepUp(base)), stepUp(stepUp(stepUp(base))), base, joinSessions(stepsDown(base)), ThroughputCoreUsed, ThroughputPlateau, ThroughputHighestPassing, ThroughputNextUndecided, ThroughputCoresUnknown, ThroughputNone, ThroughputUndecided)
	if r.Config["loadCpus"] == "" {
		b.WriteString("The load generator was not pinned to cpus of its own (`-load-cpus`), so whether it had cpus to spare was not checked.\n\n")
	}
	b.WriteString("| | server cores | why the steps stopped | sessions played |\n| --- | --- | --- | --- |\n")
	for _, f := range r.Servers[sc.Name] {
		t := r.Throughput[sustainKey(sc.Name, f)]
		switch {
		case t == nil:
			fmt.Fprintf(b, "| %s | - | not measured | - |\n", BrowseLabel(f))
		case t.Sessions == 0:
			fmt.Fprintf(b, "| %s | - | %s | %v |\n", BrowseLabel(f), t.Why, t.Steps)
		default:
			rs := r.cellRounds(f, sc.Name, t.Sessions)
			fmt.Fprintf(b, "| %s | %s | %s | %v |\n", BrowseLabel(f), coresCell(rs, func(x *Round) float64 { return x.CPUSeconds }), t.Why, t.Steps)
		}
	}
}

// closedDetail writes what the closed rounds of a scenario at a number of sessions did.
func (r *BrowseResult) closedDetail(b *strings.Builder, sc BrowseScenario, sessions int) {
	fw := r.Servers[sc.Name]
	fmt.Fprintf(b, "\n#### %d concurrent sessions\n\n", sessions)
	b.WriteString("| | page views/s | visits/s | CPU ms/page view | peak RSS MiB | requests/page view | body KB/page view | full load P50 / P99 ms (n) | click P50 / P99 ms (n) | write P50 / P99 ms (n) | server cores | client cores |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, f := range fw {
		rs := r.cellRounds(f, sc.Name, sessions)
		if len(rs) == 0 {
			continue
		}
		reads := func(x *Round) float64 { return float64(x.Requests) - count(x, "write ") }
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", BrowseLabel(f),
			fmtCell(each(rs, pageViewsPerSecond), "%.1f"),
			fmtCell(each(rs, func(x *Round) float64 { return float64(x.Visits) / x.Seconds }), "%.2f"),
			fmtCell(each(rs, func(x *Round) float64 { return 1000 * x.CPUSeconds / float64(max(1, x.PageViews)) }), "%.2f"),
			fmtCell(each(rs, func(x *Round) float64 { return x.PeakRSS }), "%.0f"),
			fmtCell(each(rs, func(x *Round) float64 { return reads(x) / float64(max(1, x.PageViews)) }), "%.1f"),
			fmtCell(each(rs, func(x *Round) float64 { return float64(x.Bytes) / 1024 / float64(max(1, x.PageViews)) }), "%.1f"),
			typeCell(rs, "load"), typeCell(rs, "click"), typeCell(rs, "write"),
			coresCell(rs, func(x *Round) float64 { return x.CPUSeconds }),
			coresCell(rs, func(x *Round) float64 { return x.ClientCPU }))
	}
	b.WriteString("\nWhat the server did a second:\n\n| | full loads/s | clicks/s | writes/s | renders/s | cache hits/s | shared renders/s |\n| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, f := range fw {
		rs := r.cellRounds(f, sc.Name, sessions)
		if len(rs) == 0 {
			continue
		}
		rate := func(get func(*Round) int) string {
			return fmtCell(each(rs, func(x *Round) float64 { return float64(get(x)) / x.Seconds }), "%.1f")
		}
		tifa := func(kind string) string {
			if !isTifa(f) {
				return "-"
			}
			return rate(func(x *Round) int { return x.Renders[kind] })
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s |\n", BrowseLabel(f), rate(func(x *Round) int { return x.FullLoads }), rate(func(x *Round) int { return x.Clicks }), rate(func(x *Round) int { return x.Writes }), tifa(RenderVM), tifa(RenderHit), tifa(RenderShared))
	}
	b.WriteString("\nWhat one visit costs the server, as requests per visit (and, for Tifa, what it did with the documents and the navigation data that said how they were answered, and the hits its render memo counted itself, from its metrics before and after the round):\n\n| | documents 200 (full pages) | documents 304 | client navigation requests | prefetches | writes | renders | cache hits | shared renders | memo hits Tifa counted |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, f := range fw {
		rs := r.cellRounds(f, sc.Name, sessions)
		if len(rs) == 0 {
			continue
		}
		per := func(prefix string) string {
			return fmtCell(each(rs, func(x *Round) float64 { return count(x, prefix) / float64(max(1, x.Visits)) }), "%.2f")
		}
		tifa := func(kind string) string {
			if !isTifa(f) {
				return "-"
			}
			return fmtCell(each(rs, func(x *Round) float64 { return float64(x.Renders[kind]) / float64(max(1, x.Visits)) }), "%.3f")
		}
		counted := "-"
		if isTifa(f) && !slices.ContainsFunc(rs, func(x Round) bool { return x.Memo == nil }) {
			counted = fmtCell(each(rs, func(x *Round) float64 { return float64(x.Memo.Hits()) / float64(max(1, x.Visits)) }), "%.3f")
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", BrowseLabel(f), per("doc 200 "), per("doc 304 "), per("nav "), per("prefetch "), per("write "), tifa(RenderVM), tifa(RenderHit), tifa(RenderShared), counted)
	}
	r.pageRendersMarkdown(b, sc, sessions)
	r.writesMarkdown(b, sc, func(f string) []Round { return r.cellRounds(f, sc.Name, sessions) })
	r.achievedMarkdown(b, sc, func(f string) []Round { return r.cellRounds(f, sc.Name, sessions) })
}

// writesMarkdown writes what the writes of the rounds of each server were, the rounds of a server
// given by rounds.
func (r *BrowseResult) writesMarkdown(b *strings.Builder, sc BrowseScenario, rounds func(fw string) []Round) {
	fw := r.Servers[sc.Name]
	b.WriteString("\nWhat the writes of a round were (median of the rounds, the range in brackets):\n\n| | add to cart | check out | edit a product | ship | write P50 ms: add / check out / edit / ship | orders at the end, studio / weekend |\n| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, f := range fw {
		rs := rounds(f)
		if len(rs) == 0 {
			continue
		}
		n := func(get func(*Round) int) string {
			return fmtCell(each(rs, func(x *Round) float64 { return float64(get(x)) }), "%.0f")
		}
		kind := func(k string) string { return n(func(x *Round) int { return x.WriteKinds[k] }) }
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s / %s / %s / %s | %s / %s |\n", BrowseLabel(f),
			kind(WriteAdd), kind(WriteCheckout), kind(WriteEdit), kind(WriteShip),
			p50Cell(rs, "write "+WriteAdd), p50Cell(rs, "write "+WriteCheckout), p50Cell(rs, "write "+WriteEdit), p50Cell(rs, "write "+WriteShip),
			n(func(x *Round) int { return x.Orders["studio"] }), n(func(x *Round) int { return x.Orders["weekend"] }))
	}
}

// achievedMarkdown writes what the rounds of a scenario planned and did, the rounds of a server given
// by rounds: the first of them, and the length of all.
func (r *BrowseResult) achievedMarkdown(b *strings.Builder, sc BrowseScenario, rounds func(fw string) []Round) {
	fw := r.Servers[sc.Name]
	b.WriteString("\nWhat the rounds planned and did (the first round, and the seconds of all). The mix is checked on the visits as planned: a round is run again unless its planned share of staff visits is within half a point of the scenario, its shopper visits plan 2.6 page views within 1% and 20% returning visitors within half a point, its adds and checkouts are within 0.2 points of the mix and its edits and ships within 3 points, or when the load generator did not start every visit. A visit that fails ends there, so a round whose server fails visits does fewer page views and writes than it planned: it is a valid round that failed, its page views a second are of a cut-short workload, and it is never counted as capacity.\n\n| | blocks | visits planned / started / succeeded / failed / unfinished | page views planned / done | full loads | clicks planned / done | writes planned / done | write share done | seconds | of which drain |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, f := range fw {
		rs := rounds(f)
		if len(rs) == 0 {
			continue
		}
		x := rs[0]
		fmt.Fprintf(b, "| %s | %d | %d / %d / %d / %d / %d | %d / %d | %d | %d / %d | %d / %d | %.2f%% | %s | %s |\n", BrowseLabel(f), x.Blocks,
			x.VisitsPlanned, x.VisitsStarted, x.Visits, x.VisitsFailed, x.VisitsUnfinished, x.PageViewsPlanned, x.PageViews, x.FullLoads, x.ClicksPlanned, x.Clicks, x.WritesPlanned, x.Writes, 100*x.WriteShare,
			fmtCell(each(rs, func(x *Round) float64 { return x.Seconds }), "%.0f"), fmtCell(each(rs, func(x *Round) float64 { return x.DrainSeconds }), "%.1f"))
	}
}

func (r *BrowseResult) sustainMarkdown(b *strings.Builder, sc BrowseScenario) {
	fw := r.Servers[sc.Name]
	if !slices.ContainsFunc(fw, func(f string) bool { return r.Sustain[sustainKey(sc.Name, f)] != nil }) {
		return
	}
	fmt.Fprintf(b, "\n#### The arrival rate %s sustains\n\nOpen model: visits start on schedule whether or not the earlier ones are done, and the actions of a visit are planned one second apart. A rate is sustained when full loads, clicks and writes each have a P99 of at most 1 s, nothing fails, no backlog is left, and the load generator keeps up, in %d rounds. The search doubles the rate, narrows it to within 10%%, then confirms it (and tries 10%% above it, within the cap of the search). It is run for %s only, whose rate sets the rate of the open run. A confirmed rate is kept in `test/compare/rates.json` with what it was measured under (the scenario, the plans and the traces, the sources, the cpu and its pinning, the Node and Next.js versions, the spacing of actions, the rule, the search and the confirmation), and a later run under all the same conditions reads it from there.\n\n| | sustained rate, visits/s | page views/s | notes |\n| --- | --- | --- | --- |\n", BrowseLabel(openBase), r.rounds(), BrowseLabel(openBase))
	for _, f := range fw {
		s := r.Sustain[sustainKey(sc.Name, f)]
		if s == nil {
			continue
		}
		if !s.Done {
			fmt.Fprintf(b, "| %s | - | - | not measured |\n", BrowseLabel(f))
			continue
		}
		if s.Rate == 0 {
			fmt.Fprintf(b, "| %s | none in range | - | %s |\n", BrowseLabel(f), s.Note)
			continue
		}
		note := s.Note
		if s.Limited {
			note = strings.TrimSpace(note + " the load generator ended the search: the rate is a lower bound")
		}
		if s.From != nil {
			passed := 0
			for _, x := range s.From.Confirm {
				if x.Pass && x.Valid {
					passed++
				}
			}
			note = strings.TrimSpace(fmt.Sprintf("%s from test/compare/rates.json: confirmed on %s with Tifa %s (%s), %d of %d confirmation rounds passed", note, s.From.Date, s.From.Tifa, s.From.Result, passed, len(s.From.Confirm)))
		}
		fmt.Fprintf(b, "| %s | %.2f | %.0f | %s |\n", BrowseLabel(f), s.Rate, sc.viewsPerVisit()*s.Rate, note)
	}
}

// latencyMarkdown writes the latency table of a scenario, and reports whether it had rounds to show.
func (r *BrowseResult) latencyMarkdown(b *strings.Builder, sc BrowseScenario) bool {
	if !slices.ContainsFunc(r.Latency, func(x Round) bool { return x.Scenario == sc.Name }) {
		if s := r.Sustain[sustainKey(sc.Name, openBase)]; sc.Latency && s != nil && s.Done && s.Rate == 0 {
			fmt.Fprintf(b, "\n#### Latency at the same arrival rate\n\nNo open run: %s has no sustainable rate in range on this scenario (%s).\n", BrowseLabel(openBase), s.Note)
		}
		return false
	}
	rate := r.LatencyRate[sc.Name]
	fmt.Fprintf(b, "\n#### Latency at the same arrival rate, half of %s's confirmed rate\n\nEvery server gets %.2f visits/s (%.0f page views/s planned), half of the rate %s was confirmed to sustain on this scenario. Latency is counted from the time an action was scheduled to the end of its foreground requests, queueing included. P50 / P99 in ms and the number of samples, over %d rounds, the failed ones too.\n\n| | full loads | clicks | writes | rounds |\n| --- | --- | --- | --- | --- |\n", BrowseLabel(r.LatencyBase[sc.Name]), rate, sc.viewsPerVisit()*rate, BrowseLabel(r.LatencyBase[sc.Name]), r.rounds())
	for _, f := range r.Servers[sc.Name] {
		// Every round it played at the rate is shown, the failed ones too: a failed round is part of
		// its result, with its errors and its tail latency.
		shown := r.latencyRounds(f, sc.Name)
		if len(shown) == 0 {
			continue
		}
		var notes []string
		for _, x := range shown {
			if !x.Pass {
				notes = append(notes, fmt.Sprintf("round %d failed: %s", x.Round, x.Why))
			}
		}
		note := fmt.Sprintf("%d of %d rounds passed", len(shown)-len(notes), len(shown))
		if len(notes) > 0 {
			note = fmt.Sprintf("failed the rule in %d of %d rounds: %s", len(notes), len(shown), strings.Join(notes, "; "))
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s |\n", BrowseLabel(f), typeCell(shown, "load"), typeCell(shown, "click"), typeCell(shown, "write"), note)
	}
	return true
}

// latencyMethodMarkdown is, in the appendix, how the rounds at the same arrival rate ran: the rule,
// and for each server its failures, the visits that failed or did not finish, its processor time
// and the rounds that were not valid and were run again.
func (r *BrowseResult) latencyMethodMarkdown(b *strings.Builder, sc BrowseScenario) {
	if !slices.ContainsFunc(r.Latency, func(x Round) bool { return x.Scenario == sc.Name }) {
		return
	}
	b.WriteString("\n#### How the rounds at the same arrival rate ran\n\nEvery server gets the same arrival rate, not half of its own capacity. A server that cannot hold that rate fails at it: the rate is not lowered for it. The latency table gives every valid round the server played at the rate, the failed ones too, and a server with a failed round failed the rule. A round in which the load generator fell behind is not valid, is run again and is only named here.\n\n| | failures | visits failed / unfinished, of planned | server CPU ms/page view | not valid, run again |\n| --- | --- | --- | --- | --- |\n")
	for _, f := range r.Servers[sc.Name] {
		shown := r.latencyRounds(f, sc.Name)
		var invalid []string
		for _, x := range r.Latency {
			if x.Framework == f && x.Scenario == sc.Name && !x.Valid {
				invalid = append(invalid, fmt.Sprintf("round %d try %d: %s", x.Round, x.Attempt, x.WhyInvalid))
			}
		}
		if len(shown) == 0 && len(invalid) == 0 {
			continue
		}
		fails, failed, unfinished, planned := 0, 0, 0, 0
		for _, x := range shown {
			fails, failed, unfinished, planned = fails+x.Failures, failed+x.VisitsFailed, unfinished+x.VisitsUnfinished, planned+x.VisitsPlanned
		}
		again := "none"
		if len(invalid) > 0 {
			again = strings.Join(invalid, "; ")
		}
		fmt.Fprintf(b, "| %s | %d | %d / %d of %d | %s | %s |\n", BrowseLabel(f), fails, failed, unfinished, planned,
			fmtCell(each(shown, func(x *Round) float64 { return 1000 * x.CPUSeconds / float64(max(1, x.PageViews)) }), "%.2f"), again)
	}
}

// latencyRounds are the valid rounds a server played at the rate of the open run of a scenario.
func (r *BrowseResult) latencyRounds(fw, sc string) []Round {
	var out []Round
	for _, x := range r.Latency {
		if x.Framework == fw && x.Scenario == sc && x.Valid {
			out = append(out, x)
		}
	}
	return out
}

// allServers are the specs of every scenario, in the order they first appear.
func (r *BrowseResult) allServers() []string {
	var out []string
	for _, sc := range r.Scenarios {
		for _, f := range r.Servers[sc.Name] {
			if !slices.Contains(out, f) {
				out = append(out, f)
			}
		}
	}
	return out
}

// baseSessions is the number of sessions every server starts at (o.Sessions).
func (r *BrowseResult) baseSessions() int {
	n, _ := strconv.Atoi(r.Config["sessions"])
	return n
}

// coresCell is the cores a process used over the window, or "-" when it was not measured (the CPU of
// the server is read from /proc, so there is none on a Mac).
func coresCell(rs []Round, cpu func(*Round) float64) string {
	if len(rs) == 0 {
		return "-"
	}
	v := each(rs, func(x *Round) float64 { return cpu(x) / x.Seconds })
	if slices.Max(v) == 0 {
		return "-"
	}
	return fmtCell(v, "%.2f")
}

// sessionsOf are the numbers of sessions the closed rounds of a scenario played, in order.
func (r *BrowseResult) sessionsOf(sc string) []int {
	var out []int
	for _, x := range r.Closed {
		if x.Scenario == sc && !slices.Contains(out, x.Sessions) {
			out = append(out, x.Sessions)
		}
	}
	slices.Sort(out)
	return out
}

// detailSessions are the numbers of sessions of a scenario whose cells get the detail tables: the
// base, and the sessions of each server's result.
func (r *BrowseResult) detailSessions(sc string) []int {
	played := r.sessionsOf(sc)
	var out []int
	for _, n := range played {
		if n == r.baseSessions() || slices.ContainsFunc(r.Servers[sc], func(f string) bool {
			t := r.Throughput[sustainKey(sc, f)]
			return t != nil && t.Sessions == n
		}) {
			out = append(out, n)
		}
	}
	return out
}

// rounds is the number of rounds of a cell: the setting of the run, or for a result without it the
// most rounds of a closed cell.
func (r *BrowseResult) rounds() int {
	if n, err := strconv.Atoi(r.Config["rounds"]); err == nil && n > 0 {
		return n
	}
	n := 0
	for _, x := range r.Closed {
		n = max(n, x.Round)
	}
	return max(n, 1)
}

func (r *BrowseResult) checks() map[string]*TraceCheck {
	out := map[string]*TraceCheck{}
	for _, sc := range r.Scenarios {
		for _, f := range r.Servers[sc.Name] {
			if out[f] != nil {
				continue
			}
			for _, x := range r.Closed {
				if x.Framework == f && x.Scenario == sc.Name && x.Sessions == r.baseSessions() && x.Valid {
					out[f] = NewTraceCheck(BrowseLabel(f), &x)
					break
				}
			}
		}
	}
	return out
}

// pageKinds are the kinds of page that the full loads of rounds rendered, sorted.
func pageKinds(rounds []Round) []string {
	seen := map[string]bool{}
	for _, x := range rounds {
		for k := range x.PageRenders {
			seen[k] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// hitCell is the share of the answers of a kind of page (documents and navigation data that said how
// they were answered) that the render memo answered, over all the rounds, with how many there were.
func hitCell(rounds []Round, kind string) string {
	hits, all := cacheHits(rounds, kind)
	if all == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%% of %d", 100*float64(hits)/float64(all), all)
}

// cacheHits are the answers of a kind of page the render memo gave in rounds, and all its answers.
func cacheHits(rounds []Round, kind string) (hits, all int) {
	for _, x := range rounds {
		for how, n := range x.PageRenders[kind] {
			all += n
			if how == RenderHit {
				hits += n
			}
		}
	}
	return hits, all
}

// pageRendersMarkdown is a diagnostic for Tifa: the render memo hits of the full loads of the
// closed rounds at a number of sessions, by kind of page (X-Tifa-Render on the documents and the navigation data).
func (r *BrowseResult) pageRendersMarkdown(b *strings.Builder, sc BrowseScenario, sessions int) {
	var rows []string
	var all []Round
	for _, f := range r.Servers[sc.Name] {
		if rs := r.cellRounds(f, sc.Name, sessions); isTifa(f) && len(rs) > 0 {
			rows = append(rows, f)
			all = append(all, rs...)
		}
	}
	kinds := pageKinds(all)
	if len(kinds) == 0 {
		return
	}
	b.WriteString("\nA diagnostic for Tifa, the render memo hits by kind of page (from X-Tifa-Render on the documents and on the navigation data, over the rounds), and, counted apart, the answers of the private pages (the cart and the checkout, which show the visitor's own cart) that came from the render memo:\n\n|")
	for _, k := range kinds {
		fmt.Fprintf(b, " | %s", k)
	}
	b.WriteString(" | private pages from the memo |\n| ---" + strings.Repeat(" | ---", len(kinds)+1) + " |\n")
	for _, f := range rows {
		rs := r.cellRounds(f, sc.Name, sessions)
		fmt.Fprintf(b, "| %s |", BrowseLabel(f))
		for _, k := range kinds {
			fmt.Fprintf(b, " %s |", hitCell(rs, k))
		}
		fmt.Fprintf(b, " %s |\n", privateCell(rs))
	}
}

// privateCell is how many answers of the private pages, the cart and the checkout, the render memo
// gave over the rounds, of all their answers.
func privateCell(rounds []Round) string {
	hits, all := 0, 0
	for _, x := range rounds {
		hits += x.PrivateHits
		for _, k := range []string{StoreKindCart, StoreKindCheckout} {
			for _, n := range x.PageRenders[k] {
				all += n
			}
		}
	}
	if all == 0 {
		return "-"
	}
	return fmt.Sprintf("%d of %d", hits, all)
}

// flagShare and flagLeast say which kinds of page the read-only control flags: those the render
// cache answered under half of the time, with at least 20 answers.
const (
	flagShare = 0.5
	flagLeast = 20
)

// cacheFlags are the kinds of page whose answers the render memo seldom gave in rounds, as
// "order 0.0% of 300", in order.
func cacheFlags(rounds []Round) []string {
	var out []string
	for _, k := range pageKinds(rounds) {
		hits, all := cacheHits(rounds, k)
		if all >= flagLeast && float64(hits) < flagShare*float64(all) {
			out = append(out, fmt.Sprintf("%s %.1f%% of %d", k, 100*float64(hits)/float64(all), all))
		}
	}
	return out
}

// controlMarkdown gives the read-only control of a scenario, if it has one, against the rounds of
// Tifa with writes: its closed rounds at the same sessions, or its latency rounds.
func (r *BrowseResult) controlMarkdown(b *strings.Builder, sc BrowseScenario) {
	var control []Round
	for _, x := range r.Control {
		if x.Scenario == sc.Name {
			control = append(control, x)
		}
	}
	if len(control) == 0 {
		return
	}
	sessions := control[0].Sessions
	label, writes := "Tifa, with writes", r.cellRounds("tifa", sc.Name, sessions)
	if len(writes) == 0 {
		label, writes = label+", the latency rounds", r.latencyRounds("tifa", sc.Name)
	}
	kinds := pageKinds(append(slices.Clone(control), writes...))
	fmt.Fprintf(b, "\n#### Read-only control\n\nA diagnostic, not a result: Tifa as it ships, at %d concurrent sessions with the traces of this scenario and no writes, one short round, against its rounds with writes. With nothing changing, the render memo should answer almost every request for a page after the first: a kind of page it answers under %.0f%% of the time (with %d answers or more) is flagged to look into. Against the rounds with writes, the hits by kind of page show which pages the writes keep from the cache.\n\n", sessions, 100*flagShare, flagLeast)
	if flags := cacheFlags(control); len(flags) > 0 {
		fmt.Fprintf(b, "**Red flag**: with no writes the render memo answered %s. Look into why: this table does not tell the cause.\n\n", strings.Join(flags, ", "))
	}
	b.WriteString("| | page views/s")
	for _, k := range kinds {
		fmt.Fprintf(b, " | %s", k)
	}
	b.WriteString(" |\n| --- | ---" + strings.Repeat(" | ---", len(kinds)) + " |\n")
	row := func(label string, rs []Round) {
		if len(rs) == 0 {
			return
		}
		fmt.Fprintf(b, "| %s | %s |", label, fmtCell(each(rs, pageViewsPerSecond), "%.1f"))
		for _, k := range kinds {
			fmt.Fprintf(b, " %s |", hitCell(rs, k))
		}
		b.WriteString("\n")
	}
	row("Tifa, no writes", control)
	row(label, writes)
}

// seriesMarkdown shows how the first valid round of each server went in steps of 10 seconds from its
// first request, closed (at the sessions of its result, else the base) and open: Tifa's cache filling
// up and the JIT of the Node servers warming show as a change over the round.
func (r *BrowseResult) seriesMarkdown(b *strings.Builder, sc BrowseScenario) {
	var rows []string
	for _, f := range r.Servers[sc.Name] {
		sessions := r.baseSessions()
		if t := r.Throughput[sustainKey(sc.Name, f)]; t != nil && t.Sessions > 0 {
			sessions = t.Sessions
		}
		show := func(label string, rs []Round) {
			if len(rs) == 0 || len(rs[0].Series) == 0 {
				return
			}
			var rates, p99 []string
			for _, s := range rs[0].Series {
				rates, p99 = append(rates, fmt.Sprintf("%.1f", s.PerSecond)), append(p99, fmt.Sprintf("%.0f", s.P99Ms))
			}
			rows = append(rows, fmt.Sprintf("| %s | %s, round %d | %s | %s |\n", BrowseLabel(f), label, rs[0].Round, strings.Join(rates, " "), strings.Join(p99, " ")))
		}
		show(fmt.Sprintf("closed, %d sessions", sessions), r.cellRounds(f, sc.Name, sessions))
		show("open", r.latencyRounds(f, sc.Name))
	}
	if len(rows) == 0 {
		return
	}
	b.WriteString("\n#### Page views a second and P99 in 10 second steps\n\nHow the first valid round of each server went, in steps of 10 seconds from the start of the round (the last step is as long as the round has left). The steps show how throughput and latency change over the round. An open round includes the ramp at the start and the drain at the end, so its first and last steps differ from the middle whatever the server does. Tifa had no warm-up and starts with an empty render memo, the Node servers had one. Page views a second, then the P99 in ms of the page views completed in the step.\n\n| | round | page views/s | P99 ms |\n| --- | --- | --- | --- |\n")
	for _, row := range rows {
		b.WriteString(row)
	}
}

// visitsText is what the report says about how the visits are played.
const visitsText = "Dynamic-traffic replay of whole visits recorded in Chromium: documents and the data requests of the client router, with no scripts, styles, images or fonts, no rendering and no hydration. It measures what each framework's server does for a visitor, not how fast a browser shows a page. A visit is a full load of a page, then clicks on links of the page on screen (no reloads), as a real browser did it against that framework (Chromium, 1280x800, no hover, the browser's own prefetching, caching and route discovery). Requests the browser cancelled are cancelled at the same time. Cookies are kept within a visit and not across visits. The writes are requests inserted into the recorded visits, since the traces hold none, and the refresh after a write is not replayed: a browser would fetch data again once a write is answered, while here a write costs the server its own request only. Every round, closed or open, starts a fresh server, so every round starts with the same data. Tifa gets no warm-up: it has no JIT, and its render memo starts empty in every round, so what the memo holds is earned inside the measured time. The Node servers get a warm-up with no writes, since their JIT needs one. Times are when the server's answer reached the client, not when a browser would have shown the page. CPU is the processor time of the whole server process tree, user and system, and includes writes and prefetches.\n\n"

// siteText is what the report says about the one site.
const siteText = "The comparison plays the shop as one site: shoppers on the storefront (the home, the product list with its filters, 28 products, the cart, the checkout, the journal and its posts, in English or Chinese) and the staff in the back office, at once on the same server. Throughput is the page views of the whole site. A shopper visit stays in one language. A shopper who adds to the cart adds a product in a colour and size that is in stock, after a product page, and one who checks out does so after the checkout page (it answers 303 to the page of the order, which is not followed). Every visit has data of its own: a new visitor starts with no cart and sees the demonstration cart of four lines, and a returning visitor comes back to a cart of their own. Those carts are made after the server starts and before the clock, through the shop's cart API, the same on every framework, one for each returning visit. A staff visit reads the orders, the stats and the projects of the back office, and some edit a product or ship an order. The back office has no product edit yet, so an edit changes the recipient of an order for now. A ship takes the oldest order that has not shipped, and a staff write that finds no order to go to places one instead (counted below). Orders and carts pile up while a server runs, and so does the memory that holds them: the table of writes says how many orders of the back office each server held at the end of a round. The product pages also tell the shop which product the visitor looked at (`POST /store/mine`), and the traces keep that request with its body. Next.js, TanStack Start and React Router render every request: a check before the run makes sure none of them sends anything that lets an answer be kept (an ETag, a Last-Modified, a positive max-age, a Next.js cache header). Tifa keeps rendered pages in its render memo.\n\nPlans come in blocks of 1000 shopper visits with the mix below exactly (by quota) and a set of staff visits, so every round plans the mix of its scenario, whose share of staff visits and of edits is placed by quota in every cycle of the site. A round is not valid, and is played again, when the visits it planned miss the mix of its scenario, when the load generator did not start every visit, when a request of the setup before the clock (the orders of the ships, the carts of the returning visitors) rendered a page, or, for Tifa, when its metrics route said that its render memo held a page right before the clock (something filled it before the round).\n\n"

// assumptionsMarkdown lists the numbers of the mix with their sources, and what each scenario sets.
func (r *BrowseResult) assumptionsMarkdown(b *strings.Builder) {
	b.WriteString("\n### The mix\n\nWhat the visits are made of, each number with where it comes from. A number that no published data backs is an assumption.\n\n| | value | source |\n| --- | --- | --- |\n")
	for _, f := range MixFacts() {
		fmt.Fprintf(b, "| %s | %s | %s |\n", f.What, f.Value, f.Source)
	}
	for _, sc := range r.Scenarios {
		fmt.Fprintf(b, "| scenario %s | staff %.0f%% of the visits, %.0f%% of their visits edit a product, %.0f%% ship an order | the staff numbers above", sc.Name, 100*sc.Staff, 100*sc.Edit, 100*sc.Ship)
		if sc.Staff != StaffShare || sc.Edit != StaffEditShare {
			b.WriteString(", with the staff share and the edit share doubled to see how much the result moves with them")
		}
		b.WriteString(" |\n")
	}
	b.WriteString("\n")
}
