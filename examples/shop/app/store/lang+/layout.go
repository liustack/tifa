// Package lang is the storefront in one language: its Layout applies to /store/{lang} and every
// page below it, and its page is the storefront's home.
package lang

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/cart"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/products"
)

// Shell is what every page of the storefront shows around itself: the language, the
// categories in the menu and the number of pieces in the cart.
type Shell struct {
	Locale     store.Locale        `json:"locale"`
	Categories []products.Category `json:"categories"`
	CartCount  int                 `json:"cartCount"`
	// FreeShippingCents is the subtotal from which standard delivery is free.
	FreeShippingCents int `json:"freeShippingCents"`
}

func Layout(c *tifa.Ctx) (Shell, error) {
	l, err := store.FindLocale(c.Param("lang"))
	if err != nil {
		return Shell{}, err
	}
	return Shell{Locale: l, Categories: products.Categories(l), CartCount: cart.Count(cart.ID(c)), FreeShippingCents: cart.FreeShipping(l)}, nil
}
