package products

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liustack/tifa/tifatest"
)

func visit(t *testing.T, seen string) (*httptest.ResponseRecorder, func(slug string)) {
	r := httptest.NewRequest("GET", "/store/zh-CN/products/ceramic-cup", nil)
	if seen != "" {
		r.AddCookie(&http.Cookie{Name: SeenCookie, Value: seen})
	}
	c, w := tifatest.NewCtx(t, r)
	return w, func(slug string) { MarkSeen(c, slug) }
}

func TestSeenKeepsTheLastProductsNewestFirst(t *testing.T) {
	w, mark := visit(t, "oak-tray.ceramic-cup")
	mark("glass-carafe")
	got := w.Header().Get("Set-Cookie")
	if !strings.Contains(got, SeenCookie+"=glass-carafe.oak-tray.ceramic-cup") || !strings.Contains(got, "Path=/store") {
		t.Fatalf("cookie %q", got)
	}
	// A product seen again moves to the front instead of twice into the list.
	w, mark = visit(t, "oak-tray.ceramic-cup.glass-carafe")
	mark("ceramic-cup")
	if got := w.Header().Get("Set-Cookie"); !strings.Contains(got, SeenCookie+"=ceramic-cup.oak-tray.glass-carafe;") {
		t.Errorf("cookie %q", got)
	}
}

func TestSeenKeepsOnlyWhatTheCatalogueHasAndTheLastEight(t *testing.T) {
	// The cookie is the visitor's to write: unknown slugs and repeats are dropped when it is read.
	r := httptest.NewRequest("GET", "/store/zh-CN/products", nil)
	r.AddCookie(&http.Cookie{Name: SeenCookie, Value: "nope.oak-tray.oak-tray..<script>.ceramic-cup"})
	c, _ := tifatest.NewCtx(t, r)
	got := Seen(c, "ceramic-cup", zh)
	if len(got) != 1 || got[0].Slug != "oak-tray" {
		t.Errorf("seen %+v", got)
	}
	var slugs []string
	for _, p := range catalogue[:12] {
		slugs = append(slugs, p.slug)
	}
	w, mark := visit(t, strings.Join(slugs, "."))
	mark("canvas-tote")
	cookie, err := http.ParseSetCookie(w.Header().Get("Set-Cookie"))
	if err != nil || len(strings.Split(cookie.Value, ".")) != 8 || !strings.HasPrefix(cookie.Value, "canvas-tote.") {
		t.Errorf("cookie %v %v", cookie, err)
	}
}
