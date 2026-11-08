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
	line   func(NewLine) (itemRecord, error)
	name   func(slug string) (string, bool) // the name of a product it sells, as order lines keep it
	// stock is how many of a product it sells it has to sell now: the orders that wait to ship have
	// been taken out of it.
	stock func(slug string) (int, bool)
}

// The studio shop sells the products of the storefront (storekit.Project), as the catalogue has them
// at the time of the order. The weekend shop keeps a catalogue of its own.
var shops = map[string]shop{
	storekit.Project: {prefix: "SH", width: 0, line: storefrontLine, name: products.OrderName, stock: products.Stock},
	"weekend":        {prefix: "WK", width: 4, line: weekendLine, name: weekendName, stock: weekendStockOf},
}

// storefrontLine is the order line of a product of the storefront in a colour and size.
func storefrontLine(l NewLine) (itemRecord, error) {
	p, err := products.OrderLine(l.Slug, l.Color, l.Size)
	if errors.Is(err, tifa.ErrNotFound) {
		return itemRecord{}, tifa.Fields{"items": "Unknown product " + l.Slug}
	}
	if err != nil {
		return itemRecord{}, err
	}
	p.Quantity = l.Quantity
	return storefrontItem(p), nil
}

func storefrontItem(p products.Line) itemRecord {
	return itemRecord{
		Slug: p.Slug, Name: p.Name, Description: p.Variant, Quantity: p.Quantity, PriceCents: p.PriceFen, Color: p.Color,
	}
}

// weekendLine is the order line of a product of the weekend shop, which comes in one colour and size.
func weekendLine(l NewLine) (itemRecord, error) {
	for _, p := range weekendCatalogue {
		if p.Slug == l.Slug && l.Color == "" && l.Size == "" {
			return item(p, l.Quantity), nil
		}
	}
	return itemRecord{}, tifa.Fields{"items": "Unknown product " + l.Slug}
}

// weekendStockOf is how many of a product the weekend shop has to sell.
func weekendStockOf(slug string) (int, bool) {
	n, ok := weekendStock[slug]
	return n, ok
}

// weekendName is the name of a product of the weekend shop.
func weekendName(slug string) (string, bool) {
	for _, p := range weekendCatalogue {
		if p.Slug == slug {
			return p.Name, true
		}
	}
	return "", false
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
// highest of the project, written like the others. The caller checks the order with CheckNewOrder
// first.
func CreateOrder(projectID string, in NewOrder, at time.Time) (string, error) {
	s, ok := shops[projectID]
	if !ok {
		return "", tifa.ErrNotFound
	}
	var items []itemRecord
	for _, l := range in.Items {
		it, err := s.line(l)
		if err != nil {
			return "", err
		}
		items = append(items, it)
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
