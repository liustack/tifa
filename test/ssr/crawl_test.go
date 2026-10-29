package ssr_test

// Whether two builds of the shop write the same HTML, byte for byte, for the shop's pages: the page
// of every URL a crawl of the running program reaches. Build the shop twice (such as
// a build before and after a change to the bundler or to slowjs), and crawl each:
//
//	TIFA_SSR_CRAWL=write:/tmp/pages-a TIFA_SSR_CRAWL_BIN=$PWD/examples/shop/bin/shop-a go test -run ShopCrawl -v ./test/ssr
//	TIFA_SSR_CRAWL=compare:/tmp/pages-a TIFA_SSR_CRAWL_BIN=$PWD/examples/shop/bin/shop-b go test -run ShopCrawl -v ./test/ssr
//
// TIFA_SSR_CRAWL_BIN is the program (default bin/shop of examples/shop). A page carries the time its data was
// loaded (the router's "u:" in milliseconds) and the loaders of the shop write the time they ran into some
// pages as RFC 3339 text, so a page that differs only there counts as the same after those times are masked,
// and the run says how many it masked. Any other difference fails the test and
// names the URL.

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// crawlSeeds are the URLs the crawl starts from, among them the pages the shop answers with a 404
// and the ones that name a language or a project it does not have. The crawl follows the links of
// every page it gets.
var crawlSeeds = []string{"/", "/projects/studio", "/projects/weekend", "/projects/studio/orders/SH-1042", "/projects/weekend/orders/WK-0201",
	"/projects/studio/stats", "/projects/weekend/stats", "/projects/nope", "/nothing-here", "/store/en-US", "/store/zh-CN", "/store/xx", "/store/en-US/journal", "/store/zh-CN/journal/stock-sync-api",
	"/store/en-US/products?page=2", "/store/en-US/products?color=moss&sort=price-asc", "/store/zh-CN/products", "/store/en-US/products/pour-over-kettle", "/store/zh-CN/products/pour-over-kettle", "/store/en-US/cart", "/store/en-US/checkout", "/store/zh-CN/cart", "/store/en-US/anything/deep",
	"/store/zh-CN/products/nope", "/store/en-US/journal/nope", "/store/en-US/%70roducts", "/projects/%73tudio"}

const crawlLimit = 160

var (
	crawlHref = regexp.MustCompile(`href="(/(?:store|projects)[^"#]*)"`)
	// crawlStamp matches the times that differ between two runs of the same program: RFC 3339 text
	// from the Go loaders, and the "u:" field of the router's state in the page, the Unix time in
	// milliseconds at which a route's data was loaded.
	crawlStamp = regexp.MustCompile(`20\d\d-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d)|\bu:1\d{12}\b`)
)

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
	page := []byte(`<a href="/store/en-US/products?color=moss&amp;sort=price-asc">x</a><a href="/other">y</a><a href="/projects/studio">z</a>`)
	got := crawlLinks(page)
	if len(got) != 2 || got[0] != "/store/en-US/products?color=moss&sort=price-asc" || got[1] != "/projects/studio" {
		t.Errorf("links = %q", got)
	}
	a := []byte("<p>2026-06-01T08:00:00Z and 2026-06-02T09:10:11.123+08:00</p>{i:\"__root__#\",u:1793200923427,s:1}")
	b := []byte("<p>2026-07-09T01:02:03Z and 2026-07-10T02:03:04.5+08:00</p>{i:\"__root__#\",u:1793200987768,s:1}")
	if v, n := crawlSame(a, b); v != "masked" || n != 3 {
		t.Errorf("same = %s %d", v, n)
	}
	if v, _ := crawlSame(a, a); v != "same" {
		t.Errorf("same = %s", v)
	}
	if v, _ := crawlSame(a, []byte("<p>other</p>")); v != "differ" {
		t.Errorf("same = %s", v)
	}
	if crawlName("/a") == crawlName("/b") || !strings.HasSuffix(crawlName("/a"), ".html") {
		t.Error("crawlName")
	}
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
		res, err := http.Get(base + url)
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
