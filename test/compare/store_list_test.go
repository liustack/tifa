package compare

import (
	"context"
	"fmt"
	"html"
	"io"
	"math/rand/v2"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// listURLsOfTheFiles are the URLs of the product list the two files have something for: the lists a
// visit can be on and the lists of the crawlers.
func listURLsOfTheFiles() []string {
	var out []string
	for _, u := range depsURLs() {
		if _, kind := StorePageKind(u); kind == StoreKindList {
			out = append(out, u)
		}
	}
	return out
}

// For the lists store_deps.json was made for (the fixed lists a visit can be on, and those of the
// crawlers), the model works out the products the file has, which were read from a shop that ran: the
// products of a page, and for a list whose products a price or a stock decides the products that pass
// its other filters, in order.
func TestTheListModelHasTheProductsOfStoreDeps(t *testing.T) {
	d := checkedDeps(t)
	recs := storeCatalogue().records
	price, inStock := shopPrices()
	common := map[string]bool{}
	for _, dep := range d.Common {
		common[dep] = true
	}
	lists, decided := 0, 0
	for _, u := range listURLsOfTheFiles() {
		f := listFilterOf(u)
		lists++
		if f.priceDecides() {
			decided++
			if got, want := f.narrowed(recs), d.Lists[d.listBase(u)]; !slices.Equal(got, want) {
				t.Errorf("%s: the products that pass its other filters are %v, the file has %v", u, got, want)
			}
			continue
		}
		// The file has the products of the page that are not on every page, with the marks of a list.
		want := []string{}
		for _, dep := range d.Pages[d.RenderURL(u)] {
			if !strings.HasPrefix(dep, allProducts+":") {
				want = append(want, dep)
			}
		}
		got := []string{}
		for _, slug := range ModelList(u, price, inStock).Products {
			if !common[slug] {
				got = append(got, slug)
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("%s: the page shows %v, the file has %v", u, got, want)
		}
	}
	// The twelve fixed lists of StoreListQueries are among them, but the two no visit can be on.
	for _, l := range StoreLangs {
		for _, q := range storeVisitLists() {
			u := StoreBase(l) + "/products"
			if q != "" {
				u += "?" + q
			}
			if !slices.Contains(listURLsOfTheFiles(), u) {
				t.Errorf("%s is not a list of the file", u)
			}
		}
	}
	if lists < 60 || decided < 10 {
		t.Errorf("%d lists checked, %d of them decided by a price or a stock: too few", lists, decided)
	}
}

// layoutLinks are the links of store_links.json that the Layout of the storefront puts on every page of
// a language: those every page of the language has, but the page a link leads to, which the file leaves
// out of its own links.
func layoutLinks(lang string) map[string]bool {
	graph := storeLinkGraph()
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
	out := map[string]bool{}
	for to, n := range has {
		want := pages
		if l, _ := StorePageKind(to); l == lang && graph[to] != nil {
			want--
		}
		if n == want {
			out[to] = true
		}
	}
	return out
}

// For the lists store_links.json was made for (a crawl of the pages in a browser), the links of the
// model are the links of the file: the file has, of the links of a page, those that lead to another
// page a visit can be on, and besides the links of the list itself those of the Layout, which every
// page has, with the same page in the other language.
func TestTheListModelHasTheLinksOfStoreLinks(t *testing.T) {
	price, inStock := shopPrices()
	graph := storeLinkGraph()
	checked := 0
	for _, l := range StoreLangs {
		layout := layoutLinks(l)
		if len(layout) < 5 {
			t.Fatalf("%s: %d links on every page: the Layout has more", l, len(layout))
		}
		other := StoreBase(StoreLangs[0])
		if l == StoreLangs[0] {
			other = StoreBase(StoreLangs[1])
		}
		for _, q := range storeVisitLists() {
			u := StoreBase(l) + "/products"
			if q != "" {
				u += "?" + q
			}
			want := slices.Sorted(slices.Values(graph[u]))
			if len(want) == 0 {
				t.Fatalf("store_links.json has no links for %s", u)
			}
			set := map[string]bool{other + "/products": true} // the language switch of the Layout links to the path alone
			for to := range layout {
				set[to] = true
			}
			for _, link := range ModelList(u, price, inStock).Links() {
				set[link.To] = true
			}
			var got []string
			for to := range set {
				if graph[to] != nil && to != u {
					got = append(got, to)
				}
			}
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Errorf("%s:\n the model and the Layout link to %v\n the file has %v", u, got, want)
			}
			checked++
		}
	}
	if checked != 20 {
		t.Errorf("%d lists checked, want the 10 a visit can be on in each language", checked)
	}
}

// The links of a list are written as the router of the shop writes them: a space is +, and what
// encodeURIComponent escapes is escaped but the characters a browser sends as they are.
func TestTheLinksOfAListAreSpelledAsTheRouterSpellsThem(t *testing.T) {
	for in, want := range map[string]string{
		"cup": "cup", "杯": "%E6%9D%AF", "pour over": "pour+over", "a&b=c+d%": "a%26b%3Dc%2Bd%25", "it's": "it%27s",
		"a/b:c?d@e,f$g": "a/b:c?d@e,f$g", "[x]{y}|^`\\": "[x]{y}|^`\\", "(a)!~*-_.": "(a)!~*-_.", "a;b#c": "a%3Bb%23c", "é\"<>": "%C3%A9%22%3C%3E",
	} {
		if got := encodeQueryPart(in); got != want {
			t.Errorf("%q is written %q, want %q", in, got, want)
		}
	}
	// The query of a link holds what is not empty or the default, in the order of the page.
	v := listValues{query: "tea cup", category: "drinkware", colors: []string{"moss", "mist"}, materials: []string{"wood"}, badges: []string{"sale", "new"}, rating: "46", min: "100", max: "300", inStock: true, sort: "price-desc"}
	if got, want := v.href("zh-CN", 2), "/zh-CN/store/products?q=tea+cup&category=drinkware&color=moss&color=mist&material=wood&badge=sale&badge=new&rating=46&min=100&max=300&stock=1&sort=price-desc&page=2"; got != want {
		t.Errorf("the link is %s, want %s", got, want)
	}
	if got := (listValues{sort: "featured"}).href("en-US", 1); got != "/store/products" {
		t.Errorf("the list with no filter is %s", got)
	}
}

// What the model says of a few lists, by hand: a search looks in the names of both languages and in the
// tags, a term that finds nothing shows no product and still the best rated ones, and a page past the
// last is the last.
func TestTheListModelSearchesFiltersSortsAndPages(t *testing.T) {
	price, inStock := shopPrices()
	all := len(storeCatalogue().records)
	whole := ModelList("/store/products", price, inStock)
	if whole.Total != all || whole.Pages != (all+PageSize-1)/PageSize || len(whole.Products) != PageSize || len(whole.Picks) != listPicks {
		t.Fatalf("the whole list: %d products on %d pages, %d on the first, %d best rated", whole.Total, whole.Pages, len(whole.Products), len(whole.Picks))
	}
	if got := StoreProducts()[:PageSize]; !slices.Equal(whole.Products, got) {
		t.Errorf("the first page is %v, want the first products of the catalogue", whole.Products)
	}
	// A Chinese term finds the same products on the English list, and the case of a term does not count.
	zh, en, upper := ModelList("/store/products?q=%E6%9D%AF", price, inStock), ModelList("/zh-CN/store/products?q=cup", price, inStock), ModelList("/store/products?q=+CUP+", price, inStock)
	if zh.Total == 0 || en.Total == 0 || !slices.Equal(upper.Products, ModelList("/store/products?q=cup", price, inStock).Products) {
		t.Errorf("a search: %d products for 杯, %d for cup, and %v for \" CUP \"", zh.Total, en.Total, upper.Products)
	}
	none := ModelList("/store/products?q=nothing-matches-this", price, inStock)
	if none.Total != 0 || len(none.Products) != 0 || none.Pages != 1 || !slices.Equal(none.Picks, whole.Picks) {
		t.Errorf("a term that finds nothing: %+v", none)
	}
	last := ModelList("/store/products?page=99&sort=newest", price, inStock)
	if last.Filter.Page != last.Pages || len(last.Products) != all-(last.Pages-1)*PageSize {
		t.Errorf("a page past the last shows page %d of %d with %d products", last.Filter.Page, last.Pages, len(last.Products))
	}
	// A price range is in whole units of the currency of the language, and a sort by price follows the
	// prices as they are now.
	dear := ModelList("/zh-CN/store/products?min=400&sort=price-desc", price, inStock)
	for i, slug := range dear.Products {
		if price[slug] < 40000 || (i > 0 && price[slug] > price[dear.Products[i-1]]) {
			t.Fatalf("the list from ¥400, the dearest first: %v", dear.Products)
		}
	}
	cheap := dear.Products[len(dear.Products)-1]
	price[cheap] = 100
	if after := ModelList("/zh-CN/store/products?min=400&sort=price-desc", price, inStock); after.Total != dear.Total-1 || slices.Contains(after.Products, cheap) {
		t.Errorf("a product priced under the range is still on the list: %v", after.Products)
	}
	// The counts of a filter leave that filter out, so a category that is chosen still counts the others.
	textiles := ModelList("/store/products?category=textiles&sort=rating", price, inStock)
	sum := 0
	for _, c := range textiles.Categories {
		sum += c.Count
	}
	if sum != all || textiles.Total >= all || textiles.Total == 0 {
		t.Errorf("the list of a category: %d products, and its categories count %d of %d", textiles.Total, sum, all)
	}
}

// The links of a list page, by hand: which are plain links, which load a document, and which are Links
// of the router.
func TestTheListModelTellsPlainLinksFromLinksOfTheRouter(t *testing.T) {
	price, inStock := shopPrices()
	p := ModelList("/zh-CN/store/products?category=drinkware&badge=new", price, inStock)
	parts := map[string][]ListLink{}
	for _, l := range p.Links() {
		parts[l.Part] = append(parts[l.Part], l)
		plain := l.Part != ListPartBreadcrumb && l.Part != ListPartProduct && l.Part != ListPartEditorial && l.Part != ListPartPick && l.Part != ListPartMenu && l.Part != ListPartWord
		if l.Plain != plain {
			t.Errorf("%s (%s): plain %v, want %v", l.To, l.Part, l.Plain, plain)
		}
		if !strings.HasPrefix(l.To, "/zh-CN/store") {
			t.Errorf("%s (%s) leaves the language of the page", l.To, l.Part)
		}
	}
	tos := func(part string) []string {
		var out []string
		for _, l := range parts[part] {
			out = append(out, l.To)
		}
		return out
	}
	base := "/zh-CN/store/products"
	for part, want := range map[string][]string{
		ListPartBreadcrumb: {"/zh-CN/store", base},
		// New is on, so its link takes it away, and the others add their filter.
		ListPartQuick:    {base + "?category=drinkware", base + "?category=drinkware&badge=new&badge=sale", base + "?category=drinkware&badge=new&rating=48", base + "?category=drinkware&badge=new&stock=1"},
		ListPartCategory: {base + "?badge=new", base + "?category=drinkware&badge=new", base + "?category=textiles&badge=new", base + "?category=stationery&badge=new", base + "?category=fragrance&badge=new", base + "?category=outdoor&badge=new"},
		ListPartChosen:   {base + "?badge=new", base + "?category=drinkware"},
		ListPartClear:    {base},
		ListPartBand:     {base + "?category=drinkware&badge=new&max=99", base + "?category=drinkware&badge=new&min=100&max=199", base + "?category=drinkware&badge=new&min=200&max=399", base + "?category=drinkware&badge=new&min=400"},
		ListPartMenu:     {base + "?category=drinkware", base + "?category=textiles", base + "?category=stationery", base + "?category=fragrance", base + "?category=outdoor"},
	} {
		if got := tos(part); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", part, got, want)
		}
	}
	if len(parts[ListPartProduct]) != len(p.Products) || len(parts[ListPartPick]) != listPicks || len(parts[ListPartWord]) != len(listPopularWords()["zh-CN"]) {
		t.Errorf("%d products, %d best rated and %d words are linked", len(parts[ListPartProduct]), len(parts[ListPartPick]), len(parts[ListPartWord]))
	}
	if got := tos(ListPartWord)[1]; got != base+"?q=%E6%9D%AF" {
		t.Errorf("the second popular word links to %s", got)
	}
	// A list of one page has no links to pages, and one of several links to the page before, each page and
	// the next.
	if len(parts[ListPartPage]) != 0 && p.Pages == 1 {
		t.Errorf("a list of one page links to pages: %v", tos(ListPartPage))
	}
	second := ModelList("/store/products?sort=newest&page=2", price, inStock)
	var pages []string
	for _, l := range second.Links() {
		if l.Part == ListPartPage {
			pages = append(pages, l.To)
		}
	}
	want := []string{"/store/products?sort=newest"}
	for n := 1; n <= second.Pages; n++ {
		if n == 1 {
			want = append(want, "/store/products?sort=newest")
		} else {
			want = append(want, "/store/products?sort=newest&page="+strconv.Itoa(n))
		}
	}
	want = append(want, "/store/products?sort=newest&page=3")
	if !slices.Equal(pages, want) {
		t.Errorf("the pages of the second page: %v, want %v", pages, want)
	}
}

