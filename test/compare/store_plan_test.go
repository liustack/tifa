package compare

import (
	"encoding/json/v2"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"
)

func TestStoreVisitPagesAreKnownPagesOnce(t *testing.T) {
	seen := map[string]bool{}
	kinds := map[string]int{}
	for _, url := range StoreVisitPages() {
		if seen[url] {
			t.Errorf("%s twice", url)
		}
		seen[url] = true
		lang, kind := StorePageKind(url)
		if kind == "" || !strings.HasPrefix(url, StoreBase(lang)) {
			t.Errorf("%s: language %q, kind %q", url, lang, kind)
		}
		kinds[kind]++
	}
	// The kinds that a first page picks from are all there.
	for _, s := range StoreFirstShares {
		if kinds[s.Kind] == 0 {
			t.Errorf("no page of kind %s", s.Kind)
		}
	}
	var total float64
	for _, s := range StoreFirstShares {
		total += s.Share
	}
	if math.Abs(total-1) > 1e-9 {
		t.Errorf("the shares of the first page add up to %v", total)
	}
}

func TestStorePageKinds(t *testing.T) {
	for url, want := range map[string][2]string{
		"/store":                         {"en-US", StoreKindHome},
		"/zh-CN/store":                   {"zh-CN", StoreKindHome},
		"/store/products":                {"en-US", StoreKindList},
		"/zh-CN/store/products?q=cup":    {"zh-CN", StoreKindList},
		"/store/products/oak-tray":       {"en-US", StoreKindProduct},
		"/store/journal":                 {"en-US", StoreKindJournal},
		"/zh-CN/store/journal/theme-css": {"zh-CN", StoreKindPost},
		"/store/cart":                    {"en-US", StoreKindCart},
		"/zh-CN/store/checkout":          {"zh-CN", StoreKindCheckout},
		"/projects/studio":               {"en-US", ""},
	} {
		if lang, kind := StorePageKind(url); lang != want[0] || kind != want[1] {
			t.Errorf("%s: %s %s, want %v", url, lang, kind, want)
		}
	}
}

// The links of the storefront are those a browser found on the pages. Every page has some, they all
// lead to a page a visit can be on, in the language of the page, and their weights sum to 1.
func TestStoreLinksLeadToVisitPagesInTheSameLanguage(t *testing.T) {
	pages := StoreVisitPages()
	for _, url := range pages {
		links := StoreLinks(url)
		if len(links) == 0 {
			t.Errorf("%s has no link a visit can click", url)
			continue
		}
		lang, _ := StorePageKind(url)
		var sum float64
		for _, l := range links {
			sum += l.Weight
			if !slices.Contains(pages, l.To) {
				t.Errorf("%s links to %s, which is no page of a visit", url, l.To)
			}
			if to, _ := StorePageKind(l.To); to != lang {
				t.Errorf("%s links to %s, in another language", url, l.To)
			}
			if l.To == url {
				t.Errorf("%s links to itself", url)
			}
		}
		if math.Abs(sum-1) > 1e-9 {
			t.Errorf("the weights of the links of %s add up to %v", url, sum)
		}
	}
}

