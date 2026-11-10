package orders

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store/products"
	"github.com/liustack/tifa/examples/shop/app/store/storekit"
)

// Placing a new order, with the rules for it.

// A NewLine is one product of a new order in a colour and size. The shop's catalogue gives its name,
// price and stock. Staff leave the colour and size empty, and the line takes the first colour of the
// product. A checkout of the storefront names the colour and size of each line of the cart.
type NewLine struct {
	Slug     string `json:"slug"`
	Color    string `json:"color"`
	Size     string `json:"size"`
	Quantity int    `json:"quantity"`
}

// NewOrder is what staff enter to place an order for a customer.
type NewOrder struct {
	Customer string    `json:"customer"`
	Phone    string    `json:"phone"`
	Email    string    `json:"email"`
	Address  string    `json:"address"`
	Note     string    `json:"note"` // what the customer wrote at checkout
	Items    []NewLine `json:"items"`
}

// shop is what a project needs to place orders: the form of its order numbers and the line of each
// product it sells.
type shop struct {
	prefix string // the order number is prefix, a dash and a number
	width  int    // the number is padded with zeros to this width
	// buy takes the pieces of the lines of a new order from the stock, or none of them, and returns
	// them as order lines.
	buy  func([]NewLine) ([]itemRecord, error)
	name func(slug string) (string, bool) // the name of a product it sells, as order lines keep it
	// stock is how many of a product it sells it has to sell now: the orders that wait to ship have
	// been taken out of it.
	stock func(slug string) (int, bool)
	photo func(slug string) (string, bool) // the folder of the photos of a product it sells
}

// The studio shop sells the products of the storefront (storekit.Project), as the catalogue has them
// at the time of the order. The weekend shop keeps a catalogue of its own.
var shops = map[string]shop{
	storekit.Project: {prefix: "SH", width: 0, buy: storefrontBuy, name: products.OrderName, stock: products.Stock, photo: products.Photo},
	"weekend":        {prefix: "WK", width: 4, buy: weekendBuy, name: weekendName, stock: weekendStockOf, photo: weekendPhoto},
}

// errNoStock is what placing an order of more than the stock answers, with the status a checkout
// of the storefront answers it with.
var errNoStock = tifa.Error{Status: 409, Message: "Not enough in stock"}

// storefrontBuy buys the lines of a new order of the studio shop from the catalogue of the
// storefront (products.Buy), as a checkout does.
func storefrontBuy(lines []NewLine) ([]itemRecord, error) {
	var takes []products.Take
	for _, l := range lines {
		takes = append(takes, products.Take{Slug: l.Slug, Color: l.Color, Size: l.Size, Quantity: l.Quantity})
	}
	bought, err := products.Buy(takes)
	switch {
	case errors.Is(err, products.ErrNoStock):
		return nil, errNoStock
	case errors.Is(err, tifa.ErrNotFound):
		for _, l := range lines {
			if _, ok := products.Stock(l.Slug); !ok {
				return nil, tifa.Fields{"items": "Unknown product " + l.Slug}
			}
		}
		return nil, err
	case err != nil:
		return nil, err
	}
	var items []itemRecord
	for _, l := range bought.Lines {
		items = append(items, storefrontItem(l))
	}
	return items, nil
}

func storefrontItem(p products.Line) itemRecord {
	return itemRecord{
		Slug: p.Slug, Photo: p.Photo, Name: p.Name, Description: p.Variant, Quantity: p.Quantity, PriceCents: p.PriceFen, Color: p.Color,
	}
}

// productName is the name of the product slug in the shop of a project as the shop has it now,
// which may not be the name an order of it was placed under. Every line of an order is a product
// its shop sells, and no shop stops selling one, so a name that is not there is a bug.
func productName(projectID, slug string) string {
	name, ok := shops[projectID].name(slug)
	if !ok {
		panic("orders of " + projectID + " hold " + slug + ", which the shop does not sell")
	}
	return name
}

// productStock is how many of the product slug the shop of a project has to sell now. Like a name,
// a stock that is not there is a bug.
func productStock(projectID, slug string) int {
	n, ok := shops[projectID].stock(slug)
	if !ok {
		panic("orders of " + projectID + " hold " + slug + ", which the shop does not sell")
	}
	return n
}

// productPhoto is the folder of the photos of the product slug in the shop of a project. Like a
// name, a photo that is not there is a bug.
func productPhoto(projectID, slug string) string {
	photo, ok := shops[projectID].photo(slug)
	if !ok {
		panic("orders of " + projectID + " hold " + slug + ", which the shop does not sell")
	}
	return photo
}

// CheckNewOrder returns the fields of a new order that do not follow the rules, or nil. It cannot
// tell whether a product is in the catalogue of the project, CreateOrder does.
func CheckNewOrder(in NewOrder) error {
	fields := tifa.Fields{}
	if err, ok := CheckRecipient(in.Customer, in.Phone, in.Address).(tifa.Fields); ok {
		fields = err
	}
	if utf8.RuneCountInString(strings.TrimSpace(in.Note)) > 200 {
		fields["note"] = "A note is at most 200 characters"
	}
	if n := len(in.Items); n < 1 || n > 3 {
		fields["items"] = "An order has 1 to 3 products"
	}
	for _, l := range in.Items {
		if l.Quantity < 1 || l.Quantity > 5 {
			fields["items"] = "Order 1 to 5 of each product"
		}
	}
	return fields.Err()
}

// CreateOrder places an order in a project at at and returns its number: the next one after the
// highest of the project, written like the others. It takes the pieces of the order from the stock
// of the shop as a checkout of the storefront does, and refuses an order of more than is left with a
// 409, taking nothing. The caller checks the order with CheckNewOrder first.
func CreateOrder(projectID string, in NewOrder, at time.Time) (string, error) {
	s, ok := shops[projectID]
	if !ok {
		return "", tifa.ErrNotFound
	}
	items, err := s.buy(in.Items)
	if err != nil {
		return "", err
	}
	return keep(projectID, s, in, items, at), nil
}

// CreateStoreOrder places an order of the storefront in the studio shop at at and returns its
// number, with the lines its checkout bought (products.Buy): they keep the names and prices the
// checkout totalled, whatever the catalogue says now. It leaves out in.Items.
func CreateStoreOrder(in NewOrder, lines []products.Line, at time.Time) string {
	var items []itemRecord
	for _, l := range lines {
		items = append(items, storefrontItem(l))
	}
	return keep(storekit.Project, shops[storekit.Project], in, items, at)
}

// keep keeps a new order of the shop s of a project with its lines, and returns its number.
func keep(projectID string, s shop, in NewOrder, items []itemRecord, at time.Time) string {
	ordersMu.Lock()
	defer ordersMu.Unlock()
	last := 0
	if p := orders[projectID]; p != nil {
		last = p.last
	}
	id := fmt.Sprintf("%s-%0*d", s.prefix, s.width, last+1)
	insert(orders, &orderRecord{
		ID: id, ProjectID: projectID, Customer: strings.TrimSpace(in.Customer), Phone: normalPhone(in.Phone),
		Email: strings.TrimSpace(in.Email), Address: strings.TrimSpace(in.Address), Note: strings.TrimSpace(in.Note),
		PlacedAt: at, Items: items,
	})
	return id
}

// Now is the instant a write of an order is made at (placing one, by staff or by a checkout,
// shipping it, a note): the time of the server, to the millisecond.
// Instants are kept to the millisecond, as JavaScript keeps them, so the ports of the shop in the
// comparison apps can make a write at the very instant the shop made it.
func Now() time.Time { return time.Now().Truncate(time.Millisecond) }
