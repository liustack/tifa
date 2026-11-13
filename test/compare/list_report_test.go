package compare

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// The tables of the render memo count the product list apart by what a URL of it is: the list of a
// search, a list with a filter other than the category, and the others (the whole list, a category, a
// sort, a page). The plans and the answers of Tifa are counted under the same names, so the report can
// set one beside the other.
func TestTheProductListIsCountedBySearchFilterAndTheRest(t *testing.T) {
	pages := map[string]string{
		"/store/products?q=cup":                                 "list, search",
		"/zh-CN/store/products?q=%E6%9D%AF&color=moss&page=2":   "list, search",
		"/store/products?q=" + SearchPlaceholder:                "list, search",
		"/store/products?color=moss&sort=newest":                "list, filter",
		"/zh-CN/store/products?min=100&max=300":                 "list, filter",
		"/store/products?category=drinkware&stock=1":            "list, filter",
		"/store/products":                                       "list, other",
		"/store/products?category=drinkware&sort=newest&page=2": "list, other",
		"/store/products?page=2&gclid=abc":                      "list, other",
		"/store":                                                "home",
		"/store/products/oak-tray":                              "product",
		"/store/cart":                                           "cart",
		"/projects/studio":                                      "staff list",
	}
	for page, want := range pages {
		if got := renderKind(page); got != want {
			t.Errorf("%s is counted as %q, want %q", page, got, want)
		}
	}

	// The plans: a landing on each list, counted by the class of the list.
	lists := []string{"/store/products?q=cup", "/store/products?color=moss&sort=newest", "/store/products?category=drinkware&sort=newest&page=2", "/zh-CN/store/products?q=%E6%9D%AF&color=moss&page=2"}
	var plans []Plan
	for i, u := range lists {
		plans = append(plans, landing(i, u))
	}
	d := handDeps()
	d.Query[storeList] = []string{"category", "color", "page", "q", "sort"}
	got, err := handWorkload(plans, nil).Expect(d, nil, expectSpan{0, len(plans), true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]ExpectedCell{"list, search": {Docs: 2, New: 2}, "list, filter": {Docs: 1, New: 1}, "list, other": {Docs: 1, New: 1}}
	if !maps.Equal(got.Humans, want) {
		t.Errorf("the plans count %v, want %v", got.Humans, want)
	}

	// The answers of Tifa: each says it was rendered in a VM, and is counted under the same names.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(RenderHeader, RenderVM)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	var traces []Trace
	for _, p := range plans {
		traces = append(traces, browserTrace(p))
	}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, traces...)
	a := newAcc()
	for seq := range plans {
		if err := visitOnce(r, a, seq); err != nil {
			t.Fatal(err)
		}
	}
	for kind, c := range want {
		if a.pageRenders[kind][RenderVM] != c.Docs || a.vmWhy[kind][RenderFirst] != c.Docs {
			t.Errorf("%s: %d answers rendered in a VM, %d of a URL not seen before, want %d of each", kind, a.pageRenders[kind][RenderVM], a.vmWhy[kind][RenderFirst], c.Docs)
		}
	}
	if len(a.pageRenders) != len(want) {
		t.Errorf("the answers are counted as %v", slices.Sorted(maps.Keys(a.pageRenders)))
	}
}

