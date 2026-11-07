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
	"tifa-nopage":  "Tifa (whole-page cache off)",
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

// cellRounds are the valid rounds of a spec in one cell of the closed table.
func (r *BrowseResult) cellRounds(fw, sc string, users int) []Round {
	var out []Round
	for _, x := range r.Closed {
		if x.Framework == fw && x.Scenario == sc && x.Users == users && x.Valid {
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

// Markdown writes the tables of the browse comparison: for every scenario, throughput at the same
// number of users, the arrival rate each server sustains and latency at the same arrival rate.
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
	b.WriteString(visitsText)
	mixes := map[string]bool{}
	for _, sc := range r.Scenarios {
		mixes[sc.Mix] = true
	}
	if mixes[MixOffice] {
		b.WriteString(officeText)
	}
	if mixes[MixRename] {
		b.WriteString(renameText)
	}
	if mixes[MixStore] {
		b.WriteString(storeText)
	}
	rounds := r.rounds()
	for _, sc := range r.Scenarios {
		r.scenarioMarkdown(&b, sc, rounds)
	}
	if checks := r.checks(); len(checks) > 0 {
		b.WriteString("\n### Do the traces replay on this machine\n\nThe answers of one round of each server (the first valid closed round at the most users of the first scenario it ran), by type of request, status and content type:\n\n")
		for _, f := range r.allServers() {
			if c := checks[f]; c != nil {
				fmt.Fprintf(&b, "- %s: %s\n", BrowseLabel(f), c.Summary())
			}
		}
	}
	b.WriteString("\nPlans:")
	for _, site := range slices.Sorted(maps.Keys(r.Plans)) {
		p := r.Plans[site]
		fmt.Fprintf(&b, " %s, %d blocks of %d visits and %d page views, %d full loads and %d clicks in all, page views by class %v.", site, p.Blocks, BlockVisits, BlockViews, p.Loads, p.Clicks, p.Pages)
	}
	b.WriteString(" Every round, with the invalid ones, is in the JSON.\n")
	return b.String()
}

func (r *BrowseResult) scenarioMarkdown(b *strings.Builder, sc BrowseScenario, rounds int) {
	fw := r.Servers[sc.Name]
	fmt.Fprintf(b, "\n### %s\n\n", sc.Title)
	users := r.usersOf(sc.Name)
	if len(users) > 0 {
		b.WriteString("#### Page views a second at the same number of users\n\nClosed loop, zero think time, every user with at most 6 connections. A cell is the median of the rounds with their range, and `failed` says why a server did not meet the rule at that many users (P99 of full loads, clicks and writes at most 1 s, no failures, no backlog): it is never measured at another number of users in its place. A page view counts when it completes. Prefetches and writes are not page views.\n\n| |")
		for _, u := range users {
			fmt.Fprintf(b, " %d users |", u)
		}
		b.WriteString("\n| --- |" + strings.Repeat(" --- |", len(users)) + "\n")
		for _, f := range fw {
			fmt.Fprintf(b, "| %s |", BrowseLabel(f))
			for _, u := range users {
				rs := r.cellRounds(f, sc.Name, u)
				if ok, why := verdict(rs, rounds); !ok {
					fmt.Fprintf(b, " failed (%s) |", why)
				} else {
					fmt.Fprintf(b, " %s |", fmtCell(each(rs, pageViewsPerSecond), "%.1f"))
				}
			}
			b.WriteString("\n")
		}
	}
	for _, u := range users {
		r.closedDetail(b, sc, u)
	}
	r.sustainMarkdown(b, sc)
	r.latencyMarkdown(b, sc)
}

// closedDetail writes what the closed rounds of a scenario at a number of users did.
func (r *BrowseResult) closedDetail(b *strings.Builder, sc BrowseScenario, users int) {
	fw := r.Servers[sc.Name]
	fmt.Fprintf(b, "\n#### %d users\n\n", users)
	b.WriteString("| | page views/s | visits/s | CPU ms/page view | peak RSS MiB | requests/page view | body KB/page view | full load P50 / P99 ms (n) | click P50 / P99 ms (n) | write P50 / P99 ms (n) | client cores |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, f := range fw {
		rs := r.cellRounds(f, sc.Name, users)
		if len(rs) == 0 {
			continue
		}
		reads := func(x *Round) float64 { return float64(x.Requests) - count(x, "write ") }
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", BrowseLabel(f),
			fmtCell(each(rs, pageViewsPerSecond), "%.1f"),
			fmtCell(each(rs, func(x *Round) float64 { return float64(x.Visits) / x.Seconds }), "%.2f"),
			fmtCell(each(rs, func(x *Round) float64 { return 1000 * x.CPUSeconds / float64(max(1, x.PageViews)) }), "%.2f"),
			fmtCell(each(rs, func(x *Round) float64 { return x.PeakRSS }), "%.0f"),
			fmtCell(each(rs, func(x *Round) float64 { return reads(x) / float64(max(1, x.PageViews)) }), "%.1f"),
			fmtCell(each(rs, func(x *Round) float64 { return float64(x.Bytes) / 1024 / float64(max(1, x.PageViews)) }), "%.1f"),
			typeCell(rs, "load"), typeCell(rs, "click"), typeCell(rs, "write"),
			fmtCell(each(rs, func(x *Round) float64 { return x.ClientCPU / x.Seconds }), "%.2f"))
	}
	b.WriteString("\nWhat the server did a second:\n\n| | full loads/s | clicks/s | writes/s | renders/s | cache hits/s | shared renders/s |\n| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, f := range fw {
		rs := r.cellRounds(f, sc.Name, users)
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
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s |\n", BrowseLabel(f), rate(func(x *Round) int { return x.FullLoads }), rate(func(x *Round) int { return x.Clicks }), rate(func(x *Round) int { return x.Writes }), tifa("vm"), tifa("cache"), tifa("shared"))
	}
	b.WriteString("\nWhat one visit costs the server, as requests per visit (and, for Tifa, what it did with the documents):\n\n| | documents 200 (full pages) | documents 304 | client navigation requests | prefetches | writes | renders | cache hits | shared renders |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, f := range fw {
		rs := r.cellRounds(f, sc.Name, users)
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
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", BrowseLabel(f), per("doc 200 "), per("doc 304 "), per("nav "), per("prefetch "), per("write "), tifa("vm"), tifa("cache"), tifa("shared"))
	}
	switch sc.Mix {
	case MixOffice:
		b.WriteString("\nWhat the writes of a round were (median of the rounds, the range in brackets):\n\n| | place an order | ship | note | recipient | sent as a place-an-order write for lack of an order | write P50 ms: place / ship / note / recipient | orders at the end, studio / weekend |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n")
		for _, f := range fw {
			rs := r.cellRounds(f, sc.Name, users)
			if len(rs) == 0 {
				continue
			}
			n := func(get func(*Round) int) string {
				return fmtCell(each(rs, func(x *Round) float64 { return float64(get(x)) }), "%.0f")
			}
			kind := func(k string) string { return n(func(x *Round) int { return x.WriteKinds[k] }) }
			fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s / %s / %s / %s | %s / %s |\n", BrowseLabel(f),
				kind(WriteCreate), kind(WriteShip), kind(WriteNote), kind(WriteRecipient), n(func(x *Round) int { return x.PoolEmpty }),
				p50Cell(rs, "write "+WriteCreate), p50Cell(rs, "write "+WriteShip), p50Cell(rs, "write "+WriteNote), p50Cell(rs, "write "+WriteRecipient),
				n(func(x *Round) int { return x.Orders["studio"] }), n(func(x *Round) int { return x.Orders["weekend"] }))
		}
	case MixStore:
		b.WriteString("\nWhat the writes of a round were (median of the rounds, the range in brackets):\n\n| | add to cart | change quantity | coupon | check out | write P50 ms: add / quantity / coupon / check out |\n| --- | --- | --- | --- | --- | --- |\n")
		for _, f := range fw {
			rs := r.cellRounds(f, sc.Name, users)
			if len(rs) == 0 {
				continue
			}
			kind := func(k string) string {
				return fmtCell(each(rs, func(x *Round) float64 { return float64(x.WriteKinds[k]) }), "%.0f")
			}
			fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s / %s / %s / %s |\n", BrowseLabel(f), kind(WriteAdd), kind(WriteQuantity), kind(WriteCoupon), kind(WriteCheckout),
				p50Cell(rs, "write "+WriteAdd), p50Cell(rs, "write "+WriteQuantity), p50Cell(rs, "write "+WriteCoupon), p50Cell(rs, "write "+WriteCheckout))
		}
	}
	b.WriteString("\nWhat the rounds achieved, against what the scenario asks for (a round is run again unless its write share is within 1 point of the scenario, its click share within 1 point of 75% and it has 4 page views a visit within 1%):\n\n| | blocks | visits | page views | full loads | clicks | writes | write share | click share | page views/visit | seconds | of which drain |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, f := range fw {
		rs := r.cellRounds(f, sc.Name, users)
		if len(rs) == 0 {
			continue
		}
		x := rs[0]
		fmt.Fprintf(b, "| %s | %d | %d | %d | %d | %d | %d | %.2f%% | %.2f%% | %.3f | %s | %s |\n", BrowseLabel(f), x.Blocks, x.Visits, x.PageViews, x.FullLoads, x.Clicks, x.Writes, 100*x.WriteShare, 100*x.ClickShare, x.ViewsPerVisit,
			fmtCell(each(rs, func(x *Round) float64 { return x.Seconds }), "%.0f"), fmtCell(each(rs, func(x *Round) float64 { return x.DrainSeconds }), "%.1f"))
	}
}

