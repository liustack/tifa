package tifa

import "github.com/liustack/tifa/internal/core"

// ExpirePages has the whole-page cache make the pages at paths again: the next request for one of
// them runs its Loads and renders it, as when its Cache time is over. Call it after a write that
// changes what such a page shows, such as a method handler that saves a product, with the path of
// every page that shows it.
//
// A path is the path of the URL without the query, decoded as c.Request().URL.Path has it:
// "/store/en/products/café", not "/store/en/products/caf%C3%A9". It expires that page for every
// query string, host and scheme. Paths match exactly: "/blog" and "/blog/" are two pages, and
// neither expires the pages below it.
//
// A request for the page that started before the call may still get the old page, and a page made
// from data read before the call is not kept. When making the new page fails, the old one stands in
// as when its time is over. A CDN in front keeps its copy until its s-maxage runs out.
//
// It does nothing for a path with no kept page, and before the server starts or after it stops.
// It panics for a path that does not start with "/", which is a mistake in the code.
func ExpirePages(paths ...string) { core.ExpirePages(paths) }
