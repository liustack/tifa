package orders

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

// queries are the order lists the check reads: the default list and its pages, both ways, and
// each filter and sort.
var queries = []ListQuery{
	{}, {Page: 2}, {Page: 99}, {Dir: SortAsc}, {Dir: SortAsc, Page: 2}, {Sort: SortAmount}, {Sort: SortAmount, Dir: SortAsc, Page: 2},
	{Status: StatusPending}, {Status: StatusShipped, Page: 2}, {Carrier: "SF Express"}, {Period: "7"}, {Period: "30", Status: StatusPending},
	{Amount: "mid"}, {Search: "sh-104"}, {Search: "林"}, {Status: "nope", Sort: "nope", Dir: "nope", Page: -3},
}

func zones(t *testing.T) []*time.Location {
	t.Helper()
	var out []*time.Location
	for _, name := range []string{"Asia/Shanghai", "Pacific/Honolulu", "America/Santiago"} {
		zone, err := time.LoadLocation(name)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, zone)
	}
	return out
}

// checkReads checks that every read of every project gives what the reference gives from the same
// orders.
func checkReads(t *testing.T, step string) {
	t.Helper()
	all := snapshot()
	same := func(what string, got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			g, _ := json.Marshal(got)
			w, _ := json.Marshal(want)
			t.Fatalf("after %s, %s:\n got %s\nwant %s", step, what, g, w)
		}
	}
	for _, project := range []string{"studio", "weekend", "nowhere"} {
		same(project+" pending", CountPending(project), refCountPending(all, project))
		for _, zone := range zones(t) {
			for _, q := range queries {
				same(fmt.Sprintf("%s list %+v in %s", project, q, zone), ListOrders(project, zone, q), refListOrders(all, project, zone, q))
			}
			same(fmt.Sprintf("%s stats in %s", project, zone), SalesStats(project, zone), refSalesStats(all, project, zone))
		}
		for _, o := range refOrdersOf(all, project) {
			got, err := GetOrder(project, o.ID)
			want, wantErr := refGetOrder(all, project, o.ID)
			same(project+" order "+o.ID, [2]any{got, err}, [2]any{want, wantErr})
			others, count, spent := CustomerOrders(project, o.ID)
			wothers, wcount, wspent := refCustomerOrders(all, project, o.ID)
			same(project+" customer of "+o.ID, [3]any{others, count, spent}, [3]any{wothers, wcount, wspent})
			for _, item := range o.Items {
				same(project+" orders with "+item.Slug, OrdersWith(project, item.Slug, o.ID, 4), refOrdersWith(all, project, item.Slug, o.ID, 4))
			}
		}
		_, err := GetOrder(project, "SH-0000")
		_, wantErr := refGetOrder(all, project, "SH-0000")
		same(project+" unknown order", err, wantErr)
		others, count, spent := CustomerOrders(project, "SH-0000")
		same(project+" customer of an unknown order", [3]any{others, count, spent}, [3]any{[]OrderRow{}, 0, 0})
		same(project+" orders with an unknown product", OrdersWith(project, "nope", "", 4), refOrdersWith(all, project, "nope", "", 4))
	}
}

