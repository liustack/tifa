package orders

import (
	"errors"
	"testing"
	"time"

	"github.com/liustack/tifa"
)

func TestListSumsTheOrdersOfOneProject(t *testing.T) {
	reset()
	s := ListOrders("studio")
	if len(s.Orders) != 42 || s.Orders[0].ID != "SH-1042" || s.Orders[41].ID != "SH-1001" {
		t.Fatalf("%d orders, from %s to %s", len(s.Orders), s.Orders[0].ID, s.Orders[len(s.Orders)-1].ID)
	}
	if s.RevenueCents != 1413400 || s.Pending != 2 || s.Shipped != 40 {
		t.Fatalf("totals %d cents, %d pending, %d shipped", s.RevenueCents, s.Pending, s.Shipped)
	}
	// Newest first, numbered in the order they were placed, and only the newest wait to be shipped.
	for i, o := range s.Orders {
		if i > 0 && (o.PlacedAt.After(s.Orders[i-1].PlacedAt) || o.ID >= s.Orders[i-1].ID) {
			t.Fatalf("%s comes after %s", o.ID, s.Orders[i-1].ID)
		}
		if (o.Status == StatusPending) != (o.ID == "SH-1042" || o.ID == "SH-1041") {
			t.Errorf("%s is %s", o.ID, o.Status)
		}
	}
	first := s.Orders[41].PlacedAt
	if y, m, d := first.Date(); y != 2026 || m != 9 || d != 14 {
		t.Errorf("the first order is placed on %s", first)
	}
	if w := ListOrders("weekend"); len(w.Orders) != 12 || w.Orders[0].ID != "WK-0201" || w.Orders[11].ID != "WK-0190" || w.Pending != 1 {
		t.Fatalf("weekend: %d orders, %d pending", len(w.Orders), w.Pending)
	}
	if empty := ListOrders("nowhere"); empty.Orders == nil || len(empty.Orders) != 0 || empty.RevenueCents != 0 {
		t.Fatalf("list of an unknown project %+v", empty)
	}
}

func TestListRowsCountPiecesAndCents(t *testing.T) {
	reset()
	row := ListOrders("studio").Orders[0]
	want := OrderRow{
		ID: "SH-1042", Customer: "林夏", Initials: "林", PlacedAt: time.Date(2026, 10, 13, 14, 32, 0, 0, shopZone),
		Status: StatusPending, Items: 2, TotalCents: 42800,
	}
	if row != want {
		t.Fatalf("row %+v, want %+v", row, want)
	}
}

func TestGetReturnsTheWholeOrder(t *testing.T) {
	reset()
	o, err := GetOrder("studio", "SH-1041")
	if err != nil {
		t.Fatal(err)
	}
	if o.Initials != "陈" || o.Status != StatusPending || o.TotalCents != 26800 || o.Items[0].PriceCents != 13400 || o.Items[0].Quantity != 2 {
		t.Fatalf("order %+v", o)
	}
	if got := o.PlacedAt.UTC().Format(time.RFC3339); got != "2026-10-12T08:47:00Z" {
		t.Fatalf("placed at %s", got)
	}
	// An order is only found under its own project.
	if _, err := GetOrder("weekend", "SH-1041"); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("order of another project: %v", err)
	}
}

// Every order lists each product once, so a page can key the lines by name.
func TestDemoOrdersListEachProductOnce(t *testing.T) {
	for _, o := range demoOrders() {
		seen := map[string]bool{}
		for _, item := range o.Items {
			if seen[item.Name] || item.Quantity < 1 {
				t.Errorf("%s: %+v", o.ID, o.Items)
			}
			seen[item.Name] = true
		}
	}
}

func TestShipOnce(t *testing.T) {
	reset()
	if err := ShipOrder("studio", "SH-1042"); err != nil {
		t.Fatal(err)
	}
	if o, _ := GetOrder("studio", "SH-1042"); o.Status != StatusShipped {
		t.Fatalf("status after shipping %q", o.Status)
	}
	if s := ListOrders("studio"); s.Pending != 1 || s.Shipped != 41 {
		t.Fatalf("totals after shipping: %d pending, %d shipped", s.Pending, s.Shipped)
	}
	if err := ShipOrder("studio", "SH-1042"); err != ErrShipped {
		t.Fatalf("second ship: %v", err)
	}
	if err := ShipOrder("weekend", "SH-1041"); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("ship an order of another project: %v", err)
	}
}

// reset puts the demonstration orders back, so each test starts from them.
func reset() {
	ordersMu.Lock()
	defer ordersMu.Unlock()
	orders = demoOrders()
}
