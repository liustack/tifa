package compare_test

import (
	"compress/gzip"
	"context"
	"fmt"
	"github.com/liustack/tifa/test/compare"
	"html"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	neturl "net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// What a crawler that runs no JavaScript finds: the URLs a page links to with a plain <a href>.

var hrefRE = regexp.MustCompile(`(?i)<a\s[^>]*?href\s*=\s*(?:"([^"]*)"|'([^']*)')`)

// canonicalURL is a storefront URL as a crawler would take it for the same page: the parameters in
// the order of their names, and the ones that say what the page shows anyway (page 1, the default
// sort) left out.
func canonicalURL(u string) string {
	path, query, _ := strings.Cut(u, "?")
	q, err := url.ParseQuery(query)
	if err != nil {
		return u
	}
	if q.Get("page") == "1" {
		q.Del("page")
	}
	if q.Get("sort") == "featured" {
		q.Del("sort")
	}
	if q.Get("reviews") == "1" {
		q.Del("reviews")
	}
	if len(q) == 0 {
		return strings.TrimSuffix(path, "/")
	}
	return strings.TrimSuffix(path, "/") + "?" + q.Encode()
}

// linksOf are the storefront URLs a page of the document body links to, as the paths and queries
// that a crawler asks for next: a link to another site, a fragment, a mailto or a file is left out.
func linksOf(page, body string) []string {
	base, _ := url.Parse("http://shop.test" + page)
	var out []string
	for _, m := range hrefRE.FindAllStringSubmatch(body, -1) {
		href := html.UnescapeString(m[1] + m[2])
		if strings.HasPrefix(href, "#") {
			continue
		}
		ref, err := url.Parse(href)
		if err != nil {
			continue
		}
		to := base.ResolveReference(ref)
		if to.Host != "shop.test" || !(strings.HasPrefix(to.Path, "/store") || strings.HasPrefix(to.Path, "/zh-CN/store")) {
			continue
		}
		u := to.Path
		if to.RawQuery != "" {
			u += "?" + to.RawQuery
		}
		out = append(out, canonicalURL(u))
	}
	return out
}

// reachableFrom fetches the pages by get, starting at starts and following only the links that lead
// to a URL of the set (a crawler follows every link, and the set is the pages it can be asked to
// fetch), and returns the canonical URLs it reached.
func reachableFrom(starts []string, in map[string]bool, get func(u string) (string, error)) (map[string]bool, error) {
	seen := map[string]bool{}
	queue := slices.Clone(starts)
	for len(queue) > 0 {
		u := canonicalURL(queue[0])
		queue = queue[1:]
		if seen[u] || !in[u] {
			continue
		}
		seen[u] = true
		body, err := get(u)
		if err != nil {
			return nil, err
		}
		queue = append(queue, linksOf(u, body)...)
	}
	return seen, nil
}

// A crawler takes the links of the HTML: a relative link, an escaped ampersand, a link with its
// parameters in another order, page 1 and the default sort spelled out, a fragment and another site.
func TestTheLinksACrawlerFollowsAreTheOnesOfTheDocument(t *testing.T) {
	body := `<a href="/store/products?sort=newest&amp;page=2">next</a><A class="x" HREF='journal'>journal</A>
	<a href="https://example.com/store/products">out</a><a href="#top">top</a><a href="mailto:a@b.c">mail</a>
	<a href="/zh-CN/store/products?page=1&sort=featured">first</a><a href="/projects/studio">staff</a><button data-href="/store/cart">go</button>`
	got := linksOf("/store/products?page=3", body)
	want := []string{"/store/products?page=2&sort=newest", "/store/journal", "/zh-CN/store/products"}
	if !slices.Equal(got, want) {
		t.Errorf("links %v, want %v", got, want)
	}
	if canonicalURL("/store/products?sort=newest&page=2") != canonicalURL("/store/products?page=2&sort=newest") {
		t.Error("the same page in another order of its parameters is another URL")
	}
	// A page that nothing links to is not reached, and one that a page of the set links to is, by
	// its links only.
	pages := map[string]string{
		"/store":            `<a href="/store/products">list</a><a href="/store/products/a">a</a>`,
		"/store/products":   `<a href="/store/products?page=2">2</a><a href="/store/products/b">b</a>`,
		"/store/products/a": `<a href="/store/products/a?reviews=2">r</a>`,
	}
	in := map[string]bool{"/store": true, "/store/products": true, "/store/products?page=2": true, "/store/products/a": true, "/store/products/a?reviews=2": true, "/store/products/b": true, "/store/products?sort=newest": true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, pages[canonicalURL(r.URL.RequestURI())])
	}))
	defer srv.Close()
	reached, err := reachableFrom([]string{"/store"}, in, func(u string) (string, error) { return fetch(srv.URL + u) })
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for u := range in {
		if !reached[u] {
			missing = append(missing, u)
		}
	}
	sort.Strings(missing)
	if want := []string{"/store/products?sort=newest"}; !slices.Equal(missing, want) {
		t.Errorf("not reached: %v, want %v", missing, want)
	}
}

