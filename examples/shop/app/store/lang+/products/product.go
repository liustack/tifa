// Package products is the catalogue of the storefront: products, their categories, colours and
// reviews, the search and filters of the product list, and the storage behind them. The product
// list, the product page, the cart and the checkout call it.
package products

import (
	"cmp"
	"slices"
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
	Sort     string   `json:"sort"` // "featured", "newest", "price-asc", "price-desc" or "rating"
	Page     int      `json:"page"` // from 1
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

// Results is one page of the product list with what its filters can still choose from.
type Results struct {
	Filter     Filter  `json:"filter"`
	Products   []Card  `json:"products"`
	Total      int     `json:"total"`
	Pages      int     `json:"pages"`
	Categories []Facet `json:"categories"`
	Colors     []Facet `json:"colors"`
	// PriceMax is the price of the dearest product, in whole units, the top of the price range.
	PriceMax int `json:"priceMax"`
}

// Search returns the page of the list the filter asks for, in the language of l.
func Search(f Filter, l store.Locale) Results {
	if !slices.Contains(Sorts, f.Sort) {
		f.Sort = "featured"
	}
	f.Page = max(f.Page, 1)
	r := Results{Filter: f, Products: []Card{}}
	match := func(p productRecord, skipCategory, skipColors bool) bool {
		if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" &&
			!strings.Contains(strings.ToLower(p.name[0]), q) && !strings.Contains(strings.ToLower(p.name[1]), q) &&
			!slices.ContainsFunc(p.tags, func(t string) bool { return strings.Contains(t, q) }) {
			return false
		}
		if !skipCategory && f.Category != "" && p.category != f.Category {
			return false
		}
		if !skipColors && len(f.Colors) > 0 && !slices.ContainsFunc(p.colors, func(c string) bool { return slices.Contains(f.Colors, c) }) {
			return false
		}
		price := store.Price(p.priceFen, l) / 100
		if (f.Min > 0 && price < f.Min) || (f.Max > 0 && price > f.Max) {
			return false
		}
		return !f.InStock || p.stock > 0
	}
	var found []productRecord
	for _, p := range catalogue {
		r.PriceMax = max(r.PriceMax, (store.Price(p.priceFen, l)+99)/100)
		if match(p, false, false) {
			found = append(found, p)
		}
	}
	for _, c := range categories {
		n := 0
		for _, p := range catalogue {
			if p.category == c.id && match(p, true, false) {
				n++
			}
		}
		r.Categories = append(r.Categories, Facet{ID: c.id, Name: c.name.In(l), Count: n})
	}
	for _, c := range colors {
		n := 0
		for _, p := range catalogue {
			if slices.Contains(p.colors, c.id) && match(p, false, true) {
				n++
			}
		}
		r.Colors = append(r.Colors, Facet{ID: c.id, Name: c.name.In(l), Hex: c.hex, Count: n})
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
	from := min((f.Page-1)*PageSize, len(found))
	for _, p := range found[from:min(from+PageSize, len(found))] {
		r.Products = append(r.Products, p.card(l))
	}
	return r
}

// Price returns the price of a product in fen, for the cart, and whether it exists.
func Price(slug string) (int, bool) {
	for _, p := range catalogue {
		if p.slug == slug {
			return p.priceFen, true
		}
	}
	return 0, false
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
		PriceCents: store.Price(p.priceFen, l), Badge: p.badge, InStock: p.stock > 0,
	}
	if p.compareFen > 0 {
		c.CompareAtCents = store.Price(p.compareFen, l)
	}
	for _, cat := range categories {
		if cat.id == p.category {
			c.Category = cat.name.In(l)
		}
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
