package compare

import (
	"cmp"
	"encoding/json/v2"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// The product list of the storefront, worked out here as the shop works it out: for any URL of the list
// (/store/products or /zh-CN/store/products with any query) the products of the page, the best rated
// products below it, what its filters can still choose from, and the links of the page. The list has
// too many URLs to ask a shop for each (a search term, any filters, a sort, a page), so the plans and
// the expected hit rate take them from this model.
//
// Every rule is copied from the code of the shop, named in the comment of the rule, and nothing here
// is a rule of its own. With TIFA_COMPARE=1, TestTheListModelIsTheListOfTheShop asks a shop that runs
// for lists drawn from a seed and requires the same products, counts and links.
//
// Names, tags, categories, colours, materials, badges and ratings are not written in a run. Prices and
// stock are (a checkout, an edit of a product), so the model takes them as they are now: price is the
// price of each product in fen and inStock whether it has stock, by slug.

// The tables of the shop that the list filters by: the categories, the colours, the families of
// materials with the words that put a material in them, and the badges
// (examples/shop/app/store/products/catalogue.go), the sorts, the rating floors and the lowest prices
// of the price bands above the first, in whole units of the currency of each language (Sorts, Search
// and bandEdges in examples/shop/app/store/products/product.go: CNY for zh-CN and USD for en-US, the
// Locales of examples/shop/app/store/storekit/storekit.go).
var (
	listCategories = []string{"drinkware", "textiles", "stationery", "fragrance", "outdoor"}
	listColors     = []string{"mist", "moss", "clay", "oat", "ink", "brass", "charcoal"}
	listMaterials  = []struct {
		id    string
		words []string
	}{
		{"metal", []string{"stainless", "brass", "pure tin", "tinplate", "tinned", "copper", "aluminium", "steel", "不锈钢", "黄铜", "纯锡", "紫铜", "铝", "钢", "铁皮"}},
		{"ceramic", []string{"clay", "stoneware", "glaze", "enamel", "陶瓷", "粗陶", "搪瓷", "釉", "高岭"}},
		{"wood", []string{"oak", "walnut", "木"}},
		{"glass", []string{"glass", "玻璃"}},
		{"fabric", []string{"linen", "cotton", "wool", "canvas", "oxford", "亚麻", "棉", "羊毛", "帆布", "牛津布"}},
		{"paper", []string{"paper", "kraft", "纸"}},
		{"wax", []string{"wax", "oil", "sandalwood", "cypress", "osmanthus", "蜡", "油", "檀香", "柏木", "桂花"}},
	}
	listBadges       = []string{"new", "sale", "limited"}
	listSorts        = []string{"featured", "newest", "price-asc", "price-desc", "rating"}
	listRatingFloors = []int{48, 46, 44}
	listBandEdges    = map[string][]int{"zh-CN": {100, 200, 400}, "en-US": {15, 30, 60}}
)

// listEditorialPost is the post of the journal a list shows among its products, after the eighth
// (Editorial in examples/shop/app/store/products/page.tsx).
const listEditorialPost = "kettle-photos"

// materialFamiliesOf are the families of materials a product belongs to: every family one of whose
// words is in one of its materials, in either language (computeMaterialFamilies in
// examples/shop/app/store/products/catalogue.go).
func materialFamiliesOf(materials [][2]string) []string {
	var out []string
	for _, g := range listMaterials {
		if slices.ContainsFunc(materials, func(m [2]string) bool {
			return slices.ContainsFunc(g.words, func(w string) bool {
				return strings.Contains(strings.ToLower(m[0]), w) || strings.Contains(strings.ToLower(m[1]), w)
			})
		}) {
			out = append(out, g.id)
		}
	}
	return out
}

// ShopLocales is the folder of the translations of the storefront, from the root of the repository.
const ShopLocales = "examples/shop/app/store/_locales"

// listPopularWords are the words each language of the list links to under its products
// (list.popularWords in the translations of the shop, read from the files as the catalogue is).
var listPopularWords = sync.OnceValue(func() map[string][]string {
	e, err := NewEnv()
	if err != nil {
		panic(err)
	}
	out := map[string][]string{}
	for _, l := range StoreLangs {
		path := filepath.Join(ShopLocales, l+".json")
		data, err := os.ReadFile(filepath.Join(e.Root, path))
		if err != nil {
			panic(err)
		}
		var file struct {
			List struct {
				PopularWords []string `json:"popularWords"`
			} `json:"list"`
		}
		if err := json.Unmarshal(data, &file); err != nil || len(file.List.PopularWords) == 0 {
			panic(path + ": no list.popularWords")
		}
		out[l] = file.List.PopularWords
	}
	return out
})

// listFilter is what a URL of the list asks for, as the Load of the list reads its query (Load in
// examples/shop/app/store/products/route.go, Filter in product.go): the search term, the category, the
// colours, the families of materials and the badges (any of each), the lowest rating in tenths of a
// star, the price range in whole units of the currency of the language, whether only what is in stock,
// the sort and the page. A number that does not parse or is negative is 0, which is no bound.
type listFilter struct {
	Lang                      string
	Query, Category           string
	Colors, Materials, Badges []string
	Rating, Min, Max          int
	InStock                   bool
	// Sort is the sort the shop takes: "featured" for one it does not have (Search in product.go). Asked is
	// the sort as the URL has it.
	Sort, Asked string
	Page        int
}

// listFilterOf reads the filter of a URL of the list.
func listFilterOf(page string) listFilter {
	lang, _ := StorePageKind(page)
	_, query, _ := strings.Cut(page, "?")
	v, _ := url.ParseQuery(query)
	number := func(s string) int {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return 0
		}
		return n
	}
	f := listFilter{
		Lang: lang, Query: v.Get("q"), Category: v.Get("category"), Colors: v["color"], Materials: v["material"], Badges: v["badge"],
		Rating: number(v.Get("rating")), Min: number(v.Get("min")), Max: number(v.Get("max")), InStock: v.Get("stock") == "1",
		Sort: v.Get("sort"), Asked: v.Get("sort"), Page: number(v.Get("page")),
	}
	if !slices.Contains(listSorts, f.Sort) {
		f.Sort = "featured"
	}
	return f
}

