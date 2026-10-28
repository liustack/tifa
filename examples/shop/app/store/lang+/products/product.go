// Package products is the catalogue of the storefront: products, their categories, colours and
// reviews, the search and filters of the product list, and the storage behind them. The product
// list, the product page, the cart and the checkout call it.
package products

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
)

// Color is a colour a product comes in. Hex is the swatch the pages draw.
type Color struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Hex  string `json:"hex"`
}

type Category struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Spec struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Card is a product as a list shows it. Amounts are in the minor unit of the locale's currency.
type Card struct {
	Slug           string  `json:"slug"`
	Name           string  `json:"name"`
	Category       string  `json:"category"`
	PriceCents     int     `json:"priceCents"`
	CompareAtCents int     `json:"compareAtCents"` // 0 when not on sale
	Rating         float64 `json:"rating"`
	Reviews        int     `json:"reviews"`
	Colors         []Color `json:"colors"`
	Badge          string  `json:"badge"` // "new", "sale", "limited" or ""
	InStock        bool    `json:"inStock"`
	// Stock is how many are left, and Summary and Materials are what the list view of a card shows
	// besides the photo.
	Stock     int      `json:"stock"`
	Summary   string   `json:"summary"`
	Materials []string `json:"materials"`
	// Number is the place of the product in the catalogue, from 1, which the cards show.
	Number int `json:"number"`
	// Sized is whether the product comes in sizes, which a card cannot pick, so it adds to the
	// cart from the product page only.
	Sized bool `json:"sized"`
}

// Product is the whole product, as its page shows it.
type Product struct {
	Card
	CategoryID  string    `json:"categoryId"`
	Summary     string    `json:"summary"`
	Description string    `json:"description"`
	Sizes       []string  `json:"sizes"`
	Stock       int       `json:"stock"`
	Materials   []string  `json:"materials"`
	Care        []string  `json:"care"`
	Specs       []Spec    `json:"specs"`
	AddedAt     time.Time `json:"addedAt"`
}

// GetProduct returns the product slug in the language of l, or tifa.ErrNotFound.
func GetProduct(slug string, l store.Locale) (Product, error) {
	for _, p := range catalogue {
		if p.slug == slug {
			return p.product(l), nil
		}
	}
	return Product{}, tifa.ErrNotFound
}

// Related returns up to n other products of the same category, then of others, best rated first.
func Related(slug string, n int, l store.Locale) []Card {
	var self productRecord
	for _, p := range catalogue {
		if p.slug == slug {
			self = p
		}
	}
	others := slices.DeleteFunc(slices.Clone(catalogue), func(p productRecord) bool { return p.slug == slug })
	slices.SortStableFunc(others, func(a, b productRecord) int {
		if (a.category == self.category) != (b.category == self.category) {
			if a.category == self.category {
				return -1
			}
			return 1
		}
		return cmp.Compare(b.rating, a.rating)
	})
	var out []Card
	for _, p := range others[:min(n, len(others))] {
		out = append(out, p.card(l))
	}
	return out
}

// Featured returns the n products the home page shows: the newest first.
func Featured(n int, l store.Locale) []Card {
	list := slices.Clone(catalogue)
	slices.SortStableFunc(list, func(a, b productRecord) int { return b.added.Compare(a.added) })
	var out []Card
	for _, p := range list[:min(n, len(list))] {
		out = append(out, p.card(l))
	}
	return out
}

// Categories returns every category in the language of l.
func Categories(l store.Locale) []Category {
	var out []Category
	for _, c := range categories {
		out = append(out, Category{ID: c.id, Name: c.name.In(l)})
	}
	return out
}

// Filter is what the product list is searched and filtered by. Min and Max are whole units of
// the locale's currency, 0 for no bound.
type Filter struct {
	Query    string   `json:"query"`
	Category string   `json:"category"`
	Colors   []string `json:"colors"`
	Min      int      `json:"min"`
	Max      int      `json:"max"`
	InStock  bool     `json:"inStock"`
	// Materials are families of materials, any of which a product is made of, such as "wood".
	Materials []string `json:"materials"`
	// Badges are the labels a product wears, any of "new", "sale" and "limited".
	Badges []string `json:"badges"`
	// Rating is the lowest rating, in tenths of a star: 46 for 4.6 and up. 0 for any.
	Rating int    `json:"rating"`
	Sort   string `json:"sort"` // "featured", "newest", "price-asc", "price-desc" or "rating"
	Page   int    `json:"page"` // from 1
}

// Sorts are the orders the list can be sorted in.
var Sorts = []string{"featured", "newest", "price-asc", "price-desc", "rating"}

