package compare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The table of server/renderurl_test.go: renderedURL does what renderURL does.
func TestTheRenderURLKeepsTheParametersTheLoadsRead(t *testing.T) {
	for _, tc := range []struct {
		target string
		keep   []string
		want   string
	}{
		{"/news?utm_source=a&page=2", []string{"page"}, "/news?page=2"},
		{"/news?utm_source=a&page=2", nil, "/news"},
		{"/news", []string{"page"}, "/news"},
		{"/news?utm_source=a", []string{"page"}, "/news"},
		{"/s?b=2&x=1&a=1&b=3", []string{"a", "b"}, "/s?a=1&b=2&b=3"},
		{"/s?q=b&x=2&q=c", []string{"q"}, "/s?q=b&q=c"},
		{"/s?q=a+b%26c&q=%2F", []string{"q"}, "/s?q=a+b%26c&q=%2F"},
		{"/s?q=&r", []string{"q", "r"}, "/s?q=&r="},
		{"/s?q=%zz&q=1&r;s=1", []string{"q", "r"}, "/s?q=1"},
		{"/s?", []string{""}, "/s"},
		{"/s?&", []string{""}, "/s"},
		{"/s?&&", []string{"", "q"}, "/s"},
		{"/s?&q=1&&q=2&", []string{"q"}, "/s?q=1&q=2"},
		{"/s?=x&", []string{""}, "/s?=x"},
		{"/s?=x&&=y&=", []string{""}, "/s?=x&=y&="},
		{"/%6Eews?page=2", []string{"page"}, "/%6Eews?page=2"},
	} {
		if got := renderedURL(tc.target, tc.keep); got != tc.want {
			t.Errorf("renderedURL(%q, %q) = %q, want %q", tc.target, tc.keep, got, tc.want)
		}
	}
}

// A landing parameter is no part of what a render sees, and neither is the order of the parameters.
func TestTheRenderURLOfALandingHasNoClickID(t *testing.T) {
	d := &StoreDeps{Query: map[string][]string{storeList: {"category", "page", "sort"}, storeHome: nil, storeProduct: {"reviews"}}}
	for page, want := range map[string]string{
		"/store?gclid=" + landingValues[0].Placeholder:                                       "/store",
		"/store/products?sort=newest&category=drinkware&_kx=" + landingValues[3].Placeholder: "/store/products?category=drinkware&sort=newest",
		"/zh-CN/store/products/oak-tray?reviews=2&fbclid=x":                                  "/zh-CN/store/products/oak-tray?reviews=2",
		"/store/products/oak-tray":                                                           "/store/products/oak-tray",
	} {
		if got := d.RenderURL(stripLanding(page)); got != want {
			t.Errorf("the render of %s sees %q, want %q", page, got, want)
		}
	}
}

const appSource = `package tifaapp

var App = server.App{
	Layouts: []server.Layout{
		{ID: "app/layout", Path: "/", SSR: true, Query: []string{"z"}},
		{ID: "app/store/layout", Path: "/store", SSR: true},
	},
	Routes: []server.Route{
		{ID: "app/store/route", Path: "/store", Page: true, Layouts: []string{"app/store/layout"}},
		{ID: "app/store/products/route", Path: "/store/products", Page: true, Layouts: []string{"app/layout", "app/store/layout"}, Query: []string{"sort", "page"}},
	},
}
`

func TestTheQueryOfARouteIsWhatItsLoadsAndLayoutsRead(t *testing.T) {
	got, err := routeQueries([]byte(appSource))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"app/store/route": {}, "app/store/products/route": {"page", "sort", "z"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("queries %v, want %v", got, want)
	}
}

