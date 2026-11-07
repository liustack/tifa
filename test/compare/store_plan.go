package compare

import (
	_ "embed"
	"encoding/json/v2"
	"math/rand/v2"
	"strings"
	"sync"
)

// Visits to the storefront. A visitor loads one page, then clicks the links of the page on screen
// and now and then reloads it, as on the workbench (plan.go), but the pages, the links and what a
// visitor does next are those of a shop. A visit stays in one language.
//
// Which pages a visit starts on, and how likely a link is to be clicked, are assumptions (see
// StoreFirstShares and storeClicks). Which links exist is not: store_links.json holds, for each
// page, the links of that page that lead to another page of the visit, as a browser reads them. Make
// it again with `compare links` after the pages of the storefront change.

//go:embed store_links.json
var storeLinksJSON []byte

// The kinds of a storefront page.
const (
	StoreKindHome     = "home"
	StoreKindList     = "list"
	StoreKindProduct  = "product"
	StoreKindJournal  = "journal"
	StoreKindPost     = "post"
	StoreKindCart     = "cart"
	StoreKindCheckout = "checkout"
)

// StorePageKind says which language and kind of page a storefront URL is. The kind is empty for a
// URL that is not a page of the storefront.
func StorePageKind(url string) (lang, kind string) {
	path, _, _ := strings.Cut(url, "?")
	lang = "en-US"
	if rest, ok := strings.CutPrefix(path, "/zh-CN"); ok {
		lang, path = "zh-CN", rest
	}
	rest, ok := strings.CutPrefix(path, "/store")
	if !ok {
		return lang, ""
	}
	switch {
	case rest == "":
		return lang, StoreKindHome
	case rest == "/products":
		return lang, StoreKindList
	case strings.HasPrefix(rest, "/products/"):
		return lang, StoreKindProduct
	case rest == "/journal":
		return lang, StoreKindJournal
	case strings.HasPrefix(rest, "/journal/"):
		return lang, StoreKindPost
	case rest == "/cart":
		return lang, StoreKindCart
	case rest == "/checkout":
		return lang, StoreKindCheckout
	}
	return lang, ""
}

// storeVisitLists are the queries of the product list that a visit can be on. The two that show
// nothing (a page past the end, a search that finds nothing) are left out.
func storeVisitLists() []string {
	var out []string
	for _, q := range StoreListQueries {
		if q != "page=99&sort=newest" && q != "q=nothing-matches-this" {
			out = append(out, q)
		}
	}
	return out
}

// StoreVisitPages are the pages a visit can be on, in both languages: the home, the list with its
// filters, every product, the cart, the checkout, the journal and its posts.
func StoreVisitPages() []string {
	var out []string
	for _, l := range StoreLangs {
		base := StoreBase(l)
		out = append(out, base)
		for _, q := range storeVisitLists() {
			if q == "" {
				out = append(out, base+"/products")
			} else {
				out = append(out, base+"/products?"+q)
			}
		}
		for _, slug := range StoreProducts {
			out = append(out, base+"/products/"+slug)
		}
		out = append(out, base+"/cart", base+"/checkout", base+"/journal")
		for _, post := range StorePosts {
			out = append(out, base+"/journal/"+post)
		}
	}
	return out
}

// StoreFirstShares are the shares of the kinds of page that a visit starts on. They are
// assumptions: most visitors arrive on a product (from a search or an advert), many on the home.
var StoreFirstShares = []struct {
	Kind  string
	Share float64
}{
	{StoreKindHome, 0.25}, {StoreKindProduct, 0.35}, {StoreKindList, 0.15},
	{StoreKindJournal, 0.05}, {StoreKindPost, 0.10}, {StoreKindCart, 0.10},
}

// storeClicks says, for the kind of page a visitor is on, how likely a click is to go to a link of
// each kind. They are assumptions, shares of the links the page has: a visitor on a list picks a
// product, on a product looks at a related one, goes back to the list or goes to the cart, and
// from the cart to the checkout. A kind the page has no link to is left out, and the rest count
// for the whole.
var storeClicks = map[string]map[string]float64{
	StoreKindHome:     {StoreKindList: 0.3, StoreKindProduct: 0.4, StoreKindJournal: 0.15, StoreKindCart: 0.1, StoreKindPost: 0.05},
	StoreKindList:     {StoreKindProduct: 0.7, StoreKindList: 0.1, StoreKindHome: 0.1, StoreKindCart: 0.1},
	StoreKindProduct:  {StoreKindProduct: 0.3, StoreKindList: 0.25, StoreKindCart: 0.25, StoreKindHome: 0.1, StoreKindJournal: 0.1},
	StoreKindJournal:  {StoreKindPost: 0.7, StoreKindHome: 0.15, StoreKindList: 0.15},
	StoreKindPost:     {StoreKindJournal: 0.4, StoreKindProduct: 0.3, StoreKindHome: 0.15, StoreKindList: 0.15},
	StoreKindCart:     {StoreKindCheckout: 0.4, StoreKindProduct: 0.3, StoreKindList: 0.2, StoreKindHome: 0.1},
	StoreKindCheckout: {StoreKindCart: 0.4, StoreKindHome: 0.3, StoreKindList: 0.3},
}

var storeLinkGraph = sync.OnceValue(func() map[string][]string {
	var g map[string][]string
	if err := json.Unmarshal(storeLinksJSON, &g); err != nil {
		panic("store_links.json: " + err.Error())
	}
	return g
})

// StoreLinks lists the links of a storefront page that a visit can click, with weights that sum to
// 1: the links the browser found on the page that stay in the language of the page.
func StoreLinks(url string) []Link {
	lang, kind := StorePageKind(url)
	var kinds []string
	by := map[string][]string{}
	for _, to := range storeLinkGraph()[url] {
		if l, k := StorePageKind(to); l == lang && storeClicks[kind][k] > 0 {
			if len(by[k]) == 0 {
				kinds = append(kinds, k)
			}
			by[k] = append(by[k], to)
		}
	}
	var total float64
	for _, k := range kinds {
		total += storeClicks[kind][k]
	}
	var out []Link
	for _, k := range kinds {
		for _, to := range by[k] {
			out = append(out, Link{to, storeClicks[kind][k] / total / float64(len(by[k]))})
		}
	}
	return out
}

// GenerateStorePlans makes n visits to the storefront whose number of page views is geometric
// with the given mean. The language is en-US or zh-CN with the same probability, the kind of the
// first page is picked by StoreFirstShares and the page among those of that kind alike. Plan i
// depends on the seed and i only.
func GenerateStorePlans(seed uint64, n int, mean float64) []Plan {
	byKind := map[string]map[string][]string{}
	for _, url := range StoreVisitPages() {
		l, k := StorePageKind(url)
		if byKind[l] == nil {
			byKind[l] = map[string][]string{}
		}
		byKind[l][k] = append(byKind[l][k], url)
	}
	return generatePlans(seed, n, mean, StoreLinks, func(rng *rand.Rand) string {
		lang := StoreLangs[rng.IntN(len(StoreLangs))]
		kind, u := StoreFirstShares[len(StoreFirstShares)-1].Kind, rng.Float64()
		for _, s := range StoreFirstShares {
			if u -= s.Share; u < 0 {
				kind = s.Kind
				break
			}
		}
		pages := byKind[lang][kind]
		return pages[rng.IntN(len(pages))]
	})
}