func (r *BrowseResult) sustainMarkdown(b *strings.Builder, sc BrowseScenario) {
	fw := r.Servers[sc.Name]
	if !slices.ContainsFunc(fw, func(f string) bool { return r.Sustain[sustainKey(sc.Name, f)] != nil }) {
		return
	}
	fmt.Fprintf(b, "\n#### The arrival rate each server sustains\n\nOpen model: visits start on schedule whether or not the earlier ones are done, and the actions of a visit are planned one second apart. A rate is sustained when full loads, clicks and writes each have a P99 of at most 1 s, nothing fails, no backlog is left, and the load generator keeps up, in %d rounds. The search doubles the rate, narrows it to within 10%%, then confirms it.\n\n| | sustained rate, visits/s | page views/s | notes |\n| --- | --- | --- | --- |\n", r.rounds())
	for _, f := range fw {
		s := r.Sustain[sustainKey(sc.Name, f)]
		if s == nil || !s.Done {
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
		fmt.Fprintf(b, "| %s | %.2f | %.0f | %s |\n", BrowseLabel(f), s.Rate, 4*s.Rate, note)
	}
}

func (r *BrowseResult) latencyMarkdown(b *strings.Builder, sc BrowseScenario) {
	if !slices.ContainsFunc(r.Latency, func(x Round) bool { return x.Scenario == sc.Name }) {
		return
	}
	rate := r.LatencyRate[sc.Name]
	fmt.Fprintf(b, "\n#### Latency at the same arrival rate\n\nOne rate that every server can sustain is needed, otherwise the slowest one's queue grows without end and its latency depends on how long the test runs. The slowest server of this scenario (%s) therefore sets the rate, and half of its limit leaves room, so the latency shows how fast each answers and not how long requests wait in line near saturation. Every server gets %.2f visits/s (%.0f page views/s planned). Latency is counted from the time an action was scheduled to the end of its foreground requests, queueing included. P50 / P99 in ms and the number of samples, over %d rounds.\n\n| | full loads | clicks | writes | failures | server CPU ms/page view | rounds |\n| --- | --- | --- | --- | --- | --- | --- |\n", BrowseLabel(r.LatencyBase[sc.Name]), rate, 4*rate, r.rounds())
	for _, f := range r.Servers[sc.Name] {
		var good, bad []Round
		for _, x := range r.Latency {
			if x.Framework != f || x.Scenario != sc.Name {
				continue
			}
			if x.Pass && x.Valid {
				good = append(good, x)
			} else {
				bad = append(bad, x)
			}
		}
		if len(good)+len(bad) == 0 {
			continue
		}
		var notes []string
		for _, x := range bad {
			why := x.Why
			if why == "" {
				why = x.WhyInvalid
			}
			notes = append(notes, fmt.Sprintf("round %d failed: %s", x.Round, why))
		}
		if len(good) == 0 {
			fmt.Fprintf(b, "| %s | failed | failed | failed | - | - | %s |\n", BrowseLabel(f), strings.Join(notes, "; "))
			continue
		}
		fails := 0
		for _, x := range good {
			fails += x.Failures
		}
		note := fmt.Sprintf("%d of %d rounds", len(good), len(good)+len(bad))
		if len(notes) > 0 {
			note += ", the failed ones left out: " + strings.Join(notes, "; ")
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s | %d | %s | %s |\n", BrowseLabel(f), typeCell(good, "load"), typeCell(good, "click"), typeCell(good, "write"), fails,
			fmtCell(each(good, func(x *Round) float64 { return 1000 * x.CPUSeconds / float64(max(1, x.PageViews)) }), "%.2f"), note)
	}
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

func (r *BrowseResult) usersOf(sc string) []int {
	var out []int
	for _, x := range r.Closed {
		if x.Scenario == sc && !slices.Contains(out, x.Users) {
			out = append(out, x.Users)
		}
	}
	slices.Sort(out)
	return out
}

// rounds is how many rounds a cell has when it is complete.
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
		users := r.usersOf(sc.Name)
		if len(users) == 0 {
			continue
		}
		for _, f := range r.Servers[sc.Name] {
			if out[f] != nil {
				continue
			}
			for _, x := range r.Closed {
				if x.Framework == f && x.Scenario == sc.Name && x.Users == users[len(users)-1] && x.Valid {
					out[f] = NewTraceCheck(BrowseLabel(f), &x)
					break
				}
			}
		}
	}
	return out
}