// The table of the file is the one the build of the shop has, when the shop is built.
func TestTheQueryTableOfTheFileIsTheOneOfTheBuild(t *testing.T) {
	e, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(e.Shop, ".tifa", "tifaapp", "app.go"))
	if err != nil {
		t.Skip("the shop is not built: " + err.Error())
	}
	all, err := routeQueries(src)
	if err != nil {
		t.Fatal(err)
	}
	d, err := storeDepsOnce()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range usedRoutes() {
		if !slices.Equal(d.Query[r], all[r]) {
			t.Errorf("%s: store_deps.json reads %v, the build of the shop %v: make it again with `compare deps`", r, d.Query[r], all[r])
		}
	}
	if len(d.Query) != len(usedRoutes()) {
		t.Errorf("the file has the query of %d routes, not %d", len(d.Query), len(usedRoutes()))
	}
}

// fakeStorefront answers the data of three pages: each shows the products it has: a card has a slug, a price and a stock.
func fakeStorefront(t *testing.T) *httptest.Server {
	card := func(slug string, price, stock bool) map[string]any {
		m := map[string]any{"slug": slug, "name": slug}
		if price {
			m["priceCents"] = 100.0
		}
		if stock {
			m["stock"] = 5.0
		}
		return m
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Query().Get("_tifa"), ".11") || r.Header.Get("Sec-Fetch-Site") != "same-origin" {
			http.Error(w, "not a data request", 400)
			return
		}
		layout := map[string]any{"menu": []any{card("ceramic-cup", true, true)}}
		var page any
		switch r.URL.Path {
		case "/store":
			page = map[string]any{
				"featured": []any{card("oak-tray", true, true), card("wool-throw", true, false)},
				"sale":     []any{card("glass-carafe", true, true)},
			}
		case "/store/products/oak-tray":
			page = map[string]any{
				"product": card("oak-tray", true, true), "related": []any{card("wool-throw", false, true)},
				"bundle": []any{card("tea-canister", true, true)},
			}
		case "/store/products":
			// A list of three products in pages of two, the dearest first when it sorts so.
			all := []string{"oak-tray", "dot-notebook", "walnut-pen"}
			if r.URL.Query().Get("sort") == "price-desc" {
				all = []string{"walnut-pen", "oak-tray", "dot-notebook"}
			}
			from := 0
			if r.URL.Query().Get("page") == "2" {
				from = 2
			}
			var cards []any
			for _, slug := range all[from:min(from+2, len(all))] {
				cards = append(cards, card(slug, true, true))
			}
			page = map[string]any{"products": cards, "pages": 2.0, "picks": []any{card("sketchbook", true, true)}}
		default:
			http.Error(w, "no page", 404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"routes": map[string]any{
			"app/store/layout":  map[string]any{"status": 200, "data": layout},
			routeOf(r.URL.Path): map[string]any{"status": 200, "data": page},
		}})
	}))
}

func TestTheDepsOfAPageAreTheProductsItsDataShows(t *testing.T) {
	srv := fakeStorefront(t)
	defer srv.Close()
	query := map[string][]string{storeHome: nil, storeProduct: {"reviews"}, storeList: {"category", "sort"}}
	urls := []string{"/store", "/store/products/oak-tray?reviews=2", "/store/products/oak-tray"}
	d, err := collectStoreDeps(context.Background(), srv.URL, "b1", query, urls)
	if err != nil {
		t.Fatal(err)
	}
	// The layout shows ceramic-cup on every page and every page shows oak-tray, so they are the common part.
	if want := []string{"ceramic-cup", "oak-tray"}; !slices.Equal(d.Common, want) {
		t.Errorf("common %v, want %v", d.Common, want)
	}
	// A product shown with a price alone or a stock alone has one dependency, and two pages that the
	// render sees alike have one entry.
	want := map[string][]string{
		"/store":                             {"wool-throw:p"},
		"/store/products/oak-tray?reviews=2": {"wool-throw:s"},
		"/store/products/oak-tray":           {"wool-throw:s"},
	}
	if !reflect.DeepEqual(d.Pages, want) {
		t.Errorf("pages %v, want %v", d.Pages, want)
	}
}

