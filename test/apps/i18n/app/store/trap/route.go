// Package trap is a page of the area app/store whose Load redirects to an address that is not a web page.
package trap

import "github.com/liustack/tifa"

func Load(c *tifa.Ctx) (struct{}, error) {
	return struct{}{}, tifa.Redirect("javascript:window.__pwned=true", 302)
}
