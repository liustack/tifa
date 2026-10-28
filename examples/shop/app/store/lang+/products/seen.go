package products

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
)

// SeenCookie holds the slugs of the products a visitor looked at last, newest first, joined by
// dots. Only the server reads it.
const SeenCookie = "shop_seen"

// seenMax is how many products the cookie remembers.
const seenMax = 8

// seenSlugs reads the cookie. The visitor can write anything in it, so it keeps only slugs of the
// catalogue, each once.
func seenSlugs(c *tifa.Ctx) []string {
	cookie, err := c.Cookie(SeenCookie)
	if err != nil {
		return nil
	}
	var out []string
	for _, slug := range strings.Split(cookie.Value, ".") {
		if len(out) < seenMax && !slices.Contains(out, slug) && known(slug) {
			out = append(out, slug)
		}
	}
	return out
}

func known(slug string) bool {
	for _, p := range catalogue {
		if p.slug == slug {
			return true
		}
	}
	return false
}

// MarkSeen puts slug in front of the products the visitor has seen and writes the cookie back.
func MarkSeen(c *tifa.Ctx, slug string) {
	if !known(slug) {
		return
	}
	list := []string{slug}
	for _, s := range seenSlugs(c) {
		if s != slug && len(list) < seenMax {
			list = append(list, s)
		}
	}
	c.SetCookie(&http.Cookie{
		Name: SeenCookie, Value: strings.Join(list, "."), Path: "/store", MaxAge: int(30 * 24 * time.Hour / time.Second),
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// Seen returns the products the visitor looked at last, newest first, leaving out except.
func Seen(c *tifa.Ctx, except string, l store.Locale) []Card {
	var out []Card
	for _, slug := range seenSlugs(c) {
		if slug == except {
			continue
		}
		for _, p := range catalogue {
			if p.slug == slug {
				out = append(out, p.card(l))
			}
		}
	}
	return out
}
