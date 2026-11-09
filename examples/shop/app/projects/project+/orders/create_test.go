package orders

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store/products"
	"github.com/liustack/tifa/tifatest"
)

func newOrder() NewOrder {
	return NewOrder{
		Customer: " 周宁 ", Phone: "137 0013-7002", Email: "zhouning@example.com", Address: " 北京市朝阳区 ", Note: "请轻放",
		Items: []NewLine{{Slug: "ceramic-cup", Quantity: 2}, {Slug: "oak-tray", Quantity: 1}},
	}
}

func TestCreateNumbersTheOrderAfterTheHighestOfItsProject(t *testing.T) {
	reset()
	at := time.Date(2026, 11, 7, 10, 0, 0, 0, chinaTime)
	for _, tc := range []struct{ project, want string }{
		{"studio", "SH-1043"}, {"weekend", "WK-0202"}, {"studio", "SH-1044"}, {"weekend", "WK-0203"},
	} {
		in := newOrder()
		if tc.project == "weekend" {
			in.Items = []NewLine{{Slug: "canvas-tote", Quantity: 1}}
		}
		id, err := CreateOrder(tc.project, in, at)
		if err != nil || id != tc.want {
			t.Fatalf("%s: %q, %v, want %q", tc.project, id, err, tc.want)
		}
	}
}

func TestCreateKeepsWhatWasEnteredAndTakesTheRestFromTheCatalogue(t *testing.T) {
	reset()
	before := ListOrders("studio", shanghai, ListQuery{})
	seen, _ := GetOrder("studio", "SH-1041")
	sold, demand := seen.Items[0].Sold, seen.Items[0].Demand
	at := time.Date(2026, 11, 7, 10, 0, 0, 0, chinaTime)
	id, err := CreateOrder("studio", newOrder(), at)
	if err != nil {
		t.Fatal(err)
	}
	o, err := GetOrder("studio", id)
	if err != nil {
		t.Fatal(err)
	}
	if o.Customer != "周宁" || o.Phone != "13700137002" || o.Address != "北京市朝阳区" || o.Note != "请轻放" || !o.PlacedAt.Equal(at) || o.Status != StatusPending || o.Shipment != nil {
		t.Fatalf("order %+v", o)
	}
	if len(o.Items) != 2 || o.Items[0].Name != "Handmade Ceramic Cup" || o.Items[0].PriceCents != 13400 || o.Items[0].Stock != 58-2 || o.TotalCents != 2*13400+58000 {
		t.Fatalf("items %+v, total %d", o.Items, o.TotalCents)
	}
	// It is the newest order: first in the list, in the totals and in what waits to ship.
	l := ListOrders("studio", shanghai, ListQuery{})
	if l.Total != 43 || len(l.Orders) != ListPageSize || l.Orders[0].ID != id || l.Pending != before.Pending+1 || l.RevenueCents != before.RevenueCents+o.TotalCents {
		t.Fatalf("list: %d orders, first %s, %d pending, %d cents", l.Total, l.Orders[0].ID, l.Pending, l.RevenueCents)
	}
	if n := CountPending("studio"); n != before.Pending+1 {
		t.Fatalf("pending %d", n)
	}
	if len(allRows("weekend")) != 12 {
		t.Fatal("an order of the studio is in the weekend shop")
	}
	// The order pages of the project count its pieces in Sold and Demand.
	if after, _ := GetOrder("studio", "SH-1041"); after.Items[0].Sold != sold+2 || after.Items[0].Demand != demand+2 {
		t.Fatalf("SH-1041 shows sold %d and demand %d, was %d and %d", after.Items[0].Sold, after.Items[0].Demand, sold, demand)
	}
}

