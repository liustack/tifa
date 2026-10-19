package cart

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
	"github.com/liustack/tifa/tifatest"
)

var (
	zh, _ = store.FindLocale("zh-CN")
	en, _ = store.FindLocale("en-US")
)

// visit returns the Ctx of a request that carries the cart cookie id, or none for "".
func visit(t *testing.T, id string) (*tifa.Ctx, *httptest.ResponseRecorder) {
	r := httptest.NewRequest("POST", "/store/zh-CN/cart", nil)
	if id != "" {
		r.AddCookie(&http.Cookie{Name: CookieName, Value: id})
	}
	return tifatest.NewCtx(t, r)
}

func TestTheFirstChangeMakesACartOfOnesOwn(t *testing.T) {
	demo := Get("", zh)
	if demo.Count != 5 || len(demo.Lines) != 4 {
		t.Fatalf("demo cart %+v", demo)
	}
	c, w := visit(t, "")
	id, err := Add(c, "pour-over-kettle", "charcoal", "", 2)
	if err != nil || id == "" || !strings.Contains(w.Header().Get("Set-Cookie"), CookieName+"="+id) {
		t.Fatalf("Add: %q %v %v", id, err, w.Header())
	}
	if n := Count(id); n != 7 {
		t.Errorf("count after adding 2 to the demo cart: %d", n)
	}
	// The same variant adds up, and stays within the most a line takes.
	c, _ = visit(t, id)
	if _, err := Add(c, "pour-over-kettle", "charcoal", "", 9); err != nil {
		t.Fatal(err)
	}
	cart := Get(id, en)
	line := cart.Lines[len(cart.Lines)-1]
	if line.ID != "pour-over-kettle.charcoal" || line.Quantity != MaxQuantity || line.ColorName != "Charcoal" || line.PriceCents != 4620 {
		t.Errorf("line %+v", line)
	}
	if Get("", zh).Count != 5 {
		t.Error("changing a cart changed the demo cart")
	}
	if _, err := SetQuantity(c, "pour-over-kettle.charcoal", 0); err != nil || Count(id) != 5 {
		t.Errorf("remove a line: %v, %d left", err, Count(id))
	}
	if _, err := SetQuantity(c, "nope.mist", 1); !errors.Is(err, tifa.ErrNotFound) {
		t.Errorf("change an unknown line: %v", err)
	}
	var fields tifa.Fields
	if _, err := Add(c, "pour-over-kettle", "pink", "", 1); !errors.As(err, &fields) || fields["color"] != "variant" {
		t.Errorf("add a colour the kettle lacks: %v", err)
	}
	if _, err := SetQuantity(c, "ceramic-cup.moss", 11); !errors.As(err, &fields) || fields["quantity"] != "quantity" {
		t.Errorf("set 11: %v", err)
	}
}