// visitsText is what the report says about the visits of every scenario.
const visitsText = "Dynamic-traffic replay of whole visits recorded in Chromium: documents and the data requests of the client router, with no scripts, styles, images or fonts, no rendering and no hydration. It measures what each framework's server does for a visitor, not how fast a browser shows a page. A visit is a full load of a page, then clicks on links of the page on screen (no reloads), as a real browser did it against that framework (Chromium, 1280x800, no hover, the browser's own prefetching, caching and route discovery). Requests the browser cancelled are cancelled at the same time. Cookies are kept within a visit and not across visits. Plans come in blocks of 95 visits and 380 page views, so every round has exactly 25% full loads, 75% client navigations and 4 page views a visit, and the writes of its scenario by quota: of 100 user actions, a page view or a write, 5, 20 or 50 are writes, as in the TPC-W browsing, shopping and ordering mixes (https://www.tpc.org/tpcw/). What a visit is made of (its length, the links clicked, what the writes are) is assumed, not measured. Every round, closed or open, starts a fresh server, and the warm-ups send no writes, so every round starts with the same data. Times are when the server's answer reached the client, not when a browser would have shown the page. CPU is the processor time of the whole server process tree, user and system, and includes writes and prefetches.\n\n"

// officeText is what the report says about the back office scenarios.
const officeText = "In the back office scenarios the staff of the shop work on orders: 40% of the writes place an order, 30% ship one, 20% add a note and 10% change the recipient, half of them in the studio shop and half in the weekend shop (by quota in every block). A place-an-order write adds a new order (the shop gives it the next number) that the lists, the stats and the pages of orders with the same product or customer show. A ship write takes the oldest order that has not shipped, and a note or a recipient write takes one of them at random, among the orders that were not shipped yet when the server started and the ones placed since. A write that finds no such order is sent as a place-an-order write instead, and counted below. Orders pile up while a server runs, so the lists and stats get heavier the more orders a framework places in a round: the table of writes says how many orders each server held at the end of a round. The conditions favor reuse of rendered pages: 58 pages that every visitor shares, almost no personalization.\n\n"

