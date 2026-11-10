package compare

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// The storefront of examples/shop, at /store and, in Chinese, at /zh-CN/store. Its data is computed in Node by
// domain/store, a port of the Go code that TestStoreDomainMatchesGo keeps equal to the Go code.

// The route ids of the storefront's Layout and Load functions, as tifa names them.
const (
	storeLayout   = "app/store/layout"
	storeHome     = "app/store/route"
	storeList     = "app/store/products/route"
	storeProduct  = "app/store/products/product+/route"
	storeCart     = "app/store/cart/route"
	storeCheckout = "app/store/checkout/route"
	storeJournal  = "app/store/journal/route"
	storePost     = "app/store/journal/post+/route"
	storeMine     = "app/store/mine/route"
)

// StoreLangs are the two languages of the storefront.
var StoreLangs = []string{"zh-CN", "en-US"}

// StoreBase is where the storefront starts in a language: /store for the default language, en-US,
// and /zh-CN/store for the other.
func StoreBase(l string) string {
	if l == "en-US" {
		return "/store"
	}
	return "/" + l + "/store"
}

// ShopCatalogue is the catalogue file of the shop, from the root of the repository: the products the
// shop starts with, which the plans (StoreProducts, StoreVariants) and the TypeScript port in domain/
// read too.
const ShopCatalogue = "examples/shop/app/store/products/catalogue.json"

// StoreProducts returns the slug of every product of the catalogue, in its order (ShopCatalogue).
func StoreProducts() []string { return storeCatalogue().slugs }

