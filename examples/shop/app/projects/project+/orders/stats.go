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
}

// ProductSales is how much of one product the shop sold.
type ProductSales struct {
	Slug         string `json:"slug"`
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
// order, with days starting at midnight in zone, the time zone of the shop. A real shop would end
// on today. The demonstration data does not move with the clock, so neither do its stats. A
// project without orders has no days.
func SalesStats(projectID string, zone *time.Location) Sales {
	s := Sales{Days: []DaySales{}, TopProducts: []ProductSales{}}
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
	for _, o := range list {
		i, ok := index[o.PlacedAt.In(zone).Format(time.DateOnly)]
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
