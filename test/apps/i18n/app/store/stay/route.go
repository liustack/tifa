// Package stay is a page of the area app/store whose Load redirects to another page of it, in the
// language of the request.
package stay

import "github.com/liustack/tifa"

func Load(c *tifa.Ctx) (struct{}, error) {
	if c.Language() == "en-US" {
		return struct{}{}, tifa.Redirect("/en-US/store/cart", 302)
	}
	return struct{}{}, tifa.Redirect("/store/cart", 302)
}
