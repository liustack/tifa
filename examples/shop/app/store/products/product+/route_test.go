package product

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liustack/tifa/examples/shop/app/store/cart"
	"github.com/liustack/tifa/examples/shop/app/store/products"
	"github.com/liustack/tifa/tifatest"
)

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

// pageData is the data of the page of a product, as JSON, with the delivery left out: it holds the
// day, which may change while the test runs.
func pageData(t *testing.T, slug string) string {
	t.Helper()
	r := httptest.NewRequest("GET", "/store/products/"+slug, nil)
	r.SetPathValue("product", slug)
	c, _ := tifatest.NewCtx(t, r)
	d, err := Load(c)
	if err != nil {
		t.Fatal(err)
	}
	d.Delivery = cart.Delivery{}
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// The page of a product gets of the stock what it shows: of the product itself and of each related
// card whether it is in stock and the pieces left when few are, and nothing for the ranking and the
// bundle. A sale that leaves a product in good supply changes no page, its own included.
func TestTheProductPageGetsOfTheStockWhatItShows(t *testing.T) {
	// The kettle (23 pieces) is the product of its page, and on the page of the cup it is a related
	// card and in the ranking of the category.
	kettle, _ := products.GetEntry("pour-over-kettle")
	own, other := pageData(t, "pour-over-kettle"), pageData(t, "ceramic-cup")
	if strings.Contains(own, `"stock"`) {
		t.Error("the page gets a count of pieces")
	}
	if !strings.Contains(other, `"pour-over-kettle"`) {
		t.Fatal("the page of the cup does not show the kettle")
	}
	setStock(t, "pour-over-kettle", kettle.Stock-2)
	if pageData(t, "pour-over-kettle") != own || pageData(t, "ceramic-cup") != other {
		t.Error("a sale of a product in good supply changes the data of its page or of the page of another product")
	}
	setStock(t, "pour-over-kettle", 11)
	if pageData(t, "pour-over-kettle") != own || pageData(t, "ceramic-cup") != other {
		t.Error("11 pieces left change the data of a page")
	}
	// From 10 pieces down the pages count them, and each sale shows.
	setStock(t, "pour-over-kettle", 10)
	ten, otherTen := pageData(t, "pour-over-kettle"), pageData(t, "ceramic-cup")
	if ten == own || otherTen == other {
		t.Error("10 pieces left change no data, and the pages say how many are left")
	}
	setStock(t, "pour-over-kettle", 9)
	if pageData(t, "pour-over-kettle") == ten || pageData(t, "ceramic-cup") == otherTen {
		t.Error("9 pieces left are the data of 10")
	}
}