func TestStorePlanSetIsSeededAndBlocksAreBalanced(t *testing.T) {
	a, b, c := GenerateStorePlanSet(7, 4), GenerateStorePlanSet(7, 4), GenerateStorePlanSet(8, 4)
	same := func(x, y Plan) bool { return slices.Equal(x.Steps, y.Steps) }
	if a.Site != SiteStore || !slices.EqualFunc(a.Plans, b.Plans, same) {
		t.Error("the same seed gave different plans")
	}
	if slices.EqualFunc(a.Plans, c.Plans, same) {
		t.Error("another seed gave the same plans")
	}
	var firstShares []float64
	var firstKinds []string
	for _, s := range StoreFirstShares {
		firstKinds, firstShares = append(firstKinds, s.Kind), append(firstShares, s.Share)
	}
	// The quota of each kind of first page in a block of 95 visits.
	want := map[string]int{}
	for _, k := range quota(newRand(1), BlockVisits, firstKinds, firstShares) {
		want[k]++
	}
	for blk := range 4 {
		var counts PlanCounts
		langs, kinds := map[string]int{}, map[string]int{}
		firsts := map[string]int{}
		for _, p := range a.Plans[blk*BlockVisits : (blk+1)*BlockVisits] {
			if p.Block != blk {
				t.Fatalf("plan %d is in block %d", p.ID, p.Block)
			}
			counts.Add(p, 0)
			if p.Steps[0].Kind != StepLoad {
				t.Fatalf("plan %d starts with %s", p.ID, p.Steps[0].Kind)
			}
			lang, kind := StorePageKind(p.Steps[0].Page)
			langs[lang]++
			kinds[kind]++
			firsts[p.Steps[0].Page]++
			for i, s := range p.Steps[1:] {
				cur := p.Steps[i].Page
				if l, _ := StorePageKind(s.Page); l != lang {
					t.Fatalf("plan %d goes from %s to %s, another language", p.ID, cur, s.Page)
				}
				if s.Kind != StepClick || !slices.ContainsFunc(StoreLinks(cur), func(l Link) bool { return l.To == s.Page }) {
					t.Fatalf("plan %d: %s -> %s is no click on a link of the page", p.ID, cur, s.Page)
				}
			}
		}
		if counts.Visits != BlockVisits || counts.Views != BlockViews || counts.Loads != BlockVisits || counts.Clicks != BlockViews-BlockVisits {
			t.Errorf("block %d: %+v", blk, counts)
		}
		big, small := StoreLangs[0], StoreLangs[1]
		if blk%2 == 1 {
			big, small = small, big
		}
		if langs[big] != 48 || langs[small] != 47 {
			t.Errorf("block %d: languages %v, want 48 %s and 47 %s", blk, langs, big, small)
		}
		for k, n := range want {
			if kinds[k] != n {
				t.Errorf("block %d: %d visits start on a %s, want %d", blk, kinds[k], k, n)
			}
		}
		// A first page repeats only once every page of its language and kind has been used.
		for url, n := range firsts {
			lang, kind := StorePageKind(url)
			pages := 0
			for _, u := range StoreVisitPages() {
				if l, k := StorePageKind(u); l == lang && k == kind {
					pages++
				}
			}
			if n > (langs[lang]+pages-1)/pages+1 {
				t.Errorf("block %d: %s starts %d visits", blk, url, n)
			}
		}
	}
}

// A visit can be on every page of the storefront and go on from it: every page has links to click.
func TestEveryStoreVisitPageHasLinks(t *testing.T) {
	for _, url := range StoreVisitPages() {
		if len(StoreLinks(url)) == 0 {
			t.Errorf("%s has no link a visit can click", url)
		}
	}
}

// shopCart is the cart of the shop as examples/shop/app/store/cart keeps it, written again to check
// that the writes of a plan are always ones the shop takes: a visitor starts with the demonstration
// cart, a product is added in a colour and size it has, a quantity is set on a line the cart holds
// and is at most 10 and at most the stock, a checkout needs a line and empties the cart.
type shopCart struct {
	lines  []cartLine
	coupon string
}

func (c *shopCart) apply(t *testing.T, w PlannedWrite) {
	t.Helper()
	req := w.Store
	var in struct {
		Color    string  `json:"color"`
		Size     string  `json:"size"`
		Quantity int     `json:"quantity"`
		Line     string  `json:"line"`
		Code     *string `json:"code"`
	}
	if err := json.Unmarshal(req.Body, &in); err != nil {
		t.Fatalf("plan %d write %d: %s: %v", w.Plan, w.After, req.Body, err)
	}
	switch req.Kind {
	case WriteAdd:
		slug := strings.TrimPrefix(req.Path[strings.Index(req.Path, "/products/"):], "/products/")
		i := slices.IndexFunc(StoreVariants, func(p StoreProduct) bool { return p.Slug == slug })
		if i < 0 {
			t.Fatalf("plan %d write %d: no product %s", w.Plan, w.After, slug)
		}
		p := StoreVariants[i]
		sizeOK := len(p.Sizes) == 0 && in.Size == "" || slices.Contains(p.Sizes, in.Size)
		if !slices.Contains(p.Colors, in.Color) || !sizeOK {
			t.Fatalf("plan %d write %d: %s has no variant %q %q", w.Plan, w.After, slug, in.Color, in.Size)
		}
		if p.Stock == 0 || in.Quantity < 1 || in.Quantity > 10 {
			t.Fatalf("plan %d write %d: add %d of %s with %d in stock", w.Plan, w.After, in.Quantity, slug, p.Stock)
		}
		id := storeLineID(slug, in.Color, in.Size)
		if j := slices.IndexFunc(c.lines, func(l cartLine) bool { return l.id == id }); j >= 0 {
			c.lines[j].quantity = min(c.lines[j].quantity+in.Quantity, 10, p.Stock)
		} else {
			c.lines = append(c.lines, cartLine{id, slug, min(in.Quantity, p.Stock)})
		}
	case WriteQuantity:
		j := slices.IndexFunc(c.lines, func(l cartLine) bool { return l.id == in.Line })
		if j < 0 {
			t.Fatalf("plan %d write %d: the cart has no line %s", w.Plan, w.After, in.Line)
		}
		if in.Quantity < 0 || in.Quantity > 10 || in.Quantity > storeStock(c.lines[j].slug) {
			t.Fatalf("plan %d write %d: quantity %d of %s", w.Plan, w.After, in.Quantity, in.Line)
		}
		if in.Quantity == 0 {
			c.lines = slices.Delete(c.lines, j, j+1)
		} else {
			c.lines[j].quantity = in.Quantity
		}
	case WriteCoupon:
		if in.Code == nil || *in.Code != "" && !slices.Contains(StoreCoupons, *in.Code) {
			t.Fatalf("plan %d write %d: coupon %v", w.Plan, w.After, in.Code)
		}
	case WriteCheckout:
		if len(c.lines) == 0 {
			t.Fatalf("plan %d write %d: a checkout of an empty cart", w.Plan, w.After)
		}
		c.lines, c.coupon = nil, ""
	default:
		t.Fatalf("plan %d write %d: kind %q", w.Plan, w.After, req.Kind)
	}
}

