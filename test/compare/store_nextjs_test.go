package compare_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/liustack/tifa/test/compare"
)

// The checks of the storefront in the Next.js app against Tifa. Each starts the shop as a tifa
// program and the Next.js app, sends both the same requests and compares what comes back.

// storeNextTargets are the servers of the storefront check, Tifa first.
func storeNextTargets(t *testing.T, env compare.Env) []compare.Target {
	t.Helper()
	return []compare.Target{mustTarget(t, env, "tifa"), mustTarget(t, env, "nextjs")}
}

// storeAnswer is what a server answered, with what the check compares.
type storeAnswer struct {
	Status int
	Header http.Header
	Body   []byte
}

// storeVisitor is a browser without a script: it keeps cookies and sends requests to one server.
type storeVisitor struct {
	t    *testing.T
	base string
	node bool // the server is the Next.js app, which takes writes under /compare-write
	http *http.Client
}

func newStoreVisitor(t *testing.T, base string, node bool) *storeVisitor {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &storeVisitor{t: t, base: base, node: node, http: client}
}

// do sends a request. A write goes to the path of its server. Pages are asked for without compression.
func (v *storeVisitor) do(method, path string, body string, header map[string]string) storeAnswer {
	v.t.Helper()
	target := path
	if v.node && method != http.MethodGet && !strings.HasSuffix(path, "/mine") {
		target = "/compare-write" + path
	}
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, v.base+target, reader)
	if err != nil {
		v.t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "identity")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, val := range header {
		if val == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, val)
		}
	}
	res, err := v.http.Do(req)
	if err != nil {
		v.t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		v.t.Fatal(err)
	}
	return storeAnswer{res.StatusCode, res.Header, data}
}

func (v *storeVisitor) view(path string) compare.View {
	v.t.Helper()
	a := v.do(http.MethodGet, path, "", nil)
	if a.Status != 200 {
		v.t.Fatalf("GET %s: %d", path, a.Status)
	}
	return compare.ParseView(a.Body)
}

var (
	orderNumber = regexp.MustCompile(`SS-[A-Z0-9]{10}`)
	clockTime   = regexp.MustCompile(`\d{1,2}:\d{2}( [AP]M)?`)
)

// normalized is the part of an answer that must be the same on both servers: the status, the
// location, the type of the body, the cookies with the random cart number taken out, and the
// body as JSON with order numbers taken out.
func (a storeAnswer) normalized() map[string]any {
	out := map[string]any{"status": a.Status}
	if loc := a.Header.Get("Location"); loc != "" {
		out["location"] = orderNumber.ReplaceAllString(loc, "SS-ORDER")
	}
	if ct := a.Header.Get("Content-Type"); ct != "" {
		out["type"] = strings.TrimSpace(strings.Split(ct, ";")[0])
	}
	var cookies []string
	for _, c := range a.Header.Values("Set-Cookie") {
		if strings.HasPrefix(c, "store_cart=") && !strings.HasPrefix(c, "store_cart=;") {
			c = "store_cart=<id>" + c[strings.Index(c, ";"):]
		}
		cookies = append(cookies, c)
	}
	if cookies != nil {
		out["cookies"] = cookies
	}
	if v := a.Header.Get("X-Tifa-Redirect"); v != "" {
		out["redirect"] = v
	}
	if strings.Contains(strings.Join(a.Header.Values("Vary"), ","), "X-Tifa-Call") {
		out["vary"] = "X-Tifa-Call"
	}
	if text := orderNumber.ReplaceAllString(string(a.Body), "SS-ORDER"); text != "" {
		var body any
		if json.Unmarshal([]byte(text), &body) == nil {
			out["body"] = body
		} else {
			out["text"] = text
		}
	}
	return out
}

func storeJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// TestStorePagesShowTheSameText checks that the Next.js app answers every page of the storefront with
// the title and text Tifa does, and as many tables, links, SVGs, buttons, photos and the like.
func TestStorePagesShowTheSameText(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()
	pages := compare.StorePages()

	views := map[string][]compare.View{}
	targets := storeNextTargets(t, env)
	for _, target := range targets {
		server, err := target.Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, page := range pages {
			body, err := compare.Fetch(server.Base, page)
			if err != nil {
				t.Errorf("%s: %v", target.Name, err)
				views[target.Name] = append(views[target.Name], compare.View{})
				continue
			}
			views[target.Name] = append(views[target.Name], compare.ParseView(body))
		}
		server.Stop()
	}
	for _, target := range targets[1:] {
		for i, page := range pages {
			if diff := compare.Diff(views["tifa"][i], views[target.Name][i]); diff != "" {
				t.Errorf("%s, %s differs from tifa:\n%s", page.Name, target.Name, diff)
			}
		}
	}
	for i, page := range pages[:4] {
		t.Logf("%-28s %3d text nodes, tifa %d bytes, Next.js %d", page.Name, len(views["tifa"][i].Text), views["tifa"][i].Bytes, views["nextjs"][i].Bytes)
	}
}

// TestStoreMissingPagesAnswer404 checks that a product, a post, a page and a language the shop does not
// have answer 404 with the same title and text on both servers.
func TestStoreMissingPagesAnswer404(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()
	got := map[string][]compare.View{}
	targets := storeNextTargets(t, env)
	for _, target := range targets {
		server, err := target.Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		v := newStoreVisitor(t, server.Base, target.Name != "tifa")
		for _, path := range compare.StoreMissing {
			a := v.do(http.MethodGet, path, "", nil)
			if a.Status != 404 {
				t.Errorf("%s: GET %s answered %d, want 404", target.Name, path, a.Status)
			}
			got[target.Name] = append(got[target.Name], compare.ParseView(a.Body))
		}
		server.Stop()
	}
	for i, path := range compare.StoreMissing {
		if diff := compare.Diff(got["tifa"][i], got["nextjs"][i]); diff != "" {
			t.Errorf("%s differs from tifa:\n%s", path, diff)
		}
	}
}

// TestStoreRedirectsToTheLanguage checks that /store sends a visitor to the language their browser asks
// for, with the same status and the same Vary.
func TestStoreRedirectsToTheLanguage(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()
	for _, target := range storeNextTargets(t, env) {
		server, err := target.Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		v := newStoreVisitor(t, server.Base, target.Name != "tifa")
		for accept, want := range map[string]string{"": "zh-CN", "en-US,en;q=0.9": "en-US", "en;q=0.8,zh;q=0.9": "zh-CN", "fr-FR,fr;q=0.9": "zh-CN", "ja": "zh-CN", "en-GB": "en-US"} {
			a := v.do(http.MethodGet, "/store", "", map[string]string{"Accept-Language": accept})
			if a.Status != http.StatusFound || a.Header.Get("Location") != "/store/"+want {
				t.Errorf("%s: GET /store with Accept-Language %q: %d %q, want 302 /store/%s", target.Name, accept, a.Status, a.Header.Get("Location"), want)
			}
			if !strings.Contains(a.Header.Get("Vary"), "Accept-Language") {
				t.Errorf("%s: the answer for %q does not vary on Accept-Language: %q", target.Name, accept, a.Header.Get("Vary"))
			}
		}
		server.Stop()
	}
}

const validOrder = `{"email":"buyer@example.com","address":{"name":"Lin Xia","phone":"13800000000","country":"CN","region":"Zhejiang","city":"Hangzhou","line1":"1 West Lake Road","line2":"","postalCode":"310000"},"delivery":"standard","payment":"card","cardNumber":"4242424242424242","cardExpiry":"12/30","cardCvc":"123","billingSame":true,"notes":"","newsletter":false,"terms":true}`

// storeStep is one request of the write check and the page views to compare after it.
type storeStep struct {
	name   string
	method string
	path   string
	body   string
	header map[string]string
	pages  []string // views to compare after the request
}

var callHeader = map[string]string{"X-Tifa-Call": "1"}

