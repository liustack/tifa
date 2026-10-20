package cart

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
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
		// Only a plain address, as the browser's schema takes: no name around it, and a domain
		// with a dot.
		{func(o *Order) { o.Email = "Lin <lin@example.com>" }, "email", "email"},
		{func(o *Order) { o.Email = "a.b@localhost" }, "email", "email"},
		{func(o *Order) { o.Email = " lin@example.com" }, "email", "email"},
		// Every text field that an order keeps has a limit, so requests cannot fill the memory.
		{func(o *Order) { o.Email = strings.Repeat("a", 243) + "@example.com" }, "email", "email"},
		{func(o *Order) { o.Address.Name = strings.Repeat("林", 101) }, "address.name", "tooLong"},
		{func(o *Order) { o.Address.Region = strings.Repeat("省", 101) }, "address.region", "tooLong"},
		{func(o *Order) { o.Address.City = strings.Repeat("市", 101) }, "address.city", "tooLong"},
		{func(o *Order) { o.Address.Line1 = strings.Repeat("路", 201) }, "address.line1", "tooLong"},
		{func(o *Order) { o.Address.Line2 = strings.Repeat("号", 201) }, "address.line2", "tooLong"},
		{func(o *Order) { o.Address.PostalCode = strings.Repeat("2", 21) }, "address.postalCode", "tooLong"},
		{func(o *Order) { o.Address.Country, o.Address.PostalCode = "SG", strings.Repeat("2", 21) }, "address.postalCode", "tooLong"},
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
		{func(o *Order) { o.Notes = strings.Repeat("🍵", 201) }, "notes", "tooLong"},
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
	// Notes are counted in characters (code points), as the page counts them: 200 emoji fit.
	o := goodOrder()
	o.Email, o.Notes = "lin.xia+shop@mail.example.cn", strings.Repeat("🍵", 200)
	if err := o.Validate(); err != nil {
		t.Errorf("a plain address and 200 emoji: %v", err)
	}
	// Each limit takes its full length, in characters, after the spaces the page trims.
	o = goodOrder()
	o.Email = strings.Repeat("a", 242) + "@example.com"
	o.Address.Name = " " + strings.Repeat("🍵", 100) + " "
	o.Address.Region, o.Address.City = strings.Repeat("省", 100), strings.Repeat("市", 100)
	o.Address.Line1, o.Address.Line2 = strings.Repeat("路", 200), strings.Repeat("号", 200)
	o.Address.Country, o.Address.PostalCode = "SG", strings.Repeat("2", 20)
	if err := o.Validate(); err != nil {
		t.Errorf("every field at its limit: %v", err)
	}
	// Card fields count only when paying by card.
	o = goodOrder()
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
	order, err := PlaceOrder(c, zh, goodOrder(), now)
	if err != nil || !strings.HasPrefix(order, "SS-") {
		t.Fatalf("PlaceOrder: %q %v", order, err)
	}
	if n := Count(id); n != 0 {
		t.Errorf("%d pieces left after the order", n)
	}
	p, err := GetPlaced(order, id, zh)
	if err != nil || p.Count != 6 || p.Email != "lin@example.com" || p.Delivery.ID != "express" {
		t.Errorf("placed %+v %v", p, err)
	}
	if _, err := PlaceOrder(c, zh, goodOrder(), now); err == nil {
		t.Error("an empty cart placed an order")
	}
	if _, err := GetPlaced("SS-1", id, zh); !errors.Is(err, tifa.ErrNotFound) {
		t.Errorf("unknown order: %v", err)
	}
}

func TestOnlyTheCartThatPlacedAnOrderSeesIt(t *testing.T) {
	now := time.Now()
	var orders, ids []string
	for range 2 {
		c, w := visit(t, "")
		order, err := PlaceOrder(c, zh, goodOrder(), now)
		if err != nil {
			t.Fatal(err)
		}
		cookie, err := http.ParseSetCookie(w.Header().Get("Set-Cookie"))
		if err != nil {
			t.Fatal(err)
		}
		orders, ids = append(orders, order), append(ids, cookie.Value)
	}
	// Order numbers are random, so one order says nothing about the number of the next.
	pattern := regexp.MustCompile(`^SS-[A-Z2-7]{10}$`)
	for _, order := range orders {
		if !pattern.MatchString(order) {
			t.Errorf("order number %q", order)
		}
	}
	if _, err := GetPlaced(orders[0], ids[0], zh); err != nil {
		t.Errorf("the cart that placed the order: %v", err)
	}
	for _, id := range []string{ids[1], "", "nope"} {
		if _, err := GetPlaced(orders[0], id, zh); !errors.Is(err, tifa.ErrNotFound) {
			t.Errorf("cart %q sees another's order: %v", id, err)
		}
	}
}

