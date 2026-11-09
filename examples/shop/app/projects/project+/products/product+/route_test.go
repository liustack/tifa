package product

import (
	"encoding/json/v2"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
	catalogue "github.com/liustack/tifa/examples/shop/app/store/products"
	"github.com/liustack/tifa/examples/shop/app/store/storekit"
	"github.com/liustack/tifa/tifatest"
)

func productCtx(t *testing.T, method, project, product string) *tifa.Ctx {
	r := httptest.NewRequest(method, "/projects/"+project+"/products/"+product, nil)
	r.SetPathValue("project", project)
	r.SetPathValue("product", product)
	c, _ := tifatest.NewCtx(t, r)
	return c
}

func TestLoadFindsTheProductInTheStudioShop(t *testing.T) {
	e, err := tifa.Use(productCtx(t, "GET", "studio", "oak-tray"), Load)
	if err != nil || e.Slug != "oak-tray" || e.NameEN != "Oak Serving Tray" || e.PriceCents != 58000 || e.Stock != 6 {
		t.Fatalf("Load: %+v, %v", e, err)
	}
	for _, tc := range [][2]string{{"studio", "no-such-product"}, {"weekend", "oak-tray"}, {"nowhere", "oak-tray"}} { // the weekend shop does not sell the tray
		if _, err := tifa.Use(productCtx(t, "GET", tc[0], tc[1]), Load); !errors.Is(err, tifa.ErrNotFound) {
			t.Errorf("%s in %s: %v", tc[1], tc[0], err)
		}
	}
}

func TestPATCHChangesTheProductOfTheStorefront(t *testing.T) {
	before, err := catalogue.GetEntry("glass-carafe")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalogue.Update("glass-carafe", before.Edit) })
	edit := before.Edit
	edit.NameEN, edit.PriceCents, edit.Stock = "Tall Carafe", 16800, 40
	got, err := PATCH(productCtx(t, "PATCH", "studio", "glass-carafe"), catalogue.ChangeOf(edit))
	if err != nil || got.NameEN != "Tall Carafe" || got.PriceCents != 16800 || got.Stock != 40 {
		t.Fatalf("PATCH: %+v, %v", got, err)
	}
	en, _ := storekit.FindLocale("en-US")
	if p, _ := catalogue.GetProduct("glass-carafe", en); p.Name != "Tall Carafe" || p.Stock != 40 {
		t.Errorf("the storefront shows %q with %d in stock", p.Name, p.Stock)
	}
	for _, tc := range [][2]string{{"studio", "no-such-product"}, {"weekend", "glass-carafe"}} {
		if _, err := PATCH(productCtx(t, "PATCH", tc[0], tc[1]), catalogue.ChangeOf(edit)); !errors.Is(err, tifa.ErrNotFound) {
			t.Errorf("%s in %s: %v", tc[1], tc[0], err)
		}
	}
	if p, _ := catalogue.GetEntry("glass-carafe"); p.Edit != edit {
		t.Errorf("a refused PATCH changed the product: %+v", p.Edit)
	}
}

// The weekend shop edits its own products with the same edit.
func TestPATCHChangesAProductOfTheWeekendShop(t *testing.T) {
	before, err := tifa.Use(productCtx(t, "GET", "weekend", "postcards"), Load)
	if err != nil || before.NameZH != "手绘明信片" {
		t.Fatalf("Load: %+v, %v", before, err)
	}
	t.Cleanup(func() { _, _ = PATCH(productCtx(t, "PATCH", "weekend", "postcards"), catalogue.ChangeOf(before.Edit)) })
	edit := before.Edit
	edit.Stock, edit.NameZH = 9, " 城市明信片 "
	got, err := PATCH(productCtx(t, "PATCH", "weekend", "postcards"), catalogue.ChangeOf(edit))
	if err != nil || got.Stock != 9 || got.NameZH != "城市明信片" {
		t.Fatalf("PATCH: %+v, %v", got, err)
	}
}

// A PATCH changes only the fields it has: one with the price alone leaves the stock as the
// checkouts left it, one with the stock alone leaves the price, and one with every field changes
// them all. A field that is null keeps its value too.
func TestPATCHChangesOnlyTheFieldsItHas(t *testing.T) {
	for _, project := range []string{"studio", "weekend"} {
		slug := map[string]string{"studio": "glass-carafe", "weekend": "postcards"}[project]
		before, err := tifa.Use(productCtx(t, "GET", project, slug), Load)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = PATCH(productCtx(t, "PATCH", project, slug), catalogue.ChangeOf(before.Edit)) })
		price, stock := before.PriceCents+100, before.Stock+7
		got, err := PATCH(productCtx(t, "PATCH", project, slug), catalogue.Change{PriceCents: &price})
		want := before.Edit
		want.PriceCents = price
		if err != nil || got.Edit != want {
			t.Fatalf("%s, the price alone: %+v, %v, want %+v", project, got.Edit, err, want)
		}
		got, err = PATCH(productCtx(t, "PATCH", project, slug), catalogue.Change{Stock: &stock})
		want.Stock = stock
		if err != nil || got.Edit != want {
			t.Fatalf("%s, the stock alone: %+v, %v, want %+v", project, got.Edit, err, want)
		}
		all := before.Edit
		all.NameEN, all.DescriptionZH = "Changed", "改了。"
		got, err = PATCH(productCtx(t, "PATCH", project, slug), catalogue.ChangeOf(all))
		if err != nil || got.Edit != all {
			t.Fatalf("%s, every field: %+v, %v, want %+v", project, got.Edit, err, all)
		}
		// A body binds as encoding/json/v2 reads it, as Tifa does: null is a field that is not there.
		var c catalogue.Change
		if err := json.Unmarshal([]byte(`{"priceCents":null,"stock":3}`), &c); err != nil || c.PriceCents != nil || c.Stock == nil || *c.Stock != 3 || c.NameEN != nil {
			t.Errorf("a body with a null price binds as %+v, %v", c, err)
		}
		bad := -1
		if err := (catalogue.Change{Stock: &bad}).Validate(); err == nil {
			t.Errorf("%s: a stock below 0 passes", project)
		}
		if err := (catalogue.Change{PriceCents: &price}).Validate(); err != nil {
			t.Errorf("%s: a change of the price alone fails: %v", project, err)
		}
	}
}
