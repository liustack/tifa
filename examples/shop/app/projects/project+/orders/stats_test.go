package orders

import (
	"slices"
	"testing"
	"time"
)

func TestSalesStatsCoverThirtyDaysEndingOnTheNewestOrder(t *testing.T) {
	s := SalesStats("studio")
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
func TestSalesStatsAddUpToTheOrderList(t *testing.T) {
	for _, id := range []string{"studio", "weekend"} {
		s, l := SalesStats(id), ListOrders(id)
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
			p.Name = item.Name
			p.Quantity += item.Quantity
			p.RevenueCents += item.Quantity * item.PriceCents
			sold[item.Name] = p
		}
	}
	top := SalesStats("studio").TopProducts
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
	for _, p := range SalesStats("weekend").TopProducts {
		if sold[p.Name].Name != "" {
			t.Errorf("weekend lists %s, a product of studio", p.Name)
		}
	}
}

func TestSalesStatsOfAnUnknownShopHaveNoDays(t *testing.T) {
	if s := SalesStats("nowhere"); len(s.Days) != 0 || len(s.TopProducts) != 0 {
		t.Fatalf("unknown shop: %+v", s)
	}
}
