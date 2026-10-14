package orders

import (
	"errors"
	"testing"

	"github.com/liustack/tifa"
)

func TestListSumsTheOrdersOfOneProject(t *testing.T) {
	reset()
	s := ListOrders("studio")
	if len(s.Orders) != 4 || s.Orders[0].ID != "SH-1042" || s.Orders[3].ID != "SH-1039" {
		t.Fatalf("orders %+v", s.Orders)
	}
	if s.Revenue != "¥1472.00" || s.Pending != 2 || s.Shipped != 2 {
		t.Fatalf("totals %s, %d pending, %d shipped", s.Revenue, s.Pending, s.Shipped)
	}
	if empty := ListOrders("nowhere"); empty.Orders == nil || len(empty.Orders) != 0 || empty.Revenue != "¥0.00" {
		t.Fatalf("list of an unknown project %+v", empty)
	}
}

func TestGetFormatsTheOrder(t *testing.T) {
	reset()
	o, err := GetOrder("studio", "SH-1041")
	if err != nil {
		t.Fatal(err)
	}
	if o.Initials != "陈" || o.Status != StatusPending || o.Total != "¥268.00" || o.Items[0].Price != "¥134.00" {
		t.Fatalf("order %+v", o)
	}
	// An order is only found under its own project.
	if _, err := GetOrder("weekend", "SH-1041"); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("order of another project: %v", err)
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
	if s := ListOrders("studio"); s.Pending != 1 || s.Shipped != 3 {
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
