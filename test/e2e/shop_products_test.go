package e2e

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestShopStaffEditsAndCheckoutsReachTheOtherSide checks that the storefront and the back office of
// the shop share their data. A staff edit of a product makes the next answer of its page a render in
// the VM and the one after it a hit of the render memo: the render memo follows the data with no
// code of the app. A checkout becomes an order of the studio shop and lowers the stock, and a
// checkout of more than the stock is refused.
func TestShopStaffEditsAndCheckoutsReachTheOtherSide(t *testing.T) {
	skipUnlessEnabled(t)
	exe := buildAt(t, shopDir(t))
	t.Setenv("SHOP_OPEN", "1")
	base, _, _ := start(t, exe)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	do := func(method, path, body string) (int, http.Header, string) {
		t.Helper()
		req, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if method != "GET" {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", base)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer res.Body.Close()
		b, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, res.Header, string(b)
	}
	// page asks for a storefront page and checks where the answer came from and what it shows.
	page := func(path, render, shows string) {
		t.Helper()
		status, header, body := get(t, base+path)
		if status != 200 || header.Get("X-Tifa-Render") != render {
			t.Errorf("%s: %d from %q, want 200 from %q", path, status, header.Get("X-Tifa-Render"), render)
		}
		if !strings.Contains(rendered(body), shows) {
			t.Errorf("%s does not show %q", path, shows)
		}
	}

	// The product page is in the render memo before the edit.
	get(t, base+"/store/products/oak-tray")
	page("/store/products/oak-tray", "memo", "Oak Serving Tray")
	page("/projects/studio/products/oak-tray", "vm", "Oak Serving Tray")

	edit := `{"priceCents":52000,"stock":5,"nameEn":"Oak Tray, Large","nameZh":"橡木大托盘","descriptionEn":"White oak, carved from one block.","descriptionZh":"整块白橡木挖制。"}`
	if status, _, body := do("PATCH", "/projects/studio/products/oak-tray", edit); status != 200 || !strings.Contains(body, `"nameEn":"Oak Tray, Large"`) {
		t.Fatalf("staff edit: %d %s", status, body)
	}
	if status, _, body := do("PATCH", "/projects/studio/products/oak-tray", strings.Replace(edit, `"stock":5`, `"stock":-1`, 1)); status != 422 || !strings.Contains(body, `"stock"`) {
		t.Errorf("an edit with a stock below 0: %d %s", status, body)
	}
	if status, _, _ := do("PATCH", "/projects/weekend/products/oak-tray", edit); status != 404 {
		t.Errorf("an edit in the weekend shop: %d", status)
	}
	// The next answer of each page renders again with the new data, and the one after it hits.
	page("/store/products/oak-tray", "vm", "Oak Tray, Large")
	page("/store/products/oak-tray", "memo", "Oak Tray, Large")
	page("/zh-CN/store/products/oak-tray", "vm", "橡木大托盘")
	page("/zh-CN/store/products/oak-tray", "memo", "¥520.00")
	page("/projects/studio/products", "vm", "Oak Tray, Large")
	page("/projects/studio/products", "memo", "Oak Tray, Large")

	// A checkout of the demonstration cart with two trays added becomes the next order of the studio shop and takes them from the stock.
	if status, _, body := do("POST", "/store/products/oak-tray", `{"color":"oat","size":"M","quantity":2}`); status != 200 {
		t.Fatalf("add to the cart: %d %s", status, body)
	}
	order := `{"email":"emma@example.com","address":{"name":"Emma Walker","phone":"+1 415 555 0134","country":"US","region":"CA","city":"San Francisco","line1":"1 Market Street","line2":"","postalCode":"94105"},"delivery":"express","payment":"alipay","cardNumber":"","cardExpiry":"","cardCvc":"","billingSame":true,"notes":"","newsletter":false,"terms":true}`
	status, header, body := do("POST", "/store/checkout", order)
	placed := header.Get("Location")
	if status != 303 || placed != "/store/checkout?placed=SH-1043" {
		t.Fatalf("checkout: %d %s %s", status, placed, body)
	}
	if _, _, page := do("GET", placed, ""); !strings.Contains(rendered(page), "SH-1043") {
		t.Errorf("the confirmation does not show the number of the order")
	}
	if status, _, body := get(t, base+"/projects/studio/orders/SH-1043"); status != 200 || !strings.Contains(rendered(body), "Emma Walker") || !strings.Contains(rendered(body), "Oak Tray, Large") {
		t.Errorf("the back office order: %d\n%.400s", status, rendered(body))
	}
	if _, _, body := get(t, base+"/projects/studio"); !strings.Contains(rendered(body), "SH-1043") {
		t.Errorf("the order list of the studio shop does not show SH-1043")
	}
	page("/store/products/oak-tray", "vm", "Only 3 left")

	// The cart takes 3 more trays only up to the stock, so the stock goes down to 1 under it: a
	// checkout of 3 is then refused, and nothing changes.
	if status, _, body := do("POST", "/store/products/oak-tray", `{"color":"oat","size":"L","quantity":3}`); status != 200 {
		t.Fatalf("add to the cart: %d %s", status, body)
	}
	if status, _, body := do("PATCH", "/projects/studio/products/oak-tray", strings.Replace(edit, `"stock":5`, `"stock":1`, 1)); status != 200 {
		t.Fatalf("staff edit: %d %s", status, body)
	}
	if status, _, body := do("POST", "/store/checkout", order); status != 409 || !strings.Contains(body, "cartStock") {
		t.Errorf("a checkout over the stock: %d %s", status, body)
	}
	if _, _, body := get(t, base+"/projects/studio"); strings.Contains(rendered(body), "SH-1044") {
		t.Errorf("a refused checkout placed an order")
	}
	if _, _, body := do("GET", "/projects/studio/products/oak-tray", ""); !strings.Contains(rendered(body), "1 in stock") {
		t.Errorf("a refused checkout changed the stock")
	}
}

// TestShopASaleInGoodSupplyLeavesThePagesInTheMemo checks what the storefront gets of the stock: a page
// gets what it shows. The menu, the picks and the rankings show nothing of it, a tile shows whether a
// product is sold out, and a card and the page of a product count the pieces only when 10 or fewer
// are left. So after a checkout of products in good supply every page comes from the render memo, the
// pages of the products just bought among them. From 10 pieces down each sale shows on the pages that
// count them, which render again, and on no other.
func TestShopASaleInGoodSupplyLeavesThePagesInTheMemo(t *testing.T) {
	skipUnlessEnabled(t)
	exe := buildAt(t, shopDir(t))
	t.Setenv("SHOP_OPEN", "1")
	base, _, _ := start(t, exe)
	started := time.Now()
	staff := newShopper(t, base)
	stock := func(slug string, pieces int) {
		t.Helper()
		if status, _, body := staff.do("PATCH", "/projects/studio/products/"+slug, `{"stock":`+strconv.Itoa(pieces)+`}`); status != 200 {
			t.Fatalf("the stock of %s: %d %s", slug, status, body)
		}
	}
	// buy is a visitor who adds one pack of coasters to the cart and checks out. The cart of a new
	// visitor holds the demonstration cart, so the order takes a kettle, two cups, an apron and a candle too.
	buy := func() {
		t.Helper()
		visitor := newShopper(t, base)
		if status, _, body := visitor.do("POST", "/store/products/linen-coasters", `{"color":"oat","size":"","quantity":1}`); status != 200 {
			t.Fatalf("add to the cart: %d %s", status, body)
		}
		order := `{"email":"emma@example.com","address":{"name":"Emma Walker","phone":"+1 415 555 0134","country":"US","region":"CA","city":"San Francisco","line1":"1 Market Street","line2":"","postalCode":"94105"},"delivery":"express","payment":"alipay","cardNumber":"","cardExpiry":"","cardCvc":"","billingSame":true,"notes":"","newsletter":false,"terms":true}`
		if status, header, body := visitor.do("POST", "/store/checkout", order); status != 303 {
			t.Fatalf("checkout: %d %s %s", status, header.Get("Location"), body)
		}
	}
	// from asks for a page and checks where its answer came from and, when shows is not empty, a text
	// it shows. The delivery dates of a product page are days of the store, so a day that changed
	// while the test ran renders pages again: the test is then not about the stock, and is skipped.
	from := func(path, render, shows string) {
		t.Helper()
		status, header, body := get(t, base+path)
		if got := header.Get("X-Tifa-Render"); status != 200 || got != render {
			if storeDayChanged(started, time.Now()) {
				t.Skip("the day of the store changed while the test ran")
			}
			t.Errorf("%s: %d from %q, want 200 from %q", path, status, got, render)
		}
		if shows != "" && !strings.Contains(rendered(body), shows) {
			t.Errorf("%s does not show %q", path, shows)
		}
	}
	const (
		home      = "/store"
		homeZH    = "/zh-CN/store"
		own       = "/store/products/linen-coasters"
		other     = "/store/products/oak-tray"           // drinkware: no coasters on its page
		with      = "/store/products?q=coaster"          // a list that shows the coasters
		without   = "/store/products?category=drinkware" // a list that does not
		drinkware = "/store/products/ceramic-cup"        // the page of a product of the demonstration cart
	)
	pages := []string{home, homeZH, own, other, with, without, drinkware}
	memo := func(why string, but ...string) {
		t.Helper()
		for _, p := range pages {
			if !slices.Contains(but, p) {
				from(p, "memo", "")
			}
		}
		if t.Failed() {
			t.Fatalf("pages rendered again %s", why)
		}
	}

	// Everything an order takes is in good supply: 80 packs of coasters, and 200 of each product of the
	// demonstration cart.
	for _, slug := range []string{"pour-over-kettle", "ceramic-cup", "linen-apron", "reading-candle"} {
		stock(slug, 200)
	}
	for _, p := range pages {
		from(p, "vm", "")
	}
	memo("with no write")
	if _, _, body := get(t, base+with); !strings.Contains(rendered(body), "Linen Coasters") {
		t.Fatalf("the search for coaster does not show the coasters")
	}

	// A checkout: the order is placed, and no page of the storefront shows anything else.
	buy()
	if _, _, body := staff.do("GET", "/projects/studio/products/linen-coasters", ""); !strings.Contains(rendered(body), "79 in stock") {
		t.Fatal("the checkout did not take a pack of coasters from the stock")
	}
	memo("after a checkout of products in good supply")
	// 11 packs left are still "in stock", and the page says no number.
	stock("linen-coasters", 11)
	memo("with 11 packs of coasters left")

	// The next sale leaves 10: the page of the coasters and the list that shows their card count them,
	// and the other pages are as they were.
	buy()
	from(own, "vm", "Only 10 left")
	from(with, "vm", "Only 10 left")
	memo("after a sale that left 10 packs of coasters", own, with)
	memo("with no write")
	buy()
	from(own, "vm", "Only 9 left")
	from(with, "vm", "Only 9 left")
	memo("after a sale that left 9 packs of coasters", own, with)
}
