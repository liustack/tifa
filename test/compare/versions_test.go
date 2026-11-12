package compare

import (
	"math/rand/v2"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// visitOf is a visit that adds walnut-pen to the cart after its first page view and checks out after its
// third.
func visitOf() VisitWrites {
	add := storeAdd(rand.New(rand.NewPCG(1, 2)), "/store", pageProduct("/store/products/walnut-pen"))
	checkout := StoreRequest{Kind: WriteCheckout, Method: "POST", Path: "/store/checkout", Body: []byte(`{}`), Status: 303}
	return VisitWrites{Writes: []PlannedWrite{
		{After: 0, Seq: 5, Kind: WriteAdd, Store: &add},
		{After: 2, Seq: 5, Kind: WriteCheckout, Store: &checkout},
	}}
}

func TestACheckoutChangesTheStockOfTheProductsInItsCart(t *testing.T) {
	eff := versionEffects(1, visitOf(), nil)
	if len(eff) != 1 || eff[0].After != 2 {
		t.Fatalf("effects %+v, want the one of the checkout after view 2 (an add changes no public page)", eff)
	}
	// The demonstration cart and the pen the visit added, which keep their price.
	want := []string{"admin:studio", "ceramic-cup:s", "linen-apron:s", "pour-over-kettle:s", "reading-candle:s", "walnut-pen:s"}
	if got := slices.Sorted(slices.Values(eff[0].Keys)); !reflect.DeepEqual(got, want) {
		t.Errorf("a checkout changes %v, want %v", got, want)
	}
}

func TestStaffWritesChangeWhatTheyEdit(t *testing.T) {
	seed := uint64(7)
	byPrice, byStock := "", ""
	for seq := range 200 {
		w := PlannedWrite{After: 1, Seq: seq, Kind: WriteEdit, Project: storefrontShop}
		slug, change := productChange(seed, w, func(string) int { return 0 })
		eff := versionEffects(seed, VisitWrites{Writes: []PlannedWrite{w}}, nil)
		if _, price := change["priceCents"]; price {
			want := []string{"admin:studio", slug + ":p"}
			slices.Sort(want)
			if got := slices.Sorted(slices.Values(eff[0].Keys)); !reflect.DeepEqual(got, want) {
				t.Fatalf("an edit of the price of %s changes %v, want %v", slug, got, want)
			}
			byPrice = slug
		} else {
			want := []string{"admin:studio", slug + ":s"}
			slices.Sort(want)
			if got := slices.Sorted(slices.Values(eff[0].Keys)); !reflect.DeepEqual(got, want) {
				t.Fatalf("an edit of the stock of %s changes %v, want %v", slug, got, want)
			}
			byStock = slug
		}
	}
	if byPrice == "" || byStock == "" {
		t.Fatalf("200 edits held no price (%q) or no stock (%q)", byPrice, byStock)
	}
	// A ship changes the back office of its shop and nothing the storefront shows: the order was
	// placed before the clock.
	ship := PlannedWrite{After: 0, Seq: 3, Kind: WriteShip, Project: "studio"}
	eff := versionEffects(seed, VisitWrites{Writes: []PlannedWrite{ship}}, nil)
	if len(eff) != 1 || !slices.Equal(eff[0].Keys, []string{"admin:studio"}) {
		t.Errorf("a ship changes %+v, want the back office of its shop alone", eff)
	}
}

func TestAReturningVisitsCartIsInItsCheckout(t *testing.T) {
	v := visitOf()
	v.Setup = returningCart(1, 5, Plan{Steps: []PlanStep{{Kind: StepLoad, Page: "/store"}}})
	eff := versionEffects(1, v, nil)
	got := map[string]bool{}
	for _, k := range eff[0].Keys {
		got[k] = true
	}
	// The cart of a returning visit holds what the setup added besides the demonstration cart.
	for _, r := range v.Setup {
		slug := r.Path[len("/store/products/"):]
		if !got[slug+":s"] {
			t.Errorf("the checkout of a returning visit does not change the stock of %s, which the setup put in its cart: %v", slug, eff[0].Keys)
		}
	}
}

// modelOf is a model over the real catalogue with the pages of handDeps. The pages of the product list
// need no entry: the model works them out.
func modelOf(t *testing.T) *versionModel {
	t.Helper()
	d := handDeps()
	d.Pages["/store/products/oak-tray"] = []string{}
	return newVersionModel(d, startStock{})
}

func recordOf(slug string) storeRecord {
	for _, r := range storeCatalogue().records {
		if r.Slug == slug {
			return r
		}
	}
	panic("no product " + slug)
}

// byPrice are the slugs of the catalogue, the dearest first, the first of the catalogue first among equals.
func byPrice() []string {
	rs := slices.Clone(storeCatalogue().records)
	slices.SortStableFunc(rs, func(a, b storeRecord) int { return b.PriceFen - a.PriceFen })
	var out []string
	for _, r := range rs {
		out = append(out, r.Slug)
	}
	return out
}

// The sale on the home is the products with a sale price among the 24 dearest. A product that was not on
// it and is made the dearest comes onto it, and then a change of its stock is a change of the home. A
// product that stays off it changes nothing, and the price put back takes it off again.
func TestAPriceEditThatBringsAProductIntoTheSaleChangesTheHomeAndItsLaterStock(t *testing.T) {
	m := modelOf(t)
	sale := func() []string { return m.shown("/store")["sale"] }
	top := byPrice()[:24]
	var out string // a product with a sale price below the 24 dearest
	for _, r := range storeCatalogue().records {
		if r.CompareFen > 0 && !slices.Contains(top, r.Slug) {
			out = r.Slug
			break
		}
	}
	if out == "" {
		t.Skip("every product with a sale price is among the 24 dearest")
	}
	if slices.Contains(sale(), out) {
		t.Fatalf("%s is on the sale already", out)
	}
	at := func() pageVersion {
		v, err := m.versionOf("/store")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	before := at()
	// A price edit of a product that is off the sale, and stays off.
	m.apply(editEffect(out, intp(recordOf(out).PriceFen+100), nil))
	if at() != before {
		t.Error("the price of a product off the sale changed the home")
	}
	// Made the dearest, it comes on.
	m.apply(editEffect(out, intp(9_999_900), nil))
	if !slices.Contains(sale(), out) {
		t.Fatalf("%s is not on the sale at 99,999 yuan: %v", out, sale())
	}
	onSale := at()
	if onSale == before {
		t.Error("a product came onto the sale and the home did not change")
	}
	// Its stock is on the page now.
	m.apply(editEffect(out, nil, intp(7000)))
	if at() == onSale {
		t.Error("the stock of a product on the sale did not change the home")
	}
	// Put back, it is off, and the home is not what it was with the product on it.
	now := at()
	m.apply(editEffect(out, intp(recordOf(out).PriceFen), nil))
	if slices.Contains(sale(), out) || at() == now {
		t.Errorf("the price put back: on the sale %v, home changed %v", slices.Contains(sale(), out), at() != now)
	}
}

func intp(n int) *int { return &n }

// A product that a price edit moves into the first page of the list by price is on it from then on: its
// stock or an order changes the page, where the stock of a product that is not on it does not.
func TestAProductThatAPriceEditMovesOntoAPageOfTheListHasItsStockOnIt(t *testing.T) {
	m := modelOf(t)
	page := "/store/products?sort=price-desc"
	cheapest := byPrice()[len(byPrice())-1]
	at := func() pageVersion {
		v, err := m.versionOf(page)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if slices.Contains(m.shown(page)["products"], cheapest) {
		t.Fatalf("%s is on the first page of the list by price", cheapest)
	}
	v0 := at()
	m.apply(editEffect(cheapest, nil, intp(5000)))
	if at() != v0 {
		t.Error("the stock of a product that is not on the page changed it")
	}
	m.apply(editEffect(cheapest, intp(9_999_900), nil))
	if !slices.Contains(m.shown(page)["products"], cheapest) {
		t.Fatalf("the dearest product is not on the first page: %v", m.shown(page)["products"])
	}
	v1 := at()
	m.apply(editEffect(cheapest, nil, intp(4000)))
	if at() == v1 {
		t.Error("the stock of a product that a price edit moved onto the page did not change it")
	}
	// A checkout of it is a change of its stock too.
	v2 := at()
	m.apply(effect{Kind: WriteCheckout, Keys: []string{stockKey(cheapest), adminKey(storefrontShop)}})
	if at() == v2 {
		t.Error("an order of a product on the page did not change it")
	}
}

// The products a product page offers as a bundle are in stock, so a product that was sold out and is
// in stock has a place among them if it is one of the best rated: the pages of its category change.
func TestARestockedProductEntersTheBundleOfTheProductsOfItsCategory(t *testing.T) {
	m := modelOf(t)
	for _, z := range storeCatalogue().records {
		if z.Stock != 0 || z.Sized {
			continue
		}
		for _, p := range storeCatalogue().records {
			if p.Slug == z.Slug || p.Category != z.Category {
				continue
			}
			page := "/store/products/" + p.Slug
			m.d.Pages[page] = []string{}
			before := m.shown(page)["bundle"]
			if slices.Contains(before, z.Slug) {
				t.Fatalf("%s is out of stock and in the bundle of %s", z.Slug, p.Slug)
			}
			// The two best rated products of the category that are in stock and have no sizes.
			var mates []storeRecord
			for _, r := range storeCatalogue().records {
				if r.Category == z.Category && r.Slug != p.Slug && !r.Sized && (r.Stock > 0 || r.Slug == z.Slug) {
					mates = append(mates, r)
				}
			}
			slices.SortStableFunc(mates, func(a, b storeRecord) int {
				switch {
				case a.Rating > b.Rating:
					return -1
				case a.Rating < b.Rating:
					return 1
				}
				return 0
			})
			want := slices.Contains([]string{mates[0].Slug, mates[1].Slug}, z.Slug)
			v0, _ := m.versionOf(page)
			m.apply(editEffect(z.Slug, nil, intp(500)))
			v1, _ := m.versionOf(page)
			if got := slices.Contains(m.shown(page)["bundle"], z.Slug); got != want {
				t.Errorf("%s in the bundle of %s after its restock: %v, want %v", z.Slug, p.Slug, got, want)
			}
			if want && v1 == v0 {
				t.Errorf("%s joined the bundle of %s and the page did not change", z.Slug, p.Slug)
			}
			return
		}
	}
	t.Skip("no product is out of stock")
}

// An edit that sets the stock of a product to none takes it out of the bundles it was in, and the pages
// that offered it change. Its stock set again brings it back.
func TestAnEditToNoStockTakesAProductOutOfTheBundles(t *testing.T) {
	m := modelOf(t)
	page := "/store/products/oak-tray"
	before := m.shown(page)["bundle"]
	if len(before) != 2 {
		t.Fatalf("the bundle of oak-tray is %v, want two products", before)
	}
	v0, _ := m.versionOf(page)
	m.apply(editEffect(before[0], nil, intp(0)))
	v1, _ := m.versionOf(page)
	if after := m.shown(page)["bundle"]; slices.Contains(after, before[0]) || len(after) != 2 || v1 == v0 {
		t.Errorf("with %s sold out the bundle is %v (was %v), page changed %v", before[0], after, before, v1 != v0)
	}
	m.apply(editEffect(before[0], nil, intp(40)))
	v2, _ := m.versionOf(page)
	if after := m.shown(page)["bundle"]; !slices.Equal(after, before) || v2 == v1 {
		t.Errorf("with %s in stock again the bundle is %v, want %v, page changed %v", before[0], after, before, v2 != v1)
	}
}

// The best rated products below every list are the cheaper first among those of one rating, so a price
// edit can bring a product among them, and then its stock is on every list.
func TestAPriceEditCanBringAProductAmongTheBestRatedBelowTheLists(t *testing.T) {
	m := modelOf(t)
	page := "/store/products?sort=newest" // no product of its own in handDeps
	picks := m.shown(page)["picks"]
	if len(picks) != 6 {
		t.Fatalf("the best rated are %v, want 6", picks)
	}
	// A product of the rating of the last pick that is not one, for being dearer.
	last := recordOf(picks[5])
	var next string
	for _, r := range storeCatalogue().records {
		if r.Rating == last.Rating && !slices.Contains(picks, r.Slug) {
			next = r.Slug
			break
		}
	}
	if next == "" {
		t.Skip("no other product has the rating of the last of the best rated")
	}
	v0, _ := m.versionOf(page)
	m.apply(editEffect(next, nil, intp(300)))
	if v, _ := m.versionOf(page); v != v0 {
		t.Errorf("the stock of %s, which is not among the best rated, changed the list", next)
	}
	m.apply(editEffect(next, intp(100), nil))
	if now := m.shown(page)["picks"]; now[0] != next {
		t.Fatalf("at 1 yuan %s is not the first of the best rated: %v", next, now)
	}
	v1, _ := m.versionOf(page)
	m.apply(editEffect(next, nil, intp(200)))
	if v2, _ := m.versionOf(page); v2 == v1 {
		t.Errorf("the stock of %s, now among the best rated, did not change the list", next)
	}
}

// A list with a price range has the products whose price in the currency of its language is in it, in
// whole units: yuan in Chinese, and dollars at 7.1 yuan each, rounded up to 10 cents, in English.
func TestAPriceRangeIsInTheCurrencyOfTheLanguage(t *testing.T) {
	for _, c := range []struct {
		fen  int
		lang string
		want int
	}{
		{29_999, "zh-CN", 299}, {30_000, "zh-CN", 300},
		{71_000, "en-US", 100}, {70_999, "en-US", 100}, {70_300, "en-US", 99}, {100, "en-US", 0},
	} {
		if got := priceIn(c.fen, c.lang); got != c.want {
			t.Errorf("%d fen in %s is %d, want %d", c.fen, c.lang, got, c.want)
		}
	}
	m := modelOf(t)
	m.d.Query[storeList] = []string{"max", "min", "page", "sort", "stock"}
	zh, en := "/zh-CN/store/products?max=300&min=100", "/store/products?max=300&min=100"
	slug := byPrice()[0]
	m.apply(editEffect(slug, intp(30_000), nil))
	if !slices.Contains(m.shown(zh)["products"], slug) || slices.Contains(m.shown(en)["products"], slug) {
		t.Errorf("at 300 yuan (42.3 dollars) %s is in the Chinese list of 100 to 300: %v, in the English one: %v", slug, slices.Contains(m.shown(zh)["products"], slug), slices.Contains(m.shown(en)["products"], slug))
	}
	m.apply(editEffect(slug, intp(30_100), nil))
	if slices.Contains(m.shown(zh)["products"], slug) {
		t.Errorf("at 301 yuan %s is in the list of 100 to 300 yuan", slug)
	}
}

// A list of what is in stock keeps its order when it is not sorted by price, and a product that is sold
// out in the catalogue and that the restock gives a stock is on it, at its place.
func TestAListOfWhatIsInStockHasWhatTheRestockGaveAStock(t *testing.T) {
	var out storeRecord
	for _, r := range storeCatalogue().records {
		if r.Stock == 0 {
			out = r
			break
		}
	}
	if out.Slug == "" {
		t.Skip("no product of the catalogue is sold out")
	}
	all := "/store/products?category=" + out.Category + "&sort=rating"
	// on is every product of the list at url, over all its pages.
	on := func(m *versionModel, url string) []string {
		var list []string
		for page := 1; page <= ModelList(url, m.price, m.inStock).Pages; page++ {
			list = append(list, m.shown(url + "&page=" + strconv.Itoa(page))["products"]...)
		}
		return list
	}
	m := newVersionModel(handDeps(), startStock{})
	whole := on(m, all)
	if !slices.Contains(whole, out.Slug) {
		t.Fatalf("%s is not in the list of its category: %v", out.Slug, whole)
	}
	if got := on(m, all+"&stock=1"); slices.Contains(got, out.Slug) || len(got) >= len(whole) {
		t.Errorf("%s is sold out and the list of what is in stock is %v", out.Slug, got)
	}
	m = newVersionModel(handDeps(), startStock{restocked: map[string]int{out.Slug: 10}})
	got := on(m, all+"&stock=1")
	var want []string
	for _, slug := range whole {
		if m.inStock[slug] {
			want = append(want, slug)
		}
	}
	if !slices.Contains(got, out.Slug) || !slices.Equal(got, want) {
		t.Errorf("after a restock of %s the list of what is in stock is %v, want %v, the list by rating without what is sold out", out.Slug, got, want)
	}
}

// Any URL of the product list has its version from the model, with no entry in the file: a search, a
// search for a word no product has, filters, a sort, a page. A list depends on the stock of the products
// it shows and of the best rated below it, so an order of a product that is on neither leaves it as it
// was, and one of a product it shows changes it. A search that finds nothing shows the best rated
// alone. A price changes a list that shows the product, and a list that comes to show another count of
// a price band or another dearest price: the list goes by what it shows, not by every price.
func TestAnyListHasItsVersionFromTheModel(t *testing.T) {
	m := newVersionModel(handDeps(), startStock{})
	found := "/store/products?q=cup&sort=newest"
	none := "/zh-CN/store/products?q=" + uniqueTerm(1, 7)
	filtered := "/store/products?category=textiles&color=oat&material=fabric&rating=44&page=2"
	lists := []string{found, none, filtered, "/store/products?min=30&max=59&stock=1", "/zh-CN/store/products?badge=sale&sort=price-asc"}
	at := func(page string) pageVersion {
		t.Helper()
		v, err := m.versionOf(page)
		if err != nil {
			t.Fatalf("%s: %v", page, err)
		}
		return v
	}
	for _, u := range lists {
		keys, ok := m.d.pageKeys(u)
		if !ok {
			t.Fatalf("no version keys for %s", u)
		}
		if len(keys) != len(m.d.Common)*2 {
			t.Fatalf("%s has %d version keys that no write decides, want those of what every page shows", u, len(keys))
		}
		if got, want := m.shown(u)["products"], ModelList(u, m.price, m.inStock).Products; !slices.Equal(got, want) {
			t.Errorf("%s shows %v, and the list model works out %v", u, got, want)
		}
		at(u)
	}
	shown := m.shown(found)["products"]
	if len(shown) < 5 || len(m.shown(none)["products"]) != 0 || len(m.shown(none)["picks"]) != listPicks {
		t.Fatalf("a search for cup shows %v, and one for a word of a visit %v with the best rated %v", shown, m.shown(none)["products"], m.shown(none)["picks"])
	}
	// A product the search shows that is not among the best rated, and one that is on neither.
	var on, off string
	for _, r := range m.recs {
		switch picked := slices.Contains(m.picked(), r.Slug); {
		case on == "" && !picked && slices.Contains(shown, r.Slug):
			on = r.Slug
		case off == "" && !picked && !slices.Contains(shown, r.Slug):
			off = r.Slug
		}
	}
	order := func(slug string) {
		m.apply(effect{Kind: WriteCheckout, Keys: []string{stockKey(slug), adminKey(storefrontShop)}})
	}
	v, empty := at(found), at(none)
	order(off)
	if at(found) != v || at(none) != empty {
		t.Errorf("an order of %s, which neither list shows, changed the search for cup: %v, the search that finds nothing: %v", off, at(found) != v, at(none) != empty)
	}
	order(on)
	if at(found) == v || at(none) != empty {
		t.Errorf("an order of %s, which the search for cup shows: it changed that list: %v, and the search that finds nothing: %v", on, at(found) != v, at(none) != empty)
	}
	// An order of one of the best rated changes every list.
	v, empty = at(found), at(none)
	order(m.picked()[0])
	if at(found) == v || at(none) == empty {
		t.Error("an order of one of the best rated did not change every list")
	}
	// A product that neither search shows, that is not on the second page of the whole list and that a
	// higher price leaves below the dearest: its price is on none of the three lists.
	whole := "/store/products?page=2"
	var moved storeRecord
	for _, r := range m.recs {
		if r.PriceFen < 40_000 && !slices.Contains(m.picked(), r.Slug) && !slices.Contains(shown, r.Slug) && !slices.Contains(m.shown(whole)["products"], r.Slug) {
			moved = r
			break
		}
	}
	if moved.Slug == "" {
		t.Fatal("no product under 400 yuan that the lists do not show")
	}
	v, empty, second := at(found), at(none), at(whole)
	// The price it has: every list shows what it showed.
	m.apply(editEffect(moved.Slug, intp(moved.PriceFen), nil))
	if at(found) != v || at(none) != empty || at(whole) != second {
		t.Errorf("the price %s has already changed the search for cup: %v, the search that finds nothing: %v, the whole list: %v", moved.Slug, at(found) != v, at(none) != empty, at(whole) != second)
	}
	// A price in a higher band, in yuan and in dollars: the whole list counts another product in two of
	// its bands. The searches do not count it, since it is not what they find.
	higher := map[bool]int{true: 15_000, false: 30_000}[moved.PriceFen < 10_000]
	if moved.PriceFen >= 20_000 {
		higher = 50_000
	}
	m.apply(editEffect(moved.Slug, intp(higher), nil))
	if at(found) != v || at(none) != empty || at(whole) == second {
		t.Errorf("%s in a higher price band changed the search for cup: %v, the search that finds nothing: %v, the whole list: %v (want false, false, true)", moved.Slug, at(found) != v, at(none) != empty, at(whole) != second)
	}
	// The dearest price of the catalogue is on every list.
	v, empty, second = at(found), at(none), at(whole)
	m.apply(editEffect(moved.Slug, intp(EditPriceFen[1]), nil))
	if at(found) == v || at(none) == empty || at(whole) == second {
		t.Error("a new dearest price did not change every list")
	}
}

// A product that gets a stock and is not among the two best rated of its category is in no bundle, so
// the product pages of its category stay as they are.
func TestAStockThatBringsAProductIntoNoBundleChangesNoProductPage(t *testing.T) {
	m := modelOf(t)
	checked := 0
	for _, z := range storeCatalogue().records {
		if z.Stock != 0 || z.Sized {
			continue
		}
		var pages []string
		stamps := map[string]pageVersion{}
		bundles := map[string][]string{}
		for _, p := range storeCatalogue().records {
			if p.Category == z.Category && p.Slug != z.Slug {
				page := "/store/products/" + p.Slug
				m.d.Pages[page] = []string{}
				pages = append(pages, page)
				stamps[page], _ = m.versionOf(page)
				bundles[page] = m.shown(page)["bundle"]
			}
		}
		m.apply(editEffect(z.Slug, nil, intp(30)))
		for _, page := range pages {
			at, _ := m.versionOf(page)
			same := slices.Equal(bundles[page], m.shown(page)["bundle"])
			if same != (at == stamps[page]) {
				t.Errorf("%s in stock: the bundle of %s is the same: %v, its version is the same: %v", z.Slug, page, same, at == stamps[page])
			}
			if same {
				checked++
			}
		}
	}
	if checked == 0 {
		t.Skip("no product out of stock leaves a bundle as it is")
	}
}

// A checkout says how many pieces it takes of each product of the storefront, and an edit of the stock
// sets a delivery on top of what the restock gave the product.
func TestTheEffectsOfOrdersAndEditsSayThePieces(t *testing.T) {
	eff := versionEffects(1, visitOf(), nil)
	// The demonstration cart (two cups, one of each other product) and the pen the visit added.
	if eff[0].Takes["ceramic-cup"] != 2 || eff[0].Takes["pour-over-kettle"] != 1 || eff[0].Takes["walnut-pen"] < 1 || len(eff[0].Takes) != 5 {
		t.Errorf("the checkout takes %v", eff[0].Takes)
	}
	// An order staff place in the weekend shop takes nothing from the storefront.
	create := versionEffects(1, VisitWrites{Writes: []PlannedWrite{{After: 0, Seq: 2, Kind: WriteCreate, Project: "weekend"}}}, nil)
	if len(create) != 1 || len(create[0].Takes) != 0 {
		t.Errorf("an order of the weekend shop takes %v from the storefront", create[0].Takes)
	}
	for seq := range 200 {
		w := PlannedWrite{After: 1, Seq: seq, Kind: WriteEdit, Project: storefrontShop}
		slug, change := productChange(7, w, func(string) int { return 0 })
		delivery, ok := change["stock"].(int)
		if !ok {
			continue
		}
		eff := versionEffects(7, VisitWrites{Writes: []PlannedWrite{w}}, func(string) int { return 300 })
		if eff[0].Stocks[slug] != 300+delivery {
			t.Fatalf("an edit of the stock of %s restocked to 300 sets %d, want %d", slug, eff[0].Stocks[slug], 300+delivery)
		}
		return
	}
	t.Fatal("200 edits held none of a stock")
}

// A page that has of a product what a card says of its stock changes when that changes: not with a
// sale that leaves more than lowStock pieces, and with every sale from there down. A page that has
// only whether the product is in stock changes when it sells out or comes back, and a page that has
// its price alone never changes with its stock.
func TestAPageFollowsWhatItHasOfTheStock(t *testing.T) {
	d := handDeps()
	d.Pages["/store/journal/kettle-photos"] = []string{"walnut-pen:pl"}
	d.Pages["/store/journal/theme-css"] = []string{"walnut-pen:pf"}
	d.Pages["/store/journal/stock-sync-api"] = []string{"walnut-pen:p"}
	d.Query[storePost] = []string{}
	card, tile, menu, pieces := "/store/journal/kettle-photos", "/store/journal/theme-css", "/store/journal/stock-sync-api", "/store/products/walnut-pen"
	// The pen starts with 40 pieces: the restock gave it 43, and the order of a ship took 3 before the clock.
	m := newVersionModel(d, startStock{restocked: map[string]int{"walnut-pen": 43}, shipped: map[string]int{"walnut-pen": 3}})
	if m.stock["walnut-pen"] != 40 || m.target("walnut-pen") != 43 {
		t.Fatalf("the pen starts with %d pieces and a target of %d", m.stock["walnut-pen"], m.target("walnut-pen"))
	}
	at := func(page string) pageVersion {
		t.Helper()
		v, err := m.versionOf(page)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// changed applies an effect and says which of the four pages it changed.
	changed := func(e effect) string {
		t.Helper()
		before := []pageVersion{at(card), at(tile), at(menu), at(pieces)}
		m.apply(e)
		out := ""
		for i, name := range []string{"card", "tile", "menu", "pieces"} {
			if at([]string{card, tile, menu, pieces}[i]) != before[i] {
				out += name + " "
			}
		}
		return strings.TrimSpace(out)
	}
	take := func(n int) effect {
		return effect{Kind: WriteCheckout, Keys: []string{stockKey("walnut-pen"), adminKey(storefrontShop)}, Takes: map[string]int{"walnut-pen": n}}
	}
	for _, tc := range []struct {
		name string
		e    effect
		left int
		want string
	}{
		{"a sale of 2 of 40", take(2), 38, "pieces"},
		{"a sale down to 11", take(27), 11, "pieces"},
		{"a sale down to 10", take(1), 10, "card pieces"},
		{"a sale down to 9", take(1), 9, "card pieces"},
		{"the last 9 sold", take(9), 0, "card tile pieces"},
		{"a delivery of 5", editEffect("walnut-pen", nil, intp(5)), 5, "card tile pieces"},
		{"a delivery up to 60", editEffect("walnut-pen", nil, intp(60)), 60, "card pieces"},
		{"a delivery up to 90", editEffect("walnut-pen", nil, intp(90)), 90, "pieces"},
		{"a price", editEffect("walnut-pen", intp(12_300), nil), 90, "card tile menu pieces"},
	} {
		if got := changed(tc.e); got != tc.want || m.stock["walnut-pen"] != tc.left {
			t.Errorf("%s changes the pages %q and leaves %d pieces, want %q and %d", tc.name, got, m.stock["walnut-pen"], tc.want, tc.left)
		}
	}
}

// The parts of a page whose products a price or a stock decides have the fields the file says: with a
// list whose cards hold what a card says of the stock, an order of a product in good supply that the
// list shows leaves the list as it was.
func TestTheProductsOfAListHaveTheFieldsTheFileSays(t *testing.T) {
	page := "/store/products?q=cup&sort=newest"
	for _, tc := range []struct {
		fields string
		want   bool
	}{{"", true}, {depPrice + depLow, false}} {
		d := handDeps()
		if tc.fields != "" {
			d.Decided = map[string]string{"products": tc.fields, "picks": tc.fields}
		}
		m := newVersionModel(d, startStock{restocked: map[string]int{"ceramic-cup": 500}})
		if !slices.Contains(m.shown(page)["products"], "ceramic-cup") {
			t.Fatalf("the search for cup does not show the cup: %v", m.shown(page)["products"])
		}
		before, _ := m.versionOf(page)
		m.apply(effect{Kind: WriteCheckout, Keys: []string{stockKey("ceramic-cup"), adminKey(storefrontShop)}, Takes: map[string]int{"ceramic-cup": 2}})
		if after, _ := m.versionOf(page); (after != before) != tc.want {
			t.Errorf("with the fields %q of the products of a list, an order of 2 of 500 cups changed the list: %v, want %v", tc.fields, after != before, tc.want)
		}
	}
}