// storeCatalogue reads the catalogue file of the shop once. The root module cannot embed a file of
// the shop's module, so it reads it where the repository is. A file it cannot read is a bug of the
// checkout, and it panics.
var storeCatalogue = sync.OnceValue(func() (c struct {
	slugs    []string
	variants []StoreProduct
	// records are what the set of URLs of a crawler (crawl.go), the data versions and the model of the
	// product list need to know of each product.
	records []storeRecord
}) {
	e, err := NewEnv()
	if err != nil {
		panic(err)
	}
	data, err := os.ReadFile(filepath.Join(e.Root, ShopCatalogue))
	if err != nil {
		panic(err)
	}
	var file []struct {
		Slug       string      `json:"slug"`
		Name       [2]string   `json:"name"`
		Category   string      `json:"category"`
		PriceFen   int         `json:"priceFen"`
		CompareFen int         `json:"compareFen"`
		Rating     float64     `json:"rating"`
		Stock      int         `json:"stock"`
		Badge      string      `json:"badge"`
		Colors     []string    `json:"colors"`
		Sizes      []string    `json:"sizes"`
		Tags       []string    `json:"tags"`
		Materials  [][2]string `json:"materials"`
		Added      time.Time   `json:"added"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		panic(ShopCatalogue + ": " + err.Error())
	}
	for _, p := range file {
		c.slugs = append(c.slugs, p.Slug)
		c.variants = append(c.variants, StoreProduct{p.Slug, p.Stock, p.Colors, p.Sizes})
		c.records = append(c.records, storeRecord{
			Slug: p.Slug, Category: p.Category, Rating: p.Rating, PriceFen: p.PriceFen, CompareFen: p.CompareFen, Stock: p.Stock, Sized: len(p.Sizes) > 0,
			Name: p.Name, nameLower: [2]string{strings.ToLower(p.Name[0]), strings.ToLower(p.Name[1])}, Tags: p.Tags, Colors: p.Colors, Badge: p.Badge,
			Materials: p.Materials, Families: materialFamiliesOf(p.Materials), Added: p.Added,
		})
	}
	return c
})

// storeRecord is a product of the catalogue file as the URLs of a crawler, the data versions of the
// pages and the model of the product list need it: its slug, its category, its rating (which gives how
// many reviews it has), its price and sale price in fen, its stock and whether it comes in sizes, and
// what the list searches and filters by (store_list.go): its name in Chinese and in English, its tags,
// its colours, its badge, the families of its materials and when it was added. The search words are
// made from its names, its tags and the names of its materials in Chinese and in English
// (store_terms.go).
type storeRecord struct {
	Slug, Category string
	Rating         float64
	PriceFen       int
	CompareFen     int
	Stock          int
	Sized          bool
	Name           [2]string
	nameLower      [2]string // the names in lower case, as the search compares them
	Tags, Colors   []string
	Badge          string
	Materials      [][2]string
	Families       []string
	Added          time.Time
}

// StoreSampleProducts are the products a check of the pages shows, one of each kind: with sizes,
// on sale, new, limited, sold out, and plain.
var StoreSampleProducts = []string{
	"pour-over-kettle", "oak-tray", "tea-canister", "glass-carafe", "walnut-pen", "linen-apron", "stoneware-vase", "camp-lantern",
}

// StorePosts are the posts of the journal.
var StorePosts = []string{"stock-sync-api", "theme-css", "kettle-photos"}

// StoreListQueries are query strings of the product list that filter, sort and page it.
var StoreListQueries = []string{
	"",
	"category=drinkware",
	"q=cup",
	"q=%E6%9D%AF",
	"color=moss&color=mist&sort=price-asc",
	"material=wood&material=metal&badge=sale&badge=new&rating=46",
	"min=100&max=300&stock=1&sort=price-desc",
	"sort=newest",
	"sort=rating&category=textiles",
	"page=2",
	"page=99&sort=newest",
	"q=nothing-matches-this",
}

// StorePages are the storefront pages the apps serve, in both languages: the home, the list with
// filters, the sample products, the cart and the checkout of a visitor without a cart, the
// journal and its posts. Routes are the ids of the Layouts and the page, as for Pages.
func StorePages() []Page {
	var out []Page
	for _, l := range StoreLangs {
		base := StoreBase(l)
		add := func(name, path, page string) {
			out = append(out, Page{Name: "store " + l + " " + name, URL: base + path, Routes: storeLayout + "," + page})
		}
		add("home", "", storeHome)
		for _, q := range StoreListQueries {
			if q == "" {
				add("products", "/products", storeList)
			} else {
				add("products?"+q, "/products?"+q, storeList)
			}
		}
		for _, slug := range StoreSampleProducts {
			add("product "+slug, "/products/"+slug, storeProduct)
		}
		add("product reviews page 2", "/products/pour-over-kettle?reviews=2", storeProduct)
		add("cart", "/cart", storeCart)
		add("checkout", "/checkout", storeCheckout)
		add("journal", "/journal", storeJournal)
		for _, post := range StorePosts {
			add("post "+post, "/journal/"+post, storePost)
		}
	}
	return out
}

// StoreMissing are pages of the storefront that answer 404: a product and a post that do not exist. A data request answers 200 with the status of each route in it.
var StoreMissing = []Page{
	{Name: "product", URL: "/zh-CN/store/products/no-such-product", Routes: storeLayout + "," + storeProduct},
	{Name: "post", URL: "/store/journal/no-such-post", Routes: storeLayout + "," + storePost},
}

// StoreStep is one request of a storefront scenario. The same steps run against the shop and against
// the port in domain/store/dump.ts, each with its own cookies, and the answers are compared.
type StoreStep struct {
	Name   string `json:"name,omitempty"`
	Method string `json:"method"`
	// URL is a path with its query. {order} in it stands for the number of the order the last 303
	// of the scenario sent the visitor to.
	URL string `json:"url"`
	// Routes, for a GET of a page, are the route ids of its Layouts and page, comma separated. The
	// step then asks for the data of those routes, as the browser router does.
	Routes string `json:"routes,omitempty"`
	// Body is sent as JSON, or as ContentType when that is set.
	Body        string `json:"body,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	// Cookie, when set, is the whole Cookie header, in place of the cookies the scenario holds.
	Cookie string `json:"cookie,omitempty"`
}

// StoreScenario is a visitor: steps that run in order with one set of cookies.
type StoreScenario struct {
	Name  string      `json:"name"`
	Steps []StoreStep `json:"steps"`
}

// StoreResult is what a step answered.
type StoreResult struct {
	Status int `json:"status"`
	// At is the time the request was sent, in milliseconds since the epoch.
	At         int64    `json:"at"`
	Body       any      `json:"body,omitempty"`
	Location   string   `json:"location,omitempty"`
	SetCookies []string `json:"setCookies,omitempty"`
}

// StoreState is a cart state worth looking at: the writes that make it, then the pages to read
// with the cookies those writes set. A check of the HTML runs the Steps against the server with
// one cookie jar, then fetches each page.
type StoreState struct {
	Name  string
	Steps []StoreStep
	Pages []Page
}

func (s StoreStep) String() string { return s.Method + " " + s.URL }

// storeOrder is an order the checkout takes, in the language l.
func storeOrder(l string, mutate ...func(m map[string]any)) string {
	address := map[string]any{"name": "林夏", "phone": "138 0013 8000", "country": "CN", "region": "上海市", "city": "上海市", "line1": "徐汇区武康路 99 号", "line2": "2 号楼 301", "postalCode": "200031"}
	email := "linxia@example.com"
	if l == "en-US" {
		address = map[string]any{"name": "Emma Walker", "phone": "+1 415 555 0134", "country": "US", "region": "CA", "city": "San Francisco", "line1": "1 Market Street", "line2": "", "postalCode": "94105"}
		email = "emma@example.com"
	}
	m := map[string]any{
		"email": email, "address": address, "delivery": "standard", "payment": "card", "cardNumber": "4242 4242 4242 4242",
		"cardExpiry": "12/30", "cardCvc": "123", "billingSame": true, "notes": "", "newsletter": false, "terms": true,
	}
	for _, f := range mutate {
		f(m)
	}
	return string(mustJSON(m))
}

func storeGET(l, path, page string) StoreStep {
	return StoreStep{Method: "GET", URL: StoreBase(l) + path, Routes: storeLayout + "," + page}
}

func storeWrite(method, l, path string, body any) StoreStep {
	s := StoreStep{Method: method, URL: StoreBase(l) + path}
	switch b := body.(type) {
	case nil:
	case string:
		s.Body = b
	default:
		s.Body = string(mustJSON(b))
	}
	return s
}

func storeMineGET(l string) StoreStep { return StoreStep{Method: "GET", URL: StoreBase(l) + "/mine"} }

// StoreAddToCart is the POST that puts a product in the cart.
func StoreAddToCart(l, slug, color, size string, quantity int) StoreStep {
	return storeWrite("POST", l, "/products/"+slug, map[string]any{"color": color, "size": size, "quantity": quantity})
}

// StoreScenarios are the visitors the data of the port is checked with: the pages without any
// state, and carts that are filled, changed, checked out and refused, in both languages.
func StoreScenarios() []StoreScenario {
	var out []StoreScenario
	for _, l := range StoreLangs {
		// The pages with no cookies.
		var pages []StoreStep
		for _, p := range StorePages() {
			if strings.HasPrefix(p.URL, StoreBase(l)) {
				pages = append(pages, StoreStep{Name: p.Name, Method: "GET", URL: p.URL, Routes: p.Routes})
			}
		}
		for _, slug := range StoreProducts() {
			pages = append(pages, storeGET(l, "/products/"+slug, storeProduct))
		}
		for _, n := range []string{"1", "2", "3", "99", "0", "-4", "x"} {
			pages = append(pages, storeGET(l, "/products/oak-tray?reviews="+n, storeProduct))
		}
		pages = append(pages,
			storeGET(l, "/products?color=moss&color=mist&page=2&sort=price-asc&min=x&max=-3&rating=zz", storeList),
			storeGET(l, "/products?q=%20Cup%20&category=nope", storeList),
			storeGET(l, "/products?sort=nope&stock=0&page=-1", storeList),
			storeGET(l, "/checkout?placed=SS-NOSUCHORDER", storeCheckout),
			storeMineGET(l),
		)
		out = append(out, StoreScenario{Name: "pages " + l, Steps: pages})

		orderOK := storeOrder(l)
		out = append(out,
			StoreScenario{Name: "add to cart " + l, Steps: []StoreStep{
				StoreAddToCart(l, "pour-over-kettle", "mist", "", 2),
				storeGET(l, "/cart", storeCart),
				storeMineGET(l),
				StoreAddToCart(l, "pour-over-kettle", "mist", "", 9),
				StoreAddToCart(l, "oak-tray", "oat", "M", 5),
				StoreAddToCart(l, "oak-tray", "oat", "M", 5),
				StoreAddToCart(l, "oak-tray", "oat", "L", 1),
				StoreAddToCart(l, "wax-cloth", "moss", "", 1),
				storeGET(l, "/cart", storeCart),
				storeGET(l, "/checkout", storeCheckout),
				storeMineGET(l),
			}},
			StoreScenario{Name: "add refused " + l, Steps: []StoreStep{
				StoreAddToCart(l, "no-such-product", "mist", "", 1),
				StoreAddToCart(l, "ceramic-cup", "ink", "", 1),
				StoreAddToCart(l, "oak-tray", "oat", "", 1),
				StoreAddToCart(l, "oak-tray", "oat", "XL", 1),
				StoreAddToCart(l, "ceramic-cup", "moss", "M", 1),
				StoreAddToCart(l, "ceramic-cup", "moss", "", 0),
				StoreAddToCart(l, "ceramic-cup", "moss", "", 11),
				StoreAddToCart(l, "ceramic-cup", "moss", "", -1),
				StoreAddToCart(l, "tea-canister", "brass", "", 1),
				storeGET(l, "/cart", storeCart),
				storeMineGET(l),
			}},
			StoreScenario{Name: "change quantity " + l, Steps: []StoreStep{
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "ceramic-cup.moss", "quantity": 5}),
				storeGET(l, "/cart", storeCart),
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "linen-apron.oat.M", "quantity": 0}),
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "pour-over-kettle.mist", "quantity": 99}),
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "pour-over-kettle.mist", "quantity": 24}),
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "pour-over-kettle.mist", "quantity": -1}),
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "no-such.line", "quantity": 1}),
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "reading-candle.charcoal", "quantity": 10}),
				storeGET(l, "/cart", storeCart),
			}},
			StoreScenario{Name: "coupons " + l, Steps: []StoreStep{
				storeWrite("POST", l, "/cart", map[string]any{"code": "shanhai10"}),
				storeGET(l, "/cart", storeCart),
				storeGET(l, "/checkout", storeCheckout),
				storeWrite("POST", l, "/cart", map[string]any{"code": " WELCOME30 "}),
				storeGET(l, "/cart", storeCart),
				storeWrite("POST", l, "/cart", map[string]any{"code": "NOPE"}),
				storeGET(l, "/cart", storeCart),
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "ceramic-cup.moss", "quantity": 0}),
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "pour-over-kettle.mist", "quantity": 0}),
				storeGET(l, "/cart", storeCart),
				storeWrite("POST", l, "/cart", map[string]any{"code": ""}),
				storeGET(l, "/cart", storeCart),
			}},
			StoreScenario{Name: "empty cart " + l, Steps: []StoreStep{
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "pour-over-kettle.mist", "quantity": 0}),
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "ceramic-cup.moss", "quantity": 0}),
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "linen-apron.oat.M", "quantity": 0}),
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "reading-candle.charcoal", "quantity": 0}),
				storeGET(l, "/cart", storeCart),
				storeGET(l, "/checkout", storeCheckout),
				storeWrite("POST", l, "/checkout", orderOK),
				storeMineGET(l),
			}},
			StoreScenario{Name: "check out " + l, Steps: []StoreStep{
				storeWrite("POST", l, "/cart", map[string]any{"code": "SHANHAI10"}),
				storeWrite("POST", l, "/checkout", orderOK),
				{Method: "GET", URL: StoreBase(l) + "/checkout?placed={order}", Routes: storeLayout + "," + storeCheckout},
				storeGET(l, "/cart", storeCart),
				storeWrite("POST", l, "/checkout", orderOK),
				{Method: "GET", URL: StoreBase(l) + "/checkout?placed={order}", Routes: storeLayout + "," + storeCheckout, Cookie: "store_cart=SOMEONEELSE"},
				{Method: "GET", URL: StoreBase(l) + "/checkout?placed={order}", Routes: storeLayout + "," + storeCheckout, Cookie: "unrelated=1"},
			}},
			StoreScenario{Name: "check out express " + l, Steps: []StoreStep{
				StoreAddToCart(l, "walnut-pen", "brass", "", 1),
				storeWrite("POST", l, "/checkout", storeOrder(l, func(m map[string]any) { m["delivery"] = "express"; m["payment"] = "alipay"; m["cardNumber"] = "" })),
				{Method: "GET", URL: StoreBase(l) + "/checkout?placed={order}", Routes: storeLayout + "," + storeCheckout},
				StoreAddToCart(l, "walnut-pen", "charcoal", "", 3),
				storeWrite("POST", l, "/checkout", storeOrder(l, func(m map[string]any) { m["delivery"] = "pickup"; m["payment"] = "wechat" })),
				{Method: "GET", URL: StoreBase(l) + "/checkout?placed={order}", Routes: storeLayout + "," + storeCheckout},
			}},
			StoreScenario{Name: "check out refused " + l, Steps: []StoreStep{
				storeWrite("POST", l, "/checkout", `{}`),
				storeWrite("POST", l, "/checkout", storeOrder(l, func(m map[string]any) { m["terms"] = false; m["email"] = "nope" })),
				storeWrite("POST", l, "/checkout", storeOrder(l, func(m map[string]any) {
					m["cardNumber"] = "4242 4242 4242 4241"
					m["cardExpiry"] = "13/30"
					m["cardCvc"] = "12"
				})),
				storeWrite("POST", l, "/checkout", storeOrder(l, func(m map[string]any) { m["payment"] = "cash"; m["delivery"] = "drone" })),
				storeWrite("POST", l, "/checkout", storeOrder(l, func(m map[string]any) {
					a := m["address"].(map[string]any)
					a["name"] = "  "
					a["phone"] = "12"
					a["country"] = "XX"
					a["postalCode"] = "abc"
					a["line1"] = strings.Repeat("长", 201)
					a["line2"] = strings.Repeat("x", 201)
					m["notes"] = strings.Repeat("n", 201)
				})),
				storeWrite("POST", l, "/checkout", storeOrder(l, func(m map[string]any) {
					a := m["address"].(map[string]any)
					a["country"] = "GB"
					a["postalCode"] = ""
				})),
				storeWrite("POST", l, "/checkout", storeOrder(l, func(m map[string]any) {
					a := m["address"].(map[string]any)
					a["country"] = "CA"
					a["postalCode"] = "K1A 0B1"
					m["email"] = strings.Repeat("a", 250) + "@example.com"
				})),
				{Method: "POST", URL: StoreBase(l) + "/checkout", Body: orderOK, Cookie: "store_cart=NOSUCHCART"},
				storeGET(l, "/cart", storeCart),
			}},
			StoreScenario{Name: "seen products " + l, Steps: []StoreStep{
				storeWrite("POST", l, "/mine", map[string]any{"slug": "oak-tray"}),
				storeWrite("POST", l, "/mine", map[string]any{"slug": "ceramic-cup"}),
				storeWrite("POST", l, "/mine", map[string]any{"slug": "oak-tray"}),
				storeWrite("POST", l, "/mine", map[string]any{"slug": "not-a-product"}),
				storeMineGET(l),
				storeGET(l, "/cart", storeCart),
				storeWrite("POST", l, "/mine", map[string]any{"slug": "tea-towels"}),
				storeWrite("POST", l, "/mine", map[string]any{"slug": "wool-throw"}),
				storeWrite("POST", l, "/mine", map[string]any{"slug": "walnut-pen"}),
				storeWrite("POST", l, "/mine", map[string]any{"slug": "desk-tray"}),
				storeWrite("POST", l, "/mine", map[string]any{"slug": "wax-cloth"}),
				storeWrite("POST", l, "/mine", map[string]any{"slug": "copper-cups"}),
				storeWrite("POST", l, "/mine", map[string]any{"slug": "camp-lantern"}),
				storeMineGET(l),
				{Method: "GET", URL: StoreBase(l) + "/mine", Cookie: "shop_seen=ceramic-cup.nope.ceramic-cup..oak-tray.x.y"},
				{Method: "GET", URL: StoreBase(l) + "/cart", Routes: storeLayout + "," + storeCart, Cookie: "shop_seen=ceramic-cup.oak-tray; store_cart=NOSUCHCART"},
				{Method: "POST", URL: StoreBase(l) + "/mine", Body: `{"slug":"wool-throw"}`, Cookie: "shop_seen=a.b.ceramic-cup"},
			}},
			StoreScenario{Name: "bad bodies " + l, Steps: []StoreStep{
				{Method: "POST", URL: StoreBase(l) + "/products/ceramic-cup", Body: `{"color":`},
				{Method: "POST", URL: StoreBase(l) + "/products/ceramic-cup", Body: ``},
				{Method: "POST", URL: StoreBase(l) + "/products/ceramic-cup", Body: `{"color":"moss","size":"","quantity":"1"}`},
				{Method: "POST", URL: StoreBase(l) + "/products/ceramic-cup", Body: `{"color":"moss","size":"","quantity":1.5}`},
				{Method: "POST", URL: StoreBase(l) + "/products/ceramic-cup", Body: `[1]`},
				{Method: "POST", URL: StoreBase(l) + "/products/ceramic-cup", Body: `null`},
				{Method: "POST", URL: StoreBase(l) + "/products/ceramic-cup", Body: `{"color":"moss","size":"","quantity":1} x`},
				{Method: "POST", URL: StoreBase(l) + "/products/ceramic-cup", Body: `{"color":"moss","size":null,"quantity":1,"extra":true}`},
				{Method: "POST", URL: StoreBase(l) + "/products/ceramic-cup", Body: `{"color":"moss","quantity":2}`},
				{Method: "POST", URL: StoreBase(l) + "/products/ceramic-cup", Body: `color=moss&quantity=1`, ContentType: "text/plain"},
				{Method: "PATCH", URL: StoreBase(l) + "/cart", Body: `{"line":1,"quantity":1}`},
				{Method: "POST", URL: StoreBase(l) + "/cart", Body: `{"code":["a"]}`},
				{Method: "POST", URL: StoreBase(l) + "/mine", Body: `{"slug":true}`},
				storeMineGET(l),
			}},
		)
	}
	var missing []StoreStep
	for _, p := range StoreMissing {
		missing = append(missing, StoreStep{Name: p.Name, Method: "GET", URL: p.URL, Routes: p.Routes})
	}
	out = append(out, StoreScenario{Name: "missing", Steps: missing})
	return out
}

