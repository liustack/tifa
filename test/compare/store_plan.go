package compare

import (
	_ "embed"
	"encoding/json/v2"
	"slices"
	"strings"
	"sync"
)

// The pages of the storefront that a shopper visit is on, and the links it clicks. A visit stays in
// one language.
//
// Which pages a visit lands on, and how likely a link is to be clicked, are assumptions (see
// LandingShares and StoreClicks in mix.go). Which links exist is not: store_links.json holds, for each
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
		for _, slug := range StoreProducts() {
			out = append(out, base+"/products/"+slug)
		}
		out = append(out, base+"/cart", base+"/checkout", base+"/journal")
		for _, post := range StorePosts {
			out = append(out, base+"/journal/"+post)
		}
	}
	return out
}

var storeLinkGraph = sync.OnceValue(func() map[string][]string {
	var g map[string][]string
	if err := json.Unmarshal(storeLinksJSON, &g); err != nil {
		panic("store_links.json: " + err.Error())
	}
	return g
})

// layoutLinks are the links the Layout of the storefront puts on every page of a language, from
// store_links.json: those every page of the language has, but the page a link leads to, which the file
// leaves out of its own links. They are sorted.
var layoutLinks = sync.OnceValue(func() map[string][]string {
	graph := storeLinkGraph()
	out := map[string][]string{}
	for _, lang := range StoreLangs {
		pages, has := 0, map[string]int{}
		for page, links := range graph {
			if l, _ := StorePageKind(page); l != lang {
				continue
			}
			pages++
			for _, to := range links {
				has[to]++
			}
		}
		for to, n := range has {
			want := pages
			if l, _ := StorePageKind(to); l == lang && graph[to] != nil {
				want--
			}
			if n == want {
				out[lang] = append(out[lang], to)
			}
		}
		slices.Sort(out[lang])
	}
	return out
})

// storeTargets are the pages a storefront page links to. A product list has too many URLs to crawl
// each (a search word, any filters, a sort, a page), so its links are the ones the model of the list
// gives the page (ModelList, with the data the shop starts with, as a browser records it) and the
// links of the Layout, which every page has. Any other page has the links store_links.json has for it.
func storeTargets(url string) []string {
	lang, kind := StorePageKind(url)
	if kind != StoreKindList {
		return storeLinkGraph()[url]
	}
	var out []string
	seen := map[string]bool{url: true}
	add := func(to string) {
		if !seen[to] {
			seen[to] = true
			out = append(out, to)
		}
	}
	for _, l := range planList(url).Links() {
		add(l.To)
	}
	for _, to := range layoutLinks()[lang] {
		add(to)
	}
	return out
}

// StoreLinks lists the links of a storefront page that a visit can click, with weights that sum to
// 1: the links of the page that stay in the language of the page (storeTargets). A landing page with
// a parameter of an ad or an email (landing.go) has those of the page without it. From a product list
// no link to another list has a weight (StoreClicks): a visit gets from a list to a list by a filter,
// a sort, a page or a search, which the plans deal by quota (store_actions.go).
func StoreLinks(url string) []Link {
	url = stripLanding(url) // a landing page has the links of the page without its parameter
	if links, ok := storeLinksOf.Load(url); ok {
		return links.([]Link)
	}
	links := storeLinks(url)
	storeLinksOf.Store(url, links)
	return links
}

// storeLinksOf keeps the links of each page StoreLinks was asked for. They are not to be changed.
var storeLinksOf sync.Map

func storeLinks(url string) []Link {
	lang, kind := StorePageKind(url)
	var kinds []string
	by := map[string][]string{}
	for _, to := range storeTargets(url) {
		if l, k := StorePageKind(to); l == lang && StoreClicks[kind][k] > 0 {
			if len(by[k]) == 0 {
				kinds = append(kinds, k)
			}
			by[k] = append(by[k], to)
		}
	}
	var total float64
	for _, k := range kinds {
		total += StoreClicks[kind][k]
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
			out = append(out, Link{to, StoreClicks[kind][k] / total * weight(to) / sum})
		}
	}
	return out
}