// byPrice says whether the list is sorted by price.
func (f listFilter) byPrice() bool { return byPriceSort(f.Sort) }

// priceDecides says whether a price or a stock decides which products the page shows: the list sorts by
// price, has a price range, or has only what is in stock.
func (f listFilter) priceDecides() bool { return f.byPrice() || f.Min > 0 || f.Max > 0 || f.InStock }

// The filters a count of a facet leaves out (match in Search of product.go).
const (
	skipNone      = ""
	skipCategory  = "category"
	skipColors    = "colors"
	skipMaterials = "materials"
	skipBadges    = "badges"
	skipRating    = "rating"
	skipPrice     = "price"
)

// narrows says whether a product passes the filters that no write of a run changes, but the one named
// by skip: the search term, trimmed as Go trims it (strings.TrimSpace, as the shop does), which is looked
// for in the name of the product in both languages, whatever the case, and in its tags as they are
// written, the category, the colours, the families of materials, the badges and the rating (match in
// Search of examples/shop/app/store/products/product.go).
func (f listFilter) narrows(r storeRecord, skip string) bool {
	if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" &&
		!strings.Contains(r.nameLower[0], q) && !strings.Contains(r.nameLower[1], q) &&
		!slices.ContainsFunc(r.Tags, func(t string) bool { return strings.Contains(t, q) }) {
		return false
	}
	if skip != skipCategory && f.Category != "" && r.Category != f.Category {
		return false
	}
	if skip != skipColors && len(f.Colors) > 0 && !slices.ContainsFunc(r.Colors, func(c string) bool { return slices.Contains(f.Colors, c) }) {
		return false
	}
	if skip != skipMaterials && len(f.Materials) > 0 && !slices.ContainsFunc(r.Families, func(m string) bool { return slices.Contains(f.Materials, m) }) {
		return false
	}
	if skip != skipBadges && len(f.Badges) > 0 && !slices.Contains(f.Badges, r.Badge) {
		return false
	}
	if skip != skipRating && f.Rating > 0 && int(math.Round(r.Rating*10)) < f.Rating {
		return false
	}
	return true
}