// The parts of a page whose products a price or a stock decides are not in the file: the sale of the
// home and the bundle of a product page. The model works them out (versionModel). No page of the
// product list is in the file at all: the list model works out what any URL of the list shows, and
// making the file for one is refused.
func TestTheFileLeavesOutThePartsAPriceOrAStockDecides(t *testing.T) {
	srv := fakeStorefront(t)
	defer srv.Close()
	query := map[string][]string{storeHome: nil, storeProduct: {"reviews"}, storeList: {"page", "sort", "stock"}}
	urls := []string{"/store", "/store/products/oak-tray"}
	d, err := collectStoreDeps(context.Background(), srv.URL, "b1", query, urls)
	if err != nil {
		t.Fatal(err)
	}
	for _, list := range []string{"/store/products", "/store/products?sort=price-desc", "/zh-CN/store/products?q=cup"} {
		if _, err := collectStoreDeps(context.Background(), srv.URL, "b1", query, append(slices.Clone(urls), list)); err == nil || !strings.Contains(err.Error(), "list model") {
			t.Errorf("a file made with %s: %v, want a refusal that names the list model", list, err)
		}
	}
	for page, deps := range d.Pages {
		for _, dep := range slices.Concat(d.Common, deps) {
			slug, _, _ := strings.Cut(dep, ":")
			if slices.Contains([]string{"glass-carafe", "tea-canister", "sketchbook", "walnut-pen", "dot-notebook"}, slug) {
				t.Errorf("%s depends on %s, which is on it by a price or a stock", page, dep)
			}
		}
	}
	want := map[string][]string{
		"/store":                   {"wool-throw:p"},
		"/store/products/oak-tray": {"wool-throw:s"},
	}
	if !reflect.DeepEqual(d.Pages, want) {
		t.Errorf("pages %v, want %v", d.Pages, want)
	}
}

// A list page shows what the Layout shows on every page, with no entry of the file. What else it
// shows the model works out as the writes come (decidedParts, listShown).
func TestAListHasTheKeysOfWhatEveryPageShows(t *testing.T) {
	d := &StoreDeps{Common: []string{"ceramic-cup", "spice-tin:p", "wool-throw:pl", "oak-tray:pf"}, Pages: map[string][]string{}}
	want := []string{priceKey("ceramic-cup"), stockKey("ceramic-cup"), priceKey("spice-tin"), priceKey("wool-throw"), lowKey("wool-throw"), priceKey("oak-tray"), flagKey("oak-tray")}
	for _, list := range []string{"/store/products?sort=price-desc", "/zh-CN/store/products?q=no-such-word", "/store/products"} {
		if keys, ok := d.pageKeys(list); !ok || !slices.Equal(keys, want) {
			t.Errorf("%s has the keys %v, want %v", list, keys, want)
		}
	}
}

