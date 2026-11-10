package compare

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// handDeps is a shop of a few pages: the home shows the kettle (price and stock), a product page its own
// product, the oak tray page only the price of the tray, and the list sorted by date no product at all.
func handDeps() *StoreDeps {
	return &StoreDeps{
		Query:  map[string][]string{storeHome: {}, storeProduct: {"reviews"}, storeList: {"page", "sort"}, adminList: {}},
		Common: []string{},
		Pages: map[string][]string{
			"/store":                               {"pour-over-kettle"},
			"/store/products/walnut-pen":           {"walnut-pen"},
			"/store/products/oak-tray":             {"oak-tray:p"},
			"/store/products?sort=newest":          {},
			"/store/products/walnut-pen?reviews=2": {"walnut-pen"},
		},
	}
}

// landing is a plan of one visit that lands on page.
func landing(id int, page string) Plan {
	return Plan{ID: id, Role: RoleShopper, Steps: []PlanStep{{Kind: StepLoad, Page: page}}}
}

// handWorkload plays the plans in order, one visit each, with the writes it is given by plan ID. Its
// traces are those of a browser whose landings alone are documents (browserTraces).
func handWorkload(plans []Plan, writes map[[2]int]PlannedWrite) *Workload {
	return &Workload{Plans: plans, Traces: browserTraces(plans), Writes: writes, Seed: 3}
}

func checkout(seq int) (PlannedWrite, PlannedWrite) {
	add := storeAdd(rand.New(rand.NewPCG(1, 2)), "/store", pageProduct("/store/products/walnut-pen"))
	co := StoreRequest{Kind: WriteCheckout, Method: "POST", Path: "/store/checkout", Body: []byte(`{}`), Status: 303}
	return PlannedWrite{After: 0, Seq: seq, Kind: WriteAdd, Store: &add}, PlannedWrite{After: 1, Seq: seq, Kind: WriteCheckout, Store: &co}
}