// storeBlockWrites gives the writes of the storefront scenario name for blocks blocks, in the order
// of the plans and the page views.
func storeBlockWrites(t *testing.T, seed uint64, set PlanSet, name string) []PlannedWrite {
	t.Helper()
	var out []PlannedWrite
	for blk := range set.Blocks {
		ws, err := BlockWrites(seed, set.Plans[blk*BlockVisits:(blk+1)*BlockVisits], scenario(t, name))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, ws...)
	}
	return out
}

func TestStoreWritesAreAlwaysOnesTheShopTakes(t *testing.T) {
	wantStatus := map[string]int{WriteAdd: 200, WriteQuantity: 200, WriteCoupon: 204, WriteCheckout: 303}
	wantMethod := map[string]string{WriteAdd: "POST", WriteQuantity: "PATCH", WriteCoupon: "POST", WriteCheckout: "POST"}
	set := GenerateStorePlanSet(3, 30)
	kinds := map[string]int{}
	carts := map[int]*shopCart{}
	last := map[int]int{}
	for _, w := range storeBlockWrites(t, 3, set, "store-w50") {
		p := set.Plans[w.Plan]
		lang, _ := StorePageKind(p.Steps[0].Page)
		if w.Store == nil || w.Store.Kind != w.Kind || w.Store.Status != wantStatus[w.Kind] || w.Store.Method != wantMethod[w.Kind] {
			t.Fatalf("plan %d write %d: %+v", p.ID, w.After, w)
		}
		if !strings.HasPrefix(w.Store.Path, StoreBase(lang)+"/") {
			t.Fatalf("plan %d write %d: path %s is not in the language of the visit (%s)", p.ID, w.After, w.Store.Path, lang)
		}
		if n, ok := last[w.Plan]; ok && w.After <= n {
			t.Fatalf("plan %d: write after view %d comes after the one after view %d", p.ID, w.After, n)
		}
		last[w.Plan] = w.After
		if carts[w.Plan] == nil {
			carts[w.Plan] = &shopCart{lines: demoCart()}
		}
		carts[w.Plan].apply(t, w)
		kinds[w.Kind]++
	}
	// The shares are those of StoreShares, but for the writes that find an empty cart and become an add.
	var total int
	for _, n := range kinds {
		total += n
	}
	for _, s := range StoreShares {
		got := float64(kinds[s.Kind]) / float64(total)
		if s.Kind == WriteAdd {
			if got < s.Share || got > s.Share+0.08 {
				t.Errorf("%.3f of the writes are %s, want %v and a little more", got, s.Kind, s.Share)
			}
		} else if got > s.Share || got < s.Share-0.08 {
			t.Errorf("%.3f of the writes are %s, want %v or a little less", got, s.Kind, s.Share)
		}
	}
}

// The storefront writes of a block are exactly the share of the scenario, after the same page
// views as the other scenarios of that share, and the same every time.
func TestStoreWritesFollowTheShareAndTheSeed(t *testing.T) {
	set := GenerateStorePlanSet(5, 2)
	for _, tc := range []struct {
		name   string
		perBlk int
	}{{"store-w5", 20}, {"store-w20", 95}, {"store-w50", 380}} {
		sc := scenario(t, tc.name)
		for blk := range 2 {
			plans := set.Plans[blk*BlockVisits : (blk+1)*BlockVisits]
			ws, err := BlockWrites(5, plans, sc)
			if err != nil {
				t.Fatal(err)
			}
			rename, err := BlockWrites(5, plans, BrowseScenario{Share: sc.Share, Mix: MixRename})
			if err != nil {
				t.Fatal(err)
			}
			if len(ws) != tc.perBlk || len(rename) != tc.perBlk {
				t.Fatalf("%s block %d: %d writes, want %d", tc.name, blk, len(ws), tc.perBlk)
			}
			for i := range ws {
				if ws[i].Plan != rename[i].Plan || ws[i].After != rename[i].After {
					t.Fatalf("%s: write %d after %d/%d, the rename mix after %d/%d", tc.name, i, ws[i].Plan, ws[i].After, rename[i].Plan, rename[i].After)
				}
			}
		}
	}
	a, b := storeBlockWrites(t, 5, set, "store-w20"), storeBlockWrites(t, 5, set, "store-w20")
	if len(a) == 0 || !slices.EqualFunc(a, b, func(x, y PlannedWrite) bool {
		return x.Plan == y.Plan && x.After == y.After && string(x.Store.Body) == string(y.Store.Body) && x.Store.Path == y.Store.Path
	}) {
		t.Error("the same seed and plans gave different writes")
	}
}