// renameText is what the report says about the scenarios where every write renames a customer.
const renameText = "In the scenarios named for a customer rename, every write gives one of the three orders of the data that can still change a customer name that no write gave before, which the lists, the stats and the other orders of that customer show, the harder case for a cache.\n\n"

// storeText is what the report says about the storefront scenarios.
const storeText = "The storefront scenarios replay visits to the shop's storefront (the home, the product list with its filters, 28 products, the cart, the checkout, the journal and its posts), in English or Chinese. A visit stays in one language. All of this is assumed, not measured: the language is half and half, the first page is the home 25%, a product 35%, a product list 15%, the journal 5%, a post 10% or the cart 10% (by quota in every block), and a click goes to one of the links a browser found on the page, with a share for each kind of link (from a list, a product 70% of the time, from a product another product 30%, the list 25%, the cart 25%, from the cart the checkout 40%). A visit starts with no cart and sees the demonstration cart of four lines that the shop shows a new visitor. Its writes follow the cart it has, so the shop takes each: 50% add a product in a colour and size it has and that is in stock, 20% change the quantity of a line in the cart (one in five removes it), 10% put a coupon on the cart or take it off, 20% check out. A quantity change or a checkout with an empty cart is an add instead, so the shares sent differ a little (the table of writes says how many of each were sent). A checkout answers 303 to the page of the order and is not followed. The cart is the visit's own (its cookie), so there is nothing shared between visits. The traces hold no writes, so what a browser fetches again after one (the cart count, a refresh of the router) is not replayed. The product pages also tell the shop which product the visitor looked at (`POST /store/mine`), and the traces keep that request with its body. Next.js, TanStack Start and React Router render every request. Tifa keeps rendered pages in two caches: the whole-page cache of the routes that have `Cache` (the home, the product list, the products and the journal, for everyone) and the render cache (a page whose data is unchanged). The storefront runs Tifa twice, once with the whole-page cache off and the render cache on, and once with both off, so that the second does the work the other frameworks do and the first shows what the render cache is worth.\n\n"
