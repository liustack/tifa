package products

import (
	"slices"

	"github.com/liustack/tifa/examples/shop/app/store"
)

// MenuGroup is one category of the store's category menu: its kinds of products, how many
// products it has, and the products the menu shows beside them.
type MenuGroup struct {
	Category
	Count    int      `json:"count"`
	Kinds    []string `json:"kinds"`
	Featured []Card   `json:"featured"`
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
func Menu(l store.Locale) []MenuGroup {
	var out []MenuGroup
	for _, c := range categories {
		g := MenuGroup{Category: Category{ID: c.id, Name: c.name.In(l)}}
		var in []productRecord
		for _, p := range catalogue {
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
			g.Featured = append(g.Featured, p.card(l))
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
