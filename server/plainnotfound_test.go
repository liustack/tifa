package server

import (
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/liustack/tifa"
)

func TestNamesFile(t *testing.T) {
	for p, want := range map[string]bool{
		"/wp-login.php": true, "/.env": true, "/a/b.tar.gz": true, "/x.A1b2C3d4": true, "/favicon.ico": true,
		"/.git/config": false, "/a.123456789": false, "/a.": false, "/a./": false, "/users": false,
		"/a.b/c": false, "/": false, "/a.p-p": false, "/a.é": false,
	} {
		if got := namesFile(p); got != want {
			t.Errorf("namesFile(%q) = %v, want %v", p, got, want)
		}
	}
}

// A URL that matches no route and names a file gets the plain 404 of a missing file under
// /assets/, with no Load and no render. Whatever a route matches is untouched, a catch-all route
// and a parameter that holds a dot included, and a URL without an extension still gets the 404 page.
func TestUnmatchedURLThatNamesAFileGetsAPlainNotFound(t *testing.T) {
	const (
		plain = "plain" // the plain 404
		page  = "page"  // the 404 page of the app
		route = "route" // a route answers
	)
	for _, tc := range []struct {
		name      string
		languages bool
		catchAll  bool
		cases     []struct{ method, url, want string }
	}{
		{"no languages", false, false, []struct{ method, url, want string }{
			{"GET", "/wp-login.php", plain}, {"HEAD", "/wp-login.php", plain}, {"GET", "/.env", plain},
			{"GET", "/a/b/c.html", plain}, {"GET", "/assets/gone-1.js", plain},
			{"GET", "/nowhere", page}, {"GET", "/.git/config", page}, {"GET", "/a/b/", page}, {"GET", "/x.toolongext", page},
			{"GET", "/users/john.doe", route}, {"GET", "/", route},
			{"POST", "/other.php", "json"},
		}},
		{"no languages, catch-all", false, true, []struct{ method, url, want string }{
			{"GET", "/docs/guide.php", route}, {"GET", "/wp-login.php", plain}, {"GET", "/nowhere", page},
		}},
		{"languages", true, false, []struct{ method, url, want string }{
			{"GET", "/wp-login.php", plain}, {"GET", "/zh-CN/missing.php", plain}, {"GET", "/en-US/.env", plain},
			{"GET", "/zh-CN/nowhere", page}, {"GET", "/nowhere", page},
			{"GET", "/en-US/users/john.doe", route}, {"GET", "/users/john.doe", route},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var loads atomic.Int32
			app := frontApp()
			var langs []string
			if tc.languages {
				app.I18n = &I18n{Default: "zh-CN", Languages: []string{"zh-CN", "en-US"}}
				langs = []string{"zh-CN", "en-US"}
			}
			app.Layouts = []Layout{{ID: "app/layout", Path: "/", SSR: true, Languages: langs,
				Load: func(c *tifa.Ctx) (any, error) { loads.Add(1); return 1, nil }}}
			app.Routes = []Route{
				{ID: "app/home", Path: "/", Page: true, SSR: true, Layouts: []string{"app/layout"}, Languages: langs},
				{ID: "app/users/name+", Path: "/users/{name}", Page: true, SSR: true, Layouts: []string{"app/layout"}, Languages: langs},
			}
			if tc.catchAll {
				app.Routes = append(app.Routes, Route{ID: "app/docs/rest++", Path: "/docs/{rest...}", Page: true, SSR: true, Layouts: []string{"app/layout"}})
			}
			var nexts []error
			app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
				err := next(c)
				nexts = append(nexts, err)
				return err
			}
			var reported atomic.Int32
			app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error { reported.Add(1); return tifa.Error{Status: 500} }
			s, r := newFrontServer(t, app)
			if tc.languages {
				s.setI18n(app.I18n)
			}
			for _, c := range tc.cases {
				loads.Store(0)
				renders, nexts0 := r.count(), len(nexts)
				w := httptest.NewRecorder()
				s.ServeHTTP(w, httptest.NewRequest(c.method, c.url, nil))
				ctx := c.method + " " + c.url
				switch c.want {
				case plain:
					if w.Code != 404 || w.Body.String() != "404 page not found\n" ||
						!strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") || w.Header().Get("Cache-Control") != "no-store" {
						t.Errorf("%s: %d %q %v, want the plain 404", ctx, w.Code, w.Body.String(), w.Header())
					}
					if loads.Load() != 0 || r.count() != renders {
						t.Errorf("%s: %d Loads and %d renders, want none", ctx, loads.Load(), r.count()-renders)
					}
					if len(nexts) > nexts0 && !errors.Is(nexts[len(nexts)-1], tifa.ErrNotFound) {
						t.Errorf("%s: OnRequest saw %v from Next", ctx, nexts[len(nexts)-1])
					}
				case page:
					if w.Code != 404 || loads.Load() != 1 || r.count() != renders+1 {
						t.Errorf("%s: %d, %d Loads and %d renders, want the 404 page", ctx, w.Code, loads.Load(), r.count()-renders)
					}
				case route:
					if w.Code != 200 {
						t.Errorf("%s: %d, want the route", ctx, w.Code)
					}
				case "json":
					if w.Code != 404 || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
						t.Errorf("%s: %d %q", ctx, w.Code, w.Body.String())
					}
				}
			}
			if reported.Load() != 0 {
				t.Errorf("OnError heard %d times", reported.Load())
			}
		})
	}
}
