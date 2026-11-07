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
	b.WriteString("What a visit is made of is assumed, not measured: its length (4 page views on average, so 75% of the page views are soft navigations), that 10% of the page views after the first are reloads, and the mix of reads and writes. The shares of writes are those of the TPC-W browsing, shopping and ordering mixes (https://www.tpc.org/tpcw/): of 100 user actions, a page view or a write, 5, 20 or 50 are writes, so a write follows a page view with probability 1/19, 1/4 or 1. What the writes are is assumed too. In the back office scenarios the staff of the shop work on orders: 40% of the writes place an order, 30% ship one, 20% add a note and 10% change the recipient, and each goes to the studio shop or the weekend shop with the same probability. A place-an-order write adds a new order (the shop gives it the next number) that the lists, the stats and the pages of orders with the same product or customer show. A ship write takes the oldest order that has not shipped, and a note or a recipient write takes one of them at random, among the orders that were not shipped yet when the server started and the ones placed since. A write that finds no such order is sent as a place-an-order write instead, and counted below. In the scenarios named for a customer rename, every write instead gives one of the three orders of the data that can still change a new customer name, which the lists, the stats and the other orders of that customer show, the harder case for a cache. Orders pile up while a server runs, so the lists and stats get heavier the more orders a framework places: the table of writes below says how many orders each server held at the end of a round. The conditions favor reuse of rendered pages: 58 pages that every visitor shares, almost no personalization. Times are when the server's answer reached the client, not when a browser would have shown the page. CPU is the processor time of the whole server process tree, user and system, and includes writes and prefetches.\n\n")
	if slices.ContainsFunc(bd.Scenarios, func(sc BrowseScenario) bool { return sc.mix() == MixStore }) {
		b.WriteString(storeText)
	}
	for _, sc := range bd.Scenarios {
		fmt.Fprintf(&b, "#### %s\n\n", sc.Title)
		b.WriteString("| | users | page views/s | visits/s | CPU ms/page view | CPU ms/visit | peak RSS MiB | full load P50 / P99 ms (n) | navigation P50 / P99 ms (n) | reload P50 ms (n) | prefetch P50 / P99 ms (n) | write P50 ms (n) | body KB/visit | client cores |\n| --- |")
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
				for _, run := range runs {
					if run.Load.Types[kind].N < minP99 {
						return "-" // a round with too few samples has no P99, and a median over it would mislead
					}
				}
				return fmtCell(typ(kind, func(t TypeStat) float64 { return t.P99 }), "%.0f")
			}
			p50 := func(kind string) string {
				if n(kind) == 0 {
					return "-"
				}
				return fmtCell(typ(kind, func(t TypeStat) float64 { return t.P50 }), "%.1f")
			}
			fmt.Fprintf(&b, "| %s | %d | %s | %s | %s | %s | %s | %s / %s (%d) | %s / %s (%d) | %s (%d) | %s / %s (%d) | %s (%d) | %s | %s |\n",
				runs[0].Label, runs[0].Load.Users,
				fmtCell(per(func(l *LoadResult) float64 { return float64(l.Views) / l.Seconds }), "%.2f"),
				fmtCell(per(func(l *LoadResult) float64 { return float64(l.Visits) / l.Seconds }), "%.2f"),
				fmtCell(per(func(l *LoadResult) float64 { return 1000 * l.CPU / max(1, float64(l.Views)) }), "%.2f"),
				fmtCell(per(func(l *LoadResult) float64 { return 1000 * l.CPU / visits(l) }), "%.1f"),
				fmtCell(per(func(l *LoadResult) float64 { return l.PeakRSS }), "%.0f"),
				p50("doc"), p99("doc"), n("doc"), p50("nav"), p99("nav"), n("nav"), p50("reload"), n("reload"), p50("prefetch"), p99("prefetch"), n("prefetch"), p50("write"), n("write"),
				fmtCell(per(func(l *LoadResult) float64 { return float64(l.Bytes) / 1024 / visits(l) }), "%.1f"),
				fmtCell(per(func(l *LoadResult) float64 { return l.ClientCPU / l.Seconds }), "%.2f"))
		}
		for _, spec := range specs {
			key := sc.Name + "/" + spec
			var parts []string
			for _, a := range bd.Attempts[key] {
				if a.Failed {
					parts = append(parts, fmt.Sprintf("%d users failed the rule in the rounds (%s, %s)", a.Users, a.Reason, a.Source))
				} else {
					parts = append(parts, fmt.Sprintf("%d users passed (%s)", a.Users, a.Source))
				}
			}
			if len(parts) > 0 {
				fmt.Fprintf(&b, "\nUsers used, %s: %s.", by[spec][0].Label, strings.Join(parts, "; then "))
			}
		}
		b.WriteString("\n\nP99 is printed where a type has at least 100 samples in every round. The client-cores column is the processor time of the load generator over the window: the client has two cpus, so a value near 2 means the client limits the visits a second.\n")
		b.WriteString("\nWhat one visit costs the server, as requests per visit:\n\n| | documents answered 200 (full page, HTML) | documents answered 304 | navigation requests (data of a click) | prefetches | writes | all |\n| --- | --- | --- | --- | --- | --- | --- |\n")
		for _, spec := range specs {
			runs := by[spec]
			count := func(prefix string) []float64 {
				var out []float64
				for _, run := range runs {
					n := 0
					for k, v := range run.Load.Responses {
						if strings.HasPrefix(k, prefix) {
							n += v
						}
					}
					out = append(out, float64(n)/max(1, float64(run.Load.Visits)))
				}
				return out
			}
			var all []float64
			for _, run := range runs {
				all = append(all, float64(run.Load.Requests)/max(1, float64(run.Load.Visits)))
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n", runs[0].Label, fmtCell(count("doc 200 "), "%.2f"), fmtCell(count("doc 304 "), "%.2f"), fmtCell(count("nav "), "%.2f"), fmtCell(count("prefetch "), "%.2f"), fmtCell(count("write "), "%.3f"), fmtCell(all, "%.2f"))
		}
		switch sc.mix() {
		case MixOffice:
			b.WriteString(writesTable(specs, by))
		case MixStore:
			b.WriteString(storeWritesTable(specs, by))
		}
		for _, spec := range specs {
			tifa := by[spec]
			if spec != "tifa" && tifa[0].Framework != "tifa" {
				continue
			}
			var rows []string
			for _, kind := range []string{"vm", "cache", "shared"} {
				rows = append(rows, fmt.Sprintf("%s %s", fmtCell(perVisit(tifa, func(l *LoadResult) float64 { return float64(l.Renders[kind]) }), "%.3f"), map[string]string{"vm": "renders", "cache": "cache hits", "shared": "shared renders"}[kind]))
			}
			rows = append(rows, fmtCell(perVisit(tifa, func(l *LoadResult) float64 { return float64(l.N304) }), "%.3f")+" 304 answers")
			fmt.Fprintf(&b, "\n%s, per visit: %s.\n", tifa[0].Label, strings.Join(rows, ", "))
		}
		b.WriteString("\n")
	}
	var lens []string
	for k := range bd.Common {
		lens = append(lens, k)
	}
	slices.Sort(lens)
	if len(lens) > 0 {
		b.WriteString("Visits replayed: ")
		for i, k := range lens {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%d of %d plans for %s (the plans that have a trace for every server)", bd.Common[k], bd.Plans, k)
		}
		b.WriteString(".\n\n")
	}
	b.WriteString("#### The search for the users\n\nUsers go 1, 2, 4, ... until visits/s rises by under 3% for two steps in a row, or a step has errors, or the P99 of a full load or of a whole navigation passes 1 s. The highest step that passes is used for the rounds (marked with *).\n\n")
	b.WriteString("| scenario / server | steps: users: visits/s (full load P99 ms, navigation P99 ms) | why it stopped |\n| --- | --- | --- |\n")
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
		fmt.Fprintf(&b, "| %s | %s | %s |\n", k, strings.Join(parts, "; "), CurveEnd(bd.Curves[k], bd.MaxUsers))
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
	sources := map[string][]string{}
	for _, run := range bd.Runs {
		if run.Round == 1 {
			sources[run.Source] = append(sources[run.Source], run.Scenario+"/"+run.Spec)
		}
	}
	if len(sources) > 1 {
		var names []string
		for k := range sources {
			names = append(names, k)
		}
		slices.Sort(names)
		b.WriteString("\nWhich run each row comes from: ")
		for i, k := range names {
			if i > 0 {
				b.WriteString("; ")
			}
			fmt.Fprintf(&b, "%s: %s", k, strings.Join(sources[k], ", "))
		}
		b.WriteString(". A rerun of a framework runs its search and its rounds again with fresh servers, and its rows replace the old ones in the record. Rounds that fail the rule are kept in the JSON under `rejected`.\n")
	}
	if len(bd.TraceCheck) > 0 {
		b.WriteString("\n#### Do the traces replay on this machine\n\nThe traces were recorded on another machine. One round of each framework at the users of the baseline, counted by request type, status and content type:\n\n")
		var ids []string
		for id := range bd.TraceCheck {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			c := bd.TraceCheck[id]
			fmt.Fprintf(&b, "- %s (%d users): %s\n", c.Label, c.Users, c.Summary())
		}
	}
	return b.String()
}

