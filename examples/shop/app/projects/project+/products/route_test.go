package products

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
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
	if err != nil || len(list) != 28 || list[0].Slug != "pour-over-kettle" || list[0].Number != 1 || list[0].NameEN == "" || list[0].Category != "Drinkware" {
		t.Fatalf("Load: %d products, %v", len(list), err)
	}
	// The weekend shop sells no products of the storefront.
	for _, project := range []string{"weekend", "nowhere"} {
		if _, err := tifa.Use(listCtx(t, project), Load); !errors.Is(err, tifa.ErrNotFound) {
			t.Errorf("%s: %v", project, err)
		}
	}
}
