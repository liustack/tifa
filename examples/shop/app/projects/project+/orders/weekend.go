package orders

import (
	"slices"
	"strings"
	"sync"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store/products"
	"github.com/liustack/tifa/examples/shop/app/store/storekit"
)

// The weekend shop sells a catalogue of its own. Staff change its products in the back office as
// they change those of the storefront, with the same edit, and every order takes its pieces from
// their stock.

// weekendProduct is a product of the weekend shop as it sells it now. It comes in one colour and
// size, which variant names as order lines keep it.
type weekendProduct struct {
	products.Edit
	slug, variant, color string
}

var (
	weekendMu    sync.Mutex
	weekendShelf = newWeekendShelf()
)

// newWeekendShelf is the weekend shop as the demonstration data starts it: the products its orders
// were sold at (weekendCatalogue), with the stock it has to sell, the orders that wait taken out.
func newWeekendShelf() []weekendProduct {
	texts := map[string][3]string{ // the Chinese title, and the description in both languages
		"canvas-tote":      {"周末帆布袋", "厚帆布托特包，装得下一天的出行。", "A heavy canvas tote that holds a day out."},
		"folding-stool":    {"露营折叠凳", "铝合金折叠凳，收起来只有一本书大。", "An aluminium stool that folds to the size of a book."},
		"picnic-blanket":   {"野餐垫", "防水底的格纹野餐垫。", "A checked picnic blanket with a waterproof back."},
		"insulated-bottle": {"保温水壶", "双层不锈钢，冷热都能保持 12 小时。", "Double-walled steel that keeps a drink hot or cold for 12 hours."},
		"postcards":        {"手绘明信片", "六张城市手绘明信片。", "Six hand-drawn postcards of cities."},
	}
	var out []weekendProduct
	for _, p := range weekendCatalogue {
		t := texts[p.Slug]
		out = append(out, weekendProduct{
			Edit: products.Edit{
				PriceCents: p.PriceCents, Stock: weekendStock[p.Slug], NameZH: t[0], NameEN: p.Name, DescriptionZH: t[1], DescriptionEN: t[2],
			},
			slug: p.Slug, variant: p.Description, color: p.Color,
		})
	}
	return out
}

// WeekendEntries returns every product of the weekend shop, in its order, as the back office lists
// them.
func WeekendEntries() []products.Entry {
	weekendMu.Lock()
	defer weekendMu.Unlock()
	var out []products.Entry
	for i, p := range weekendShelf {
		out = append(out, products.Entry{Edit: p.Edit, Slug: p.slug, Number: i + 1})
	}
	return out
}

// GetWeekendEntry returns the product slug of the weekend shop, or tifa.ErrNotFound.
func GetWeekendEntry(slug string) (products.Entry, error) {
	for _, e := range WeekendEntries() {
		if e.Slug == slug {
			return e, nil
		}
	}
	return products.Entry{}, tifa.ErrNotFound
}

// UpdateWeekend changes the fields of a product of the weekend shop that a change has, under the lock
// that orders take their stock under, or returns tifa.ErrNotFound. The change must have passed
// Validate. Orders placed before keep what they were sold at.
func UpdateWeekend(slug string, c products.Change) error {
	weekendMu.Lock()
	defer weekendMu.Unlock()
	i := slices.IndexFunc(weekendShelf, func(p weekendProduct) bool { return p.slug == slug })
	if i < 0 {
		return tifa.ErrNotFound
	}
	e := c.Over(weekendShelf[i].Edit)
	e.NameEN, e.NameZH = strings.TrimSpace(e.NameEN), strings.TrimSpace(e.NameZH)
	e.DescriptionEN, e.DescriptionZH = strings.TrimSpace(e.DescriptionEN), strings.TrimSpace(e.DescriptionZH)
	weekendShelf[i].Edit = e
	return nil
}

// weekendBuy takes the pieces of the lines of a new order from the stock of the weekend shop, or
// none of them, and returns them as order lines, all under one lock, as products.Buy does for the
// storefront. A product it does not sell, or a colour or size (it has one of each), is an error of
// the field, and more than the stock is a 409.
func weekendBuy(lines []NewLine) ([]itemRecord, error) {
	weekendMu.Lock()
	defer weekendMu.Unlock()
	want := map[int]int{}
	var out []itemRecord
	for _, l := range lines {
		i := slices.IndexFunc(weekendShelf, func(p weekendProduct) bool { return p.slug == l.Slug })
		if i < 0 || l.Color != "" || l.Size != "" {
			return nil, tifa.Fields{"items": "Unknown product " + l.Slug}
		}
		want[i] += l.Quantity
		if want[i] > weekendShelf[i].Stock {
			return nil, errNoStock
		}
		p := weekendShelf[i]
		out = append(out, itemRecord{Slug: p.slug, Name: p.NameEN, Description: p.variant, Quantity: l.Quantity, PriceCents: p.PriceCents, Color: p.color})
	}
	for i, n := range want {
		weekendShelf[i].Stock -= n
	}
	return out, nil
}

// weekendStockOf is how many of a product the weekend shop has to sell.
func weekendStockOf(slug string) (int, bool) {
	e, err := GetWeekendEntry(slug)
	return e.Stock, err == nil
}

// weekendName is the name of a product of the weekend shop, as it sells it now.
func weekendName(slug string) (string, bool) {
	e, err := GetWeekendEntry(slug)
	return e.NameEN, err == nil
}

// ShopEntries returns the products of the shop of a project as the back office lists them: the
// catalogue of the storefront for the studio shop, its own for the weekend shop. A project with no
// shop is tifa.ErrNotFound.
func ShopEntries(projectID string) ([]products.Entry, error) {
	switch projectID {
	case storekit.Project:
		return products.Entries(), nil
	case "weekend":
		return WeekendEntries(), nil
	}
	return nil, tifa.ErrNotFound
}

// ShopEntry returns the product slug of the shop of a project, or tifa.ErrNotFound.
func ShopEntry(projectID, slug string) (products.Entry, error) {
	switch projectID {
	case storekit.Project:
		return products.GetEntry(slug)
	case "weekend":
		return GetWeekendEntry(slug)
	}
	return products.Entry{}, tifa.ErrNotFound
}

// UpdateShopProduct changes the fields of the product slug of the shop of a project that a change
// has, or returns tifa.ErrNotFound. The change must have passed Validate.
func UpdateShopProduct(projectID, slug string, c products.Change) error {
	switch projectID {
	case storekit.Project:
		return products.Apply(slug, c)
	case "weekend":
		return UpdateWeekend(slug, c)
	}
	return tifa.ErrNotFound
}