func TestCreateRules(t *testing.T) {
	reset()
	var fields tifa.Fields
	for name, mod := range map[string]func(*NewOrder){
		"customer":    func(in *NewOrder) { in.Customer = " " },
		"phone":       func(in *NewOrder) { in.Phone = "12345" },
		"address":     func(in *NewOrder) { in.Address = "" },
		"note":        func(in *NewOrder) { in.Note = string(make([]rune, 201)) + "x" },
		"items(none)": func(in *NewOrder) { in.Items = nil },
		"items(four)": func(in *NewOrder) { in.Items = make([]NewLine, 4) },
		"items(zero)": func(in *NewOrder) { in.Items[0].Quantity = 0 },
		"items(six)":  func(in *NewOrder) { in.Items[0].Quantity = 6 },
	} {
		in := newOrder()
		mod(&in)
		want := name
		if i := len(want) - 1; want[i] == ')' {
			want = "items"
		}
		if err := CheckNewOrder(in); !errors.As(err, &fields) || fields[want] == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := CheckNewOrder(newOrder()); err != nil {
		t.Errorf("a good order: %v", err)
	}
	in := newOrder()
	in.Items[1].Slug = "no-such-product"
	if _, err := CreateOrder("studio", in, time.Now()); !errors.As(err, &fields) || fields["items"] == "" {
		t.Errorf("an unknown product: %v", err)
	}
	in = newOrder()
	in.Items[1].Color = "ink" // the oak tray comes in oat only
	if _, err := CreateOrder("studio", in, time.Now()); !errors.As(err, &fields) || fields["items"] == "" {
		t.Errorf("a colour the product does not come in: %v", err)
	}
	if _, err := CreateOrder("weekend", newOrder(), time.Now()); !errors.As(err, &fields) || fields["items"] == "" {
		t.Errorf("a product of the storefront in the weekend shop: %v", err)
	}
	if _, err := CreateOrder("nowhere", newOrder(), time.Now()); !errors.Is(err, tifa.ErrNotFound) {
		t.Errorf("an unknown project: %v", err)
	}
	if n := len(allRows("studio")); n != 42 {
		t.Errorf("a refused order was kept: %d orders", n)
	}
}

func TestPOSTPlacesTheOrderAndAnswersWithIt(t *testing.T) {
	reset()
	r := httptest.NewRequest("POST", "/projects/weekend/orders", nil)
	r.SetPathValue("project", "weekend")
	c, _ := tifatest.NewCtx(t, r)
	o, err := POST(c, NewOrder{
		Customer: "苏禾", Phone: "13500135004", Email: "suhe@example.com", Address: "苏州市姑苏区",
		Items: []NewLine{{Slug: "postcards", Quantity: 5}},
	})
	if err != nil || o.ID != "WK-0202" || o.TotalCents != 18000 || o.Status != StatusPending || time.Since(o.PlacedAt) > time.Minute {
		t.Fatalf("%+v, %v", o, err)
	}
}

// The studio shop sells the products of the storefront: an order takes their names, prices and
// stock from its catalogue as it is when the order is placed.
func TestStudioOrdersTakeTheProductsOfTheStorefront(t *testing.T) {
	reset()
	before, err := products.GetEntry("ceramic-cup")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = products.Update("ceramic-cup", before.Edit) })
	at := time.Date(2026, 11, 7, 10, 0, 0, 0, chinaTime)
	in := newOrder()
	in.Items = []NewLine{{Slug: "glass-carafe", Color: "mist", Quantity: 1}, {Slug: "linen-apron", Color: "ink", Size: "M", Quantity: 2}, {Slug: "ceramic-cup", Quantity: 1}}
	id, err := CreateOrder("studio", in, at)
	if err != nil {
		t.Fatal(err)
	}
	o, _ := GetOrder("studio", id)
	want := []LineItem{
		{Slug: "glass-carafe", Name: "Glass Carafe", Description: "Mist", Quantity: 1, PriceCents: 15800, Color: "mist", Stock: 12 - 1, Sold: 1, Demand: 1},
		{Slug: "linen-apron", Name: "Cotton Linen Apron", Description: "Ink / M", Quantity: 2, PriceCents: 16800, Color: "ink", Stock: 34 - 2},
		{Slug: "ceramic-cup", Name: "Handmade Ceramic Cup", Description: "Moss", Quantity: 1, PriceCents: 13400, Color: "moss", Stock: 58 - 1},
	}
	for i, w := range want {
		got := o.Items[i]
		got.Sold, got.Demand = 0, 0
		w.Sold, w.Demand = 0, 0
		if got != w {
			t.Errorf("line %d is %+v, want %+v", i, got, w)
		}
	}
	edit := before.Edit
	edit.NameEN, edit.PriceCents, edit.Stock = "New Ceramic Cup", 12000, 7
	if err := products.Update("ceramic-cup", edit); err != nil {
		t.Fatal(err)
	}
	in.Items = []NewLine{{Slug: "ceramic-cup", Quantity: 1}}
	id, err = CreateOrder("studio", in, at)
	if err != nil {
		t.Fatal(err)
	}
	if o, _ := GetOrder("studio", id); o.Items[0].Name != "New Ceramic Cup" || o.Items[0].PriceCents != 12000 || o.Items[0].Stock != 7-1 || o.TotalCents != 12000 {
		t.Errorf("after an edit the order holds %+v", o.Items[0])
	}
	// An order placed before keeps what it was placed at.
	if o, _ := GetOrder("studio", "SH-1041"); o.Items[0].Name != "Handmade Ceramic Cup" || o.Items[0].PriceCents != 13400 {
		t.Errorf("SH-1041 now holds %+v", o.Items[0])
	}
}

