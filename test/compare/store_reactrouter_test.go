package compare_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/liustack/tifa/test/compare"
)

// The storefront checks for the React Router app. Like the others they need TIFA_COMPARE=1:
//
//	TIFA_COMPARE=1 go test -count=1 -v -run 'ReactRouterStore' ./test/compare

// orderNumber is the number of a placed order, which is random in each server.
var orderNumber = regexp.MustCompile(`SS-[A-Z0-9]{10}`)

// storeReply is an answer to a request, as far as a visitor or the browser code can tell.
type storeReply struct {
	status int
	header http.Header
	body   []byte
}

// storeVisitor is a browser that has cookies, in front of one server. write turns the URL of a
// handler in the shop into the URL of the same handler in that server.
type storeVisitor struct {
	base  string
	write func(path string) string
	jar   map[string]string
}

func (v *storeVisitor) do(method, path string, call bool, body any, headers map[string]string) (storeReply, error) {
	var reader io.Reader
	if raw, ok := body.([]byte); ok {
		reader = bytes.NewReader(raw)
	} else if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return storeReply{}, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, v.base+path, reader)
	if err != nil {
		return storeReply{}, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if call {
		req.Header.Set("X-Tifa-Call", "1")
		req.Header.Set("Accept", "application/json")
	}
	for k, val := range headers {
		req.Header.Set(k, val)
	}
	var names []string
	for name := range v.jar {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		req.AddCookie(&http.Cookie{Name: name, Value: v.jar[name]})
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return storeReply{}, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return storeReply{}, err
	}
	for _, c := range res.Cookies() {
		if c.MaxAge < 0 {
			delete(v.jar, c.Name)
		} else {
			v.jar[c.Name] = c.Value
		}
	}
	return storeReply{res.StatusCode, res.Header, data}, nil
}

// describe writes what matters of a reply in a form that is the same for two servers that behave
// alike: the status, the headers a client acts on, the cookies without their random values, and
// the body with order numbers masked and JSON in a fixed order.
func (r storeReply) describe(cartIDs ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "status %d\n", r.status)
	for _, name := range []string{"Location", "X-Tifa-Redirect", "Cache-Control", "Vary", "Allow"} {
		if v := r.header.Values(name); len(v) > 0 {
			fmt.Fprintf(&b, "%s: %s\n", name, strings.Join(v, ", "))
		}
	}
	if ct, _, _ := strings.Cut(r.header.Get("Content-Type"), ";"); ct != "" {
		fmt.Fprintf(&b, "Content-Type: %s\n", ct)
	}
	var cookies []string
	for _, line := range r.header.Values("Set-Cookie") {
		for _, id := range cartIDs {
			if id != "" {
				line = strings.ReplaceAll(line, id, "<cart>")
			}
		}
		if strings.HasPrefix(line, "store_cart=") && !strings.HasPrefix(line, "store_cart=;") && !strings.HasPrefix(line, "store_cart=<cart>") {
			line = "store_cart=<cart>" + line[strings.Index(line, ";"):]
		}
		cookies = append(cookies, "Set-Cookie: "+line)
	}
	sort.Strings(cookies)
	for _, c := range cookies {
		b.WriteString(c + "\n")
	}
	body := bytes.TrimSpace(r.body)
	var value any
	if json.Unmarshal(body, &value) == nil && len(body) > 0 {
		pretty, _ := json.MarshalIndent(value, "", " ")
		body = pretty
	}
	b.WriteString(orderNumber.ReplaceAllString(string(body), "SS-<order>"))
	return b.String()
}

