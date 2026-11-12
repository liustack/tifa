package ssr_test

// Whether two builds of the shop write the same HTML, byte for byte, for the shop's pages: the page
// of every URL a crawl of the running program reaches. Build the shop twice (such as
// a build before and after a change to the bundler or to quickjs), and crawl each:
//
//	TIFA_SSR_CRAWL=write:/tmp/pages-a TIFA_SSR_CRAWL_BIN=$PWD/examples/shop/bin/shop-a go test -run ShopCrawl -v ./test/ssr
//	TIFA_SSR_CRAWL=compare:/tmp/pages-a TIFA_SSR_CRAWL_BIN=$PWD/examples/shop/bin/shop-b go test -run ShopCrawl -v ./test/ssr
//
// TIFA_SSR_CRAWL_BIN is the program (default bin/shop of examples/shop). The loaders of the shop write the
// time they ran into some pages as RFC 3339 text, so a page that differs only there counts as the same after
// those times are masked, and the run says how many it masked. Any other difference fails the test and
// names the URL.
//
// Two more settings are for a change to the shop itself, whose build has other scripts and may send
// other data for pages that must show the same:
//
//   - TIFA_SSR_CRAWL_SHOWN=1 compares what a page shows: the script with the data of the router is left
//     out of both pages, and the names of the scripts and styles of the build are masked.
//   - TIFA_SSR_CRAWL_STOCK=0,1,10,11,500 sets the stock of the products of the storefront before the
//     crawl, through the back office as staff do: the first product of the catalogue gets the first
//     number, the next the second, and so on around the list. One number gives every product that stock.

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// crawlSeeds are the URLs the crawl starts from, among them the pages the shop answers with a 404
// and the ones that name a language or a project it does not have. The crawl follows the links of
// every page it gets.
var crawlSeeds = []string{"/", "/projects/studio", "/projects/weekend", "/projects/studio/orders/SH-1042", "/projects/weekend/orders/WK-0201",
	"/projects/studio/stats", "/projects/weekend/stats", "/projects/nope", "/nothing-here", "/store", "/zh-CN/store", "/zh-CN/store/xx", "/store/journal", "/zh-CN/store/journal/stock-sync-api",
	"/store/products?page=2", "/store/products?color=moss&sort=price-asc", "/zh-CN/store/products", "/store/products/pour-over-kettle", "/zh-CN/store/products/pour-over-kettle", "/store/cart", "/store/checkout", "/zh-CN/store/cart", "/store/anything/deep",
	"/zh-CN/store/products/nope", "/store/journal/nope", "/store/%70roducts", "/projects/%73tudio"}

const crawlLimit = 160

var (
	crawlHref = regexp.MustCompile(`href="(/(?:en-US/store|store|projects)[^"#]*)"`)
	// crawlStamp matches the times that differ between two runs of the same program: RFC 3339 text
	// from the Go loaders, which is data of the app.
	crawlStamp = regexp.MustCompile(`20\d\d-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d)`)
)

// crawlData matches the script the router writes its data into (the results of the Loads and the build
// ID), and crawlBuilt the scripts and styles of the build, whose names carry a hash of their content.
var (
	crawlData  = regexp.MustCompile(`(?s)<script data-tsr-stream-part="[^"]*">.*?</script>`)
	crawlBuilt = regexp.MustCompile(`/assets/[A-Za-z0-9_.-]+\.(js|css)`)
)

// crawlShown is a page with what it shows and nothing of how the build made it: without the data of
// the router, and with the scripts and styles of the build named alike.
func crawlShown(page []byte) []byte {
	return crawlBuilt.ReplaceAll(crawlData.ReplaceAll(page, []byte("<script data-tsr-stream-part></script>")), []byte("/assets/built.$1"))
}

