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
	// The kinds that a visit lands on are all there.
	var total float64
	for _, s := range LandingShares {
		if kinds[s.Kind] == 0 {
			t.Errorf("no page of kind %s", s.Kind)
		}
		total += s.Share
	}
	if math.Abs(total-1) > 1e-9 {
		t.Errorf("the shares of the landing pages add up to %v", total)
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
// and is at most 10, a checkout needs a line and empties the cart. It holds no stock: the restock
// gives every product an add puts in a cart stock enough (StockFor).
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
		t.Fatalf("visit %d write %d: %s: %v", w.Seq, w.After, req.Body, err)
	}
	switch req.Kind {
	case WriteAdd:
		slug := strings.TrimPrefix(req.Path[strings.Index(req.Path, "/products/"):], "/products/")
		i := slices.IndexFunc(StoreVariants(), func(p StoreProduct) bool { return p.Slug == slug })
		if i < 0 {
			t.Fatalf("visit %d write %d: no product %s", w.Seq, w.After, slug)
		}
		p := StoreVariants()[i]
		sizeOK := len(p.Sizes) == 0 && in.Size == "" || slices.Contains(p.Sizes, in.Size)
		if !slices.Contains(p.Colors, in.Color) || !sizeOK {
			t.Fatalf("visit %d write %d: %s has no variant %q %q", w.Seq, w.After, slug, in.Color, in.Size)
		}
		if in.Quantity < 1 || in.Quantity > 10 {
			t.Fatalf("visit %d write %d: add %d of %s", w.Seq, w.After, in.Quantity, slug)
		}
		id := storeLineID(slug, in.Color, in.Size)
		if j := slices.IndexFunc(c.lines, func(l cartLine) bool { return l.id == id }); j >= 0 {
			c.lines[j].quantity = min(c.lines[j].quantity+in.Quantity, 10)
		} else {
			c.lines = append(c.lines, cartLine{id, slug, min(in.Quantity, 10)})
		}
	case WriteQuantity:
		j := slices.IndexFunc(c.lines, func(l cartLine) bool { return l.id == in.Line })
		if j < 0 {
			t.Fatalf("visit %d write %d: the cart has no line %s", w.Seq, w.After, in.Line)
		}
		if in.Quantity < 0 || in.Quantity > 10 {
			t.Fatalf("visit %d write %d: quantity %d of %s", w.Seq, w.After, in.Quantity, in.Line)
		}
		if in.Quantity == 0 {
			c.lines = slices.Delete(c.lines, j, j+1)
		} else {
			c.lines[j].quantity = in.Quantity
		}
	case WriteCoupon:
		if in.Code == nil || *in.Code != "" && !slices.Contains(StoreCoupons, *in.Code) {
			t.Fatalf("visit %d write %d: coupon %v", w.Seq, w.After, in.Code)
		}
	case WriteCheckout:
		if len(c.lines) == 0 {
			t.Fatalf("visit %d write %d: a checkout of an empty cart", w.Seq, w.After)
		}
		c.lines, c.coupon = nil, ""
	default:
		t.Fatalf("visit %d write %d: kind %q", w.Seq, w.After, req.Kind)
	}
}

// storeBlockWrites gives the writes of the storefront scenario name for blocks blocks, in the order

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

// siteWorkload is a workload of a scenario over blocks blocks of the plans of the site. The traces are
// those of a browser whose landings alone are documents (landingTrace): these tests look at the visits
// and the writes only.
func siteWorkload(t *testing.T, seed uint64, blocks int, name string) *Workload {
	t.Helper()
	return siteWorkloadOf(t, seed, blocks, name, func(p Plan) Trace { return landingTrace(p) })
}