// StoreStates are the cart states the pages are looked at in: the steps that make each, then the
// pages whose content depends on the cart.
func StoreStates() []StoreState {
	var out []StoreState
	for _, l := range StoreLangs {
		pages := func(prefix string) []Page {
			return []Page{
				{Name: prefix + " cart", URL: StoreBase(l) + "/cart", Routes: storeLayout + "," + storeCart},
				{Name: prefix + " checkout", URL: StoreBase(l) + "/checkout", Routes: storeLayout + "," + storeCheckout},
			}
		}
		out = append(out,
			StoreState{Name: "demo cart " + l, Pages: pages("demo")},
			StoreState{Name: "own cart " + l, Steps: []StoreStep{
				StoreAddToCart(l, "walnut-pen", "brass", "", 2),
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "ceramic-cup.moss", "quantity": 4}),
				storeWrite("POST", l, "/mine", map[string]any{"slug": "oak-tray"}),
				storeWrite("POST", l, "/mine", map[string]any{"slug": "tea-towels"}),
			}, Pages: pages("own")},
			StoreState{Name: "coupon " + l, Steps: []StoreStep{storeWrite("POST", l, "/cart", map[string]any{"code": "SHANHAI10"})}, Pages: pages("coupon")},
			StoreState{Name: "free shipping " + l, Steps: []StoreStep{
				storeWrite("POST", l, "/cart", map[string]any{"code": "WELCOME30"}),
				StoreAddToCart(l, "wool-throw", "oat", "", 1),
			}, Pages: pages("free shipping")},
			StoreState{Name: "empty cart " + l, Steps: []StoreStep{
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "pour-over-kettle.mist", "quantity": 0}),
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "ceramic-cup.moss", "quantity": 0}),
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "linen-apron.oat.M", "quantity": 0}),
				storeWrite("PATCH", l, "/cart", map[string]any{"line": "reading-candle.charcoal", "quantity": 0}),
			}, Pages: pages("empty")},
			StoreState{Name: "placed order " + l, Steps: []StoreStep{storeWrite("POST", l, "/checkout", storeOrder(l))},
				Pages: []Page{{Name: "placed checkout", URL: StoreBase(l) + "/checkout?placed={order}", Routes: storeLayout + "," + storeCheckout}}},
		)
	}
	return out
}