// affords says whether a product at a price in fen, in stock or not, passes the price range and the
// stock filter of the list, the range left out for skipPrice: the price is in whole units of the
// currency of the language (the rest of match in Search of product.go).
func (f listFilter) affords(fen int, inStock bool, skip string) bool {
	if skip != skipPrice {
		if p := priceIn(fen, f.Lang); (f.Min > 0 && p < f.Min) || (f.Max > 0 && p > f.Max) {
			return false
		}
	}
	return !f.InStock || inStock
}

// narrowed are the products that pass the filters no write changes, in the order of the sort, or in
// the order of the catalogue when the list sorts by price or is not sorted: newest first by the date
// added, best rated first, the order of the catalogue among equals (Search in product.go).
func (f listFilter) narrowed(recs []storeRecord) []string {
	var found []storeRecord
	for _, r := range recs {
		if f.narrows(r, skipNone) {
			found = append(found, r)
		}
	}
	switch f.Sort {
	case "newest":
		slices.SortStableFunc(found, func(a, b storeRecord) int { return b.Added.Compare(a.Added) })
	case "rating":
		slices.SortStableFunc(found, func(a, b storeRecord) int { return cmp.Compare(b.Rating, a.Rating) })
	}
	out := make([]string, len(found))
	for i, r := range found {
		out[i] = r.Slug
	}
	return out
}

// sortByPrice sorts slugs as the list sorts by price: by the price in fen, and in the order they come
// in among equals (Search in product.go).
func sortByPrice(slugs []string, price map[string]int, desc bool) []string {
	out := slices.Clone(slugs)
	slices.SortStableFunc(out, func(a, b string) int {
		if desc {
			return cmp.Compare(price[b], price[a])
		}
		return cmp.Compare(price[a], price[b])
	})
	return out
}

// cut makes the page of the list from narrowed, the products that pass its other filters: those in the
// price range and, for a list of what is in stock, in stock, sorted by price when the list sorts so,
// and cut into pages of PageSize. A page past the last is the last, and one before the first the first
// (Search in product.go). It returns the products of the page, how many there are in all, the pages
// and the page shown.
func (f listFilter) cut(narrowed []string, price map[string]int, inStock map[string]bool) (products []string, total, pages, page int) {
	var found []string
	for _, slug := range narrowed {
		if f.affords(price[slug], inStock[slug], skipNone) {
			found = append(found, slug)
		}
	}
	if f.byPrice() {
		found = sortByPrice(found, price, f.Sort == "price-desc")
	}
	pages = max(1, (len(found)+PageSize-1)/PageSize)
	page = min(max(f.Page, 1), pages)
	from := (page - 1) * PageSize
	return found[from:min(from+PageSize, len(found))], len(found), pages, page
}

// topRatedOf are the n best rated products, the cheaper first among equals and the order of the
// catalogue after that (TopRated in examples/shop/app/store/products/product.go).
func topRatedOf(recs []storeRecord, price map[string]int, n int) []string {
	list := slices.Clone(recs)
	slices.SortStableFunc(list, func(a, b storeRecord) int {
		return cmp.Or(cmp.Compare(b.Rating, a.Rating), cmp.Compare(price[a.Slug], price[b.Slug]))
	})
	out := make([]string, min(n, len(list)))
	for i := range out {
		out[i] = list[i].Slug
	}
	return out
}

// listPicks is how many best rated products a list shows below its products (Load in
// examples/shop/app/store/products/route.go).
const listPicks = 6

// ListFacet is one value of a filter of the list with the number of products the other filters leave
// for it (Facet in product.go).
type ListFacet struct {
	ID    string
	Count int
}

// ListBand is a range of prices in whole units of the currency of the language, with the number of
// products in it that the other filters leave. Max is 0 for the open band at the top (PriceBand in
// product.go).
type ListBand struct{ Min, Max, Count int }