func perVisit(runs []BrowseRun, f func(*LoadResult) float64) []float64 {
	var out []float64
	for _, run := range runs {
		out = append(out, f(run.Load)/max(1, float64(run.Load.Visits)))
	}
	return out
}

// writesTable says, for the back office scenarios, what the writes of a round were: how many of each
// kind were sent, how many of them were a place-an-order write only because no order was left to go
// to, how long each kind took, and how many orders each project held at the end of the round.
func writesTable(specs []string, by map[string][]BrowseRun) string {
	var b strings.Builder
	b.WriteString("\nWhat the writes of a round were (median of the rounds, the range in brackets):\n\n| | place an order | ship | note | recipient | sent as a place-an-order write for lack of an order | write P50 ms: place / ship / note / recipient | orders at the end, studio / weekend |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, spec := range specs {
		runs := by[spec]
		per := func(f func(l *LoadResult) float64) []float64 {
			var out []float64
			for _, run := range runs {
				out = append(out, f(run.Load))
			}
			return out
		}
		sent := func(kind string) string {
			return fmtCell(per(func(l *LoadResult) float64 { return float64(l.Writes[kind]) }), "%.0f")
		}
		p50 := func(kind string) string {
			var out []float64
			for _, run := range runs {
				if t := run.Load.Types["write "+kind]; t.N > 0 {
					out = append(out, t.P50)
				}
			}
			return fmtCell(out, "%.1f")
		}
		orders := func(project string) string {
			return fmtCell(per(func(l *LoadResult) float64 { return float64(l.Orders[project]) }), "%.0f")
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s / %s / %s / %s | %s / %s |\n", runs[0].Label,
			sent(WriteCreate), sent(WriteShip), sent(WriteNote), sent(WriteRecipient),
			fmtCell(per(func(l *LoadResult) float64 { return float64(l.PoolEmpty) }), "%.0f"),
			p50(WriteCreate), p50(WriteShip), p50(WriteNote), p50(WriteRecipient), orders("studio"), orders("weekend"))
	}
	return b.String()
}