var storeWriteSteps = []storeStep{
	{name: "mine of a new visitor", method: "GET", path: "/store/zh-CN/mine"},
	{name: "mine of another language", method: "GET", path: "/store/en-US/mine"},
	{name: "mine in a language the shop lacks", method: "GET", path: "/store/xx-XX/mine"},
	{name: "coupon nobody gave out", method: "POST", path: "/store/zh-CN/cart", body: `{"code":"NOPE"}`},
	{name: "a line the cart lacks", method: "PATCH", path: "/store/zh-CN/cart", body: `{"line":"no-such.line","quantity":1}`},
	{name: "quantity over the limit", method: "PATCH", path: "/store/zh-CN/cart", body: `{"line":"ceramic-cup.moss","quantity":99}`},
	{name: "quantity below zero", method: "PATCH", path: "/store/zh-CN/cart", body: `{"line":"ceramic-cup.moss","quantity":-1}`},
	{name: "nothing written yet", method: "GET", path: "/store/zh-CN/cart", pages: []string{"/store/zh-CN/cart", "/store/zh-CN/checkout"}},
	{name: "change a quantity", method: "PATCH", path: "/store/zh-CN/cart", body: `{"line":"ceramic-cup.moss","quantity":5}`, header: callHeader,
		pages: []string{"/store/zh-CN/cart", "/store/zh-CN/checkout"}},
	{name: "mine after it", method: "GET", path: "/store/zh-CN/mine", header: callHeader},
	{name: "add a product with a size", method: "POST", path: "/store/zh-CN/products/linen-apron", body: `{"color":"oat","size":"M","quantity":2}`, pages: []string{"/store/zh-CN/cart"}},
	{name: "add the same again", method: "POST", path: "/store/zh-CN/products/linen-apron", body: `{"color":"oat","size":"M","quantity":2}`, header: callHeader},
	{name: "add a colour the product lacks", method: "POST", path: "/store/zh-CN/products/linen-apron", body: `{"color":"pink","size":"M","quantity":1}`},
	{name: "add a size the product lacks", method: "POST", path: "/store/zh-CN/products/ceramic-cup", body: `{"color":"moss","size":"XL","quantity":1}`},
	{name: "add none", method: "POST", path: "/store/zh-CN/products/ceramic-cup", body: `{"color":"moss","size":"","quantity":0}`},
	{name: "add a product the shop lacks", method: "POST", path: "/store/zh-CN/products/no-such-product", body: `{"color":"moss","size":"","quantity":1}`},
	{name: "coupon in lower case", method: "POST", path: "/store/zh-CN/cart", body: `{"code":" shanhai10 "}`, pages: []string{"/store/zh-CN/cart", "/store/en-US/cart"}},
	{name: "coupon with a call", method: "POST", path: "/store/zh-CN/cart", body: `{"code":"WELCOME30"}`, header: callHeader, pages: []string{"/store/zh-CN/checkout"}},
	{name: "take the coupon off", method: "POST", path: "/store/zh-CN/cart", body: `{"code":""}`, pages: []string{"/store/zh-CN/cart"}},
	{name: "remove a line", method: "PATCH", path: "/store/zh-CN/cart", body: `{"line":"linen-apron.oat.M","quantity":0}`, pages: []string{"/store/zh-CN/cart"}},
	{name: "remember a product", method: "POST", path: "/store/zh-CN/mine", body: `{"slug":"oak-tray"}`, pages: []string{"/store/zh-CN/cart"}},
	{name: "remember another with a call", method: "POST", path: "/store/zh-CN/mine", body: `{"slug":"walnut-pen"}`, header: callHeader},
	{name: "remember one the shop lacks", method: "POST", path: "/store/zh-CN/mine", body: `{"slug":"no-such-product"}`},
	{name: "mine remembers them", method: "GET", path: "/store/zh-CN/mine", pages: []string{"/store/zh-CN/cart", "/store/en-US/cart"}},
	{name: "a body that is not JSON", method: "PATCH", path: "/store/zh-CN/cart", body: `{"line":`},
	{name: "a field of the wrong type", method: "PATCH", path: "/store/zh-CN/cart", body: `{"line":"a","quantity":"2"}`},
	{name: "a body of another type", method: "POST", path: "/store/zh-CN/cart", body: `code=NOPE`, header: map[string]string{"Content-Type": "application/x-www-form-urlencoded"}},
	{name: "an order with nothing filled in", method: "POST", path: "/store/zh-CN/checkout", body: `{}`},
	{name: "an order with a wrong card", method: "POST", path: "/store/zh-CN/checkout", body: strings.Replace(validOrder, "4242424242424242", "4242424242424241", 1)},
	{name: "an order with a call", method: "POST", path: "/store/zh-CN/checkout", body: validOrder, header: callHeader, pages: []string{"@placed", "/store/zh-CN/cart", "/store/zh-CN/checkout"}},
	{name: "the same order again", method: "POST", path: "/store/zh-CN/checkout", body: validOrder},
	{name: "add after the order", method: "POST", path: "/store/en-US/products/ceramic-cup", body: `{"color":"moss","size":"","quantity":1}`, pages: []string{"/store/en-US/cart", "/store/en-US/checkout"}},
	{name: "an order from the other language", method: "POST", path: "/store/en-US/checkout", body: validOrder, pages: []string{"@placed", "/store/en-US/cart"}},
}

