package e2e

import (
	"strings"
	"testing"
)

// checkShopModules checks that the modules of the heavier shop pages are in the HTML the server
// renders, and that the filters and cookies behind them work.
func checkShopModules(t *testing.T, base string) {
	t.Helper()
	for _, tc := range []struct {
		path  string
		wants []string
		not   []string
	}{
		// The product list: facets with counts, quick filters, the view switch, the buying guide
		// and the rows under the list.
		{"/store/products", []string{
			"Quick filters", "Wax and oils", `aria-label="Layout"`, "Buying guide", "Popular searches", "Best rated", "Browse by category",
			"Under $15", "4.8 and up", "Quick view of Morning Pour-Over Kettle", "Ships within 48 hours",
		}, []string{"Recently viewed", "Remove "}},
		// The product page: the whole specification, the comparison, the split of the price, ten
		// reviews a page with the shop's replies, and seven questions.
		{"/store/products/pour-over-kettle", []string{
			"Compared with similar products", "Where the price goes", "How soon it arrives", "Often mentioned", "Best rated in Drinkware",
			"Packing and shipping", "Warranty and returns", "The shop replied:", `aria-label="Page 7 of 7"`, "Showing 10 of 63 reviews", "Official shop",
		}, nil},
		{"/store/products/pour-over-kettle?reviews=7", []string{"Showing 63 of 63 reviews"}, []string{"Showing 10 of 63 reviews"}},
		// Filters by material, badge and rating narrow the list and show as chosen conditions.
		{"/store/products?material=glass&badge=sale", []string{"Glass Carafe", `aria-label="Remove Glass"`, `aria-label="Remove On sale"`}, []string{"Quick view of Oak Tray", "Quick view of Linen Coasters"}},
		{"/zh-CN/store/products?rating=48&category=drinkware", []string{"手作陶瓷杯", `aria-label="移除 4.8 分以上"`}, []string{"玻璃冷水壶"}},
	} {
		status, _, page := get(t, base+tc.path)
		in := rendered(page)
		for _, want := range tc.wants {
			if status != 200 || !strings.Contains(in, want) {
				t.Errorf("%s: %d lacks %s", tc.path, status, want)
			}
		}
		for _, no := range tc.not {
			if strings.Contains(in, no) {
				t.Errorf("%s has %s", tc.path, no)
			}
		}
	}
	// The product page is the same for everyone and sets no cookie. The browser tells the mine route
	// which product is open, which remembers it in a cookie, and the same route gives the list back.
	s := newShopper(t, base)
	if _, header, _ := s.do("GET", "/store/products/ceramic-cup", ""); len(header.Values("Set-Cookie")) != 0 {
		t.Errorf("the product page sets a cookie: %v", header.Values("Set-Cookie"))
	}
	if status, header, _ := s.do("POST", "/store/mine", `{"slug":"ceramic-cup"}`); status != 204 || !strings.Contains(strings.Join(header.Values("Set-Cookie"), ";"), "shop_seen=ceramic-cup") {
		t.Errorf("remember a product: %d %v", status, header.Values("Set-Cookie"))
	}
	if _, _, mine := s.do("GET", "/store/mine", ""); !strings.Contains(mine, `"name":"Handmade Ceramic Cup"`) || !strings.Contains(mine, `"cart":{`) {
		t.Errorf("the visitor's own data lacks the product just seen: %.300s", mine)
	}
}

// checkProductPageCounts checks the product page's questions and the next page of reviews.
func checkProductPageCounts(t *testing.T, base string) {
	t.Helper()
	_, _, page := get(t, base+"/store/products/pour-over-kettle")
	if n := strings.Count(rendered(page), `font-mono text-primary">Q</span>`); n != 7 {
		t.Errorf("%d questions, want 7", n)
	}
	_, _, first := get(t, base+"/store/products/pour-over-kettle")
	_, _, second := get(t, base+"/store/products/pour-over-kettle?reviews=2")
	if reviewsOf(first) == reviewsOf(second) {
		t.Error("the second page of reviews is the first")
	}
}

// reviewsOf returns the part of a product page that holds its reviews.
func reviewsOf(page string) string {
	html := rendered(page)
	i := strings.Index(html, `id="reviews"`)
	j := strings.Index(html, `id="questions"`)
	if i < 0 || j < i {
		return ""
	}
	return html[i:j]
}

