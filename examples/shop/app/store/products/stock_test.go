package products

import (
	"encoding/json"
	"strings"
	"testing"
)

// setStock gives a product a stock for the test, as staff do in the back office.
func setStock(t *testing.T, slug string, stock int) {
	t.Helper()
	if err := Apply(slug, Change{Stock: &stock}); err != nil {
		t.Fatal(err)
	}
}

// sent is the data a page gets for v, as JSON.
func sent(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A card says how many are left only when few are. With more it says "in stock", so the card of a
// product in good supply is the same data after each sale.
func TestACardCountsThePiecesOnlyWhenFewAreLeft(t *testing.T) {
	t.Cleanup(resetCatalogue)
	cards := map[int]string{}
	for _, tc := range []struct {
		stock   int
		inStock bool
		low     int
	}{{0, false, 0}, {1, true, 1}, {10, true, 10}, {11, true, 0}, {181, true, 0}, {183, true, 0}} {
		setStock(t, "ceramic-cup", tc.stock)
		c := catalogueCard(t, "ceramic-cup")
		if c.InStock != tc.inStock || c.LowStock != tc.low {
			t.Errorf("with %d pieces the card has in stock %v and %d left to count, want %v and %d", tc.stock, c.InStock, c.LowStock, tc.inStock, tc.low)
		}
		cards[tc.stock] = sent(t, c)
		if strings.Contains(cards[tc.stock], `"stock"`) {
			t.Errorf("with %d pieces the card sends the pieces: %s", tc.stock, cards[tc.stock])
		}
	}
	if cards[183] != cards[181] || cards[181] != cards[11] {
		t.Errorf("a sale of a product in good supply changes its card:\n%s\n%s\n%s", cards[183], cards[181], cards[11])
	}
	if cards[11] == cards[10] || cards[10] == cards[1] || cards[1] == cards[0] {
		t.Error("a sale of a product with few pieces left does not change its card")
	}
}

func catalogueCard(t *testing.T, slug string) Card {
	t.Helper()
	p, ok := record(slug)
	if !ok {
		t.Fatalf("no product %s", slug)
	}
	return p.card(en)
}

// The page of a product gets what its card gets of the stock. The cart still needs the pieces, which
// Product keeps for Go and does not send.
func TestAProductPageGetsNoPieceCount(t *testing.T) {
	t.Cleanup(resetCatalogue)
	pages := map[int]string{}
	for _, stock := range []int{0, 10, 11, 183, 181} {
		setStock(t, "ceramic-cup", stock)
		p, err := GetProduct("ceramic-cup", en)
		if err != nil {
			t.Fatal(err)
		}
		if p.Stock != stock || p.InStock != (stock > 0) {
			t.Errorf("with %d pieces Go sees %d, in stock %v", stock, p.Stock, p.InStock)
		}
		pages[stock] = sent(t, p)
		if strings.Contains(pages[stock], `"stock"`) {
			t.Errorf("with %d pieces the page gets the pieces", stock)
		}
	}
	if pages[183] != pages[181] || pages[181] != pages[11] {
		t.Error("a sale of a product in good supply changes the data of its page")
	}
	if pages[11] == pages[10] || pages[10] == pages[0] {
		t.Error("a sale of a product with few pieces left does not change the data of its page")
	}
}

// The parts of a page that say nothing of the stock get nothing of it: the menu, the ranking of a
// category, the bundle and the products under the reviews of the home. A tile says only whether the
// product is sold out.
func TestWhatShowsNoStockGetsNone(t *testing.T) {
	t.Cleanup(resetCatalogue)
	parts := func() map[string]string {
		tiles := []Tile{}
		for _, c := range Search(Filter{Category: "drinkware", Sort: "rating"}, en).Products[:4] {
			tiles = append(tiles, c.Tile())
		}
		return map[string]string{
			"menu":    sent(t, Menu(en)),
			"ranking": sent(t, BestInCategory("pour-over-kettle", 5, en)),
			"bundle":  sent(t, Bundle("pour-over-kettle", 2, en)),
			"reviews": sent(t, LatestReviews(6, en)),
			"tiles":   sent(t, tiles),
		}
	}
	before := parts()
	for name, data := range before {
		for _, field := range []string{`"stock"`, `"lowStock"`} {
			if strings.Contains(data, field) {
				t.Errorf("the %s gets %s", name, field)
			}
		}
		if name != "tiles" && strings.Contains(data, `"inStock"`) {
			t.Errorf("the %s gets whether a product is in stock", name)
		}
	}
	for _, name := range []string{"menu", "ranking", "bundle", "tiles"} {
		if !strings.Contains(before[name], `"ceramic-cup"`) {
			t.Fatalf("the %s does not show the cup: %s", name, before[name])
		}
	}
	if strings.Contains(before["reviews"], `"priceCents"`) {
		t.Error("the reviews of the home get the price of their product, which they do not show")
	}
	// The cup is in every one of them but the reviews. Selling it down to 3 changes none.
	setStock(t, "ceramic-cup", 3)
	for name, data := range parts() {
		if data != before[name] {
			t.Errorf("3 cups left change the %s", name)
		}
	}
	// Sold out, it leaves the bundle and its tile says so. The menu and the ranking still show it as before.
	setStock(t, "ceramic-cup", 0)
	after := parts()
	for _, name := range []string{"menu", "ranking", "reviews"} {
		if after[name] != before[name] {
			t.Errorf("the cup sold out changes the %s", name)
		}
	}
	if after["tiles"] == before["tiles"] || after["bundle"] == before["bundle"] {
		t.Error("the cup sold out changes neither its tile nor the bundle")
	}
}
