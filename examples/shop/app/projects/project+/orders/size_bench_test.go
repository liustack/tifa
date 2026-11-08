package orders

import (
	"fmt"
	"testing"
	"time"
)

// grow puts the demonstration orders back and places orders in studio until it holds n, a minute
// apart from the day after the newest demonstration order, so they all fall in the days the list
// and the stats draw, as the orders a round of the comparison places do. Two in five ship, and one
// in ten gets a new customer name first, as the comparison's writes do.
func grow(tb testing.TB, n int) {
	tb.Helper()
	reset()
	start := time.Date(2026, 10, 14, 9, 0, 0, 0, chinaTime)
	slugs := []string{"pour-over-kettle", "ceramic-cup", "oak-tray", "linen-apron"}
	for i := range n - 42 { // studio starts with 42
		c := customers[i%len(customers)]
		at := start.Add(time.Duration(i) * time.Minute)
		id, err := CreateOrder("studio", NewOrder{
			Customer: c.name, Phone: c.phone, Email: c.email, Address: c.address,
			Items: []NewLine{{Slug: slugs[i%len(slugs)], Quantity: 1 + i%3}},
		}, at)
		if err != nil {
			tb.Fatal(err)
		}
		if i%10 == 0 {
			if err := UpdateRecipient("studio", id, fmt.Sprintf("顾客 %d", i), c.phone, c.address); err != nil {
				tb.Fatal(err)
			}
		}
		if i%5 < 2 {
			if err := ShipOrder("studio", id, Carriers[i%len(Carriers)].Code, fmt.Sprintf("SF%010d", i), at.Add(time.Hour)); err != nil {
				tb.Fatal(err)
			}
		}
	}
	if got := ListOrders("studio", shanghai, ListQuery{}).Total; got != n {
		tb.Fatalf("studio holds %d orders, want %d", got, n)
	}
}

// BenchmarkReads times the reads of the pages of the workbench with studio at about 50, 2000 and
// 5000 orders: the order list, an order with the other orders of its customer and of its first
// product, the pending count every page shows in the sidebar, and the stats.
func BenchmarkReads(b *testing.B) {
	for _, n := range []int{50, 2000, 5000} {
		grow(b, n)
		b.Run(fmt.Sprintf("orders=%d/list", n), func(b *testing.B) {
			for b.Loop() {
				ListOrders("studio", shanghai, ListQuery{})
			}
		})
		b.Run(fmt.Sprintf("orders=%d/order", n), func(b *testing.B) {
			for b.Loop() {
				o, _ := GetOrder("studio", "SH-1032")
				CustomerOrders("studio", o.ID)
				OrdersWith("studio", o.Items[0].Slug, o.ID, 4)
			}
		})
		b.Run(fmt.Sprintf("orders=%d/pending", n), func(b *testing.B) {
			for b.Loop() {
				CountPending("studio")
			}
		})
		b.Run(fmt.Sprintf("orders=%d/stats", n), func(b *testing.B) {
			for b.Loop() {
				SalesStats("studio", shanghai)
			}
		})
		b.Run(fmt.Sprintf("orders=%d/create", n), func(b *testing.B) {
			at := time.Date(2026, 10, 20, 9, 0, 0, 0, chinaTime)
			for b.Loop() {
				at = at.Add(time.Second)
				if _, err := CreateOrder("weekend", NewOrder{Customer: "林夏", Phone: "13800138000", Address: "上海市徐汇区", Items: []NewLine{{Slug: "postcards", Quantity: 1}}}, at); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	reset()
}