// A dependency names the most a page has of the stock of a product, and what every page has of a
// product is what all of them share.
func TestTheFieldsOfADependency(t *testing.T) {
	for _, tc := range []struct{ seen, want string }{
		{"p", "slug:p"}, {"psf", "slug"}, {"fsp", "slug"}, {"plf", "slug:pl"}, {"pf", "slug:pf"}, {"lf", "slug:l"}, {"f", "slug:f"}, {"pp", "slug:p"}, {"", ""},
	} {
		if got := depOf("slug", tc.seen); got != tc.want {
			t.Errorf("the fields %q are written %q, want %q", tc.seen, got, tc.want)
		}
		if tc.want == "" {
			continue
		}
		if slug, fields := fieldsOf(tc.want); slug != "slug" || fields != canonFields(tc.seen) {
			t.Errorf("%q reads as %s with %q", tc.want, slug, fields)
		}
	}
	for _, tc := range []struct{ a, b, want string }{
		{"p", "pl", "p"}, {"pl", "pl", "pl"}, {"ps", "pl", "pl"}, {"pl", "pf", "pf"}, {"s", "p", ""}, {"p", "", ""},
	} {
		if got := sharedFields(tc.a, tc.b); got != tc.want {
			t.Errorf("%q and %q share %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

// productsIn reads what the data of a page has of each product from the names of its fields.
func TestProductsInReadsTheFieldsOfAProduct(t *testing.T) {
	var data any
	if err := json.Unmarshal([]byte(`{"menu":[{"slug":"a","priceCents":1}],"cards":[{"slug":"b","priceCents":1,"inStock":true,"lowStock":0},{"slug":"a","name":"x"}],
		"tiles":[{"slug":"c","priceCents":1,"inStock":false}],"old":{"slug":"d","priceCents":1,"inStock":true,"stock":7},"reviews":[{"product":{"slug":"e","name":"y"}}]}`), &data); err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	productsIn(data, found)
	if got, want := depList(found), []string{"a:p", "b:pl", "c:pf", "d"}; !slices.Equal(got, want) {
		t.Errorf("the data shows %v, want %v", got, want)
	}
}

// The file is the one this checkout makes: the sources of the shop, the URLs and every page in it. It
// has no page of the product list, and the lists a visit can land on and those of the crawlers have
// their version keys all the same.
func TestTheFileOfTheDepsIsTheOneOfThisCheckout(t *testing.T) {
	d := checkedDeps(t)
	for _, u := range depsURLs() {
		if keys, ok := d.pageKeys(u); !ok || len(keys) == 0 {
			t.Errorf("no version keys for %s", u)
		}
	}
	for key := range d.Pages {
		if _, kind := StorePageKind(key); kind == StoreKindList {
			t.Errorf("the file has an entry for the list %s", key)
		}
	}
	lists := 0
	public, _ := CrawlURLs()
	m := newVersionModel(d, startStock{})
	for _, u := range slices.Concat(StoreVisitPages(), public) {
		if _, kind := StorePageKind(u); kind == StoreKindList {
			lists++
			if _, err := m.versionOf(u); err != nil {
				t.Errorf("%s: %v", u, err)
			}
		}
	}
	if lists < 60 {
		t.Errorf("%d lists among the pages of the visits and of the crawlers: too few", lists)
	}
}

// A file that was made from other sources of the shop, or for other URLs, is refused.
func TestAFileOfTheDepsMadeFromOtherSourcesIsRefused(t *testing.T) {
	checkedDeps(t)
	e, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	old := storeDepsJSON
	defer func() { storeDepsJSON = old }()
	d, err := parseStoreDeps(old)
	if err != nil {
		t.Fatal(err)
	}
	for what, change := range map[string]func(*StoreDeps){
		"sources": func(d *StoreDeps) { d.Sources = "0000000000000000" },
		"urls":    func(d *StoreDeps) { d.URLs = "0000000000000000" },
		"a page":  func(d *StoreDeps) { delete(d.Pages, d.RenderURL("/store")) },
		"a list":  func(d *StoreDeps) { d.Pages["/store/products?sort=newest"] = []string{"ceramic-cup:p"} },
	} {
		c := *d
		c.Pages = map[string][]string{}
		for k, v := range d.Pages {
			c.Pages[k] = v
		}
		change(&c)
		data, err := marshalStoreDeps(&c)
		if err != nil {
			t.Fatal(err)
		}
		storeDepsJSON = data
		if _, err := CheckStoreDeps(e.Root); err == nil || !strings.Contains(err.Error(), "compare deps") {
			t.Errorf("a file with other %s: %v, want a refusal that says how to make it again", what, err)
		}
	}
}

func TestTheFileOfTheDepsHoldsTheProductsAPageShows(t *testing.T) {
	d, err := storeDepsOnce()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Common) == 0 {
		t.Error("no product is on every page, but the Layout of the storefront shows some")
	}
}
