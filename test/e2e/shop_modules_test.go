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
		{"/store/en-US/products", []string{
			"Quick filters", "Wax and oils", `aria-label="Layout"`, "Buying guide", "Popular searches", "Best rated", "Browse by category",
			"Under $15", "4.8 and up", "Quick view of Morning Pour-Over Kettle", "Ships within 48 hours",
		}, []string{"Recently viewed", "Remove "}},
		// The product page: the whole specification, the comparison, the split of the price, ten
		// reviews a page with the shop's replies, and seven questions.
		{"/store/en-US/products/pour-over-kettle", []string{
			"Compared with similar products", "Where the price goes", "How soon it arrives", "Often mentioned", "Best rated in Drinkware",
			"Packing and shipping", "Warranty and returns", "The shop replied:", `aria-label="Page 7 of 7"`, "Showing 10 of 63 reviews", "Official shop",
		}, nil},
		{"/store/en-US/products/pour-over-kettle?reviews=7", []string{"Showing 63 of 63 reviews"}, []string{"Showing 10 of 63 reviews"}},
		// Filters by material, badge and rating narrow the list and show as chosen conditions.
		{"/store/en-US/products?material=glass&badge=sale", []string{"Glass Carafe", `aria-label="Remove Glass"`, `aria-label="Remove On sale"`}, []string{"Quick view of Oak Tray", "Quick view of Linen Coasters"}},
		{"/store/zh-CN/products?rating=48&category=drinkware", []string{"手作陶瓷杯", `aria-label="移除 4.8 分以上"`}, []string{"玻璃冷水壶"}},
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
	// A product page remembers the product in a cookie, and the list shows it under "Recently viewed".
	s := newShopper(t, base)
	if _, header, _ := s.do("GET", "/store/en-US/products/ceramic-cup", ""); !strings.Contains(strings.Join(header.Values("Set-Cookie"), ";"), "shop_seen=ceramic-cup") {
		t.Errorf("the product page does not set shop_seen: %v", header.Values("Set-Cookie"))
	}
	_, _, list := s.do("GET", "/store/en-US/products", "")
	if i := strings.Index(rendered(list), "Recently viewed"); i < 0 || !strings.Contains(rendered(list)[i:], "Handmade Ceramic Cup") {
		t.Error("the list does not show the product just seen")
	}
}

// checkProductPageCounts checks the product page's questions and the next page of reviews.
func checkProductPageCounts(t *testing.T, base string) {
	t.Helper()
	_, _, page := get(t, base+"/store/en-US/products/pour-over-kettle")
	if n := strings.Count(rendered(page), `font-mono text-primary">Q</span>`); n != 7 {
		t.Errorf("%d questions, want 7", n)
	}
	_, _, first := get(t, base+"/store/en-US/products/pour-over-kettle")
	_, _, second := get(t, base+"/store/en-US/products/pour-over-kettle?reviews=2")
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
