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
	for _, accept := range append(append(StoreAcceptLanguages(), generatedAcceptLanguages(8000)...), tieAcceptLanguages()...) {
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
// id and order number, and the instants counted from the time of the request. An instant becomes
// its distance in milliseconds from the request that made it, written "@instant <ms>", for
// DiffStore to compare within instantTolerance: a run an hour later is the same and a delivery that
// is off by an hour is not. The time an order was placed, and its delivery (the keys under "placed"), are
// counted from the request that placed it (the 303 of the checkout), not from the request that reads
// it later. A quote for the cart is counted from the request that reads it. It is applied to the answers
// of the shop and of the port alike.
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
			return orderNumber.ReplaceAllString(x, "SS-<order>")
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
			rs[i].Location = orderNumber.ReplaceAllString(rs[i].Location, "SS-<order>")
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

// StoreAcceptLanguages are values of Accept-Language that the storefront answers with the language
// golang.org/x/text/language picks: the common ones, the ones it refuses as a whole (a weight
// that does not parse, a tag it does not know), the spellings it takes (en_US, upper case, spaces,
// a weight in any float form) and pairs of tags in both orders and with weights, made the same
// way on every run.
func StoreAcceptLanguages() []string {
	out := []string{
		"", "zh-CN", "zh", "zh-TW", "zh-Hant", "en", "en-US", "en-GB", "en-AU,en;q=0.9", "fr", "fr-FR,fr;q=0.9", "ja,en;q=0.5",
		"de;q=0.9,en;q=0.8,zh;q=0.7", "zh;q=0.3,en;q=0.8", "en;q=0, zh", "*", "xx-invalid", "en-US,zh-CN;q=0.9", "es-419,es;q=0.9,en;q=0.5",
		"en;q=bad", "en,invalid_tag", "en_US", "zh_CN", "zh_Hant_TW", "EN-us", "ZH-cn", " en ; q = 0.5 ", "en;q=0.5;x=1", "en;level=1",
		"en;Q=0.5", "en;q=1e-1", "en;q=.5", "en;q=+1", "en;q=inf", "en;q=-inf", "en;q=nan", "en;q=0x1p-1", "en;q=1_0", "en;q=-1", "en;q=0", "en;q=5",
		"en;q=0.0000001", "en;q=1e-50", "en;q=1e50", "zh;q=0,en", "zh;q=0", "en;q=", "en;q", "en;", "en;=0.5", "zh,en;q=0.5;", "english", "Deutsch", "french", "Italian",
		"*;q=0.5,en", "en,*", ",,en,,", "en,", ",en", ",", "-", "en-", "-en", "en--US", "en-US-", "x-foo", "x", "und", "und-US", "und-Latn", "und-Hans",
		"zh-yue", "yue", "cmn", "zh-cmn-Hans", "zh-Hans-CN", "zh-Hant-TW", "zh-HK", "zh-SG", "zh-MO", "en-Latn-GB", "en-Cyrl", "en-Latn", "ja-Latn", "sr-Latn", "sr-Cyrl",
		"no", "nb", "nn", "iw", "he", "in", "id", "ji", "mo", "ro", "sh", "tl", "fil", "zu", "zul", "af", "xx", "zz", "qaa", "aaa", "eng", "zho", "chi", "en-ZZ", "en-XX", "en-QO",
		"en-US-u-ca-gregory", "en-u-ca", "en-a-b", "en-a-bb", "en-valencia", "ca-valencia", "en-posix", "en-1994", "zh-x-private", "en-t-zh", "en-US-x-a", "en-US-x",
		"en-GB-oed", "i-klingon", "i-default", "art-lojban", "zh-min-nan", "zh-guoyu", "en-419", "es-419", "en-001", "en-150", "zh-CN-u-nu-hanidec", "en-840", "en-USA",
		"en-Latn-USA", "en-t-xx", "en;q=1e999", "en;q=+nan", "en;q=-nan", "en;q=0x1__0p0", "en;q=0x1p1_",
		"en,zh;q=0x0p1024", "en;q=0x1" + strings.Repeat("0", 256) + "p-1024", "en,zh;q=0x0p99999", "en,zh;q=0x0.0p1", "zh,en;q=0x0p-99999",
		"en,zh;q=0x" + strings.Repeat("f", 40) + "p-160", "en,zh;q=0x" + strings.Repeat("0", 300) + "1p0", "en,zh;q=0x1" + strings.Repeat("0", 300) + "p-1200",
		"1", "123", "abcdefghi", "abcdefgh", "en-abcdefghi", "en\ttab", "en-US,", "en;q=0.5,", "ZH-HANS", "ZH-HANT-HK", "zH_hAnS_cN",
	}
	// Hexadecimal and decimal weights at the edges of a float32: the largest, the smallest, a tie that
	// rounds to even, one past the largest, and a mantissa and an exponent that are out of range alone but not together.
	for _, w := range []string{
		"0x1p127", "0x1p128", "0x1.fffffep127", "0x1.ffffffp127", "0x1.fffff8p127", "0x1.fffffe0000001p127", "0x1p-126", "0x1p-149", "0x1p-150", "0x1.8p-150", "0x1.0000001p-150", "0x1p-151", "0x1p-1074", "0x1p-1075",
		"0x.8p1", "0x8p-3", "0x8.p-3", "0x.p1", "0x1P+1", "0X1p1", "0x1p+", "0x1p", "0x_1p0", "0x1_0p0", "0x1p_1", "0x1p1_0", "0x1__0p0", "0x1p1_",
		"3.4028235e38", "3.4028236e38", "3.40282356779733661637539395458142568447e38", "3.4028235677973366e38", "1e38", "1e39", "1e-45", "1e-46", "7e-46", "1.4e-45", "0e999999", "1e99999999999", "0.000000000000000000000000000000000000000000001",
		"inf", "Infinity", "-Infinity", "infinit", "+inf", "nan", "NAN", "-nan", "+nan",
	} {
		out = append(out, "en;q="+w+",zh;q=0.5", "zh,en;q="+w)
	}
	vocabulary := []string{"en", "en-US", "en-GB", "en-AU", "zh", "zh-CN", "zh-TW", "zh-HK", "zh-Hans", "zh-Hant", "zh-Hant-TW", "yue", "fr", "fr-CA", "de", "ja", "ko", "es-MX", "pt-BR", "ru", "ar", "hi", "zu", "af", "und", "*", "no", "sr-Latn", "ja-Latn", "en-Cyrl", "xx", "yue-HK", "cmn", "zh-cmn", "zh-Hant-HK", "zh-Latn", "en-Latn-US", "en-Hans", "nb-NO", "iw", "en-GB-x-a", "pt-PT", "en-SG", "zh-Hans-SG"}
	weights := []string{"", ";q=1", ";q=0.9", ";q=0.5", ";q=0.1", ";q=0", ";q=0.9999", ";q=2"}
	seed := uint32(12345)
	next := func(n int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>8) % n
	}
	for i := 0; i < 2500; i++ {
		n := 2 + next(2)
		var parts []string
		for j := 0; j < n; j++ {
			parts = append(parts, vocabulary[next(len(vocabulary))]+weights[next(len(weights))])
		}
		out = append(out, strings.Join(parts, ","))
	}
	return out
}

