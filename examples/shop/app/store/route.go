package store

import (
	"net/http"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store/storekit"
)

// GET sends a visitor of /store to the storefront in the language their browser prefers. The
// answer depends on Accept-Language, so caches keep one for each.
func GET(c *tifa.Ctx) error {
	c.Writer().Header().Add("Vary", "Accept-Language")
	return tifa.Redirect("/store/"+storekit.Preferred(c.Header("Accept-Language")).Code, http.StatusFound)
}
