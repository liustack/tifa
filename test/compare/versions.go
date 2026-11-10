package compare

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
)

// The data version of a page of the shop. A page has the version of the things its data is made from, and
// a write changes some of them: a version key names one. "slug:p" is the price of a product, "slug:s" its
// stock, "slug:f" whether it is in stock at all, "admin:<shop>" every page of the back office, which a
// write to either shop changes as a whole (a rough rule: a list, the stats and the page of one order are
// not all changed by a write that touches one order), "home:sale" which products are on the sale of the
// home and "bundle:slug" which are in the bundle of a product page. What a page is made from, in the
// storefront, is store_deps.json, with the products on the page that a price or a stock decides, which
// the model works out from the prices and the stock as the writes leave them (versionModel).
//
// A version counts writes, not the values written: an edit that sets the price a product has already, or
// puts back the one it had, changes the version of its pages though their data is what it was. Tifa's
// memo goes by the data, so it can hit there, and the expected rate is a little low for it.

// effect is what a write of a visit changes: the version keys, after the page view After. Only the
// writes that change the data of a page are effects: an add to the cart, a coupon and a change of a
// quantity change the cart of the visitor, which no shared page shows. Prices are the prices an edit
// sets, in fen, and Stocks the stock an edit sets, by slug.
type effect struct {
	After  int
	Kind   string
	Keys   []string
	Prices map[string]int
	Stocks map[string]int
}

func priceKey(slug string) string { return slug + ":" + depPrice }
func stockKey(slug string) string { return slug + ":" + depStock }
func flagKey(slug string) string  { return slug + ":" + depFlag }
func adminKey(shop string) string { return "admin:" + shop }

// saleKey is the version key of which products are on the sale of the home, and bundleKey of which are
// in the bundle of the page of a product.
const saleKey = "home:sale"

func bundleKey(slug string) string { return "bundle:" + slug }

// editEffect is the effect of an edit of a product in the back office that sets a price (in fen) or a
// stock or both.
func editEffect(slug string, price, stock *int) effect {
	e := effect{Kind: WriteEdit, Keys: []string{adminKey(storefrontShop)}}
	if price != nil {
		e.Keys = append(e.Keys, priceKey(slug))
		e.Prices = map[string]int{slug: *price}
	}
	if stock != nil {
		e.Keys = append(e.Keys, stockKey(slug))
		e.Stocks = map[string]int{slug: *stock}
	}
	slices.Sort(e.Keys)
	return e
}

// versionEffects are the effects of the writes of one visit, in the order the visit sends them. A
// checkout takes the pieces of its cart from the stock of the shop and places an order in it, a create
// does the same for its items, an edit of a product changes the price or the stock of that product (as
// productChange sets it), and a ship changes only the back office: its order was placed before the
// clock, so it took its pieces then. A checkout is taken never to sell a product out (the restock
// gives every product the pieces the run can buy twice over), so it changes no "in stock".
func versionEffects(seed uint64, v VisitWrites) []effect {
	takes := takesOf(seed, v)
	var out []effect
	for _, w := range sortedWrites(v.Writes) {
		var e effect
		switch w.Kind {
		case WriteCheckout, WriteCreate:
			var keys []string
			for _, t := range takes {
				if t.kind == w.Kind && t.after == w.After {
					keys = append(keys, stockKey(t.slug))
				}
			}
			shop := w.Project
			if w.Kind == WriteCheckout {
				shop = storefrontShop
			}
			e = effect{Kind: w.Kind, Keys: append(keys, adminKey(shop))}
		case WriteEdit:
			slug, change := productChange(seed, w, func(string) int { return 0 })
			if p, ok := change["priceCents"].(int); ok {
				e = editEffect(slug, &p, nil)
			} else {
				s := change["stock"].(int)
				e = editEffect(slug, nil, &s)
			}
		case WriteShip:
			e = effect{Kind: w.Kind, Keys: []string{adminKey(w.Project)}}
		default:
			continue
		}
		slices.Sort(e.Keys)
		e.Keys = slices.Compact(e.Keys)
		e.After = w.After
		out = append(out, e)
	}
	return out
}