// The appendix lists the product list of Tifa in three rows, the searches, the filtered lists and the
// others, in that order where the list stood, each with what the plans allow beside what was measured,
// as every other kind of page.
func TestTheAppendixListsTheProductListBySearchFilterAndTheRest(t *testing.T) {
	sc := mustScenario("shop")
	r := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa"}}, Version: map[string]string{}, Sustain: map[string]*Sustain{}, Config: map[string]string{"sessions": "8"}}
	x := expectedRound("tifa", "home", 120, 108, 12, 100, 8, map[string]int{RenderFirst: 2, RenderChanged: 10})
	for kind, n := range map[string][5]int{
		// docs, hits, vm, memo, shared
		"list, search": {200, 40, 160, 38, 2},
		"list, filter": {100, 30, 70, 30, 0},
		"list, other":  {300, 240, 60, 230, 10},
		"product":      {400, 200, 200, 190, 10},
	} {
		x.PageRenders[kind] = map[string]int{"vm": n[2], "memo": n[3], "shared": n[4]}
		x.VMWhy[kind] = map[string]int{RenderFirst: n[2] - 10, RenderChanged: 10}
		x.Expected.Humans[kind] = ExpectedCell{Docs: n[0], Hits: n[1], New: n[0] - n[1]}
	}
	r.Closed = append(r.Closed, x)
	_, appendix, _ := strings.Cut(r.Markdown(), "\n## Appendix\n")
	rows := []string{
		"| Tifa | home | 120 | 100 | 8 | 12 | 90.0% of 120 | 90.0% of 120 |",
		"| Tifa | list, search | 200 | 38 | 2 | 160 | 20.0% of 200 | 20.0% of 200 | 150 | 10 | 0 |",
		"| Tifa | list, filter | 100 | 30 | 0 | 70 | 30.0% of 100 | 30.0% of 100 | 60 | 10 | 0 |",
		"| Tifa | list, other | 300 | 230 | 10 | 60 | 80.0% of 300 | 80.0% of 300 | 50 | 10 | 0 |",
		"| Tifa | product | 400 | 190 | 10 | 200 | 50.0% of 400 | 50.0% of 400 |",
	}
	at := -1
	for _, row := range rows {
		i := strings.Index(appendix, row)
		if i < 0 {
			t.Fatalf("the appendix lacks %q:\n%s", row, appendix)
		}
		if i < at {
			t.Errorf("%q stands before the row above it", row)
		}
		at = i
	}
	// The diagnostic of the hits by kind of page has the three as columns, in the same order.
	if !strings.Contains(appendix, "| home | list, search | list, filter | list, other | product |") {
		t.Errorf("the hits by kind of page do not list the product list by class:\n%s", appendix)
	}
	// The report says what the three are, and why a list and a product page hit less than they might.
	for _, want := range []string{
		"The product list is counted in three rows",
		"the exact stock of the 10 products of its menu",
		"every checkout changes the data of every page of the storefront",
	} {
		if !strings.Contains(appendix, want) {
			t.Errorf("the appendix lacks %q", want)
		}
	}
}

// A click is an action on a page after the landing, by the step of the plan, and the browser does some
// as a document load: a search, a filter by a plain link, the next page of a list. The latency of the
// clicks is also kept apart by which the browser did, read from the trace of the page view, and the
// appendix has it for the rounds at the same arrival rate: how many of the clicks of each server were
// document loads and how many the router took, each with its P50 and its P99.
func TestTheClicksAreAlsoTimedByHowTheBrowserDidThem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer srv.Close()
	plan := Plan{Steps: []PlanStep{
		{Kind: StepLoad, Page: "/store"},
		{Kind: StepClick, Page: "/store/products?q=cup", Act: ActSearch, Term: "cup"},
		{Kind: StepClick, Page: "/store/products/oak-tray", Act: ActLink},
		{Kind: StepClick, Page: "/store/products?category=drinkware", Act: ActFilter},
		{Kind: StepClick, Page: "/store/products?category=drinkware&sort=newest", Act: ActSort},
	}}
	r := newTestRunner(srv.URL, BrowseScenario{Name: "x"}, browserTrace(plan))
	a := newAcc()
	if err := visitOnce(r, a, 0); err != nil {
		t.Fatal(err)
	}
	if len(a.lat["load"]) != 1 || len(a.lat["click"]) != 4 || len(a.lat["click doc"]) != 2 || len(a.lat["click nav"]) != 2 {
		t.Errorf("%d full loads, %d clicks, %d of them document loads and %d by the router, want 1, 4, 2 and 2", len(a.lat["load"]), len(a.lat["click"]), len(a.lat["click doc"]), len(a.lat["click nav"]))
	}

	sc := mustScenario("shop")
	res := &BrowseResult{Scenarios: []BrowseScenario{sc}, Servers: map[string][]string{sc.Name: {"tifa", "nextjs"}}, Version: map[string]string{},
		LatencyRate: map[string]float64{"shop": 8}, LatencyBase: map[string]string{"shop": "nextjs"}, Sustain: map[string]*Sustain{"shop/nextjs": {Rate: 16, Done: true}}}
	for round := 1; round <= 2; round++ {
		for _, fw := range []string{"tifa", "nextjs"} {
			x := closedRound(fw, sc.Name, 0, round, 1000, 100, true)
			x.Model = "open"
			x.Types["click doc"] = TypeStat{N: 30, P50: 6, P99: 40}
			x.Types["click nav"] = TypeStat{N: 270, P50: 2, P99: 12}
			res.Latency = append(res.Latency, x)
		}
	}
	body, appendix, _ := strings.Cut(res.Markdown(), "\n## Appendix\n")
	for _, want := range []string{
		"| | actions after the landing | as a document load | by the router |",
		"| Tifa | 2.0 / 15 (600) | 6.0 / 40 (60) | 2.0 / 12 (540) |",
		"| Next.js | 2.0 / 15 (600) | 6.0 / 40 (60) | 2.0 / 12 (540) |",
	} {
		if !strings.Contains(appendix, want) {
			t.Errorf("the appendix lacks %q:\n%s", want, appendix)
		}
		if strings.Contains(body, want) {
			t.Errorf("the body has %q", want)
		}
	}
	// A result of a run that did not time them apart has no such table.
	for i := range res.Latency {
		delete(res.Latency[i].Types, "click doc")
		delete(res.Latency[i].Types, "click nav")
	}
	if _, appendix, _ := strings.Cut(res.Markdown(), "\n## Appendix\n"); strings.Contains(appendix, "as a document load |") {
		t.Error("a result with no clicks timed apart has the table of them")
	}
}