// checkOrderPageModules checks the modules of the workbench order page: a waiting order shows what
// to pick and check before shipping, a shipped one how its parcel went. How the carriers did for
// the shop is on the stats page, not here. A note put on an order shows on its page, and an empty
// one is refused.
func checkOrderPageModules(t *testing.T, base string) {
	t.Helper()
	for _, tc := range []struct {
		path  string
		wants []string
		not   []string
	}{
		// 4 to ship: the 2 orders of the data that wait to ship, and the 2 orders that the checkouts of
		// the storefront checks before this one placed in the studio shop.
		{"/projects/studio/orders/SH-1041", []string{
			"Activity", "Internal notes", "Picking list", "Stock check", "Carrier comparison", "Gross profit estimate", "Address check", "Payment and invoice", "Order tags", "Before shipping", "Orders with the same product",
			"两只杯子分开包", "Print order", "Ready to ship", "Stock reserved", "4 to ship",
		}, []string{"Tracking history", "Carrier times", "After shipping"}},
		{"/projects/studio/orders/SH-1040", []string{
			"Tracking history", "After shipping", "Delivered, signed by the recipient", "On the trunk line", "Label printed", "Notification sent", "Sold in shop",
		}, []string{"Picking list", "Stock check", "Before shipping", "Carrier times"}},
	} {
		status, _, page := get(t, base+tc.path)
		in := rendered(page)
		for _, want := range tc.wants {
			if status != 200 || !strings.Contains(in, want) {
				t.Errorf("%s: %d lacks %s", tc.path, status, want)
			}
		}
		for _, no := range tc.not {
			if strings.Contains(in, no) {
				t.Errorf("%s has %s", tc.path, no)
			}
		}
	}
	// A parcel's way has 11 steps, each with a place under a tooltip trigger.
	_, _, shipped := get(t, base+"/projects/studio/orders/SH-1040")
	if n := strings.Count(rendered(shipped), `data-slot="tooltip-trigger"`); n < 11 {
		t.Errorf("%d tooltip triggers on a shipped order", n)
	}
	url := base + "/projects/studio/orders/SH-1041"
	if status, body := send(t, "PUT", url, "same-origin", `{"text":"  "}`); status != 422 || !strings.Contains(body, "Write something") {
		t.Errorf("an empty note: %d %s", status, body)
	}
	if status, body := send(t, "PUT", url, "same-origin", `{"text":"顾客改了送货时间，周三前送到。"}`); status != 200 || !strings.Contains(body, "顾客改了送货时间") {
		t.Errorf("a note: %d %s", status, body)
	}
	_, _, page := get(t, url)
	if !strings.Contains(rendered(page), "顾客改了送货时间，周三前送到。") || !strings.Contains(rendered(page), "Add note") {
		t.Error("the note is not on the order page")
	}
	if status, _ := send(t, "PUT", url, "cross-site", `{"text":"x"}`); status != 403 {
		t.Errorf("a note from another site: %d", status)
	}
}

// checkStatsPanels checks the panels the stats page renders on the server: key numbers, the table
// of every product with its sparklines, the heatmap of order times, the carriers and the customers.
func checkStatsPanels(t *testing.T, base string) {
	t.Helper()
	_, _, page := get(t, base+"/projects/weekend/stats")
	in := rendered(page)
	for _, want := range []string{
		"Pieces sold", "Pieces per order", "Amount to ship", "Repeat customers", "Product sales", "Order times", "Carrier performance", "How fast we ship", "Regulars", "Recent orders",
		"Mon", "Sun", "Within 6 hours", "Over 24 hours", "WK-0201", "野餐垫",
	} {
		if !strings.Contains(in, want) {
			t.Errorf("weekend stats lack %s", want)
		}
	}
	// One sparkline for each of the five products in the table, besides the ones of the key numbers.
	if n := strings.Count(in, `<polyline`); n < 5+2 {
		t.Errorf("%d sparklines", n)
	}
	if strings.Count(in, `role="row"`) != 7 {
		t.Errorf("the heatmap has %d rows, want 7", strings.Count(in, `role="row"`))
	}
	if strings.Contains(in, "recharts") {
		t.Error("recharts rendered on the server")
	}
}

// checkCartAndCheckoutModules checks the coupons, promises and suggestions around the cart and
// the checkout, and that a coupon from the list goes on the cart.
func checkCartAndCheckoutModules(t *testing.T, base string) {
	t.Helper()
	for _, tc := range []struct {
		path  string
		wants []string
	}{
		{"/store/cart", []string{"Coupons you can use", "SHANHAI10", "10% off everything", "$4.30 off orders over $28.20", "Seven day returns", "A person answers"}},
		{"/zh-CN/store/cart", []string{"可用优惠", "全场 9 折", "满 200 元减 30 元", "破损补发"}},
		{"/store/checkout", []string{"Before you pay, one more thing?", "Safe to pay", "Ships within 48 hours"}},
	} {
		status, _, page := get(t, base+tc.path)
		for _, want := range tc.wants {
			if status != 200 || !strings.Contains(rendered(page), want) {
				t.Errorf("%s: %d lacks %s", tc.path, status, want)
			}
		}
	}
	// Eight products are suggested under the cart.
	_, _, cart := get(t, base+"/store/cart")
	i := strings.Index(rendered(cart), "Goes well with")
	if i < 0 {
		t.Fatal("the cart has no suggestions")
	}
	if n := strings.Count(rendered(cart)[i:], `<article`); n < 8 {
		t.Errorf("%d suggested products under the cart", n)
	}
}