var (
	cartIDValue = regexp.MustCompile(`^store_cart=[A-Z2-7]{26};`)
	orderNumber = regexp.MustCompile(`SH-[0-9]+`)
)

// RunStoreOnShop runs scenarios against a running shop, each with its own cookies, as a browser
// does: a plain request for a write and a method handler, and for a page the request for its data.
func RunStoreOnShop(ctx context.Context, base, buildID string, scenarios []StoreScenario) ([][]StoreResult, error) {
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var all [][]StoreResult
	for _, sc := range scenarios {
		jar := map[string]string{}
		order := ""
		var results []StoreResult
		for _, st := range sc.Steps {
			u := strings.ReplaceAll(st.URL, "{order}", order)
			if st.Routes != "" {
				sep := "?"
				if strings.Contains(u, "?") {
					sep = "&"
				}
				u += sep + "_tifa=" + buildID + "." + strings.Repeat("1", strings.Count(st.Routes, ",")+1)
			}
			var body io.Reader
			if st.Body != "" {
				body = strings.NewReader(st.Body)
			}
			req, err := http.NewRequestWithContext(ctx, st.Method, base+u, body)
			if err != nil {
				return nil, err
			}
			if st.Body != "" || st.Method == "POST" || st.Method == "PATCH" {
				ct := st.ContentType
				if ct == "" {
					ct = "application/json"
				}
				req.Header.Set("Content-Type", ct)
			}
			req.Header.Set("Accept-Encoding", "identity")
			if st.Cookie != "" {
				req.Header.Set("Cookie", st.Cookie)
			} else if h := cookieHeader(jar); h != "" {
				req.Header.Set("Cookie", h)
			}
			sent := time.Now()
			res, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", st, err)
			}
			data, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				return nil, err
			}
			r := StoreResult{Status: res.StatusCode, At: sent.UnixMilli(), Location: res.Header.Get("Location")}
			if len(data) > 0 && strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
				if err := json.Unmarshal(data, &r.Body); err != nil {
					return nil, fmt.Errorf("%s answered %d with %q: %w", st, res.StatusCode, data, err)
				}
				// The data of a page comes with the session of the visitor, which is not the app's.
				if m, ok := r.Body.(map[string]any); ok && st.Routes != "" {
					r.Body = map[string]any{"routes": m["routes"]}
				}
			}
			for _, c := range res.Header.Values("Set-Cookie") {
				r.SetCookies = append(r.SetCookies, c)
				if parsed, err := http.ParseSetCookie(c); err == nil {
					if parsed.MaxAge < 0 {
						delete(jar, parsed.Name)
					} else {
						jar[parsed.Name] = parsed.Value
					}
				}
			}
			if m := orderNumber.FindString(r.Location); m != "" {
				order = m
			}
			results = append(results, r)
		}
		all = append(all, results)
	}
	return all, nil
}

