package order

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
	"github.com/liustack/tifa/tifatest"
)

func orderCtx(t *testing.T, method, project, order string) *tifa.Ctx {
	r := httptest.NewRequest(method, "/projects/"+project+"/orders/"+order, nil)
	r.SetPathValue("project", project)
	r.SetPathValue("order", order)
	c, _ := tifatest.NewCtx(t, r)
	return c
}

func TestLoadFindsTheOrderInItsProject(t *testing.T) {
	o, err := tifa.Use(orderCtx(t, "GET", "studio", "SH-1041"), Load)
	if err != nil || o.ID != "SH-1041" {
		t.Fatalf("Load: %+v, %v", o, err)
	}
	if _, err := tifa.Use(orderCtx(t, "GET", "weekend", "SH-1041"), Load); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("order of another project: %v", err)
	}
	if _, err := tifa.Use(orderCtx(t, "GET", "nowhere", "SH-1041"), Load); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("order of an unknown project: %v", err)
	}
}

// Shipping itself is tested in the orders package, which resets its demo data. Shipping here would
// change the data that the next run of this test reads, so POST is checked on an order already
// shipped.
func TestPOSTShipsTheOrderOfItsProject(t *testing.T) {
	if _, err := POST(orderCtx(t, "POST", "studio", "SH-1040")); !errors.Is(err, orders.ErrShipped) {
		t.Fatalf("POST of a shipped order: %v", err)
	}
	if _, err := POST(orderCtx(t, "POST", "weekend", "SH-1040")); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("POST of an order of another project: %v", err)
	}
}
