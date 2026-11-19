package server

import (
	"strings"
	"testing"

	"github.com/liustack/tifa"
)

func pathApp() App {
	app := frontApp()
	app.I18n = &I18n{Default: "zh-CN", Languages: []string{"zh-CN", "en-US"}}
	app.SiteURL = "https://a.example.com"
	app.Routes = append(app.Routes,
		Route{ID: "app/store/items/item+", Path: "/store/items/{id}", Page: true, SSR: true, Languages: storeLanguages, Layouts: []string{"app/layout"},
			Query: []string{"tab"},
			Load:  func(c *tifa.Ctx) (any, error) { return c.Path() + "|" + c.Query("tab"), nil },
			POST: Output(func(c *tifa.Ctx) (string, error) {
				return c.Path(), nil
			})},
		Route{ID: "app/plain/route", Path: "/plain", Page: true, SSR: true, Layouts: []string{"app/layout"},
			Load: func(c *tifa.Ctx) (any, error) { return c.Path(), nil }},
	)
	return app
}

// c.Path is the path of the page as the visitor sees it in the address bar: the language prefix
// included, escaped as the browser has it, and no query. A page load and the data request of a
// client navigation have the same one, and so has the query the Load reads. A handler gets the
// path of the handler.
func TestPathIsThePageTheVisitorAsked(t *testing.T) {
	app := pathApp()
	s, _ := newFrontServer(t, app)
	s.setI18n(app.I18n)
	for _, tc := range []struct{ target, want string }{
		{"/en-US/store/items/a%20b?tab=x&utm=1", "/en-US/store/items/a%20b|x"},
		{"/store/items/7?tab=y", "/store/items/7|y"},
		{"/plain", "/plain"},
	} {
		data := getData(s, tc.target, "b1", "01")
		if got := routesOf(t, data)[pageID(tc.target)].Data; got != tc.want {
			t.Errorf("data request %s: Load saw %q, want %q", tc.target, got, tc.want)
		}
		page := get(s, tc.target)
		if page.Code != 200 || !strings.Contains(page.Body.String(), `"`+tc.want+`"`) {
			t.Errorf("page %s: %d %s, want the Load to see %q", tc.target, page.Code, page.Body.String(), tc.want)
		}
	}
	w := send(s, "POST", "/en-US/store/items/a%20b?x=1", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `"/en-US/store/items/a%20b"` {
		t.Errorf("handler: %d %s", w.Code, w.Body.String())
	}
}

func pageID(target string) string {
	if strings.Contains(target, "/store/") {
		return "app/store/items/item+"
	}
	return "app/plain/route"
}