func fetch(u string) (string, error) {
	res, err := http.Get(u)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	return string(b), err
}

// crawlerUA is the User-Agent of the crawlers of these checks: Googlebot, as the servers of the
// frameworks tell a crawler by it.
const crawlerUA = "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.6422.175 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"

// crawlGet asks for a URL as a crawler does: one GET with no cookie and no validator, for a document,
// following no redirect.
func crawlGet(c *http.Client, base, u string) (status int, header http.Header, body string, err error) {
	req, err := http.NewRequest("GET", base+u, nil)
	if err != nil {
		return 0, nil, "", err
	}
	req.Header.Set("User-Agent", crawlerUA)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Encoding", "gzip")
	res, err := c.Do(req)
	if err != nil {
		return 0, nil, "", err
	}
	defer res.Body.Close()
	var r io.Reader = res.Body
	if res.Header.Get("Content-Encoding") == "gzip" {
		zr, err := gzip.NewReader(res.Body)
		if err != nil {
			return 0, nil, "", err
		}
		defer zr.Close()
		r = zr
	}
	b, err := io.ReadAll(r)
	return res.StatusCode, res.Header, string(b), err
}

func newCrawler() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// forEachURL runs f for every URL, 8 at a time.
func forEachURL(urls []string, f func(u string)) {
	next := make(chan string)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range next {
				f(u)
			}
		}()
	}
	for _, u := range urls {
		next <- u
	}
	close(next)
	wg.Wait()
}

// Every URL of the set a crawler fetches (U) answers 200 in all four servers, and every old URL (O)
// answers 404. A crawler is sent the User-Agent of Googlebot, which the servers of the frameworks read.
func TestStorefrontEveryCrawlerURLAnswersWhatItShould(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()
	u, o := compare.CrawlURLs()
	for _, name := range []string{"tifa", "start", "nextjs", "reactrouter"} {
		t.Run(name, func(t *testing.T) {
			server, err := mustTarget(t, env, name).Start(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			defer server.Stop()
			c := newCrawler()
			var mu sync.Mutex
			var bad []string
			// What the answers are made of, for the comparison of the work of the servers: the bytes of the
			// answers, and how many of the old URLs got the bare error document of Next.js (an empty
			// #__next_error__ that the browser fills in) and not a page that the server rendered.
			var bytes, bare [2]int
			check := func(want, set int) func(string) {
				return func(url string) {
					status, _, body, err := crawlGet(c, server.Base, url)
					mu.Lock()
					defer mu.Unlock()
					if err != nil || status != want {
						bad = append(bad, fmt.Sprintf("%s: status %d, want %d (%v)", url, status, want, err))
					}
					bytes[set] += len(body)
					if strings.Contains(body, `id="__next_error__"`) {
						bare[set]++
					}
				}
			}
			forEachURL(u, check(200, 0))
			forEachURL(o, check(404, 1))
			t.Logf("%s: the %d public URLs answer %d bytes on average, and the %d old URLs (404) %d bytes, %d of them the bare error document of Next.js", name, len(u), bytes[0]/len(u), len(o), bytes[1]/len(o), bare[1])
			sort.Strings(bad)
			if len(bad) > 0 {
				t.Errorf("%d of the %d URLs answered another status, the first:\n%s", len(bad), len(u)+len(o), strings.Join(bad[:min(len(bad), 10)], "\n"))
			}
		})
	}
}

// The sizes the URLs of a crawler assume are those of the shop: the page links of each list and the
// review links of each product, as Tifa's HTML has them, go up to the number of pages the catalogue and
// the page sizes give.
func TestStorefrontCrawlerURLsFollowThePagesOfTheShop(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	server, err := mustTarget(t, env, "tifa").Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	c := newCrawler()
	u, _ := compare.CrawlURLs()
	// The most that a page of U links to as the page of a parameter: with its URL without that
	// parameter, the pages of a list or the reviews of a product.
	last := map[string]int{}
	for _, url := range u {
		path, query, _ := strings.Cut(url, "?")
		q, _ := neturl.ParseQuery(query)
		for _, param := range []string{"page", "reviews"} {
			if n, _ := strconv.Atoi(q.Get(param)); n > 0 {
				q.Del(param)
				key := path
				if len(q) > 0 {
					key += "?" + q.Encode()
				}
				last[key+" "+param] = max(last[key+" "+param], n)
			}
		}
	}
	var bad []string
	var mu sync.Mutex
	var firsts []string
	for key := range last {
		firsts = append(firsts, key)
	}
	sort.Strings(firsts)
	forEachURL(firsts, func(key string) {
		url, param, _ := strings.Cut(key, " ")
		status, _, body, err := crawlGet(c, server.Base, url)
		most := 1
		for _, l := range linksOfParam(url, body, param) {
			most = max(most, l)
		}
		if err != nil || status != 200 || most != last[key] {
			mu.Lock()
			bad = append(bad, fmt.Sprintf("%s: the page has links to %s up to %d (status %d, %v), U goes up to %d", url, param, most, status, err, last[key]))
			mu.Unlock()
		}
	})
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("%d of the %d pages differ from U, the first:\n%s", len(bad), len(firsts), strings.Join(bad[:min(len(bad), 10)], "\n"))
	}
	// A page that has no pager (one page of reviews, a list of one page) is not in last: every product
	// has its page in U, and the pages past the last of its reviews are not.
	t.Logf("checked the page links of %d lists and products", len(firsts))
}

