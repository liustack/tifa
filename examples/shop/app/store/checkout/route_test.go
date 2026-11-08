package checkout

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
	"github.com/liustack/tifa/examples/shop/app/store/cart"
	"github.com/liustack/tifa/examples/shop/app/store/storekit"
	"github.com/liustack/tifa/tifatest"
)

// A checkout places its order at the instant every write of an order takes, orders.Now: the time
// of the server to the millisecond.
func TestACheckoutPlacesItsOrderAtTheTimeOfOrders(t *testing.T) {
	r := httptest.NewRequest("POST", "/zh-CN/store/checkout", nil)
	c, _ := tifatest.NewCtx(t, r)
	in := cart.Order{
		Email:    "lin@example.com",
		Address:  cart.Address{Name: "林夏", Phone: "138 0013 8000", Country: "CN", Region: "上海市", City: "上海市", Line1: "武康路 99 号", PostalCode: "200031"},
		Delivery: "express", Payment: "card", CardNumber: "4242 4242 4242 4242", CardExpiry: "08/29", CardCVC: "123", BillingSame: true, Terms: true,
	}
	before := time.Now()
	var to *tifa.RedirectError
	if err := POST(c, in); !errors.As(err, &to) {
		t.Fatalf("POST: %v", err)
	}
	_, id, ok := strings.Cut(to.URL, "placed=")
	if !ok {
		t.Fatalf("POST sends the visitor to %s", to.URL)
	}
	o, err := orders.GetOrder(storekit.Project, id)
	if err != nil {
		t.Fatal(err)
	}
	if o.PlacedAt.Nanosecond()%int(time.Millisecond) != 0 || o.PlacedAt.Before(before.Truncate(time.Millisecond)) {
		t.Errorf("the order %s was placed at %v, not a millisecond of now", id, o.PlacedAt)
	}
}
