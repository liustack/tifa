package mine

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa/examples/shop/app/store/cart"
	"github.com/liustack/tifa/examples/shop/app/store/products"
	"github.com/liustack/tifa/examples/shop/app/store/storekit"
	"github.com/liustack/tifa/tifatest"
)

var (
	zh, _ = storekit.FindLocale("zh-CN")
	en, _ = storekit.FindLocale("en-US")
)

func request(method, seen string) *http.Request {
	r := httptest.NewRequest(method, "/zh-CN/store/mine", nil)
	if seen != "" {
		r.AddCookie(&http.Cookie{Name: products.SeenCookie, Value: seen})
	}
	return r
}

func TestGETAnswersWithTheCartAndTheSeenProductsOfTheCookies(t *testing.T) {
	c, _ := tifatest.NewCtx(t, request("GET", "oak-tray.ceramic-cup"))
	got, err := GET(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Seen) != 2 || got.Seen[0].Slug != "oak-tray" || got.Cart.FreeShippingCents == 0 {
		t.Errorf("mine %+v", got)
	}
	c, _ = tifatest.NewCtx(t, request("GET", ""))
	if got, err = GET(c); err != nil || got.Seen == nil || len(got.Seen) != 0 {
		t.Errorf("a visitor who saw nothing: %+v %v", got.Seen, err)
	}
	if want := cart.Get("", zh); got.Cart.Count != want.Count {
		t.Errorf("cart count %d, want the demonstration cart's %d", got.Cart.Count, want.Count)
	}
}

func TestPOSTRemembersTheProduct(t *testing.T) {
	c, w := tifatest.NewCtx(t, request("POST", "oak-tray"))
	if err := POST(c, SeenInput{Slug: "ceramic-cup"}); err != nil {
		t.Fatal(err)
	}
	cookie, err := http.ParseSetCookie(w.Header().Get("Set-Cookie"))
	if err != nil || cookie.Name != products.SeenCookie || cookie.Value != "ceramic-cup.oak-tray" {
		t.Errorf("cookie %v %v", cookie, err)
	}
	c, w = tifatest.NewCtx(t, request("POST", ""))
	if err := POST(c, SeenInput{Slug: "not-a-product"}); err != nil || w.Header().Get("Set-Cookie") != "" {
		t.Errorf("unknown slug: %v %v", err, w.Header())
	}
}

func TestGETAnswersInTheLanguageOfThePrefix(t *testing.T) {
	r := httptest.NewRequest("GET", "/store/mine", nil)
	c, _ := tifatest.NewCtx(t, r)
	got, err := GET(c)
	if err != nil {
		t.Fatal(err)
	}
	if want := cart.Get("", en); got.Cart.Lines[0].Name != want.Lines[0].Name {
		t.Errorf("first line %q, want the English %q", got.Cart.Lines[0].Name, want.Lines[0].Name)
	}
}
