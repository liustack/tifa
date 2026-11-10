package compare

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
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
	"tifa-nocache": "Tifa, memo off (reference)",
	"nextjs":       "Next.js",
	"start":        "TanStack Start",
	"reactrouter":  "React Router",
}

// browseReferences are the specs of the browse comparison that are references, not frameworks
// compared, with what each stands for, as the tables say it under their rows.
var browseReferences = map[string]string{
	"tifa-nocache": "Tifa with `--ssr-memo-size 0`, which turns off keeping finished renders. It may still answer navigation data with no render, share a render in progress between requests of a page at the same time, and answer 304. It is what Tifa costs when no finished render is kept, not one of the frameworks compared, and no result is read against it",
}

// referencesMarkdown says, under a table of the body, what each reference among its rows stands for.
func (r *BrowseResult) referencesMarkdown(b *strings.Builder, sc BrowseScenario) {
	for _, f := range r.Servers[sc.Name] {
		if what, ok := browseReferences[f]; ok {
			fmt.Fprintf(b, "\n%s is %s.\n", BrowseLabel(f), what)
		}
	}
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
	if off := r.notFormal(); len(off) > 0 {
		fmt.Fprintf(&b, "\n**Not a formal run.** It differs from a formal run (FormalBrowse, pinned, by browse.sh, every server) in: %s. Its numbers are not results of the comparison.\n", strings.Join(off, ", "))
	}
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
		r.cappedMarkdown(b, sc)
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
	r.warmUpMarkdown(b, sc)
	r.memoMarkdown(b, sc)
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
	compressionMarkdown(b, r.Servers[sc.Name], func(f string) []Round {
		if t := r.Throughput[sustainKey(sc.Name, f)]; t != nil && t.Sessions > 0 {
			return r.cellRounds(f, sc.Name, t.Sessions)
		}
		return nil
	})
	b.WriteString("| | result | sessions | page views/s | active people at one page per 5 to 10 s | full load P50 / P99 ms (n) | click P50 / P99 ms (n) | write P50 / P99 ms (n) | crawler requests/s | crawler document P50 / P99 ms (n) |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	marked := false
	for _, f := range r.Servers[sc.Name] {
		t := r.Throughput[sustainKey(sc.Name, f)]
		switch {
		case t == nil:
			fmt.Fprintf(b, "| %s | not measured | - | - | - | - | - | - | - | - |\n", BrowseLabel(f))
		case t.Sessions == 0:
			fmt.Fprintf(b, "| %s | %s | - | - | - | - | - | - | - | - |\n", BrowseLabel(f), t.Result)
		default:
			rs := r.cellRounds(f, sc.Name, t.Sessions)
			views := each(rs, pageViewsPerSecond)
			cell := fmtCell(views, "%.1f")
			if slices.ContainsFunc(rs, func(x Round) bool { return x.Capped }) {
				cell += cappedMark
				marked = true
			}
			fmt.Fprintf(b, "| %s | %s | %d | %s | %s | %s | %s | %s | %s | %s |\n", BrowseLabel(f), t.Result, t.Sessions, cell, peopleCell(Median(views)),
				typeCell(rs, "load"), typeCell(rs, "click"), typeCell(rs, "write"), crawlerRate(rs), typeCell(rs, "crawler"))
		}
	}
	if marked {
		b.WriteString("\n" + cappedMark + " The visits of a round of this cell reached the cap of the prepared visits: every server of the scenario gets the data of the same visits before the clock, and a round plays no more. The appendix gives how long each such round took.\n")
	}
	r.referencesMarkdown(b, sc)
}

// cappedMarkdown lists, in the appendix, every closed try of a scenario whose visits reached the
// cap of the prepared visits (Round.Capped), at any number of sessions and valid or not, with how
// long it took against the window of the target.
func (r *BrowseResult) cappedMarkdown(b *strings.Builder, sc BrowseScenario) {
	var tries []Round
	for _, f := range r.Servers[sc.Name] {
		for _, x := range r.Closed {
			if x.Framework == f && x.Scenario == sc.Name && x.Capped {
				tries = append(tries, x)
			}
		}
	}
	if len(tries) == 0 {
		return
	}
	window := func(seconds float64) string {
		target, err := time.ParseDuration(r.Config["target"])
		if err != nil {
			return "-"
		}
		lo, hi := roundLength(target)
		span := fmt.Sprintf("%.0f to %.0f s", lo, hi)
		switch {
		case seconds < lo:
			return "shorter than the window, " + span
		case seconds > hi:
			return "longer than the window, " + span
		}
		return "in the window, " + span
	}
	b.WriteString("\n#### Rounds at the cap of the prepared visits\n\nEvery server of a scenario gets the data of the same visits before the clock of a closed round, and a round plays no more of them. These tries were sized to that many visits or more. One that took less than the window of the target was kept, since it could not be made longer.\n\n| | sessions | round | try | visits planned / ended | seconds | against the target | valid |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	slices.SortStableFunc(tries, func(x, y Round) int {
		return cmp.Or(cmp.Compare(x.Sessions, y.Sessions), cmp.Compare(x.Round, y.Round), cmp.Compare(x.Attempt, y.Attempt))
	})
	for _, x := range tries {
		valid := "valid"
		if !x.Valid {
			valid = "not valid: " + x.WhyInvalid
		}
		fmt.Fprintf(b, "| %s | %d | %d | %d | %d / %d | %.0f s | %s | %s |\n", BrowseLabel(x.Framework), x.Sessions, x.Round, x.Attempt, x.VisitsPlanned, x.Visits+x.VisitsFailed, x.Seconds, window(x.Seconds), valid)
	}
}