// storeWritesTable says, for the storefront scenarios, what the writes of a round were: how many of
// each kind were sent and how long each kind took.
func storeWritesTable(specs []string, by map[string][]BrowseRun) string {
	var b strings.Builder
	b.WriteString("\nWhat the writes of a round were (median of the rounds, the range in brackets):\n\n| | add to cart | change quantity | coupon | check out | write P50 ms: add / quantity / coupon / check out |\n| --- | --- | --- | --- | --- | --- |\n")
	kinds := []string{WriteAdd, WriteQuantity, WriteCoupon, WriteCheckout}
	for _, spec := range specs {
		runs := by[spec]
		sent := func(kind string) string {
			var out []float64
			for _, run := range runs {
				out = append(out, float64(run.Load.Writes[kind]))
			}
			return fmtCell(out, "%.0f")
		}
		p50 := func(kind string) string {
			var out []float64
			for _, run := range runs {
				if t := run.Load.Types["write "+kind]; t.N > 0 {
					out = append(out, t.P50)
				}
			}
			return fmtCell(out, "%.1f")
		}
		fmt.Fprintf(&b, "| %s |", runs[0].Label)
		for _, k := range kinds {
			fmt.Fprintf(&b, " %s |", sent(k))
		}
		fmt.Fprintf(&b, " %s / %s / %s / %s |\n", p50(kinds[0]), p50(kinds[1]), p50(kinds[2]), p50(kinds[3]))
	}
	return b.String()
}

// storeText is what the report says about the storefront scenarios, once, when a record has one.
const storeText = "The storefront scenarios replay visits to the shop's storefront (the home, the product list with its filters, 28 products, the cart, the checkout, the journal and its posts), in English or Chinese. A visit stays in one language. All of this is assumed, not measured: the language is 50/50, the first page is the home 25%, a product 35%, a product list 15%, the journal 5%, a post 10% or the cart 10%, and a click goes to one of the links a browser found on the page, with a share for each kind of link (from a list, a product 70% of the time, from a product another product 30%, the list 25%, the cart 25%, from the cart the checkout 40%). A visit starts with no cart and sees the demonstration cart of four lines that the shop shows a new visitor. Its writes follow the cart it has, so the shop takes each: 50% add a product in a colour and size it has and that is in stock, 20% change the quantity of a line in the cart (one in five removes it), 10% put a coupon on the cart or take it off, 20% check out. A quantity change or a checkout with an empty cart is an add instead, so the shares sent differ a little (the table of writes says how many of each were sent). A checkout answers 303 to the page of the order and is not followed. The cart is the visit's own (its cookie), so there is nothing shared between visits. The writes of a lower rate are not among those of a higher one, as the cart a write finds depends on the writes before it. The traces hold no writes, so what a browser fetches again after one (the cart count, a refresh of the router) is not replayed. The product pages also tell the shop which product the visitor looked at (`POST /store/mine`), and the traces keep that request with its body. Next.js, TanStack Start and React Router render every request. Tifa keeps rendered pages in two caches: the whole-page cache of the routes that have `Cache` (the home, the product list, the products and the journal, for everyone) and the render cache (a page whose data is unchanged). The storefront runs Tifa twice, once with the whole-page cache off and the render cache on, and once with both off, so that the second does the work the other frameworks do and the first shows what the render cache is worth.\n\n"
