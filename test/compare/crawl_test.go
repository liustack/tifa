package compare

import (
	"encoding/json/v2"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// U is worked out from the catalogue file in this test again, without the code under test: the
// pages of the list, of each category and of each sort, every product with the pages of its reviews,
// the journal and its posts, in both languages.
func TestTheCrawlerURLsFollowTheCatalogueAndThePageSizes(t *testing.T) {
	env, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(env.Root, ShopCatalogue))
	if err != nil {
		t.Fatal(err)
	}
	var file []struct {
		Slug     string  `json:"slug"`
		Category string  `json:"category"`
		Rating   float64 `json:"rating"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if len(file) != 150 {
		t.Fatalf("the catalogue holds %d products", len(file))
	}
	pages := func(n, size int) int { return int(math.Ceil(float64(n) / float64(size))) }
	byCategory := map[string]int{}
	for _, p := range file {
		byCategory[p.Category]++
	}
	want := map[string]bool{}
	perLang := 0
	for _, lang := range []string{"/zh-CN/store", "/store"} {
		n0 := len(want)
		want[lang] = true
		list := func(query string, items int) {
			for p := 1; p <= pages(items, 24); p++ {
				switch {
				case query == "" && p == 1:
					want[lang+"/products"] = true
				case query == "":
					want[lang+"/products?page="+strconv.Itoa(p)] = true
				case p == 1:
					want[lang+"/products?"+query] = true
				default:
					want[lang+"/products?"+query+"&page="+strconv.Itoa(p)] = true
				}
			}
		}
		list("", len(file))
		for c, n := range byCategory {
			list("category="+c, n)
		}
		for _, s := range []string{"newest", "price-desc"} {
			list("sort="+s, len(file))
		}
		for _, p := range file {
			want[lang+"/products/"+p.Slug] = true
			reviews := 9 + len(p.Slug)*3 + int(p.Rating*10)%7
			for r := 2; r <= pages(reviews, 10); r++ {
				want[lang+"/products/"+p.Slug+"?reviews="+strconv.Itoa(r)] = true
			}
		}
		want[lang+"/journal"] = true
		for _, post := range []string{"stock-sync-api", "theme-css", "kettle-photos"} {
			want[lang+"/journal/"+post] = true
		}
		perLang = len(want) - n0
	}
	u, o := CrawlURLs()
	if len(u) != len(want) || len(u) != 2*perLang {
		t.Errorf("U holds %d URLs, want %d (%d for each language)", len(u), len(want), perLang)
	}
	seen := map[string]bool{}
	for _, url := range u {
		if seen[url] {
			t.Errorf("%s is in U twice", url)
		}
		seen[url] = true
		if !want[url] {
			t.Errorf("%s is in U and is no public page of the catalogue", url)
		}
		if _, kind := StorePageKind(url); kind == StoreKindCart || kind == StoreKindCheckout || kind == "" {
			t.Errorf("%s is in U and is a %q page", url, kind)
		}
	}
	for url := range want {
		if !seen[url] {
			t.Errorf("%s is missing from U", url)
		}
	}
	for _, lang := range []string{"/zh-CN/store", "/store"} {
		old := 0
		for _, url := range o {
			if strings.HasPrefix(url, lang+"/") {
				old++
			}
			if seen[url] {
				t.Errorf("the old URL %s is also in U", url)
			}
		}
		if old != 30 {
			t.Errorf("O holds %d URLs of %s, want 30", old, lang)
		}
	}
	if len(o) != 60 || len(slices.Compact(slices.Sorted(slices.Values(o)))) != 60 {
		t.Errorf("O holds %d URLs, not 60 different ones", len(o))
	}
	for _, url := range o {
		slug := url[strings.LastIndex(url, "/")+1:]
		if _, kind := StorePageKind(url); kind != StoreKindProduct && kind != StoreKindPost || slices.Contains(StoreProducts(), slug) || slices.Contains(StorePosts, slug) {
			t.Errorf("the old URL %s is a %s that the shop has", url, kind)
		}
	}
	for _, url := range u {
		if strings.Contains(url, "sort=price-asc") || strings.Contains(url, "sort=rating") {
			t.Errorf("%s is in U: no plain link leads to this sort", url)
		}
	}
	t.Logf("U: %d URLs (%d for each language), O: %d", len(u), perLang, len(o))
}

// The kinds a crawler URL is counted under in the report.
func TestTheKindsOfCrawlerURLs(t *testing.T) {
	for url, want := range map[string]string{
		"/store": "home", "/zh-CN/store/products?page=2": "list", "/store/products/oak-tray": "product",
		"/store/products/oak-tray?reviews=3": "reviews", "/store/journal": "journal", "/zh-CN/store/journal/theme-css": "post",
	} {
		if got := crawlKind(url); got != want {
			t.Errorf("%s is a %s, want %s", url, got, want)
		}
	}
	u, _ := CrawlURLs()
	kinds := map[string]int{}
	for _, url := range u {
		kinds[crawlKind(url)]++
	}
	for _, k := range []string{"home", "list", "product", "reviews", "journal", "post"} {
		if kinds[k] == 0 {
			t.Errorf("U has no %s", k)
		}
	}
}

// robotsRefuses reads the robots.txt of the shop and says whether its rules for every crawler keep a
// URL out: a Disallow pattern matches from the start of the path and the query, and a star in it
// stands for any characters, as the crawlers of the large search engines read it.
func robotsRefuses(t *testing.T) func(url string) bool {
	t.Helper()
	env, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(env.Shop, "public", "robots.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var patterns [][]string
	everyone := false
	for line := range strings.SplitSeq(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		name, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		switch value = strings.TrimSpace(value); strings.ToLower(name) {
		case "user-agent":
			everyone = value == "*"
		case "disallow":
			if everyone && value != "" {
				patterns = append(patterns, strings.Split(value, "*"))
			}
		case "allow":
			t.Fatalf("robots.txt has an Allow rule, which this test does not read: %s", line)
		}
	}
	if len(patterns) == 0 {
		t.Fatal("robots.txt keeps every crawler out of nothing")
	}
	return func(url string) bool {
		for _, parts := range patterns {
			rest, ok := strings.CutPrefix(url, parts[0])
			for _, part := range parts[1:] {
				if !ok {
					break
				}
				_, rest, ok = strings.Cut(rest, part)
			}
			if ok {
				return true
			}
		}
		return false
	}
}

// The shop has a robots.txt that keeps crawlers out of the search results and the filtered lists, and
// lets them into the lists of the categories, the sorts and the pages. So the URLs of the crawlers (U)
// are all allowed, with the old URLs, and no search and no filter of the plans is: a crawler that
// follows the plain links of the pages to a popular word, a quick filter or a price band is told to
// stay out, which is why U has none of them.
func TestTheCrawlersAskForWhatRobotsAllows(t *testing.T) {
	refuses := robotsRefuses(t)
	public, old := CrawlURLs()
	for _, u := range slices.Concat(public, old) {
		if refuses(u) {
			t.Errorf("robots.txt keeps crawlers out of %s, which a crawler of the comparison asks for", u)
		}
	}
	// Every page a visit can be on that is no list is allowed too: robots.txt is about the list alone.
	for _, u := range StoreVisitPages() {
		if _, kind := StorePageKind(u); kind != StoreKindList && refuses(u) {
			t.Errorf("robots.txt keeps crawlers out of %s", u)
		}
	}
	searches, filters, kept := 0, 0, 0
	for _, p := range GenerateSitePlanSet(1, 1).Plans {
		for _, s := range p.Steps {
			if _, kind := StorePageKind(s.Page); kind != StoreKindList {
				continue
			}
			f := listFilterOf(stripLanding(s.Page))
			narrowed := f.Query != "" || len(f.Colors)+len(f.Materials)+len(f.Badges) > 0 || f.Rating > 0 || f.Min > 0 || f.Max > 0 || f.InStock
			if refuses(s.Page) != narrowed {
				t.Fatalf("robots.txt keeps crawlers out of %s: %v, and it has a search word or a filter other than the category: %v", s.Page, refuses(s.Page), narrowed)
			}
			switch {
			case f.Query != "":
				searches++
			case narrowed:
				filters++
			default:
				kept++
			}
		}
	}
	if searches < 100 || filters < 100 || kept < 100 {
		t.Errorf("the plans have %d searches, %d filtered lists and %d other lists: too few to check robots.txt on", searches, filters, kept)
	}
}
