package orders

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store/products"
)

func TestListSumsTheOrdersOfOneProject(t *testing.T) {
	reset()
	studio := ListOrders("studio", shanghai, ListQuery{})
	s := allRows("studio")
	if studio.Total != 42 || len(s) != 42 || s[0].ID != "SH-1042" || s[41].ID != "SH-1001" {
		t.Fatalf("%d orders, from %s to %s", len(s), s[0].ID, s[len(s)-1].ID)
	}
	// The page holds the newest, the totals are over all.
	if len(studio.Orders) != ListPageSize || !reflect.DeepEqual(studio.Orders, s[:ListPageSize]) {
		t.Fatalf("the first page holds %d orders", len(studio.Orders))
	}
	if studio.RevenueCents != 1413400 || studio.Pending != 2 || studio.Shipped != 40 {
		t.Fatalf("totals %d cents, %d pending, %d shipped", studio.RevenueCents, studio.Pending, studio.Shipped)
	}
	// Newest first, numbered in the order they were placed, and only the newest wait to be shipped.
	for i, o := range s {
		if i > 0 && (o.PlacedAt.After(s[i-1].PlacedAt) || o.ID >= s[i-1].ID) {
			t.Fatalf("%s comes after %s", o.ID, s[i-1].ID)
		}
		if (o.Status == StatusPending) != (o.ID == "SH-1042" || o.ID == "SH-1041") {
			t.Errorf("%s is %s", o.ID, o.Status)
		}
	}
	first := s[41].PlacedAt
	if y, m, d := first.Date(); y != 2026 || m != 9 || d != 14 {
		t.Errorf("the first order is placed on %s", first)
	}
	if w := ListOrders("weekend", shanghai, ListQuery{}); w.Total != 12 || len(w.Orders) != 12 || w.Orders[0].ID != "WK-0201" || w.Orders[11].ID != "WK-0190" || w.Pending != 1 {
		t.Fatalf("weekend: %d orders, %d pending", len(w.Orders), w.Pending)
	}
	if empty := ListOrders("nowhere", shanghai, ListQuery{}); empty.Orders == nil || len(empty.Orders) != 0 || empty.Total != 0 || empty.RevenueCents != 0 || empty.OldestPending != nil || empty.Carriers == nil || empty.Daily.Orders == nil {
		t.Fatalf("list of an unknown project %+v", empty)
	}
}

// The cards of the list are drawn from numbers over all the orders, since the page holds only the
// newest.
func TestListNumbersCoverTheOrdersTheFirstPageLeavesOut(t *testing.T) {
	reset()
	l := ListOrders("studio", shanghai, ListQuery{})
	if l.OldestPending == nil || l.OldestPending.ID != "SH-1041" {
		t.Errorf("the oldest order waiting to ship is %+v", l.OldestPending)
	}
	if !l.NewestAt.Equal(time.Date(2026, 10, 13, 14, 32, 0, 0, chinaTime)) {
		t.Errorf("newest at %s", l.NewestAt)
	}
	// SH-1001 to SH-1017 are not on the page, and their carriers are still listed.
	want := map[string]bool{}
	for _, row := range allRows("studio") {
		if row.Carrier != "" {
			want[row.Carrier] = true
		}
	}
	if len(l.Carriers) != len(want) || !slices.IsSorted(l.Carriers) {
		t.Errorf("carriers %v, want %v", l.Carriers, want)
	}
	d := l.Daily
	if len(d.Orders) != 14 || len(d.RevenueCents) != 14 || len(d.Pending) != 14 || len(d.Shipped) != 14 {
		t.Fatalf("days %+v", d)
	}
	// The last day is 13 October, on which only SH-1042 was placed.
	if d.Orders[13] != 1 || d.Pending[13] != 1 || d.Shipped[13] != 0 || d.RevenueCents[13] != 42800 {
		t.Errorf("last day: %d orders, %d pending, %d shipped, %d cents", d.Orders[13], d.Pending[13], d.Shipped[13], d.RevenueCents[13])
	}
	var orders, cents int
	for i := range d.Orders {
		orders += d.Orders[i]
		cents += d.RevenueCents[i]
		if d.Pending[i]+d.Shipped[i] != d.Orders[i] {
			t.Errorf("day %d: %d pending and %d shipped of %d", i, d.Pending[i], d.Shipped[i], d.Orders[i])
		}
	}
	// The same count taken from every order by its date.
	from := l.NewestAt.Add(-13 * 24 * time.Hour).In(shanghai).Format(time.DateOnly)
	var wantOrders, wantCents int
	for _, r := range allRows("studio") {
		if r.PlacedAt.In(shanghai).Format(time.DateOnly) >= from {
			wantOrders++
			wantCents += r.TotalCents
		}
	}
	if orders != wantOrders || cents != wantCents || orders <= 10 {
		t.Errorf("14 days hold %d orders and %d cents, want %d and %d", orders, cents, wantOrders, wantCents)
	}
}