// cappedMark marks a cell of the body with a round that played every visit the data before its clock
// was made for (Round.Capped).
const cappedMark = "¹"

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
	r.crawlerMarkdown(b, sc, sessions)
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
	fmt.Fprintf(b, "\nWhat the rounds planned and did (the first round, and the seconds of all). The mix is checked on the visits as planned: a round is run again unless its planned share of staff visits is within half a point of the scenario, its shopper visits plan %g page views within 1%% and 20%% returning visitors within half a point, its adds and checkouts are within 0.2 points of the mix and its edits and ships within 3 points, or when the load generator did not start every visit. A visit that fails ends there, so a round whose server fails visits does fewer page views and writes than it planned: it is a valid round that failed, its page views a second are of a cut-short workload, and it is never counted as capacity.\n\n| | blocks | visits planned / started / succeeded / failed / unfinished | page views planned / done | full loads | clicks planned / done | writes planned / done | write share done | seconds | of which drain |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n", ShopperViewsMean)
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
	fmt.Fprintf(b, "\n#### Latency at the same arrival rate, half of %s's confirmed rate\n\nEvery server gets %.2f visits/s (%.0f page views/s planned), half of the rate %s was confirmed to sustain on this scenario. Latency is counted from the time an action was scheduled to the end of its foreground requests, queueing included. P50 / P99 in ms and the number of samples, over %d rounds, the failed ones too.\n\n", BrowseLabel(r.LatencyBase[sc.Name]), rate, sc.viewsPerVisit()*rate, BrowseLabel(r.LatencyBase[sc.Name]), r.rounds())
	compressionMarkdown(b, r.Servers[sc.Name], func(f string) []Round { return r.latencyRounds(f, sc.Name) })
	b.WriteString("| | full loads | clicks | writes | crawler documents | rounds |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, f := range r.Servers[sc.Name] {
		// Every round it played at the rate is shown, the failed ones too: a failed round is part of
		// its result, with its errors and its tail latency.
		// Every server is listed: having no valid round, or too few to decide, is its result too.
		shown, want := r.latencyRounds(f, sc.Name), r.rounds()
		if len(shown) == 0 {
			fmt.Fprintf(b, "| %s | - | - | - | - | no valid result: 0 of %d rounds valid |\n", BrowseLabel(f), want)
			continue
		}
		var notes []string
		for _, x := range shown {
			if !x.Pass {
				notes = append(notes, fmt.Sprintf("round %d failed: %s", x.Round, x.Why))
			}
		}
		var note string
		switch {
		case len(notes) > 0: // a valid round that failed decides it, however many were valid
			note = fmt.Sprintf("failed the rule in %d of %d rounds: %s", len(notes), len(shown), strings.Join(notes, "; "))
		case len(shown) < want:
			note = fmt.Sprintf("not decided: %d of %d rounds valid, %d of %d passed", len(shown), want, len(shown), len(shown))
		default:
			note = fmt.Sprintf("%d of %d rounds passed", len(shown), len(shown))
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s |\n", BrowseLabel(f), typeCell(shown, "load"), typeCell(shown, "click"), typeCell(shown, "write"), typeCell(shown, "crawler"), note)
	}
	r.referencesMarkdown(b, sc)
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
	r.renderWhyMarkdown(b, rows, func(f string) []Round { return r.cellRounds(f, sc.Name, sessions) })
	landingMarkdown(b, rows, func(f string) []Round { return r.cellRounds(f, sc.Name, sessions) })
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
	b.WriteString("\n#### Page views a second and P99 in 10 second steps\n\nHow the first valid round of each server went, in steps of 10 seconds from the start of the round (the last step is as long as the round has left). The steps show how throughput and latency change over the round. An open round includes the ramp at the start and the drain at the end, so its first and last steps differ from the middle whatever the server does. Every server had the same warm-up before the round, so the render memo of Tifa starts with what the warm-up left in it. Page views a second, then the P99 in ms of the page views completed in the step.\n\n| | round | page views/s | P99 ms |\n| --- | --- | --- | --- |\n")
	for _, row := range rows {
		b.WriteString(row)
	}
}

