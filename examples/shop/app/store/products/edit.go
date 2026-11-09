package products

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store/storekit"
)

// What staff change in the back office, and what orders take from the catalogue.

// Edit is what staff can change of a product: the price in fen, the stock, and the title and
// description in both languages. The back office checks it with the same rules before sending it
// (the schema of its edit page), and Validate checks it again here.
type Edit struct {
	PriceCents    int    `json:"priceCents"`
	Stock         int    `json:"stock"`
	NameEN        string `json:"nameEn"`
	NameZH        string `json:"nameZh"`
	DescriptionEN string `json:"descriptionEn"`
	DescriptionZH string `json:"descriptionZh"`
}

// Entry is a product as the back office lists and edits it.
type Entry struct {
	Edit
	Slug   string `json:"slug"`
	Number int    `json:"number"`
	// Category is the name of the category in English, the language of the back office.
	Category string `json:"category"`
}

// The limits of an edit. Prices are in fen.
const (
	minPriceFen    = 100       // 1 yuan
	maxPriceFen    = 9_999_900 // 99,999 yuan
	maxStock       = 99_999
	maxName        = 60
	maxDescription = 1000
)

// Validate checks an edit field by field, after trimming the spaces around the texts.
func (e Edit) Validate() error {
	f := tifa.Fields{}
	if e.PriceCents < minPriceFen || e.PriceCents > maxPriceFen {
		f["priceCents"] = "Enter a price from ¥1 to ¥99,999"
	}
	if e.Stock < 0 || e.Stock > maxStock {
		f["stock"] = "Enter a stock from 0 to 99,999"
	}
	for _, t := range []struct {
		field, value, empty, long string
		max                       int
	}{
		{"nameEn", e.NameEN, "Enter the English title", "A title is at most 60 characters", maxName},
		{"nameZh", e.NameZH, "Enter the Chinese title", "A title is at most 60 characters", maxName},
		{"descriptionEn", e.DescriptionEN, "Enter the English description", "A description is at most 1,000 characters", maxDescription},
		{"descriptionZh", e.DescriptionZH, "Enter the Chinese description", "A description is at most 1,000 characters", maxDescription},
	} {
		switch v := strings.TrimSpace(t.value); {
		case v == "":
			f[t.field] = t.empty
		case utf8.RuneCountInString(v) > t.max:
			f[t.field] = t.long
		}
	}
	return f.Err()
}

// Update changes a product to what an edit says, or returns tifa.ErrNotFound. The edit must have
// passed Validate. The next request of every storefront page shows the change.
func Update(slug string, e Edit) error { return Apply(slug, ChangeOf(e)) }

// Change is an edit that changes only the fields it has, as the PATCH of the back office takes it:
// a field that is missing or null keeps its value. So an edit of the price leaves the stock as the
// checkouts since left it, and two edits of other fields never undo each other.
type Change struct {
	PriceCents    *int    `json:"priceCents"`
	Stock         *int    `json:"stock"`
	NameEN        *string `json:"nameEn"`
	NameZH        *string `json:"nameZh"`
	DescriptionEN *string `json:"descriptionEn"`
	DescriptionZH *string `json:"descriptionZh"`
}

// ChangeOf is the change that sets every field to what e says.
func ChangeOf(e Edit) Change {
	return Change{&e.PriceCents, &e.Stock, &e.NameEN, &e.NameZH, &e.DescriptionEN, &e.DescriptionZH}
}

// Over returns e with the fields the change has in place of its own.
func (c Change) Over(e Edit) Edit {
	set := func(dst *int, src *int) {
		if src != nil {
			*dst = *src
		}
	}
	text := func(dst *string, src *string) {
		if src != nil {
			*dst = *src
		}
	}
	set(&e.PriceCents, c.PriceCents)
	set(&e.Stock, c.Stock)
	text(&e.NameEN, c.NameEN)
	text(&e.NameZH, c.NameZH)
	text(&e.DescriptionEN, c.DescriptionEN)
	text(&e.DescriptionZH, c.DescriptionZH)
	return e
}

// validEdit passes Validate: a change over it fails only on the fields the change has.
var validEdit = Edit{PriceCents: minPriceFen, NameEN: "-", NameZH: "-", DescriptionEN: "-", DescriptionZH: "-"}

// Validate checks the fields the change has, with the rules of Edit.Validate.
func (c Change) Validate() error { return c.Over(validEdit).Validate() }

// Apply changes the fields of a product that a change has, under the lock that checkouts take
// their stock under, or returns tifa.ErrNotFound. The change must have passed Validate. The next
// request of every storefront page shows it.
func Apply(slug string, c Change) error {
	catalogueMu.Lock()
	defer catalogueMu.Unlock()
	i := slices.IndexFunc(catalogue, func(p productRecord) bool { return p.slug == slug })
	if i < 0 {
		return tifa.ErrNotFound
	}
	p := &catalogue[i]
	e := c.Over(p.entry().Edit)
	p.priceFen, p.stock = e.PriceCents, e.Stock
	p.name = T{strings.TrimSpace(e.NameZH), strings.TrimSpace(e.NameEN)}
	p.description = T{strings.TrimSpace(e.DescriptionZH), strings.TrimSpace(e.DescriptionEN)}
	return nil
}

