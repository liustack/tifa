package compare

import (
	"fmt"
	"slices"
	"strings"
)

func fmtCell(values []float64, format string) string {
	if len(values) == 0 {
		return "-"
	}
	return cell(values, &metricDef{Format: fixed(format)})
}

// BrowseLabel is how a framework is named in the tables.
func BrowseLabel(spec string) string {
	switch spec {
	case "tifa":
		return "Tifa"
	case "nextjs":
		return "Next.js"
	case "start":
		return "TanStack Start"
	case "reactrouter":
		return "React Router"
	}
	return spec
}

// cellRounds are the valid rounds of a framework in one cell of the closed table.
func (r *BrowseResult) cellRounds(fw, mix string, users int) []Round {
	var out []Round
	for _, x := range r.Closed {
		if x.Framework == fw && x.Mix == mix && x.Users == users && x.Valid {
			out = append(out, x)
		}
	}
	return out
}

func each(rounds []Round, f func(*Round) float64) []float64 {
	var out []float64
	for i := range rounds {
		out = append(out, f(&rounds[i]))
	}
	return out
}

func pageViewsPerSecond(x *Round) float64 { return float64(x.PageViews) / x.Seconds }

// verdict says whether a cell passes: every round did, and there were as many as asked for.
func verdict(rounds []Round, want int) (bool, string) {
	if len(rounds) < want {
		return false, fmt.Sprintf("%d of %d rounds", len(rounds), want)
	}
	for _, x := range rounds {
		if !x.Pass {
			return false, x.Why
		}
	}
	return true, ""
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
	if len(rounds) == 0 {
		return "-"
	}
	p50 := each(rounds, func(x *Round) float64 { return x.Types[kind].P50 })
	p99 := each(rounds, func(x *Round) float64 { return x.Types[kind].P99 })
	n := 0
	for _, x := range rounds {
		n += x.Types[kind].N
	}
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("%s / %s (%d)", fmtCell(p50, "%.1f"), fmtCell(p99, "%.0f"), n)
}

