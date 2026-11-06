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
	"time"
)

// The storefront of examples/shop, at /store/{lang}. Its data is computed in Node by
// domain/store, a port of the Go code that TestStoreDomainMatchesGo keeps equal to the Go code.

// The route ids of the storefront's Layout and Load functions, as tifa names them.
const (
	storeLayout   = "app/store/lang+/layout"
	storeHome     = "app/store/lang+/route"
	storeList     = "app/store/lang+/products/route"
	storeProduct  = "app/store/lang+/products/product+/route"
	storeCart     = "app/store/lang+/cart/route"
	storeCheckout = "app/store/lang+/checkout/route"
	storeJournal  = "app/store/lang+/journal/route"
	storePost     = "app/store/lang+/journal/post+/route"
	storeMine     = "app/store/lang+/mine/route"
)

// StoreLangs are the two languages of the storefront.
var StoreLangs = []string{"zh-CN", "en-US"}

// StoreProducts are every product of the catalogue, in its order. TestStoreDomainMatchesGo checks
// that the list is the catalogue.
var StoreProducts = []string{
	"pour-over-kettle", "ceramic-cup", "glass-carafe", "oak-tray", "tea-canister", "linen-coasters", "linen-apron", "wool-throw",
	"tea-towels", "canvas-tote", "brass-bookmarks", "dot-notebook", "walnut-pen", "desk-tray", "reading-candle", "forest-diffuser",
	"incense-set", "folding-stool", "picnic-blanket", "insulated-bottle", "enamel-mug", "stoneware-vase", "cotton-napkins",
	"pocket-journal", "hinoki-spray", "camp-lantern", "copper-cups", "wax-cloth",
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
		base := "/store/" + l
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

// StoreMissing are pages of the storefront that answer 404: a language, a product and a post that
// do not exist. A data request answers 200 with the status of each route in it.
var StoreMissing = []Page{
	{Name: "language", URL: "/store/fr-FR", Routes: storeLayout + "," + storeHome},
	{Name: "language products", URL: "/store/fr-FR/products", Routes: storeLayout + "," + storeList},
	{Name: "product", URL: "/store/zh-CN/products/no-such-product", Routes: storeLayout + "," + storeProduct},
	{Name: "post", URL: "/store/en-US/journal/no-such-post", Routes: storeLayout + "," + storePost},
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
	Cookie         string `json:"cookie,omitempty"`
	AcceptLanguage string `json:"acceptLanguage,omitempty"`
}

// StoreScenario is a visitor: steps that run in order with one set of cookies.
type StoreScenario struct {
	Name  string      `json:"name"`
	Steps []StoreStep `json:"steps"`
}

// StoreResult is what a step answered.
type StoreResult struct {
	Status     int      `json:"status"`
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
	return StoreStep{Method: "GET", URL: "/store/" + l + path, Routes: storeLayout + "," + page}
}

func storeWrite(method, l, path string, body any) StoreStep {
	s := StoreStep{Method: method, URL: "/store/" + l + path}
	switch b := body.(type) {
	case nil:
	case string:
		s.Body = b
	default:
		s.Body = string(mustJSON(b))
	}
	return s
}

func storeMineGET(l string) StoreStep { return StoreStep{Method: "GET", URL: "/store/" + l + "/mine"} }

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
			if strings.HasPrefix(p.URL, "/store/"+l) {
				pages = append(pages, StoreStep{Name: p.Name, Method: "GET", URL: p.URL, Routes: p.Routes})
			}
		}
		for _, slug := range StoreProducts {
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
				storeWrite("PATCH", "xx-XX", "/cart", map[string]any{"line": "ceramic-cup.moss", "quantity": 1}),
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
				{Method: "GET", URL: "/store/" + l + "/checkout?placed={order}", Routes: storeLayout + "," + storeCheckout},
				storeGET(l, "/cart", storeCart),
				storeWrite("POST", l, "/checkout", orderOK),
				{Method: "GET", URL: "/store/" + l + "/checkout?placed={order}", Routes: storeLayout + "," + storeCheckout, Cookie: "store_cart=SOMEONEELSE"},
				{Method: "GET", URL: "/store/" + l + "/checkout?placed={order}", Routes: storeLayout + "," + storeCheckout, Cookie: "unrelated=1"},
			}},
			StoreScenario{Name: "check out express " + l, Steps: []StoreStep{
				StoreAddToCart(l, "walnut-pen", "brass", "", 1),
				storeWrite("POST", l, "/checkout", storeOrder(l, func(m map[string]any) { m["delivery"] = "express"; m["payment"] = "alipay"; m["cardNumber"] = "" })),
				{Method: "GET", URL: "/store/" + l + "/checkout?placed={order}", Routes: storeLayout + "," + storeCheckout},
				StoreAddToCart(l, "walnut-pen", "charcoal", "", 3),
				storeWrite("POST", l, "/checkout", storeOrder(l, func(m map[string]any) { m["delivery"] = "pickup"; m["payment"] = "wechat" })),
				{Method: "GET", URL: "/store/" + l + "/checkout?placed={order}", Routes: storeLayout + "," + storeCheckout},
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
				{Method: "POST", URL: "/store/" + l + "/checkout", Body: orderOK, Cookie: "store_cart=NOSUCHCART"},
				{Method: "POST", URL: "/store/xx-XX/checkout", Body: orderOK},
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
				{Method: "GET", URL: "/store/" + l + "/mine", Cookie: "shop_seen=ceramic-cup.nope.ceramic-cup..oak-tray.x.y"},
				{Method: "GET", URL: "/store/" + l + "/cart", Routes: storeLayout + "," + storeCart, Cookie: "shop_seen=ceramic-cup.oak-tray; store_cart=NOSUCHCART"},
				{Method: "POST", URL: "/store/" + l + "/mine", Body: `{"slug":"wool-throw"}`, Cookie: "shop_seen=a.b.ceramic-cup"},
			}},
			StoreScenario{Name: "bad bodies " + l, Steps: []StoreStep{
				{Method: "POST", URL: "/store/" + l + "/products/ceramic-cup", Body: `{"color":`},
				{Method: "POST", URL: "/store/" + l + "/products/ceramic-cup", Body: ``},
				{Method: "POST", URL: "/store/" + l + "/products/ceramic-cup", Body: `{"color":"moss","size":"","quantity":"1"}`},
				{Method: "POST", URL: "/store/" + l + "/products/ceramic-cup", Body: `{"color":"moss","size":"","quantity":1.5}`},
				{Method: "POST", URL: "/store/" + l + "/products/ceramic-cup", Body: `[1]`},
				{Method: "POST", URL: "/store/" + l + "/products/ceramic-cup", Body: `null`},
				{Method: "POST", URL: "/store/" + l + "/products/ceramic-cup", Body: `{"color":"moss","size":"","quantity":1} x`},
				{Method: "POST", URL: "/store/" + l + "/products/ceramic-cup", Body: `{"color":"moss","size":null,"quantity":1,"extra":true}`},
				{Method: "POST", URL: "/store/" + l + "/products/ceramic-cup", Body: `{"color":"moss","quantity":2}`},
				{Method: "POST", URL: "/store/" + l + "/products/ceramic-cup", Body: `color=moss&quantity=1`, ContentType: "text/plain"},
				{Method: "PATCH", URL: "/store/" + l + "/cart", Body: `{"line":1,"quantity":1}`},
				{Method: "POST", URL: "/store/" + l + "/cart", Body: `{"code":["a"]}`},
				{Method: "POST", URL: "/store/" + l + "/mine", Body: `{"slug":true}`},
				storeMineGET(l),
			}},
		)
	}
	var redirects []StoreStep
	for _, accept := range []string{"", "zh-CN", "zh", "zh-TW", "zh-Hant", "en", "en-US", "en-GB", "en-AU,en;q=0.9", "fr", "fr-FR,fr;q=0.9", "ja,en;q=0.5", "de;q=0.9,en;q=0.8,zh;q=0.7", "zh;q=0.3,en;q=0.8", "en;q=0, zh", "*", "xx-invalid", "en-US,zh-CN;q=0.9", "es-419,es;q=0.9,en;q=0.5"} {
		redirects = append(redirects, StoreStep{Name: "Accept-Language " + accept, Method: "GET", URL: "/store", AcceptLanguage: accept})
	}
	out = append(out, StoreScenario{Name: "language redirect", Steps: redirects})
	var missing []StoreStep
	for _, p := range StoreMissing {
		missing = append(missing, StoreStep{Name: p.Name, Method: "GET", URL: p.URL, Routes: p.Routes})
	}
	missing = append(missing, StoreStep{Method: "GET", URL: "/store/fr-FR/mine"})
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
				{Name: prefix + " cart", URL: "/store/" + l + "/cart", Routes: storeLayout + "," + storeCart},
				{Name: prefix + " checkout", URL: "/store/" + l + "/checkout", Routes: storeLayout + "," + storeCheckout},
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
				Pages: []Page{{Name: "placed checkout", URL: "/store/" + l + "/checkout?placed={order}", Routes: storeLayout + "," + storeCheckout}}},
		)
	}
	return out
}