// ListPage is a page of the product list as the shop makes it for a URL: the filter with the page that
// is shown, the products of the page in order (their slugs), the best rated products below them, how
// many products pass the filters and on how many pages, and what each filter can still choose from
// (List in examples/shop/app/store/products/route.go, Results in product.go).
type ListPage struct {
	Filter       listFilter
	Products     []string
	Picks        []string
	Total, Pages int
	Categories   []ListFacet
	Colors       []ListFacet
	Materials    []ListFacet
	Badges       []ListFacet
	Ratings      []ListFacet
	Bands        []ListBand
	// PriceMax is the price of the dearest product of the catalogue, in whole units of the currency of
	// the language, rounded up.
	PriceMax int
}

// shopPrices are the prices of the catalogue file in fen and whether each product is in stock there: what
// a shop holds when it starts, before a restock or any write.
func shopPrices() (price map[string]int, inStock map[string]bool) {
	price, inStock = map[string]int{}, map[string]bool{}
	for _, r := range storeCatalogue().records {
		price[r.Slug], inStock[r.Slug] = r.PriceFen, r.Stock > 0
	}
	return price, inStock
}

// ModelList works out the page of the product list at a URL, with the prices in fen and whether each
// product is in stock as they are now (Search and TopRated in
// examples/shop/app/store/products/product.go, called by Load in route.go).
func ModelList(page string, price map[string]int, inStock map[string]bool) ListPage {
	return modelList(page, price, inStock, topRatedOf(storeCatalogue().records, price, listPicks))
}

// modelList is ModelList with the best rated products given, which are the same for every list at the
// same prices.
func modelList(page string, price map[string]int, inStock map[string]bool, picks []string) ListPage {
	recs := storeCatalogue().records
	f := listFilterOf(page)
	p := ListPage{Picks: picks}
	p.Products, p.Total, p.Pages, f.Page = f.cut(f.narrowed(recs), price, inStock)
	p.Filter = f
	for _, r := range recs {
		p.PriceMax = max(p.PriceMax, (centsIn(price[r.Slug], f.Lang)+99)/100)
	}
	// passes says, for each filter a count leaves out, which products pass the others: worked out once
	// for each of them, since every value of a filter counts over the same products.
	passes := map[string][]bool{}
	for _, skip := range []string{skipCategory, skipColors, skipMaterials, skipBadges, skipRating, skipPrice} {
		list := make([]bool, len(recs))
		for i, r := range recs {
			list[i] = f.narrows(r, skip) && f.affords(price[r.Slug], inStock[r.Slug], skip)
		}
		passes[skip] = list
	}
	// count is how many products pass the other filters and are in.
	count := func(skip string, in func(storeRecord) bool) int {
		n := 0
		for i, r := range recs {
			if passes[skip][i] && in(r) {
				n++
			}
		}
		return n
	}
	for _, id := range listCategories {
		p.Categories = append(p.Categories, ListFacet{id, count(skipCategory, func(r storeRecord) bool { return r.Category == id })})
	}
	for _, id := range listColors {
		p.Colors = append(p.Colors, ListFacet{id, count(skipColors, func(r storeRecord) bool { return slices.Contains(r.Colors, id) })})
	}
	for _, g := range listMaterials {
		p.Materials = append(p.Materials, ListFacet{g.id, count(skipMaterials, func(r storeRecord) bool { return slices.Contains(r.Families, g.id) })})
	}
	for _, id := range listBadges {
		p.Badges = append(p.Badges, ListFacet{id, count(skipBadges, func(r storeRecord) bool { return r.Badge == id })})
	}
	for _, floor := range listRatingFloors {
		p.Ratings = append(p.Ratings, ListFacet{strconv.Itoa(floor), count(skipRating, func(r storeRecord) bool { return int(math.Round(r.Rating*10)) >= floor })})
	}
	edges := listBandEdges[f.Lang]
	for i := range len(edges) + 1 {
		b := ListBand{}
		if i > 0 {
			b.Min = edges[i-1]
		}
		if i < len(edges) {
			b.Max = edges[i] - 1
		}
		b.Count = count(skipPrice, func(r storeRecord) bool {
			at := priceIn(price[r.Slug], f.Lang)
			return at >= b.Min && (b.Max == 0 || at <= b.Max)
		})
		p.Bands = append(p.Bands, b)
	}
	return p
}

