package compare_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strings"
	"testing"

	"github.com/liustack/tifa/test/compare"
)

// The storefront of the shop, in the TanStack Start app and in Tifa. The tests need TIFA_COMPARE=1
// like the others of this package.

type reply struct {
	status int
	header http.Header
	body   []byte
}

// get asks for a path with no compression, never following a redirect, and with the cookies of jar.
func get(t *testing.T, jar http.CookieJar, base, path string, header http.Header) reply {
	t.Helper()
	return send(t, jar, base, "GET", path, header, nil)
}

func send(t *testing.T, jar http.CookieJar, base, method, path string, header http.Header, body []byte) reply {
	t.Helper()
	req, err := http.NewRequest(method, base+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Accept-Encoding", "identity")
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return reply{res.StatusCode, res.Header, data}
}

// extra404 are pages of the storefront that answer 404 inside it or in the root, besides
// StoreMissing: a URL no page has, in a language, and one below a page.
var extra404 = []string{"/store/zh-CN/no-such-page", "/store/en-US/cart/extra", "/store/zh-CN/products/oak-tray/extra", "/store/zh-CN/journal/theme-css/extra", "/store/en-US/products/extra/more", "/store/zh-CN/checkout?placed=SS-NOSUCHORDER"}

// TestStoreStartShowsTheSamePagesAsTifa asks Tifa and Start for the storefront's pages (the
// shared lists in store.go) and checks that they answer with the same status and show the same
// title, text and elements. Each cart state of StoreStates is built on each server with the
// writes of the state, as one visitor, and its pages are read with that visitor's cookies.
func TestStoreStartShowsTheSamePagesAsTifa(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()
	var bases [2]string
	for i, name := range []string{"tifa-nocache", "start"} {
		server, err := mustTarget(t, env, name).Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		defer server.Stop()
		bases[i] = server.Base
	}
	check := func(jars [2]http.CookieJar, path string, want int) {
		a, b := get(t, jars[0], bases[0], path, nil), get(t, jars[1], bases[1], path, nil)
		if a.status != want || b.status != want {
			t.Errorf("%s: status %d in Tifa and %d in Start, want %d", path, a.status, b.status, want)
			return
		}
		va, vb := compare.ParseView(a.body), compare.ParseView(b.body)
		for _, v := range []*compare.View{&va, &vb} {
			for i, text := range v.Text {
				v.Text[i] = orderNumber.ReplaceAllString(text, "SS-ORDER")
			}
		}
		if d := compare.Diff(va, vb); d != "" {
			t.Errorf("%s differs:\n%s", path, d)
		} else {
			t.Logf("%-72s %d  %6d bytes in Tifa, %6d in Start", path, a.status, len(a.body), len(b.body))
		}
	}
	for _, p := range compare.StorePages() {
		check([2]http.CookieJar{}, p.URL, 200)
	}
	for _, p := range compare.StoreMissing {
		check([2]http.CookieJar{}, p.URL, 404)
	}
	for _, path := range extra404 {
		check([2]http.CookieJar{}, path, 404)
	}
	for _, state := range compare.StoreStates() {
		var jars [2]http.CookieJar
		var orders [2]string // the order each server placed last
		for i := range jars {
			jars[i], _ = cookiejar.New(nil)
			for _, s := range state.Steps {
				r := send(t, jars[i], bases[i], s.Method, s.URL, jsonHeader, []byte(s.Body))
				if r.status >= 400 {
					t.Fatalf("%s: %s answered %d on %s: %s", state.Name, s, r.status, bases[i], r.body)
				}
				if loc := r.header.Get("Location"); r.status == 303 {
					orders[i] = loc[strings.Index(loc, "placed=")+len("placed="):]
				}
			}
		}
		for _, p := range state.Pages {
			if strings.Contains(p.URL, "{order}") {
				// The order numbers are random, so each server is asked for its own. They are
				// masked in the text.
				a := get(t, jars[0], bases[0], strings.ReplaceAll(p.URL, "{order}", orders[0]), nil)
				b := get(t, jars[1], bases[1], strings.ReplaceAll(p.URL, "{order}", orders[1]), nil)
				va, vb := compare.ParseView(a.body), compare.ParseView(b.body)
				for _, v := range []*compare.View{&va, &vb} {
					for i, text := range v.Text {
						v.Text[i] = orderNumber.ReplaceAllString(text, "SS-ORDER")
					}
				}
				if a.status != b.status || compare.Diff(va, vb) != "" {
					t.Errorf("%s: %d in Tifa, %d in Start\n%s", p.Name, a.status, b.status, compare.Diff(va, vb))
				}
				continue
			}
			check(jars, p.URL, 200)
		}
	}
}

// step is one request of a visit and what the test keeps of its answer.
type step struct {
	label  string
	method string
	path   string
	header http.Header
	body   string
	// view says the answer is a page and is compared by what a reader sees, not byte for byte.
	view bool
}

// seen is what a server did for a step: the status, the Location, the cookies it set (names
// and attributes, the cart's random value left out) and the body, or what a reader sees.
type seen struct {
	Status   int
	Location string
	Vary     bool // the answer says it depends on Accept-Language
	Cookies  []string
	Body     string
	View     compare.View
}

var (
	orderNumber = regexp.MustCompile(`SS-[0-9A-Z]{10}`)
	cartCookie  = regexp.MustCompile(`store_cart=[^;]+`)
)

// observe runs the steps against a server with one cookie jar, as one visitor.
func observe(t *testing.T, base string, steps []step) []seen {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []seen
	for _, s := range steps {
		r := send(t, jar, base, s.method, s.path, s.header, []byte(s.body))
		o := seen{Status: r.status, Location: orderNumber.ReplaceAllString(r.header.Get("Location"), "SS-ORDER"), Vary: strings.Contains(strings.Join(r.header.Values("Vary"), ","), "Accept-Language")}
		for _, c := range r.header.Values("Set-Cookie") {
			o.Cookies = append(o.Cookies, cartCookie.ReplaceAllString(c, "store_cart=ID"))
		}
		if s.view {
			v := compare.ParseView(r.body)
			for i, text := range v.Text {
				v.Text[i] = orderNumber.ReplaceAllString(text, "SS-ORDER")
			}
			o.View = v
		} else if r.status < 300 || r.status >= 400 {
			o.Body = orderNumber.ReplaceAllString(string(r.body), "SS-ORDER")
		}
		out = append(out, o)
	}
	return out
}

var (
	jsonHeader = http.Header{"Content-Type": {"application/json"}}
	callHeader = http.Header{"Content-Type": {"application/json"}, "Accept": {"application/json"}, "X-Tifa-Call": {"1"}}
)

// A valid order for the checkout, as the form sends it.
const goodOrder = `{"email":"lin@example.com","address":{"name":"Lin Xia","phone":"138 0013 8000","country":"CN","region":"Shanghai","city":"Shanghai","line1":"99 Wukang Road","line2":"","postalCode":"200031"},"delivery":"standard","payment":"card","cardNumber":"4242 4242 4242 4242","cardExpiry":"12/30","cardCvc":"123","billingSame":true,"notes":"","newsletter":false,"terms":true}`

// storeWriteSteps is a visit that uses every write of the storefront, the ways each can fail
// included, and looks at the page it changes after each.
func storeWriteSteps() []step {
	const zh = "/store/zh-CN"
	return []step{
		{label: "store redirect, no language", method: "GET", path: "/store"},
		{label: "store redirect, English", method: "GET", path: "/store", header: http.Header{"Accept-Language": {"en-GB,en;q=0.8"}}},
		{label: "mine of a new visitor", method: "GET", path: zh + "/mine"},
		{label: "mark seen", method: "POST", path: zh + "/mine", header: jsonHeader, body: `{"slug":"pour-over-kettle"}`},
		{label: "mark seen, unknown", method: "POST", path: zh + "/mine", header: jsonHeader, body: `{"slug":"nothing"}`},
		{label: "mark seen again", method: "POST", path: zh + "/mine", header: jsonHeader, body: `{"slug":"oak-tray"}`},
		{label: "mine after seeing", method: "GET", path: zh + "/mine"},
		{label: "cart page, demonstration cart", method: "GET", path: zh + "/cart", view: true},
		{label: "add: unknown colour", method: "POST", path: zh + "/products/pour-over-kettle", header: jsonHeader, body: `{"color":"pink","size":"","quantity":1}`},
		{label: "add: quantity 0", method: "POST", path: zh + "/products/pour-over-kettle", header: jsonHeader, body: `{"color":"mist","size":"","quantity":0}`},
		{label: "add: unknown product", method: "POST", path: zh + "/products/nothing", header: jsonHeader, body: `{"color":"mist","size":"","quantity":1}`},
		{label: "add: bad JSON", method: "POST", path: zh + "/products/pour-over-kettle", header: jsonHeader, body: `{`},
		{label: "add", method: "POST", path: zh + "/products/pour-over-kettle", header: jsonHeader, body: `{"color":"mist","size":"","quantity":2}`},
		{label: "add with size", method: "POST", path: zh + "/products/linen-apron", header: jsonHeader, body: `{"color":"oat","size":"M","quantity":1}`},
		{label: "mine after adding", method: "GET", path: zh + "/mine"},
		{label: "cart page", method: "GET", path: zh + "/cart", view: true},
		{label: "quantity: unknown line", method: "PATCH", path: zh + "/cart", header: jsonHeader, body: `{"line":"nothing.red","quantity":1}`},
		{label: "quantity: too many", method: "PATCH", path: zh + "/cart", header: jsonHeader, body: `{"line":"pour-over-kettle.mist","quantity":11}`},
		{label: "quantity", method: "PATCH", path: zh + "/cart", header: jsonHeader, body: `{"line":"pour-over-kettle.mist","quantity":5}`},
		{label: "quantity: remove", method: "PATCH", path: zh + "/cart", header: jsonHeader, body: `{"line":"linen-apron.oat.M","quantity":0}`},
		{label: "coupon: unknown", method: "POST", path: zh + "/cart", header: jsonHeader, body: `{"code":"NOPE"}`},
		{label: "coupon", method: "POST", path: zh + "/cart", header: jsonHeader, body: `{"code":" shanhai10 "}`},
		{label: "cart page with coupon", method: "GET", path: zh + "/cart", view: true},
		{label: "coupon: off", method: "POST", path: zh + "/cart", header: jsonHeader, body: `{"code":""}`},
		{label: "checkout page", method: "GET", path: zh + "/checkout", view: true},
		{label: "checkout: empty form", method: "POST", path: zh + "/checkout", header: callHeader, body: `{}`},
		{label: "checkout: bad card", method: "POST", path: zh + "/checkout", header: callHeader, body: strings.Replace(goodOrder, "4242 4242 4242 4242", "4242 4242 4242 4241", 1)},
		{label: "checkout", method: "POST", path: zh + "/checkout", header: callHeader, body: goodOrder},
		{label: "checkout again, empty cart", method: "POST", path: zh + "/checkout", header: callHeader, body: goodOrder},
		{label: "cart page, empty", method: "GET", path: zh + "/cart", view: true},
		{label: "mine, empty cart", method: "GET", path: zh + "/mine"},
		{label: "add in English", method: "POST", path: "/store/en-US/products/ceramic-cup", header: jsonHeader, body: `{"color":"moss","size":"","quantity":1}`},
		{label: "checkout as a form post", method: "POST", path: "/store/en-US/checkout", header: jsonHeader, body: goodOrder},
	}
}

// TestStoreStartWritesDoWhatTifaDoes plays a visit that uses every write of the storefront on
// Tifa and on Start and checks that each step gets the same status, redirect, cookies and body, and
// that the page after a write shows the same.
func TestStoreStartWritesDoWhatTifaDoes(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()
	steps := storeWriteSteps()
	var results [2][]seen
	for i, name := range []string{"tifa-nocache", "start"} {
		server, err := mustTarget(t, env, name).Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		results[i] = observe(t, server.Base, steps)
		server.Stop()
	}
	for i, s := range steps {
		a, b := results[0][i], results[1][i]
		viewDiff := compare.Diff(a.View, b.View)
		a.View, b.View = compare.View{}, compare.View{}
		if fmt.Sprint(a) != fmt.Sprint(b) || viewDiff != "" {
			t.Errorf("%s: %s %s\n  Tifa:  %+v\n  Start: %+v\n%s", s.label, s.method, s.path, a, b, viewDiff)
		} else {
			t.Logf("%-32s %d %s", s.label, a.Status, strings.Join(a.Cookies, " "))
		}
	}
}

// TestStoreStartKeepsTheWorkbench checks that the six workbench pages are still the same in Start
// now that the app also serves the storefront (the photos come from the shop's own images.ts).
func TestStoreStartKeepsTheWorkbench(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()
	var bases [2]string
	for i, name := range []string{"tifa-nocache", "start"} {
		server, err := mustTarget(t, env, name).Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		defer server.Stop()
		bases[i] = server.Base
	}
	for _, p := range compare.Pages {
		a, b := get(t, nil, bases[0], p.URL, nil), get(t, nil, bases[1], p.URL, nil)
		if a.status != 200 || b.status != 200 {
			t.Errorf("%s: status %d in Tifa and %d in Start", p.URL, a.status, b.status)
			continue
		}
		if d := compare.Diff(compare.ParseView(a.body), compare.ParseView(b.body)); d != "" {
			t.Errorf("%s differs:\n%s", p.URL, d)
		}
	}
}