func cookieHeader(jar map[string]string) string {
	var parts []string
	for _, name := range slices.Sorted(maps.Keys(jar)) {
		parts = append(parts, name+"="+jar[name])
	}
	return strings.Join(parts, "; ")
}

// RunStoreOnNode runs the same scenarios against the port in domain/store/dump.ts.
func (e Env) RunStoreOnNode(ctx context.Context, scenarios []StoreScenario) ([][]StoreResult, error) {
	input, err := json.Marshal(scenarios)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(e.Root, "test", "compare", "domain", "store", "dump.ts"))
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("node domain/store/dump.ts: %w\n%s", err, stderr.String())
	}
	var results [][]StoreResult
	if err := json.Unmarshal(out, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// instantTolerance is how far two answers may be apart in an instant they count from their own
// request: the time the two servers take to answer, and the clocks of the two runs read in
// milliseconds. A delivery that is off by a second is not within it.
const instantTolerance = 250 * time.Millisecond

const instantPrefix = "@instant "

// NormalizeStore removes what differs between two runs of the same scenario: the random cart
// id, the order number (the next of the studio shop, which depends on the orders placed before),
// and the instants counted from the time of the request. An instant becomes its distance in
// milliseconds from the request that made it, written "@instant <ms>", for
// DiffStore to compare within instantTolerance: a run an hour later is the same. The time an order was
// placed (under "placed") is counted from the request that placed it (the 303 of the checkout), not
// from the request that reads it later. A delivery date ("earliest", "latest") is a day of the store,
// carried as noon in its time zone, so it is not counted from a request: it becomes "@day <ms>", the
// instant itself, compared as it is. It is applied to the answers of the shop and of the port alike.
func NormalizeStore(results [][]StoreResult) {
	var from time.Time
	instant := func(t time.Time) string {
		if t.Year() <= 1 {
			return "@instant none" // the zero time of an order without a delivery
		}
		return fmt.Sprintf("%s%d", instantPrefix, t.Sub(from).Milliseconds())
	}
	var walk func(v any, orderAt, at time.Time, placed bool) any
	walk = func(v any, orderAt, at time.Time, placed bool) any {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				if s, ok := e.(string); ok && (k == "earliest" || k == "latest" || k == "placedAt") {
					if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
						from = at
						if k != "placedAt" {
							// A delivery date is a day of the store, the same all day on both servers, so it is
							// compared as it is and not by how far it is from the request.
							x[k] = fmt.Sprintf("@day %d", t.UnixMilli())
							continue
						}
						if (k == "placedAt" || placed) && !orderAt.IsZero() {
							from = orderAt
						}
						x[k] = instant(t)
						continue
					}
				}
				x[k] = walk(e, orderAt, at, placed || k == "placed")
			}
		case []any:
			for i := range x {
				x[i] = walk(x[i], orderAt, at, placed)
			}
		case string:
			return orderNumber.ReplaceAllString(x, "SH-<order>")
		}
		return v
	}
	for _, rs := range results {
		var orderAt time.Time
		for i := range rs {
			at := time.UnixMilli(rs[i].At)
			if rs[i].Status == 303 && strings.Contains(rs[i].Location, "placed=") {
				orderAt = at
			}
			rs[i].Body = walk(rs[i].Body, orderAt, at, false)
			rs[i].Location = orderNumber.ReplaceAllString(rs[i].Location, "SH-<order>")
			for j, c := range rs[i].SetCookies {
				rs[i].SetCookies[j] = cartIDValue.ReplaceAllString(c, "store_cart=<cart>;")
			}
		}
	}
}

