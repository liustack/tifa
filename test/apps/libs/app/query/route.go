package query

import (
	"fmt"
	"math"
	"time"

	"example.com/libstest/app/seed"
	"github.com/liustack/tifa"
)

type Customer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type Item struct {
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

type Order struct {
	ID       string   `json:"id"`
	Number   int      `json:"number"`
	Customer Customer `json:"customer"`
	Status   string   `json:"status"`
	Total    float64  `json:"total"`
	Items    []Item   `json:"items"`
	PlacedAt string   `json:"placedAt"`
	Tags     []string `json:"tags"`
	Paid     bool     `json:"paid"`
}

type Me struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

type Stats struct {
	Revenue    float64 `json:"revenue"`
	Orders     int     `json:"orders"`
	Refunds    int     `json:"refunds"`
	Conversion float64 `json:"conversion"`
}

type Data struct {
	Me     Me      `json:"me"`
	Stats  Stats   `json:"stats"`
	Orders []Order `json:"orders"`
}

// Load returns what an admin page of orders fetches: the user, the stats and 300 orders. The page
// puts them into TanStack Query's cache, dehydrates it and writes it into the HTML.
func Load(c *tifa.Ctx) (Data, error) {
	m := seed.New(13)
	status := []string{"pending", "processing", "shipped", "delivered", "cancelled"}
	d := Data{
		Me:    Me{ID: "u_1", Name: "Ada Lovelace", Role: "admin", Permissions: []string{"orders:read", "orders:write", "refunds:write"}},
		Stats: Stats{Revenue: 123456.78, Orders: 300, Refunds: 12, Conversion: 0.0342},
	}
	for i := 0; i < 300; i++ {
		o := Order{ID: fmt.Sprintf("o_%d", 100000+i*7), Number: 4100 + i, Tags: []string{}}
		o.Customer = Customer{ID: fmt.Sprintf("c_%d", m.Int(90)), Name: m.Words(2), Email: fmt.Sprintf("c%d@example.com", i)}
		o.Status = seed.Pick(m, status)
		o.Total = math.Round(m.R()*90000) / 100
		for n := 1 + m.Int(4); n > 0; n-- {
			o.Items = append(o.Items, Item{SKU: fmt.Sprintf("SKU-%d", 1000+m.Int(9000)), Qty: 1 + m.Int(3)})
		}
		o.PlacedAt = time.Date(2026, time.Month(1+m.Int(7)), 1+m.Int(28), m.Int(24), m.Int(60), 0, 0, time.UTC).Format("2006-01-02T15:04:05.000Z")
		if m.R() < 0.3 {
			o.Tags = append(o.Tags, "gift")
		}
		o.Paid = m.R() < 0.8
		d.Orders = append(d.Orders, o)
	}
	return d, nil
}
