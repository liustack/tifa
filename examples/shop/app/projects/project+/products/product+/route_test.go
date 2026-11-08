package product

import (
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
	for _, tc := range [][2]string{{"studio", "no-such-product"}, {"weekend", "oak-tray"}, {"nowhere", "oak-tray"}} {
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
	got, err := PATCH(productCtx(t, "PATCH", "studio", "glass-carafe"), edit)
	if err != nil || got.NameEN != "Tall Carafe" || got.PriceCents != 16800 || got.Stock != 40 {
		t.Fatalf("PATCH: %+v, %v", got, err)
	}
	en, _ := storekit.FindLocale("en-US")
	if p, _ := catalogue.GetProduct("glass-carafe", en); p.Name != "Tall Carafe" || p.Stock != 40 {
		t.Errorf("the storefront shows %q with %d in stock", p.Name, p.Stock)
	}
	for _, tc := range [][2]string{{"studio", "no-such-product"}, {"weekend", "glass-carafe"}} {
		if _, err := PATCH(productCtx(t, "PATCH", tc[0], tc[1]), edit); !errors.Is(err, tifa.ErrNotFound) {
			t.Errorf("%s in %s: %v", tc[1], tc[0], err)
		}
	}
	if p, _ := catalogue.GetEntry("glass-carafe"); p.Edit != edit {
		t.Errorf("a refused PATCH changed the product: %+v", p.Edit)
	}
}