// sortedWrites are the writes of a visit in the order it sends them.
func sortedWrites(ws []PlannedWrite) []PlannedWrite {
	out := slices.Clone(ws)
	slices.SortStableFunc(out, func(a, b PlannedWrite) int { return a.After - b.After })
	return out
}

// pageKeys are the version keys of a page URL that do not depend on the state of the shop: the entries
// of the file for a page of the storefront, and the back office of both shops for a page of the staff.
// ok is false for a URL the file has no entry for.
func (d *StoreDeps) pageKeys(page string) (keys []string, ok bool) {
	if strings.HasPrefix(page, "/projects/") {
		// A page of one shop shows something of the other (the project list links to it with its counts), so
		// a write to either shop changes the pages of both.
		return []string{adminKey("studio"), adminKey("weekend")}, true
	}
	list, ok := d.Pages[d.RenderURL(page)]
	if !ok {
		return nil, false
	}
	for _, dep := range slices.Concat(d.Common, list) {
		slug, field, has := strings.Cut(dep, ":")
		if slug == allProducts {
			for _, r := range storeCatalogue().records {
				if field == depFlag {
					keys = append(keys, flagKey(r.Slug))
				} else {
					keys = append(keys, priceKey(r.Slug))
				}
			}
			continue
		}
		switch {
		case !has:
			keys = append(keys, priceKey(slug), stockKey(slug))
		case field == depPrice:
			keys = append(keys, priceKey(slug))
		default:
			keys = append(keys, stockKey(slug))
		}
	}
	return keys, true
}

// versionModel keeps what the version of a page depends on as the writes come: the price of each
// product, whether it is in stock, and when each version key last changed (a count of writes). The
// version of a page is the last change of anything it depends on, so a page asked for twice with the
// same version has the same data.
//
// Most of what a page shows is the same products whatever the writes do, and store_deps.json has them.
// These parts of the data are not: a price or a stock decides which products are in them, so the model
// works them out with the rules of the shop (examples/shop/app/store), and a page depends on the
// products in them now and on what decides them.
//
//   - The sale of the home (Load in route.go): of the first page of the list by price, the dearest
//     first (products.Search, 24 products), those with a sale price.
//   - The products of a page of the list (products.Search in products/product.go) that sorts by price,
//     has a price range (min, max, in whole units of the currency of the language) or has only what is
//     in stock (stock=1): the products that pass its other filters, which the file has (StoreDeps.Lists),
//     filtered by the range and the stock, sorted by price when it sorts so (the order of the catalogue
//     among equals), and cut into pages of 24. The list model does that part (listFilter.cut in
//     store_list.go), here and for a list it works out whole.
//   - The best rated products below every list (products.TopRated in products/product.go, 6 of them):
//     the cheaper first among products of one rating (topRatedOf in store_list.go).
//   - The bundle of a product page (products.Bundle in products/menu.go): the two best rated products
//     of its category that come in no sizes and are in stock.
//
// The other parts keep their products: the newest, the best rated of a category, the related products,
// the menu, and a list in another order with no range.
type versionModel struct {
	d       *StoreDeps
	recs    []storeRecord
	by      map[string]storeRecord
	price   map[string]int
	inStock map[string]bool
	ids     map[string]int
	stamp   []int // when each key last changed, by its id
	clock   int
	sale    string   // the products on the sale of the home, as apply last saw them
	onSale  []string // sold and picked for the prices as they are, nil after a price changed
	picks   []string
	static  map[string][]int // the ids of the keys of a page that the file gives
}

// newVersionModel starts with the prices and the stock of the catalogue file. restocked is the stock
// the restock before the clock gives the products it raises (runner.restocked), which are in stock
// whatever the file says.
func newVersionModel(d *StoreDeps, restocked map[string]int) *versionModel {
	m := &versionModel{d: d, price: map[string]int{}, inStock: map[string]bool{}, ids: map[string]int{}, static: map[string][]int{}, by: map[string]storeRecord{}}
	m.recs = storeCatalogue().records
	for _, r := range m.recs {
		m.price[r.Slug] = r.PriceFen
		m.inStock[r.Slug] = r.Stock > 0 || restocked[r.Slug] > 0
		m.by[r.Slug] = r
	}
	m.sale = strings.Join(m.sold(), ",")
	return m
}

