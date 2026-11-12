package products

import (
	"slices"

	"github.com/liustack/tifa/examples/shop/app/store/storekit"
)

// MenuGroup is one category of the store's category menu: its kinds of products, how many
// products it has, and the products the menu shows beside them.
type MenuGroup struct {
	Category
	Count    int           `json:"count"`
	Kinds    []string      `json:"kinds"`
	Featured []MenuProduct `json:"featured"`
}

// MenuProduct is a product as the menu shows it beside a category: its photo, number, name and
// price. The menu is on every page of the store and says nothing of the stock, so it gets none of
// it, and a sale changes the Layout of no page.
type MenuProduct struct {
	Slug           string `json:"slug"`
	Photo          string `json:"photo"`
	Name           string `json:"name"`
	Category       string `json:"category"`
	PriceCents     int    `json:"priceCents"`
	CompareAtCents int    `json:"compareAtCents"`
	Number         int    `json:"number"`
}

// kinds are the kinds of products in each category, as the menu lists them.
var kinds = map[string][]T{
	"drinkware":  {{"手冲器具", "Pour-over"}, {"茶具", "Tea things"}, {"杯子", "Cups"}, {"花器", "Vases"}, {"托盘", "Trays"}},
	"textiles":   {{"盖毯", "Throws"}, {"餐巾与茶巾", "Napkins and towels"}, {"围裙", "Aprons"}, {"杯垫", "Coasters"}, {"包袋", "Bags"}},
	"stationery": {{"钢笔", "Pens"}, {"笔记本", "Notebooks"}, {"书签", "Bookmarks"}, {"桌面收纳", "Desk trays"}},
	"fragrance":  {{"香薰蜡烛", "Candles"}, {"扩香", "Diffusers"}, {"线香", "Incense"}, {"喷雾", "Room sprays"}},
	"outdoor":    {{"营灯", "Lanterns"}, {"折叠家具", "Folding furniture"}, {"野餐", "Picnic"}, {"水壶与杯", "Bottles and mugs"}},
}

// Menu returns the category menu: each category with its kinds and its two best rated products.
func Menu(l storekit.Locale) []MenuGroup {
	all := records()
	var out []MenuGroup
	for _, c := range categories {
		g := MenuGroup{Category: Category{ID: c.id, Name: c.name.In(l)}}
		var in []productRecord
		for _, p := range all {
			if p.category == c.id {
				in = append(in, p)
			}
		}
		g.Count = len(in)
		for _, k := range kinds[c.id] {
			g.Kinds = append(g.Kinds, k.In(l))
		}
		slices.SortStableFunc(in, func(a, b productRecord) int { return compareRating(b, a) })
		for _, p := range in[:min(2, len(in))] {
			c := p.card(l)
			g.Featured = append(g.Featured, MenuProduct{
				Slug: c.Slug, Photo: c.Photo, Name: c.Name, Category: c.Category, PriceCents: c.PriceCents, CompareAtCents: c.CompareAtCents,
				Number: c.Number,
			})
		}
		out = append(out, g)
	}
	return out
}

func compareRating(a, b productRecord) int {
	switch {
	case a.rating < b.rating:
		return -1
	case a.rating > b.rating:
		return 1
	}
	return 0
}

// BundleItem is a product the page of another offers to buy with it: its photo, number, name and
// price, and Color, the ID of the colour it goes into the cart in. Only products in stock are
// offered, so it says nothing of the stock.
type BundleItem struct {
	Slug       string `json:"slug"`
	Photo      string `json:"photo"`
	Name       string `json:"name"`
	PriceCents int    `json:"priceCents"`
	Number     int    `json:"number"`
	Color      string `json:"color"`
}

// Bundle returns up to n products to buy together with slug: others of its category that come in
// one size, best rated first, so each goes into the cart in its first colour.
func Bundle(slug string, n int, l storekit.Locale) []BundleItem {
	self, _ := record(slug)
	var in []productRecord
	for _, p := range records() {
		if p.category == self.category && p.slug != slug && len(p.sizes) == 0 && p.stock > 0 {
			in = append(in, p)
		}
	}
	slices.SortStableFunc(in, func(a, b productRecord) int { return compareRating(b, a) })
	var out []BundleItem
	for _, p := range in[:min(n, len(in))] {
		c := p.card(l)
		out = append(out, BundleItem{Slug: c.Slug, Photo: c.Photo, Name: c.Name, PriceCents: c.PriceCents, Number: c.Number, Color: p.colors[0]})
	}
	return out
}
