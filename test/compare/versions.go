package compare

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
)

// The data version of a page of the shop. A page has the version of the things its data is made from, and
// a write changes some of them: a version key names one. "slug:p" is the price of a product, "slug:s" the
// pieces it has in stock, "slug:l" what a card says of its stock (sold out, the pieces when lowStock or
// fewer are left, or in stock: a sale of a product in good supply does not change it), "slug:f" whether
// it is in stock at all, "admin:<shop>" every page of the back office, which a
// write to either shop changes as a whole (a rough rule: a list, the stats and the page of one order are
// not all changed by a write that touches one order), "home:sale" which products are on the sale of the
// home and "bundle:slug" which are in the bundle of a product page. What a page is made from, in the
// storefront, is store_deps.json, with the products on the page that a price or a stock decides, which
// the model works out from the prices and the stock as the writes leave them (versionModel). A page of
// the product list is not in the file: the list has too many URLs to ask a shop for each, and the
// model works out all of it (listShown).
//
// A version key counts writes, not the values written: an edit that sets the price a product has
// already, or puts back the one it had, changes the version of its pages though their data is what it
// was. Tifa's memo goes by the data, so it can hit there, and the expected rate is a little low for it.
// Two things go by the value. What a card says of a stock ("slug:l") changes when the model, which
// follows the pieces of every product, sees it change. And what a page of the product list shows of
// all the products together (its counts, its price bands, the dearest price) is compared as it is
// (pageVersion.shown): an edit of a price changes a list only when one of those comes out different,
// or the product is on the page.

// effect is what a write of a visit changes: the version keys, after the page view After. Only the
// writes that change the data of a page are effects: an add to the cart, a coupon and a change of a
// quantity change the cart of the visitor, which no shared page shows. Prices are the prices an edit
// sets, in fen, Stocks the stock an edit sets, and Takes the pieces an order takes from the stock of
// the storefront, all by slug.
type effect struct {
	After  int
	Kind   string
	Keys   []string
	Prices map[string]int
	Stocks map[string]int
	Takes  map[string]int
}

func priceKey(slug string) string { return slug + ":" + depPrice }
func stockKey(slug string) string { return slug + ":" + depStock }
func lowKey(slug string) string   { return slug + ":" + depLow }
func flagKey(slug string) string  { return slug + ":" + depFlag }