// siteWorkloadOf is siteWorkload with the trace of each plan made by trace.
func siteWorkloadOf(t *testing.T, seed uint64, blocks int, name string, trace func(Plan) Trace) *Workload {
	t.Helper()
	set := GenerateSitePlanSet(seed, blocks)
	traces := map[int]Trace{}
	for _, p := range set.Plans {
		traces[p.ID] = trace(p)
	}
	w, err := NewWorkload(&set, traces, scenario(t, name), seed)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// A plan is played again and again over a round, and a cart page that is the same every time is
// answered from a render memo every time. So what a play writes is drawn for the play: an add buys
// the product of its page, in a colour, a size and a quantity drawn from the visit. Two plays of a
// plan send the same writes only when that draw comes out the same, about one time in three for a
// product of one colour and no size.
func TestStoreWritesDifferOnEveryPlayOfAPlan(t *testing.T) {
	w := siteWorkload(t, 3, 2, "shop")
	n := w.cycleLen()
	same, withWrites := 0, 0
	for s := range n {
		if w.planFor(s).Role != RoleShopper {
			continue
		}
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
	if withWrites == 0 || same*3 > withWrites {
		t.Errorf("%d of %d plans with writes send the same writes on their next play", same, withWrites)
	}
	if a, b := w.writesOf(5), w.writesOf(5); !maps.EqualFunc(a, b, func(x, y PlannedWrite) bool {
		return x.Kind == y.Kind && x.After == y.After && (x.Store == nil) == (y.Store == nil) && (x.Store == nil || x.Store.Path == y.Store.Path && string(x.Store.Body) == string(y.Store.Body))
	}) {
		t.Error("the same visit gave different writes twice")
	}
}

// Over the visits of a few cycles every shopper write is one the shop takes against the cart of its
// visit (the demonstration cart, or the cart made for a returning visit), in the language of the
// visit, with the method and status of its kind. An add buys the product of the page it follows. A
// shopper visit sends the writes its plan names, and the writes a round plans are the ones it sends.
func TestStoreWritesPerVisitAreValidAndFollowThePlan(t *testing.T) {
	wantStatus := map[string]int{WriteAdd: 200, WriteCheckout: 303}
	wantMethod := map[string]string{WriteAdd: "POST", WriteCheckout: "POST"}
	w := siteWorkload(t, 3, 2, "shop")
	n := 3 * w.cycleLen()
	sent := 0
	for s := range n {
		p := w.planFor(s)
		ws := w.writesOf(s)
		if p.Role != RoleShopper {
			sent += len(ws)
			continue
		}
		if len(ws) != len(p.Writes) {
			t.Fatalf("visit %d sends %d writes, its plan names %v", s, len(ws), p.Writes)
		}
		cart := &shopCart{lines: demoCart()}
		if p.Returning {
			for _, q := range returningCart(w.Seed, s, p) {
				cart.apply(t, PlannedWrite{Kind: q.Kind, Store: &q})
			}
		}
		lang, _ := StorePageKind(p.Steps[0].Page)
		for _, pw := range p.Writes {
			x, ok := ws[pw.After]
			if !ok || x.Kind != pw.Kind || x.Seq != s {
				t.Fatalf("visit %d: the plan names %v, the visit sends %+v", s, pw, ws)
			}
			if x.Store == nil || x.Store.Kind != x.Kind || x.Store.Status != wantStatus[x.Kind] || x.Store.Method != wantMethod[x.Kind] {
				t.Fatalf("visit %d write %d: %+v", s, x.After, x)
			}
			if !strings.HasPrefix(x.Store.Path, StoreBase(lang)+"/") {
				t.Fatalf("visit %d write %d: path %s is not in the language of the visit (%s)", s, x.After, x.Store.Path, lang)
			}
			if page := p.Steps[x.After].Page; x.Kind == WriteAdd && x.Store.Path != stripLanding(page) {
				t.Fatalf("visit %d: an add after the page %s puts %s in the cart", s, page, x.Store.Path)
			}
			cart.apply(t, x)
			sent++
		}
	}
	var r Round
	w.plan(&r, 0, n)
	if r.WritesPlanned != sent {
		t.Errorf("planned %d writes, the visits send %d", r.WritesPlanned, sent)
	}
}

// Every checkout of a round places the order of a visitor of its own: the name, the email and the
// address come from the seed and the visit, in the language of the visit, so no two checkouts of a
// round send the same order, and the same visit sends the same order to every server.
func TestEveryCheckoutHasAVisitorOfItsOwn(t *testing.T) {
	w := siteWorkload(t, 3, 2, "shop")
	seen := map[string]int{}
	checkouts := 0
	for s := range 2 * w.cycleLen() {
		for _, x := range w.writesOf(s) {
			if x.Kind != WriteCheckout {
				continue
			}
			checkouts++
			var o struct {
				Email   string         `json:"email"`
				Address map[string]any `json:"address"`
			}
			if err := json.Unmarshal(x.Store.Body, &o); err != nil {
				t.Fatal(err)
			}
			name, line1 := o.Address["name"].(string), o.Address["line1"].(string)
			if name == "" || len(name) > 100 || line1 == "" || !strings.HasSuffix(o.Email, "@example.com") {
				t.Fatalf("visit %d checks out as %q <%s> at %q", s, name, o.Email, line1)
			}
			who := name + "|" + o.Email + "|" + line1 + "|" + o.Address["line2"].(string)
			if prev, ok := seen[who]; ok {
				t.Fatalf("visits %d and %d check out as the same visitor %s", prev, s, who)
			}
			seen[who] = s
			if again := w.writesOf(s)[x.After]; string(again.Store.Body) != string(x.Store.Body) {
				t.Fatalf("visit %d sends another order on a second look", s)
			}
		}
	}
	if checkouts < 20 {
		t.Fatalf("%d checkouts", checkouts)
	}
}