// TestStoreWritesMatchTifa sends the same writes, in the same order, to Tifa and to the Next.js app, each with
// a visitor of its own who keeps the cookies, and checks that each answer has the same status, location,
// cookies, type and body, and that the cart and the checkout then show the same text. The writes are the
// ones the storefront makes: add to cart, change a quantity, coupons, remember a product, and checkout.
func TestStoreWritesMatchTifa(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()

	type result struct {
		answers []storeAnswer
		views   [][]compare.View
	}
	results := map[string]*result{}
	targets := storeNextTargets(t, env)
	for _, target := range targets {
		server, err := target.Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		v := newStoreVisitor(t, server.Base, target.Name != "tifa")
		r := &result{}
		results[target.Name] = r
		for _, step := range storeWriteSteps {
			r.answers = append(r.answers, v.do(step.method, step.path, step.body, step.header))
			var views []compare.View
			for _, page := range step.pages {
				if page == "@placed" {
					// The confirmation the order redirected to.
					id := orderNumber.FindString(string(r.answers[len(r.answers)-1].Body) + r.answers[len(r.answers)-1].Header.Get("Location"))
					page = strings.TrimSuffix(step.path, "/checkout") + "/checkout?placed=" + id
				}
				views = append(views, v.view(page))
			}
			r.views = append(r.views, views)
		}
		server.Stop()
	}

	tifa, next := results["tifa"], results["nextjs"]
	for i, step := range storeWriteSteps {
		want, got := tifa.answers[i].normalized(), next.answers[i].normalized()
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s: %s %s\n tifa:    %s\n Next.js: %s", step.name, step.method, step.path, storeJSON(want), storeJSON(got))
		}
		for j, page := range step.pages {
			if diff := compare.Diff(normalizedView(tifa.views[i][j]), normalizedView(next.views[i][j])); diff != "" {
				t.Errorf("%s: %s differs after it:\n%s", step.name, page, diff)
			}
		}
	}

	// The writes did something: the pages after a write differ from the pages before it.
	cart := func(name string) compare.View {
		for i, s := range storeWriteSteps {
			if s.name == name {
				return tifa.views[i][0]
			}
		}
		t.Fatalf("no step %q", name)
		return compare.View{}
	}
	if compare.Diff(cart("nothing written yet"), cart("change a quantity")) == "" {
		t.Error("the cart page is the same after a change of quantity")
	}
	if compare.Diff(cart("remove a line"), cart("remember a product")) != "" {
		t.Error("remembering a product changed the cart")
	}
	for _, step := range []string{"an order with a call"} {
		if a := tifa.answers[indexOfStoreStep(t, step)]; a.Header.Get("X-Tifa-Redirect") != "303" {
			t.Errorf("%s: Tifa answered without X-Tifa-Redirect: %d %v", step, a.Status, a.Header)
		}
	}
	if a := next.answers[indexOfStoreStep(t, "an order with a call")]; !bytes.Contains(a.Body, []byte("/store/zh-CN/checkout?placed=SS-")) {
		t.Errorf("the order did not redirect to its confirmation: %s", a.Body)
	}
}

func indexOfStoreStep(t *testing.T, name string) int {
	t.Helper()
	for i, s := range storeWriteSteps {
		if s.name == name {
			return i
		}
	}
	t.Fatalf("no step %q", name)
	return -1
}

// normalizedView takes what differs by the second out of a view: the order number and the time of day.
func normalizedView(v compare.View) compare.View {
	text := make([]string, len(v.Text))
	for i, s := range v.Text {
		s = orderNumber.ReplaceAllString(s, "SS-ORDER")
		text[i] = clockTime.ReplaceAllString(s, "HH:MM")
	}
	v.Text = text
	v.Title = orderNumber.ReplaceAllString(v.Title, "SS-ORDER")
	return v
}
