// Package shop is the demonstration shop workspace built with tifa. This file holds its hooks.
package shop

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	"github.com/liustack/tifa"
)

// OnStart lists the products and the journal posts for sitemap.xml and llms.txt.
func OnStart(context.Context) error {
	tifa.SetSitemap(sitemap)
	return nil
}

// OnRequest keeps visitors who are not signed in out of the workbench and the metrics and sends
// them to the sign-in page, which sends them back afterwards. The storefront needs no sign-in.
// It asks which route matched, not what the URL says, since one route has many spellings.
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	route := c.Pattern()
	if !Open && (route == "/projects" || strings.HasPrefix(route, "/projects/") || route == "/metrics") && tifa.Session[Session](c) == nil {
		return tifa.Redirect("/login?next="+url.QueryEscape(c.Request().URL.RequestURI()), 302)
	}
	return next(c)
}

func OnError(c *tifa.Ctx, err error) tifa.Error {
	slog.ErrorContext(c.Context(), "request failed", "path", c.Request().URL.Path, "err", err)
	return tifa.Error{Status: 500, Message: "The service is unavailable. Please try again later."}
}
