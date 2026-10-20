// Package admin is the data of a small MUI admin: orders of a shop with customers and items.
package admin

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/liustack/tifa"
)

type Item struct {
	SKU        string `json:"sku"`
	Name       string `json:"name"`
	Quantity   int    `json:"quantity"`
	PriceCents int    `json:"priceCents"`
}

type Order struct {
	ID         string    `json:"id"`
	Customer   string    `json:"customer"`
	Email      string    `json:"email"`
	City       string    `json:"city"`
	Status     string    `json:"status"` // "pending", "paid", "shipped", "refunded"
	Channel    string    `json:"channel"`
	PlacedAt   time.Time `json:"placedAt"`
	TotalCents int       `json:"totalCents"`
	Items      []Item    `json:"items"`
	Notes      string    `json:"notes"`
}

var (
	firsts   = []string{"Olivia", "Liam", "Emma", "Noah", "Ava", "Elijah", "Sophia", "Lucas", "Mia", "Mateo", "Amelia", "Levi", "Harper", "Ezra", "Evelyn"}
	lasts    = []string{"Smith", "Johnson", "Garcia", "Brown", "Davis", "Miller", "Wilson", "Moore", "Taylor", "Lee", "Martin", "Clark"}
	cities   = []string{"Austin", "Denver", "Seattle", "Portland", "Chicago", "Boston", "Atlanta", "Phoenix"}
	statuses = []string{"pending", "paid", "paid", "shipped", "shipped", "shipped", "refunded"}
	channels = []string{"Web", "Mobile", "Marketplace", "Retail"}
	products = []Item{
		{"MUG-01", "Stoneware mug", 0, 1800}, {"KTL-02", "Pour-over kettle", 0, 6400}, {"TWL-03", "Linen tea towel", 0, 1200},
		{"CND-04", "Soy candle", 0, 2200}, {"BAG-05", "Canvas tote", 0, 2600}, {"NTB-06", "Dot grid notebook", 0, 1400},
		{"TRY-07", "Oak tray", 0, 4800}, {"VAS-08", "Bud vase", 0, 3200},
	}
)

var orders = func() []Order {
	r := rand.New(rand.NewPCG(7, 9))
	start := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	out := make([]Order, 240)
	for i := range out {
		f, l := firsts[r.IntN(len(firsts))], lasts[r.IntN(len(lasts))]
		o := Order{
			ID: fmt.Sprintf("ORD-%04d", 1240-i), Customer: f + " " + l,
			Email: fmt.Sprintf("%s.%s@example.com", lower(f), lower(l)), City: cities[r.IntN(len(cities))],
			Status: statuses[r.IntN(len(statuses))], Channel: channels[r.IntN(len(channels))],
			PlacedAt: start.Add(time.Duration(240-i) * 4 * time.Hour),
		}
		for range 1 + r.IntN(4) {
			it := products[r.IntN(len(products))]
			it.Quantity = 1 + r.IntN(3)
			o.Items = append(o.Items, it)
			o.TotalCents += it.Quantity * it.PriceCents
		}
		if i%5 == 0 {
			o.Notes = "Leave at the front desk."
		}
		out[i] = o
	}
	return out
}()

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// Page is one page of the order list.
type Page struct {
	Orders []Order `json:"orders"`
	Total  int     `json:"total"`
	Page   int     `json:"page"`
	Size   int     `json:"size"`
}

func List(status string, page, size int) Page {
	var found []Order
	for _, o := range orders {
		if status == "" || o.Status == status {
			found = append(found, o)
		}
	}
	from := min(page*size, len(found))
	return Page{Orders: found[from:min(from+size, len(found))], Total: len(found), Page: page, Size: size}
}

func Get(id string) (Order, error) {
	for _, o := range orders {
		if o.ID == id {
			return o, nil
		}
	}
	return Order{}, tifa.ErrNotFound
}

// Stats are the numbers of the dashboard.
type Stats struct {
	RevenueCents int            `json:"revenueCents"`
	Orders       int            `json:"orders"`
	Customers    int            `json:"customers"`
	RefundRate   float64        `json:"refundRate"`
	ByStatus     map[string]int `json:"byStatus"`
	ByChannel    []Share        `json:"byChannel"`
	Recent       []Order        `json:"recent"`
	TopProducts  []Share        `json:"topProducts"`
}

type Share struct {
	Name    string  `json:"name"`
	Value   int     `json:"value"`
	Percent float64 `json:"percent"`
}

func Dashboard() Stats {
	s := Stats{ByStatus: map[string]int{}, Orders: len(orders), Recent: orders[:12]}
	customers := map[string]bool{}
	channel := map[string]int{}
	product := map[string]int{}
	for _, o := range orders {
		s.RevenueCents += o.TotalCents
		s.ByStatus[o.Status]++
		customers[o.Email] = true
		channel[o.Channel] += o.TotalCents
		for _, it := range o.Items {
			product[it.Name] += it.Quantity
		}
	}
	s.Customers = len(customers)
	s.RefundRate = float64(s.ByStatus["refunded"]) / float64(len(orders))
	for _, c := range channels {
		s.ByChannel = append(s.ByChannel, Share{c, channel[c], float64(channel[c]) / float64(s.RevenueCents)})
	}
	maxQ := 0
	for _, p := range products {
		maxQ = max(maxQ, product[p.Name])
	}
	for _, p := range products {
		s.TopProducts = append(s.TopProducts, Share{p.Name, product[p.Name], float64(product[p.Name]) / float64(maxQ)})
	}
	return s
}