// generatedAcceptLanguages are n values of Accept-Language built from the grammar of a language
// tag and of a weight, with parts that are right, parts that are almost right and characters put
// in or taken out at random, from a fixed seed. Go reads each of them, so the port is held to what
// Go does with all of them, which a list of words chosen by hand cannot ask for.
func generatedAcceptLanguages(n int) []string {
	seed := uint64(0x9e3779b97f4a7c15)
	next := func(k int) int {
		seed = seed*6364136223846793005 + 1442695040888963407
		return int((seed >> 33) % uint64(k))
	}
	pick := func(list ...string) string { return list[next(len(list))] }
	letters := func(k int, alphabet string) string {
		out := make([]byte, k)
		for i := range out {
			out[i] = alphabet[next(len(alphabet))]
		}
		return string(out)
	}
	languages := []string{"en", "zh", "yue", "cmn", "wuu", "gan", "hak", "nan", "hsn", "lzh", "fr", "de", "ja", "ko", "es", "pt", "ru", "ar", "hi", "zu", "af", "no", "nb", "iw", "he", "in", "id", "ji", "und", "sr", "ca", "eng", "zho", "chi", "mo", "sh", "tl"}
	scripts := []string{"Latn", "Hans", "Hant", "Cyrl", "Arab", "Hani", "Zzzz", "Qaaa"}
	regions := []string{"US", "GB", "CN", "TW", "HK", "SG", "MO", "AU", "CA", "FR", "DE", "JP", "419", "001", "150", "840", "USA", "GBR", "CHN", "TWN", "UK", "ZZ", "XX", "QO", "AA"}
	variants := []string{"valencia", "posix", "1994", "1996", "pinyin", "wadegile", "oxendict", "fonipa", "abcde", "nedis"}
	extensions := []string{"u-ca-gregory", "u-nu-latn-ca-buddhist", "u-ca", "u-ca-gregory-ca-buddhist", "u-ca-gregory-ca-gregory", "u-attr1-ca-iso8601", "t-zh", "t-zh-hans", "t-xx", "t-en-us-h0-hybrid", "t-h0-hybrid", "t", "a-bb", "a-b", "x-private", "x-a-b", "x", "u", "z-abc-de", "b-cd-ef-g"}
	separator := func() string { return pick("-", "-", "-", "_") }
	tag := func() string {
		var b strings.Builder
		lang := pick(append([]string{"en", "en", "en", "zh"}, languages...)...)
		if next(8) == 0 {
			lang = letters(2+next(2), "abcdefghijklmnopqrstuvwxyz")
		}
		b.WriteString(lang)
		if next(10) == 0 {
			b.WriteString(separator() + pick(languages...)) // an extlang
		}
		if next(3) == 0 {
			b.WriteString(separator() + pick(scripts...))
		}
		if next(3) == 0 {
			b.WriteString(separator() + pick(regions...))
		}
		if next(8) == 0 {
			b.WriteString(separator() + pick(variants...))
		}
		if next(6) == 0 {
			b.WriteString(separator() + pick(extensions...))
		}
		s := b.String()
		switch next(6) {
		case 0:
			s = strings.ToUpper(s)
		case 1:
			s = strings.ToLower(s)
		}
		return s
	}
	weight := func() string {
		numbers := []string{"1", "0.9", "0.5", "0.1", "0", "0.0", "1.0", ".5", "5.", "1e0", "1E-1", "1e999", "1e-999", "2", "-1", "+1", "+.5", "00.5", "0.5e1", "1_0", "1__0", "_1", "1_", "0.1_5", "1e1_0", "1e_1", "0x1p0", "0X1P-1", "0x.8p1", "0x1.8p0", "0x1p", "0x1", "0x1__0p0", "0x_1p0", "0x1p1_", "0x1_p1", "0xffffffp100", "0x1p128", "3.5e38", "3.4e38", "inf", "-inf", "+Inf", "infinity", "Infinity", "infin", "nan", "NaN", "+nan", "-nan", "bad", "", "1,5", "0,5", "0b1", "0o7"}
		prefix := pick("q=", "q=", "q=", "q =", "Q=", "q", "=", "q=", "level=", "")
		return ";" + pick("", " ", "  ") + prefix + pick("", " ") + pick(numbers...)
	}
	mutate := func(s string) string {
		for k := next(3); k > 0 && s != ""; k-- {
			at := next(len(s))
			switch next(3) {
			case 0:
				s = s[:at] + s[at+1:]
			case 1:
				s = s[:at] + letters(1, "abcxyz019-_;,= .+*") + s[at:]
			default:
				s = s[:at] + letters(1, "abcxyz019-_;,= .+*") + s[at+1:]
			}
		}
		return s
	}
	var out []string
	for len(out) < n {
		var entries []string
		for k := 1 + next(3); k > 0; k-- {
			e := tag()
			if next(3) == 0 {
				e += weight()
			}
			if next(12) == 0 {
				e = mutate(e)
			}
			entries = append(entries, e)
		}
		header := strings.Join(entries, pick(",", ",", ", ", " , "))
		if next(30) == 0 {
			header = mutate(header)
		}
		out = append(out, header)
	}
	return out
}