// Entries returns every product of the catalogue, in its order.
func Entries() []Entry {
	var out []Entry
	for _, p := range records() {
		out = append(out, p.entry())
	}
	return out
}

// GetEntry returns the product slug, or tifa.ErrNotFound.
func GetEntry(slug string) (Entry, error) {
	p, ok := record(slug)
	if !ok {
		return Entry{}, tifa.ErrNotFound
	}
	return p.entry(), nil
}

func (p productRecord) entry() Entry {
	e := Entry{
		Edit: Edit{
			PriceCents: p.priceFen, Stock: p.stock, NameEN: p.name[1], NameZH: p.name[0],
			DescriptionEN: p.description[1], DescriptionZH: p.description[0],
		},
		Slug: p.slug, Number: p.number,
	}
	for _, c := range categories {
		if c.id == p.category {
			e.Category = c.name[1]
		}
	}
	return e
}

// ErrNoStock is the error of Buy when a product has fewer pieces than a checkout takes.
var ErrNoStock = errors.New("not enough in stock")

// Take is how many pieces of a product in a colour and size a checkout takes from the stock.
type Take struct {
	Slug, Color, Size string
	Quantity          int
}

// A Purchase is what a checkout bought, as the catalogue had it when the checkout took its stock.
// The checkout totals its order and places it from the purchase, so an edit of staff at the same
// time changes neither.
type Purchase struct {
	Lines    []Line                   // one for each take, in their order, with its quantity
	products map[string]productRecord // by slug
}

// Product returns a product of the purchase in the language of l, as GetProduct returned it when
// the purchase was made, or tifa.ErrNotFound for one it does not hold.
func (b Purchase) Product(slug string, l storekit.Locale) (Product, error) {
	p, ok := b.products[slug]
	if !ok {
		return Product{}, tifa.ErrNotFound
	}
	return p.product(l), nil
}

// Buy takes the pieces of every take from the stock, or none of them, and returns what it bought,
// all under one lock of the catalogue. It returns ErrNoStock when a product has fewer pieces than
// its takes add up to, tifa.ErrNotFound for a product the catalogue does not have, and the error of
// line (tifa.Fields) for a colour or size the product does not come in.
func Buy(takes []Take) (Purchase, error) {
	catalogueMu.Lock()
	defer catalogueMu.Unlock()
	out := Purchase{products: map[string]productRecord{}}
	want := map[int]int{} // pieces, by the index of the product in the catalogue
	for _, t := range takes {
		i := slices.IndexFunc(catalogue, func(p productRecord) bool { return p.slug == t.Slug })
		if i < 0 {
			return Purchase{}, tifa.ErrNotFound
		}
		want[i] += t.Quantity
		if want[i] > catalogue[i].stock {
			return Purchase{}, ErrNoStock
		}
		line, err := catalogue[i].line(t.Color, t.Size)
		if err != nil {
			return Purchase{}, err
		}
		line.Quantity = t.Quantity
		out.Lines = append(out.Lines, line)
	}
	for i, n := range want {
		catalogue[i].stock -= n
		out.products[catalogue[i].slug] = catalogue[i]
	}
	return out, nil
}

// Line is a product in one colour and size as a line of a back office order keeps it: its English
// name (the back office is in English), the colour and size as one text and the price in fen.
type Line struct {
	Slug     string
	Name     string
	Variant  string // such as "Ink / M"
	Color    string // the ID of the colour
	PriceFen int
	Quantity int // the pieces a purchase bought (see Buy)
}

// line is the product in a colour and size as an order line. Staff who place an order name no
// colour, and the line takes the first colour of the product and no size. A colour or size the
// product does not come in is an error.
func (p productRecord) line(color, size string) (Line, error) {
	if color == "" && size == "" {
		color = p.colors[0]
	} else if !slices.Contains(p.colors, color) || !(len(p.sizes) == 0 && size == "" || slices.Contains(p.sizes, size)) {
		return Line{}, tifa.Fields{"items": "Unknown colour or size of " + p.slug}
	}
	variant := colorText(color)[1]
	if size != "" {
		variant += " / " + size
	}
	return Line{Slug: p.slug, Name: p.name[1], Variant: variant, Color: color, PriceFen: p.priceFen}, nil
}

// Stock is how many of the product slug the shop has to sell now, and false for a product the
// catalogue does not have. The orders that wait to ship have been taken out of it.
func Stock(slug string) (int, bool) {
	p, ok := record(slug)
	return p.stock, ok
}

// OrderName is the name of the product slug as the lines of an order keep it, its English name as
// the catalogue has it now (the back office is in English), and false for a product the catalogue
// does not have.
func OrderName(slug string) (string, bool) {
	p, ok := record(slug)
	return p.name[1], ok
}

// colorText is the name of a colour in both languages.
func colorText(id string) T {
	for _, c := range colors {
		if c.id == id {
			return c.name
		}
	}
	return T{id, id}
}
