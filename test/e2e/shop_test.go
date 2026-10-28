package e2e

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// shopDir is the example application. Its node_modules must be installed
// (pnpm install --frozen-lockfile in examples/shop).
func shopDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "examples", "shop")
}

// sidebar is in the root Layout of the shop, so its presence tells which level a 404 rendered at.
const sidebar = `aria-label="工作区导航"`

func TestShopApp(t *testing.T) {
	skipUnlessEnabled(t)
	dir := shopDir(t)
	exe := buildAt(t, dir)
	typecheck(t, dir)

	// go vet and go test pass in the app and do not reach into .tifa.
	for _, args := range [][]string{{"list", "./..."}, {"vet", "./..."}, {"test", "-count=1", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil || strings.Contains(string(out), ".tifa") {
			t.Errorf("go %s in the shop: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	base, _, _ := start(t, exe)
	var built struct{ BuildID, Head string }
	readJSON(t, filepath.Join(dir, ".tifa", "dist", "build.json"), &built)

	// The site root sends visitors to the first project.
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := noFollow.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 302 || res.Header.Get("Location") != "/projects/studio" {
		t.Errorf("GET /: %d %s", res.StatusCode, res.Header.Get("Location"))
	}

	// The order page is in the HTML, rendered in the VM with both Layouts around it.
	detail := base + "/projects/studio/orders/SH-1042"
	status, header, body := get(t, detail)
	if status != 200 || header.Get("X-Tifa-Render") != "vm" {
		t.Errorf("order page: %d %v", status, header)
	}
	for _, want := range []string{"<title>SH-1042 · 山海工作台</title>", "林夏", "晨间手冲壶", "¥428.00", ">山海工作室</h1>", "2</span> 单待发货", sidebar, "确认发货", "选择快递公司"} {
		if !strings.Contains(body, want) {
			t.Errorf("order page lacks %s", want)
		}
	}
	// The list is a table that pages in the browser: the rendered page holds the 25 newest
	// orders and the totals, with money and times in Chinese. The data written into the page
	// after it holds all 42.
	_, _, list := get(t, base+"/projects/studio")
	rendered, _, _ := strings.Cut(list[strings.Index(list, `id="tifa-root"`):], "<script")
	for _, want := range []string{">SH-1042</a>", ">SH-1018</a>", "¥14,134.00", "10月13日 14:32"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("order list lacks %s", want)
		}
	}
	if strings.Contains(rendered, "SH-1017") || !strings.Contains(list, `\"id\":\"SH-1001\"`) {
		t.Errorf("the order list renders other than its first page, or lacks the data of the others:\n%s", list)
	}
	// The stats page renders its key numbers. The charts load in the browser only, so the HTML
	// has a placeholder of a fixed height for each, and recharts is in none of the scripts the
	// head loads: the entry and the chunks it preloads.
	status, _, stats := get(t, base+"/projects/studio/stats")
	for _, want := range []string{"<title>销售统计 · 山海工作室 · 山海工作台</title>", "销售额", "¥14,134.00", "95.2%", "热销商品"} {
		if status != 200 || !strings.Contains(stats, want) {
			t.Errorf("stats page: %d lacks %s", status, want)
		}
	}
	if n := strings.Count(stats, `data-slot="skeleton"`); n != 2 {
		t.Errorf("stats page: %d chart placeholders, want 2", n)
	}
	if strings.Contains(stats, "recharts") {
		t.Error("stats page: recharts rendered on the server")
	}
	// What every page loads is one script, the entry, and the stylesheet. React, which the
	// chunks of the charts import too, is in the entry rather than in a chunk of its own.
	if n := strings.Count(built.Head, ".js\""); n != 1 || strings.Contains(built.Head, "modulepreload") {
		t.Errorf("the head loads %d scripts, want the entry alone: %s", n, built.Head)
	}
	// The order page, the stats page and the project Layout export head. Their components load
	// with their routes too, so none of their texts is in what every page loads.
	for _, m := range headURL.FindAllStringSubmatch(built.Head, -1) {
		_, _, script := get(t, base+m[1])
		if !strings.HasSuffix(m[1], ".js") {
			continue
		}
		for _, text := range []string{"recharts-wrapper", "没有找到这个订单", "销售统计", "订单都已发出"} {
			if strings.Contains(script, text) {
				t.Errorf("%s, which every page loads, holds %s", m[1], text)
			}
		}
	}
	// The order page preloads the chunks of the project Layout and of its route right after the
	// head of the build, so the browser fetches them alongside the entry script. The order route
	// holds its NotFound. An unknown URL shows the root Layout, which is in the entry, and
	// preloads nothing.
	after, ok := strings.CutPrefix(body[strings.Index(body, built.Head)+len(built.Head):], `<link rel="modulepreload" href="`)
	preloads, _, _ := strings.Cut(after, "<!--tifa-head-->")
	var chunks strings.Builder
	for _, m := range headURL.FindAllStringSubmatch(`<link rel="modulepreload" href="`+preloads, -1) {
		if strings.Contains(built.Head, m[1]) {
			t.Errorf("the order page preloads %s, which the head loads", m[1])
		}
		status, _, script := get(t, base+m[1])
		if status != 200 {
			t.Errorf("preload %s: %d", m[1], status)
		}
		chunks.WriteString(script)
	}
	if !ok || !strings.Contains(chunks.String(), "没有找到这个订单") {
		t.Errorf("the order page does not preload its route:\n%s", preloads)
	}
	// An unknown project is a 404 of the project Layout, which the root Layout shows, so that page
	// preloads nothing either.
	for _, path := range []string{"/nowhere", "/projects/nope"} {
		if _, _, page := get(t, base+path); !strings.Contains(page, built.Head+"<!--tifa-head-->") {
			t.Errorf("the 404 page %s preloads more than the head:\n%s", path, page)
		}
	}
	// Same data, same ETag: 304. The order page sets Cache = true: the next request skips the VM.
	if status, _, _ := get(t, detail, "If-None-Match", header.Get("ETag")); status != 304 {
		t.Errorf("If-None-Match: %d", status)
	}
	if _, again, _ := get(t, detail); again.Get("X-Tifa-Render") != "cache" {
		t.Errorf("second request of a Cache = true page: %s", again.Get("X-Tifa-Render"))
	}

	// The files the order page loads besides its own HTML: the head of the build and its preloads.
	checkCompressed(t, base, dir, built.BuildID, body[strings.Index(body, built.Head):strings.Index(body, "<!--tifa-head-->")])

	// 404 at each level: the order inside the project Layout, the project inside the root Layout,
	// an unknown URL on its own. Each one keeps a title, the last from the root Layout's documentHead.
	for _, tc := range []struct {
		path, want  string
		withSidebar bool
	}{
		{"/projects/studio/orders/WK-0201", ">没有找到这个订单</div>", true},
		{"/projects/nope", "没有找到这个页面", true},
		{"/projects/nope/stats", "没有找到这个页面", true},
		{"/nowhere", "没有找到这个页面", false},
	} {
		status, _, body := get(t, base+tc.path)
		// The project Layout shows the project name in its heading.
		project := strings.Contains(body, ">山海工作室</h1>")
		if status != 404 || !strings.Contains(body, tc.want) || !strings.Contains(body, "山海工作台</title>") || strings.Contains(body, sidebar) != tc.withSidebar || project != (tc.path == "/projects/studio/orders/WK-0201") {
			t.Errorf("%s: %d, want 404 with %q, sidebar %v\n%s", tc.path, status, tc.want, tc.withSidebar, body)
		}
	}

	// Shipping: only from a page of this host, with a carrier and a tracking number, once.
	order := base + "/projects/studio/orders/SH-1042"
	ship := `{"carrier":"SF","trackingNo":"SF1234567890"}`
	if status, _ := send(t, "POST", order, "", ship); status != 403 {
		t.Errorf("POST without Origin: %d", status)
	}
	if status, _ := send(t, "POST", order, "http://other.example", ship); status != 403 {
		t.Errorf("POST from another site: %d", status)
	}
	if status, body := send(t, "POST", order, base, `{"carrier":"","trackingNo":"SF12"}`); status != 422 || !strings.Contains(body, `"carrier":"请选择快递公司"`) || !strings.Contains(body, `"trackingNo":"运单号是 8 到 20 位字母或数字"`) {
		t.Errorf("ship without a carrier: %d %s", status, body)
	}
	// The recipient can change until the order ships.
	if status, body := send(t, "PATCH", order, base, `{"customer":"林夏","phone":"138 0013 8000","address":"上海市徐汇区武康路 99 号"}`); status != 200 || !strings.Contains(body, `"phone":"13800138000"`) {
		t.Errorf("change the recipient: %d %s", status, body)
	}
	if status, body := send(t, "POST", order, base, ship); status != 200 || !strings.Contains(body, `"carrierName":"顺丰速运"`) {
		t.Errorf("ship: %d %s", status, body)
	}
	if status, _ := send(t, "POST", order, base, ship); status != 409 {
		t.Errorf("ship twice: %d", status)
	}
	if status, _ := send(t, "PATCH", order, base, `{"customer":"林夏","phone":"13800138000","address":"上海市"}`); status != 409 {
		t.Errorf("change the recipient of a shipped order: %d", status)
	}
	// The navigation data of the list now carries the new totals, in one response for all routes.
	status, _, body = get(t, base+"/projects/studio", "X-Tifa-Route", "app/layout,app/projects/project+/layout,app/projects/project+/route", "X-Tifa-Build", built.BuildID)
	for _, want := range []string{`"app/projects/project+/layout":{"status":200,"data":{"id":"studio","name":"山海工作室"`, `"pending":1`, `"shipped":41`, `"app/layout":{"status":200`} {
		if status != 200 || !strings.Contains(body, want) {
			t.Errorf("list data after shipping: %d lacks %s\n%s", status, want, body)
		}
	}
	if status, _, _ := get(t, base+"/projects/studio", "X-Tifa-Route", "app/projects/project+/layout", "X-Tifa-Build", "old"); status != 409 {
		t.Errorf("data request of another build: %d", status)
	}

	checkStorefront(t, base, built.BuildID)
	checkStorefrontKeepsCustomersApart(t, base)
}

// rendered returns the part of a page React rendered, without the data written after it.
func rendered(page string) string {
	i := strings.Index(page, `id="tifa-root"`)
	if i < 0 {
		return ""
	}
	html, _, _ := strings.Cut(page[i:], "<script")
	return html
}

// checkStorefront checks the customer side of the shop at /store/{lang}: pages in both
// languages with their money and dates, search and filters in the query string, Markdown with
// highlighted code, and a cart kept in the server's memory, with its ID in a cookie, through to
// a placed order. buildID is the build of the running program.
func checkStorefront(t *testing.T, base, buildID string) {
	t.Helper()
	for _, tc := range []struct {
		path  string
		wants []string
	}{
		{"/store/zh-CN", []string{"<title>山海 · 日常器物</title>", "秋天的第一壶茶"}},
		{"/store/en-US/products?color=moss&sort=price-asc", []string{"<title>All products · Shanhai</title>", "11 products", `products/dot-notebook">Dot Grid Notebook</a>`}},
		{"/store/en-US/products/pour-over-kettle", []string{"Morning Pour-Over Kettle", "$46.20", "63 reviews", "Order now and get it between"}},
		{"/store/zh-CN/products/pour-over-kettle", []string{"晨间手冲壶", "¥328.00"}},
		{"/store/zh-CN/journal/stock-sync-api", []string{"用开放接口同步库存", "<table", `hljs language-go`, `class="hljs-keyword"`}},
	} {
		// The title is in the head, the rest in what React rendered.
		status, _, page := get(t, base+tc.path)
		for _, want := range tc.wants {
			in := rendered(page)
			if strings.HasPrefix(want, "<title>") {
				in = page
			}
			if status != 200 || !strings.Contains(in, want) {
				t.Errorf("%s: %d lacks %s", tc.path, status, want)
			}
		}
	}
	// Product photos and the fonts of the stylesheet are files under /assets/ whose names carry a
	// hash of their content, so they are cached for good.
	_, _, home := get(t, base+"/store/zh-CN")
	photo := regexp.MustCompile(`src="(/assets/[^"]+\.avif)"`).FindStringSubmatch(home)
	css := regexp.MustCompile(`href="(/assets/[^"]+\.css)"`).FindStringSubmatch(home)
	if photo == nil || css == nil {
		t.Fatalf("home page has no photo or no stylesheet under /assets/")
	}
	_, _, sheet := get(t, base+css[1])
	font := regexp.MustCompile(`url\((/assets/[^)]+\.woff2)\)`).FindStringSubmatch(sheet)
	if font == nil {
		t.Fatalf("stylesheet %s loads no font under /assets/", css[1])
	}
	for _, asset := range []struct{ path, kind string }{{photo[1], "image/avif"}, {font[1], "font/woff2"}} {
		status, header, _ := get(t, base+asset.path)
		if status != 200 || header.Get("Content-Type") != asset.kind || !strings.Contains(header.Get("Cache-Control"), "immutable") {
			t.Errorf("%s: %d %v", asset.path, status, header)
		}
	}
	// Prices are in the rendered order the filters ask for: the cheapest moss product first.
	_, _, list := get(t, base+"/store/en-US/products?color=moss&sort=price-asc")
	if a, b := strings.Index(rendered(list), "Dot Grid Notebook"), strings.Index(rendered(list), "Beeswax Wraps"); a < 0 || b < a {
		t.Errorf("moss products by price: notebook at %d, wraps at %d", a, b)
	}
	// The head of the language Layout sets the language of <html> from the locale its Go
	// function checked, over the root Layout's zh-CN, also on the 404 pages inside it. A path no
	// page of the store has is a 404 inside it too. A language the store does not have is the
	// root's 404, in the root's language.
	for _, tc := range []struct {
		path   string
		status int
		lang   string
		text   string
	}{
		{"/store/en-US", 200, "en-US", "Free delivery"},
		{"/store/zh-CN", 200, "zh-CN", "秋天的第一壶茶"},
		{"/store/xx", 404, "zh-CN", "没有找到这个页面"},
		{"/store/zh-CN/products/nope", 404, "zh-CN", "没有找到这个页面"},
		{"/store/en-US/journal/nope", 404, "en-US", "We could not find this page"},
		{"/store/en-US/nope/deeper", 404, "en-US", "We could not find this page"},
	} {
		status, _, page := get(t, base+tc.path)
		if start := `<!DOCTYPE html><html lang="` + tc.lang + `"><head>`; status != tc.status || !strings.HasPrefix(page, start) || !strings.Contains(rendered(page), tc.text) {
			t.Errorf("%s: %d, want %d with %s in a document starting %s:\n%.120s", tc.path, status, tc.status, tc.text, start, page)
		}
	}
	// A link from /store/en-US to /store/zh-CN loads the language Layout again, its Go function
	// reads the language: the browser's request gets the zh-CN locale, from which the head sets
	// lang="zh-CN" on <html> (the runtime's head tests check that it does).
	status, _, data := get(t, base+"/store/zh-CN", "X-Tifa-Route", "app/store/lang+/layout,app/store/lang+/route", "X-Tifa-Build", buildID)
	if want := `"app/store/lang+/layout":{"status":200,"data":{"locale":{"code":"zh-CN"`; status != 200 || !strings.Contains(data, want) {
		t.Errorf("navigation data of /store/zh-CN: %d, want %s:\n%.300s", status, want, data)
	}

	// The cart lives in the server's memory, and the first change sets a cookie with its ID.
	// Errors come back field by field.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	do := func(method, path, body string) (int, http.Header, string) {
		t.Helper()
		req, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if method != "GET" {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", base)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer res.Body.Close()
		b, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, res.Header, string(b)
	}
	kettle := "/store/en-US/products/pour-over-kettle"
	if status, body := send(t, "POST", base+kettle, base, `{"color":"pink","size":"","quantity":1}`); status != 422 || !strings.Contains(body, `"color":"variant"`) {
		t.Errorf("add a colour the kettle lacks: %d %s", status, body)
	}
	status, header, body := do("POST", kettle, `{"color":"charcoal","size":"","quantity":2}`)
	if status != 200 || !strings.Contains(header.Get("Set-Cookie"), "store_cart=") || body != `{"count":7}` {
		t.Errorf("add to cart: %d %s %v", status, body, header)
	}
	if status, _, body := do("PATCH", "/store/en-US/cart", `{"line":"pour-over-kettle.charcoal","quantity":3}`); status != 200 || !strings.Contains(body, `"id":"pour-over-kettle.charcoal","slug":"pour-over-kettle","name":"Morning Pour-Over Kettle","color":"charcoal","colorName":"Charcoal"`) || !strings.Contains(body, `"count":8,`) {
		t.Errorf("change a quantity: %d %s", status, body)
	}
	if status, _, body := do("POST", "/store/en-US/cart", `{"code":"NOPE"}`); status != 422 || !strings.Contains(body, `"code":"coupon"`) {
		t.Errorf("unknown coupon: %d %s", status, body)
	}
	if status, _, _ := do("POST", "/store/zh-CN/cart", `{"code":"welcome30"}`); status != 204 {
		t.Errorf("apply a coupon: %d", status)
	}
	if _, _, page := do("GET", "/store/zh-CN/cart", ""); !strings.Contains(rendered(page), "WELCOME30") || !strings.Contains(rendered(page), "−<!-- -->¥30.00") {
		t.Errorf("cart after the coupon lacks the discount\n%s", rendered(page))
	}
	// The order is checked on the server too, and placing it empties the cart.
	if status, _, body := do("POST", "/store/zh-CN/checkout", `{"email":"x","address":{"name":"","phone":"1","country":"CN","region":"","city":"","line1":"","line2":"","postalCode":"abc"},"delivery":"express","payment":"card","cardNumber":"4242424242424241","cardExpiry":"13/20","cardCvc":"1","billingSame":true,"notes":"","newsletter":false,"terms":false}`); status != 422 ||
		!strings.Contains(body, `"address.postalCode":"postalCode"`) || !strings.Contains(body, `"cardNumber":"card"`) || !strings.Contains(body, `"terms":"terms"`) {
		t.Errorf("checkout with errors: %d %s", status, body)
	}
	status, header, _ = do("POST", "/store/zh-CN/checkout", `{"email":"lin@example.com","address":{"name":"林夏","phone":"138 0013 8000","country":"CN","region":"上海市","city":"上海市","line1":"武康路 99 号","line2":"","postalCode":"200031"},"delivery":"express","payment":"alipay","cardNumber":"","cardExpiry":"","cardCvc":"","billingSame":true,"notes":"","newsletter":false,"terms":true}`)
	placed := header.Get("Location")
	if status != 303 || !strings.HasPrefix(placed, "/store/zh-CN/checkout?placed=SS-") {
		t.Fatalf("place the order: %d %s", status, placed)
	}
	if _, _, page := do("GET", placed, ""); !strings.Contains(rendered(page), "已提交，确认邮件已发往 lin@example.com") {
		t.Errorf("order confirmation:\n%s", rendered(page))
	}
	if _, _, page := do("GET", "/store/zh-CN/cart", ""); !strings.Contains(rendered(page), "购物车是空的") {
		t.Errorf("cart after the order:\n%s", rendered(page))
	}
	// A small cart pays for delivery: the summary gives the cheapest delivery with a price, not
	// the free pickup in store.
	if status, _, _ := do("POST", "/store/zh-CN/products/ceramic-cup", `{"color":"moss","size":"","quantity":1}`); status != 200 {
		t.Errorf("add a cup to the empty cart: %d", status)
	}
	if _, _, page := do("GET", "/store/zh-CN/cart", ""); !strings.Contains(rendered(page), "最低 ¥15.00") {
		t.Errorf("delivery of a small cart:\n%s", rendered(page))
	}
}

// shopper is a visitor of the storefront with a cookie jar of their own.
type shopper struct {
	t      *testing.T
	base   string
	client *http.Client
}

func newShopper(t *testing.T, base string) *shopper {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &shopper{t, base, client}
}

// do makes a request from the storefront's own origin, with a JSON body unless it is a GET, and
// returns the status, the headers and the body of the answer. Cookies are extra cookies to
// send.
func (s *shopper) do(method, path, body string, cookies ...*http.Cookie) (int, http.Header, string) {
	s.t.Helper()
	req, err := http.NewRequest(method, s.base+path, strings.NewReader(body))
	if err != nil {
		s.t.Fatal(err)
	}
	if method != "GET" {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", s.base)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	res, err := s.client.Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		s.t.Fatal(err)
	}
	return res.StatusCode, res.Header, string(b)
}

// checkStorefrontKeepsCustomersApart checks what one visitor of the storefront must not reach:
// the workbench's data, another visitor's order, and memory for carts that requests make up.
// It also checks that a placed order costs what the checkout showed, and that query parameters
// out of range still give a page.
func checkStorefrontKeepsCustomersApart(t *testing.T, base string) {
	t.Helper()
	// The page data of the storefront has none of the root Layout's workbench data, which the
	// workbench's pages have, as JSON in a script string.
	_, _, home := get(t, base+"/store/zh-CN")
	_, _, workbench := get(t, base+"/projects/studio")
	for _, owner := range []string{`\"name\":\"店主\"`, "周末小店", `\"pending\":`} {
		if strings.Contains(home, owner) || !strings.Contains(workbench, owner) {
			t.Errorf("the storefront carries %s, or the workbench lacks it", owner)
		}
	}
	// A page past the last shows the last, from the 25th product on.
	if status, _, list := get(t, base+"/store/zh-CN/products?page=9223372036854775807"); status != 200 || !strings.Contains(rendered(list), "第 25 到") {
		t.Errorf("a page past the last of the list: %d", status)
	}

	// A request that fails makes no cart, and a cart ID the server did not make is not kept.
	lin := newShopper(t, base)
	if status, header, _ := lin.do("PATCH", "/store/zh-CN/cart", `{"line":"nope.mist","quantity":1}`); status != 404 || header.Get("Set-Cookie") != "" {
		t.Errorf("change a line not in the cart: %d %s", status, header.Get("Set-Cookie"))
	}
	forged := &http.Cookie{Name: "store_cart", Value: strings.Repeat("f", 3000)}
	if status, header, _ := lin.do("POST", "/store/zh-CN/products/ceramic-cup", `{"color":"moss","size":"","quantity":1}`, forged); status != 200 ||
		!strings.Contains(header.Get("Set-Cookie"), "store_cart=") || strings.Contains(header.Get("Set-Cookie"), forged.Value) {
		t.Errorf("add with a cart ID the server did not make: %d %.80s", status, header.Get("Set-Cookie"))
	}

	// The English checkout adds up prices in dollars, and the order costs the same: the
	// demonstration cart with 10% off and next-day delivery, which is ¥25 at 7.1 yuan a dollar,
	// rounded up to 10 cents.
	emma := newShopper(t, base)
	if status, _, _ := emma.do("POST", "/store/en-US/cart", `{"code":"SHANHAI10"}`); status != 204 {
		t.Fatalf("apply a coupon: %d", status)
	}
	_, _, body := emma.do("PATCH", "/store/en-US/cart", `{"line":"ceramic-cup.moss","quantity":2}`)
	var cart struct{ SubtotalCents, DiscountCents int }
	if err := json.Unmarshal([]byte(body), &cart); err != nil {
		t.Fatalf("cart %s: %v", body, err)
	}
	total := cart.SubtotalCents - cart.DiscountCents + 360
	status, header, _ := emma.do("POST", "/store/en-US/checkout", `{"email":"emma@example.com","address":{"name":"Emma Walker","phone":"+1 415 555 0134","country":"US","region":"CA","city":"San Francisco","line1":"1 Market Street","line2":"","postalCode":"94105"},"delivery":"express","payment":"alipay","cardNumber":"","cardExpiry":"","cardCvc":"","billingSame":true,"notes":"","newsletter":false,"terms":true}`)
	placed := header.Get("Location")
	if status != 303 || !strings.HasPrefix(placed, "/store/en-US/checkout?placed=SS-") {
		t.Fatalf("place the order: %d %s", status, placed)
	}
	want := fmt.Sprintf("$%d.%02d", total/100, total%100)
	if _, _, page := emma.do("GET", placed, ""); !strings.Contains(rendered(page), want+" in all") || !strings.Contains(rendered(page), "emma@example.com") {
		t.Errorf("confirmation lacks %s in all:\n%s", want, rendered(page))
	}
	// The order keeps the email address, so a long one is refused rather than kept.
	long := strings.Repeat("a", 243) + "@example.com"
	if status, _, body := emma.do("POST", "/store/en-US/checkout", `{"email":"`+long+`","address":{"name":"Emma Walker","phone":"+1 415 555 0134","country":"US","region":"CA","city":"San Francisco","line1":"1 Market Street","line2":"","postalCode":"94105"},"delivery":"express","payment":"alipay","cardNumber":"","cardExpiry":"","cardCvc":"","billingSame":true,"notes":"","newsletter":false,"terms":true}`); status != 422 || !strings.Contains(body, `"email":"email"`) {
		t.Errorf("an email address of 255 characters: %d %s", status, body)
	}
	// Only the cart that placed the order sees it. Its email address is on the page.
	for _, other := range []*shopper{lin, newShopper(t, base)} {
		if status, _, page := other.do("GET", placed, ""); status != 404 || strings.Contains(page, "emma@example.com") || !strings.Contains(rendered(page), "We could not find this page") {
			t.Errorf("another visitor opens the order: %d", status)
		}
	}
}

// send makes a request with a JSON body from origin, or with no Origin when it is empty, and
// returns the status and the body of the answer.
func send(t *testing.T, method, url, origin, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return res.StatusCode, string(b)
}

// Static analysis does not run user code: a package of the shop whose init() writes a file
// builds, and the file appears only when the built program runs.
func TestShopBuildDoesNotRunInit(t *testing.T) {
	skipUnlessEnabled(t)
	app := copyShop(t)
	marker := filepath.Join(t.TempDir(), "init-ran")
	write(t, filepath.Join(app, "internal", "sideeffect", "sideeffect.go"), `package sideeffect

import "os"

func init() { os.WriteFile(`+"`"+marker+"`"+`, []byte("init ran"), 0o644) }
`)
	route := filepath.Join(app, "app", "projects", "project+", "route.go")
	replaceIn(t, route, "import (\n", "import (\n\t_ \"github.com/liustack/tifa/examples/shop/internal/sideeffect\"\n")

	exe := buildAt(t, app)
	if _, err := os.Stat(marker); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("tifa build ran init(): stat %s: %v", marker, err)
	}
	// The package is part of the program: running it runs init().
	if out, err := exec.Command(exe, "version").CombinedOutput(); err != nil {
		t.Fatalf("%s version: %v\n%s", exe, err, out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the built program did not run init(): %v", err)
	}
}

// Every convention violation fails tifa build with the file and line, using the analyzer's fixtures.
func TestBuildReportsViolations(t *testing.T) {
	skipUnlessEnabled(t)
	for _, tc := range []struct{ fixture, want string }{
		{"duppath", "app/items/key+/route.go:1:"},
		{"ongetpage", "app/page/route.go:7:"},
		{"layoutparam", "app/projects/layout.go:6:"},
		{"usenonancestor", "app/shop/route.go:11:"},
		{"badtype", "app/page/route.go:56:"},
		{"bindquery", "app/list/route.go:12:"},
	} {
		dir := filepath.Join(repoRoot(t), "internal", "analyze", "testdata", tc.fixture)
		cmd := exec.Command(cli(t), "build", "--dir", dir, "-o", filepath.Join(t.TempDir(), "app"))
		out, err := cmd.CombinedOutput()
		// Each violation is a line that starts with the file and line, relative to the app.
		if err == nil || !strings.HasPrefix(string(out), tc.want) && !strings.Contains(string(out), "\n"+tc.want) {
			t.Errorf("%s: %v, want a failure naming %s\n%s", tc.fixture, err, tc.want, out)
		}
		if _, err := os.Stat(filepath.Join(dir, ".tifa")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: a failed analysis generated .tifa: %v", tc.fixture, err)
		}
	}
}

// copyShop copies the shop's sources into a temporary module that points at this repository
// and shares the shop's node_modules.
func copyShop(t *testing.T) string {
	t.Helper()
	src, dst := shopDir(t), filepath.Join(t.TempDir(), "shop")
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch rel {
			case "node_modules", ".tifa", "dist":
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(src, "node_modules"), filepath.Join(dst, "node_modules")); err != nil {
		t.Fatal(err)
	}
	replaceIn(t, filepath.Join(dst, "go.mod"), "=> ../..", "=> "+repoRoot(t))
	return dst
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func replaceIn(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s lacks %q", path, old)
	}
	write(t, path, strings.Replace(string(data), old, new, 1))
}

// checkCompressed checks that the built shop sends its pages, navigation data and browser bundle
// compressed: pages and data compressed while they are sent, the bundle as the .gz files of the
// build, every one with Vary: Accept-Encoding.
func checkCompressed(t *testing.T, base, dir, buildID, head string) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	fetch := func(path, accept string, header ...string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequest("GET", base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept-Encoding", accept)
		for i := 0; i < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(res.Header.Values("Vary"), ","), "Accept-Encoding") {
			t.Errorf("%s: Vary %q", path, res.Header.Values("Vary"))
		}
		return res, body
	}
	gunzip := func(data []byte) string {
		t.Helper()
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		plain, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return string(plain)
	}

	// The order list is over 200 KB of HTML, which gzip makes a few times smaller.
	_, plain := fetch("/projects/studio", "identity")
	page, body := fetch("/projects/studio", "gzip")
	if page.Header.Get("Content-Encoding") != "gzip" || !strings.HasPrefix(page.Header.Get("ETag"), `W/"`) || len(body) > len(plain)/4 || !strings.Contains(gunzip(body), ">SH-1042</a>") {
		t.Errorf("order list with gzip: %v, %d bytes of %d", page.Header, len(body), len(plain))
	}
	if page, _ := fetch("/projects/studio", "gzip, deflate, br, zstd"); page.Header.Get("Content-Encoding") != "zstd" {
		t.Errorf("order list with zstd: %v", page.Header)
	}
	if page, body := fetch("/projects/studio", "identity"); page.Header.Get("Content-Encoding") != "" || page.Header.Get("Content-Length") != strconv.Itoa(len(body)) {
		t.Errorf("order list without compression: %v, %d bytes", page.Header, len(body))
	}
	// The order page sets Cache = true: a hit goes out as the gzip form the cache stored, with its length.
	fetch("/projects/studio/orders/SH-1042", "gzip")
	if order, body := fetch("/projects/studio/orders/SH-1042", "gzip"); order.Header.Get("X-Tifa-Render") != "cache" || order.Header.Get("Content-Encoding") != "gzip" ||
		order.Header.Get("Content-Length") != strconv.Itoa(len(body)) || !strings.Contains(gunzip(body), "SH-1042") {
		t.Errorf("cached order with gzip: %v, %d bytes", order.Header, len(body))
	}
	data, body := fetch("/projects/studio", "gzip", "X-Tifa-Route", "app/layout,app/projects/project+/layout,app/projects/project+/route", "X-Tifa-Build", buildID)
	if data.Header.Get("Content-Encoding") != "gzip" || !strings.Contains(gunzip(body), `"app/layout":{"status":200`) {
		t.Errorf("navigation data: %v", data.Header)
	}

	// Each file the head loads, the entry script of over 256 KB among them, goes out as its .gz
	// file. A file under 1024 bytes, such as a small chunk the order page preloads, has none and
	// goes out as it is.
	largest, small := 0, 0
	for _, m := range headURL.FindAllStringSubmatch(head, -1) {
		file := filepath.Join(dir, ".tifa", "dist", filepath.FromSlash(m[1]))
		if info, err := os.Stat(file); err == nil && info.Size() < 1024 {
			if _, err := os.Stat(file + ".gz"); !os.IsNotExist(err) {
				t.Errorf("%s of %d bytes has a .gz: %v", m[1], info.Size(), err)
			}
			if res, body := fetch(m[1], "gzip, deflate, br, zstd"); res.Header.Get("Content-Encoding") != "" || int64(len(body)) != info.Size() {
				t.Errorf("%s: %v, %d bytes, want the %d of the file", m[1], res.Header, len(body), info.Size())
			}
			small++
			continue
		}
		want, err := os.ReadFile(file + ".gz")
		if err != nil {
			t.Errorf("%s has no .gz: %v", m[1], err)
			continue
		}
		if info, err := os.Stat(file); err == nil && info.Size() > int64(largest) {
			largest = int(info.Size())
		}
		res, body := fetch(m[1], "gzip, deflate, br, zstd")
		if res.Header.Get("Content-Encoding") != "gzip" || !bytes.Equal(body, want) || !strings.Contains(res.Header.Get("Cache-Control"), "immutable") ||
			!strings.HasPrefix(res.Header.Get("Content-Type"), "text/") || res.Header.Get("Content-Length") != strconv.Itoa(len(want)) {
			t.Errorf("%s: %v, %d bytes, want the %d of its .gz", m[1], res.Header, len(body), len(want))
		}
	}
	if largest < 256<<10 || small == 0 {
		t.Errorf("the largest file the head loads has %d bytes and %d have under 1024, the check wants one over 256 KB and one under 1024 bytes", largest, small)
	}
}