// tieAcceptLanguages are headers made to reach the last rules of the matcher, which only decide
// between tags that the earlier rules rank alike: tags of one language written with other regions
// and scripts, with another language between them or not (so that the first is not pinned), in
// both orders and with weights that put them in another order. They are every ordered pair of a
// small set of tags, and pairs and triples of a larger set from a fixed seed with a language
// repeated.
func tieAcceptLanguages() []string {
	var out []string
	var small []string
	for _, l := range []string{"en", "zh"} {
		for _, s := range []string{"", "Hans", "Hant"} {
			for _, r := range []string{"", "US", "CN", "SG", "HK", "TW", "GB", "AU", "CA", "001", "150", "419"} {
				small = append(small, strings.Join(filterEmpty(l, s, r), "-"))
			}
		}
	}
	for _, a := range small {
		for _, b := range small {
			out = append(out, a+","+b)
		}
	}
	var universe []string
	for _, l := range []string{"en", "zh", "yue", "cmn", "und", "eng", "zho"} {
		for _, s := range []string{"", "Latn", "Hans", "Hant", "Cyrl"} {
			for _, r := range []string{"", "US", "CN", "SG", "HK", "TW", "GB", "AU", "CA", "NZ", "MO", "IN", "FR", "001", "150", "419", "UK"} {
				universe = append(universe, strings.Join(filterEmpty(l, s, r), "-"))
			}
		}
	}
	seed := uint64(0x2545f4914f6cdd1d)
	next := func(k int) int {
		seed = seed*6364136223846793005 + 1442695040888963407
		return int((seed >> 33) % uint64(k))
	}
	weights := []string{"", "", ";q=0.9", ";q=0.8", ";q=0.5", ";q=1"}
	tag := func() string { return universe[next(len(universe))] + weights[next(len(weights))] }
	for i := 0; i < 20000; i++ {
		a, b, c := tag(), tag(), tag()
		if next(2) == 0 {
			// The first and the last of one language, with another between them.
			lang, _, _ := strings.Cut(a, "-")
			lang, _, _ = strings.Cut(lang, ";")
			c = lang + c[strings.IndexAny(c+"-", "-;"):]
		}
		if next(3) == 0 {
			out = append(out, a+","+b)
		} else {
			out = append(out, a+","+b+","+c)
		}
	}
	return out
}

func filterEmpty(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