// visitsText is what the report says about how the visits are played.
const visitsText = "Dynamic-traffic replay of whole visits recorded in Chromium: documents and the data requests of the client router, with no scripts, styles, images or fonts, no rendering and no hydration. It measures what each framework's server does for a visitor, not how fast a browser shows a page. A visit is a full load of a page, then clicks on links of the page on screen (no reloads), as a real browser did it against that framework (Chromium, 1280x800, no hover, the browser's own prefetching, caching and route discovery). Requests the browser cancelled are cancelled at the same time. Cookies are kept within a visit and not across visits. The writes are requests inserted into the recorded visits, since the traces hold none, and the refresh after a write is not replayed: a browser would fetch data again once a write is answered, while here a write costs the server its own request only. Every round, closed or open, starts a fresh server, so every round starts with the same data. Before the clock of each round every server gets the same warm-up: the visits after those the data was made for, as many as the longest closed round any server was sized to (in an open round, as many as the round plays), with the mix and the writes of the round, played as the round plays (its sessions, or its rate). It counts in no result. The render memo of Tifa is empty before the warm-up, and the appendix gives what it held after it. The Node servers have no other warm-up: this one warms their JIT on the code the round runs. Times are when the server's answer reached the client, not when a browser would have shown the page. CPU is the processor time of the whole server process tree, user and system, and includes writes and prefetches.\n\n"

// siteText is what the report says about the one site.
const siteText = "The comparison plays the shop as one site: shoppers on the storefront (the home, the product list with its filters, 150 products, the cart, the checkout, the journal and its posts, in English or Chinese) and the staff in the back office, at once on the same server. Throughput is the page views of the whole site. Crawlers ask for the public pages of the storefront as a stream of their own, 9% of the HTML requests: a crawler request is no visit and no page view, and the tables list the crawler requests apart from people. A shopper visit stays in one language. A shopper who adds to the cart adds the product of the product page they are on, in a colour, a size and a quantity drawn from the seed and the visit, and one who checks out does so after the checkout page, with a name, an email and an address of the visit's own (it answers 303 to the page of the order, which is not followed). A returning visitor comes back to a cart of their own, made for that visit alone, so the private data of every visit that has some (the cart of a returning visitor, a checkout) is drawn for that visit alone from the seed and the visit, while a new visitor sees the demonstration cart, the same for every new visitor, as the empty cart of a real shop is the same for every anonymous visitor. A staff visit reads the orders, the stats and the projects of the back office, and some edit a product or ship an order: an edit is one PATCH of one field of a product, a price or a stock drawn from the seed and the visit, with nothing read first, and each ship ships an order of its own, placed for it before the clock. Before the clock every server of a scenario gets the same requests, none of which renders a page, made for the same visits whatever the server was sized to: a restock that gives every product the stock the orders can take, then the orders of the ships, then the carts of the returning visitors. Then every server gets the same warm-up, counted in no result: visits of the mix after those of the round, with their writes, played as the round plays (its sessions, or its rate). The orders that checkouts place are orders of the back office that no ship takes. As in a real shop, the time of an order is the clock of the server when it is placed, before the clock of the round or in it. Orders and carts pile up while a server runs, and so does the memory that holds them: the table of writes says how many orders of the back office each server held at the end of a round. The product pages also tell the shop which product the visitor looked at (`POST /store/mine`), and the traces keep that request with its body. Next.js, TanStack Start and React Router render every request: a check before the run makes sure none of them sends anything that lets an answer be kept (an ETag, a Last-Modified, a positive max-age, a Next.js cache header). Tifa keeps rendered pages in its render memo.\n\nPlans come in blocks of 1000 shopper visits with the mix below exactly (by quota) and a set of staff visits, so every round plans the mix of its scenario, whose share of staff visits and of edits is placed by quota in every cycle of the site. A round is not valid, and is played again, when the visits it planned miss the mix of its scenario, when the load generator did not start every visit, when a request of the setup before the clock (the restock, the orders of the ships, the carts of the returning visitors) rendered a page or got another status than planned, or, for Tifa, when its metrics route said that its render memo held a page right before the warm-up (something filled it before), or the shop refused a request of the warm-up with 409 or 422.\n\n"

// assumptionsMarkdown lists the numbers of the mix with their sources, and what each scenario sets.
func (r *BrowseResult) assumptionsMarkdown(b *strings.Builder) {
	b.WriteString("\n### The mix\n\nWhat the visits are made of, each number with where it comes from. A number that no published data backs is an assumption.\n\n| | value | source |\n| --- | --- | --- |\n")
	for _, f := range MixFacts() {
		fmt.Fprintf(b, "| %s | %s | %s |\n", f.What, f.Value, f.Source)
	}
	b.WriteString("\n")
}

