// Package store is the storefront: its Layout applies to /store and every page below it, in each
// language of the area (see _locales), and its page is the storefront's home.
package store

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store/cart"
	"github.com/liustack/tifa/examples/shop/app/store/journal"
	"github.com/liustack/tifa/examples/shop/app/store/products"
	"github.com/liustack/tifa/examples/shop/app/store/storekit"
)

// Home is the storefront's home: new products, what the shop uses itself, the best of each
// category, what is on sale, what customers wrote and the latest journal posts.
type Home struct {
	Featured   []products.Card       `json:"featured"`
	Picks      []Pick                `json:"picks"`
	ByCategory []CategoryPicks       `json:"byCategory"`
	Sale       []products.Card       `json:"sale"`
	Reviews    []products.ReviewCard `json:"reviews"`
	Posts      []journal.Summary     `json:"posts"`
}

// Pick is one of the products the shop uses itself, as the home page shows it: its photo in use,
// name, number, rating and price. It says nothing of the stock, so it gets none of it.
type Pick struct {
	Slug       string  `json:"slug"`
	Photo      string  `json:"photo"`
	Name       string  `json:"name"`
	PriceCents int     `json:"priceCents"`
	Rating     float64 `json:"rating"`
	Reviews    int     `json:"reviews"`
	Number     int     `json:"number"`
}

// CategoryPicks are the best rated products of one category, each as a tile.
type CategoryPicks struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Products []products.Tile `json:"products"`
}

// picks are the products the shop uses every day, which the home page shows on their own.
var picks = []string{"pour-over-kettle", "ceramic-cup", "oak-tray"}

func Load(c *tifa.Ctx) (Home, error) {
	l, err := storekit.FindLocale(c.Language())
	if err != nil {
		return Home{}, err
	}
	h := Home{Featured: products.Featured(8, l), Reviews: products.LatestReviews(6, l), Posts: journal.List(l)[:3]}
	for _, slug := range picks {
		p, err := products.GetProduct(slug, l)
		if err != nil {
			return Home{}, err
		}
		h.Picks = append(h.Picks, Pick{Slug: p.Slug, Photo: p.Photo, Name: p.Name, PriceCents: p.PriceCents, Rating: p.Rating, Reviews: p.Reviews, Number: p.Number})
	}
	for _, cat := range products.Categories(l) {
		best := products.Search(products.Filter{Category: cat.ID, Sort: "rating"}, l).Products
		tiles := CategoryPicks{ID: cat.ID, Name: cat.Name}
		for _, p := range best[:min(4, len(best))] {
			tiles.Products = append(tiles.Products, p.Tile())
		}
		h.ByCategory = append(h.ByCategory, tiles)
	}
	for _, p := range products.Search(products.Filter{Sort: "price-desc"}, l).Products {
		if p.CompareAtCents > 0 {
			h.Sale = append(h.Sale, p)
		}
	}
	return h, nil
}

// Shell is what every page of the storefront shows around itself: the language, the category
// menu and the subtotal from which delivery is free. It is the same for every visitor, so the
// render memo reuses a page across visitors. The cart in the header and the products a visitor
// looked at come from the mine route after the page has loaded.
type Shell struct {
	Locale            storekit.Locale      `json:"locale"`
	Menu              []products.MenuGroup `json:"menu"`
	FreeShippingCents int                  `json:"freeShippingCents"`
}

func Layout(c *tifa.Ctx) (Shell, error) {
	l, err := storekit.FindLocale(c.Language())
	if err != nil {
		return Shell{}, err
	}
	return Shell{Locale: l, Menu: products.Menu(l), FreeShippingCents: cart.FreeShipping(l)}, nil
}