// The links hold the URLs a browser requests (& as it is, not &amp;). That none depends on the
// visitor is the crawl's job (compare links): it keeps a link only when a new visitor and two
// visitors with other history and carts all see it, so the products the cart suggests for its first
// line or lists as seen fall out. What is left on the cart page is the same for everyone.
func TestStoreLinksAreRequestURLs(t *testing.T) {
	if strings.Contains(string(storeLinksJSON), "&amp;") {
		t.Error("store_links.json holds an HTML-escaped URL")
	}
	for _, url := range StoreVisitPages() {
		for _, l := range StoreLinks(url) {
			if strings.Contains(l.To, "&amp;") {
				t.Errorf("%s: link %s is escaped", url, l.To)
			}
		}
	}
}

// storeWorkload is a storefront workload over blocks blocks of plans. The traces are empty: these
// tests look at the writes only.
func storeWorkload(t *testing.T, seed uint64, blocks int, name string) *Workload {
	t.Helper()
	set := GenerateStorePlanSet(seed, blocks)
	traces := map[int]Trace{}
	for _, p := range set.Plans {
		traces[p.ID] = Trace{}
	}
	w, err := NewWorkload(&set, traces, scenario(t, name), seed)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// A plan is played again and again over a round, and a cart page that is the same every time is
// answered from a render memo every time. So two plays of one plan must send other writes.
func TestStoreWritesDifferOnEveryPlayOfAPlan(t *testing.T) {
	w := storeWorkload(t, 3, 2, "store-w50")
	n := len(w.Plans)
	same, withWrites := 0, 0
	for s := range n {
		a, b := w.writesOf(s), w.writesOf(s+n)
		if len(a) == 0 {
			continue
		}
		withWrites++
		if len(a) != len(b) {
			t.Fatalf("visit %d sends %d writes, its next play %d", s, len(a), len(b))
		}
		equal := true
		for j, x := range a {
			if y, ok := b[j]; !ok || x.Store.Path != y.Store.Path || string(x.Store.Body) != string(y.Store.Body) {
				equal = false
			}
		}
		if equal {
			same++
		}
	}
	if withWrites == 0 || same*20 > withWrites {
		t.Errorf("%d of %d plans with writes send the same writes on their next play", same, withWrites)
	}
	if a, b := w.writesOf(5), w.writesOf(5); !maps.EqualFunc(a, b, func(x, y PlannedWrite) bool {
		return x.Store.Path == y.Store.Path && string(x.Store.Body) == string(y.Store.Body)
	}) {
		t.Error("the same visit gave different writes twice")
	}
}

// Over the visits of a few rounds every write is one the shop takes against the cart of its visit,
// the kinds keep the shares, and the writes a round plans are the ones it sends.
func TestStoreWritesPerVisitAreValidAndKeepTheShares(t *testing.T) {
	w := storeWorkload(t, 3, 2, "store-w50")
	n := 3 * len(w.Plans)
	kinds := map[string]int{}
	sent := 0
	for s := range n {
		cart := &shopCart{lines: demoCart()}
		ws := w.writesOf(s)
		for j := range w.planFor(s).Steps {
			if x, ok := ws[j]; ok {
				cart.apply(t, x)
				kinds[x.Kind]++
				sent++
			}
		}
	}
	var r Round
	w.plan(&r, 0, n, false)
	if r.WritesPlanned != sent {
		t.Errorf("planned %d writes, the visits send %d", r.WritesPlanned, sent)
	}
	for _, s := range StoreShares {
		got := float64(kinds[s.Kind]) / float64(sent)
		if s.Kind == WriteAdd {
			if got < s.Share || got > s.Share+0.08 {
				t.Errorf("%.3f of the writes are %s, want %v and a little more", got, s.Kind, s.Share)
			}
		} else if got > s.Share+0.02 || got < s.Share-0.08 {
			t.Errorf("%.3f of the writes are %s, want %v or a little less", got, s.Kind, s.Share)
		}
	}
}