func TestExpectedHitsFollowThePagesAndTheWritesOfAPlan(t *testing.T) {
	click := "?gclid=" + landingValues[0].Placeholder
	add, co := checkout(0)
	plans := []Plan{
		landing(0, "/store"),       // 0: new
		landing(1, "/store"+click), // 1: the click id is no part of the page, so the same page: a hit
		{ID: 2, Role: RoleShopper, Steps: []PlanStep{{Kind: StepLoad, Page: "/store/products/walnut-pen"}, {Kind: StepClick, Page: "/store/checkout"}}}, // 2: new, and its visit checks out: the stock of the kettle and the pen change
		landing(3, "/store"),                      // 3: the kettle changed: seen before, another version
		landing(4, "/store"),                      // 4: the same version: a hit
		landing(5, "/store/products/oak-tray"),    // 5: new
		landing(6, "/store/products/walnut-pen"),  // 6: the pen's stock changed: another version
		landing(7, "/store/products?sort=newest"), // 7: new, and no write changes it
		landing(8, "/store/products?sort=newest"), // 8: a hit
	}
	// Visit 2 adds the pen and checks out (the demonstration cart holds the kettle), on its page views 0 and 1.
	add.Seq, co.Seq = 2, 2
	w := handWorkload(plans, map[[2]int]PlannedWrite{{2, 0}: add, {2, 1}: co})
	got, err := w.Expect(handDeps(), nil, expectSpan{0, len(plans), true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]ExpectedCell{
		"home":    {Docs: 4, Hits: 2, Changed: 1, New: 1},
		"product": {Docs: 3, Hits: 0, Changed: 1, New: 2},
		"list":    {Docs: 2, Hits: 1, Changed: 0, New: 1},
	}
	for kind, c := range want {
		if g := got.Humans[kind]; g != c {
			t.Errorf("%s: %+v, want %+v", kind, g, c)
		}
	}
	if len(got.Humans) != len(want) {
		t.Errorf("kinds %v, want %v", got.Humans, want)
	}
	// By the parameter the landing carries, the two requests of the home with and without a click id.
	if g := got.Groups[LandingClickID]; g.Docs != 1 || g.Hits != 1 {
		t.Errorf("landings with a click id: %+v, want the one request, a hit", g)
	}
	// The class of each document is the one the replay counts the renders of Tifa by.
	for seq, class := range map[int]docClass{0: docNew, 1: docSame, 2: docNew, 3: docChanged, 4: docSame, 6: docChanged, 8: docSame} {
		if g, _ := got.humanClass(seq, 0); g != class {
			t.Errorf("visit %d is %v, want %v", seq, g, class)
		}
	}
}

func TestThePriceOfAProductDoesNotChangeAPageThatShowsItsStockAlone(t *testing.T) {
	d := handDeps()
	d.Pages["/store"] = []string{"pour-over-kettle:s"}
	// An edit of the price of the kettle.
	var edit PlannedWrite
	for seq := 0; ; seq++ {
		edit = PlannedWrite{After: 0, Seq: seq, Kind: WriteEdit, Project: storefrontShop}
		if slug, c := productChange(3, edit, func(string) int { return 0 }); slug == "pour-over-kettle" && c["priceCents"] != nil {
			break
		}
	}
	plans := []Plan{landing(0, "/store"), landing(1, "/store")}
	w := handWorkload(plans, map[[2]int]PlannedWrite{{0, 0}: edit})
	got, err := w.Expect(d, nil, expectSpan{0, 2, true})
	if err != nil {
		t.Fatal(err)
	}
	if c := got.Humans["home"]; c.Docs != 2 || c.Hits != 1 {
		t.Errorf("a price edit changed a page that shows the stock alone: %+v", c)
	}
}

// What the warm-up showed counts as seen, and what it did to the data counts, but the warm-up is not
// counted.
func TestTheWarmUpIsSeenAndNotCounted(t *testing.T) {
	plans := []Plan{landing(0, "/store"), landing(1, "/store"), landing(2, "/store/products/oak-tray")}
	w := handWorkload(plans, nil)
	got, err := w.Expect(handDeps(), nil, expectSpan{0, 1, false}, expectSpan{1, 2, true})
	if err != nil {
		t.Fatal(err)
	}
	if c := got.Humans["home"]; c != (ExpectedCell{Docs: 1, Hits: 1}) {
		t.Errorf("home: %+v, want one hit of a page the warm-up showed", c)
	}
	if c := got.Humans["product"]; c != (ExpectedCell{Docs: 1, New: 1}) {
		t.Errorf("product: %+v, want one new page", c)
	}
	if _, ok := got.humanClass(0, 0); ok {
		t.Error("the warm-up has a class: it is no part of the round")
	}
}

func TestAPageTheDepsDoNotKnowIsAnError(t *testing.T) {
	w := handWorkload([]Plan{landing(0, "/store/products/never-seen")}, nil)
	_, err := w.Expect(handDeps(), nil, expectSpan{0, 1, true})
	if err == nil || !strings.Contains(err.Error(), "never-seen") {
		t.Errorf("error %v, want one that names the page", err)
	}
}

// bruteForce is the same count made the slow way: the version of a page is the list of the values of
// what it depends on, and the pages seen before are kept with every version they had.
func bruteForce(w *Workload, d *StoreDeps, restocked map[string]int, spans []expectSpan) (map[string]ExpectedCell, map[string]ExpectedCell, int) {
	values := map[string]int{}
	seen := map[string]bool{}
	humans, crawlers := map[string]ExpectedCell{}, map[string]ExpectedCell{}
	old := 0
	m := newVersionModel(d, restocked)
	bump := func(keys []string) {
		for _, k := range keys {
			values[k]++
		}
	}
	doc := func(url string, cells map[string]ExpectedCell, kind string, count bool) {
		keys, _ := d.pageKeys(stripLanding(url))
		keys = append(keys, m.decidedKeys(url)...)
		var v strings.Builder
		v.WriteString(d.RenderURL(stripLanding(url)) + "|")
		for _, k := range slices.Compact(slices.Sorted(slices.Values(keys))) {
			fmt.Fprintf(&v, "%s=%d;", k, values[k])
		}
		path := d.RenderURL(stripLanding(url)) + "|"
		var c ExpectedCell
		if count {
			c = cells[kind]
			c.Docs++
		}
		switch {
		case seen[v.String()]:
			c.Hits++
		case anySeen(seen, path):
			c.Changed++
		default:
			c.New++
		}
		seen[v.String()] = true
		if count {
			cells[kind] = c
		}
	}
	for _, sp := range spans {
		for tt := w.humanStart(sp.seq0); tt < w.humanStart(sp.seq0+sp.n); tt++ {
			crawler, k := w.slot(tt)
			if crawler {
				req := w.crawlOf(k)
				if req.Old {
					if sp.count {
						old++
					}
					continue
				}
				doc(req.URL, crawlers, crawlKind(req.URL), sp.count)
				continue
			}
			p := w.planFor(k)
			effects := versionEffects(w.Seed, w.stockOf(k, 1)[0])
			trace, _ := w.traceOf(p)
			subs := subsFor(w.Seed, k, p)
			for i, s := range p.Steps {
				if page := subs.apply(s.Page); trace.Steps[i].document() {
					doc(page, humans, pageClass(page), sp.count)
				}
				for _, e := range effects {
					if e.After == i {
						bump(m.apply(e))
					}
				}
			}
		}
	}
	return humans, crawlers, old
}

func anySeen(seen map[string]bool, prefix string) bool {
	for k := range seen {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// listClicks are the page views of a plan that a click to a product list makes. The plain links of the
// shop lead to a list (the category bar, the quick filters, the pages), so the tests take these for the
// clicks that load a document.
func listClicks(p Plan) []int {
	var out []int
	for i, s := range p.Steps {
		if _, kind := StorePageKind(s.Page); i > 0 && kind == StoreKindList {
			out = append(out, i)
		}
	}
	return out
}

// On the plans of the mix, with its writes and its crawlers, the count is the slow one. Some page
// views after the landing load a document, as in the browser: here every one that shows a product list
// store_deps.json has an entry for. The file has the lists a visit can land on and those of the
// crawlers, not yet the lists a search or a filter leads to, so the others stay with the router here.
func TestExpectedHitsAreWhatABruteForceCountsOnTheMix(t *testing.T) {
	d := checkedDeps(t)
	// A variant loads a document where the visit of its plan does, so a click loads one when the file
	// has the list of every form of the plan.
	forms := map[int][]Plan{}
	for _, p := range GenerateSitePlanSet(1, 1).Plans {
		for v := range 1 + len(p.Variants) {
			forms[p.ID] = append(forms[p.ID], planForm(p, v))
		}
	}
	known := func(p Plan) []int {
		var out []int
		for _, i := range listClicks(p) {
			if !slices.ContainsFunc(forms[p.ID], func(f Plan) bool {
				_, ok := d.Pages[d.RenderURL(f.Steps[i].Page)]
				return !ok || priceDecidesList(f.Steps[i].Page) && d.Lists[d.listBase(f.Steps[i].Page)] == nil
			}) {
				out = append(out, i)
			}
		}
		return out
	}
	w := siteWorkloadOf(t, 1, 1, "shop", func(p Plan) Trace { return landingTrace(p, known(p)...) })
	clicks := 0
	for _, p := range w.Plans {
		clicks += len(known(p))
	}
	if clicks < 100 {
		t.Fatalf("%d clicks of the plans load a document: too few to check them", clicks)
	}
	n := w.Scenario.blockVisits()
	spans := []expectSpan{{3 * n, n, false}, {0, 2 * n, true}}
	restocked := StockFor(w.Seed, w.stockOf(0, 4*n))[storefrontShop]
	got, err := w.Expect(d, restocked, spans...)
	if err != nil {
		t.Fatal(err)
	}
	humans, crawlers, old := bruteForce(w, d, restocked, spans)
	if len(humans) == 0 || len(crawlers) == 0 {
		t.Fatalf("no document counted: %v %v", humans, crawlers)
	}
	for kind, c := range humans {
		if got.Humans[kind] != c {
			t.Errorf("people, %s: %+v, a brute force counts %+v", kind, got.Humans[kind], c)
		}
	}
	for kind, c := range crawlers {
		if got.Crawlers[kind] != c {
			t.Errorf("crawlers, %s: %+v, a brute force counts %+v", kind, got.Crawlers[kind], c)
		}
	}
	if got.Old != old || old == 0 {
		t.Errorf("old URLs %d, a brute force counts %d", got.Old, old)
	}
	if len(got.Humans) != len(humans) || len(got.Crawlers) != len(crawlers) {
		t.Errorf("kinds: %v %v, a brute force has %v %v", got.Humans, got.Crawlers, humans, crawlers)
	}
}

// A search for a word of the visit's own asks for a page that no visit asked for before. The plan holds
// the placeholder, and the replay asks for the page of the word of the visit (subsFor), so the
// expectation takes that page too: played over and over, in the warm-up and in the round, the search
// of the plan is a new page every time. The dependencies here have the page of the placeholder as
// well, so that an expectation that forgot the word finds a page, the same for every visit, and
// counts hits.
func TestExpectTakesASearchForAWordOfTheVisitForAPageOfItsOwn(t *testing.T) {
	search := "/store/products?q=" + SearchPlaceholder
	plan := Plan{ID: 0, Role: RoleShopper, Steps: []PlanStep{
		{Kind: StepLoad, Page: "/store"},
		{Kind: StepClick, Page: search, Act: ActSearch, Term: SearchPlaceholder},
	}}
	w := handWorkload([]Plan{plan}, nil)
	const warm, counted = 30, 50
	d := handDeps()
	d.Query[storeList] = []string{"page", "q", "sort"}
	d.Pages[search] = []string{}
	words := map[string]bool{}
	for seq := range warm + counted {
		word := uniqueTerm(w.Seed, seq)
		words[word] = true
		d.Pages["/store/products?q="+word] = []string{}
	}
	if len(words) != warm+counted {
		t.Fatalf("%d visits have %d words", warm+counted, len(words))
	}
	// The warm-up plays the visits after those of the round, and counts in nothing.
	got, err := w.Expect(d, nil, expectSpan{counted, warm, false}, expectSpan{0, counted, true})
	if err != nil {
		t.Fatal(err)
	}
	if list := got.Humans["list"]; list.Docs != counted || list.New != counted || list.Hits != 0 || list.Changed != 0 {
		t.Errorf("the searches of %d visits after a warm-up of %d: %+v, want every one a new page", counted, warm, list)
	}
	if home := got.Humans["home"]; home.Docs != counted || home.Hits != counted {
		t.Errorf("the landings on the home: %+v, want every one a hit after the warm-up", home)
	}
}