// The parts of a list page that hold links, in the order of the page.
const (
	ListPartBreadcrumb = "breadcrumb" // the home and, on a list of a category, the whole list
	ListPartQuick      = "quick"      // the quick filters: new, on sale, 4.8 and up, in stock
	ListPartCategory   = "category"   // the category bar: all, and each category
	ListPartChosen     = "chosen"     // a chosen filter, which the link takes away
	ListPartClear      = "clear"      // the list with no filter
	ListPartBand       = "band"       // a price band
	ListPartProduct    = "product"    // a product of the page
	ListPartEditorial  = "editorial"  // the post of the journal among the products
	ListPartPage       = "page"       // a page of the list: the one before, each page, the next
	ListPartPick       = "pick"       // a best rated product below the list
	ListPartMenu       = "menu"       // a category card below the list
	ListPartWord       = "word"       // a popular search word
)

// ListLink is a link of a list page: where it leads, the part of the page it is in, and whether it is a
// plain link. A browser loads a document for a plain link, and the client router takes a Link and asks
// for the data of the page.
type ListLink struct {
	To    string
	Part  string
	Plain bool
}

// listValues is the filter of a list as its form holds it, which the links of the page are built from
// (formValues in examples/shop/app/store/products/page.tsx).
type listValues struct {
	query, category           string
	colors, materials, badges []string
	rating, min, max          string
	inStock                   bool
	sort                      string
}

// values are the values of the form for the filter the shop read: a number that is 0 is empty, and the
// search term is trimmed, as the schema of the form does (filterSchema in
// examples/shop/app/store/products/filter-schema.ts). The schema runs in JavaScript, so the trim is
// the one of JavaScript (jsTrim), not the one of Go that the search of the shop uses (narrows). The
// schema refuses a term of over 60 characters after the trim and a lowest price above the highest, and
// the page then takes the values as they are with the sort "featured" (ProductList in page.tsx). The 60
// are Unicode code points and not UTF-16 units: zod 4, which the shop pins, counts the length of a
// string so ($ZodCheckMaxLength in zod/v4/core/checks.js), so a character outside the basic plane is
// one.
func (f listFilter) values() listValues {
	text := func(n int) string {
		if n > 0 {
			return strconv.Itoa(n)
		}
		return ""
	}
	v := listValues{
		query: f.Query, category: f.Category, colors: f.Colors, materials: f.Materials, badges: f.Badges,
		rating: text(f.Rating), min: text(f.Min), max: text(f.Max), inStock: f.InStock, sort: f.Sort,
	}
	trimmed := jsTrim(f.Query)
	if utf8.RuneCountInString(trimmed) > 60 || (f.Min > 0 && f.Max > 0 && f.Min > f.Max) {
		v.sort = "featured"
		return v
	}
	v.query = trimmed
	return v
}

// jsTrim trims a string as String.prototype.trim of JavaScript does: the white space and the line
// terminators of ECMAScript, which are tab, line feed, vertical tab, form feed, carriage return, space,
// U+00A0, U+FEFF, the other space separators of Unicode (U+1680, U+2000 to U+200A, U+202F, U+205F,
// U+3000) and U+2028 and U+2029. strings.TrimSpace of Go differs in two characters: it trims U+0085,
// which JavaScript keeps, and keeps U+FEFF, which JavaScript trims.
func jsTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
			return true
		}
		return r >= 0x2000 && r <= 0x200A
	})
}

