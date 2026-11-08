package compare

import (
	_ "embed"
	"encoding/json/v2"
	"strings"
	"sync"
)

// The pages of the storefront that a shopper visit is on, and the links it clicks. A visit stays in
// one language.
//
// Which pages a visit lands on, and how likely a link is to be clicked, are assumptions (see
// LandingShares in mix.go and storeClicks). Which links exist is not: store_links.json holds, for each
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

// storeClicks says, for the kind of page a visitor is on, how likely a click is to go to a link of
// each kind. They are assumptions, shares of the links the page has: a visitor on a list picks a
// product, on a product looks at a related one, goes back to the list, to the cart or to the
// checkout (the link of the cart drawer), and from the cart to the checkout. A kind the page has no
// link to is left out, and the rest count for the whole. Among the links of a kind, a product is
// picked by its popularity (productWeight), any other page alike.
var storeClicks = map[string]map[string]float64{
	StoreKindHome:     {StoreKindList: 0.3, StoreKindProduct: 0.4, StoreKindJournal: 0.15, StoreKindCart: 0.1, StoreKindPost: 0.05},
	StoreKindList:     {StoreKindProduct: 0.7, StoreKindList: 0.1, StoreKindHome: 0.1, StoreKindCart: 0.1},
	StoreKindProduct:  {StoreKindProduct: 0.3, StoreKindList: 0.25, StoreKindCart: 0.25, StoreKindCheckout: 0.1, StoreKindHome: 0.1, StoreKindJournal: 0.1},
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
		weight := func(string) float64 { return 1 }
		if k == StoreKindProduct {
			weight = func(to string) float64 { return productWeight(productOf(to)) }
		}
		var sum float64
		for _, to := range by[k] {
			sum += weight(to)
		}
		for _, to := range by[k] {
			out = append(out, Link{to, storeClicks[kind][k] / total * weight(to) / sum})
		}
	}
	return out
}
