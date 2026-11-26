package server

import (
	"sync"
	"testing"

	"github.com/liustack/tifa"
)

// sight is what a Load saw of its request.
type sight struct{ project, order, orderOfCopy, rawQuery, requestURI, page, tab string }

var sights sync.Map

func lookAt(c *tifa.Ctx) sight {
	r := c.Request()
	return sight{project: c.Param("project"), order: c.Param("order"), orderOfCopy: r.PathValue("order"),
		rawQuery: r.URL.RawQuery, requestURI: r.RequestURI, page: c.Query("page"), tab: c.Query("tab")}
}

func projectLayout(c *tifa.Ctx) (string, error) {
	sights.Store("layout", lookAt(c))
	return "project", nil
}

type orderQuery struct {
	Page int `json:"page"`
}

func orderLoad(c *tifa.Ctx, q orderQuery) (string, error) {
	sights.Store("page", lookAt(c))
	// The Layout may run first here or in the server's own run: it sees the same either way.
	_, err := tifa.Use(c, projectLayout)
	return "order", err
}

// The generated app gives each Load and Layout function as Func, and the server gives each its own
// view of the request: the query parameters it declared, written the way the render key writes
// them, and for a Layout no path parameter of a folder below it.
func TestEachLoadSeesItsOwnPartOfTheRequest(t *testing.T) {
	app := App{
		Layouts: []Layout{
			{ID: "app/layout", Path: "/"},
			{ID: "app/projects/project+/layout", Path: "/projects/{project}",
				Load: func(c *tifa.Ctx) (any, error) { return tifa.Use(c, projectLayout) }, Func: projectLayout},
		},
		Routes: []Route{{
			ID: "app/projects/project+/orders/order+/route", Path: "/projects/{project}/orders/{order}", Page: true, SSR: true,
			Layouts: []string{"app/layout", "app/projects/project+/layout"},
			Load:    LoadInput(orderLoad), Func: orderLoad, Query: []string{"page"}, Whole: []string{"page"},
		}},
	}
	s, _ := newFrontServer(t, app)
	for _, w := range []func() int{
		func() int { return get(s, "/projects/a/orders/7?page=007&tab=x&utm_source=mail").Code },
		func() int {
			return getData(s, "/projects/a/orders/7?page=007&tab=x&utm_source=mail", "b1", "11").Code
		},
	} {
		sights.Clear()
		if code := w(); code != 200 {
			t.Fatalf("answered %d", code)
		}
		layout, _ := sights.Load("layout")
		page, _ := sights.Load("page")
		if want := (sight{project: "a", requestURI: "/projects/a/orders/7"}); layout != want {
			t.Errorf("the Layout saw %+v, want %+v", layout, want)
		}
		if want := (sight{project: "a", order: "7", orderOfCopy: "7", rawQuery: "page=7", requestURI: "/projects/a/orders/7?page=7", page: "7"}); page != want {
			t.Errorf("the page saw %+v, want %+v", page, want)
		}
	}
}