func (m *versionModel) id(key string) int {
	i, ok := m.ids[key]
	if !ok {
		i = len(m.stamp)
		m.ids[key] = i
		m.stamp = append(m.stamp, 0)
	}
	return i
}

// apply takes a write: it sets the prices and the stock of the effect and changes the version keys, the
// ones of the effect and those that follow from the new state (a product that is in stock or no longer
// is, other products on the sale of the home). It returns the keys it changed.
//
// An order (a checkout or a create) is taken never to sell a product out: the restock gives every
// product twice the pieces the run can buy, and an edit sets no less.
func (m *versionModel) apply(e effect) []string {
	m.clock++
	var bumped []string
	bump := func(key string) {
		m.stamp[m.id(key)] = m.clock
		bumped = append(bumped, key)
	}
	for slug, p := range e.Prices {
		m.price[slug] = p
		m.onSale, m.picks = nil, nil
	}
	for _, slug := range slices.Sorted(maps.Keys(e.Stocks)) {
		if in := e.Stocks[slug] > 0; in != m.inStock[slug] {
			// The product pages of its category that offer it in their bundle, or did, have another bundle.
			var mates []string
			had := map[string]string{}
			for _, r := range m.recs {
				if r.Category == m.by[slug].Category {
					mates = append(mates, r.Slug)
					had[r.Slug] = strings.Join(m.bundle(r.Slug), ",")
				}
			}
			m.inStock[slug] = in
			bump(flagKey(slug))
			for _, mate := range mates {
				if strings.Join(m.bundle(mate), ",") != had[mate] {
					bump(bundleKey(mate))
				}
			}
		}
	}
	for _, k := range e.Keys {
		bump(k)
	}
	if len(e.Prices) > 0 {
		if sale := strings.Join(m.sold(), ","); sale != m.sale {
			m.sale = sale
			bump(saleKey)
		}
	}
	return bumped
}

func (m *versionModel) allSlugs() []string {
	out := make([]string, len(m.recs))
	for i, r := range m.recs {
		out[i] = r.Slug
	}
	return out
}

// sold are the products on the sale of the home: of the 24 dearest, those with a sale price (Load in
// examples/shop/app/store/route.go).
func (m *versionModel) sold() []string {
	if m.onSale == nil {
		m.onSale = []string{}
		for _, slug := range sortByPrice(m.allSlugs(), m.price, true)[:min(PageSize, len(m.recs))] {
			if m.by[slug].CompareFen > 0 {
				m.onSale = append(m.onSale, slug)
			}
		}
	}
	return m.onSale
}

// picked are the best rated products below every list, with the prices as they are (topRatedOf in
// store_list.go).
func (m *versionModel) picked() []string {
	if m.picks == nil {
		m.picks = topRatedOf(m.recs, m.price, listPicks)
	}
	return m.picks
}

// bundle is the products a product page offers with the product slug: the two best rated of the others
// of its category that come in no sizes and are in stock (products.Bundle in
// examples/shop/app/store/products/menu.go).
func (m *versionModel) bundle(slug string) []string {
	self := m.by[slug]
	var members []string
	for _, r := range m.recs {
		if r.Category == self.Category && r.Slug != slug && !r.Sized && m.inStock[r.Slug] {
			members = append(members, r.Slug)
		}
	}
	slices.SortStableFunc(members, func(a, b string) int {
		ra, rb := m.by[a].Rating, m.by[b].Rating
		switch {
		case ra > rb:
			return -1
		case ra < rb:
			return 1
		}
		return 0
	})
	return members[:min(2, len(members))]
}

// priceIn is the price of a product in whole units of the currency of a language, as the list filters
// by it: storekit.Price in examples/shop/app/store/storekit, which shows a dollar price at 7.1 yuan to
// the dollar, rounded up to 10 cents.
func priceIn(fen int, lang string) int {
	if lang == "zh-CN" {
		return fen / 100
	}
	return int(math.Ceil(float64(fen)/7.1/10)) * 10 / 100
}