// listSample draws URLs of the product list from a seed: searches for words of the catalogue in both
// languages, for words that find nothing and for words written with spaces and capitals, filters of one
// to four conditions with every kind of condition and values the shop does not have, price bands and
// ranges, every sort and pages within and past the pages of the list.
func listSample(seed uint64, n int) []string {
	rng := rand.New(rand.NewPCG(seed, 0x6c697374))
	recs := storeCatalogue().records
	var terms []string
	for _, r := range recs {
		terms = append(terms, r.Tags...)
		terms = append(terms, strings.Fields(strings.ToLower(r.Name[1]))...)
		zh := []rune(r.Name[0])
		terms = append(terms, r.Name[0], string(zh[len(zh)-1:]), string(zh[max(0, len(zh)-2):]))
	}
	slices.Sort(terms)
	terms = slices.Compact(terms)
	pick := func(list []string) string { return list[rng.IntN(len(list))] }
	some := func(list []string) []string {
		out := []string{pick(list)}
		if rng.IntN(3) == 0 {
			out = append(out, pick(list))
		}
		return out
	}
	var materials []string
	for _, g := range listMaterials {
		materials = append(materials, g.id)
	}
	random := func() string {
		const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
		out := make([]byte, 8)
		for i := range out {
			out[i] = letters[rng.IntN(len(letters))]
		}
		return string(out)
	}
	var out []string
	for i := range n {
		lang := StoreLangs[i%2]
		v := listValues{sort: "featured"}
		// A third of the lists search, and of those one in five for a word that finds nothing.
		switch rng.IntN(15) {
		case 0:
			v.query = random()
		case 1:
			v.query = strings.ToUpper(pick(terms))
		case 2:
			v.query = pick(terms) + " " // the page trims it in its links
		case 3, 4:
			v.query = pick(terms)
		}
		// None to four conditions, fewer with a search term, so that most lists show something.
		conditions := []int{0, 0, 1, 1, 1, 2, 2, 2, 3, 4}[rng.IntN(10)]
		if v.query != "" {
			conditions = []int{0, 0, 0, 1, 1, 2}[rng.IntN(6)]
		}
		for range conditions {
			switch rng.IntN(8) {
			case 0:
				v.category = pick(append(slices.Clone(listCategories), listCategories[0], listCategories[1], "nope"))
			case 1:
				v.colors = some(listColors)
			case 2:
				v.materials = some(materials)
			case 3:
				v.badges = some(append(slices.Clone(listBadges), ""))
			case 4:
				v.rating = pick([]string{"44", "44", "46", "46", "48", "47", "50"})
			case 5:
				// A band of the page.
				edges := listBandEdges[lang]
				switch b := rng.IntN(len(edges) + 1); {
				case b == 0:
					v.max = strconv.Itoa(edges[0] - 1)
				case b == len(edges):
					v.min = strconv.Itoa(edges[b-1])
				default:
					v.min, v.max = strconv.Itoa(edges[b-1]), strconv.Itoa(edges[b]-1)
				}
			case 6:
				// A range typed into the form, now and then with the lowest price above the highest.
				v.min, v.max = strconv.Itoa(1+rng.IntN(100)), strconv.Itoa(1+rng.IntN(600))
			case 7:
				v.inStock = true
			}
		}
		if rng.IntN(3) > 0 {
			v.sort = pick(append(slices.Clone(listSorts), "nope"))
		}
		page := []int{1, 1, 1, 2, 2, 3, 7, 99}[rng.IntN(8)]
		out = append(out, v.href(lang, page))
	}
	return out
}

