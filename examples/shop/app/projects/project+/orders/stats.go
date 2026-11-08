package orders

import (
	"cmp"
	"slices"
	"time"
)

// Sales of a shop over the last thirty days, for the stats page.

// statsDays is how many days the stats page covers.
const statsDays = 30

// DaySales are the orders placed on one day in the shop's time zone. Shipped counts those of them
// that have shipped since.
type DaySales struct {
	Date         string `json:"date"` // 2026-10-13
	Orders       int    `json:"orders"`
	RevenueCents int    `json:"revenueCents"`
	Shipped      int    `json:"shipped"`
	Pieces       int    `json:"pieces"`
}

// ProductSales is how much of one product the shop sold.
type ProductSales struct {
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Quantity     int    `json:"quantity"`
	RevenueCents int    `json:"revenueCents"`
}

// ProductRow is one product in the ranking of everything the shop sold: how many pieces in how
// many orders, and the pieces on each of the days, oldest first.
type ProductRow struct {
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Quantity     int    `json:"quantity"`
	Orders       int    `json:"orders"`
	RevenueCents int    `json:"revenueCents"`
	Daily        []int  `json:"daily"`
}

// CustomerSales is what one customer bought from the shop.
type CustomerSales struct {
	Name       string `json:"name"`
	Initials   string `json:"initials"`
	Orders     int    `json:"orders"`
	SpentCents int    `json:"spentCents"`
}

// DispatchBand is how many orders the shop handed to the carrier within a time of being placed.
type DispatchBand struct {
	Label  string `json:"label"`
	Orders int    `json:"orders"`
}

// Totals are the numbers of the whole shop that the days do not hold.
type Totals struct {
	Pieces           int     `json:"pieces"`
	PendingOrders    int     `json:"pendingOrders"`
	PendingCents     int     `json:"pendingCents"`
	AvgDispatchHours float64 `json:"avgDispatchHours"`
	Customers        int     `json:"customers"`
	Repeat           int     `json:"repeat"` // customers with more than one order
}

// Sales are the daily sales of a shop, oldest first, its five best selling products for the
// chart, and the rest of what its stats page shows.
type Sales struct {
	Days        []DaySales      `json:"days"`
	TopProducts []ProductSales  `json:"topProducts"`
	Products    []ProductRow    `json:"products"`
	Carriers    []CarrierSales  `json:"carriers"`
	Customers   []CustomerSales `json:"customers"`
	// Heat counts the orders by the day of the week, Monday first, and the two hours of the day
	// they were placed in, in the time zone of the shop.
	Heat     [][]int        `json:"heat"`
	Dispatch []DispatchBand `json:"dispatch"`
	Recent   []OrderRow     `json:"recent"`
	Totals   Totals         `json:"totals"`
}

