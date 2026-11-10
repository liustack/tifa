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
	"slices"
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
const sidebar = `aria-label="Workspace navigation"`

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

	// The workbench needs the demo account. SHOP_OPEN lets the checks below in, and the sign-in checks
	// run last, on the shop as it is deployed.
	t.Setenv("SHOP_OPEN", "1")
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
	for _, want := range []string{"<title>SH-1042 · Shanhai Workbench</title>", "林夏", "Morning Pour-Over Kettle", "¥428.00", ">山海工作室</h1>", "2</span> to ship", sidebar, "Confirm shipment", "Pick a carrier"} {
		if !strings.Contains(body, want) {
			t.Errorf("order page lacks %s", want)
		}
	}
	// The list is a table that pages in the browser: the rendered page holds the 25 newest
	// orders and the totals, with money and times in English. The data written into the page
	// after it holds the same 25 rows and the total of all 42, and the other rows come from the
	// server when the visitor goes to page 2.
	_, _, list := get(t, base+"/projects/studio")
	rendered, _, _ := strings.Cut(list[strings.Index(list, `id="tifa-root"`):], "<script")
	for _, want := range []string{">SH-1042</a>", ">SH-1018</a>", "¥14,134.00", "October 13 at 14:32"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("order list lacks %s", want)
		}
	}
	if strings.Contains(list, "SH-1017") || !strings.Contains(list, `\"total\":42`) {
		t.Errorf("the order list holds more than its first page, or lacks the total of all orders:\n%s", list)
	}
	// The stats page renders its key numbers. The charts load in the browser only, so the HTML
	// has a placeholder of a fixed height for each, and recharts is in none of the scripts the
	// head loads: the entry and the chunks it preloads.
	status, _, stats := get(t, base+"/projects/studio/stats")
	for _, want := range []string{"<title>Sales stats · 山海工作室 · Shanhai Workbench</title>", "Sales", "¥14,134.00", "95.2%", "Best sellers"} {
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
		for _, text := range []string{"recharts-wrapper", "Order not found", "Sales stats", "All orders shipped"} {
			if strings.Contains(script, text) {
				t.Errorf("%s, which every page loads, holds %s", m[1], text)
			}
		}
	}
	// The order page preloads the chunks of the project Layout and of its route right after the
	// head of the build, so the browser fetches them alongside the entry script. The order route
	// holds its Error. An unknown URL shows its 404 inside the root Layout, which is in the entry,
	// and preloads nothing.
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
	if !ok || !strings.Contains(chunks.String(), "Order not found") {
		t.Errorf("the order page does not preload its route:\n%s", preloads)
	}
	// An unknown project is a 404 of the project Layout, which the root Layout shows, so that page
	// preloads nothing either.
	for _, path := range []string{"/nowhere", "/projects/nope"} {
		if _, _, page := get(t, base+path); !strings.Contains(page, built.Head+"<!--tifa-head-->") {
			t.Errorf("the 404 page %s preloads more than the head:\n%s", path, page)
		}
	}
	// Same data, same ETag: 304. Pages are cached by default: the next request skips the VM.
	if status, _, _ := get(t, detail, "If-None-Match", header.Get("ETag")); status != 304 {
		t.Errorf("If-None-Match: %d", status)
	}
	if _, again, _ := get(t, detail); again.Get("X-Tifa-Render") != "memo" {
		t.Errorf("second request of a page: %s", again.Get("X-Tifa-Render"))
	}

	// The files the order page loads besides its own HTML: the head of the build and its preloads.
	checkCompressed(t, base, dir, built.BuildID, body[strings.Index(body, built.Head):strings.Index(body, "<!--tifa-head-->")])

	// 404 at each level: the order inside the project Layout, the project and an unknown URL
	// inside the root Layout, whose sidebar lists the shops. Each one keeps a title, the last from
	// the root Layout's documentHead.
	if _, _, body := get(t, base+"/nowhere"); !strings.Contains(body, ">周末小店</span>") {
		t.Errorf("the 404 of an unknown URL lists no shops in the sidebar:\n%s", body)
	}
	for _, tc := range []struct {
		path, want  string
		withSidebar bool
	}{
		{"/projects/studio/orders/WK-0201", ">Order not found</div>", true},
		{"/projects/nope", "Page not found", true},
		{"/projects/nope/stats", "Page not found", true},
		{"/nowhere", "Page not found", true},
	} {
		status, _, body := get(t, base+tc.path)
		// The project Layout shows the project name in its heading.
		project := strings.Contains(body, ">山海工作室</h1>")
		if status != 404 || !strings.Contains(body, tc.want) || !strings.Contains(body, "Shanhai Workbench</title>") || strings.Contains(body, sidebar) != tc.withSidebar || project != (tc.path == "/projects/studio/orders/WK-0201") {
			t.Errorf("%s: %d, want 404 with %q, sidebar %v\n%s", tc.path, status, tc.want, tc.withSidebar, body)
		}
	}

	// Shipping: with a carrier and a tracking number, once. The framework refuses a write that a
	// browser sends from another site, in either way a browser says so, before the app sees it. A
	// request with no browser headers, such as curl, is not a browser and reaches the handler.
	order := base + "/projects/studio/orders/SH-1042"
	ship := `{"carrier":"SF","trackingNo":"SF1234567890"}`
	if status, body := send(t, "POST", order, "cross-site", ship); status != 403 || !strings.Contains(body, "cross-origin request detected") {
		t.Errorf("POST from another site: %d %s", status, body)
	}
	if status, _ := send(t, "POST", order, "old-browser-cross-site", ship); status != 403 {
		t.Errorf("POST from another site, by a browser without Sec-Fetch-Site: %d", status)
	}
	if status, _ := send(t, "POST", order, "", `{"carrier":"","trackingNo":"SF12"}`); status != 422 {
		t.Errorf("POST without browser headers: %d", status)
	}
	if status, body := send(t, "POST", order, "same-origin", `{"carrier":"","trackingNo":"SF12"}`); status != 422 || !strings.Contains(body, `"carrier":"Pick a carrier"`) || !strings.Contains(body, `"trackingNo":"The tracking number is 8 to 20 letters or digits"`) {
		t.Errorf("ship without a carrier: %d %s", status, body)
	}
	// The recipient can change until the order ships.
	if status, body := send(t, "PATCH", order, "same-origin", `{"customer":"林夏","phone":"138 0013 8000","address":"上海市徐汇区武康路 99 号"}`); status != 200 || !strings.Contains(body, `"phone":"13800138000"`) {
		t.Errorf("change the recipient: %d %s", status, body)
	}
	if status, body := send(t, "POST", order, "same-origin", ship); status != 200 || !strings.Contains(body, `"carrierName":"SF Express"`) {
		t.Errorf("ship: %d %s", status, body)
	}
	if status, _ := send(t, "POST", order, "same-origin", ship); status != 409 {
		t.Errorf("ship twice: %d", status)
	}
	if status, _ := send(t, "PATCH", order, "same-origin", `{"customer":"林夏","phone":"13800138000","address":"上海市"}`); status != 409 {
		t.Errorf("change the recipient of a shipped order: %d", status)
	}
	// The navigation data of the list now carries the new totals, in one response for all routes.
	status, _, body = get(t, dataURL(base+"/projects/studio", built.BuildID, "111"))
	for _, want := range []string{`"app/projects/project+/layout":{"status":200,"data":{"id":"studio","name":"山海工作室"`, `"pending":1`, `"shipped":41`, `"app/layout":{"status":200`} {
		if status != 200 || !strings.Contains(body, want) {
			t.Errorf("list data after shipping: %d lacks %s\n%s", status, want, body)
		}
	}
	// A page of another build is told which build the server runs, so its browser reloads once for it.
	if status, header, _ := get(t, dataURL(base+"/projects/studio", "old", "010")); status != 409 || header.Get("X-Tifa-Build") != built.BuildID {
		t.Errorf("data request of another build: %d, build %q", status, header.Get("X-Tifa-Build"))
	}
	// So is a handler function of that page, before the handler runs: the order stays as it was.
	callFrom := func(build string) (int, string) {
		req, err := http.NewRequest("POST", order, strings.NewReader(ship))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tifa-Call", "1")
		req.Header.Set("X-Tifa-Build", build)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return res.StatusCode, res.Header.Get("X-Tifa-Build")
	}
	if status, named := callFrom("old"); status != 409 || named != built.BuildID {
		t.Errorf("handler call of another build: %d, build %q", status, named)
	}
	if status, named := callFrom(built.BuildID); status != 409 || named != "" {
		t.Errorf("handler call of this build: %d, build %q (the order shipped already, which is the handler's own 409)", status, named)
	}

	checkStorefront(t, base, built.BuildID)
	checkStorefrontKeepsCustomersApart(t, base)
	checkStorefrontIsNotShared(t, base)
	checkEveryShopPageHitsTheMemo(t, base, nil)
	checkShopModules(t, base)
	checkProductPageCounts(t, base)
	checkOrderPageModules(t, base)
	checkStatsPanels(t, base)
	checkCartAndCheckoutModules(t, base)
	t.Run("sign in", func(t *testing.T) { checkShopSignIn(t, exe, built.BuildID) })
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

