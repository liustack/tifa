package tifatest_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/server"
	"github.com/liustack/tifa/tifatest"
)

// A Ctx from NewCtx has the path of its request, as the visitor wrote it: the language prefix is
// in it, the query is not, and an escape stays an escape.
func TestCtxPathIsThePathOfTheRequest(t *testing.T) {
	for _, tc := range []struct{ target, want string }{
		{"/store/mine", "/store/mine"},
		{"/en-US/store/mine?q=1", "/en-US/store/mine"},
		{"/items/a%20b?next=%2Fx", "/items/a%20b"},
		{"/", "/"},
	} {
		c, _ := tifatest.NewCtx(t, httptest.NewRequest("GET", tc.target, nil))
		if got := c.Path(); got != tc.want {
			t.Errorf("Path() for %s = %q, want %q", tc.target, got, tc.want)
		}
	}
}

// Under Start the server builds the Ctx: the prefix of the language is in Path for a handler and
// for the Load of a page, which has no front end here and answers with its data.
func TestStartPathHasTheLanguagePrefix(t *testing.T) {
	app := server.App{
		I18n: &server.I18n{Default: "zh-CN", Languages: []string{"zh-CN", "en-US"}, SiteURL: "https://a.example.com"},
		Routes: []server.Route{
			{ID: "app/store/route", Path: "/store", Page: true, SSR: true, Layouts: []string{}, Languages: []string{"zh-CN", "en-US"},
				Load: func(c *tifa.Ctx) (any, error) { return c.Path(), nil },
				POST: server.Output(func(c *tifa.Ctx) (string, error) { return c.Path(), nil })},
		},
	}
	srv := tifatest.Start(t, app)
	for _, tc := range []struct{ target, want string }{
		{"/store?q=1", `{"app/store/route":"/store"}`},
		{"/en-US/store?q=1", `{"app/store/route":"/en-US/store"}`},
	} {
		res := get(t, srv, tc.target)
		if body, _ := io.ReadAll(res.Body); res.StatusCode != 200 || string(body) != tc.want {
			t.Errorf("GET %s: %d %s, want %s", tc.target, res.StatusCode, body, tc.want)
		}
	}
	res := postJSON(t, srv, "/en-US/store?x=1", `{}`)
	if body, _ := io.ReadAll(res.Body); res.StatusCode != 200 || strings.TrimSpace(string(body)) != `"/en-US/store"` {
		t.Errorf("POST: %d %s", res.StatusCode, body)
	}
}