func TestCouponsAndDelivery(t *testing.T) {
	c, _ := visit(t, "")
	id, err := Add(c, "ceramic-cup", "moss", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	c, _ = visit(t, id)
	var fields tifa.Fields
	if err := ApplyCoupon(c, "nope"); !errors.As(err, &fields) || fields["code"] != "coupon" {
		t.Errorf("unknown coupon: %v", err)
	}
	if err := ApplyCoupon(c, " shanhai10 "); err != nil {
		t.Fatal(err)
	}
	cart := Get(id, zh)
	if cart.Coupon != "SHANHAI10" || cart.DiscountCents != cart.SubtotalCents/10 {
		t.Errorf("10%% off %d: %d", cart.SubtotalCents, cart.DiscountCents)
	}
	if err := ApplyCoupon(c, "WELCOME30"); err != nil {
		t.Fatal(err)
	}
	if cart := Get(id, en); cart.DiscountCents != store.Price(3000, en) {
		t.Errorf("30 yuan off in dollars: %d", cart.DiscountCents)
	}
	// Standard delivery is free from the threshold, after the discount.
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	cheap := Cart{SubtotalCents: 10000, FreeShippingCents: FreeShipping(zh)}
	dear := Cart{SubtotalCents: 40000, DiscountCents: 5000, FreeShippingCents: FreeShipping(zh)}
	if d := Deliveries(cheap, now, zh); d[0].ID != "standard" || d[0].PriceCents != 1500 || !d[1].Earliest.Equal(d[1].Latest) {
		t.Errorf("deliveries of a small cart %+v", d)
	}
	if d := Deliveries(dear, now, zh); d[0].PriceCents != 0 || !d[0].Earliest.Equal(now.Add(72*time.Hour)) {
		t.Errorf("deliveries of a large cart %+v", d)
	}
}

func goodOrder() Order {
	return Order{
		Email:    "lin@example.com",
		Address:  Address{Name: "林夏", Phone: "138 0013 8000", Country: "CN", Region: "上海市", City: "上海市", Line1: "武康路 99 号", PostalCode: "200031"},
		Delivery: "express", Payment: "card", CardNumber: "4242 4242 4242 4242", CardExpiry: "08/29", CardCVC: "123", BillingSame: true, Terms: true,
	}
}

func TestOrdersAreCheckedFieldByField(t *testing.T) {
	if err := goodOrder().Validate(); err != nil {
		t.Fatalf("a good order: %v", err)
	}
	for _, tc := range []struct {
		change func(*Order)
		field  string
		msg    string
	}{
		{func(o *Order) { o.Email = "lin" }, "email", "email"},
		{func(o *Order) { o.Address.Name = " " }, "address.name", "required"},
		{func(o *Order) { o.Address.Phone = "12" }, "address.phone", "phone"},
		{func(o *Order) { o.Address.PostalCode = "2000" }, "address.postalCode", "postalCode"},
		{func(o *Order) { o.Address.Country, o.Address.PostalCode = "US", "94105-12" }, "address.postalCode", "postalCode"},
		{func(o *Order) { o.Address.Country, o.Address.PostalCode = "SG", "" }, "address.postalCode", "required"},
		{func(o *Order) { o.Delivery = "rocket" }, "delivery", "required"},
		{func(o *Order) { o.CardNumber = "4242 4242 4242 4241" }, "cardNumber", "card"},
		{func(o *Order) { o.CardExpiry = "13/29" }, "cardExpiry", "expiry"},
		{func(o *Order) { o.CardCVC = "12" }, "cardCvc", "cvc"},
		{func(o *Order) { o.Notes = strings.Repeat("字", 201) }, "notes", "tooLong"},
		{func(o *Order) { o.Terms = false }, "terms", "terms"},
		{func(o *Order) { o.Payment = "cash" }, "payment", "required"},
	} {
		o := goodOrder()
		tc.change(&o)
		var fields tifa.Fields
		if err := o.Validate(); !errors.As(err, &fields) || len(fields) != 1 || fields[tc.field] != tc.msg {
			t.Errorf("want %s: %s, got %v", tc.field, tc.msg, err)
		}
	}
	// Card fields count only when paying by card.
	o := goodOrder()
	o.Payment, o.CardNumber = "alipay", ""
	if err := o.Validate(); err != nil {
		t.Errorf("alipay without a card: %v", err)
	}
}

func TestPlacingAnOrderEmptiesTheCart(t *testing.T) {
	c, _ := visit(t, "")
	id, err := Add(c, "ceramic-cup", "moss", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	c, _ = visit(t, id)
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	order, err := PlaceOrder(c, goodOrder(), now)
	if err != nil || !strings.HasPrefix(order, "SS-") {
		t.Fatalf("PlaceOrder: %q %v", order, err)
	}
	if n := Count(id); n != 0 {
		t.Errorf("%d pieces left after the order", n)
	}
	p, err := GetPlaced(order, zh)
	if err != nil || p.Count != 6 || p.Email != "lin@example.com" || p.Delivery.ID != "express" {
		t.Errorf("placed %+v %v", p, err)
	}
	if _, err := PlaceOrder(c, goodOrder(), now); err == nil {
		t.Error("an empty cart placed an order")
	}
	if _, err := GetPlaced("SS-1", zh); !errors.Is(err, tifa.ErrNotFound) {
		t.Errorf("unknown order: %v", err)
	}
}
