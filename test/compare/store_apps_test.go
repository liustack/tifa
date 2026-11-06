package compare_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/liustack/tifa/test/compare"
)

// The storefront of the shop in the three Node apps against Tifa. One harness runs every check
// against each app: the same requests go to Tifa and to the app, each with a visitor that keeps
// cookies, and what comes back must be equal. Needs TIFA_COMPARE=1 like the other tests:
//
//	TIFA_COMPARE=1 go test -count=1 -v -run TestStorefront ./test/compare
//
// Where an app cannot answer as Tifa does, the app says so in storeApps, with the reason, and the
// case is skipped for that app and logged. Nothing else is left out.

// storeApp is one of the Node apps.
type storeApp struct {
	name  string
	label string
	// write turns the URL of a write in the shop into the URL of the same write in this app.
	// Next.js and React Router have a page at the URL of the cart, the checkout and the product,
	// and answer a POST or a PATCH there with the page, so their writes are at a path of their
	// own, under /store so that the cookies, whose path is /store, reach them. A wrong method is
	// sent to the same URL, so the 405 is the one of the write.
	write func(path string) string
	// except are the cases the app cannot answer as Tifa does, with the reason.
	except map[string]string
}

var storeApps = []storeApp{
	{
		name: "start", label: "TanStack Start", write: func(p string) string { return p },
		except: map[string]string{
			"slash": "TanStack Router sends a URL with a trailing slash away with a 307, and /store/ is the index route of the storefront, which sends the visitor to a language",
		},
	},
	{
		name: "nextjs", label: "Next.js", write: nextWrite,
		except: map[string]string{
			"slash body": "Next.js sends a URL with a trailing slash away with its own 308, which has the target as its body and no type, so only the status and Location are compared",
			"404 page":   "Next.js does not write the not-found page of a layout with a dynamic segment (/store/[lang]) into the HTML: it answers 404 with its error document and draws the page in the browser, so only the status and the title are compared (a confirmation that is not found, raised by the page itself, gets the title of the root, so only its status)",
		},
	},
	{name: "reactrouter", label: "React Router", write: reactRouterWrite},
}

