package store

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/liustack/tifa/examples/shop/app/store/products"
	"github.com/liustack/tifa/tifatest"
)

// The cup is one of the picks, and the best rated of its category, so the menu shows it too.
func TestTheHomePageShowsAnEditOfAProduct(t *testing.T) {
	before, err := products.GetEntry("ceramic-cup")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = products.Update("ceramic-cup", before.Edit) })
	edit := before.Edit
	edit.NameEN, edit.PriceCents = "Dawn Cup", 29900
	if err := products.Update("ceramic-cup", edit); err != nil {
		t.Fatal(err)
	}
	c, _ := tifatest.NewCtx(t, httptest.NewRequest("GET", "/store", nil))
	home, err := Load(c)
	if err != nil {
		t.Fatal(err)
	}
	if i := slices.IndexFunc(home.Picks, func(p Pick) bool { return p.Slug == "ceramic-cup" }); i < 0 || home.Picks[i].Name != "Dawn Cup" || home.Picks[i].PriceCents != 4220 {
		t.Errorf("picks: %+v", home.Picks)
	}
	tiles := home.ByCategory[0].Products
	if i := slices.IndexFunc(tiles, func(p products.Tile) bool { return p.Slug == "ceramic-cup" }); i < 0 || tiles[i].Name != "Dawn Cup" || tiles[i].PriceCents != 4220 {
		t.Errorf("drinkware: %+v", tiles)
	}
	shell, err := Layout(c)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(shell.Menu[0].Featured, func(p products.MenuProduct) bool { return p.Name == "Dawn Cup" && p.PriceCents == 4220 }) {
		t.Errorf("the menu shows %+v", shell.Menu[0].Featured)
	}
}

// setStock gives a product a stock for the test and puts back the one it had.
func setStock(t *testing.T, slug string, stock int) {
	t.Helper()
	before, err := products.GetEntry(slug)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = products.Apply(slug, products.Change{Stock: &before.Stock}) })
	if err := products.Apply(slug, products.Change{Stock: &stock}); err != nil {
		t.Fatal(err)
	}
}

// The home and the Layout get of the stock what they show: the cards of the new products and of the
// sale say how many are left when few are, a tile says whether its product is sold out, and the
// menu, the picks and the products under the reviews say nothing. So a sale of a product in good
// supply leaves the data of the home as it was, and the render memo answers the next visitor.
func TestTheHomeGetsOfTheStockWhatItShows(t *testing.T) {
	c, _ := tifatest.NewCtx(t, httptest.NewRequest("GET", "/store", nil))
	data := func() (home, shell, picks string) {
		t.Helper()
		h, err := Load(c)
		if err != nil {
			t.Fatal(err)
		}
		s, err := Layout(c)
		if err != nil {
			t.Fatal(err)
		}
		text := func(v any) string {
			out, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			return string(out)
		}
		return text(h), text(s), text(h.Picks)
	}
	home, shell, picks := data()
	for name, text := range map[string]string{"home": home, "Layout": shell} {
		if strings.Contains(text, `"stock"`) {
			t.Errorf("the %s gets a count of pieces", name)
		}
	}
	for name, text := range map[string]string{"Layout": shell, "picks": picks} {
		if strings.Contains(text, `tock"`) {
			t.Errorf("the %s gets something of the stock, and shows nothing of it: %s", name, text)
		}
	}
	// The cup (58 pieces) and the kettle (23) are picks, tiles of their category and in the menu.
	cup, _ := products.GetEntry("ceramic-cup")
	kettle, _ := products.GetEntry("pour-over-kettle")
	setStock(t, "ceramic-cup", cup.Stock-2)
	setStock(t, "pour-over-kettle", kettle.Stock-1)
	if h, s, _ := data(); h != home || s != shell {
		t.Error("a sale of two products in good supply changes the data of the home or of the Layout")
	}
	// With 3 cups left the menu and the picks are the same. Sold out, they still are, and the tile of
	// the cup says so.
	setStock(t, "ceramic-cup", 3)
	if _, s, p := data(); s != shell || p != picks {
		t.Error("3 cups left change the Layout or the picks")
	}
	setStock(t, "ceramic-cup", 0)
	if h, s, p := data(); s != shell || p != picks || h == home {
		t.Errorf("the cup sold out: the Layout changed %v, the picks changed %v, the home changed %v (want false, false, true)", s != shell, p != picks, h != home)
	}
}