// Another list on the page of an order holds the newest few, and the count is over all of them.
func TestCustomerOrdersAreTheNewestFewWithTheCountOfAll(t *testing.T) {
	reset()
	t.Cleanup(reset)
	stockUp()
	at := time.Date(2026, 11, 7, 10, 0, 0, 0, chinaTime)
	in := newOrder() // 周宁
	for i := range 14 {
		if _, err := CreateOrder("studio", in, at.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	others, count, spent := CustomerOrders("studio", "SH-1043")
	if len(others) != SideListSize || count < 15 || others[0].ID != "SH-1056" || slices.ContainsFunc(others, func(r OrderRow) bool { return r.ID == "SH-1043" }) {
		t.Fatalf("%d others, first %s, count %d", len(others), others[0].ID, count)
	}
	var cents, n int
	for _, r := range allRows("studio") {
		if r.Customer == "周宁" {
			cents += r.TotalCents
			n++
		}
	}
	if spent != cents || count != n {
		t.Errorf("spent %d in %d orders, want %d in %d", spent, count, cents, n)
	}
}

func TestListRowsCountPiecesAndCents(t *testing.T) {
	reset()
	row := allRows("studio")[0]
	want := OrderRow{
		ID: "SH-1042", Customer: "林夏", Initials: "林", PlacedAt: time.Date(2026, 10, 13, 14, 32, 0, 0, chinaTime),
		Status: StatusPending, Items: 2, Lines: 2, Product: "Morning Pour-Over Kettle", Slug: "pour-over-kettle", Photo: "pour-over-kettle", TotalCents: 42800,
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
	want := Shipment{
		Carrier: "SF", CarrierName: "SF Express", TrackingNo: "SF1234567890", ShippedAt: at,
		Events: []TrackEvent{{At: at, Place: warehouse, Note: "Courier collected the parcel"}},
	}
	if o.Status != StatusShipped || o.Shipment == nil || !reflect.DeepEqual(*o.Shipment, want) {
		t.Fatalf("after shipping: %s %+v", o.Status, o.Shipment)
	}
	if s := ListOrders("studio", shanghai, ListQuery{}); s.Pending != 1 || s.Shipped != 41 {
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
		for _, row := range allRows(p) {
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
		{"", "SF1234567890", tifa.Fields{"carrier": "Pick a carrier"}},
		{"DHL", "SF1234567890", tifa.Fields{"carrier": "Pick a carrier"}},
		{"SF", "SF12", tifa.Fields{"trackingNo": "The tracking number is 8 to 20 letters or digits"}},
		{"SF", "SF-1234-5678", tifa.Fields{"trackingNo": "The tracking number is 8 to 20 letters or digits"}},
		{"", "", tifa.Fields{"carrier": "Pick a carrier", "trackingNo": "The tracking number is 8 to 20 letters or digits"}},
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
		{"", "13800138000", "上海市徐汇区漕溪北路 88 号", tifa.Fields{"customer": "Enter the recipient"}},
		{"林夏", "12345", "上海市徐汇区漕溪北路 88 号", tifa.Fields{"phone": "Enter an 11-digit phone number"}},
		{"林夏", "13800138000", "  ", tifa.Fields{"address": "Enter the delivery address"}},
		{"林夏", "13800138000", long, tifa.Fields{"address": "The address is at most 100 characters"}},
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
	if row := allRows("studio")[1]; row.ID != "SH-1041" || row.Customer != "陈小默" {
		t.Fatalf("the list shows %+v", row)
	}
	if err := UpdateRecipient("studio", "SH-1040", "周宁", "13800138000", "北京市朝阳区"); err != ErrRecipientLocked {
		t.Fatalf("change a shipped order: %v", err)
	}
	if err := UpdateRecipient("weekend", "SH-1041", "周宁", "13800138000", "北京市朝阳区"); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("change an order of another project: %v", err)
	}
}

// stockUp gives every product of both shops the most stock the back office takes, for a test that
// places more orders than the demonstration stock holds. reset puts the stock back.
func stockUp() {
	for _, e := range startEntries {
		e.Stock = 99_999
		if err := products.Update(e.Slug, e.Edit); err != nil {
			panic(err)
		}
	}
	weekendMu.Lock()
	for i := range weekendShelf {
		weekendShelf[i].Stock = 99_999
	}
	weekendMu.Unlock()
}

// startEntries is the catalogue of the storefront as the tests of this package start with it.
var startEntries = products.Entries()

// reset puts the demonstration orders back, with the stock of both shops as they start, so each
// test starts from them.
func reset() {
	for _, e := range startEntries {
		if err := products.Update(e.Slug, e.Edit); err != nil {
			panic(err)
		}
	}
	weekendMu.Lock()
	weekendShelf = newWeekendShelf()
	weekendMu.Unlock()
	ordersMu.Lock()
	defer ordersMu.Unlock()
	orders = load(demoOrders())
}

func TestEveryProductHasPhotos(t *testing.T) {
	reset()
	for _, o := range snapshot() {
		for _, item := range o.Items {
			for _, view := range []string{"main", "detail", "scene"} {
				for _, width := range []int{160, 480, 1200} {
					file := fmt.Sprintf("../../../store/images/%s/%s-%d.avif", item.Slug, view, width)
					if _, err := os.Stat(file); err != nil {
						t.Errorf("%s of %s: %v", item.Name, o.ID, err)
					}
				}
			}
		}
	}
}

func TestListRowsShowTheCarrierOfShippedOrders(t *testing.T) {
	reset()
	rows := allRows("studio")
	if rows[0].Carrier != "" || rows[0].TrackingNo != "" {
		t.Errorf("pending row %+v", rows[0])
	}
	if rows[2].ID != "SH-1040" || rows[2].Carrier != "SF Express" || rows[2].TrackingNo != "SF1408273615" {
		t.Errorf("shipped row %+v", rows[2])
	}
}

func TestDemoParcelsTravelToTheCityOfTheAddress(t *testing.T) {
	reset()
	o, _ := GetOrder("studio", "SH-1040") // 周宁, 北京市朝阳区
	events := o.Shipment.Events
	if len(events) != 11 || !events[0].At.Equal(o.Shipment.ShippedAt) || events[0].Place != warehouse {
		t.Fatalf("events %+v", events)
	}
	if last := events[10]; last.Place != "北京市朝阳区" || last.Note != "Delivered, signed by the recipient" {
		t.Errorf("last event %+v", last)
	}
	for i := 1; i < len(events); i++ {
		if !events[i].At.After(events[i-1].At) {
			t.Errorf("event %d at %s is not after %s", i, events[i].At, events[i-1].At)
		}
	}
	if n, _ := GetOrder("studio", "SH-1042"); n.Note == "" {
		t.Error("SH-1042 has no note")
	}
}

func TestCustomerOrdersAreTheOtherOrdersOfTheBuyer(t *testing.T) {
	reset()
	others, count, spent := CustomerOrders("studio", "SH-1042") // 林夏
	if count != len(others)+1 || count < 2 {
		t.Fatalf("%d orders, %d others", count, len(others))
	}
	sum := 42800
	for _, o := range others {
		if o.Customer != "林夏" || o.ID == "SH-1042" {
			t.Errorf("other order %+v", o)
		}
		sum += o.TotalCents
	}
	if spent != sum {
		t.Errorf("spent %d, want %d", spent, sum)
	}
	if others, count, _ := CustomerOrders("studio", "SH-0000"); len(others) != 0 || count != 0 {
		t.Errorf("unknown order: %d others, %d orders", len(others), count)
	}
}