// mainLinks are the links in the page of an HTML document of the storefront, without those of the
// Layout around it: the href of every <a> inside <main id="store-main">, in order, each once.
func mainLinks(doc string) ([]string, error) {
	_, rest, ok := strings.Cut(doc, `<main id="store-main"`)
	if !ok {
		return nil, fmt.Errorf("no <main id=\"store-main\">")
	}
	end := strings.LastIndex(rest, "</main>")
	if end < 0 {
		return nil, fmt.Errorf("no </main>")
	}
	var out []string
	for _, tag := range anchorTag.FindAllString(rest[:end], -1) {
		m := hrefAttr.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		if to := html.UnescapeString(m[1]); !slices.Contains(out, to) {
			out = append(out, to)
		}
	}
	return out, nil
}

var (
	anchorTag = regexp.MustCompile(`<a\s[^>]*>`)
	hrefAttr  = regexp.MustCompile(`\shref="([^"]*)"`)
)

// TestTheListModelIsTheListOfTheShop asks a shop that runs for lists drawn from a seed, and for the
// fixed lists of the checks, in both languages, and requires of each that the model has what the shop
// answers: the products of the page and the best rated ones below it, in order, the number of products
// and of pages, the page shown, the sort, what every filter can still choose from (the navigation data
// of the page), and the links of the page, in order (the <a href> of the HTML inside the main element).
func TestTheListModelIsTheListOfTheShop(t *testing.T) {
	env := depsEnv(t)
	ctx := context.Background()
	buildID, err := env.ShopBuildID()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(env.Specs(), func(s Spec) bool { return s.Name == "tifa-nocache" })
	server, err := env.Specs()[i].Start(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	urls := listSample(1, 600)
	for _, l := range StoreLangs {
		for _, q := range slices.Concat(StoreListQueries, []string{
			"color=moss&color=mist&page=2&sort=price-asc&min=x&max=-3&rating=zz", "q=%20Cup%20&category=nope", "sort=nope&stock=0&page=-1",
			"q=" + strings.Repeat("a", 61), "min=300&max=100", "badge=", "q=it%27s+a+%22cup%22+%26+more",
			// The trim of the form is the one of JavaScript: U+FEFF goes and U+0085 stays, at the limit of 60
			// characters too, which are code points.
			"q=%EF%BB%BFcup%EF%BB%BF", "q=%C2%85cup%C2%85", "q=%C2%A0%E3%80%80cup%E2%80%A8",
			"sort=newest&q=%EF%BB%BF" + strings.Repeat("a", 60) + "%EF%BB%BF", "sort=newest&q=%C2%85" + strings.Repeat("a", 60),
			"sort=newest&q=" + strings.Repeat("%F0%9F%98%80", 31), "sort=newest&q=" + strings.Repeat("%F0%9F%98%80", 60), "sort=newest&q=" + strings.Repeat("%F0%9F%98%80", 61),
		}) {
			u := StoreBase(l) + "/products"
			if q != "" {
				u += "?" + q
			}
			urls = append(urls, u)
		}
	}
	slices.Sort(urls)
	urls = slices.Compact(urls)

	price, inStock := shopPrices()
	// How many lists the sample has of each kind, and of the last three those that show a product.
	var empty, paged, several, ranged, searched int
	sorts := map[string]int{}
	slugs := func(v any) []string {
		out := []string{}
		list, _ := v.([]any)
		for _, e := range list {
			m, _ := e.(map[string]any)
			slug, _ := m["slug"].(string)
			out = append(out, slug)
		}
		return out
	}
	facets := func(v any) []ListFacet {
		var out []ListFacet
		list, _ := v.([]any)
		for _, e := range list {
			m, _ := e.(map[string]any)
			id, _ := m["id"].(string)
			count, _ := m["count"].(float64)
			out = append(out, ListFacet{id, int(count)})
		}
		return out
	}
	for _, u := range urls {
		want := ModelList(u, price, inStock)
		f := want.Filter
		conditions := 0
		for _, on := range []bool{f.Category != "", len(f.Colors) > 0, len(f.Materials) > 0, len(f.Badges) > 0, f.Rating > 0, f.Min > 0 || f.Max > 0, f.InStock} {
			if on {
				conditions++
			}
		}
		if want.Total == 0 {
			empty++
		}
		if f.Page > 1 {
			paged++
		}
		if want.Total > 0 && conditions > 1 {
			several++
		}
		if want.Total > 0 && (f.Min > 0 || f.Max > 0) {
			ranged++
		}
		if want.Total > 0 && strings.TrimSpace(f.Query) != "" {
			searched++
		}
		if want.Total > 1 {
			sorts[f.Sort]++
		}

		data, err := fetchData(ctx, server.Base, buildID, u)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := data.Routes[storeList].Data.(map[string]any)
		if !ok {
			t.Fatalf("%s: no data of the list", u)
		}
		filter, _ := got["filter"].(map[string]any)
		number := func(m map[string]any, key string) int {
			n, _ := m[key].(float64)
			return int(n)
		}
		if g := slugs(got["products"]); !slices.Equal(g, want.Products) && (len(g) > 0 || len(want.Products) > 0) {
			t.Errorf("%s: the shop shows %v, the model %v", u, g, want.Products)
		}
		if g := slugs(got["picks"]); !slices.Equal(g, want.Picks) {
			t.Errorf("%s: the best rated of the shop are %v, of the model %v", u, g, want.Picks)
		}
		if number(got, "total") != want.Total || number(got, "pages") != want.Pages || number(filter, "page") != f.Page || filter["sort"] != f.Sort {
			t.Errorf("%s: the shop has %d products on %d pages, shows page %d sorted by %v, the model %d on %d, page %d by %s",
				u, number(got, "total"), number(got, "pages"), number(filter, "page"), filter["sort"], want.Total, want.Pages, f.Page, f.Sort)
		}
		for name, wantFacets := range map[string][]ListFacet{"categories": want.Categories, "colors": want.Colors, "materials": want.Materials, "badges": want.Badges, "ratings": want.Ratings} {
			if g := facets(got[name]); !slices.Equal(g, wantFacets) {
				t.Errorf("%s: the %s of the shop are %v, of the model %v", u, name, g, wantFacets)
			}
		}
		var bands []ListBand
		list, _ := got["bands"].([]any)
		for _, e := range list {
			m, _ := e.(map[string]any)
			bands = append(bands, ListBand{number(m, "min"), number(m, "max"), number(m, "count")})
		}
		if !slices.Equal(bands, want.Bands) {
			t.Errorf("%s: the price bands of the shop are %v, of the model %v", u, bands, want.Bands)
		}

		res, err := http.Get(server.Base + u)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("GET %s: %d %v", u, res.StatusCode, err)
		}
		links, err := mainLinks(string(body))
		if err != nil {
			t.Fatalf("GET %s: %v", u, err)
		}
		var model []string
		for _, l := range want.Links() {
			if !slices.Contains(model, l.To) {
				model = append(model, l.To)
			}
		}
		if !slices.Equal(links, model) {
			t.Errorf("%s:\n the page links to %v\n the model to   %v", u, links, model)
		}
		if t.Failed() {
			t.FailNow() // one list that differs says enough
		}
	}
	t.Logf("%d lists asked: %d with no product, %d past the first page, and with products %d with several conditions, %d with a price range and %d with a search term, and with more than one product by sort %v", len(urls), empty, paged, several, ranged, searched, sorts)
	if empty < 30 || paged < 30 || several < 60 || ranged < 60 || searched < 60 {
		t.Errorf("the sample is thin: %d lists with no product, %d past the first page, and with products %d with several conditions, %d with a price range and %d with a search term", empty, paged, several, ranged, searched)
	}
	for _, s := range listSorts {
		if sorts[s] < 30 {
			t.Errorf("%d lists of more than one product sorted by %s: too few", sorts[s], s)
		}
	}
}