// The reads of the package keep indexes and totals that every write changes. After each kind of
// write, one at a time, they must give what the reference gives by going through all the orders.
func TestReadsMatchTheReferenceAfterEveryWrite(t *testing.T) {
	reset()
	t.Cleanup(reset)
	checkReads(t, "the start")
	day := time.Date(2026, 10, 14, 9, 30, 0, 0, chinaTime)
	create := func(project, customer string, at time.Time, lines ...NewLine) string {
		t.Helper()
		id, err := CreateOrder(project, NewOrder{Customer: customer, Phone: "13800138000", Address: "上海市徐汇区", Items: lines}, at)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	a := create("studio", "林夏", day, NewLine{"oak-tray", 1}, NewLine{"ceramic-cup", 2})
	checkReads(t, "a create")
	must(ShipOrder("studio", a, "ZTO", "ZTO20261014001", day.Add(5*time.Hour)))
	checkReads(t, "a ship")
	if err := ShipOrder("studio", a, "SF", "SF20261014001", day.Add(6*time.Hour)); !errors.Is(err, ErrShipped) {
		t.Fatalf("second ship: %v", err)
	}
	checkReads(t, "a second ship, refused")
	must(AddNote("studio", a, "Owner", "已发出。", day.Add(7*time.Hour)))
	must(AddNote("studio", "SH-1040", "Owner", "顾客来电。", day.Add(7*time.Hour)))
	checkReads(t, "notes")

	// A new name, the old one back, and the name of another customer, who then has one more order.
	must(UpdateRecipient("studio", "SH-1041", "新顾客 1", "13800138000", "北京市朝阳区"))
	checkReads(t, "a rename")
	must(UpdateRecipient("studio", "SH-1041", "陈默", "13900139001", "杭州市西湖区 · 演示地址"))
	checkReads(t, "a rename back")
	must(UpdateRecipient("studio", "SH-1042", "林夏", "13800138000", "上海市徐汇区"))
	checkReads(t, "a rename to a customer with orders")
	if err := UpdateRecipient("studio", a, "别人", "13800138000", "上海市徐汇区"); !errors.Is(err, ErrRecipientLocked) {
		t.Fatalf("rename of a shipped order: %v", err)
	}
	checkReads(t, "a rename of a shipped order, refused")
	// A customer with two orders who gives one away is no longer a repeat customer.
	create("studio", "回头客", day, NewLine{"oak-tray", 1})
	twice := create("studio", "回头客", day.Add(time.Minute), NewLine{"oak-tray", 1})
	checkReads(t, "a second order of a customer")
	must(UpdateRecipient("studio", twice, "新顾客 3", "13800138000", "北京市朝阳区"))
	checkReads(t, "a rename of one of two orders")

	// Across days: the next day, past the 14 days of the list and the 30 of the stats, two at the
	// same time, one placed before the newest, and one that holds a product twice.
	create("studio", "周宁", day.Add(24*time.Hour), NewLine{"linen-apron", 3})
	checkReads(t, "a create the next day")
	b := create("studio", "宋知秋", day.Add(40*24*time.Hour), NewLine{"pour-over-kettle", 1})
	checkReads(t, "a create 40 days later")
	c1 := create("studio", "宋知秋", day.Add(40*24*time.Hour), NewLine{"brass-bookmarks", 1})
	c2 := create("studio", "陆川", day.Add(40*24*time.Hour), NewLine{"brass-bookmarks", 2})
	checkReads(t, "two creates at the same time")
	create("studio", "叶青", day.Add(20*24*time.Hour), NewLine{"reading-candle", 1})
	checkReads(t, "a create placed before the newest")
	create("studio", "叶青", day.Add(41*24*time.Hour), NewLine{"reading-candle", 1}, NewLine{"reading-candle", 2})
	checkReads(t, "a create with a product twice")
	create("weekend", "林夏", day, NewLine{"canvas-tote", 1})
	checkReads(t, "a create in the other shop")

	// Shipping the oldest order waiting, the ones of the same time, and all of studio.
	must(ShipOrder("studio", "SH-1041", "EMS", "EMS2026101400", day.Add(time.Hour)))
	checkReads(t, "a ship of the oldest pending")
	must(ShipOrder("studio", c2, "SF", "SF20261014002", day.Add(41*24*time.Hour)))
	must(ShipOrder("studio", c1, "SF", "SF20261014003", day.Add(41*24*time.Hour)))
	checkReads(t, "ships of orders placed at the same time")
	must(UpdateRecipient("studio", b, "新顾客 2", "13800138000", "北京市朝阳区"))
	for _, row := range allRows("studio") {
		if row.Status == StatusPending {
			must(ShipOrder("studio", row.ID, "JD", "JD2026101400", day.Add(42*24*time.Hour)))
		}
	}
	checkReads(t, "shipping everything in studio")
	if CountPending("studio") != 0 || ListOrders("studio", shanghai, ListQuery{}).OldestPending != nil {
		t.Fatal("studio has orders waiting")
	}
}

// Reads and writes at once: the race detector checks the locks, and the reads after them must
// still match the reference. Reads in zones not read before count their days while others read.
func TestReadsAndWritesAtOnce(t *testing.T) {
	reset()
	t.Cleanup(reset)
	zs := zones(t)
	day := time.Date(2026, 10, 14, 9, 30, 0, 0, chinaTime)
	var wg sync.WaitGroup
	for w := range 4 {
		wg.Go(func() {
			for i := range 50 {
				at := day.Add(time.Duration(w*50+i) * time.Minute)
				id, err := CreateOrder("studio", NewOrder{Customer: customers[i%len(customers)].name, Phone: "13800138000", Address: "上海市徐汇区", Items: []NewLine{{"ceramic-cup", 1 + i%3}}}, at)
				if err != nil {
					t.Error(err)
					return
				}
				switch i % 4 {
				case 0:
					err = ShipOrder("studio", id, "SF", "SF2026101400", at.Add(time.Hour))
				case 1:
					err = UpdateRecipient("studio", id, fmt.Sprintf("顾客 %d-%d", w, i), "13800138000", "上海市徐汇区")
				case 2:
					err = AddNote("studio", id, "Owner", "已确认。", at)
				}
				if err != nil {
					t.Error(err)
				}
			}
		})
	}
	for r := range 4 {
		wg.Go(func() {
			for i := range 50 {
				zone := zs[(r+i)%len(zs)]
				ListOrders("studio", zone, ListQuery{})
				SalesStats("studio", zone)
				CountPending("studio")
				if o, err := GetOrder("studio", "SH-1032"); err == nil {
					CustomerOrders("studio", o.ID)
					OrdersWith("studio", o.Items[0].Slug, o.ID, 4)
				}
			}
		})
	}
	wg.Wait()
	checkReads(t, "reads and writes at once")
}

// In New York the clocks go back an hour on 2026-11-01, so that day has 25 hours. With the newest
// order at 23:30 that evening, the last two of the 14 steps of 24 hours both land on 11-01. The
// list shows that date twice, and both show the whole day.
func TestListOrdersShowADayOf25HoursTwice(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reset)
	ordersMu.Lock()
	for i, placed := range []string{"2026-11-02T04:30:00Z", "2026-11-01T05:30:00Z", "2026-10-31T15:00:00Z", "2026-10-20T15:00:00Z"} {
		at, err := time.Parse(time.RFC3339, placed)
		if err != nil {
			t.Fatal(err)
		}
		insert(orders, &orderRecord{ID: fmt.Sprint("NY-", i), ProjectID: "nyc", PlacedAt: at, Items: []itemRecord{{Slug: "oak-tray", Quantity: 1, PriceCents: 1000 * (i + 1)}}})
	}
	ordersMu.Unlock()
	all := snapshot()
	got := ListOrders("nyc", newYork, ListQuery{})
	if want := refListOrders(all, "nyc", newYork, ListQuery{}); !reflect.DeepEqual(got, want) {
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(want)
		t.Fatalf("list in New York:\n got %s\nwant %s", g, w)
	}
	d := got.Daily
	if d.Orders[12] != 2 || d.Orders[13] != 2 || d.RevenueCents[12] != 3000 || d.RevenueCents[13] != 3000 || d.Orders[11] != 1 {
		t.Fatalf("daily orders %v, revenue %v, want the last two at 2 orders and 3000 cents, the one before at 1", d.Orders, d.RevenueCents)
	}
}