// PageSize is how many products a page of the list holds.
const PageSize = 24

// Facet is one value of a filter with the number of products the other filters leave for it.
type Facet struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Hex   string `json:"hex,omitempty"`
	Count int    `json:"count"`
}

// PriceBand is a range of prices in whole units of the locale's currency, with the number of
// products in it that the other filters leave. Max is 0 for the open top band.
type PriceBand struct {
	Min   int `json:"min"`
	Max   int `json:"max"`
	Count int `json:"count"`
}

// bandEdges are the lowest prices of the bands above the first, in whole units of each currency.
var bandEdges = map[string][]int{"CNY": {100, 200, 400}, "USD": {15, 30, 60}}

// Results is one page of the product list with what its filters can still choose from.
type Results struct {
	Filter     Filter  `json:"filter"`
	Products   []Card  `json:"products"`
	Total      int     `json:"total"`
	Pages      int     `json:"pages"`
	Categories []Facet `json:"categories"`
	Colors     []Facet `json:"colors"`
	Materials  []Facet `json:"materials"`
	Badges     []Facet `json:"badges"`
	// Ratings are the floors the list can be narrowed to, best first. A facet's ID is the floor in
	// tenths of a star.
	Ratings []Facet `json:"ratings"`
	// Bands are four ranges of price, cheapest first, with the products each leaves.
	Bands []PriceBand `json:"bands"`
	// PriceMax is the price of the dearest product, in whole units, the top of the price range.
	PriceMax int `json:"priceMax"`
}

// Search returns the page of the list the filter asks for, in the language of l.
func Search(f Filter, l store.Locale) Results {
	if !slices.Contains(Sorts, f.Sort) {
		f.Sort = "featured"
	}
	r := Results{Products: []Card{}}
	// match tells whether a product passes every filter but the one named by skip, which a facet
	// leaves out so that it counts what choosing each of its values would leave.
	match := func(p productRecord, skip string) bool {
		if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" &&
			!strings.Contains(strings.ToLower(p.name[0]), q) && !strings.Contains(strings.ToLower(p.name[1]), q) &&
			!slices.ContainsFunc(p.tags, func(t string) bool { return strings.Contains(t, q) }) {
			return false
		}
		if skip != "category" && f.Category != "" && p.category != f.Category {
			return false
		}
		if skip != "colors" && len(f.Colors) > 0 && !slices.ContainsFunc(p.colors, func(c string) bool { return slices.Contains(f.Colors, c) }) {
			return false
		}
		if skip != "materials" && len(f.Materials) > 0 && !slices.ContainsFunc(materialFamilies(p), func(m string) bool { return slices.Contains(f.Materials, m) }) {
			return false
		}
		if skip != "badges" && len(f.Badges) > 0 && !slices.Contains(f.Badges, p.badge) {
			return false
		}
		if skip != "rating" && f.Rating > 0 && int(math.Round(p.rating*10)) < f.Rating {
			return false
		}
		if skip != "price" {
			price := store.Price(p.priceFen, l) / 100
			if (f.Min > 0 && price < f.Min) || (f.Max > 0 && price > f.Max) {
				return false
			}
		}
		return !f.InStock || p.stock > 0
	}
	var found []productRecord
	for _, p := range catalogue {
		r.PriceMax = max(r.PriceMax, (store.Price(p.priceFen, l)+99)/100)
		if match(p, "") {
			found = append(found, p)
		}
	}
	// count is how many products pass the other filters and also satisfy in.
	count := func(skip string, in func(productRecord) bool) int {
		n := 0
		for _, p := range catalogue {
			if in(p) && match(p, skip) {
				n++
			}
		}
		return n
	}
	for _, c := range categories {
		r.Categories = append(r.Categories, Facet{ID: c.id, Name: c.name.In(l), Count: count("category", func(p productRecord) bool { return p.category == c.id })})
	}
	for _, c := range colors {
		r.Colors = append(r.Colors, Facet{ID: c.id, Name: c.name.In(l), Hex: c.hex, Count: count("colors", func(p productRecord) bool { return slices.Contains(p.colors, c.id) })})
	}
	for _, m := range materialGroups {
		r.Materials = append(r.Materials, Facet{ID: m.id, Name: m.name.In(l), Count: count("materials", func(p productRecord) bool { return slices.Contains(materialFamilies(p), m.id) })})
	}
	for _, b := range badges {
		r.Badges = append(r.Badges, Facet{ID: b.id, Name: b.name.In(l), Count: count("badges", func(p productRecord) bool { return p.badge == b.id })})
	}
	for _, floor := range []int{48, 46, 44} {
		r.Ratings = append(r.Ratings, Facet{ID: strconv.Itoa(floor), Count: count("rating", func(p productRecord) bool { return int(math.Round(p.rating*10)) >= floor })})
	}
	edges := bandEdges[l.Currency]
	for i := range len(edges) + 1 {
		b := PriceBand{}
		if i > 0 {
			b.Min = edges[i-1]
		}
		if i < len(edges) {
			b.Max = edges[i] - 1
		}
		b.Count = count("price", func(p productRecord) bool {
			price := store.Price(p.priceFen, l) / 100
			return price >= b.Min && (b.Max == 0 || price <= b.Max)
		})
		r.Bands = append(r.Bands, b)
	}
	switch f.Sort {
	case "newest":
		slices.SortStableFunc(found, func(a, b productRecord) int { return b.added.Compare(a.added) })
	case "price-asc":
		slices.SortStableFunc(found, func(a, b productRecord) int { return cmp.Compare(a.priceFen, b.priceFen) })
	case "price-desc":
		slices.SortStableFunc(found, func(a, b productRecord) int { return cmp.Compare(b.priceFen, a.priceFen) })
	case "rating":
		slices.SortStableFunc(found, func(a, b productRecord) int { return cmp.Compare(b.rating, a.rating) })
	}
	r.Total = len(found)
	r.Pages = max(1, (len(found)+PageSize-1)/PageSize)
	// The page comes from the query string: keep it within the pages before multiplying, so a
	// large one shows the last page instead of overflowing the offset.
	f.Page = min(max(f.Page, 1), r.Pages)
	r.Filter = f
	from := (f.Page - 1) * PageSize
	for _, p := range found[from:min(from+PageSize, len(found))] {
		r.Products = append(r.Products, p.card(l))
	}
	return r
}