// cardStock is what a card says of a stock of pieces: 0 for sold out, the pieces when lowStock or fewer
// are left, and one number for any stock above.
func cardStock(pieces int) int {
	if pieces > lowStock {
		return lowStock + 1
	}
	return max(pieces, 0)
}
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
// clock, so it took its pieces then. target is the stock the restock gave a product, which an edit of
// the stock sets a delivery on top of (runner.stockTarget), and nil for none.
func versionEffects(seed uint64, v VisitWrites, target func(slug string) int) []effect {
	if target == nil {
		target = func(string) int { return 0 }
	}
	takes := takesOf(seed, v)
	var out []effect
	for _, w := range sortedWrites(v.Writes) {
		var e effect
		switch w.Kind {
		case WriteCheckout, WriteCreate:
			var keys []string
			pieces := map[string]int{}
			for _, t := range takes {
				if t.kind == w.Kind && t.after == w.After {
					keys = append(keys, stockKey(t.slug))
					if t.project == storefrontShop {
						pieces[t.slug] += t.pieces
					}
				}
			}
			shop := w.Project
			if w.Kind == WriteCheckout {
				shop = storefrontShop
			}
			e = effect{Kind: w.Kind, Keys: append(keys, adminKey(shop)), Takes: pieces}
		case WriteEdit:
			slug, change := productChange(seed, w, target)
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
// of the file for a page of the storefront, what every page has for a page of the product list (any
// URL of the list, with no entry in the file: the rest of a list is worked out as the writes come,
// decidedParts and listShown), and the back office of both shops for a page of the staff. ok is false
// for a URL the file has no entry for.
func (d *StoreDeps) pageKeys(page string) (keys []string, ok bool) {
	if strings.HasPrefix(page, "/projects/") {
		// A page of one shop shows something of the other (the project list links to it with its counts), so
		// a write to either shop changes the pages of both.
		return []string{adminKey("studio"), adminKey("weekend")}, true
	}
	var list []string
	if _, kind := StorePageKind(page); kind != StoreKindList {
		if list, ok = d.Pages[d.RenderURL(page)]; !ok {
			return nil, false
		}
	}
	for _, dep := range slices.Concat(d.Common, list) {
		keys = append(keys, fieldKeys(fieldsOf(dep))...)
	}
	return keys, true
}

// decidedFields is the fields a product has in a part of a page whose products a price or a stock
// decides, as the file says: the price and the pieces in stock for a file that does not.
func (d *StoreDeps) decidedFields(part string) string {
	if f, ok := d.Decided[part]; ok {
		return f
	}
	return depPrice + depStock
}

// startStock says what stock the storefront starts the clock with where it is not that of the
// catalogue file: restocked is the stock the restock gave the products it raised (runner.restocked),
// and shipped the pieces the orders of the ships took after it (setupOrders places them before the
// clock), both by slug.
type startStock struct{ restocked, shipped map[string]int }

// versionModel keeps what the version of a page depends on as the writes come: the price of each
// product, the pieces it has in stock, and when each version key last changed (a count of writes). The
// version of a page is the last change of anything it depends on, with what a list shows of all the
// products (pageVersion), so a page asked for twice with the same version has the same data.
//
// Most of what a page shows is the same products whatever the writes do, and store_deps.json has them.
// These parts of the data are not: a price or a stock decides which products are in them, so the model
// works them out with the rules of the shop (examples/shop/app/store), and a page depends on the
// products in them now and on what decides them.
//
//   - The sale of the home (Load in route.go): of the first page of the list by price, the dearest
//     first (products.Search, 24 products), those with a sale price.
//   - The products of a page of the list (products.Search in products/product.go), for any URL of the
//     list: the products that pass the filters no write changes (the search term, the category, the
//     colours, the materials, the badges, the rating), filtered by the price range (min, max, in whole
//     units of the currency of the language) and the stock (stock=1), sorted by price when the list
//     sorts so (the order of the catalogue among equals), and cut into pages of 24. The list model does
//     it (listFilter.narrowed and listFilter.cut in store_list.go). A search for a word no product has
//     gives no product, so such a page depends on the best rated below it and on what every list
//     shows of all the products (listShown).
//   - The best rated products below every list (products.TopRated in products/product.go, 6 of them):
//     the cheaper first among products of one rating (topRatedOf in store_list.go).
//   - The bundle of a product page (products.Bundle in products/menu.go): the two best rated products
//     of its category that come in no sizes and are in stock.
//
// The other parts keep their products: the newest, the best rated of a category, the related products
// and the menu.
type versionModel struct {
	d       *StoreDeps
	recs    []storeRecord
	narrow  map[string][]string // the products that pass the filters of a list that no write changes, by its URL
	by      map[string]storeRecord
	price   map[string]int
	stock   map[string]int // the pieces of each product
	inStock map[string]bool
	start   startStock
	ids     map[string]int
	stamp   []int // when each key last changed, by its id
	clock   int
	sale    string   // the products on the sale of the home, as apply last saw them
	onSale  []string // sold and picked for the prices as they are, nil after a price changed
	picks   []string
	static  map[string][]int // the ids of the keys of a page that the file gives
}

// newVersionModel starts with the prices of the catalogue file and the stock the clock starts with:
// that of the file, or what the restock raised it to (target), less the pieces the orders of the ships
// took before the clock.
func newVersionModel(d *StoreDeps, start startStock) *versionModel {
	m := &versionModel{d: d, start: start, price: map[string]int{}, stock: map[string]int{}, inStock: map[string]bool{}, ids: map[string]int{}, static: map[string][]int{}, by: map[string]storeRecord{}, narrow: map[string][]string{}}
	m.recs = storeCatalogue().records
	for _, r := range m.recs {
		m.by[r.Slug] = r
		m.price[r.Slug] = r.PriceFen
		m.stock[r.Slug] = max(m.target(r.Slug)-start.shipped[r.Slug], 0)
		m.inStock[r.Slug] = m.stock[r.Slug] > 0
	}
	m.sale = strings.Join(m.sold(), ",")
	return m
}

// target is the stock the restock gave a product of the storefront: its stock in the catalogue file,
// or what the restock raised it to (runner.stockTarget). An edit of the stock sets a delivery on top.
func (m *versionModel) target(slug string) int {
	return max(m.by[slug].Stock, m.start.restocked[slug])
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

// apply takes a write: it sets the prices of the effect, takes the pieces of an order from the stock,
// sets the stock of an edit, and changes the version keys, the ones of the effect and those that follow
// from the new state (a card that says something else of the stock, a product that is in stock or no
// longer is, other products on the sale of the home). It returns the keys it changed.
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
	// set gives a product a stock of pieces.
	set := func(slug string, pieces int) {
		was := m.stock[slug]
		m.stock[slug] = pieces
		if cardStock(was) != cardStock(pieces) {
			bump(lowKey(slug))
		}
		if in := pieces > 0; in != m.inStock[slug] {
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
	for _, slug := range slices.Sorted(maps.Keys(e.Takes)) {
		set(slug, max(m.stock[slug]-e.Takes[slug], 0))
	}
	for _, slug := range slices.Sorted(maps.Keys(e.Stocks)) {
		set(slug, e.Stocks[slug])
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

// centsIn is the price of a product in the minor unit of the currency of a language: storekit.Price in
// examples/shop/app/store/storekit, which shows a dollar price at 7.1 yuan to the dollar, rounded up to
// 10 cents.
func centsIn(fen int, lang string) int {
	if lang == "zh-CN" {
		return fen
	}
	return int(math.Ceil(float64(fen)/7.1/10)) * 10
}

// priceIn is the price of a product in whole units of the currency of a language, as the list filters
// by it.
func priceIn(fen int, lang string) int { return centsIn(fen, lang) / 100 }

// listed are the products of a page of the list, as the list model works them out with the prices and
// the stock as they are: of the products that pass the filters no write changes, which are kept for
// the URL (listFilter.narrowed), those the range and the stock keep, in the order of the list, on the
// page of the URL (listFilter.cut in store_list.go).
func (m *versionModel) listed(page string) []string {
	f := listFilterOf(page)
	narrowed, ok := m.narrow[page]
	if !ok {
		narrowed = f.narrowed(m.recs)
		m.narrow[page] = narrowed
	}
	products, _, _, _ := f.cut(narrowed, m.price, m.inStock)
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
		return []string{"picks", "products"}
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

// decidedKeys are the version keys of the decidedParts of a page: what the page has of the products in
// them now (StoreDeps.decidedFields), and what decides which products are on the sale of the home and
// in the bundle of a product. What decides the products of a list is in what the list shows
// (listShown).
func (m *versionModel) decidedKeys(page string) []string {
	page = stripLanding(page)
	var keys []string
	for part, slugs := range m.shown(page) {
		fields := m.d.decidedFields(part)
		for _, s := range slugs {
			keys = append(keys, fieldKeys(s, fields)...)
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

// pageVersion is the version of the data of a page: at is when a version key it depends on last
// changed, and shown, of a page of the product list, what it shows of all the products together
// (listShown). Two versions that are equal are of the same data.
type pageVersion struct {
	at    int
	shown string
}

// listShown is what a page of the product list shows that any product can change, as a text: the
// products of the page and the best rated below it, how many products pass the filters and on how many
// pages, the page shown, the count of each value of each filter, the count of each price band and the
// dearest price of the catalogue (Results in examples/shop/app/store/products/product.go). The list
// model works it out with the prices and the stock as they are (modelList).
func (m *versionModel) listShown(page string) string {
	p := modelList(page, m.price, m.inStock, m.picked())
	return fmt.Sprint(p.Products, p.Picks, p.Total, p.Pages, p.Filter.Page, p.Categories, p.Colors, p.Materials, p.Badges, p.Ratings, p.Bands, p.PriceMax)
}

// versionOf is the version of a page: when anything it depends on last changed, and for a list what it
// shows. It fails for a page the file has no entry for.
func (m *versionModel) versionOf(page string) (pageVersion, error) {
	page = stripLanding(page)
	const again = "make store_deps.json again with `compare deps`"
	ids, ok := m.static[page]
	if !ok {
		keys, found := m.d.pageKeys(page)
		if !found {
			return pageVersion{}, fmt.Errorf("the dependencies have no entry for %s: %s", page, again)
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
	v := pageVersion{at: at}
	if _, kind := StorePageKind(page); kind == StoreKindList {
		v.shown = m.listShown(page)
	}
	return v, nil
}