// crawlStock gives the products of the storefront the stock of spec (TIFA_SSR_CRAWL_STOCK): numbers
// with commas between, dealt to the products in the order of the catalogue file, around the list.
// It edits each product in the back office, signed in with cookie.
func crawlStock(t *testing.T, base, cookie, spec string) {
	t.Helper()
	var pieces []int
	for _, s := range strings.Split(spec, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < 0 {
			t.Fatalf("TIFA_SSR_CRAWL_STOCK=%s: %q is not a stock", spec, s)
		}
		pieces = append(pieces, n)
	}
	data, err := os.ReadFile(filepath.Join(shopDir, "app", "store", "products", "catalogue.json"))
	if err != nil {
		t.Fatal(err)
	}
	var products []struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(data, &products); err != nil || len(products) == 0 {
		t.Fatalf("the catalogue of the shop: %d products, %v", len(products), err)
	}
	for i, p := range products {
		body := fmt.Sprintf(`{"stock":%d}`, pieces[i%len(pieces)])
		req, err := http.NewRequest("PATCH", base+"/projects/studio/products/"+p.Slug, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("the stock of %s: status %d", p.Slug, res.StatusCode)
		}
	}
}

// crawlName is the file a URL is kept in.
func crawlName(url string) string { return fmt.Sprintf("%x", sha1.Sum([]byte(url)))[:10] + ".html" }

// crawlLinks returns the links of the shop in a page, with the HTML escape of & undone.
func crawlLinks(page []byte) []string {
	var out []string
	for _, m := range crawlHref.FindAllSubmatch(page, -1) {
		out = append(out, strings.ReplaceAll(string(m[1]), "&amp;", "&"))
	}
	return out
}

// crawlSame says how two pages compare: "same" byte for byte, "masked" when only RFC 3339 times
// differ (and how many there were), or "differ".
func crawlSame(a, b []byte) (verdict string, stamps int) {
	switch {
	case bytes.Equal(a, b):
		return "same", 0
	case bytes.Equal(crawlStamp.ReplaceAll(a, []byte("TS")), crawlStamp.ReplaceAll(b, []byte("TS"))):
		return "masked", len(crawlStamp.FindAll(a, -1))
	}
	return "differ", 0
}

func TestCrawlHelpers(t *testing.T) {
	page := []byte(`<a href="/store/products?color=moss&amp;sort=price-asc">x</a><a href="/other">y</a><a href="/projects/studio">z</a>`)
	got := crawlLinks(page)
	if len(got) != 2 || got[0] != "/store/products?color=moss&sort=price-asc" || got[1] != "/projects/studio" {
		t.Errorf("links = %q", got)
	}
	a := []byte("<p>2026-06-01T08:00:00Z and 2026-06-02T09:10:11.123+08:00</p>{i:\"__root__#\",u:0,s:1}")
	b := []byte("<p>2026-07-09T01:02:03Z and 2026-07-10T02:03:04.5+08:00</p>{i:\"__root__#\",u:0,s:1}")
	if v, n := crawlSame(a, b); v != "masked" || n != 2 {
		t.Errorf("same = %s %d", v, n)
	}
	if v, _ := crawlSame(a, a); v != "same" {
		t.Errorf("same = %s", v)
	}
	if v, _ := crawlSame(a, []byte("<p>other</p>")); v != "differ" {
		t.Errorf("same = %s", v)
	}
	// The time the router stamps a match with is no data of the app: a page that carries one differs.
	stamped := []byte("<p>2026-06-01T08:00:00Z and 2026-06-02T09:10:11.123+08:00</p>{i:\"__root__#\",u:1793200923427,s:1}")
	if v, _ := crawlSame(a, stamped); v != "differ" {
		t.Errorf("same = %s for a page with a load time", v)
	}
	if crawlName("/a") == crawlName("/b") || !strings.HasSuffix(crawlName("/a"), ".html") {
		t.Error("crawlName")
	}
	// What a page shows is the same with other data and other scripts of the build, and not with another
	// text or another photo.
	shown := func(stock, app, text, photo string) []byte {
		return []byte(`<link rel="stylesheet" href="/assets/style-` + app + `.css"><script type="module" src="/assets/app-` + app + `.js"></script><img src="/assets/main-480-` + photo + `.avif"><p>` + text + `</p><script data-tsr-stream-part="">$R[0]={stock:` + stock + `}</script></body>`)
	}
	one := crawlShown(shown("183", "CIwkNFhx", "Ships in 48 hours", "D6mB_FlU"))
	if !bytes.Equal(one, crawlShown(shown("181", "kxp92-pE", "Ships in 48 hours", "D6mB_FlU"))) {
		t.Errorf("other data and other scripts show another page: %s", one)
	}
	if bytes.Equal(one, crawlShown(shown("183", "CIwkNFhx", "Only 9 left", "D6mB_FlU"))) || bytes.Equal(one, crawlShown(shown("183", "CIwkNFhx", "Ships in 48 hours", "BHZRnFk7"))) {
		t.Error("another text or another photo shows the same page")
	}
}

