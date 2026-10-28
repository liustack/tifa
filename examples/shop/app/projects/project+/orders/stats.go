package orders

import (
	"cmp"
	"slices"
	"strings"
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
// project without orders has no days.
func SalesStats(projectID string, zone *time.Location) Sales {
	s := Sales{Days: []DaySales{}, TopProducts: []ProductSales{}, Products: []ProductRow{}, Carriers: CarrierStats(projectID), Customers: []CustomerSales{}, Heat: make([][]int, 7), Dispatch: dispatchBands(), Recent: []OrderRow{}}
	for i := range s.Heat {
		s.Heat[i] = make([]int, 12)
	}
	list := ordersOf(projectID) // newest first
	if len(list) == 0 {
		return s
	}
	// An order belongs to the day of its time in zone. The dates themselves are only labels of
	// the calendar, counted in UTC: in a zone whose daylight saving time starts at midnight, such
	// as America/Santiago, that day has no 00:00, and counting there would list the day before
	// twice and leave that day out.
	last := list[0].PlacedAt.In(zone)
	first := time.Date(last.Year(), last.Month(), last.Day()-(statsDays-1), 0, 0, 0, 0, time.UTC)
	index := map[string]int{}
	for i := range statsDays {
		date := first.AddDate(0, 0, i).Format(time.DateOnly)
		index[date] = i
		s.Days = append(s.Days, DaySales{Date: date})
	}
	products := map[string]*ProductSales{}
	rows := map[string]*ProductRow{}
	customers := map[string]*CustomerSales{}
	var dispatched time.Duration
	for _, o := range list {
		local := o.PlacedAt.In(zone)
		s.Heat[(int(local.Weekday())+6)%7][local.Hour()/2]++
		c := customers[o.Customer]
		if c == nil {
			c = &CustomerSales{Name: o.Customer, Initials: initials(o.Customer)}
			customers[o.Customer] = c
		}
		c.Orders++
		c.SpentCents += total(o)
		if sh := o.Shipment; sh != nil {
			h := sh.ShippedAt.Sub(o.PlacedAt)
			dispatched += h
			s.Dispatch[dispatchBand(h)].Orders++
		} else {
			s.Totals.PendingOrders++
			s.Totals.PendingCents += total(o)
		}
		i, ok := index[local.Format(time.DateOnly)]
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
				p = &ProductSales{Slug: item.Slug, Name: item.Name}
				products[item.Name] = p
				rows[item.Name] = &ProductRow{Slug: item.Slug, Name: item.Name, Daily: make([]int, statsDays)}
			}
			p.Quantity += item.Quantity
			p.RevenueCents += item.Quantity * item.PriceCents
			r := rows[item.Name]
			r.Quantity += item.Quantity
			r.Orders++
			r.RevenueCents += item.Quantity * item.PriceCents
			r.Daily[i] += item.Quantity
			d.Pieces += item.Quantity
			s.Totals.Pieces += item.Quantity
		}
	}
	if shipped := len(list) - s.Totals.PendingOrders; shipped > 0 {
		s.Totals.AvgDispatchHours = dispatched.Hours() / float64(shipped)
	}
	for _, c := range customers {
		s.Customers = append(s.Customers, *c)
		s.Totals.Customers++
		if c.Orders > 1 {
			s.Totals.Repeat++
		}
	}
	slices.SortFunc(s.Customers, func(a, b CustomerSales) int {
		return cmp.Or(cmp.Compare(b.SpentCents, a.SpentCents), cmp.Compare(a.Name, b.Name))
	})
	s.Customers = s.Customers[:min(8, len(s.Customers))]
	for _, r := range rows {
		s.Products = append(s.Products, *r)
	}
	slices.SortFunc(s.Products, func(a, b ProductRow) int {
		return cmp.Or(cmp.Compare(b.Quantity, a.Quantity), cmp.Compare(b.RevenueCents, a.RevenueCents), cmp.Compare(a.Name, b.Name))
	})
	for _, o := range list[:min(8, len(list))] {
		s.Recent = append(s.Recent, orderRow(o))
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

// CarrierSales is how a carrier did for a shop: how many parcels it took, how long the shop took to
// hand them over after the order, and how long the carrier took to deliver.
type CarrierSales struct {
	Code             string  `json:"code"`
	Name             string  `json:"name"`
	Orders           int     `json:"orders"`
	AvgDispatchHours float64 `json:"avgDispatchHours"`
	AvgTransitHours  float64 `json:"avgTransitHours"`
}

// CarrierStats returns a line for each carrier, in the order of Carriers. Transit counts only the
// parcels that have been signed for.
func CarrierStats(projectID string) []CarrierSales {
	out := make([]CarrierSales, len(Carriers))
	dispatch := make([]time.Duration, len(Carriers))
	transit := make([]time.Duration, len(Carriers))
	delivered := make([]int, len(Carriers))
	for i, c := range Carriers {
		out[i] = CarrierSales{Code: c.Code, Name: c.Name}
	}
	for _, o := range ordersOf(projectID) {
		s := o.Shipment
		i := -1
		if s != nil {
			i = slices.IndexFunc(Carriers, func(c Carrier) bool { return c.Code == s.Carrier })
		}
		if i < 0 {
			continue
		}
		out[i].Orders++
		dispatch[i] += s.ShippedAt.Sub(o.PlacedAt)
		if n := len(s.Events); n > 0 && strings.HasPrefix(s.Events[n-1].Note, "已签收") {
			delivered[i]++
			transit[i] += s.Events[n-1].At.Sub(s.ShippedAt)
		}
	}
	for i := range out {
		if out[i].Orders > 0 {
			out[i].AvgDispatchHours = dispatch[i].Hours() / float64(out[i].Orders)
		}
		if delivered[i] > 0 {
			out[i].AvgTransitHours = transit[i].Hours() / float64(delivered[i])
		}
	}
	return out
}

// dispatchBands are the times within which orders are handed to the carrier.
var dispatchLimits = []time.Duration{6 * time.Hour, 12 * time.Hour, 24 * time.Hour}

func dispatchBands() []DispatchBand {
	return []DispatchBand{{Label: "6 小时内"}, {Label: "6 到 12 小时"}, {Label: "12 到 24 小时"}, {Label: "超过 24 小时"}}
}

func dispatchBand(d time.Duration) int {
	for i, limit := range dispatchLimits {
		if d < limit {
			return i
		}
	}
	return len(dispatchLimits)
}