func TestACartPlacesOneOrder(t *testing.T) {
	c, _ := visit(t, "")
	id, err := Add(c, "ceramic-cup", "moss", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	// Many clicks on the button at once place one order: the others find the cart empty.
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 20)
	for i := range errs {
		c, _ := visit(t, id)
		wg.Go(func() {
			<-start
			_, errs[i] = PlaceOrder(c, zh, goodOrder(), time.Now())
		})
	}
	close(start)
	wg.Wait()
	placedOrders := 0
	for _, err := range errs {
		var e tifa.Error
		switch {
		case err == nil:
			placedOrders++
		case !errors.As(err, &e) || e.Status != 409:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if placedOrders != 1 {
		t.Errorf("one cart placed %d orders", placedOrders)
	}
}

func TestFailedChangesMakeNoCart(t *testing.T) {
	c, w := visit(t, "")
	if _, err := SetQuantity(c, "nope.mist", 1); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("change an unknown line: %v", err)
	}
	if cookie := w.Header().Get("Set-Cookie"); cookie != "" {
		t.Errorf("a failed change gave a cart: %s", cookie)
	}
}

func TestOnlyCartIDsTheServerMadeAreKept(t *testing.T) {
	forged := strings.Repeat("f", 3000)
	c, w := visit(t, forged)
	if _, err := PlaceOrder(c, zh, goodOrder(), time.Now()); err != nil {
		t.Fatal(err)
	}
	cookie := w.Header().Get("Set-Cookie")
	if strings.Contains(cookie, forged) || !strings.Contains(cookie, CookieName+"=") {
		t.Errorf("checking out with a forged cart ID set %.80s", cookie)
	}
	if n := Count(forged); n != Get("", zh).Count {
		t.Errorf("the forged ID holds a cart of %d pieces", n)
	}
	c, w = visit(t, forged)
	id, err := Add(c, "ceramic-cup", "moss", "", 1)
	if err != nil || id == forged || !strings.Contains(w.Header().Get("Set-Cookie"), CookieName+"="+id) {
		t.Errorf("adding with a forged cart ID: %.80s %v", id, err)
	}
}

func TestRecentForgetsTheIdleAndTheLeastUsed(t *testing.T) {
	r := newRecent[int](3, time.Hour)
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	for i, key := range []string{"a", "b", "c"} {
		r.add(key, i, at)
	}
	if v, ok := r.get("a", at.Add(time.Minute)); !ok || v != 0 {
		t.Fatalf("a: %d %v", v, ok)
	}
	// b is now the least recently used, so a fourth key pushes it out.
	r.add("d", 3, at.Add(2*time.Minute))
	if _, ok := r.get("b", at.Add(2*time.Minute)); ok || r.len() != 3 {
		t.Errorf("b kept past the limit, %d held", r.len())
	}
	// c was last used at the start: an hour and a second later it is gone, a is not.
	later := at.Add(time.Hour + time.Second)
	if _, ok := r.get("c", later); ok {
		t.Error("c kept after an idle hour")
	}
	if _, ok := r.get("a", later); !ok {
		t.Error("a forgotten within an hour of its last use")
	}
	// Adding sweeps out what has been idle too long.
	r.add("e", 4, at.Add(3*time.Hour))
	if r.len() != 1 {
		t.Errorf("%d held after everything else went idle", r.len())
	}
	if carts.limit != maxCarts || placed.limit != maxPlaced {
		t.Errorf("limits %d and %d", carts.limit, placed.limit)
	}
}

func TestAnOrderIsTotalledInTheCurrencyOfItsStorefront(t *testing.T) {
	c, w := visit(t, "")
	if err := ApplyCoupon(c, "SHANHAI10"); err != nil {
		t.Fatal(err)
	}
	cookie, err := http.ParseSetCookie(w.Header().Get("Set-Cookie"))
	if err != nil {
		t.Fatal(err)
	}
	id := cookie.Value
	// The checkout of the English storefront adds up the prices it shows, each in dollars.
	now := time.Now()
	cart := Get(id, en)
	want := cart.SubtotalCents - cart.DiscountCents + Deliveries(cart, now, en)[1].PriceCents
	c, _ = visit(t, id)
	order, err := PlaceOrder(c, en, goodOrder(), now)
	if err != nil {
		t.Fatal(err)
	}
	// The order keeps its currency when the confirmation is read in the other language.
	for _, l := range []store.Locale{en, zh} {
		p, err := GetPlaced(order, id, l)
		if err != nil || p.TotalCents != want || p.Currency != "USD" {
			t.Errorf("in %s: %d %s, want %d USD (%v)", l.Code, p.TotalCents, p.Currency, want, err)
		}
	}
}

func TestAQuantityStaysWithinTheStock(t *testing.T) {
	c, _ := visit(t, "")
	id, err := Add(c, "oak-tray", "oat", "M", 1)
	if err != nil {
		t.Fatal(err)
	}
	// The tray has 6 in stock: the cart page offers up to 6, and Go takes no more either.
	c, w := visit(t, id)
	var fields tifa.Fields
	if _, err := SetQuantity(c, "oak-tray.oat.M", 7); !errors.As(err, &fields) || fields["quantity"] != "stock" {
		t.Errorf("set 7 of 6 in stock: %v", err)
	}
	if w.Header().Get("Set-Cookie") != "" {
		t.Error("a refused change set the cookie")
	}
	if _, err := SetQuantity(c, "oak-tray.oat.M", 6); err != nil {
		t.Errorf("set 6 of 6 in stock: %v", err)
	}
}

// The browser checks the order with checkout-schema.ts before sending it. Its email rule, its
// limits and the way it counts characters must be the ones Validate uses, or Go refuses what
// the page let through.
func TestTheBrowserChecksEmailsAndLengthsAsGoDoes(t *testing.T) {
	schema, err := os.ReadFile("../checkout/checkout-schema.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		fmt.Sprintf("export const limits = { email: %d, name: %d, region: %d, city: %d, line1: %d, line2: %d, postalCode: %d, notes: %d }\n",
			maxEmail, maxName, maxPlace, maxPlace, maxLine, maxLine, maxPostalCode, maxNotes),
		"const email = z.string().max(limits.email, 'email').regex(/" + emailPattern.String() + "/, 'email')\n",
		"const atMost = (n: number) => (s: string) => [...s].length <= n\n",
	} {
		if !strings.Contains(string(schema), want) {
			t.Errorf("checkout-schema.ts lacks %s", want)
		}
	}
}
