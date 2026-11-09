package store

import (
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/liustack/tifa/examples/shop/app/store/products"
	"github.com/liustack/tifa/tifatest"
)

func TestTheHomePageShowsAnEditOfAProduct(t *testing.T) {
	before, err := products.GetEntry("pour-over-kettle")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = products.Update("pour-over-kettle", before.Edit) })
	edit := before.Edit
	edit.NameEN, edit.PriceCents = "Dawn Kettle", 29900
	if err := products.Update("pour-over-kettle", edit); err != nil {
		t.Fatal(err)
	}
	c, _ := tifatest.NewCtx(t, httptest.NewRequest("GET", "/store", nil))
	home, err := Load(c)
	if err != nil {
		t.Fatal(err)
	}
	for name, list := range map[string][]products.Card{"picks": home.Picks, "drinkware": home.ByCategory[0].Products} {
		i := slices.IndexFunc(list, func(p products.Card) bool { return p.Slug == "pour-over-kettle" })
		if i < 0 || list[i].Name != "Dawn Kettle" || list[i].PriceCents != 4220 {
			t.Errorf("%s: %+v", name, list)
		}
	}
	shell, err := Layout(c)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(shell.Menu[0].Featured, func(p products.Card) bool { return p.Name == "Dawn Kettle" }) {
		t.Errorf("the menu shows %+v", shell.Menu[0].Featured)
	}
}
