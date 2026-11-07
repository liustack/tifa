package project

import (
	"errors"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
	"github.com/liustack/tifa/tifatest"
)

func listCtx(t *testing.T, project, query string) *tifa.Ctx {
	r := httptest.NewRequest("GET", "/projects/"+project+"?"+query, nil)
	r.SetPathValue("project", project)
	c, _ := tifatest.NewCtx(t, r)
	return c
}

func ids(rows []orders.OrderRow) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func TestLoadReadsTheFiltersAndThePageFromTheQuery(t *testing.T) {
	l, err := Load(listCtx(t, "studio", ""))
	if err != nil || l.Matching != 42 || len(l.Orders) != 25 || l.Filter.Page != 1 || l.Pages != 2 {
		t.Fatalf("no query: %+v, %v", l, err)
	}
	// Shipped orders, page 2: SH-1002 and the older ones are on the page, which the first page of
	// the project does not hold.
	l, err = Load(listCtx(t, "studio", "status=shipped&page=2"))
	if err != nil || l.Matching != 40 || len(l.Orders) != 15 || l.Orders[14].ID != "SH-1001" || l.Filter.Status != "shipped" || l.Filter.Page != 2 {
		t.Fatalf("shipped, page 2: %v, matching %d, rows %v", err, l.Matching, ids(l.Orders))
	}
	// Every filter, a carrier with a space in its name and a search with a customer.
	q := url.Values{"carrier": {"SF Express"}, "period": {"30"}, "amount": {"mid"}, "q": {"林"}}.Encode()
	l, err = Load(listCtx(t, "studio", q))
	p, _ := projects.GetProject("studio")
	zone, _ := time.LoadLocation(p.TimeZone)
	want := orders.ListOrders("studio", zone, orders.ListQuery{Carrier: "SF Express", Period: "30", Amount: "mid", Search: "林"})
	if err != nil || l.Matching != want.Matching || !slices.Equal(ids(l.Orders), ids(want.Orders)) || l.Filter != want.Filter {
		t.Fatalf("%s: %v, %+v", q, err, l.Filter)
	}
	// A page that is not a number is the first, and a filter that matches nothing is no error.
	l, err = Load(listCtx(t, "studio", "page=two&q=nobody+by+this+name"))
	if err != nil || l.Filter.Page != 1 || l.Matching != 0 || len(l.Orders) != 0 || l.Total != 42 {
		t.Fatalf("nothing: %+v, %v", l, err)
	}
	if _, err := Load(listCtx(t, "nowhere", "status=shipped")); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("an unknown project: %v", err)
	}
}