// matchInstants makes the instants of two answers that are the same within instantTolerance the
// same text, and leaves the others to be reported.
func matchInstants(a, b any) {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			return
		}
		for k, e := range x {
			f, ok := y[k]
			if !ok {
				continue
			}
			s, sok := e.(string)
			u, uok := f.(string)
			if sok && uok && strings.HasPrefix(s, instantPrefix) && strings.HasPrefix(u, instantPrefix) {
				var p, q int64
				_, err1 := fmt.Sscan(s[len(instantPrefix):], &p)
				_, err2 := fmt.Sscan(u[len(instantPrefix):], &q)
				if err1 == nil && err2 == nil && absDuration(time.Duration(p-q)*time.Millisecond) <= instantTolerance {
					y[k] = s
				}
				continue
			}
			matchInstants(e, f)
		}
	case []any:
		if y, ok := b.([]any); ok {
			for i := range x {
				if i < len(y) {
					matchInstants(x[i], y[i])
				}
			}
		}
	}
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// DiffStore lists where two sets of answers differ, naming the scenario and the step.
func DiffStore(scenarios []StoreScenario, want, got [][]StoreResult, max int) []string {
	var out []string
	if len(want) != len(got) {
		return []string{fmt.Sprintf("Go ran %d scenarios, Node %d", len(want), len(got))}
	}
	for i, sc := range scenarios {
		if len(want[i]) != len(got[i]) {
			out = append(out, fmt.Sprintf("%s: Go answered %d steps, Node %d", sc.Name, len(want[i]), len(got[i])))
			continue
		}
		for j, st := range sc.Steps {
			w, g := want[i][j], got[i][j]
			where := fmt.Sprintf("%s, step %d %s", sc.Name, j+1, st)
			if w.Status != g.Status {
				out = append(out, fmt.Sprintf("%s: status Go %d, Node %d (Go body %s, Node body %s)", where, w.Status, g.Status, show(w.Body), show(g.Body)))
				continue
			}
			if w.Location != g.Location {
				out = append(out, fmt.Sprintf("%s: Location Go %q, Node %q", where, w.Location, g.Location))
			}
			if strings.Join(w.SetCookies, "\n") != strings.Join(g.SetCookies, "\n") {
				out = append(out, fmt.Sprintf("%s: Set-Cookie Go %q, Node %q", where, w.SetCookies, g.SetCookies))
			}
			matchInstants(w.Body, g.Body)
			for _, d := range DiffData(w.Body, g.Body, 5) {
				out = append(out, where+": "+d)
			}
			if len(out) >= max {
				return out[:max]
			}
		}
	}
	return out
}