// HasVariant reports whether a product comes in the colour and size, and how many are in stock.
func HasVariant(slug, color, size string) (stock int, ok bool) {
	for _, p := range catalogue {
		if p.slug == slug {
			sizeOK := len(p.sizes) == 0 && size == "" || slices.Contains(p.sizes, size)
			return p.stock, slices.Contains(p.colors, color) && sizeOK
		}
	}
	return 0, false
}

// ColorName returns the name of a colour in the language of l.
func ColorName(id string, l store.Locale) string {
	for _, c := range colors {
		if c.id == id {
			return c.name.In(l)
		}
	}
	return id
}

func (p productRecord) card(l store.Locale) Card {
	c := Card{
		Slug: p.slug, Name: p.name.In(l), Rating: p.rating, Reviews: reviewCount(p),
		PriceCents: store.Price(p.priceFen, l), Badge: p.badge, InStock: p.stock > 0, Stock: p.stock, Summary: p.summary.In(l), Sized: len(p.sizes) > 0,
		Number: slices.IndexFunc(catalogue, func(r productRecord) bool { return r.slug == p.slug }) + 1,
	}
	if p.compareFen > 0 {
		c.CompareAtCents = store.Price(p.compareFen, l)
	}
	for _, cat := range categories {
		if cat.id == p.category {
			c.Category = cat.name.In(l)
		}
	}
	for _, m := range p.materials {
		c.Materials = append(c.Materials, m.In(l))
	}
	for _, id := range p.colors {
		for _, col := range colors {
			if col.id == id {
				c.Colors = append(c.Colors, Color{ID: id, Name: col.name.In(l), Hex: col.hex})
			}
		}
	}
	return c
}

func (p productRecord) product(l store.Locale) Product {
	out := Product{
		Card: p.card(l), CategoryID: p.category, Summary: p.summary.In(l), Description: p.description.In(l),
		Sizes: p.sizes, Stock: p.stock, AddedAt: p.added,
	}
	for _, m := range p.materials {
		out.Materials = append(out.Materials, m.In(l))
	}
	for _, c := range careNotes[p.category] {
		out.Care = append(out.Care, c.In(l))
	}
	for _, s := range p.specs {
		out.Specs = append(out.Specs, Spec{Label: s[0].In(l), Value: s[1].In(l)})
	}
	return out
}

// TopRated returns the n best rated products, the cheaper first among equals.
func TopRated(n int, l store.Locale) []Card {
	list := slices.Clone(catalogue)
	slices.SortStableFunc(list, func(a, b productRecord) int {
		return cmp.Or(compareRating(b, a), cmp.Compare(a.priceFen, b.priceFen))
	})
	var out []Card
	for _, p := range list[:min(n, len(list))] {
		out = append(out, p.card(l))
	}
	return out
}