// compressionMarkdown names, above a table of the body, each server that compressed no answer in the
// rounds of the table, with the body it sent a page view against the frameworks compared that
// compressed (a reference is not one of them): every server answers as its framework ships, so what
// goes over the wire differs with it. A server whose rounds did not count their encodings (a result
// of an older run) is left out.
func compressionMarkdown(b *strings.Builder, servers []string, rounds func(f string) []Round) {
	type sent struct {
		label string
		kb    float64
	}
	var plain, packed []sent
	used := map[string]bool{}
	for _, f := range servers {
		rs := rounds(f)
		if len(rs) == 0 || slices.ContainsFunc(rs, func(x Round) bool { return x.Encodings == nil }) {
			continue
		}
		s := sent{BrowseLabel(f), Median(each(rs, func(x *Round) float64 { return float64(x.Bytes) / 1024 / float64(max(1, x.PageViews)) }))}
		compressed := false
		for _, x := range rs {
			for e, n := range x.Encodings {
				if e != "identity" && n > 0 {
					compressed, used[e] = true, true
				}
			}
		}
		if compressed {
			if _, ref := browseReferences[f]; !ref {
				packed = append(packed, s)
			}
		} else {
			plain = append(plain, s)
		}
	}
	for _, p := range plain {
		fmt.Fprintf(b, "**%s compresses no answer: %.1f KB of body a page view", p.label, p.kb)
		if len(packed) > 0 {
			lo, hi := packed[0].kb, packed[0].kb
			for _, s := range packed {
				lo, hi = min(lo, s.kb), max(hi, s.kb)
			}
			fmt.Fprintf(b, ", against %.1f to %.1f KB for the others, which send %s", lo, hi, strings.Join(slices.Sorted(maps.Keys(used)), " and "))
		}
		b.WriteString(".** Every server answers as its framework ships.\n\n")
	}
}

// expectedOf is the documents of a kind of page that the plans count in rounds, with the hits they allow,
// and false when no round has an expected hit rate (a result of an older run, or a round whose plans
// could not be worked out).
func expectedOf(rounds []Round, kind string) (ExpectedCell, bool) {
	var c ExpectedCell
	ok := false
	for _, x := range rounds {
		if x.Expected != nil {
			ok = true
			c = c.Sum(x.Expected.Humans[kind])
		}
	}
	return c, ok
}

// memoDropped is the pages the render memo of Tifa dropped to make room in rounds, from the start of
// the server on (the warm-up too, since a page it dropped is one the round misses), and false when no
// round read it.
func memoDropped(rounds []Round) (int64, bool) {
	var n int64
	read := false
	for _, x := range rounds {
		if x.Memo != nil && x.SetupError == "" && x.MemoError == "" {
			read = true
			n += x.Memo.End.Evictions - x.Memo.Start.Evictions
		}
	}
	return n, read
}

// Where the memo of Tifa answers under what the plans allow by expectedGap points or more, with over
// expectedLeast answers, the report flags the kind of page.
const (
	expectedGap   = 5
	expectedLeast = 100
)

// underExpected says whether answered of answers, the share the memo gave or shared, is a red flag
// against what the plans allow (c): over expectedLeast answers, and expectedGap points or more under
// the expected share. It counts in whole numbers, so that a gap of exactly expectedGap points is one.
func underExpected(answered, answers int, c ExpectedCell) bool {
	if answers <= expectedLeast || c.Docs == 0 {
		return false
	}
	// answered/answers + expectedGap/100 <= c.Hits/c.Docs
	return 100*answered*c.Docs+expectedGap*answers*c.Docs <= 100*c.Hits*answers
}

