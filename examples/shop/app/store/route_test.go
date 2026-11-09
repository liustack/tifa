package store

import (
	"net/http/httptest"
	"slices"
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
	for name, list := range map[string][]products.Card{"picks": home.Picks, "drinkware": home.ByCategory[0].Products} {
		i := slices.IndexFunc(list, func(p products.Card) bool { return p.Slug == "ceramic-cup" })
		if i < 0 || list[i].Name != "Dawn Cup" || list[i].PriceCents != 4220 {
			t.Errorf("%s: %+v", name, list)
		}
	}
	shell, err := Layout(c)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(shell.Menu[0].Featured, func(p products.Card) bool { return p.Name == "Dawn Cup" }) {
		t.Errorf("the menu shows %+v", shell.Menu[0].Featured)
	}
}