// ShopBuildID is the id of the build of bin/shop, which the data requests of a page carry.
func (e Env) ShopBuildID() (string, error) {
	var built struct {
		BuildID string `json:"buildId"`
	}
	data, err := os.ReadFile(filepath.Join(e.Shop, ".tifa", "dist", "build.json"))
	if err != nil {
		return "", fmt.Errorf("build the shop first: %w", err)
	}
	if err := json.Unmarshal(data, &built); err != nil {
		return "", err
	}
	return built.BuildID, nil
}

// storeZones are the time zones of the storefront's languages, where its delivery dates are counted.
var storeZones = []string{"Asia/Shanghai", "America/Los_Angeles"}

// StoreDayChanged says whether the day changed in the time zone of either language of the store
// between two instants. A delivery date is a calendar day of the store, so two servers that answer
// on both sides of such a moment give different dates and both are right. A caller that finds a
// difference then runs that sample again, once, and does not widen any tolerance.
func StoreDayChanged(a, b time.Time) bool {
	for _, name := range storeZones {
		zone, err := time.LoadLocation(name)
		if err != nil {
			panic(err)
		}
		ay, am, ad := a.In(zone).Date()
		by, bm, bd := b.In(zone).Date()
		if ay != by || am != bm || ad != bd {
			return true
		}
	}
	return false
}

// OnlyDeliveryDateDiffs says whether every difference that DiffStore listed is in a delivery date
// ("earliest" or "latest"), the only values that depend on the day of the store. A list as long as
// the limit it was made with may have lost others, so it does not count.
func OnlyDeliveryDateDiffs(diffs []string, limit int) bool {
	if len(diffs) == 0 || len(diffs) >= limit {
		return false
	}
	for _, d := range diffs {
		if !deliveryDateDiff.MatchString(d) {
			return false
		}
	}
	return true
}

var deliveryDateDiff = regexp.MustCompile(`/(earliest|latest): `)

// StoreRunSpan is the first and the last time a request of the runs was sent.
func StoreRunSpan(runs ...[][]StoreResult) (first, last time.Time) {
	for _, run := range runs {
		for _, rs := range run {
			for _, r := range rs {
				at := time.UnixMilli(r.At)
				if first.IsZero() || at.Before(first) {
					first = at
				}
				if at.After(last) {
					last = at
				}
			}
		}
	}
	return first, last
}