// linksOfParam are the numbers a page links to in a query parameter of the URL of the page itself.
func linksOfParam(page, body, param string) []int {
	var out []int
	path, _, _ := strings.Cut(page, "?")
	for _, l := range linksOf(page, body) {
		p, q, _ := strings.Cut(l, "?")
		if p != path {
			continue
		}
		v, _ := neturl.ParseQuery(q)
		if n, err := strconv.Atoi(v.Get(param)); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// A crawler that runs no JavaScript finds a page only by a link, so every URL of U must be reached by the
// links of the HTML from the home of each language: the pages a plain crawler cannot get to (the price
// sorts of the list, which are a form control and not a link) are not in U.
func TestStorefrontEveryCrawlerURLIsReachedByALink(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	server, err := mustTarget(t, env, "tifa").Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	c := newCrawler()
	u, _ := compare.CrawlURLs()
	in := map[string]bool{}
	for _, url := range u {
		in[canonicalURL(url)] = true
	}
	reached, err := reachableFrom([]string{compare.StoreBase("zh-CN"), compare.StoreBase("en-US")}, in, func(url string) (string, error) {
		status, _, body, err := crawlGet(c, server.Base, url)
		if err == nil && status != 200 {
			err = fmt.Errorf("%s answered %d", url, status)
		}
		return body, err
	})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string][]string{}
	for _, url := range u {
		if !reached[canonicalURL(url)] {
			lang, _ := compare.StorePageKind(url)
			k := lang + ", " + kindOf(url)
			kinds[k] = append(kinds[k], url)
		}
	}
	total := 0
	for _, k := range slices.Sorted(maps.Keys(kinds)) {
		total += len(kinds[k])
		t.Logf("%s: %d of U, for example %s", k, len(kinds[k]), strings.Join(kinds[k][:min(3, len(kinds[k]))], ", "))
	}
	t.Logf("U holds %d URLs, a crawler that follows the links of the HTML from the two homes reaches %d of them, and does not reach %d", len(u), len(u)-total, total)
	alone, err := reachableFrom([]string{compare.StoreBase("en-US")}, in, func(url string) (string, error) {
		status, _, body, err := crawlGet(c, server.Base, url)
		if err == nil && status != 200 {
			err = fmt.Errorf("%s answered %d", url, status)
		}
		return body, err
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("from the English home alone it reaches %d of them", len(alone))
	for _, price := range []string{"price-asc", "price-desc"} {
		for _, url := range u {
			if strings.Contains(url, "sort="+price) {
				t.Errorf("%s is in U, and no link of the HTML leads to a price sort", url)
			}
		}
	}
	if total > 0 {
		t.Errorf("%d URLs of U are not reached by the links of the HTML from the homes", total)
	}
}

// kindOf is the kind of a URL of U (crawlKind in the package).
func kindOf(url string) string {
	switch {
	case strings.Contains(url, "?reviews="):
		return "reviews"
	case strings.Contains(url, "/products/"):
		return "product"
	case strings.Contains(url, "/products"):
		switch {
		case strings.Contains(url, "sort="):
			return "list, a sort"
		case strings.Contains(url, "category="):
			return "list, a category"
		}
		return "list, no condition"
	case strings.Contains(url, "/journal/"):
		return "post"
	case strings.Contains(url, "/journal"):
		return "journal"
	}
	return "home"
}
