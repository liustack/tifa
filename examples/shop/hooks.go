// Package shop is the demonstration shop workspace built with tifa. This file holds its hooks.
package shop

import (
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/liustack/tifa"
)

// open lets the workbench be seen without signing in. SHOP_OPEN=1 is for the comparison with
// Next.js and TanStack Start and for load tests, which cannot sign in. Do not set it on a server.
var open = os.Getenv("SHOP_OPEN") == "1"

// OnRequest keeps visitors who are not signed in out of the workbench and sends them to the
// sign-in page, which sends them back afterwards. The storefront needs no sign-in.
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	path := c.Request().URL.Path
	if !open && (path == "/projects" || strings.HasPrefix(path, "/projects/")) && tifa.Session[Session](c) == nil {
		return tifa.Redirect("/login?next="+url.QueryEscape(c.Request().URL.RequestURI()), 302)
	}
	return next(c)
}

func OnError(c *tifa.Ctx, err error) tifa.Error {
	slog.ErrorContext(c.Context(), "request failed", "path", c.Request().URL.Path, "err", err)
	return tifa.Error{Status: 500, Message: "服务暂时不可用，请稍后重试"}
}
