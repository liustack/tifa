package orders

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa"
)

func TestOrdersCarryTheirPayment(t *testing.T) {
	reset()
	o, _ := GetOrder("studio", "SH-1042")
	p := o.Payment
	if p.Method == "" || !strings.HasPrefix(p.TransactionNo, "4200") || len(p.TransactionNo) != 24 {
		t.Fatalf("payment %+v", p)
	}
	if p.SubtotalCents != 42800 || p.ShippingCents != 0 || p.PaidCents != 42800 || !p.PaidAt.Equal(o.PlacedAt.Add(2*time.Minute)) {
		t.Errorf("payment %+v", p)
	}
	// The same order always pays the same way.
	again, _ := GetOrder("studio", "SH-1042")
	if again.Payment != p {
		t.Error("payment changed between reads")
	}
}

func TestTheLogTellsWhatHappenedToAnOrderInOrder(t *testing.T) {
	reset()
	pending, _ := GetOrder("studio", "SH-1042")
	if len(pending.Log) < 4 || pending.Log[0].Actor != "顾客" || pending.Log[0].Action != "下单" {
		t.Fatalf("pending log %+v", pending.Log)
	}
	shipped, _ := GetOrder("studio", "SH-1040")
	if len(shipped.Log) < 9 {
		t.Fatalf("shipped log has %d entries: %+v", len(shipped.Log), shipped.Log)
	}
	var shipping bool
	for i, e := range shipped.Log {
		if i > 0 && e.At.Before(shipped.Log[i-1].At) {
			t.Errorf("entry %d (%s) is before entry %d", i, e.Action, i-1)
		}
		if e.Action == "发货" && e.At.Equal(shipped.Shipment.ShippedAt) && strings.Contains(e.Detail, shipped.Shipment.TrackingNo) {
			shipping = true
		}
	}
	if !shipping {
		t.Errorf("no entry for shipping: %+v", shipped.Log)
	}
	if last := shipped.Log[len(shipped.Log)-1]; last.Actor != "快递" || last.Action != "已签收" {
		t.Errorf("last entry %+v", last)
	}
}

func TestStaffPutNotesOnAnOrder(t *testing.T) {
	reset()
	o, _ := GetOrder("studio", "SH-1042")
	if len(o.Notes) != 2 || o.Notes[0].Author == "" {
		t.Fatalf("seeded notes %+v", o.Notes)
	}
	at := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	if err := AddNote("studio", "SH-1042", "小满", "已和顾客确认，周三前送到。", at); err != nil {
		t.Fatal(err)
	}
	o, _ = GetOrder("studio", "SH-1042")
	if len(o.Notes) != 3 || o.Notes[2].Text != "已和顾客确认，周三前送到。" || !o.Notes[2].At.Equal(at) {
		t.Fatalf("notes %+v", o.Notes)
	}
	// A note is also a line of the log.
	if last := o.Log[len(o.Log)-1]; last.Action != "添加备注" || last.Actor != "小满" {
		t.Errorf("last log entry %+v", last)
	}
	if err := AddNote("studio", "SH-0000", "小满", "x", at); !errors.Is(err, tifa.ErrNotFound) {
		t.Errorf("note on an unknown order: %v", err)
	}
	var fields tifa.Fields
	if err := CheckNote("  "); !errors.As(err, &fields) || fields["text"] == "" {
		t.Errorf("empty note: %v", err)
	}
	if err := CheckNote(strings.Repeat("字", 201)); !errors.As(err, &fields) {
		t.Errorf("long note: %v", err)
	}
	if err := CheckNote("好的"); err != nil {
		t.Errorf("a note: %v", err)
	}
}

func TestEveryLineHasABinInTheWarehouse(t *testing.T) {
	reset()
	bin := regexp.MustCompile(`^[A-D]-\d{2}-[1-4]$`)
	o, _ := GetOrder("studio", "SH-1042")
	if len(o.Packing) != len(o.Items) {
		t.Fatalf("%d packing lines for %d items", len(o.Packing), len(o.Items))
	}
	for i, line := range o.Packing {
		if !bin.MatchString(line.Bin) || line.Quantity != o.Items[i].Quantity || line.Name != o.Items[i].Name {
			t.Errorf("line %+v", line)
		}
	}
}

func TestOrdersWithFindsTheNewestOrdersOfAProduct(t *testing.T) {
	reset()
	rows := OrdersWith("studio", "pour-over-kettle", "SH-1042", 4)
	if len(rows) != 4 {
		t.Fatalf("%d orders", len(rows))
	}
	for i, r := range rows {
		if r.ID == "SH-1042" || (i > 0 && !r.PlacedAt.Before(rows[i-1].PlacedAt)) {
			t.Errorf("row %d is %s", i, r.ID)
		}
		o, _ := GetOrder("studio", r.ID)
		if !slices.ContainsFunc(o.Items, func(i LineItem) bool { return i.Slug == "pour-over-kettle" }) {
			t.Errorf("%s has no kettle", r.ID)
		}
	}
	if len(OrdersWith("studio", "nope", "", 4)) != 0 {
		t.Error("orders of a product that does not exist")
	}
}

func TestLinesCountWhatTheShopSoldOfTheProduct(t *testing.T) {
	reset()
	o, _ := GetOrder("studio", "SH-1042")
	want := 0
	for _, order := range orders {
		if order.ProjectID != "studio" {
			continue
		}
		for _, item := range order.Items {
			if item.Slug == o.Items[0].Slug {
				want += item.Quantity
			}
		}
	}
	if o.Items[0].Sold != want || want < 4 {
		t.Errorf("sold %d, want %d", o.Items[0].Sold, want)
	}
}

func TestCarrierStatsAverageWhatTheShopShipped(t *testing.T) {
	reset()
	stats := CarrierStats("studio")
	if len(stats) != len(Carriers) {
		t.Fatalf("%d carriers", len(stats))
	}
	shipped := 0
	for i, c := range stats {
		shipped += c.Orders
		if c.Code != Carriers[i].Code || c.Name != Carriers[i].Name {
			t.Errorf("carrier %d is %s", i, c.Code)
		}
		// Every demonstration parcel leaves 3 to 22 hours after the order and takes 33 hours.
		if c.Orders > 0 && (c.AvgTransitHours != 33 || c.AvgDispatchHours < 3 || c.AvgDispatchHours > 22) {
			t.Errorf("%s: %d orders, dispatch %.1f h, transit %.1f h", c.Code, c.Orders, c.AvgDispatchHours, c.AvgTransitHours)
		}
	}
	if shipped != 40 {
		t.Errorf("%d shipped orders of studio, want 40", shipped)
	}
}

func TestLinesKnowTheStockAndWhatWaitingOrdersNeed(t *testing.T) {
	reset()
	o, _ := GetOrder("studio", "SH-1042") // a kettle and a pair of coasters, waiting
	kettle := o.Items[0]
	if kettle.Stock != 23 || kettle.Demand != 1 {
		t.Errorf("kettle: stock %d, demand %d", kettle.Stock, kettle.Demand)
	}
	cups, _ := GetOrder("studio", "SH-1041") // two cups, waiting
	if cups.Items[0].Demand != 2 {
		t.Errorf("cups: demand %d", cups.Items[0].Demand)
	}
	// A shipped order's lines are not waiting for anything.
	shipped, _ := GetOrder("studio", "SH-1040")
	if shipped.Items[0].Demand != 0 {
		t.Errorf("shipped line demand %d", shipped.Items[0].Demand)
	}
}
