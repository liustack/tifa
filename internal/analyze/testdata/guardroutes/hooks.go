package guardroutes

import (
	"strings"

	"github.com/liustack/tifa"
)

func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if c.Pattern() == "/photo" {
		return tifa.ErrNotFound
	}
	if "/account/orders/{id}" != c.Pattern() {
		return tifa.ErrNotFound
	}
	if strings.HasPrefix(c.Pattern(), "/acount/") {
		return tifa.ErrNotFound
	}
	if c.Pattern() == "GET /photos" {
		return tifa.ErrNotFound
	}
	if route := c.Pattern(); route == "/acount" || strings.HasPrefix(route, "/nothing/") {
		return tifa.ErrNotFound
	}
	switch c.Pattern() {
	case "/photos", "/photoz":
		return tifa.ErrNotFound
	case "/files/{path}":
		return tifa.ErrNotFound
	}
	return next(c)
}
