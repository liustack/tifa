package compare

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// What a crawler asks for: the public pages of the storefront, in both languages (the set U of
// CrawlURLs), and a few old URLs the shop no longer has (the set O), which answer 404. The set is
// made from the catalogue file of the shop and the sizes of the pages, so that it follows the
// catalogue.

// Where the shop pages its lists and the reviews of a product, as examples/shop does
// (products.PageSize and products.ReviewsPerPage).
const (
	PageSize       = 24
	ReviewsPerPage = 10
)

// StoreSorts are the sorts of the product list that U has besides the default (featured), which has no
// query: the two that the HTML of the home links to (newest and price-desc). The shop has two more,
// price-asc and rating, which are a form control of the list and have no plain link anywhere in the
// HTML (checked on the pages of the shop), so a crawler that runs no JavaScript cannot get to them and
// they are left out of U.
var StoreSorts = []string{"newest", "price-desc"}

// reviewCount is how many reviews a product has, the formula of examples/shop (reviewCount in
// app/store/products/review.go). The check against the pages of a running shop is
// TestEveryCrawlerURLAnswersWhatItShould.
func reviewCount(slug string, rating float64) int { return 9 + len(slug)*3 + int(rating*10)%7 }

// pagesOf is how many pages hold n items of size each.
func pagesOf(n, size int) int { return max(1, (n+size-1)/size) }

// crawlLists are the URLs of the pages of the product list in a language: every page of the list with
// no condition, of each category, and of each sort other than the default.
func crawlLists(lang string) []string {
	base := StoreBase(lang) + "/products"
	var out []string
	add := func(query string, items int) {
		for p := 1; p <= pagesOf(items, PageSize); p++ {
			q := query
			if p > 1 {
				q = strings.TrimPrefix(q+"&page="+strconv.Itoa(p), "&")
			}
			if q == "" {
				out = append(out, base)
			} else {
				out = append(out, base+"?"+q)
			}
		}
	}
	recs := storeCatalogue().records
	add("", len(recs))
	counts := map[string]int{}
	var categories []string
	for _, r := range recs {
		if counts[r.Category] == 0 {
			categories = append(categories, r.Category)
		}
		counts[r.Category]++
	}
	for _, c := range categories {
		add("category="+c, counts[c])
	}
	for _, s := range StoreSorts {
		add("sort="+s, len(recs))
	}
	return out
}

// crawlPublic is the URLs of the public pages of a language: the home, the pages of the list, every
// product with the pages of its reviews, the journal and its posts. The cart and the checkout are
// private pages, and are left out.
func crawlPublic(lang string) []string {
	base := StoreBase(lang)
	out := []string{base}
	out = append(out, crawlLists(lang)...)
	for _, r := range storeCatalogue().records {
		out = append(out, base+"/products/"+r.Slug)
		for p := 2; p <= pagesOf(reviewCount(r.Slug, r.Rating), ReviewsPerPage); p++ {
			out = append(out, base+"/products/"+r.Slug+"?reviews="+strconv.Itoa(p))
		}
	}
	out = append(out, base+"/journal")
	for _, post := range StorePosts {
		out = append(out, base+"/journal/"+post)
	}
	return out
}

// crawlOld is the old URLs of a language: products spread over the catalogue as they were called
// a year ago (the slug and -2024), and posts of an old year. The shop has none of them.
func crawlOld(lang string) []string {
	base := StoreBase(lang)
	var out []string
	slugs := StoreProducts()
	for i := range OldProductsPerLanguage {
		out = append(out, base+"/products/"+slugs[(i*len(slugs)/OldProductsPerLanguage)%len(slugs)]+"-2024")
	}
	for i := range OldPostsPerLanguage {
		out = append(out, base+"/journal/"+fmt.Sprintf("news-2024-%02d", i+1))
	}
	return out
}

// CrawlURLs returns the public URLs a crawler fetches (U) and the old URLs it also asks for (O), in
// both languages. A URL is in one of them once.
var CrawlURLs = sync.OnceValues(func() (public, old []string) {
	for _, l := range StoreLangs {
		public = append(public, crawlPublic(l)...)
		old = append(old, crawlOld(l)...)
	}
	return public, old
})

// crawlKind says what a crawler URL is: home, list, product, reviews (a page of the reviews of a
// product), journal or post.
func crawlKind(url string) string {
	_, kind := StorePageKind(url)
	if kind == StoreKindProduct && strings.Contains(url, "?reviews=") {
		return "reviews"
	}
	return kind
}