// SalesStats returns the sales of a project over the thirty days that end on the day of its newest
// order, with days starting at midnight in zone, the time zone of the shop. A real shop would end
// on today. The demonstration data does not move with the clock, so neither do its stats. A
// project without orders has no days. Everything comes from the totals the writes keep, so the
// stats cost the same however many orders there are.
func SalesStats(projectID string, zone *time.Location) Sales {
	ordersMu.RLock()
	defer ordersMu.RUnlock()
	p := ordersOf(projectID)
	s := Sales{Days: []DaySales{}, TopProducts: []ProductSales{}, Products: []ProductRow{}, Carriers: carrierStats(p), Customers: []CustomerSales{}, Heat: make([][]int, 7), Dispatch: dispatchBands(), Recent: []OrderRow{}}
	for i := range s.Heat {
		s.Heat[i] = make([]int, 12)
	}
	if len(p.all) == 0 {
		return s
	}
	z := p.daysIn(zone)
	for i := range s.Heat {
		copy(s.Heat[i], z.heat[i][:])
	}
	// An order belongs to the day of its time in zone. The dates themselves are only labels of
	// the calendar, counted in UTC: in a zone whose daylight saving time starts at midnight, such
	// as America/Santiago, that day has no 00:00, and counting there would list the day before
	// twice and leave that day out.
	last := p.all[len(p.all)-1].PlacedAt.In(zone)
	first := time.Date(last.Year(), last.Month(), last.Day()-(statsDays-1), 0, 0, 0, 0, time.UTC)
	products := map[string]*ProductRow{}
	for i := range statsDays {
		date := first.AddDate(0, 0, i).Format(time.DateOnly)
		day := DaySales{Date: date}
		if d := z.days[date]; d != nil {
			day.Orders, day.RevenueCents, day.Shipped, day.Pieces = d.orders, d.revenueCents, d.shipped, d.pieces
			s.Totals.Pieces += d.pieces
			for name, dp := range d.products {
				r := products[name]
				if r == nil {
					r = &ProductRow{Slug: dp.slug, Name: name, Daily: make([]int, statsDays)}
					products[name] = r
				}
				r.Quantity += dp.quantity
				r.Orders += dp.lines
				r.RevenueCents += dp.revenueCents
				r.Daily[i] += dp.quantity
			}
		}
		s.Days = append(s.Days, day)
	}
	for _, r := range products {
		s.Products = append(s.Products, *r)
		s.TopProducts = append(s.TopProducts, ProductSales{Slug: r.Slug, Name: r.Name, Quantity: r.Quantity, RevenueCents: r.RevenueCents})
	}
	slices.SortFunc(s.Products, func(a, b ProductRow) int {
		return cmp.Or(cmp.Compare(b.Quantity, a.Quantity), cmp.Compare(b.RevenueCents, a.RevenueCents), cmp.Compare(a.Name, b.Name))
	})
	slices.SortFunc(s.TopProducts, func(a, b ProductSales) int {
		return cmp.Or(cmp.Compare(b.Quantity, a.Quantity), cmp.Compare(b.RevenueCents, a.RevenueCents), cmp.Compare(a.Name, b.Name))
	})
	s.TopProducts = s.TopProducts[:min(5, len(s.TopProducts))]

	for _, c := range p.ranked[:min(8, len(p.ranked))] {
		s.Customers = append(s.Customers, CustomerSales{Name: c.name, Initials: initials(c.name), Orders: len(c.orders), SpentCents: c.spentCents})
	}
	s.Totals.Customers, s.Totals.Repeat = len(p.customers), p.repeat
	s.Totals.PendingOrders, s.Totals.PendingCents = len(p.pending), p.pendingCents
	if shipped := len(p.all) - len(p.pending); shipped > 0 {
		s.Totals.AvgDispatchHours = p.dispatched.Hours() / float64(shipped)
	}
	for i, n := range p.bands {
		s.Dispatch[i].Orders = n
	}
	s.Recent = newestFirst(p.all, 0, 8, "")
	return s
}

// CarrierSales is how a carrier did for a shop: how many parcels it took, how long the shop took to
// hand them over after the order, and how long the carrier took to deliver.
type CarrierSales struct {
	Code             string  `json:"code"`
	Name             string  `json:"name"`
	Orders           int     `json:"orders"`
	AvgDispatchHours float64 `json:"avgDispatchHours"`
	AvgTransitHours  float64 `json:"avgTransitHours"`
}

// carrierStats returns a line for each carrier, in the order of Carriers. Transit counts only the
// parcels that have been signed for. The caller holds ordersMu for reading.
func carrierStats(p *projectOrders) []CarrierSales {
	out := make([]CarrierSales, len(Carriers))
	for i, c := range Carriers {
		t := p.carriers[i]
		out[i] = CarrierSales{Code: c.Code, Name: c.Name, Orders: t.orders}
		if t.orders > 0 {
			out[i].AvgDispatchHours = t.dispatch.Hours() / float64(t.orders)
		}
		if t.delivered > 0 {
			out[i].AvgTransitHours = t.transit.Hours() / float64(t.delivered)
		}
	}
	return out
}

// dispatchBands are the times within which orders are handed to the carrier.
var dispatchLimits = []time.Duration{6 * time.Hour, 12 * time.Hour, 24 * time.Hour}

func dispatchBands() []DispatchBand {
	return []DispatchBand{{Label: "Within 6 hours"}, {Label: "6 to 12 hours"}, {Label: "12 to 24 hours"}, {Label: "Over 24 hours"}}
}

func dispatchBand(d time.Duration) int {
	for i, limit := range dispatchLimits {
		if d < limit {
			return i
		}
	}
	return len(dispatchLimits)
}