// renderWhyMarkdown splits, for each Tifa server, its answers by kind of page into those the render
// memo gave and those it rendered in a VM, and these by why, as far as the plans tell (Expect) or the
// client can tell (renderWhy), over the rounds, with the hit rate the plans allow beside the one
// measured. A server whose rounds did not split them (a result of an older run) is left out.
func (r *BrowseResult) renderWhyMarkdown(b *strings.Builder, servers []string, rounds func(f string) []Round) {
	var rows, flags []string
	again := false // a round with no data versions has URLs answered before: a column for them
	for _, f := range servers {
		for _, x := range rounds(f) {
			for _, why := range x.VMWhy {
				again = again || why[RenderAgain] > 0
			}
		}
	}
	for _, f := range servers {
		rs := rounds(f)
		if len(rs) == 0 || slices.ContainsFunc(rs, func(x Round) bool { return x.VMWhy == nil }) {
			continue
		}
		dropped, read := memoDropped(rs)
		for _, k := range pageKinds(rs) {
			hits, all := cacheHits(rs, k)
			why := map[string]int{}
			vm, shared := 0, 0
			for _, x := range rs {
				for w, n := range x.VMWhy[k] {
					why[w] += n
				}
				shared += x.PageRenders[k][RenderShared]
				vm += x.PageRenders[k][RenderVM]
			}
			cell := func(w string, private bool) string {
				if private != privateKind(k) {
					return "-"
				}
				return strconv.Itoa(why[w])
			}
			measured := "-"
			if all > 0 {
				measured = fmt.Sprintf("%.1f%% of %d", 100*float64(hits+shared)/float64(all), all)
			}
			allowed := "-"
			if c, ok := expectedOf(rs, k); ok && c.Docs > 0 {
				allowed = fmt.Sprintf("%.1f%% of %d", 100*c.Rate(), c.Docs)
				if read && dropped > 0 && !privateKind(k) {
					allowed += fmt.Sprintf(" (the memo dropped %d pages)", dropped)
				}
				if f == "tifa" && underExpected(hits+shared, all, c) {
					flags = append(flags, fmt.Sprintf("%s, %s: the memo and shared renders answered %.1f%% of %d documents, the plans allow %.1f%%", BrowseLabel(f), k, 100*float64(hits+shared)/float64(all), all, 100*c.Rate()))
				}
			}
			row := fmt.Sprintf("| %s | %s | %d | %d | %d | %d | %s | %s | %s | %s | %s |", BrowseLabel(f), k, all, hits, shared, vm, allowed, measured,
				cell(RenderFirst, false), cell(RenderChanged, false), cell(RenderSame, false))
			if again {
				row += " " + cell(RenderAgain, false) + " |"
			}
			rows = append(rows, row+" "+cell(RenderPrivate, true)+" | "+cell(RenderPrivateCart, true)+" |\n")
		}
	}
	if len(rows) == 0 {
		return
	}
	b.WriteString("\nWhy Tifa rendered a page in a VM and did not answer it from the render memo, by kind of page, over the rounds: the answers (documents and navigation data that said how they were answered), those from the memo, the answers that shared a render in progress of the same page, and the renders in a VM, which add up to the answers. Next to them, the hit rate the plans allow a memo that drops nothing (the share of the documents whose page was asked for before, in the warm-up too, with the same data, worked out from the plans, the writes and the crawler requests alone, so it is the same for every server: see the expected hit rate in the method), and the share of the answers the memo gave or shared that Tifa measured. The renders in a VM are split by why, as the plans tell them for the landing document of a visit: a page not asked for before, a page asked for before whose data a write changed since, and a page asked for before with the same data. The last should have been a hit, so it was dropped when the memo was full (the pages it dropped are noted next to the rate), came in another order than the plans (the servers play the writes and the documents at once), or is a bug. A private page (the cart and the checkout, whose data is the visitor's cart and the products they looked at, the only pages of the shop whose `Load`s read cookies) is counted by whether the visit sent a cart cookie, whatever its URL. Only documents are in the plans: a click gets the data of a page and no HTML, so it is no request that puts a page in the memo, and the plans count none. The rate the plans allow is low in three known ways, none of them measured in points: a version of the data counts writes and not values, so an edit that sets the value a product has already, or puts an earlier one back, counts as a change though the data is what it was, and the memo, which goes by the data, can hit; a price edit counts as a change of every page of the product list, and some of them stay the same; and the pages of the back office follow a rough rule, a write to either shop changes all of them. The split has limits, which the report states: requests of a page at the same time share a render in progress (counted as shared, and the one that rendered as its class), a request the client canceled (its round ended) is not counted though Tifa may have rendered it, and the answers counted are those received whole with `X-Tifa-Render` saying vm, memo or shared. With the memo off nothing comes from the memo, and the rate the plans allow says how much the workload repeats its pages.\n\n")
	for _, f := range flags {
		fmt.Fprintf(b, "**Red flag**: %s.\n\n", f)
	}
	b.WriteString("| | kind of page | answers | from the memo | shared a render in progress | rendered in a VM | the plans allow from the memo | memo and shared, measured | VM: URL not seen before | VM: URL seen, data changed | VM: URL seen, data the same |")
	if again {
		b.WriteString(" VM: URL answered before, no data version |")
	}
	b.WriteString(" private page, no cart cookie | private page, cart cookie |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |")
	if again {
		b.WriteString(" --- |")
	}
	b.WriteString(" --- | --- |\n")
	for _, row := range rows {
		b.WriteString(row)
	}
}

// notFormal is how a run differs from a formal one (FormalBrowse): its settings, the pinning of the
// server and the load, whether browse.sh ran it, and its servers. It is empty for a formal run.
func (r *BrowseResult) notFormal() []string {
	var off []string
	formal := browseConfig(FormalBrowse, "", "", "")
	for _, k := range []string{"sessions", "rounds", "sizing", "target", "trialWindow", "finalWindow", "maxRate", "seed"} {
		if v, ok := r.Config[k]; !ok {
			off = append(off, fmt.Sprintf("%s not recorded (formal %s)", k, formal[k]))
		} else if v != formal[k] {
			off = append(off, fmt.Sprintf("%s %s (formal %s)", k, v, formal[k]))
		}
	}
	if r.Config["serverCpus"] == "" || r.Config["loadCpus"] == "" {
		off = append(off, "the server and the load not pinned to cpus of their own")
	}
	if r.Stamp == "" {
		off = append(off, "not run by browse.sh, so its traces were not checked against the checkout")
	}
	for _, sc := range r.Scenarios {
		if !slices.Equal(r.Servers[sc.Name], BrowseFrameworks) {
			off = append(off, fmt.Sprintf("servers %s (formal %s)", strings.Join(r.Servers[sc.Name], ", "), strings.Join(BrowseFrameworks, ", ")))
		}
	}
	return off
}

