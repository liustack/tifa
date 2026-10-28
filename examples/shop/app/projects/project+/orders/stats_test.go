package orders

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// shanghai is the zone the demonstration shops trade in.
var shanghai = func() *time.Location {
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	return zone
}()

func TestSalesStatsCoverThirtyDaysEndingOnTheNewestOrder(t *testing.T) {
	s := SalesStats("studio", shanghai)
	if len(s.Days) != 30 || s.Days[0].Date != "2026-09-14" || s.Days[29].Date != "2026-10-13" {
		t.Fatalf("days: %d from %+v", len(s.Days), s.Days)
	}
	for i := 1; i < len(s.Days); i++ {
		prev, _ := time.Parse(time.DateOnly, s.Days[i-1].Date)
		if s.Days[i].Date != prev.AddDate(0, 0, 1).Format(time.DateOnly) {
			t.Fatalf("day %d is %s after %s", i, s.Days[i].Date, s.Days[i-1].Date)
		}
	}
}

// Every order of the demo data falls inside the thirty days, so the days add up to the list.
// Days start at midnight in the zone of the shop. The newest order of studio was placed at 14:32
// on 10-13 in Shanghai, which is still 10-12 in Honolulu.
func TestSalesStatsCutDaysInTheZoneOfTheShop(t *testing.T) {
	honolulu, err := time.LoadLocation("Pacific/Honolulu")
	if err != nil {
		t.Fatal(err)
	}
	s := SalesStats("studio", honolulu)
	if len(s.Days) != 30 || s.Days[29].Date != "2026-10-12" || s.Days[29].Orders != 1 {
		t.Fatalf("last day in Honolulu: %+v", s.Days[len(s.Days)-1])
	}
}

// In Santiago, daylight saving time starts at midnight: 2026-09-06 has no 00:00, the clocks go
// from 23:59 on 09-05 to 01:00. Each day is still listed once and keeps its orders.
func TestSalesStatsKeepADayThatStartsAfterMidnight(t *testing.T) {
	santiago, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reset)
	ordersMu.Lock()
	for i, placed := range []string{"2026-09-10T12:00:00Z", "2026-09-06T04:30:00Z", "2026-09-06T03:59:00Z"} {
		at, err := time.Parse(time.RFC3339, placed)
		if err != nil {
			t.Fatal(err)
		}
		orders = append(orders, orderRecord{ID: fmt.Sprint("CL-", i), ProjectID: "chile", PlacedAt: at})
	}
	ordersMu.Unlock()
	s := SalesStats("chile", santiago)
	seen, placed := map[string]bool{}, 0
	for _, d := range s.Days {
		if seen[d.Date] {
			t.Errorf("day %s listed twice", d.Date)
		}
		seen[d.Date] = true
		placed += d.Orders
	}
	if len(s.Days) != 30 || !seen["2026-09-06"] || placed != 3 {
		t.Fatalf("%d days, 09-06 listed %v, %d orders, want 30 days with 09-06 and 3 orders", len(s.Days), seen["2026-09-06"], placed)
	}
}

func TestSalesStatsAddUpToTheOrderList(t *testing.T) {
	for _, id := range []string{"studio", "weekend"} {
		s, l := SalesStats(id, shanghai), ListOrders(id)
		var orders, revenue, shipped int
		for _, d := range s.Days {
			orders += d.Orders
			revenue += d.RevenueCents
			shipped += d.Shipped
		}
		if orders != len(l.Orders) || revenue != l.RevenueCents || shipped != l.Shipped {
			t.Errorf("%s: %d orders, %d cents, %d shipped, want %d, %d, %d", id, orders, revenue, shipped, len(l.Orders), l.RevenueCents, l.Shipped)
		}
	}
}

