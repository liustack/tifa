package orders

import (
	"errors"
	"maps"
	"strings"
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
		ID: "SH-1042", Customer: "林夏", Initials: "林", PlacedAt: time.Date(2026, 10, 13, 14, 32, 0, 0, chinaTime),
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
	at := time.Date(2026, 10, 14, 9, 30, 0, 0, chinaTime)
	if err := ShipOrder("studio", "SH-1042", "SF", " SF1234567890 ", at); err != nil {
		t.Fatal(err)
	}
	o, _ := GetOrder("studio", "SH-1042")
	want := Shipment{Carrier: "SF", CarrierName: "顺丰速运", TrackingNo: "SF1234567890", ShippedAt: at}
	if o.Status != StatusShipped || o.Shipment == nil || *o.Shipment != want {
		t.Fatalf("after shipping: %s %+v", o.Status, o.Shipment)
	}
	if s := ListOrders("studio"); s.Pending != 1 || s.Shipped != 41 {
		t.Fatalf("totals after shipping: %d pending, %d shipped", s.Pending, s.Shipped)
	}
	if err := ShipOrder("studio", "SH-1042", "JD", "JD0001234567", at); err != ErrShipped {
		t.Fatalf("second ship: %v", err)
	}
	if o, _ := GetOrder("studio", "SH-1042"); o.Shipment.Carrier != "SF" {
		t.Fatalf("shipping twice changed the shipment: %+v", o.Shipment)
	}
	if err := ShipOrder("weekend", "SH-1041", "SF", "SF1234567890", at); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("ship an order of another project: %v", err)
	}
}

// Orders that shipped before carry their shipment, those waiting have none.
func TestDemoShipmentsFollowTheirOrders(t *testing.T) {
	reset()
	for _, p := range []string{"studio", "weekend"} {
		for _, row := range ListOrders(p).Orders {
			o, _ := GetOrder(p, row.ID)
			if (o.Shipment != nil) != (o.Status == StatusShipped) {
				t.Fatalf("%s is %s with shipment %+v", o.ID, o.Status, o.Shipment)
			}
			if o.Shipment != nil && (!o.Shipment.ShippedAt.After(o.PlacedAt) || CheckShipment(o.Shipment.Carrier, o.Shipment.TrackingNo) != nil) {
				t.Errorf("%s: shipment %+v of an order placed at %s", o.ID, o.Shipment, o.PlacedAt)
			}
		}
	}
}

func TestCheckShipment(t *testing.T) {
	for _, tc := range []struct {
		carrier, trackingNo string
		fields              tifa.Fields
	}{
		{"SF", "SF1234567890", nil},
		{"EMS", " 1234abcd ", nil},
		{"", "SF1234567890", tifa.Fields{"carrier": "请选择快递公司"}},
		{"DHL", "SF1234567890", tifa.Fields{"carrier": "请选择快递公司"}},
		{"SF", "SF12", tifa.Fields{"trackingNo": "运单号是 8 到 20 位字母或数字"}},
		{"SF", "SF-1234-5678", tifa.Fields{"trackingNo": "运单号是 8 到 20 位字母或数字"}},
		{"", "", tifa.Fields{"carrier": "请选择快递公司", "trackingNo": "运单号是 8 到 20 位字母或数字"}},
	} {
		err := CheckShipment(tc.carrier, tc.trackingNo)
		if tc.fields == nil {
			if err != nil {
				t.Errorf("%q %q: %v", tc.carrier, tc.trackingNo, err)
			}
			continue
		}
		var fields tifa.Fields
		if !errors.As(err, &fields) || !maps.Equal(fields, tc.fields) {
			t.Errorf("%q %q: %v, want %v", tc.carrier, tc.trackingNo, err, tc.fields)
		}
	}
}

func TestCheckRecipient(t *testing.T) {
	long := strings.Repeat("路", 101)
	for _, tc := range []struct {
		customer, phone, address string
		fields                   tifa.Fields
	}{
		{"林夏", "13800138000", "上海市徐汇区漕溪北路 88 号", nil},
		{" 林夏 ", " 138 0013 8000 ", "上海市徐汇区漕溪北路 88 号", nil},
		{"", "13800138000", "上海市徐汇区漕溪北路 88 号", tifa.Fields{"customer": "请填写收件人"}},
		{"林夏", "12345", "上海市徐汇区漕溪北路 88 号", tifa.Fields{"phone": "请填写 11 位手机号"}},
		{"林夏", "13800138000", "  ", tifa.Fields{"address": "请填写收货地址"}},
		{"林夏", "13800138000", long, tifa.Fields{"address": "收货地址最多 100 个字"}},
	} {
		err := CheckRecipient(tc.customer, tc.phone, tc.address)
		if tc.fields == nil {
			if err != nil {
				t.Errorf("%q %q %q: %v", tc.customer, tc.phone, tc.address, err)
			}
			continue
		}
		var fields tifa.Fields
		if !errors.As(err, &fields) || !maps.Equal(fields, tc.fields) {
			t.Errorf("%q %q %q: %v, want %v", tc.customer, tc.phone, tc.address, err, tc.fields)
		}
	}
}

func TestUpdateRecipientUntilTheOrderShips(t *testing.T) {
	reset()
	if err := UpdateRecipient("studio", "SH-1041", " 陈小默 ", "139 0013 9000", " 杭州市西湖区文三路 1 号 "); err != nil {
		t.Fatal(err)
	}
	o, _ := GetOrder("studio", "SH-1041")
	if o.Customer != "陈小默" || o.Initials != "陈" || o.Phone != "13900139000" || o.Address != "杭州市西湖区文三路 1 号" {
		t.Fatalf("after the change: %+v", o)
	}
	if row := ListOrders("studio").Orders[1]; row.ID != "SH-1041" || row.Customer != "陈小默" {
		t.Fatalf("the list shows %+v", row)
	}
	if err := UpdateRecipient("studio", "SH-1040", "周宁", "13800138000", "北京市朝阳区"); err != ErrRecipientLocked {
		t.Fatalf("change a shipped order: %v", err)
	}
	if err := UpdateRecipient("weekend", "SH-1041", "周宁", "13800138000", "北京市朝阳区"); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("change an order of another project: %v", err)
	}
}

// reset puts the demonstration orders back, so each test starts from them.
func reset() {
	ordersMu.Lock()
	defer ordersMu.Unlock()
	orders = demoOrders()
}
