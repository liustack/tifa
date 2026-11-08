package cart

import (
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
	"github.com/liustack/tifa/examples/shop/app/store/products"
	"github.com/liustack/tifa/examples/shop/app/store/storekit"
)

// newCart makes a cart of its own with the lines given as slug, colour, size and quantity, and
// returns its ID.
func newCart(t *testing.T, lines ...lineRecord) string {
	t.Helper()
	c, w := visit(t, "")
	if err := ApplyCoupon(c, ""); err != nil {
		t.Fatal(err)
	}
	cookie, err := http.ParseSetCookie(w.Header().Get("Set-Cookie"))
	if err != nil {
		t.Fatal(err)
	}
	carts.mu.Lock()
	rec, _ := carts.get(cookie.Value, time.Now())
	rec.lines = slices.Clone(lines)
	carts.mu.Unlock()
	return cookie.Value
}

// keepEntry puts a product back as it is now when the test ends.
func keepEntry(t *testing.T, slug string) products.Entry {
	t.Helper()
	e, err := products.GetEntry(slug)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = products.Update(slug, e.Edit) })
	return e
}

func TestACheckoutBecomesAnOrderOfTheStudioShop(t *testing.T) {
	apron, carafe := keepEntry(t, "linen-apron"), keepEntry(t, "glass-carafe")
	id := newCart(t, lineRecord{"linen-apron", "ink", "M", 2}, lineRecord{"glass-carafe", "mist", "", 1})
	zone, _ := time.LoadLocation("Asia/Shanghai")
	before := orders.ListOrders(storekit.Project, zone, orders.ListQuery{})
	c, _ := visit(t, id)
	in := goodOrder()
	in.Notes = "请附手写卡片"
	now := time.Date(2026, 11, 9, 9, 30, 0, 0, time.UTC)
	number, err := PlaceOrder(c, zh, in, now)
	if err != nil {
		t.Fatal(err)
	}
	o, err := orders.GetOrder(storekit.Project, number)
	if err != nil {
		t.Fatalf("the back office has no order %s: %v", number, err)
	}
	if o.Customer != "林夏" || o.Phone != "13800138000" || o.Email != "lin@example.com" || o.Note != "请附手写卡片" || !o.PlacedAt.Equal(now) || o.Status != orders.StatusPending {
		t.Errorf("the order is %+v", o)
	}
	if o.Address != "武康路 99 号, 上海市, 上海市, 200031, 中国大陆" {
		t.Errorf("the address is %q", o.Address)
	}
	if len(o.Items) != 2 || o.Items[0].Slug != "linen-apron" || o.Items[0].Description != "墨蓝 / M" || o.Items[0].Quantity != 2 || o.Items[0].PriceCents != 16800 ||
		o.Items[1].Slug != "glass-carafe" || o.Items[1].Quantity != 1 || o.TotalCents != 2*16800+15800 {
		t.Errorf("the lines are %+v", o.Items)
	}
	after := orders.ListOrders(storekit.Project, zone, orders.ListQuery{})
	if after.Total != before.Total+1 || after.Orders[0].ID != number || after.Pending != before.Pending+1 {
		t.Errorf("the order list of the studio shop: %d orders, first %s", after.Total, after.Orders[0].ID)
	}
	if e, _ := products.GetEntry("linen-apron"); e.Stock != apron.Stock-2 {
		t.Errorf("the apron has %d, was %d", e.Stock, apron.Stock)
	}
	if e, _ := products.GetEntry("glass-carafe"); e.Stock != carafe.Stock-1 {
		t.Errorf("the carafe has %d, was %d", e.Stock, carafe.Stock)
	}
	// The confirmation reads the order of the back office.
	p, err := GetPlaced(number, id, zh)
	if err != nil || p.ID != number || p.Email != "lin@example.com" || p.Count != 3 || !p.PlacedAt.Equal(now) || p.Delivery.ID != "express" {
		t.Errorf("the confirmation is %+v, %v", p, err)
	}
}