// yesNo writes a bool in a cell of a table.
func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// warmUpMarkdown writes, for every round of a scenario that had a warm-up (the closed rounds, the
// latency rounds and the read-only control, valid or not, of every server), what the warm-up did
// and the cores the server used in the last second of the pause after it, where its cpu was read.
func (r *BrowseResult) warmUpMarkdown(b *strings.Builder, sc BrowseScenario) {
	var rows []string
	add := func(what string, x *Round) {
		w := x.WarmUp
		if x.Scenario != sc.Name || w == nil {
			return
		}
		cores := "-"
		if w.IdleCores != nil {
			cores = fmt.Sprintf("%.2f", *w.IdleCores)
		}
		rows = append(rows, fmt.Sprintf("| %s | %s, round %d | %d | %s | %d of %d | %d of %d | %d | %.0f | %s |\n", BrowseLabel(x.Framework), what, x.Round, x.Attempt, yesNo(x.Valid), w.Visits, w.VisitsPlanned, w.Writes, w.WritesPlanned, w.Failures, w.Seconds, cores))
	}
	for i := range r.Closed {
		add(fmt.Sprintf("closed, %d sessions", r.Closed[i].Sessions), &r.Closed[i])
	}
	for i := range r.Latency {
		add(fmt.Sprintf("open, %.2f visits/s", r.Latency[i].Rate), &r.Latency[i])
	}
	for i := range r.Control {
		add("read-only control", &r.Control[i])
	}
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(b, "\n#### The warm-up before each round\n\nWhat the warm-up before the clock of each round did, every try: the visits and the writes it did of those it planned, its failures and how long it took. After its last request ended every server was left alone for %.0f seconds before the clock, so that it could end what it still did for a request the trace cancelled. The last column is the cores the server used in the last second of that pause (- where the cpu of the server was not read).\n\n| | round | try | valid | visits done | writes done | failures | seconds | server cores in the last second before the clock |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n", settlePause.Seconds())
	for _, row := range rows {
		b.WriteString(row)
	}
}

// memoMarkdown writes, for every round of a Tifa server of a scenario that read its metrics (the
// closed rounds, the latency rounds and the read-only control, valid or not), what its render memo
// held after the warm-up and at the end, the pages it dropped to make room in the round and the hits
// it counted from the clock on, all from its metrics route.
func (r *BrowseResult) memoMarkdown(b *strings.Builder, sc BrowseScenario) {
	var rows []string
	add := func(what string, x *Round) {
		if x.Scenario != sc.Name || !isTifa(x.Framework) || x.Memo == nil {
			return
		}
		m := x.Memo
		mib := func(n int64) string { return fmt.Sprintf("%.1f", float64(n)/(1<<20)) }
		// A round that read the memo before its warm-up and then stopped before its clock (a setup
		// error: the warm-up was refused, left requests in flight, or the read after it failed) has
		// no read after the warm-up, and a round whose metrics did not answer at its end
		// (MemoError) none at the end: what was not read is no number.
		warm := []string{"not read", "not read"}
		end := []string{"not read", "not read", "not read", "not read"}
		if x.SetupError == "" {
			warm = []string{strconv.FormatInt(m.Warm.Entries, 10), mib(m.Warm.Bytes)}
			if x.MemoError == "" {
				end = []string{strconv.FormatInt(m.End.Entries, 10), mib(m.End.Bytes), strconv.FormatInt(m.End.Evictions-m.Warm.Evictions, 10), strconv.FormatInt(m.Hits(), 10)}
			}
		}
		rows = append(rows, fmt.Sprintf("| %s | %s, round %d | %d | %s | %s | %s |\n", BrowseLabel(x.Framework), what, x.Round, x.Attempt, yesNo(x.Valid), strings.Join(warm, " | "), strings.Join(end, " | ")))
	}
	for i := range r.Closed {
		add(fmt.Sprintf("closed, %d sessions", r.Closed[i].Sessions), &r.Closed[i])
	}
	for i := range r.Latency {
		add(fmt.Sprintf("open, %.2f visits/s", r.Latency[i].Rate), &r.Latency[i])
	}
	for i := range r.Control {
		add("read-only control", &r.Control[i])
	}
	if len(rows) == 0 {
		return
	}
	b.WriteString("\n#### What the render memo of Tifa held\n\nFrom the metrics route of each Tifa server, every round that read it: the pages and bytes the render memo held after the warm-up (right before the clock) and at the end, the pages it dropped to make room for others in the round (least recently used first, `ssr.memo_size`), and the hits it counted itself in the round, its count at the end less the one after the warm-up. Its memo was empty before the warm-up. Every try is listed, valid or not. A read that was not made or did not answer (in a round its warm-up stopped before the clock, or whose metrics route failed at its end) says not read, and nothing is worked out from it.\n\n| | round | try | valid | pages after the warm-up | MiB after the warm-up | pages at the end | MiB at the end | pages dropped in the round | hits Tifa counted in the round |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, row := range rows {
		b.WriteString(row)
	}
}

