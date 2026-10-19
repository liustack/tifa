package cart

import (
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/products"
)

// View is the cart page: the cart, how it can be delivered, and products to add.
type View struct {
	Cart       Cart            `json:"cart"`
	Deliveries []Delivery      `json:"deliveries"`
	Suggested  []products.Card `json:"suggested"`
}

func Load(c *tifa.Ctx) (View, error) {
	l, err := store.FindLocale(c.Param("lang"))
	if err != nil {
		return View{}, err
	}
	cart := Get(ID(c), l)
	slug := "ceramic-cup"
	if len(cart.Lines) > 0 {
		slug = cart.Lines[0].Slug
	}
	return View{Cart: cart, Deliveries: Deliveries(cart, time.Now(), l), Suggested: products.Related(slug, 4, l)}, nil
}

type QuantityInput struct {
	Line     string `json:"line"`
	Quantity int    `json:"quantity"`
}

// PATCH changes the quantity of a line, 0 to remove it, and answers with the cart.
func PATCH(c *tifa.Ctx, in QuantityInput) (Cart, error) {
	l, err := store.FindLocale(c.Param("lang"))
	if err != nil {
		return Cart{}, err
	}
	id, err := SetQuantity(c, in.Line, in.Quantity)
	if err != nil {
		return Cart{}, err
	}
	return Get(id, l), nil
}

type CouponInput struct {
	Code string `json:"code"`
}

// POST puts a coupon on the cart, or takes it off for an empty code.
func POST(c *tifa.Ctx, in CouponInput) error {
	return ApplyCoupon(c, in.Code)
}