var (
	nextPageWrite   = regexp.MustCompile(`^/store/([^/]+)/(cart|checkout|products/)`)
	reactPageWrite  = regexp.MustCompile(`^/store/([^/]+)/(cart|checkout|products/)`)
	minePath        = regexp.MustCompile(`^/store/[^/]+/mine`)
	orderNumber     = regexp.MustCompile(`SS-[A-Z0-9]{10}`)
	cartCookie      = regexp.MustCompile(`store_cart=[^;]+`)
	isoTime         = regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d)`)
	clockTime       = regexp.MustCompile(`\d{1,2}:\d{2}( [AP]M)?`)
	placedLocation  = regexp.MustCompile(`/store/[a-zA-Z-]+/checkout\?placed=SS-[A-Z0-9]{10}`)
	storeCallHeader = http.Header{"X-Tifa-Call": {"1"}, "Accept": {"application/json"}}
)

func nextWrite(p string) string {
	if minePath.MatchString(p) {
		return p
	}
	return nextPageWrite.ReplaceAllString(p, "/store/compare-write/$1/$2")
}

func reactRouterWrite(p string) string {
	if minePath.MatchString(p) {
		return p
	}
	return reactPageWrite.ReplaceAllString(p, "/store/$1/_write/$2")
}

// mask takes out what differs by the request: the order number and the cart of the visitor, which
// are random, and times, which come from the clock.
func mask(s string) string {
	s = orderNumber.ReplaceAllString(s, "SS-ORDER")
	s = isoTime.ReplaceAllString(s, "TIME")
	return s
}

func maskCookie(s string) string {
	return cartCookie.ReplaceAllStringFunc(s, func(c string) string {
		if c == "store_cart=" {
			return c
		}
		return "store_cart=ID"
	})
}

// maskView does the same to what a reader sees of a page, and to the times of day it shows.
func maskView(v compare.View) compare.View {
	text := make([]string, len(v.Text))
	for i, s := range v.Text {
		text[i] = clockTime.ReplaceAllString(mask(s), "HH:MM")
	}
	v.Text = text
	v.Title = mask(v.Title)
	return v
}

// reply is an answer, as far as a visitor or the code of the browser can tell.
type reply struct {
	status int
	header http.Header
	body   []byte
}

// describe writes what matters of an answer in a form that is the same for two servers that
// behave alike: the status, the headers a client acts on, the cookies without the random cart,
// and the body with JSON in one line and random values masked.
func (r reply) describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "status %d\n", r.status)
	for _, name := range []string{"Location", "X-Tifa-Redirect", "Allow"} {
		if v := r.header.Values(name); len(v) > 0 {
			fmt.Fprintf(&b, "%s: %s\n", name, mask(strings.Join(v, ", ")))
		}
	}
	if v := r.header.Get("Cache-Control"); v == "no-store" {
		b.WriteString("Cache-Control: no-store\n")
	}
	var vary []string
	for _, v := range r.header.Values("Vary") {
		for _, token := range strings.Split(v, ",") {
			if token = strings.TrimSpace(token); strings.EqualFold(token, "X-Tifa-Call") || strings.EqualFold(token, "Accept-Language") {
				vary = append(vary, token)
			}
		}
	}
	sort.Strings(vary)
	if len(vary) > 0 {
		fmt.Fprintf(&b, "Vary: %s\n", strings.Join(vary, ", "))
	}
	if ct, _, _ := strings.Cut(r.header.Get("Content-Type"), ";"); ct != "" {
		fmt.Fprintf(&b, "Content-Type: %s\n", ct)
	}
	var cookies []string
	for _, c := range r.header.Values("Set-Cookie") {
		cookies = append(cookies, "Set-Cookie: "+maskCookie(c))
	}
	sort.Strings(cookies)
	for _, c := range cookies {
		b.WriteString(c + "\n")
	}
	body := bytes.TrimSpace(r.body)
	var compact bytes.Buffer
	if json.Compact(&compact, body) == nil && compact.Len() > 0 {
		body = compact.Bytes()
	}
	b.WriteString(mask(string(body)))
	return b.String()
}

// visitor is a browser without a script in front of one server: it keeps cookies and never
// follows a redirect. write is the server's turn of the URL of a write into its own.
type visitor struct {
	t     *testing.T
	base  string
	write func(string) string
	http  *http.Client
}

func newVisitor(t *testing.T, base string, write func(string) string) *visitor {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &visitor{t: t, base: base, write: write, http: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// request is what a visitor sends.
type request struct {
	method, path string
	body         string
	contentType  string // JSON when there is a body and this is empty
	call         bool   // sent as the generated functions of the browser send it
	header       http.Header
	noJar        bool // the cookies are in header, not the visitor's
}

// do sends a request. A write, and any request that is not a GET, goes to the URL of the server's
// write. The answer is read without compression.
func (v *visitor) do(r request) reply {
	v.t.Helper()
	path := r.path
	if r.method != "GET" {
		path = v.write(path)
	}
	req, err := http.NewRequest(r.method, v.base+path, strings.NewReader(r.body))
	if err != nil {
		v.t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "identity")
	if r.body != "" {
		ct := r.contentType
		if ct == "" {
			ct = "application/json"
		}
		req.Header.Set("Content-Type", ct)
	}
	if r.call {
		for k, val := range storeCallHeader {
			req.Header[k] = val
		}
	}
	for k, val := range r.header {
		req.Header[k] = val
	}
	client := v.http
	if r.noJar {
		client = &http.Client{CheckRedirect: v.http.CheckRedirect}
	}
	res, err := client.Do(req)
	if err != nil {
		v.t.Fatalf("%s %s: %v", r.method, path, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		v.t.Fatal(err)
	}
	return reply{res.StatusCode, res.Header, data}
}

func (v *visitor) get(path string) reply { return v.do(request{method: "GET", path: path}) }

func (v *visitor) view(path string) (int, compare.View) {
	r := v.get(path)
	return r.status, maskView(compare.ParseView(r.body))
}

// pair is a visitor on Tifa and one on the app, who get the same requests.
type pair struct {
	t    *testing.T
	tifa *visitor
	app  *visitor
	spec storeApp
}

func (p *pair) fresh() *pair {
	return &pair{t: p.t, tifa: newVisitor(p.t, p.tifa.base, p.tifa.write), app: newVisitor(p.t, p.app.base, p.app.write), spec: p.spec}
}

// both sends a request to both servers and fails when the answers differ.
func (p *pair) both(name string, r request) (tifa, app reply) {
	p.t.Helper()
	tifa, app = p.tifa.do(r), p.app.do(r)
	if a, b := tifa.describe(), app.describe(); a != b {
		p.t.Errorf("%s: %s %s differs.\nTifa:\n%s\n\n%s:\n%s", name, r.method, r.path, a, p.spec.label, b)
	}
	return tifa, app
}

// views compares a page on both servers: status and what a reader sees. A confirmation page
// {order} is asked for by the number each server made.
func (p *pair) views(name, path string, orders [2]string, want int) {
	p.t.Helper()
	ts, tv := p.tifa.view(strings.ReplaceAll(path, "{order}", orders[0]))
	as, av := p.app.view(strings.ReplaceAll(path, "{order}", orders[1]))
	if ts != want || as != want {
		p.t.Errorf("%s: %s answered %d in Tifa and %d in %s, want %d", name, path, ts, as, p.spec.label, want)
		return
	}
	if want == 404 {
		if why, ok := p.spec.except["404 page"]; ok {
			// A confirmation that is not found is a 404 the page itself raises, which Next.js draws from its root, so it has the title of the root.
			if tv.Title != av.Title && !strings.Contains(path, "placed=") {
				p.t.Errorf("%s: %s: title %q in Tifa, %q in %s", name, path, tv.Title, av.Title, p.spec.label)
			}
			p.t.Logf("exception, %s %s: %s", p.spec.label, path, why)
			return
		}
	}
	if d := compare.Diff(tv, av); d != "" {
		p.t.Errorf("%s: %s differs from Tifa:\n%s", name, path, d)
	}
}

// placedOrder finds the number of the order a checkout placed in its answer.
func placedOrder(r reply) string {
	return orderNumber.FindString(r.header.Get("Location") + string(r.body))
}

// TestStorefront runs the checks of the storefront against each Node app. Tifa runs without its
// render cache, as in the other comparisons, and is started once.
func TestStorefront(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()
	tifa, err := mustTarget(t, env, "tifa-nocache").Start(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer tifa.Stop()

	for _, app := range storeApps {
		t.Run(app.name, func(t *testing.T) {
			server, err := mustTarget(t, env, app.name).Start(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			defer server.Stop()
			p := &pair{t: t, tifa: newVisitor(t, tifa.Base, func(s string) string { return s }), app: newVisitor(t, server.Base, app.write), spec: app}
			t.Run("pages", p.pages)
			t.Run("states", p.states)
			t.Run("scenarios", p.scenarios)
			t.Run("redirects", p.redirects)
			t.Run("other requests", p.otherRequests)
			t.Run("forms", p.forms)
			t.Run("refused bodies", p.refused)
			t.Run("writes with a call", func(t *testing.T) { p.writes(t, true) })
			t.Run("writes without a call", func(t *testing.T) { p.writes(t, false) })
		})
	}
}

// extra404 are pages of the storefront that answer 404 inside it or in the root, besides
// StoreMissing: a URL no page has, in a language, and one below a page.
var extra404 = []string{"/store/zh-CN/no-such-page", "/store/en-US/cart/extra", "/store/zh-CN/products/oak-tray/extra", "/store/zh-CN/journal/theme-css/extra", "/store/en-US/products/extra/more", "/store/zh-CN/checkout?placed=SS-NOSUCHORDER"}

// pages: every page of StorePages, StoreMissing and extra404 shows the same in both.
func (p *pair) pages(t *testing.T) {
	p = p.fresh()
	p.t = t
	for _, page := range compare.StorePages() {
		p.views(page.Name, page.URL, [2]string{}, 200)
	}
	for _, page := range compare.StoreMissing {
		p.views("404 page", page.URL, [2]string{}, 404)
	}
	for _, path := range extra404 {
		p.views("404 page", path, [2]string{}, 404)
	}
}

// states: the cart and the checkout in each cart state of StoreStates, built on each server by
// the writes of the state. The numbers of the orders are the servers' own.
func (p *pair) states(t *testing.T) {
	for _, state := range compare.StoreStates() {
		q := p.fresh()
		q.t = t
		var orders [2]string
		for _, s := range state.Steps {
			r := request{method: s.Method, path: s.URL, body: s.Body}
			a, b := q.tifa.do(r), q.app.do(r)
			if a.status >= 400 || b.status >= 400 {
				t.Fatalf("%s: %s answered %d in Tifa and %d in %s: %s %s", state.Name, s, a.status, b.status, p.spec.label, a.body, b.body)
			}
			if id := placedOrder(a); id != "" {
				orders[0] = id
			}
			if id := placedOrder(b); id != "" {
				orders[1] = id
			}
		}
		for _, page := range state.Pages {
			q.views(state.Name+" "+page.Name, page.URL, orders, 200)
		}
	}
}

// scenarios: the visitors that check the port of the domain against the Go shop, as plain
// requests to both servers. A step that asks for the data of a page has no counterpart here.
func (p *pair) scenarios(t *testing.T) {
	for _, sc := range compare.StoreScenarios() {
		q := p.fresh()
		q.t = t
		var orders [2]string
		for _, st := range sc.Steps {
			if st.Routes != "" {
				continue
			}
			r := request{method: st.Method, path: strings.ReplaceAll(st.URL, "{order}", orders[0]), body: st.Body, contentType: st.ContentType, header: http.Header{}}
			if st.AcceptLanguage != "" {
				r.header.Set("Accept-Language", st.AcceptLanguage)
			}
			if st.Cookie != "" {
				r.header.Set("Cookie", st.Cookie)
				r.noJar = true
			}
			a := q.tifa.do(r)
			r.path = strings.ReplaceAll(st.URL, "{order}", orders[1])
			b := q.app.do(r)
			if id := placedOrder(a); id != "" {
				orders[0] = id
			}
			if id := placedOrder(b); id != "" {
				orders[1] = id
			}
			if x, y := a.describe(), b.describe(); x != y {
				t.Errorf("%s: %s differs.\nTifa:\n%s\n\n%s:\n%s", sc.Name, st, x, p.spec.label, y)
			}
		}
	}
}

// redirects: GET /store goes to the language the browser asks for, and /store/ goes to /store.
func (p *pair) redirects(t *testing.T) {
	q := p.fresh()
	q.t = t
	for _, accept := range []string{"", "en-US,en;q=0.9", "en-GB", "en;q=0.8,zh;q=0.9", "zh-TW,zh;q=0.8", "fr-FR,fr;q=0.9", "ja", "de"} {
		q.both("redirect by language "+accept, request{method: "GET", path: "/store", header: http.Header{"Accept-Language": {accept}}})
	}
	slash := func(name, path string) {
		t.Helper()
		if why, ok := p.spec.except["slash"]; ok {
			t.Logf("exception, %s %s: %s", p.spec.label, path, why)
			return
		}
		if why, ok := p.spec.except["slash body"]; ok {
			a, b := q.tifa.get(path), q.app.get(path)
			if a.status != b.status || a.header.Get("Location") != b.header.Get("Location") {
				t.Errorf("%s: %s: %d %s in Tifa, %d %s in %s", name, path, a.status, a.header.Get("Location"), b.status, b.header.Get("Location"), p.spec.label)
			}
			t.Logf("exception, %s %s: %s", p.spec.label, path, why)
			return
		}
		q.both(name, request{method: "GET", path: path})
	}
	slash("slash", "/store/")
	slash("slash with a query", "/store/?x=1")
	slash("slash below a page", "/store/zh-CN/cart/")
	slash("slash below a language", "/store/en-US/")
	// The workbench shares the app, so what the apps do for a trailing slash is checked there too.
	for _, page := range compare.Pages {
		slash("slash after the workbench "+page.Name, strings.SplitN(page.URL, "?", 2)[0]+"/")
	}
}

// otherRequests: a method a URL does not take is a 405 with Allow, and a few other answers that
// are not a page or a write.
func (p *pair) otherRequests(t *testing.T) {
	q := p.fresh()
	q.t = t
	for _, c := range [][2]string{
		{"POST", "/store"}, {"PUT", "/store"}, {"PATCH", "/store"}, {"DELETE", "/store"},
		{"PUT", "/store/zh-CN/mine"}, {"PATCH", "/store/zh-CN/mine"}, {"DELETE", "/store/en-US/mine"},
		{"PUT", "/store/zh-CN/cart"}, {"DELETE", "/store/zh-CN/cart"},
		{"PUT", "/store/zh-CN/checkout"}, {"PATCH", "/store/zh-CN/checkout"}, {"DELETE", "/store/en-US/checkout"},
		{"PATCH", "/store/zh-CN/products/ceramic-cup"}, {"PUT", "/store/zh-CN/products/ceramic-cup"}, {"DELETE", "/store/zh-CN/products/ceramic-cup"},
	} {
		for _, call := range []bool{false, true} {
			q.both(fmt.Sprintf("wrong method, call %v", call), request{method: c[0], path: c[1], call: call})
		}
	}
	q.views("checkout page with an empty order", "/store/zh-CN/checkout?placed=", [2]string{}, 200)
}

// A valid order for the checkout, as the form sends it.
const (
	cardOrder   = `{"email":"lin@example.com","address":{"name":"Lin Xia","phone":"138 0013 8000","country":"CN","region":"Shanghai","city":"Shanghai","line1":"99 Wukang Road","line2":"","postalCode":"200031"},"delivery":"standard","payment":"card","cardNumber":"4242 4242 4242 4242","cardExpiry":"12/30","cardCvc":"123","billingSame":true,"notes":"","newsletter":false,"terms":true}`
	alipayOrder = `{"email":"linxia@example.com","address":{"name":"林夏","phone":"138 0013 8000","country":"CN","region":"上海市","city":"上海市","line1":"徐汇区武康路 99 号","line2":"2 号楼 301","postalCode":"200031"},"delivery":"standard","payment":"alipay","cardNumber":"","cardExpiry":"","cardCvc":"","billingSame":true,"notes":"","newsletter":false,"terms":true}`
)

// step is one request of the visit that uses every write of the storefront, and the pages to
// compare after it besides the cart, the checkout and mine, which are read after every write.
type step struct {
	name        string
	method      string
	path        string
	body        string
	contentType string
	pages       []string
}

func w(name, method, path, body string, pages ...string) step {
	return step{name: name, method: method, path: path, body: body, pages: pages}
}

const (
	zh = "/store/zh-CN"
	en = "/store/en-US"
)

// storeWrites is the visit: every write, the ways each can fail, and the page after the ones that
// change something.
var storeWrites = []step{
	w("mine of a new visitor", "GET", zh+"/mine", ""),
	w("mine of another language", "GET", en+"/mine", ""),
	w("mine in a language the shop lacks", "GET", "/store/xx-XX/mine", ""),
	w("mark seen", "POST", zh+"/mine", `{"slug":"pour-over-kettle"}`, zh+"/cart"),
	w("mark seen, unknown", "POST", zh+"/mine", `{"slug":"nothing"}`),
	w("mark seen again", "POST", zh+"/mine", `{"slug":"oak-tray"}`),
	w("mark seen, in English", "POST", en+"/mine", `{"slug":"ceramic-cup"}`),
	w("mark seen, bad body", "POST", zh+"/mine", `{"slug":3}`),
	w("mine after seeing", "GET", en+"/mine", ""),
	w("cart page of the demonstration cart", "GET", zh+"/cart", "", zh+"/cart"),
	w("coupon: unknown", "POST", zh+"/cart", `{"code":"NOPE"}`),
	w("quantity: unknown line", "PATCH", zh+"/cart", `{"line":"nothing.red","quantity":1}`),
	w("quantity: too many", "PATCH", zh+"/cart", `{"line":"pour-over-kettle.mist","quantity":11}`),
	w("quantity: below zero", "PATCH", zh+"/cart", `{"line":"ceramic-cup.moss","quantity":-1}`),
	w("quantity: over the limit", "PATCH", zh+"/cart", `{"line":"ceramic-cup.moss","quantity":99}`),
	w("add: unknown colour", "POST", zh+"/products/pour-over-kettle", `{"color":"pink","size":"","quantity":1}`),
	w("add: colour the product lacks", "POST", zh+"/products/linen-apron", `{"color":"ink","size":"M","quantity":1}`),
	w("add: size the product lacks", "POST", zh+"/products/ceramic-cup", `{"color":"moss","size":"XL","quantity":1}`),
	w("add: quantity 0", "POST", zh+"/products/pour-over-kettle", `{"color":"mist","size":"","quantity":0}`),
	w("add: too many", "POST", zh+"/products/linen-apron", `{"color":"oat","size":"M","quantity":11}`),
	w("add: sold out", "POST", zh+"/products/tea-canister", `{"color":"brass","size":"","quantity":1}`),
	w("add: unknown product", "POST", zh+"/products/nothing", `{"color":"mist","size":"","quantity":1}`),
	w("add: bad JSON", "POST", zh+"/products/pour-over-kettle", `{`),
	w("add: field of the wrong type", "POST", zh+"/products/pour-over-kettle", `{"color":"mist","size":"","quantity":"2"}`),
	w("add", "POST", zh+"/products/pour-over-kettle", `{"color":"mist","size":"","quantity":2}`, zh+"/cart", en+"/cart"),
	w("add with a size", "POST", zh+"/products/linen-apron", `{"color":"oat","size":"M","quantity":2}`, zh+"/cart"),
	w("add the same again", "POST", zh+"/products/linen-apron", `{"color":"oat","size":"M","quantity":9}`, zh+"/cart"),
	w("mine after adding", "GET", zh+"/mine", ""),
	w("quantity: bad body", "PATCH", zh+"/cart", `{"line":`),
	w("quantity: field of the wrong type", "PATCH", zh+"/cart", `{"line":"a","quantity":"2"}`),
	w("quantity", "PATCH", zh+"/cart", `{"line":"pour-over-kettle.mist","quantity":5}`, zh+"/cart", zh+"/checkout"),
	w("quantity, in English", "PATCH", en+"/cart", `{"line":"pour-over-kettle.mist","quantity":4}`, en+"/cart"),
	w("quantity: remove", "PATCH", zh+"/cart", `{"line":"linen-apron.oat.M","quantity":0}`, zh+"/cart"),
	w("coupon", "POST", zh+"/cart", `{"code":" shanhai10 "}`, zh+"/cart", en+"/cart"),
	w("second coupon", "POST", zh+"/cart", `{"code":"WELCOME30"}`, zh+"/checkout"),
	w("coupon: bad body", "POST", zh+"/cart", `[]`),
	w("coupon: off", "POST", zh+"/cart", `{"code":""}`, zh+"/cart"),
	w("checkout page", "GET", zh+"/checkout", "", zh+"/checkout"),
	w("checkout: empty form", "POST", zh+"/checkout", `{}`),
	w("checkout: bad email", "POST", zh+"/checkout", strings.Replace(alipayOrder, "linxia@example.com", "not an email", 1)),
	w("checkout: bad card", "POST", zh+"/checkout", strings.Replace(cardOrder, "4242 4242 4242 4242", "4242 4242 4242 4241", 1)),
	w("checkout: bad body", "POST", zh+"/checkout", `{"email":`),
	w("checkout", "POST", zh+"/checkout", cardOrder, "@placed", zh+"/cart", zh+"/checkout"),
	w("checkout again, empty cart", "POST", zh+"/checkout", cardOrder),
	w("cart page, empty", "GET", zh+"/cart", "", zh+"/cart"),
	w("mine, empty cart", "GET", zh+"/mine", ""),
	w("add in English", "POST", en+"/products/ceramic-cup", `{"color":"moss","size":"","quantity":1}`, en+"/cart", en+"/checkout"),
	w("add after the order", "POST", zh+"/products/oak-tray", `{"color":"oat","size":"L","quantity":1}`, zh+"/cart"),
	w("checkout in English", "POST", en+"/checkout", alipayOrder, "@placed", en+"/cart"),
	w("add a last one", "POST", en+"/products/ceramic-cup", `{"color":"clay","size":"","quantity":1}`),
	w("checkout of a cart that has a coupon", "POST", en+"/checkout", cardOrder, "@placed"),
}

// writes sends storeWrites to both servers as one visitor each. With call, the writes carry
// X-Tifa-Call like the generated functions of the browser, which Tifa answers with a result in
// {"result": ...} and a redirect as a 200 with X-Tifa-Redirect. Without it they are plain
// requests: the result as it is and a real 303. After each write the cart, the checkout and
// mine must show the same, and the pages the step names.
func (p *pair) writes(t *testing.T, call bool) {
	q := p.fresh()
	q.t = t
	var orders [2]string
	for _, s := range storeWrites {
		isWrite := s.method != "GET"
		r := request{method: s.method, path: s.path, body: s.body, contentType: s.contentType, call: call && (isWrite || strings.HasSuffix(s.path, "/mine"))}
		if isPage(s) {
			q.views(s.name, s.path, orders, 200)
			continue
		}
		a, b := q.both(s.name, r)
		if id := placedOrder(a); id != "" {
			orders[0] = id
		}
		if id := placedOrder(b); id != "" {
			orders[1] = id
		}
		pages := s.pages
		if isWrite {
			pages = append([]string{zh + "/cart", zh + "/checkout"}, pages...)
		}
		for _, page := range pages {
			if page == "@placed" {
				q.views(s.name+": the confirmation", strings.TrimSuffix(s.path, "/checkout")+"/checkout?placed={order}", orders, 200)
				continue
			}
			q.views(s.name+": page after it", page, orders, 200)
		}
		if isWrite {
			q.both(s.name+": mine afterwards", request{method: "GET", path: zh + "/mine"})
		}
	}
}

// storeWriteOf is the turn of the URL of a write for the server of a target: the same for Tifa,
// the write of the app for the others.
func storeWriteOf(target string) func(string) string {
	for _, app := range storeApps {
		if app.name == target {
			return app.write
		}
	}
	return func(s string) string { return s }
}

// isPage says a step reads a page, which is compared by what a reader sees. A GET of mine is JSON.
func isPage(s step) bool { return s.method == "GET" && !strings.HasSuffix(s.path, "/mine") }

// forms: Tifa reads a form body of a write where the port reads JSON only, so the apps answer
// 415. The write is refused before anything changes.
func (p *pair) forms(t *testing.T) {
	q := p.fresh()
	q.t = t
	const form = "application/x-www-form-urlencoded"
	if a := q.tifa.do(request{method: "POST", path: zh + "/cart", body: "code=SHANHAI10", contentType: form}); a.status != 204 {
		t.Errorf("Tifa answered %d to a coupon sent as a form, want 204: %s", a.status, a.body)
	}
	if b := q.app.do(request{method: "POST", path: zh + "/cart", body: "code=SHANHAI10", contentType: form}); b.status != 415 {
		t.Errorf("%s answered %d to a coupon sent as a form, want 415: %s", p.spec.label, b.status, b.body)
	}
	t.Logf("exception, %s: Tifa reads a form body (204), the port reads JSON only (415)", p.spec.label)
}

// refusedBodies are bodies that Go's JSON decoder (encoding/json/v2, which tifa binds with) refuses
// with a 400, though a looser reader takes them: a number that is not written as an integer or
// does not fit, a name given twice, text after the value, bytes that are not UTF-8. The path is
// the one of the write in Tifa.
var refusedBodies = []step{
	w("quantity 1.0", "POST", zh+"/products/pour-over-kettle", `{"color":"mist","size":"","quantity":1.0}`),
	w("quantity 1e0", "POST", zh+"/products/pour-over-kettle", `{"color":"mist","size":"","quantity":1e0}`),
	w("quantity written twice", "POST", zh+"/products/pour-over-kettle", `{"color":"mist","size":"","quantity":1,"quantity":2}`),
	w("quantity out of int64", "POST", zh+"/products/pour-over-kettle", `{"color":"mist","size":"","quantity":9223372036854775808}`),
	w("quantity below int64", "PATCH", zh+"/cart", `{"line":"pour-over-kettle.mist","quantity":-9223372036854775809}`),
	w("quantity with a fraction", "PATCH", zh+"/cart", `{"line":"pour-over-kettle.mist","quantity":2.5}`),
	w("colour written twice", "POST", zh+"/products/pour-over-kettle", `{"color":"mist","color":"moss","size":"","quantity":1}`),
	w("coupon written twice", "POST", zh+"/cart", `{"code":"SHANHAI10","code":"WELCOME30"}`),
	w("text after the value", "POST", zh+"/cart", `{"code":"SHANHAI10"} x`),
	w("two values", "POST", zh+"/cart", `{"code":"SHANHAI10"}{"code":"WELCOME30"}`),
	w("bytes that are not UTF-8", "POST", zh+"/cart", "{\"code\":\"\xff\"}"),
	w("lone surrogate", "POST", zh+"/cart", `{"code":"\ud800"}`),
	w("leading zero", "POST", zh+"/products/pour-over-kettle", `{"color":"mist","size":"","quantity":01}`),
	w("a name written twice inside the address", "POST", zh+"/checkout", strings.Replace(cardOrder, `"city":"Shanghai"`, `"city":"Shanghai","city":"Beijing"`, 1)),
	w("an unknown name written twice", "POST", zh+"/cart", `{"x":1,"x":2,"code":"SHANHAI10"}`),
}

// refused: each of the bodies is a 400 in Tifa and in the apps, and leaves the cart and the cookies
// as they were.
func (p *pair) refused(t *testing.T) {
	q := p.fresh()
	q.t = t
	add := request{method: "POST", path: zh + "/products/pour-over-kettle", body: `{"color":"mist","size":"","quantity":2}`}
	q.both("a cart to keep", add)
	cart := func(v *visitor) string { return v.do(request{method: "GET", path: zh + "/mine"}).describe() }
	before := [2]string{cart(q.tifa), cart(q.app)}
	for _, s := range refusedBodies {
		for _, call := range []bool{true, false} {
			a, b := q.both(s.name, request{method: s.method, path: s.path, body: s.body, call: call})
			if a.status != 400 {
				t.Errorf("%s: Tifa answered %d, want 400: %s", s.name, a.status, a.body)
			}
			if len(a.header.Values("Set-Cookie")) != len(b.header.Values("Set-Cookie")) {
				t.Errorf("%s: Set-Cookie differs", s.name)
			}
			if after := [2]string{cart(q.tifa), cart(q.app)}; after != before {
				t.Fatalf("%s: a refused write changed the cart.\nTifa before:\n%s\nafter:\n%s\n%s before:\n%s\nafter:\n%s", s.name, before[0], after[0], p.spec.label, before[1], after[1])
			}
		}
	}
}