// landingMarkdown is a diagnostic for Tifa: how it answered the documents of the landings of the
// storefront with a click id, with an email parameter and with none, over the rounds. The parameters
// are no part of the page, so the three groups go to the same pages and should be answered alike.
func landingMarkdown(b *strings.Builder, servers []string, rounds func(f string) []Round) {
	var rows []string
	for _, f := range servers {
		rs := rounds(f)
		if len(rs) == 0 || slices.ContainsFunc(rs, func(x Round) bool { return x.Landing == nil }) {
			continue
		}
		for _, g := range LandingGroups {
			var answers, memo, shared, vm int
			for _, x := range rs {
				for how, n := range x.Landing[g] {
					answers += n
					switch how {
					case RenderHit:
						memo += n
					case RenderShared:
						shared += n
					case RenderVM:
						vm += n
					}
				}
			}
			allowed := "-"
			var c ExpectedCell
			for _, x := range rs {
				if x.Expected != nil {
					c = c.Sum(x.Expected.Groups[g])
				}
			}
			if c.Docs > 0 {
				allowed = fmt.Sprintf("%.1f%% of %d", 100*c.Rate(), c.Docs)
			}
			share := "-"
			if answers > 0 {
				share = fmt.Sprintf("%.1f%%", 100*float64(memo)/float64(answers))
			}
			rows = append(rows, fmt.Sprintf("| %s | with %s | %d | %d (%s) | %d | %d | %s |\n", BrowseLabel(f), landingLabels[g], answers, memo, share, shared, vm, allowed))
		}
	}
	if len(rows) == 0 {
		return
	}
	b.WriteString("\nThe documents of the landings of the storefront that Tifa answered, by the parameter their URL carries, over the rounds. A click id (`gclid`, `fbclid`, `msclkid`) is different for every click and an email parameter (`_kx`, `mc_eid`) for every recipient, and neither is part of the page, so the three groups ask for the same pages and should be answered alike. Only the documents of the storefront count (the back office has no landings with a parameter), and the warm-up is left out. The last column is the hit rate the plans allow for the group (a landing parameter is no part of the page, so it is the same for the three).\n\n| | landing documents | answers | from the memo | shared a render in progress | rendered in a VM | the plans allow from the memo |\n| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, row := range rows {
		b.WriteString(row)
	}
}

// landingLabels say what the URL of a group carries.
var landingLabels = map[string]string{LandingClickID: "a click id", LandingEmail: "an email parameter", LandingNone: "no parameter"}

// crawlerRate is the crawler requests a second of the rounds (the ones answered as expected over the
// seconds of the round), the median over them, or "-" for rounds with no crawler request.
func crawlerRate(rounds []Round) string {
	var out []float64
	for _, x := range rounds {
		if x.Crawler != nil && x.Seconds > 0 {
			out = append(out, float64(x.Crawler.Done)/x.Seconds)
		}
	}
	if len(out) == 0 {
		return "-"
	}
	return fmtCell(out, "%.1f")
}

// crawlerKinds are the kinds of URL a crawler asks for, in the order of the report (crawlKind).
var crawlerKinds = []string{"home", "list", "product", "reviews", "journal", "post"}

