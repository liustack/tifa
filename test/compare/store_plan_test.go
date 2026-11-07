package compare

import (
	"encoding/json/v2"
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

func TestStorePlansAreTheSameForTheSameSeedAndDifferForAnother(t *testing.T) {
	a, b, c := GenerateStorePlans(7, 50, 4), GenerateStorePlans(7, 50, 4), GenerateStorePlans(8, 50, 4)
	same := func(x, y Plan) bool { return slices.Equal(x.Steps, y.Steps) }
	if !slices.EqualFunc(a, b, same) {
		t.Error("the same seed gave different plans")
	}
	if slices.EqualFunc(a, c, same) {
		t.Error("another seed gave the same plans")
	}
}

func TestStorePlanShapes(t *testing.T) {
	plans := GenerateStorePlans(1, 20000, 4)
	var views, reloads, later, zh int
	firstKind := map[string]int{}
	for _, p := range plans {
		views += len(p.Steps)
		if p.Steps[0].Kind != StepLoad {
			t.Fatalf("plan %d starts with %s", p.ID, p.Steps[0].Kind)
		}
		lang, kind := StorePageKind(p.Steps[0].Page)
		firstKind[kind]++
		if lang == "zh-CN" {
			zh++
		}
		for i, s := range p.Steps[1:] {
			later++
			cur := p.Steps[i].Page
			if l, _ := StorePageKind(s.Page); l != lang {
				t.Fatalf("plan %d goes from %s to %s, another language", p.ID, cur, s.Page)
			}
			switch s.Kind {
			case StepReload:
				reloads++
				if s.Page != cur {
					t.Fatalf("plan %d reloads %s on %s", p.ID, s.Page, cur)
				}
			case StepClick:
				if !slices.ContainsFunc(StoreLinks(cur), func(l Link) bool { return l.To == s.Page }) {
					t.Fatalf("plan %d clicks %s on %s, which has no such link", p.ID, s.Page, cur)
				}
			default:
				t.Fatalf("plan %d has a %s step after the first", p.ID, s.Kind)
			}
		}
	}
	if got := float64(views) / float64(len(plans)); math.Abs(got-4) > 0.2 {
		t.Errorf("%.2f page views a visit, want 4", got)
	}
	if got := float64(reloads) / float64(later); math.Abs(got-ReloadShare) > 0.01 {
		t.Errorf("%.3f of the later views are reloads, want %v", got, ReloadShare)
	}
	if got := float64(zh) / float64(len(plans)); math.Abs(got-0.5) > 0.02 {
		t.Errorf("%.3f of the visits are in Chinese, want half", got)
	}
	for _, s := range StoreFirstShares {
		if got := float64(firstKind[s.Kind]) / float64(len(plans)); math.Abs(got-s.Share) > 0.02 {
			t.Errorf("%.3f of the visits start on a %s, want %v", got, s.Kind, s.Share)
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

func TestStoreWritesAreAlwaysOnesTheShopTakes(t *testing.T) {
	wantStatus := map[string]int{WriteAdd: 200, WriteQuantity: 200, WriteCoupon: 204, WriteCheckout: 303}
	wantMethod := map[string]string{WriteAdd: "POST", WriteQuantity: "PATCH", WriteCoupon: "POST", WriteCheckout: "POST"}
	kinds := map[string]int{}
	for _, p := range GenerateStorePlans(3, 3000, 4) {
		lang, _ := StorePageKind(p.Steps[0].Page)
		cart := &shopCart{lines: demoCart()}
		for _, w := range PlanStoreWrites(3, p, 1) {
			if w.Store == nil || w.Store.Kind != w.Kind || w.Store.Status != wantStatus[w.Kind] || w.Store.Method != wantMethod[w.Kind] {
				t.Fatalf("plan %d write %d: %+v", p.ID, w.After, w)
			}
			if !strings.HasPrefix(w.Store.Path, StoreBase(lang)+"/") {
				t.Fatalf("plan %d write %d: path %s is not in the language of the visit (%s)", p.ID, w.After, w.Store.Path, lang)
			}
			cart.apply(t, w)
			kinds[w.Kind]++
		}
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

func TestStoreWritesFollowTheRateAndTheSeed(t *testing.T) {
	plans := GenerateStorePlans(5, 4000, 4)
	for _, rate := range []float64{1.0 / 19, 0.25, 1} {
		views, writes := 0, 0
		for _, p := range plans {
			views += len(p.Steps)
			writes += len(PlanStoreWrites(5, p, rate))
		}
		if got := float64(writes) / float64(views); math.Abs(got-rate) > 0.01 {
			t.Errorf("rate %.3f: %.3f writes a page view", rate, got)
		}
	}
	a, b := PlanStoreWrites(5, plans[7], 1), PlanStoreWrites(5, plans[7], 1)
	if len(a) == 0 || !slices.EqualFunc(a, b, func(x, y PlannedWrite) bool {
		return x.After == y.After && string(x.Store.Body) == string(y.Store.Body) && x.Store.Path == y.Store.Path
	}) {
		t.Error("the same seed and plan gave different writes")
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
