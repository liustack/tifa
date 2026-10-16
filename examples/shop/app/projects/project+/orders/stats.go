package orders

import (
	"cmp"
	"slices"
	"time"
)

// Sales of a shop over the last thirty days, for the stats page.

// shopZone is the zone the shops trade in, China Standard Time. Their days start at midnight
// there. China has no daylight saving time, so a fixed zone needs no zoneinfo.
var shopZone = time.FixedZone("CST", 8*3600)

// statsDays is how many days the stats page covers.
const statsDays = 30

// DaySales are the orders placed on one day in the shop's zone. Shipped counts those of them
// that have shipped since.
type DaySales struct {
	Date         string `json:"date"` // 2026-10-13
	Orders       int    `json:"orders"`
	RevenueCents int    `json:"revenueCents"`
	Shipped      int    `json:"shipped"`
}

// ProductSales is how much of one product the shop sold.
type ProductSales struct {
	Name         string `json:"name"`
	Quantity     int    `json:"quantity"`
	RevenueCents int    `json:"revenueCents"`
}

// Sales are the daily sales of a shop, oldest first, and its five best selling products.
type Sales struct {
	Days        []DaySales     `json:"days"`
	TopProducts []ProductSales `json:"topProducts"`
}

// SalesStats returns the sales of a project over the thirty days that end on the day of its newest
// order. A real shop would end on today in its zone. The demonstration data does not move with
// the clock, so neither do its stats. A project without orders has no days.
func SalesStats(projectID string) Sales {
	s := Sales{Days: []DaySales{}, TopProducts: []ProductSales{}}
	list := ordersOf(projectID) // newest first
	if len(list) == 0 {
		return s
	}
	last := list[0].PlacedAt.In(shopZone)
	first := time.Date(last.Year(), last.Month(), last.Day()-(statsDays-1), 0, 0, 0, 0, shopZone)
	index := map[string]int{}
	for i := range statsDays {
		date := first.AddDate(0, 0, i).Format(time.DateOnly)
		index[date] = i
		s.Days = append(s.Days, DaySales{Date: date})
	}
	products := map[string]*ProductSales{}
	for _, o := range list {
		i, ok := index[o.PlacedAt.In(shopZone).Format(time.DateOnly)]
		if !ok {
			continue
		}
		d := &s.Days[i]
		d.Orders++
		d.RevenueCents += total(o)
		if o.Shipment != nil {
			d.Shipped++
		}
		for _, item := range o.Items {
			p := products[item.Name]
			if p == nil {
				p = &ProductSales{Name: item.Name}
				products[item.Name] = p
			}
			p.Quantity += item.Quantity
			p.RevenueCents += item.Quantity * item.PriceCents
		}
	}
	for _, p := range products {
		s.TopProducts = append(s.TopProducts, *p)
	}
	slices.SortFunc(s.TopProducts, func(a, b ProductSales) int {
		return cmp.Or(cmp.Compare(b.Quantity, a.Quantity), cmp.Compare(b.RevenueCents, a.RevenueCents), cmp.Compare(a.Name, b.Name))
	})
	if len(s.TopProducts) > 5 {
		s.TopProducts = s.TopProducts[:5]
	}
	return s
}