// listBase is the render URL of a list without what a price or a stock decides its products by: the
// range, the stock, a sort by price and the page. It names the products that pass the other filters
// of the list, in its order when it does not sort by price (StoreDeps.Lists).
func (d *StoreDeps) listBase(page string) string {
	path, query, _ := strings.Cut(page, "?")
	var parts []string
	for part := range strings.SplitSeq(query, "&") {
		switch name, value, _ := strings.Cut(part, "="); {
		case part == "", name == "page", name == "min", name == "max", name == "stock":
		case name == "sort" && byPriceSort(value):
		default:
			parts = append(parts, part)
		}
	}
	return renderedURL(path+"?"+strings.Join(parts, "&"), d.Query[storeList])
}

// listed are the products of a page of the list whose products a price or a stock decides: of the
// products that pass its other filters, which the file has, those the list model keeps and shows with
// the prices and the stock as they are (listFilter.cut in store_list.go).
func (m *versionModel) listed(page string) []string {
	products, _, _, _ := listFilterOf(page).cut(m.d.Lists[m.d.listBase(page)], m.price, m.inStock)
	return products
}

// decidedParts are the fields of the data of a page (of its own route, not of its Layout) whose
// products a price or a stock decides. The file leaves them out, and the model works them out (shown).
func decidedParts(page string) []string {
	switch _, kind := StorePageKind(page); kind {
	case StoreKindHome:
		return []string{"sale"}
	case StoreKindProduct:
		return []string{"bundle"}
	case StoreKindList:
		if priceDecidesList(page) {
			return []string{"picks", "products"}
		}
		return []string{"picks"}
	}
	return nil
}

// shown are the products in each of the decidedParts of a page, in the order the page shows them, with
// the prices and the stock the model has now.
func (m *versionModel) shown(page string) map[string][]string {
	page = stripLanding(page)
	out := map[string][]string{}
	for _, part := range decidedParts(page) {
		switch part {
		case "sale":
			out[part] = m.sold()
		case "bundle":
			out[part] = m.bundle(productOf(page))
		case "picks":
			out[part] = m.picked()
		case "products":
			out[part] = m.listed(page)
		}
	}
	return out
}

// decidedKeys are the version keys of the decidedParts of a page: the price and the stock of the
// products in them now, and what decides which products those are where the file does not have it. The
// file has all the prices for every list ("*:p") and whether each product is in stock for a list of
// what is in stock ("*:f").
func (m *versionModel) decidedKeys(page string) []string {
	page = stripLanding(page)
	var keys []string
	for _, slugs := range m.shown(page) {
		for _, s := range slugs {
			keys = append(keys, priceKey(s), stockKey(s))
		}
	}
	switch _, kind := StorePageKind(page); kind {
	case StoreKindHome:
		keys = append(keys, saleKey)
	case StoreKindProduct:
		keys = append(keys, bundleKey(productOf(page)))
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// stampOf is the version of a page: when anything it depends on last changed. It fails for a page the
// file has no entry for.
func (m *versionModel) stampOf(page string) (int, error) {
	page = stripLanding(page)
	const again = "make store_deps.json again with `compare deps`"
	ids, ok := m.static[page]
	if !ok {
		keys, found := m.d.pageKeys(page)
		if !found {
			return 0, fmt.Errorf("the dependencies have no entry for %s: %s", page, again)
		}
		if slices.Contains(decidedParts(page), "products") && m.d.Lists[m.d.listBase(page)] == nil {
			return 0, fmt.Errorf("the dependencies have no products for the list of %s: %s", page, again)
		}
		for _, k := range keys {
			ids = append(ids, m.id(k))
		}
		m.static[page] = ids
	}
	at := 0
	for _, i := range ids {
		at = max(at, m.stamp[i])
	}
	for _, k := range m.decidedKeys(page) {
		at = max(at, m.stamp[m.id(k)])
	}
	return at, nil
}