// checkStorefront checks the customer side of the shop at /zh-CN/store and /store: pages in both
// languages with their money and dates, search and filters in the query string, Markdown with
// highlighted code, and a cart kept in the server's memory, with its ID in a cookie, through to
// a placed order. buildID is the build of the running program.
func checkStorefront(t *testing.T, base, buildID string) {
	t.Helper()
	for _, tc := range []struct {
		path  string
		wants []string
	}{
		// The search of the header is a plain form, so its action carries the language.
		{"/zh-CN/store", []string{"<title>山海 · 日常器物</title>", "秋天的第一壶茶", `action="/zh-CN/store/products"`}},
		{"/store", []string{`action="/store/products"`}},
		{"/store/products?color=moss&sort=price-asc", []string{"<title>All products · Shanhai</title>", "44 products", `products/mini-journal">Mini Journal</a>`}},
		{"/store/products/pour-over-kettle", []string{"Morning Pour-Over Kettle", "$46.20", "63 reviews", "Order now and get it between"}},
		{"/zh-CN/store/products/pour-over-kettle", []string{"晨间手冲壶", "¥328.00"}},
		{"/zh-CN/store/journal/stock-sync-api", []string{"用开放接口同步库存", "<table", `hljs language-go`, `class="hljs-keyword"`}},
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
	_, _, home := get(t, base+"/zh-CN/store")
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
	// Chinese headings use the system serif fonts, so no Chinese font file is bundled.
	if strings.Contains(sheet, "Noto Serif SC") {
		t.Errorf("stylesheet %s still bundles Noto Serif SC", css[1])
	}
	for _, asset := range []struct{ path, kind string }{{photo[1], "image/avif"}, {font[1], "font/woff2"}} {
		status, header, _ := get(t, base+asset.path)
		if status != 200 || header.Get("Content-Type") != asset.kind || !strings.Contains(header.Get("Cache-Control"), "immutable") {
			t.Errorf("%s: %d %v", asset.path, status, header)
		}
	}
	// Prices are in the rendered order the filters ask for: the cheapest moss product first.
	_, _, list := get(t, base+"/store/products?color=moss&sort=price-asc")
	if a, b := strings.Index(rendered(list), "Dot Grid Notebook"), strings.Index(rendered(list), "Beeswax Wraps"); a < 0 || b < a {
		t.Errorf("moss products by price: notebook at %d, wraps at %d", a, b)
	}
	// Tifa links each page to itself in both languages, and names the default language for a
	// browser that asks for neither.
	for path, versions := range map[string][2]string{
		"/store":                           {"/zh-CN/store", "/store"},
		"/store/products/pour-over-kettle": {"/zh-CN/store/products/pour-over-kettle", "/store/products/pour-over-kettle"},
		"/zh-CN/store/journal":             {"/zh-CN/store/journal", "/store/journal"},
	} {
		_, _, page := get(t, base+path)
		head := page[:strings.Index(page, "</head>")]
		for lang, to := range map[string]string{"zh-CN": versions[0], "en-US": versions[1], "x-default": versions[1]} {
			if want := `<link rel="alternate" hreflang="` + lang + `" href="https://shop.example.com` + to + `">`; !strings.Contains(head, want) {
				t.Errorf("%s: the head lacks %s", path, want)
			}
		}
	}
	// A page that shows an error is no page to link in other languages: a 404 would send search
	// engines to a URL that does not exist, in each language. Its head keeps the language and the
	// title and has no alternates.
	for _, path := range []string{"/store/products/does-not-exist", "/zh-CN/store/journal/nope", "/store/nope", "/store/nope/deeper"} {
		status, _, page := get(t, base+path)
		head := page[:strings.Index(page, "</head>")]
		if status != 404 || strings.Contains(head, `rel="alternate"`) || strings.Contains(head, "hreflang") || !strings.Contains(head, "<title>") {
			t.Errorf("%s: %d, the head of a 404 must have a title and no alternate links:\n%.600s", path, status, head)
		}
	}
	// Tifa writes the language of <html> from the language of the route, also on the 404 pages
	// inside the store Layout. A path no page of the store has is a 404 inside it too, with no page
	// of its own for it. The store Layout in app/store is in layout_.tsx, so the workbench of the
	// root Layout is not around it.
	for _, tc := range []struct {
		path   string
		status int
		lang   string
		text   string
	}{
		{"/store", 200, "en-US", "Free delivery"},
		{"/zh-CN/store", 200, "zh-CN", "秋天的第一壶茶"},
		{"/zh-CN/store/products/nope", 404, "zh-CN", "没有找到这个页面"},
		{"/store/journal/nope", 404, "en-US", "We could not find this page"},
		{"/store/nope/deeper", 404, "en-US", "We could not find this page"},
		{"/store/nope", 404, "en-US", "We could not find this page"},
		{"/zh-CN/store/journal/x/y", 404, "zh-CN", "没有找到这个页面"},
		{"/zh-CN/store/xx", 404, "zh-CN", "没有找到这个页面"},
	} {
		status, _, page := get(t, base+tc.path)
		if start := `<!DOCTYPE html><html data-session="out" lang="` + tc.lang + `"><head>`; status != tc.status || !strings.HasPrefix(page, start) || !strings.Contains(rendered(page), tc.text) || strings.Contains(page, sidebar) {
			t.Errorf("%s: %d, want %d with %s in a document starting %s:\n%.120s", tc.path, status, tc.status, tc.text, start, page)
		}
	}
	// The default language with a prefix goes to the URL without it, with its query.
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if res, err := noFollow.Get(base + "/en-US/store/products?x=1"); err != nil {
		t.Fatal(err)
	} else {
		res.Body.Close()
		if res.StatusCode != 308 || res.Header.Get("Location") != "/store/products?x=1" {
			t.Errorf("/en-US/store/products?x=1: %d %s, want 308 to /store/products?x=1", res.StatusCode, res.Header.Get("Location"))
		}
	}
	// A language the store does not have is a 404 before any page is looked for.
	for _, path := range []string{"/xx/store", "/fr/store/products"} {
		if status, _, _ := get(t, base+path); status != 404 {
			t.Errorf("%s: %d, want 404", path, status)
		}
	}
	// /store is the storefront in the default language and answers the same to every browser:
	// nothing in the answer depends on Accept-Language.
	for _, accept := range []string{"en-GB,en;q=0.9", "zh-CN", ""} {
		_, header, page := get(t, base+"/store", "Accept-Language", accept)
		if !strings.Contains(rendered(page), "Free delivery") || slices.Contains(header.Values("Vary"), "Accept-Language") {
			t.Errorf("/store with Accept-Language %q is not the English storefront for everyone: %v", accept, header)
		}
	}
	// A link from /store to /zh-CN/store loads the Layout again, whose Go function reads the
	// language of the route: the browser's request gets the zh-CN locale.
	status, _, data := get(t, dataURL(base+"/zh-CN/store", buildID, "11"))
	if want := `"app/store/layout":{"status":200,"data":{"locale":{"code":"zh-CN"`; status != 200 || !strings.Contains(data, want) {
		t.Errorf("navigation data of /zh-CN/store: %d, want %s:\n%.300s", status, want, data)
	}
	// The root Layout does not wrap the store, so the store has no data of it: the chain of a page
	// of the store has two Loads and that of a URL of the store that no page has one, and a request
	// that counts the root Layout in has one digit too many.
	for path, layers := range map[string]string{"/zh-CN/store": "111", "/zh-CN/store/nope": "11"} {
		if status, _, data := get(t, dataURL(base+path, buildID, layers)); status != 400 {
			t.Errorf("navigation data of the root Layout at %s: %d, want 400:\n%.300s", path, status, data)
		}
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
	kettle := "/store/products/pour-over-kettle"
	if status, body := send(t, "POST", base+kettle, "same-origin", `{"color":"pink","size":"","quantity":1}`); status != 422 || !strings.Contains(body, `"color":"variant"`) {
		t.Errorf("add a colour the kettle lacks: %d %s", status, body)
	}
	status, header, body := do("POST", kettle, `{"color":"charcoal","size":"","quantity":2}`)
	if status != 200 || !strings.Contains(header.Get("Set-Cookie"), "store_cart=") || body != `{"count":7}` {
		t.Errorf("add to cart: %d %s %v", status, body, header)
	}
	if status, _, body := do("PATCH", "/store/cart", `{"line":"pour-over-kettle.charcoal","quantity":3}`); status != 200 || !strings.Contains(body, `"id":"pour-over-kettle.charcoal","slug":"pour-over-kettle","photo":"pour-over-kettle","name":"Morning Pour-Over Kettle","color":"charcoal","colorName":"Charcoal"`) || !strings.Contains(body, `"count":8,`) {
		t.Errorf("change a quantity: %d %s", status, body)
	}
	if status, _, body := do("POST", "/store/cart", `{"code":"NOPE"}`); status != 422 || !strings.Contains(body, `"code":"coupon"`) {
		t.Errorf("unknown coupon: %d %s", status, body)
	}
	if status, _, _ := do("POST", "/zh-CN/store/cart", `{"code":"welcome30"}`); status != 204 {
		t.Errorf("apply a coupon: %d", status)
	}
	if _, _, page := do("GET", "/zh-CN/store/cart", ""); !strings.Contains(rendered(page), "WELCOME30") || !strings.Contains(rendered(page), "−<!-- -->¥30.00") {
		t.Errorf("cart after the coupon lacks the discount\n%s", rendered(page))
	}
	// The order is checked on the server too, and placing it empties the cart.
	if status, _, body := do("POST", "/zh-CN/store/checkout", `{"email":"x","address":{"name":"","phone":"1","country":"CN","region":"","city":"","line1":"","line2":"","postalCode":"abc"},"delivery":"express","payment":"card","cardNumber":"4242424242424241","cardExpiry":"13/20","cardCvc":"1","billingSame":true,"notes":"","newsletter":false,"terms":false}`); status != 422 ||
		!strings.Contains(body, `"address.postalCode":"postalCode"`) || !strings.Contains(body, `"cardNumber":"card"`) || !strings.Contains(body, `"terms":"terms"`) {
		t.Errorf("checkout with errors: %d %s", status, body)
	}
	status, header, _ = do("POST", "/zh-CN/store/checkout", `{"email":"lin@example.com","address":{"name":"林夏","phone":"138 0013 8000","country":"CN","region":"上海市","city":"上海市","line1":"武康路 99 号","line2":"","postalCode":"200031"},"delivery":"express","payment":"alipay","cardNumber":"","cardExpiry":"","cardCvc":"","billingSame":true,"notes":"","newsletter":false,"terms":true}`)
	placed := header.Get("Location")
	if status != 303 || !strings.HasPrefix(placed, "/zh-CN/store/checkout?placed=SH-") {
		t.Fatalf("place the order: %d %s", status, placed)
	}
	if _, _, page := do("GET", placed, ""); !strings.Contains(rendered(page), "已提交，确认邮件已发往 lin@example.com") {
		t.Errorf("order confirmation:\n%s", rendered(page))
	}
	// The confirmation page is a page of the shop like the others: asked for twice with no write in
	// between, the second answer comes from the render memo. Its delivery dates are those of the day
	// the order was placed.
	secondFromMemo(t, "order confirmation", func() (int, http.Header) {
		status, h, _ := do("GET", placed, "")
		return status, h
	})
	if _, _, page := do("GET", "/zh-CN/store/cart", ""); !strings.Contains(rendered(page), "购物车是空的") {
		t.Errorf("cart after the order:\n%s", rendered(page))
	}
	// A small cart pays for delivery: the summary gives the cheapest delivery with a price, not
	// the free pickup in store.
	if status, _, _ := do("POST", "/zh-CN/store/products/ceramic-cup", `{"color":"moss","size":"","quantity":1}`); status != 200 {
		t.Errorf("add a cup to the empty cart: %d", status)
	}
	if _, _, page := do("GET", "/zh-CN/store/cart", ""); !strings.Contains(rendered(page), "最低 ¥15.00") {
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
	_, _, home := get(t, base+"/zh-CN/store")
	_, _, workbench := get(t, base+"/projects/studio")
	for _, owner := range []string{`\"sidebarOpen\":`, "周末小店", `\"pending\":`} {
		if strings.Contains(home, owner) || !strings.Contains(workbench, owner) {
			t.Errorf("the storefront carries %s, or the workbench lacks it", owner)
		}
	}
	// A page past the last shows the last, from the 145th product on.
	if status, _, list := get(t, base+"/zh-CN/store/products?page=9223372036854775807"); status != 200 || !strings.Contains(rendered(list), "第 145 到") {
		t.Errorf("a page past the last of the list: %d", status)
	}

	// A request that fails makes no cart, and a cart ID the server did not make is not kept.
	lin := newShopper(t, base)
	if status, header, _ := lin.do("PATCH", "/zh-CN/store/cart", `{"line":"nope.mist","quantity":1}`); status != 404 || header.Get("Set-Cookie") != "" {
		t.Errorf("change a line not in the cart: %d %s", status, header.Get("Set-Cookie"))
	}
	forged := &http.Cookie{Name: "store_cart", Value: strings.Repeat("f", 3000)}
	if status, header, _ := lin.do("POST", "/zh-CN/store/products/ceramic-cup", `{"color":"moss","size":"","quantity":1}`, forged); status != 200 ||
		!strings.Contains(header.Get("Set-Cookie"), "store_cart=") || strings.Contains(header.Get("Set-Cookie"), forged.Value) {
		t.Errorf("add with a cart ID the server did not make: %d %.80s", status, header.Get("Set-Cookie"))
	}

	// The English checkout adds up prices in dollars, and the order costs the same: the
	// demonstration cart with 10% off and next-day delivery, which is ¥25 at 7.1 yuan a dollar,
	// rounded up to 10 cents.
	emma := newShopper(t, base)
	if status, _, _ := emma.do("POST", "/store/cart", `{"code":"SHANHAI10"}`); status != 204 {
		t.Fatalf("apply a coupon: %d", status)
	}
	_, _, body := emma.do("PATCH", "/store/cart", `{"line":"ceramic-cup.moss","quantity":2}`)
	var cart struct{ SubtotalCents, DiscountCents int }
	if err := json.Unmarshal([]byte(body), &cart); err != nil {
		t.Fatalf("cart %s: %v", body, err)
	}
	total := cart.SubtotalCents - cart.DiscountCents + 360
	status, header, _ := emma.do("POST", "/store/checkout", `{"email":"emma@example.com","address":{"name":"Emma Walker","phone":"+1 415 555 0134","country":"US","region":"CA","city":"San Francisco","line1":"1 Market Street","line2":"","postalCode":"94105"},"delivery":"express","payment":"alipay","cardNumber":"","cardExpiry":"","cardCvc":"","billingSame":true,"notes":"","newsletter":false,"terms":true}`)
	placed := header.Get("Location")
	if status != 303 || !strings.HasPrefix(placed, "/store/checkout?placed=SH-") {
		t.Fatalf("place the order: %d %s", status, placed)
	}
	want := fmt.Sprintf("$%d.%02d", total/100, total%100)
	if _, _, page := emma.do("GET", placed, ""); !strings.Contains(rendered(page), want+" in all") || !strings.Contains(rendered(page), "emma@example.com") {
		t.Errorf("confirmation lacks %s in all:\n%s", want, rendered(page))
	}
	// The English confirmation is cached like the Chinese one.
	secondFromMemo(t, "English order confirmation", func() (int, http.Header) {
		status, h, _ := emma.do("GET", placed, "")
		return status, h
	})
	// The order keeps the email address, so a long one is refused rather than kept.
	long := strings.Repeat("a", 243) + "@example.com"
	if status, _, body := emma.do("POST", "/store/checkout", `{"email":"`+long+`","address":{"name":"Emma Walker","phone":"+1 415 555 0134","country":"US","region":"CA","city":"San Francisco","line1":"1 Market Street","line2":"","postalCode":"94105"},"delivery":"express","payment":"alipay","cardNumber":"","cardExpiry":"","cardCvc":"","billingSame":true,"notes":"","newsletter":false,"terms":true}`); status != 422 || !strings.Contains(body, `"email":"email"`) {
		t.Errorf("an email address of 255 characters: %d %s", status, body)
	}
	// Only the cart that placed the order sees it. Its email address is on the page.
	for _, other := range []*shopper{lin, newShopper(t, base)} {
		if status, _, page := other.do("GET", placed, ""); status != 404 || strings.Contains(page, "emma@example.com") || !strings.Contains(rendered(page), "We could not find this page") {
			t.Errorf("another visitor opens the order: %d", status)
		}
	}
}

// send sends a JSON write the way a client of the given kind does and returns the status and the
// body of the answer. site is "" for a client that is not a browser, "same-origin" for a page of
// the app, "cross-site" for a page of another site (a current browser sends Sec-Fetch-Site and
// Origin) and "old-browser-cross-site" for one that sends only the Origin.
func send(t *testing.T, method, url, site, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	switch site {
	case "":
	case "same-origin":
		req.Header.Set("Sec-Fetch-Site", site)
		req.Header.Set("Origin", req.URL.Scheme+"://"+req.URL.Host)
	case "cross-site":
		req.Header.Set("Sec-Fetch-Site", site)
		req.Header.Set("Origin", "https://other.example")
	case "old-browser-cross-site":
		req.Header.Set("Origin", "https://other.example")
	default:
		t.Fatalf("unknown site %q", site)
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

// A storefront page whose component throws while it renders answers 500 with the Error of the
// store, and the head of that page is the head of a page that shows an error: the language and the
// title of the language Layout, no links to the page in other languages. The head of the Layout
// ran over a page that rendered, so the links were there before the page was rendered again with
// the error.
func TestShopStorefrontErrorPageHasNoAlternates(t *testing.T) {
	skipUnlessEnabled(t)
	app := copyShop(t)
	write(t, filepath.Join(app, "app", "store", "crash", "page.tsx"), `export default function Crash(): never {
  throw new Error('the page cannot render')
}
`)
	base, _, _ := start(t, buildAt(t, app))
	for lang, prefix := range map[string]string{"en-US": "", "zh-CN": "/zh-CN"} {
		status, _, page := get(t, base+prefix+"/store/crash")
		head := page[:strings.Index(page, "</head>")]
		if status != 500 || !strings.Contains(page, `<html data-session="out" lang="`+lang+`"`) || !strings.Contains(head, "<title>") ||
			strings.Contains(head, `rel="alternate"`) || strings.Contains(head, "hreflang") {
			t.Errorf("%s/store/crash: %d, the head of the 500 must have the language and a title and no alternate links:\n%.600s", prefix, status, head)
		}
		// The store still renders its own pages beside it.
		if status, _, _ := get(t, base+prefix+"/store"); status != 200 {
			t.Errorf("%s/store: %d after the crash", prefix, status)
		}
	}
}

// Every convention violation fails tifa build with the file and line, using the analyzer's fixtures.
func TestBuildReportsViolations(t *testing.T) {
	skipUnlessEnabled(t)
	for _, tc := range []struct{ fixture, want string }{
		{"duppath", "app/items/key+/route.go:1:"},
		{"ongetpage", "app/page/route.go:7:"},
		{"layoutparam", "app/projects/route.go:8:"},
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
		// A failed analysis takes no lock and generates nothing, so the fixture stays clean.
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
	if page, _ := fetch("/projects/studio", "gzip, deflate, br, zstd"); page.Header.Get("Content-Encoding") != "gzip" {
		t.Errorf("order list when zstd is offered too: %v", page.Header)
	}
	if page, body := fetch("/projects/studio", "identity"); page.Header.Get("Content-Encoding") != "" || page.Header.Get("Content-Length") != strconv.Itoa(len(body)) {
		t.Errorf("order list without compression: %v, %d bytes", page.Header, len(body))
	}
	// A hit goes out as the gzip form the cache stored, with its length.
	fetch("/projects/studio/orders/SH-1042", "gzip")
	if order, body := fetch("/projects/studio/orders/SH-1042", "gzip"); order.Header.Get("X-Tifa-Render") != "memo" || order.Header.Get("Content-Encoding") != "gzip" ||
		order.Header.Get("Content-Length") != strconv.Itoa(len(body)) || !strings.Contains(gunzip(body), "SH-1042") {
		t.Errorf("cached order with gzip: %v, %d bytes", order.Header, len(body))
	}
	data, body := fetch(dataURL("/projects/studio", buildID, "111"), "gzip")
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
