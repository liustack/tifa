package server

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/liustack/tifa"
)

// routesApp has a home page, a page with a parameter, a page whose Load fails with a 404, a page
// with Memo = false and an API route. Each page shows its URL.
func routesApp() App {
	return App{
		Layouts: []Layout{{ID: "app/layout", Path: "/", SSR: true, Load: func(c *tifa.Ctx) (any, error) { return "site", nil }}},
		Routes: []Route{
			{ID: "app/home", Path: "/", Page: true, SSR: true, Layouts: []string{"app/layout"}},
			{ID: "app/items/item+", Path: "/items/{item}", Page: true, SSR: true, Layouts: []string{"app/layout"},
				Load: func(c *tifa.Ctx) (any, error) { return c.Param("item"), nil }},
			{ID: "app/gone", Path: "/gone", Page: true, SSR: true, Layouts: []string{"app/layout"},
				Load: func(c *tifa.Ctx) (any, error) { return nil, tifa.ErrNotFound }},
			{ID: "app/live", Path: "/live", Page: true, SSR: true, NoMemo: true, Layouts: []string{"app/layout"}},
			{ID: "app/api/ping", Path: "/api/ping", GET: func(c *tifa.Ctx) error { return c.Text(200, "pong") }},
		},
	}
}

// The memo counts its hits, misses and shared renders by the route pattern of the page, the one
// c.Pattern() gives. A URL that matches no page counts under "", and the error page of a Load that
// failed under its route. A page with Memo = false, an API route and navigation data do not go
// through the memo and count nowhere. The counts of the whole memo are the sums.
func TestTheMemoCountsByRoute(t *testing.T) {
	s, _ := newFrontServer(t, routesApp())
	for _, target := range []string{"/", "/", "/items/a", "/items/a", "/items/b", "/gone", "/gone", "/nowhere", "/live", "/api/ping", dataURL("/items/a", "b1", "11")} {
		get(s, target)
	}
	want := map[string]tifa.MemoRouteStats{
		"/":             {Hits: 1, Misses: 1},
		"/items/{item}": {Hits: 1, Misses: 2},
		"/gone":         {Misses: 2},
		"":              {Misses: 1},
	}
	got := s.Stats().Memo
	if !reflect.DeepEqual(got.Routes, want) {
		t.Errorf("by route %+v, want %+v", got.Routes, want)
	}
	if got.Hits != 2 || got.Misses != 6 || got.Shared != 0 || got.Entries != 3 {
		t.Errorf("render memo %+v", got)
	}
}

// A page in several languages counts under one route: the pattern has no language.
func TestTheMemoCountsTheLanguagesOfARouteAsOne(t *testing.T) {
	app := App{
		I18n: &I18n{Default: "zh-CN", Languages: []string{"zh-CN", "en-US"}, SiteURL: "https://a.example.com"},
		Routes: []Route{
			{ID: "app/store/items/item+", Path: "/store/items/{item}", Page: true, SSR: true, Languages: storeLanguages,
				Load: func(c *tifa.Ctx) (any, error) { return c.Param("item"), nil }},
		},
	}
	s, _ := newFrontServer(t, app)
	s.setI18n(app.I18n)
	for _, target := range []string{"/store/items/a", "/en-US/store/items/a", "/store/items/a", "/en-US/store/items/a", "/en-US/store/items/b"} {
		if w := get(s, target); w.Code != 200 {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body.String())
		}
	}
	want := map[string]tifa.MemoRouteStats{"/store/items/{item}": {Hits: 2, Misses: 3}}
	if got := s.Stats().Memo.Routes; !reflect.DeepEqual(got, want) {
		t.Errorf("by route %+v, want %+v", got, want)
	}
}

// Requests at the same time lose no count: each request of a route is a hit, a miss or a shared
// render of it, once.
func TestTheMemoCountsByRouteAtTheSameTime(t *testing.T) {
	s, _ := newFrontServer(t, routesApp())
	const workers, each = 8, 50
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range each {
				get(s, fmt.Sprintf("/items/%d", (w+i)%5))
				get(s, "/")
			}
		}()
	}
	wg.Wait()
	got := s.Stats().Memo
	for route, n := range map[string]uint64{"/items/{item}": workers * each, "/": workers * each} {
		r := got.Routes[route]
		if r.Hits+r.Misses+r.Shared != n {
			t.Errorf("%s: %+v, want %d in all", route, r, n)
		}
	}
	if r := got.Routes["/items/{item}"]; r.Misses+r.Shared < 5 || got.Hits+got.Misses+got.Shared != 2*workers*each {
		t.Errorf("render memo %+v", got)
	}
}