var (
	cartIDValue = regexp.MustCompile(`^store_cart=[A-Z2-7]{26};`)
	orderNumber = regexp.MustCompile(`SS-[A-Z2-7]{10}`)
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
			if st.AcceptLanguage != "" {
				req.Header.Set("Accept-Language", st.AcceptLanguage)
			}
			req.Header.Set("Accept-Encoding", "identity")
			if st.Cookie != "" {
				req.Header.Set("Cookie", st.Cookie)
			} else if h := cookieHeader(jar); h != "" {
				req.Header.Set("Cookie", h)
			}
			res, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", st, err)
			}
			data, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				return nil, err
			}
			r := StoreResult{Status: res.StatusCode, Location: res.Header.Get("Location")}
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

// NormalizeStore removes what differs between two runs of the same scenario: the random cart
// id and order number, and the instants counted from the time of the request, which become the
// number of days from now. It is applied to the answers of the shop and of the port alike.
func NormalizeStore(results [][]StoreResult, now time.Time) {
	var walk func(v any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				if s, ok := e.(string); ok && (k == "earliest" || k == "latest" || k == "placedAt") {
					if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
						x[k] = fmt.Sprintf("%+d days from now", int(t.Sub(now).Round(24*time.Hour)/(24*time.Hour)))
						continue
					}
				}
				x[k] = walk(e)
			}
		case []any:
			for i := range x {
				x[i] = walk(x[i])
			}
		case string:
			return orderNumber.ReplaceAllString(x, "SS-<order>")
		}
		return v
	}
	for _, rs := range results {
		for i := range rs {
			rs[i].Body = walk(rs[i].Body)
			rs[i].Location = orderNumber.ReplaceAllString(rs[i].Location, "SS-<order>")
			for j, c := range rs[i].SetCookies {
				rs[i].SetCookies[j] = cartIDValue.ReplaceAllString(c, "store_cart=<cart>;")
			}
		}
	}
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