func TestACheckoutOverTheStockIsRefused(t *testing.T) {
	e := keepEntry(t, "oak-tray")
	edit := e.Edit
	edit.Stock = 2
	if err := products.Update("oak-tray", edit); err != nil {
		t.Fatal(err)
	}
	// Two lines of one product hold 3 together, one more than the stock.
	id := newCart(t, lineRecord{"oak-tray", "oat", "M", 2}, lineRecord{"oak-tray", "oat", "L", 1}, lineRecord{"ceramic-cup", "moss", "", 1})
	cup := keepEntry(t, "ceramic-cup")
	zone, _ := time.LoadLocation("Asia/Shanghai")
	before := orders.ListOrders(storekit.Project, zone, orders.ListQuery{}).Total
	c, _ := visit(t, id)
	_, err := PlaceOrder(c, zh, goodOrder(), time.Now())
	var te tifa.Error
	if !errors.As(err, &te) || te.Status != 409 || te.Message != "cartStock" {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if n := orders.ListOrders(storekit.Project, zone, orders.ListQuery{}).Total; n != before {
		t.Errorf("a refused checkout placed an order: %d orders, was %d", n, before)
	}
	if got, _ := products.GetEntry("oak-tray"); got.Stock != 2 {
		t.Errorf("a refused checkout left %d trays", got.Stock)
	}
	if got, _ := products.GetEntry("ceramic-cup"); got.Stock != cup.Stock {
		t.Errorf("a refused checkout took cups: %d, was %d", got.Stock, cup.Stock)
	}
	if n := Count(id); n != 4 {
		t.Errorf("a refused checkout emptied the cart: %d pieces", n)
	}
	// With one tray fewer the order goes through and takes the last two.
	if _, err := SetQuantity(c, "oak-tray.oat.L", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := PlaceOrder(c, zh, goodOrder(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := products.GetEntry("oak-tray"); got.Stock != 0 {
		t.Errorf("%d trays left", got.Stock)
	}
}

// A checkout is placed and totalled from the catalogue as it was when it took the stock: staff who
// change the price and title of a product while it is placed change neither the order nor the total
// of its confirmation.
func TestAnEditDuringACheckoutChangesNeitherItsOrderNorItsTotal(t *testing.T) {
	apron := keepEntry(t, "linen-apron")
	id := newCart(t, lineRecord{"linen-apron", "ink", "M", 1}, lineRecord{"linen-apron", "ink", "L", 1})
	c, _ := visit(t, id)
	now := time.Date(2026, 11, 9, 9, 30, 0, 0, time.UTC)
	in := goodOrder()
	in.Delivery = "standard"
	// What the checkout showed: the cart at the old price, with its delivery.
	shown := price([]lineRecord{{"linen-apron", "ink", "M", 1}, {"linen-apron", "ink", "L", 1}}, "", zh, products.GetProduct)
	want := shown.SubtotalCents
	for _, d := range Deliveries(shown, now, zh) {
		if d.ID == in.Delivery {
			want += d.PriceCents
		}
	}
	edit := apron.Edit
	edit.PriceCents, edit.NameZH = apron.PriceCents+5000, "新围裙"
	beforeCreateOrder = func() {
		if err := products.Update("linen-apron", edit); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { beforeCreateOrder = nil })
	number, err := PlaceOrder(c, zh, in, now)
	if err != nil {
		t.Fatal(err)
	}
	o, err := orders.GetOrder(storekit.Project, number)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range o.Items {
		if it.PriceCents != apron.PriceCents || it.Name != apron.NameZH {
			t.Errorf("a line of the order is %s at %d, want %s at %d as the checkout priced it", it.Name, it.PriceCents, apron.NameZH, apron.PriceCents)
		}
	}
	if o.TotalCents != 2*apron.PriceCents {
		t.Errorf("the order totals %d, want %d", o.TotalCents, 2*apron.PriceCents)
	}
	p, err := GetPlaced(number, id, zh)
	if err != nil {
		t.Fatal(err)
	}
	if p.TotalCents != want {
		t.Errorf("the confirmation totals %d, want %d", p.TotalCents, want)
	}
}