// The links of a list hold the search term as the form of the page trims it, which is the trim of
// JavaScript and not the one of Go: U+FEFF is white space in JavaScript alone and U+0085 in Go alone.
// The search of the shop is Go, so the products follow the trim of Go. The form refuses a term of over
// 60 characters after its own trim, and the links then keep the term as it is, with the default sort.
// The 60 are code points, as zod counts them: a character of two UTF-16 units is one.
func TestTheLinksOfAListTrimTheTermAsJavaScriptDoes(t *testing.T) {
	price, inStock := shopPrices()
	sixty := strings.Repeat("a", 60)
	for _, tc := range []struct {
		name, term string
		linked     string // the term in the links of the page
		found      bool   // the search of the shop finds products
		refused    bool   // the form refuses the term: the links lose the sort
	}{
		{"a plain term", "cup", "cup", true, false},
		{"spaces", " \t\n cup \r\n", "cup", true, false},
		{"U+00A0, U+3000 and U+2028, white space in both", "\u00a0\u3000cup\u2028", "cup", true, false},
		{"U+FEFF, white space in JavaScript alone", "\ufeffcup\ufeff", "cup", false, false},
		{"U+0085, white space in Go alone", "\u0085cup\u0085", "\u0085cup\u0085", true, false},
		{"60 characters", sixty, sixty, false, false},
		{"61 characters", sixty + "a", sixty + "a", false, true},
		{"60 characters between two U+FEFF", "\ufeff" + sixty + "\ufeff", sixty, false, false},
		{"60 characters after a U+0085", "\u0085" + sixty, "\u0085" + sixty, false, true},
		{"60 characters of two UTF-16 units each", strings.Repeat("😀", 60), strings.Repeat("😀", 60), false, false},
		{"61 characters of two UTF-16 units each", strings.Repeat("😀", 61), strings.Repeat("😀", 61), false, true},
	} {
		p := ModelList("/store/products?sort=newest&q="+encodeQueryPart(tc.term), price, inStock)
		if (p.Total > 0) != tc.found {
			t.Errorf("%s: the search finds %d products, want some %v", tc.name, p.Total, tc.found)
		}
		want := "/store/products?q=" + encodeQueryPart(tc.linked) + "&category=drinkware"
		if !tc.refused {
			want += "&sort=newest"
		}
		var got string
		for _, l := range p.Links() {
			if l.Part == ListPartCategory && strings.Contains(l.To, "category=drinkware") {
				got = l.To
			}
		}
		if got != want {
			t.Errorf("%s: the category bar links to %s, want %s", tc.name, got, want)
		}
	}
}
