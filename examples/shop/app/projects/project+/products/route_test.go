package products

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store/products"
	"github.com/liustack/tifa/tifatest"
)

func listCtx(t *testing.T, project string) *tifa.Ctx {
	r := httptest.NewRequest("GET", "/projects/"+project+"/products", nil)
	r.SetPathValue("project", project)
	c, _ := tifatest.NewCtx(t, r)
	return c
}

func TestTheStudioShopListsTheProductsOfTheStorefront(t *testing.T) {
	list, err := tifa.Use(listCtx(t, "studio"), Load)
	if err != nil || len(list) != len(products.Entries()) || len(list) < 2 || list[0].Slug != "pour-over-kettle" || list[0].Number != 1 || list[0].NameEN == "" || list[0].Category != "Drinkware" {
		t.Fatalf("Load: %d products, %v", len(list), err)
	}
	// The weekend shop lists its own products.
	if list, err := tifa.Use(listCtx(t, "weekend"), Load); err != nil || len(list) != 5 || list[0].Slug != "canvas-tote" || list[0].NameEN == "" {
		t.Errorf("weekend: %+v, %v", list, err)
	}
	for _, project := range []string{"nowhere"} {
		if _, err := tifa.Use(listCtx(t, project), Load); !errors.Is(err, tifa.ErrNotFound) {
			t.Errorf("%s: %v", project, err)
		}
	}
}