// crawlSignIn signs in as the demo account of the shop, whose workbench needs it, and returns the
// Cookie header of the session. The cookie is Secure, so a cookie jar would not send it over http.
func crawlSignIn(t *testing.T, base string) string {
	t.Helper()
	body := `{"email":"owner@shanhai.example","password":"shanhai-demo"}`
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := noFollow.Post(base+"/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	for _, c := range res.Cookies() {
		if strings.HasSuffix(c.Name, "tifa_session") {
			return c.Name + "=" + c.Value
		}
	}
	t.Fatalf("signing in: %d, no session cookie", res.StatusCode)
	return ""
}

func TestShopCrawl(t *testing.T) {
	mode, dir, ok := strings.Cut(os.Getenv("TIFA_SSR_CRAWL"), ":")
	if !ok || (mode != "write" && mode != "compare") || dir == "" {
		t.Skip("set TIFA_SSR_CRAWL=write:DIR or compare:DIR, with TIFA_SSR_CRAWL_BIN the program")
	}
	exe := os.Getenv("TIFA_SSR_CRAWL_BIN")
	if exe == "" {
		exe = filepath.Join(shopDir, "bin", "shop")
	}
	base := startProgram(t, exe)
	cookie := crawlSignIn(t, base)
	if spec := os.Getenv("TIFA_SSR_CRAWL_STOCK"); spec != "" {
		crawlStock(t, base, cookie, spec)
	}
	shown := os.Getenv("TIFA_SSR_CRAWL_SHOWN") == "1"
	if mode == "write" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	queue := append([]string(nil), crawlSeeds...)
	var same, masked, differ, stamps int
	for len(queue) > 0 && len(seen) < crawlLimit {
		url := queue[0]
		queue = queue[1:]
		if seen[url] {
			continue
		}
		seen[url] = true
		req, err := http.NewRequest("GET", base+url, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Cookie", cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		page := append([]byte(fmt.Sprintf("<!-- %s %d -->\n", url, res.StatusCode)), body...)
		file := filepath.Join(dir, crawlName(url))
		if mode == "write" {
			if err := os.WriteFile(file, page, 0o644); err != nil {
				t.Fatal(err)
			}
		} else {
			want, err := os.ReadFile(file)
			if err != nil {
				t.Errorf("%s: not in %s: %v", url, dir, err)
				differ++
			} else {
				if shown {
					want, page = crawlShown(want), crawlShown(page)
				}
				v, n := crawlSame(want, page)
				switch v {
				case "same":
					same++
				case "masked":
					masked++
					stamps += n
				default:
					differ++
					t.Errorf("%s: the page differs", url)
				}
			}
		}
		for _, l := range crawlLinks(body) {
			if !seen[l] && len(queue) < 400 {
				queue = append(queue, l)
			}
		}
	}
	if mode == "write" {
		t.Logf("%d pages written to %s", len(seen), dir)
		return
	}
	if extra, _ := filepath.Glob(filepath.Join(dir, "*.html")); len(extra) != len(seen) {
		names := make([]string, 0, len(seen))
		for u := range seen {
			names = append(names, u)
		}
		sort.Strings(names)
		t.Errorf("%d pages in %s, %d crawled now", len(extra), dir, len(seen))
	}
	t.Logf("%d pages: %d the same as served, %d the same with %d times masked, %d differ", len(seen), same, masked, stamps, differ)
}