// encodeQueryPart writes a name or a value of a query as the router of the shop writes it in a link
// (encodeQueryPart in js/runtime/router.tsx): encodeURIComponent, but with a space as +, the
// characters $ , / : ? @ [ \ ] ^ ` { | } as they are and ' escaped.
func encodeQueryPart(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.IndexByte("-_.!~*()$,/:?@[\\]^`{|}", c) >= 0:
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

// href is the URL of the list in a language with the filters v and a page: the query holds what is not
// empty or the default, in the order the page writes it (search and useListHref in page.tsx,
// stringifySearch in js/runtime/router.tsx).
func (v listValues) href(lang string, page int) string {
	var parts []string
	add := func(name string, values ...string) {
		for _, value := range values {
			parts = append(parts, encodeQueryPart(name)+"="+encodeQueryPart(value))
		}
	}
	if v.query != "" {
		add("q", v.query)
	}
	if v.category != "" {
		add("category", v.category)
	}
	add("color", v.colors...)
	add("material", v.materials...)
	add("badge", v.badges...)
	if v.rating != "" {
		add("rating", v.rating)
	}
	if v.min != "" {
		add("min", v.min)
	}
	if v.max != "" {
		add("max", v.max)
	}
	if v.inStock {
		add("stock", "1")
	}
	if v.sort != "featured" {
		add("sort", v.sort)
	}
	if page > 1 {
		add("page", strconv.Itoa(page))
	}
	if len(parts) == 0 {
		return StoreBase(lang) + "/products"
	}
	return StoreBase(lang) + "/products?" + strings.Join(parts, "&")
}

// without is list with every id taken out.
func without(list []string, id string) []string {
	out := []string{}
	for _, x := range list {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

// toggled is list without id when it has it, and with id at its end when it does not (toggle in
// QuickFilters of page.tsx).
func toggled(list []string, id string) []string {
	if slices.Contains(list, id) {
		return without(list, id)
	}
	return append(slices.Clone(list), id)
}

// Links are the links of the page, in the order of the page (ProductList in
// examples/shop/app/store/products/page.tsx and the components it shows). They are the links of the page
// itself: those of the Layout around it (the header, the menu, the footer) are on every page of the
// storefront, and the products a visitor looked at before are not in a page a server sends a visitor
// with no history. Each place of the page that links somewhere is one entry (a card of a product, with
// its two Links to the product, is one), so a URL can be there more than once, and both as a plain
// link and as a Link: the category bar and the category cards lead to the same list of a category.
func (p ListPage) Links() []ListLink {
	lang, v := p.Filter.Lang, p.Filter.values()
	base := StoreBase(lang)
	var out []ListLink
	plain := func(part, to string) { out = append(out, ListLink{to, part, true}) }
	link := func(part, to string) { out = append(out, ListLink{to, part, false}) }
	with := func(change func(*listValues)) string {
		next := v
		change(&next)
		return next.href(lang, 1)
	}

	// The breadcrumb: the home, and the whole list on a list of a category the shop has.
	link(ListPartBreadcrumb, base)
	if slices.Contains(listCategories, v.category) {
		link(ListPartBreadcrumb, base+"/products")
	}
	// The quick filters, each a plain link that adds its filter or, when it is on, takes it away
	// (QuickFilters).
	plain(ListPartQuick, with(func(n *listValues) { n.badges = toggled(v.badges, "new") }))
	plain(ListPartQuick, with(func(n *listValues) { n.badges = toggled(v.badges, "sale") }))
	plain(ListPartQuick, with(func(n *listValues) {
		if n.rating = "48"; v.rating == "48" {
			n.rating = ""
		}
	}))
	plain(ListPartQuick, with(func(n *listValues) { n.inStock = !v.inStock }))
	// The category bar: every category with the other filters kept, as plain links.
	plain(ListPartCategory, with(func(n *listValues) { n.category = "" }))
	for _, id := range listCategories {
		plain(ListPartCategory, with(func(n *listValues) { n.category = id }))
	}
	// The chosen filters, each a plain link to the list without it, and one to the list with no filter
	// (chosenFilters).
	var chosen []string
	if v.query != "" {
		chosen = append(chosen, with(func(n *listValues) { n.query = "" }))
	}
	if v.category != "" {
		chosen = append(chosen, with(func(n *listValues) { n.category = "" }))
	}
	for _, c := range v.colors {
		chosen = append(chosen, with(func(n *listValues) { n.colors = without(v.colors, c) }))
	}
	for _, m := range v.materials {
		chosen = append(chosen, with(func(n *listValues) { n.materials = without(v.materials, m) }))
	}
	for _, b := range v.badges {
		chosen = append(chosen, with(func(n *listValues) { n.badges = without(v.badges, b) }))
	}
	if v.rating != "" {
		chosen = append(chosen, with(func(n *listValues) { n.rating = "" }))
	}
	if v.min != "" || v.max != "" {
		chosen = append(chosen, with(func(n *listValues) { n.min, n.max = "", "" }))
	}
	if v.inStock {
		chosen = append(chosen, with(func(n *listValues) { n.inStock = false }))
	}
	if len(chosen) > 0 {
		for _, to := range chosen {
			plain(ListPartChosen, to)
		}
		plain(ListPartClear, base+"/products")
	}
	// The price bands of the form, each a plain link that sets the range or, when it is the range of the
	// list, takes it away (FilterForm).
	for _, b := range p.Bands {
		lo, hi := "", ""
		if b.Min > 0 {
			lo = strconv.Itoa(b.Min)
		}
		if b.Max > 0 {
			hi = strconv.Itoa(b.Max)
		}
		if v.min == lo && v.max == hi {
			lo, hi = "", ""
		}
		plain(ListPartBand, with(func(n *listValues) { n.min, n.max = lo, hi }))
	}
	// The products of the page, with the post of the journal after the eighth, or a link to the list with
	// no filter when there is none.
	for i, slug := range p.Products {
		link(ListPartProduct, base+"/products/"+slug)
		if i == 7 {
			link(ListPartEditorial, base+"/journal/"+listEditorialPost)
		}
	}
	if len(p.Products) == 0 {
		plain(ListPartClear, base+"/products")
	}
	// The pages, on a list of more than one: the page before, every page and the next, as plain links.
	if p.Pages > 1 {
		plain(ListPartPage, v.href(lang, max(1, p.Filter.Page-1)))
		for n := 1; n <= p.Pages; n++ {
			plain(ListPartPage, v.href(lang, n))
		}
		plain(ListPartPage, v.href(lang, min(p.Pages, p.Filter.Page+1)))
	}
	// Below the list: the best rated products, a card for each category (the menu of the Layout) and the
	// popular search words, all Links.
	for _, slug := range p.Picks {
		link(ListPartPick, base+"/products/"+slug)
	}
	for _, id := range listCategories {
		link(ListPartMenu, listValues{category: id, sort: "featured"}.href(lang, 1))
	}
	for _, w := range listPopularWords()[lang] {
		link(ListPartWord, listValues{query: w, sort: "featured"}.href(lang, 1))
	}
	return out
}

// byPriceSort says whether a sort of the list, as a URL writes it, is by price.
func byPriceSort(sort string) bool { return sort == "price-asc" || sort == "price-desc" }

// priceDecidesList says whether a price or a stock decides which products a page of the list shows: it
// sorts by price, has a price range, or has only what is in stock.
func priceDecidesList(page string) bool { return listFilterOf(page).priceDecides() }

// The classes of the URLs of the product list, for the tables that count them apart.
const (
	ListSearch = "search" // a search: the list of a search term, with whatever else
	ListFilter = "filter" // a filtered list: no term, and a colour, a material, a badge, a rating, a price range or only what is in stock
	ListOther  = "other"  // the others: the whole list and the list of a category, in any sort, on any page
)

// ListClass is the class of a URL of the product list. The lists of the class "other" are the ones a
// crawler asks for and a visit lands on (the whole list, a category, a sort, a page): few URLs, asked
// for again and again. The two other classes are the long tail: the robots.txt of the shop keeps
// crawlers out of them, and a visit comes to one by an action on a page.
func ListClass(page string) string {
	switch f := listFilterOf(stripLanding(page)); {
	case strings.TrimSpace(f.Query) != "":
		return ListSearch
	case len(f.Colors)+len(f.Materials)+len(f.Badges) > 0 || f.Rating > 0 || f.Min > 0 || f.Max > 0 || f.InStock:
		return ListFilter
	}
	return ListOther
}
