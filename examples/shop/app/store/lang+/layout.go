// Package lang is the storefront in one language: its Layout applies to /store/{lang} and every
// page below it, and its page is the storefront's home.
package lang

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/cart"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/products"
)

// Shell is what every page of the storefront shows around itself: the language, the category
// menu and the cart, which the header opens in a drawer.
type Shell struct {
	Locale store.Locale         `json:"locale"`
	Menu   []products.MenuGroup `json:"menu"`
	Cart   cart.Cart            `json:"cart"`
}

func Layout(c *tifa.Ctx) (Shell, error) {
	l, err := store.FindLocale(c.Param("lang"))
	if err != nil {
		return Shell{}, err
	}
	return Shell{Locale: l, Menu: products.Menu(l), Cart: cart.Get(cart.ID(c), l)}, nil
}
