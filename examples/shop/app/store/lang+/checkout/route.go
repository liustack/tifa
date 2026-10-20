// Package checkout is the checkout page of the storefront: the order form for the visitor's
// cart, and the confirmation once the order is placed.
package checkout

import (
	"net/http"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/cart"
)

// View is the checkout: the cart and what the form offers, or the order just placed.
type View struct {
	Cart       cart.Cart       `json:"cart"`
	Deliveries []cart.Delivery `json:"deliveries"`
	Countries  []cart.Country  `json:"countries"`
	Saved      []cart.Address  `json:"saved"`
	Email      string          `json:"email"`
	// Placed is the order the visitor just placed, from ?placed=, which the page confirms in
	// place of the form.
	Placed *cart.Placed `json:"placed"`
}

func Load(c *tifa.Ctx) (View, error) {
	l, err := store.FindLocale(c.Param("lang"))
	if err != nil {
		return View{}, err
	}
	if id := c.Query("placed"); id != "" {
		p, err := cart.GetPlaced(id, cart.ID(c), l)
		if err != nil {
			return View{}, err
		}
		return View{Placed: &p}, nil
	}
	ct := cart.Get(cart.ID(c), l)
	email := "linxia@example.com"
	if l.Code == "en-US" {
		email = "emma@example.com"
	}
	return View{
		Cart: ct, Deliveries: cart.Deliveries(ct, time.Now(), l), Countries: cart.Countries(l),
		Saved: cart.SavedAddresses(l), Email: email,
	}, nil
}

// POST places the order and sends the visitor to its confirmation.
func POST(c *tifa.Ctx, in cart.Order) error {
	l, err := store.FindLocale(c.Param("lang"))
	if err != nil {
		return err
	}
	id, err := cart.PlaceOrder(c, l, in, time.Now())
	if err != nil {
		return err
	}
	return c.Redirect("/store/"+c.Param("lang")+"/checkout?placed="+id, http.StatusSeeOther)
}
