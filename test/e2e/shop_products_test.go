package e2e

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
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