func TestTopProductsAreTheBestSellersOfTheShop(t *testing.T) {
	sold := map[string]ProductSales{}
	for _, row := range ListOrders("studio").Orders {
		o, err := GetOrder("studio", row.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range o.Items {
			p := sold[item.Name]
			p.Slug, p.Name = item.Slug, item.Name
			p.Quantity += item.Quantity
			p.RevenueCents += item.Quantity * item.PriceCents
			sold[item.Name] = p
		}
	}
	top := SalesStats("studio", shanghai).TopProducts
	if len(top) != 5 {
		t.Fatalf("top products: %+v", top)
	}
	for i, p := range top {
		if sold[p.Name] != p {
			t.Errorf("%s: %+v, want %+v", p.Name, p, sold[p.Name])
		}
		if i > 0 && (p.Quantity > top[i-1].Quantity || p.Quantity == top[i-1].Quantity && p.RevenueCents > top[i-1].RevenueCents) {
			t.Errorf("%s comes after %s", p.Name, top[i-1].Name)
		}
	}
	// No product left out sold more than the last one shown.
	for name, p := range sold {
		if !slices.ContainsFunc(top, func(q ProductSales) bool { return q.Name == name }) && p.Quantity > top[4].Quantity {
			t.Errorf("%s sold %d but is not in the top 5", name, p.Quantity)
		}
	}
	for _, p := range SalesStats("weekend", shanghai).TopProducts {
		if sold[p.Name].Name != "" {
			t.Errorf("weekend lists %s, a product of studio", p.Name)
		}
	}
}

func TestSalesStatsOfAnUnknownShopHaveNoDays(t *testing.T) {
	if s := SalesStats("nowhere", shanghai); len(s.Days) != 0 || len(s.TopProducts) != 0 {
		t.Fatalf("unknown shop: %+v", s)
	}
}

func TestSalesStatsRankEveryProductWithItsDays(t *testing.T) {
	s := SalesStats("studio", shanghai)
	if len(s.Products) < 8 {
		t.Fatalf("%d products", len(s.Products))
	}
	var pieces, orders int
	byName := map[string]ProductRow{}
	for i, p := range s.Products {
		byName[p.Name] = p
		pieces += p.Quantity
		orders += p.Orders
		if len(p.Daily) != 30 {
			t.Errorf("%s has %d days", p.Name, len(p.Daily))
		}
		sum := 0
		for _, n := range p.Daily {
			sum += n
		}
		if sum != p.Quantity {
			t.Errorf("%s: days add up to %d, sold %d", p.Name, sum, p.Quantity)
		}
		if i > 0 && p.Quantity > s.Products[i-1].Quantity {
			t.Errorf("%s sold more than %s above it", p.Name, s.Products[i-1].Name)
		}
	}
	var dayPieces int
	for _, d := range s.Days {
		dayPieces += d.Pieces
	}
	if pieces != dayPieces || pieces != s.Totals.Pieces {
		t.Errorf("products %d, days %d, totals %d pieces", pieces, dayPieces, s.Totals.Pieces)
	}
	// The best sellers of the chart are the first of the ranking.
	for i, p := range s.TopProducts {
		if s.Products[i].Name != p.Name || s.Products[i].Quantity != p.Quantity {
			t.Errorf("rank %d is %s, the chart has %s", i, s.Products[i].Name, p.Name)
		}
	}
	if k := byName["晨间手冲壶"]; k.Orders < 3 || k.Orders > k.Quantity {
		t.Errorf("kettle %+v", k)
	}
}

func TestSalesStatsCountWhenOrdersCome(t *testing.T) {
	s := SalesStats("studio", shanghai)
	if len(s.Heat) != 7 {
		t.Fatalf("%d weekdays", len(s.Heat))
	}
	total := 0
	for _, row := range s.Heat {
		if len(row) != 12 {
			t.Fatalf("%d blocks", len(row))
		}
		for _, n := range row {
			total += n
		}
	}
	if total != 42 {
		t.Errorf("heat counts %d orders, want 42", total)
	}
	// SH-1042 was placed at 14:32 on a Tuesday: Monday is the first row, 14:00 to 16:00 the eighth block.
	if s.Heat[1][7] < 1 {
		t.Errorf("Tuesday 14:00 to 16:00: %d", s.Heat[1][7])
	}
	shipped := 0
	for _, b := range s.Dispatch {
		shipped += b.Orders
	}
	if len(s.Dispatch) != 4 || shipped != 40 {
		t.Errorf("dispatch %+v adds up to %d shipped orders", s.Dispatch, shipped)
	}
}

func TestSalesStatsListCustomersRecentOrdersAndTotals(t *testing.T) {
	s := SalesStats("studio", shanghai)
	if len(s.Customers) != 8 || len(s.Recent) != 8 || s.Recent[0].ID != "SH-1042" {
		t.Fatalf("%d customers, %d recent, first %v", len(s.Customers), len(s.Recent), s.Recent)
	}
	for i, c := range s.Customers {
		if i > 0 && c.SpentCents > s.Customers[i-1].SpentCents {
			t.Errorf("%s spent more than %s above", c.Name, s.Customers[i-1].Name)
		}
		_, count, spent := CustomerOrders("studio", firstOrderOf("studio", c.Name))
		if c.Orders != count || c.SpentCents != spent {
			t.Errorf("%s: %d orders, %d cents, want %d, %d", c.Name, c.Orders, c.SpentCents, count, spent)
		}
	}
	if s.Totals.PendingOrders != 2 || s.Totals.PendingCents != 42800+26800 || s.Totals.Customers < 10 || s.Totals.Repeat < 1 || s.Totals.Repeat > s.Totals.Customers {
		t.Errorf("totals %+v", s.Totals)
	}
	if h := s.Totals.AvgDispatchHours; h < 3 || h > 22 {
		t.Errorf("average dispatch %.1f hours", h)
	}
}

func firstOrderOf(projectID, customer string) string {
	for _, r := range ListOrders(projectID).Orders {
		if r.Customer == customer {
			return r.ID
		}
	}
	return ""
}