// The report says what the recording leaves out of the pointer, and which way that leans, where it
// says how the visits were recorded: in the words of docs/testing.md.
func TestTheReportSaysWhatTheRecordingDoesNotDoWithThePointer(t *testing.T) {
	for _, want := range []string{
		"the pointer off the page but for its clicks",
		"pass over other links and stop on them",
		"Tifa and TanStack Start preload the link a pointer lies on, and none of that is recorded or replayed, while Next.js prefetches the links that come into view, with no pointer, and all of that is",
		"This is not equal: it leans towards Tifa and TanStack Start, by an amount that was not measured",
		"a search, filters, a sort or the next page of a list",
	} {
		if !strings.Contains(visitsText, want) {
			t.Errorf("the report lacks %q where it says how the visits are played", want)
		}
	}
	if strings.Contains(visitsText, "no hover") {
		t.Error("the report still says no hover, which says less than what the recorder does")
	}
}

// The report says that every checkout changes the data of every page of the storefront: the Layout of
// the storefront shows the exact stock of the products of its menu on every page (the dependencies
// every page has, StoreDeps.Common), and every checkout of the plans takes one of them, since the cart
// a visitor starts with holds one. This holds it on the plans of two blocks.
func TestEveryCheckoutOfThePlansTakesAProductOfTheMenu(t *testing.T) {
	d := checkedDeps(t)
	if len(d.Common) != 10 {
		t.Fatalf("every page depends on %v, want the 10 products of the menu of the Layout", d.Common)
	}
	menu := map[string]bool{}
	for _, dep := range d.Common {
		if strings.Contains(dep, ":") {
			t.Fatalf("every page depends on %s, want the price and the stock of a product", dep)
		}
		menu[stockKey(dep)] = true
	}
	w := siteWorkload(t, 1, 2, "shop")
	checkouts := 0
	for seq := range w.cycleLen() {
		for _, e := range versionEffects(w.Seed, w.stockOf(seq, 1)[0]) {
			if e.Kind != WriteCheckout {
				continue
			}
			checkouts++
			if !slices.ContainsFunc(e.Keys, func(k string) bool { return menu[k] }) {
				t.Errorf("the checkout of visit %d takes %v, and no product of the menu", seq, e.Keys)
			}
		}
	}
	if checkouts != 2*13 {
		t.Errorf("%d checkouts in two blocks, want 26", checkouts)
	}
}
