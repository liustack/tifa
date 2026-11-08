package orders

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/liustack/tifa"
)

// Placing a new order, with the rules for it.

// A NewLine is one product of a new order. The shop's catalogue gives its name, price, colour and
// stock.
type NewLine struct {
	Slug     string `json:"slug"`
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

// shop is what a project needs to place orders: the form of its order numbers and its products.
type shop struct {
	prefix    string // the order number is prefix, a dash and a number
	width     int    // the number is padded with zeros to this width
	catalogue []itemRecord
}

var shops = map[string]shop{
	"studio":  {prefix: "SH", width: 0, catalogue: studioCatalogue},
	"weekend": {prefix: "WK", width: 4, catalogue: weekendCatalogue},
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
		i := indexProduct(s.catalogue, l.Slug)
		if i < 0 {
			return "", tifa.Fields{"items": "Unknown product " + l.Slug}
		}
		items = append(items, item(s.catalogue[i], l.Quantity))
	}
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
	return id, nil
}

func indexProduct(catalogue []itemRecord, slug string) int {
	for i, p := range catalogue {
		if p.Slug == slug {
			return i
		}
	}
	return -1
}