// Every order takes its pieces from the stock, the ones staff place as a checkout does: the stock
// goes down by what the order holds, so what the warehouse holds (the stock and what waits to ship)
// stays the same, and an order of more than is left is refused with 409 and takes nothing. That holds
// in both shops.
func TestAStaffOrderTakesItsStock(t *testing.T) {
	reset()
	t.Cleanup(reset)
	for _, tc := range []struct{ project, slug string }{{"studio", "oak-tray"}, {"weekend", "postcards"}} {
		stock := productStock(tc.project, tc.slug)
		waiting := 0
		if p := ordersOf(tc.project).products[tc.slug]; p != nil {
			waiting = p.waiting
		}
		in := newOrder()
		in.Items = []NewLine{{Slug: tc.slug, Quantity: 2}}
		id, err := CreateOrder(tc.project, in, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		o, _ := GetOrder(tc.project, id)
		if it := o.Items[0]; it.Stock != stock-2 || it.Stock+it.Demand != stock+waiting {
			t.Errorf("%s: %s went from %d in stock and %d to ship to %d and %d, want 2 fewer in stock and the warehouse the same",
				tc.project, tc.slug, stock, waiting, it.Stock, it.Demand)
		}
		in.Items = []NewLine{{Slug: tc.slug, Quantity: 5}}
		// postcards start with 260, so this ends when the stock goes down.
		for n := 0; productStock(tc.project, tc.slug) >= 5; n++ {
			if n == 100 {
				t.Fatalf("%s: 100 orders of 5 took nothing from %d %s", tc.project, productStock(tc.project, tc.slug), tc.slug)
			}
			if _, err := CreateOrder(tc.project, in, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		left := productStock(tc.project, tc.slug)
		before := len(ordersOf(tc.project).all)
		_, err = CreateOrder(tc.project, in, time.Now())
		var te tifa.Error
		if !errors.As(err, &te) || te.Status != 409 {
			t.Errorf("%s: an order of 5 with %d left: %v, want a 409", tc.project, left, err)
		}
		if productStock(tc.project, tc.slug) != left || len(ordersOf(tc.project).all) != before {
			t.Errorf("%s: a refused order took stock or was kept", tc.project)
		}
	}
}

// The instant of a write is kept to the millisecond, as JavaScript keeps it.
func TestNowIsToTheMillisecond(t *testing.T) {
	for range 100 {
		if n := Now(); n.Nanosecond()%int(time.Millisecond) != 0 {
			t.Fatalf("Now() = %v has a part under a millisecond", n)
		}
	}
}

// The back office is in English, so the lines of an order, the demonstration ones and the ones staff
// or a checkout place, name their products in English, as the catalogue of their shop does.
func TestOrdersNameTheirProductsInEnglish(t *testing.T) {
	reset()
	t.Cleanup(reset)
	o, err := GetOrder("studio", "SH-1042")
	if err != nil {
		t.Fatal(err)
	}
	if got := o.Items[0].Name; got != "Morning Pour-Over Kettle" {
		t.Errorf("SH-1042 holds %q, want the English name of the kettle", got)
	}
	w, err := GetOrder("weekend", "WK-0201")
	if err != nil {
		t.Fatal(err)
	}
	if got := w.Items[0].Name; got != "Weekend Canvas Tote" {
		t.Errorf("WK-0201 holds %q, want the English name of the tote", got)
	}
	at := time.Date(2026, 11, 7, 10, 0, 0, 0, chinaTime)
	for _, tc := range []struct{ project, slug, want string }{
		{"studio", "oak-tray", "Oak Serving Tray"}, {"weekend", "postcards", "Hand-drawn Postcards"},
	} {
		id, err := CreateOrder(tc.project, NewOrder{Customer: "周宁", Phone: "13700137002", Address: "北京市朝阳区", Items: []NewLine{{Slug: tc.slug, Quantity: 1}}}, at)
		if err != nil {
			t.Fatal(err)
		}
		o, err := GetOrder(tc.project, id)
		if err != nil {
			t.Fatal(err)
		}
		if got := o.Items[0].Name; got != tc.want {
			t.Errorf("an order of %s holds %q, want %q", tc.slug, got, tc.want)
		}
		if got := productName(tc.project, tc.slug); got != tc.want {
			t.Errorf("the stats name %s %q, want %q", tc.slug, got, tc.want)
		}
	}
}

// The colour and size of a line are in English too: a demonstration order as it was written, a
// line of the storefront with the English name of its colour, and a line of the weekend shop as it
// sells the product.
func TestOrderLinesDescribeTheirVariantInEnglish(t *testing.T) {
	reset()
	t.Cleanup(reset)
	o, err := GetOrder("studio", "SH-1042")
	if err != nil {
		t.Fatal(err)
	}
	if got := o.Items[0].Description; got != "Mist / 600 ml" {
		t.Errorf("SH-1042 describes its kettle as %q", got)
	}
	at := time.Date(2026, 11, 7, 10, 0, 0, 0, chinaTime)
	for _, tc := range []struct{ project, slug, want string }{
		{"studio", "pour-over-kettle", "Mist"}, {"weekend", "postcards", "Cities / 6 cards"},
	} {
		id, err := CreateOrder(tc.project, NewOrder{Customer: "周宁", Phone: "13700137002", Address: "北京市朝阳区", Items: []NewLine{{Slug: tc.slug, Quantity: 1}}}, at)
		if err != nil {
			t.Fatal(err)
		}
		o, err := GetOrder(tc.project, id)
		if err != nil {
			t.Fatal(err)
		}
		if got := o.Items[0].Description; got != tc.want {
			t.Errorf("an order of %s describes it as %q, want %q", tc.slug, got, tc.want)
		}
	}
}
