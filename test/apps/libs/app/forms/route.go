package forms

import (
	"fmt"
	"strings"

	"example.com/libstest/app/seed"
	"github.com/liustack/tifa"
)

type Address struct {
	Line1    string `json:"line1,omitempty"`
	Line2    string `json:"line2,omitempty"`
	City     string `json:"city,omitempty"`
	Postcode string `json:"postcode,omitempty"`
	Country  string `json:"country,omitempty"`
}

type Item struct {
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

// Checkout is what the user sent: the page validates it with zod, as a server action does, and
// shows every problem next to its field.
type Checkout struct {
	Email        string  `json:"email"`
	Phone        string  `json:"phone"`
	FirstName    string  `json:"firstName"`
	LastName     string  `json:"lastName"`
	Company      string  `json:"company"`
	VAT          string  `json:"vat"`
	Shipping     Address `json:"shipping"`
	BillingSame  bool    `json:"billingSame"`
	Billing      Address `json:"billing"`
	Delivery     string  `json:"delivery"`
	DeliveryDate string  `json:"deliveryDate"`
	Instructions string  `json:"instructions"`
	Coupon       string  `json:"coupon"`
	GiftWrap     bool    `json:"giftWrap"`
	GiftMessage  string  `json:"giftMessage"`
	Newsletter   bool    `json:"newsletter"`
	Terms        bool    `json:"terms"`
	Items        []Item  `json:"items"`
	CardName     string  `json:"cardName"`
	CardNumber   string  `json:"cardNumber"`
	CardExpiry   string  `json:"cardExpiry"`
	CardCVC      string  `json:"cardCvc"`
}

// Contact is one row of a file being imported, as text.
type Contact struct {
	Name    string   `json:"name"`
	Email   string   `json:"email"`
	Age     string   `json:"age"`
	Tags    []string `json:"tags"`
	Website string   `json:"website,omitempty"`
	Joined  string   `json:"joined"`
}

type Data struct {
	Sent     Checkout  `json:"sent"`
	Contacts []Contact `json:"contacts"`
}

// Load returns a checkout with a dozen mistakes and 150 contacts, 40% of them with a problem.
func Load(c *tifa.Ctx) (Data, error) {
	d := Data{Sent: Checkout{
		Email: "ada@example", Phone: "+44 20 7946 0958", FirstName: "Ada", Company: "Analytical Engines Ltd", VAT: "GB12345",
		Shipping: Address{Line1: "12", Line2: "Flat 3", City: "London", Postcode: "SW1A 1AA", Country: "UK"}, Billing: Address{City: "Paris"},
		Delivery: "express", DeliveryDate: "2026-07-3", Instructions: strings.Repeat("Leave it with the neighbour at number 14, ", 6), Coupon: "summer",
		GiftWrap: true, Newsletter: true,
		Items:    []Item{{"SKU-1234", 2}, {"SKU-99", 1}, {"SKU-5555", 12}, {"SKU-0042", 1}},
		CardName: "A L", CardNumber: "4242 4242 4242 424", CardExpiry: "13/27", CardCVC: "12",
	}}
	m := seed.New(19)
	for i := 0; i < 150; i++ {
		bad := m.R() < 0.4
		c := Contact{Name: m.Words(2), Email: fmt.Sprintf("user%d@example.com", i), Age: fmt.Sprint(18 + m.Int(60)), Tags: []string{"lead"}}
		if bad && m.R() < 0.3 {
			c.Name = "x"
		}
		if bad && m.R() < 0.5 {
			c.Email = fmt.Sprintf("user%d@", i)
		}
		if bad && m.R() < 0.4 {
			c.Age = fmt.Sprint(10 + m.Int(8))
		}
		if m.R() < 0.1 {
			c.Tags = []string{"vip", "boss"}
		}
		if m.R() < 0.5 {
			c.Website = fmt.Sprintf("https://example.com/%d", i)
			if bad && m.R() < 0.5 {
				c.Website = "not a url"
			}
		}
		c.Joined = fmt.Sprintf("2026-0%d-%d", 1+m.Int(9), 10+m.Int(19))
		d.Contacts = append(d.Contacts, c)
	}
	return d, nil
}