// Markdown writes the tables of the browse comparison.
func (r *BrowseResult) Markdown() string {
	var b strings.Builder
	v := r.Version
	fmt.Fprintf(&b, "Measured %s. Go %s, Node %s", r.Date, strings.TrimPrefix(v["go"], "go"), v["node"])
	for _, k := range sortedKeys(r.Version) {
		if !slices.Contains([]string{"go", "node"}, k) {
			fmt.Fprintf(&b, ", %s %s", k, r.Version[k])
		}
	}
	fmt.Fprintf(&b, ". %s, %s. Placement: %s.\n\n", r.Machine.CPU, r.Machine.Kernel, r.Place.Note)
	b.WriteString("Dynamic-traffic replay of whole visits recorded in Chromium: documents and the data requests of the client router, with no scripts, styles, images or fonts, no rendering and no hydration. It measures what each framework's server does for a visitor, not how fast a browser shows a page.\n\n")
	fw := slices.Clone(r.frameworks())
	rounds := r.rounds()

	b.WriteString("### Page views a second at the same number of users\n\n")
	b.WriteString("Closed loop, zero think time, every user with at most 6 connections. A cell is the median of the rounds with their range, and `failed` says why a framework did not meet the rule at that many users (P99 of full loads, clicks and writes at most 1 s, no failures, no backlog): it is never measured at another number of users in its place. A page view counts when it completes. Prefetches and writes are not page views.\n\n")
	type col struct {
		mix   Mix
		users int
	}
	var cols []col
	for _, m := range r.Mixes {
		users := r.usersOf(m.Name)
		for _, u := range users {
			cols = append(cols, col{m, u})
		}
	}
	b.WriteString("| |")
	for _, c := range cols {
		fmt.Fprintf(&b, " %s, %d users |", c.mix.Title, c.users)
	}
	b.WriteString("\n| --- |" + strings.Repeat(" --- |", len(cols)) + "\n")
	for _, f := range fw {
		fmt.Fprintf(&b, "| %s |", BrowseLabel(f))
		for _, c := range cols {
			rs := r.cellRounds(f, c.mix.Name, c.users)
			if ok, why := verdict(rs, rounds); !ok {
				fmt.Fprintf(&b, " failed (%s) |", why)
			} else {
				fmt.Fprintf(&b, " %s |", fmtCell(each(rs, pageViewsPerSecond), "%.1f"))
			}
		}
		b.WriteString("\n")
	}

	for _, c := range cols {
		fmt.Fprintf(&b, "\n#### %s, %d users\n\n", c.mix.Title, c.users)
		b.WriteString("| | page views/s | visits/s | CPU ms/page view | peak RSS MiB | requests/page view | body KB/page view | full load P50 / P99 ms (n) | click P50 / P99 ms (n) | write P50 / P99 ms (n) | client cores |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
		for _, f := range fw {
			rs := r.cellRounds(f, c.mix.Name, c.users)
			if len(rs) == 0 {
				continue
			}
			reads := func(x *Round) float64 { return float64(x.Requests) - count(x, "write ") }
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", BrowseLabel(f),
				fmtCell(each(rs, pageViewsPerSecond), "%.1f"),
				fmtCell(each(rs, func(x *Round) float64 { return float64(x.Visits) / x.Seconds }), "%.2f"),
				fmtCell(each(rs, func(x *Round) float64 { return 1000 * x.CPUSeconds / float64(max(1, x.PageViews)) }), "%.2f"),
				fmtCell(each(rs, func(x *Round) float64 { return x.PeakRSS }), "%.0f"),
				fmtCell(each(rs, func(x *Round) float64 { return reads(x) / float64(max(1, x.PageViews)) }), "%.1f"),
				fmtCell(each(rs, func(x *Round) float64 { return float64(x.Bytes) / 1024 / float64(max(1, x.PageViews)) }), "%.1f"),
				typeCell(rs, "load"), typeCell(rs, "click"), typeCell(rs, "write"),
				fmtCell(each(rs, func(x *Round) float64 { return x.ClientCPU / x.Seconds }), "%.2f"))
		}
		b.WriteString("\nWhat one visit costs the server, as requests per visit (and, for Tifa, what it did with the documents):\n\n| | documents 200 (full pages) | documents 304 | client navigation requests | prefetches | writes | renders | cache hits | shared renders |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
		for _, f := range fw {
			rs := r.cellRounds(f, c.mix.Name, c.users)
			if len(rs) == 0 {
				continue
			}
			per := func(prefix string) string {
				return fmtCell(each(rs, func(x *Round) float64 { return count(x, prefix) / float64(max(1, x.Visits)) }), "%.2f")
			}
			tifa := func(kind string) string {
				if f != "tifa" {
					return "-"
				}
				return fmtCell(each(rs, func(x *Round) float64 { return float64(x.Renders[kind]) / float64(max(1, x.Visits)) }), "%.3f")
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", BrowseLabel(f), per("doc 200 "), per("doc 304 "), per("nav "), per("prefetch "), per("write "), tifa("vm"), tifa("cache"), tifa("shared"))
		}
		b.WriteString("\nWhat the rounds achieved, against what the mix asks for (a round is run again unless its write share is within 1 point of the mix, its click share within 1 point of 75% and it has 4 page views a visit within 1%):\n\n| | blocks | visits | page views | full loads | clicks | writes | write share | click share | page views/visit | seconds | of which drain |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
		for _, f := range fw {
			rs := r.cellRounds(f, c.mix.Name, c.users)
			if len(rs) == 0 {
				continue
			}
			x := rs[0]
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %d | %.2f%% | %.2f%% | %.3f | %s | %s |\n", BrowseLabel(f), x.Blocks, x.Visits, x.PageViews, x.FullLoads, x.Clicks, x.Writes, 100*x.WriteShare, 100*x.ClickShare, x.ViewsPerVisit,
				fmtCell(each(rs, func(x *Round) float64 { return x.Seconds }), "%.0f"), fmtCell(each(rs, func(x *Round) float64 { return x.DrainSeconds }), "%.1f"))
		}
	}

	b.WriteString("\n### The arrival rate each framework sustains\n\nOpen model: visits start on schedule whether or not the earlier ones are done, and the actions of a visit are planned one second apart. A rate is sustained when full loads, clicks and writes each have a P99 of at most 1 s, nothing fails, no backlog is left, and the load generator keeps up, in three rounds. The search doubles the rate, narrows it to within 10%, then confirms it.\n\n| | sustained rate, visits/s | page views/s | notes |\n| --- | --- | --- | --- |\n")
	for _, f := range fw {
		s := r.Sustain[f]
		if s == nil || !s.Done {
			fmt.Fprintf(&b, "| %s | - | - | not measured |\n", BrowseLabel(f))
			continue
		}
		note := s.Note
		if s.Limited {
			note = strings.TrimSpace(note + " the load generator ended the search: the rate is a lower bound")
		}
		fmt.Fprintf(&b, "| %s | %.2f | %.0f | %s |\n", BrowseLabel(f), s.Rate, 4*s.Rate, note)
	}

	if len(r.Latency) > 0 {
		fmt.Fprintf(&b, "\n### Latency at the same arrival rate\n\nOne rate that every framework can sustain is needed, otherwise the slowest one's queue grows without end and its latency depends on how long the test runs. The slowest framework (%s) therefore sets the rate, and half of its limit leaves room, so the latency shows how fast each answers and not how long requests wait in line near saturation. Every framework gets %.2f visits/s (%.0f page views/s planned). Latency is counted from the time an action was scheduled to the end of its foreground requests, queueing included. P50 / P99 in ms and the number of samples, over three rounds.\n\n| | full loads | clicks | writes | failures | server CPU ms/page view |\n| --- | --- | --- | --- | --- | --- |\n", BrowseLabel(r.LatencyBase), r.LatencyRate, 4*r.LatencyRate)
		for _, f := range fw {
			var rs []Round
			for _, x := range r.Latency {
				if x.Framework == f {
					rs = append(rs, x)
				}
			}
			if len(rs) == 0 {
				continue
			}
			fails := 0
			for _, x := range rs {
				fails += x.Failures
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %d | %s |\n", BrowseLabel(f), typeCell(rs, "load"), typeCell(rs, "click"), typeCell(rs, "write"), fails,
				fmtCell(each(rs, func(x *Round) float64 { return 1000 * x.CPUSeconds / float64(max(1, x.PageViews)) }), "%.2f"))
		}
	}

	if checks := r.checks(); len(checks) > 0 {
		b.WriteString("\n### Do the traces replay on this machine\n\nThe answers of one round of each framework (the first round of the first mix at the most users), by type of request, status and content type:\n\n")
		for _, f := range fw {
			if c := checks[f]; c != nil {
				fmt.Fprintf(&b, "- %s: %s\n", BrowseLabel(f), c.Summary())
			}
		}
	}
	fmt.Fprintf(&b, "\nPlans: %d blocks of %d visits and %d page views, %d full loads and %d clicks in all, page views by class %v. Every round, with the invalid ones, is in the JSON.\n", r.Plans.Blocks, BlockVisits, BlockViews, r.Plans.Loads, r.Plans.Clicks, r.Plans.Pages)
	return b.String()
}

func (r *BrowseResult) frameworks() []string {
	var out []string
	for _, f := range BrowseFrameworks {
		for _, x := range r.Closed {
			if x.Framework == f {
				out = append(out, f)
				break
			}
		}
	}
	for _, f := range BrowseFrameworks {
		if !slices.Contains(out, f) && r.Sustain[f] != nil {
			out = append(out, f)
		}
	}
	return out
}

func (r *BrowseResult) usersOf(mix string) []int {
	var out []int
	for _, x := range r.Closed {
		if x.Mix == mix && !slices.Contains(out, x.Users) {
			out = append(out, x.Users)
		}
	}
	slices.Sort(out)
	return out
}

// rounds is how many rounds a cell has when it is complete.
func (r *BrowseResult) rounds() int {
	n := 0
	for _, x := range r.Closed {
		n = max(n, x.Round)
	}
	return max(n, 1)
}

func (r *BrowseResult) checks() map[string]*TraceCheck {
	out := map[string]*TraceCheck{}
	if len(r.Mixes) == 0 {
		return out
	}
	users := r.usersOf(r.Mixes[0].Name)
	if len(users) == 0 {
		return out
	}
	for _, f := range r.frameworks() {
		for _, x := range r.Closed {
			if x.Framework == f && x.Mix == r.Mixes[0].Name && x.Users == users[len(users)-1] && x.Valid {
				out[f] = NewTraceCheck(BrowseLabel(f), &x)
				break
			}
		}
	}
	return out
}