// storeServers starts the shop and the React Router app.
func storeServers(t *testing.T, env compare.Env) (tifa, react *compare.Server) {
	t.Helper()
	buildApps(t, env)
	ctx := context.Background()
	var err error
	if tifa, err = mustTarget(t, env, "tifa").Start(ctx, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tifa.Stop)
	if react, err = mustTarget(t, env, "reactrouter").Start(ctx, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(react.Stop)
	return tifa, react
}

// TestReactRouterStorePagesShowTheSameText checks that every storefront page shows the same title
// and text in React Router as in Tifa, and that a URL no page has answers 404 with the same error
// page.
func TestReactRouterStorePagesShowTheSameText(t *testing.T) {
	env := setup(t)
	tifa, react := storeServers(t, env)
	get := func(base, path string) (int, compare.View) {
		v := &storeVisitor{base: base, jar: map[string]string{}}
		r, err := v.do("GET", path, false, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r.status, compare.ParseView(r.body)
	}
	pages := compare.StorePages()
	for _, page := range pages {
		ws, want := get(tifa.Base, page.URL)
		gs, got := get(react.Base, page.URL)
		if ws != 200 || gs != 200 {
			t.Errorf("%s: status %d in Tifa, %d in React Router", page.Name, ws, gs)
			continue
		}
		if diff := compare.Diff(want, got); diff != "" {
			t.Errorf("%s (%s) differs from Tifa:\n%s", page.Name, page.URL, diff)
		}
	}
	for _, missing := range compare.StoreMissing {
		path := missing.URL
		ws, want := get(tifa.Base, path)
		gs, got := get(react.Base, path)
		if ws != 404 || gs != 404 {
			t.Errorf("%s: status %d in Tifa, %d in React Router, want 404", path, ws, gs)
		}
		if diff := compare.Diff(want, got); diff != "" {
			t.Errorf("%s differs from Tifa:\n%s", path, diff)
		}
	}
	t.Logf("%d storefront pages and %d missing URLs compared", len(pages), len(compare.StoreMissing))
}

// TestReactRouterStoreRedirectsByLanguage checks GET /store.
func TestReactRouterStoreRedirectsByLanguage(t *testing.T) {
	env := setup(t)
	tifa, react := storeServers(t, env)
	for _, accept := range []string{"", "en-US,en;q=0.9", "en-GB", "zh-TW,zh;q=0.8", "fr-FR,fr;q=0.9", "de"} {
		var replies [2]string
		for i, base := range []string{tifa.Base, react.Base} {
			v := &storeVisitor{base: base, jar: map[string]string{}}
			r, err := v.do("GET", "/store", false, nil, map[string]string{"Accept-Language": accept})
			if err != nil {
				t.Fatal(err)
			}
			replies[i] = fmt.Sprintf("%d %s", r.status, r.header.Get("Location"))
		}
		if replies[0] != replies[1] {
			t.Errorf("Accept-Language %q:\nTifa:\n%s\nReact Router:\n%s", accept, replies[0], replies[1])
		}
	}
}

// step is one request of a visitor, sent to both servers.
type step struct {
	name   string
	method string
	path   string
	call   bool // sent as the browser's generated functions send it
	body   any
	// pages are storefront pages to read afterwards, to see that the next view shows the change.
	pages []string
}

// TestReactRouterStoreWritesBehaveLikeTifa drives a visitor through every write of the storefront
// on both servers, each with a cookie jar of its own, and compares the answer of each request and
// then the pages that show its effect.
func TestReactRouterStoreWritesBehaveLikeTifa(t *testing.T) {
	env := setup(t)
	tifa, react := storeServers(t, env)
	visitors := storeVisitors(tifa, react)
	order := func(email string) map[string]any {
		return map[string]any{
			"email": email,
			"address": map[string]any{
				"name": "林夏", "phone": "138 0013 8000", "country": "CN", "region": "上海市", "city": "上海市",
				"line1": "徐汇区武康路 99 号", "line2": "2 号楼 301", "postalCode": "200031",
			},
			"delivery": "standard", "payment": "alipay", "cardNumber": "", "cardExpiry": "", "cardCvc": "",
			"billingSame": true, "notes": "", "newsletter": false, "terms": true,
		}
	}
	cartPages := []string{"/store/zh-CN/cart", "/store/zh-CN/checkout", "/store/en-US/cart"}
	steps := []step{
		{name: "mine of a new visitor", method: "GET", path: "/store/zh-CN/mine", call: true},
		{name: "mine without the call header", method: "GET", path: "/store/en-US/mine"},
		{name: "mark a product seen", method: "POST", path: "/store/zh-CN/mine", call: true, body: map[string]any{"slug": "ceramic-cup"}, pages: []string{"/store/zh-CN/cart"}},
		{name: "mark a second product seen", method: "POST", path: "/store/zh-CN/mine", call: true, body: map[string]any{"slug": "oak-tray"}},
		{name: "mark a product that does not exist", method: "POST", path: "/store/zh-CN/mine", call: true, body: map[string]any{"slug": "no-such"}},
		{name: "mine after seeing", method: "GET", path: "/store/en-US/mine", call: true},
		{name: "add to the cart", method: "POST", path: "/store/zh-CN/products/linen-apron", call: true, body: map[string]any{"color": "oat", "size": "M", "quantity": 2}, pages: cartPages},
		{name: "add the same again", method: "POST", path: "/store/zh-CN/products/linen-apron", call: true, body: map[string]any{"color": "oat", "size": "M", "quantity": 9}, pages: cartPages},
		{name: "add with a colour it lacks", method: "POST", path: "/store/zh-CN/products/linen-apron", call: true, body: map[string]any{"color": "ink", "size": "M", "quantity": 1}},
		{name: "add too many", method: "POST", path: "/store/zh-CN/products/linen-apron", call: true, body: map[string]any{"color": "oat", "size": "M", "quantity": 11}},
		{name: "add a product that is sold out", method: "POST", path: "/store/zh-CN/products/tea-canister", call: true, body: map[string]any{"color": "brass", "size": "", "quantity": 1}},
		{name: "add a product that does not exist", method: "POST", path: "/store/zh-CN/products/no-such", call: true, body: map[string]any{"color": "oat", "size": "", "quantity": 1}},
		{name: "add in English", method: "POST", path: "/store/en-US/products/ceramic-cup", call: true, body: map[string]any{"color": "moss", "size": "", "quantity": 1}, pages: cartPages},
		{name: "add without the call header", method: "POST", path: "/store/en-US/products/ceramic-cup", body: map[string]any{"color": "clay", "size": "", "quantity": 1}},
		{name: "set a quantity", method: "PATCH", path: "/store/zh-CN/cart", call: true, body: map[string]any{"line": "linen-apron.oat.M", "quantity": 4}, pages: cartPages},
		{name: "set a quantity above the maximum", method: "PATCH", path: "/store/zh-CN/cart", call: true, body: map[string]any{"line": "linen-apron.oat.M", "quantity": 11}},
		{name: "set a quantity of a line that is not there", method: "PATCH", path: "/store/zh-CN/cart", call: true, body: map[string]any{"line": "no-such.line", "quantity": 1}},
		{name: "set a quantity in English", method: "PATCH", path: "/store/en-US/cart", call: true, body: map[string]any{"line": "ceramic-cup.moss", "quantity": 2}, pages: cartPages},
		{name: "remove a line", method: "PATCH", path: "/store/zh-CN/cart", call: true, body: map[string]any{"line": "ceramic-cup.mist", "quantity": 0}, pages: cartPages},
		{name: "coupon", method: "POST", path: "/store/zh-CN/cart", call: true, body: map[string]any{"code": " shanhai10 "}, pages: cartPages},
		{name: "second coupon", method: "POST", path: "/store/zh-CN/cart", call: true, body: map[string]any{"code": "WELCOME30"}, pages: cartPages},
		{name: "coupon that does not exist", method: "POST", path: "/store/zh-CN/cart", call: true, body: map[string]any{"code": "NOPE"}},
		{name: "take the coupon off", method: "POST", path: "/store/zh-CN/cart", call: true, body: map[string]any{"code": ""}, pages: cartPages},
		{name: "body that is not JSON", method: "POST", path: "/store/zh-CN/cart", call: true, body: []byte("{not json")},
		{name: "wrong method", method: "DELETE", path: "/store/zh-CN/cart", call: true},
		{name: "wrong method on mine", method: "PATCH", path: "/store/zh-CN/mine", call: true, body: map[string]any{}},
		{name: "unknown language", method: "GET", path: "/store/fr/mine", call: true},
		{name: "unknown language write", method: "PATCH", path: "/store/fr/cart", call: true, body: map[string]any{"line": "x", "quantity": 1}},
		{name: "checkout with a bad email", method: "POST", path: "/store/zh-CN/checkout", call: true, body: order("not an email")},
		{name: "checkout with nothing", method: "POST", path: "/store/zh-CN/checkout", call: true, body: map[string]any{}},
		{name: "checkout", method: "POST", path: "/store/zh-CN/checkout", call: true, body: order("linxia@example.com"), pages: cartPages},
		{name: "checkout the empty cart", method: "POST", path: "/store/zh-CN/checkout", call: true, body: order("linxia@example.com")},
		{name: "add after the order", method: "POST", path: "/store/zh-CN/products/oak-tray", call: true, body: map[string]any{"color": "oat", "size": "L", "quantity": 1}, pages: cartPages},
		{name: "checkout without the call header", method: "POST", path: "/store/zh-CN/checkout", body: order("linxia@example.com")},
	}

	var cartIDs [2]string
	for _, s := range steps {
		var replies [2]storeReply
		for i, v := range visitors {
			path := s.path
			if s.method != "GET" || strings.HasSuffix(path, "/mine") || s.call {
				path = v.write(path)
			}
			r, err := v.do(s.method, path, s.call, s.body, nil)
			if err != nil {
				t.Fatalf("%s: %v", s.name, err)
			}
			replies[i] = r
			cartIDs[i] = v.jar["store_cart"]
		}
		a, b := replies[0].describe(cartIDs[0], cartIDs[1]), replies[1].describe(cartIDs[0], cartIDs[1])
		if a != b {
			t.Errorf("%s: the answers differ.\nTifa:\n%s\n\nReact Router:\n%s", s.name, a, b)
		}
		// A redirect names the order that was placed: read the confirmation of each server.
		pages := slices.Clone(s.pages)
		if location := replies[0].header.Get("Location"); location != "" || bytes.Contains(replies[0].body, []byte("placed=")) {
			pages = append(pages, "placed")
		}
		for _, page := range pages {
			var views [2]compare.View
			for i, v := range visitors {
				path := page
				if page == "placed" {
					m := regexp.MustCompile(`/store/[a-zA-Z-]+/checkout\?placed=SS-[A-Z0-9]{10}`).Find(replies[i].body)
					if m == nil {
						m = []byte(replies[i].header.Get("Location"))
					}
					path = string(m)
				}
				r, err := v.do("GET", path, false, nil, nil)
				if err != nil {
					t.Fatalf("%s: %s: %v", s.name, page, err)
				}
				if r.status != 200 {
					t.Errorf("%s: %s answered %d", s.name, path, r.status)
				}
				views[i] = compare.ParseView(orderNumber.ReplaceAll(r.body, []byte("SS-ORDER")))
			}
			if diff := compare.Diff(views[0], views[1]); diff != "" {
				t.Errorf("%s: %s differs from Tifa after it:\n%s", s.name, page, diff)
			}
		}
		// The cart of the visitor, as the header asks for it.
		var mines [2]string
		for i, v := range visitors {
			r, err := v.do("GET", v.write("/store/zh-CN/mine"), true, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			mines[i] = r.describe()
		}
		if mines[0] != mines[1] {
			t.Errorf("%s: mine differs afterwards.\nTifa:\n%s\n\nReact Router:\n%s", s.name, mines[0], mines[1])
		}
	}
}

// TestReactRouterStoreUnknownCartIsRefused checks the order of a visitor whose cart cookie names a
// cart the server does not have, which answers 409 and drops the cookie.
func TestReactRouterStoreUnknownCartIsRefused(t *testing.T) {
	env := setup(t)
	tifa, react := storeServers(t, env)
	var replies [2]string
	for i, base := range []string{tifa.Base, react.Base} {
		v := &storeVisitor{base: base, jar: map[string]string{"store_cart": "forgotten"}}
		path := "/store/zh-CN/checkout"
		if i == 1 {
			path = "/compare-store/zh-CN/checkout"
		}
		r, err := v.do("POST", path, true, map[string]any{
			"email": "linxia@example.com", "delivery": "standard", "payment": "alipay", "billingSame": true, "terms": true,
			"address": map[string]any{"name": "林夏", "phone": "138 0013 8000", "country": "CN", "region": "上海市", "city": "上海市", "line1": "徐汇区武康路 99 号", "postalCode": "200031"},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		replies[i] = r.describe()
		if _, ok := v.jar["store_cart"]; ok {
			t.Errorf("server %d kept the cookie of a cart it does not have", i)
		}
	}
	if replies[0] != replies[1] {
		t.Errorf("Tifa:\n%s\n\nReact Router:\n%s", replies[0], replies[1])
	}
}

// runStep sends a step of the shared scenarios to a visitor. Steps that ask for the data of a page
// (Routes) use the data protocol of Tifa's router and have no counterpart here.
func (v *storeVisitor) runStep(st compare.StoreStep, order string) (storeReply, error) {
	path := strings.ReplaceAll(st.URL, "{order}", order)
	headers := map[string]string{}
	if st.AcceptLanguage != "" {
		headers["Accept-Language"] = st.AcceptLanguage
	}
	if st.Cookie != "" {
		headers["Cookie"] = st.Cookie
	}
	var body any
	if st.Body != "" {
		body = []byte(st.Body)
	}
	if st.ContentType != "" {
		headers["Content-Type"] = st.ContentType
	}
	if st.Method != "GET" || strings.HasSuffix(path, "/mine") {
		path = v.write(path)
	}
	saved := v.jar
	if st.Cookie != "" {
		v.jar = map[string]string{}
	}
	defer func() { v.jar = saved }()
	return v.do(st.Method, path, false, body, headers)
}

func storeVisitors(tifa, react *compare.Server) [2]*storeVisitor {
	return [2]*storeVisitor{
		{base: tifa.Base, write: func(p string) string { return p }, jar: map[string]string{}},
		{base: react.Base, write: func(p string) string { return strings.Replace(p, "/store/", "/compare-store/", 1) }, jar: map[string]string{}},
	}
}

var placedOrder = regexp.MustCompile(`SS-[A-Z0-9]{10}`)

// TestReactRouterStoreScenariosBehaveLikeTifa runs the scenarios that check the port of the
// domain against the Go shop, as plain requests, on both servers.
func TestReactRouterStoreScenariosBehaveLikeTifa(t *testing.T) {
	env := setup(t)
	tifa, react := storeServers(t, env)
	for _, sc := range compare.StoreScenarios() {
		visitors := storeVisitors(tifa, react)
		var orders [2]string
		for _, st := range sc.Steps {
			if st.Routes != "" {
				continue
			}
			var replies [2]string
			for i, v := range visitors {
				r, err := v.runStep(st, orders[i])
				if err != nil {
					t.Fatalf("%s: %s: %v", sc.Name, st, err)
				}
				if m := placedOrder.FindString(r.header.Get("Location")); m != "" {
					orders[i] = m
				}
				replies[i] = r.describe(v.jar["store_cart"])
			}
			if replies[0] != replies[1] {
				t.Errorf("%s: %s: the answers differ.\nTifa:\n%s\n\nReact Router:\n%s", sc.Name, st, replies[0], replies[1])
			}
		}
	}
}

// TestReactRouterStoreStatesShowTheSameText builds each cart state on both servers, then compares
// the pages that depend on the cart.
func TestReactRouterStoreStatesShowTheSameText(t *testing.T) {
	env := setup(t)
	tifa, react := storeServers(t, env)
	for _, state := range compare.StoreStates() {
		visitors := storeVisitors(tifa, react)
		var orders [2]string
		for _, st := range state.Steps {
			for i, v := range visitors {
				r, err := v.runStep(st, orders[i])
				if err != nil {
					t.Fatalf("%s: %s: %v", state.Name, st, err)
				}
				if m := placedOrder.FindString(r.header.Get("Location")); m != "" {
					orders[i] = m
				}
			}
		}
		for _, page := range state.Pages {
			var views [2]compare.View
			for i, v := range visitors {
				r, err := v.do("GET", strings.ReplaceAll(page.URL, "{order}", orders[i]), false, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				if r.status != 200 {
					t.Errorf("%s: %s answered %d", state.Name, page.Name, r.status)
				}
				views[i] = compare.ParseView(placedOrder.ReplaceAll(r.body, []byte("SS-ORDER")))
			}
			if diff := compare.Diff(views[0], views[1]); diff != "" {
				t.Errorf("%s: %s differs from Tifa:\n%s", state.Name, page.Name, diff)
			}
		}
	}
}