// crawlerMarkdown is a diagnostic for the crawler requests of the closed rounds at a number of
// sessions: how big the set of URLs is, what each server did with them, and how Tifa answered them by
// set (U or O), by language and by kind of URL. A crawler request is no visit and no page view, so it
// is in none of the counts of people above, and the CPU of the server and its memory include it.
func (r *BrowseResult) crawlerMarkdown(b *strings.Builder, sc BrowseScenario, sessions int) {
	var servers []string
	for _, f := range r.Servers[sc.Name] {
		if rs := r.cellRounds(f, sc.Name, sessions); len(rs) > 0 && slices.ContainsFunc(rs, func(x Round) bool { return x.Crawler != nil }) {
			servers = append(servers, f)
		}
	}
	if len(servers) == 0 {
		return
	}
	public, old := CrawlURLs()
	kinds := map[string]int{}
	for _, u := range public {
		if lang, _ := StorePageKind(u); lang == StoreLangs[0] {
			kinds[crawlKind(u)]++
		}
	}
	var by []string
	for _, k := range crawlerKinds {
		by = append(by, fmt.Sprintf("%s %d", k, kinds[k]))
	}
	fmt.Fprintf(b, "\nThe crawler requests (they are no visits and no page views, so no count of people above has them, and the CPU of the server and its memory include them). U holds %d URLs, %d in each language (%s), and O holds %d old URLs that answer 404. Each request is drawn from U or O by its number in the stream of crawler requests, the same for every server, and is sent with the User-Agent of a crawler. The Node frameworks may read it. In the installed sources, TanStack Start and the default entry of React Router ask `isbot`, which names every User-Agent of the mix a bot (React Router then waits for the whole page), and Next.js takes Googlebot for a bot that runs JavaScript and does not know the AI User-Agents. What each does for a bot beyond that was not checked. Each answers a crawler as it ships, so a crawler document can cost more or less than a page view of a person: that is not a fault of the test. Over the rounds:\n\n| | crawler requests planned | answered as expected | failed | body KB/request | body KB/request of an old URL (404) | answers |\n| --- | --- | --- | --- | --- | --- | --- |\n",
		len(public), len(public)/len(StoreLangs), strings.Join(by, ", "), len(old))
	for _, f := range servers {
		var planned, done, failed int
		var bytes, oldBytes int64
		statuses := map[string]int{}
		for _, x := range r.cellRounds(f, sc.Name, sessions) {
			if c := x.Crawler; c != nil {
				planned, done, failed, bytes, oldBytes = planned+c.Planned, done+c.Done, failed+c.Failed, bytes+c.Bytes, oldBytes+c.OldBytes
				for k, n := range c.Statuses {
					statuses[k] += n
				}
			}
		}
		var answers []string
		for _, k := range slices.Sorted(maps.Keys(statuses)) {
			answers = append(answers, fmt.Sprintf("%s x%d", k, statuses[k]))
		}
		old := 0
		for k, n := range statuses {
			if strings.HasPrefix(k, "O ") {
				old += n
			}
		}
		fmt.Fprintf(b, "| %s | %d | %d | %d | %.1f | %.1f | %s |\n", BrowseLabel(f), planned, done, failed, float64(bytes)/1024/float64(max(done, 1)), float64(oldBytes)/1024/float64(max(old, 1)), strings.Join(answers, ", "))
	}
	b.WriteString("\nThe old URLs are 404 pages of the shop that every server answers with a 404, but not with the same work: Next.js answers an old URL with an error document and renders no page for it on the server (the browser would draw it from the payload, and a crawler runs no script), and the other three servers render a whole page for it, which the body of an old URL above shows. Real deployments of Next.js answer so, and it is measured as shipped, as the compression of TanStack Start is. The old URLs are 5% of the crawler requests, about 0.45% of all the HTML requests.\n")
	// How Tifa answered them, by what they ask for: the set, the language and the kind of URL.
	type tally struct {
		answers, memo, shared, vm int
		why                       map[string]int
	}
	var rows []string
	for _, f := range servers {
		if !isTifa(f) {
			continue
		}
		by := map[string]*tally{}
		for _, x := range r.cellRounds(f, sc.Name, sessions) {
			if x.Crawler == nil {
				continue
			}
			for key, hows := range x.Crawler.Renders {
				parts := strings.Split(key, "|")
				for _, dim := range parts {
					if by[dim] == nil {
						by[dim] = &tally{why: map[string]int{}}
					}
					for w, n := range x.Crawler.Why[key] {
						by[dim].why[w] += n
					}
					for how, n := range hows {
						by[dim].answers += n
						switch how {
						case RenderHit:
							by[dim].memo += n
						case RenderShared:
							by[dim].shared += n
						case RenderVM:
							by[dim].vm += n
						}
					}
				}
			}
		}
		// What the plans allow, summed over the rounds: by kind of URL, and for U as the kinds together.
		allowed := map[string]ExpectedCell{}
		for _, x := range r.cellRounds(f, sc.Name, sessions) {
			if x.Expected != nil {
				for k, c := range x.Expected.Crawlers {
					allowed[k] = allowed[k].Sum(c)
				}
			}
		}
		allowed["U"] = Total(allowed)
		for _, dim := range slices.Concat([]string{"U", "O"}, StoreLangs, crawlerKinds) {
			if t := by[dim]; t != nil && t.answers > 0 {
				cell := "-"
				if c := allowed[dim]; c.Docs > 0 {
					cell = fmt.Sprintf("%.1f%% of %d", 100*c.Rate(), c.Docs)
				}
				rows = append(rows, fmt.Sprintf("| %s | %s | %d | %d (%.1f%%) | %d | %d | %s | %d | %d | %d |\n", BrowseLabel(f), dim, t.answers, t.memo, 100*float64(t.memo)/float64(t.answers), t.shared, t.vm, cell, t.why[RenderFirst], t.why[RenderChanged], t.why[RenderSame]))
			}
		}
	}
	if len(rows) == 0 {
		return
	}
	b.WriteString("\nHow Tifa answered the crawler requests (`X-Tifa-Render` on the documents), by the set they were drawn from (U or O, the old URLs, which are 404 pages that the memo does not keep), by language and by kind of URL, over the rounds. A crawler request is asked once and has no cookie and no click behind it, so a hit here is a page that a visitor or an earlier crawler request had made. The plans allow column is the share of the documents of crawlers (the old URLs are not in it) whose page was asked for before, by a visitor or a crawler request, with the same data, as for people (see the expected hit rate in the method), and the last three columns split the renders in a VM by the class the plans give the document (a kind of URL also counts the old URLs, which the plans leave out):\n\n| | answers by | answers | from the memo | shared a render in progress | rendered in a VM | the plans allow from the memo | VM: URL not seen before | VM: URL seen, data changed | VM: URL seen, data the same |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, row := range rows {
		b.WriteString(row)
	}
}
