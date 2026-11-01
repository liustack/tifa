package server

import (
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
)

const defaultCache = "private, no-cache"

func cacheControl(w *httptest.ResponseRecorder) string { return w.Header().Get("Cache-Control") }

// Every dynamic answer says private, no-cache unless the application chose another value, so a
// CDN that caches answers without a Cache-Control never keeps a page meant for one visitor.
func TestPagesAnswerPrivateNoCache(t *testing.T) {
	s, r := newFrontServer(t, frontApp())
	data := []string{"X-Tifa-Route", "app/layout", "X-Tifa-Build", "b1"}
	page := get(s, "/projects/p1")
	for _, tc := range []struct {
		name string
		w    *httptest.ResponseRecorder
		code int
	}{
		{"page", page, 200},
		{"page with SSR off", get(s, "/login"), 200},
		{"page of the HTML cache, first", get(s, "/stats"), 200},
		{"page of the HTML cache, again", get(s, "/stats"), 200},
		{"page conditional", get(s, "/projects/p1", "If-None-Match", page.Header().Get("ETag")), 304},
		{"data request", get(s, "/projects/p1", data...), 200},
		{"data request of another build", get(s, "/projects/p1", "X-Tifa-Route", "app/layout", "X-Tifa-Build", "old"), 409},
		{"404 page", get(s, "/nowhere"), 404},
		{"405", send(s, "DELETE", "/projects/p1", ""), 405},
	} {
		if tc.w.Code != tc.code || cacheControl(tc.w) != defaultCache {
			t.Errorf("%s: %d, Cache-Control %q", tc.name, tc.w.Code, cacheControl(tc.w))
		}
	}
	r.result = func(ssr.Input) (string, error) { return "", &ssr.RedirectError{Location: "/projects/a", Status: 308} }
	if w := get(s, "/projects/%61"); w.Code != 308 || cacheControl(w) != defaultCache {
		t.Errorf("router redirect: %d, Cache-Control %q", w.Code, cacheControl(w))
	}
}

func TestHandlersAnswerPrivateNoCache(t *testing.T) {
	for _, compress := range []bool{false, true} {
		cfg := config.Default()
		cfg.Server.Compress = compress
		s := New(cfg, Hooks{})
		s.Handle("GET", "/json", func(c *tifa.Ctx) error { return c.JSON(200, map[string]int{"a": 1}) })
		s.Handle("GET", "/text", func(c *tifa.Ctx) error { return c.Text(200, "ok") })
		s.Handle("POST", "/none", func(c *tifa.Ctx) error { return c.NoContent(204) })
		s.Handle("GET", "/redirect", func(c *tifa.Ctx) error { return c.Redirect("/json", 302) })
		s.Handle("GET", "/own", func(c *tifa.Ctx) error {
			_, err := c.Writer().Write([]byte("written by hand"))
			return err
		})
		s.Handle("GET", "/own-status", func(c *tifa.Ctx) error {
			c.Writer().WriteHeader(201)
			return nil
		})
		s.Handle("GET", "/fail", func(c *tifa.Ctx) error { return tifa.Error{Status: 409, Message: "taken"} })
		s.Handle("GET", "/missing", func(c *tifa.Ctx) error { return tifa.ErrNotFound })
		for _, tc := range []struct {
			method, target string
			code           int
		}{
			{"GET", "/json", 200}, {"GET", "/text", 200}, {"POST", "/none", 204}, {"GET", "/redirect", 302},
			{"GET", "/own", 200}, {"GET", "/own-status", 201}, {"GET", "/fail", 409}, {"GET", "/missing", 404},
			{"GET", "/nowhere", 404}, {"POST", "/json", 405}, {"GET", "/a//json", 308},
		} {
			w := send(s, tc.method, tc.target, "")
			if w.Code != tc.code || cacheControl(w) != defaultCache {
				t.Errorf("compress %v, %s %s: %d, Cache-Control %q", compress, tc.method, tc.target, w.Code, cacheControl(w))
			}
		}
	}
}

// The default only fills a gap. A value the application set in OnRequest, a Load or a method
// handler stays, and so does one set before a handler writes by hand.
func TestApplicationCacheControlIsNotReplaced(t *testing.T) {
	app := frontApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx) error {
		if c.Query("hook") != "" {
			c.SetHeader("Cache-Control", "public, max-age=60")
		}
		return c.Next()
	}
	app.Routes = append(app.Routes, Route{ID: "app/loaded", Path: "/loaded", Page: true, SSR: true, Layouts: []string{"app/layout"},
		Load: func(c *tifa.Ctx) (any, error) {
			c.SetHeader("Cache-Control", "public, max-age=30")
			return 1, nil
		}})
	app.Routes = append(app.Routes,
		Route{ID: "app/handled", Path: "/handled", POST: func(c *tifa.Ctx) error {
			c.SetHeader("Cache-Control", "no-store")
			return c.JSON(200, 1)
		}},
		Route{ID: "app/own", Path: "/own", GET: func(c *tifa.Ctx) error {
			c.Writer().Header().Set("Cache-Control", "max-age=5")
			_, err := c.Writer().Write([]byte("by hand"))
			return err
		}},
	)
	s, _ := newFrontServer(t, app)
	data := []string{"X-Tifa-Route", "app/layout", "X-Tifa-Build", "b1"}
	for _, tc := range []struct {
		name string
		w    *httptest.ResponseRecorder
		want string
	}{
		{"OnRequest, page", get(s, "/projects/p1?hook=1"), "public, max-age=60"},
		{"OnRequest, data request", get(s, "/projects/p1?hook=1", data...), "public, max-age=60"},
		{"OnRequest, 404 page", get(s, "/nowhere?hook=1"), "public, max-age=60"},
		{"Load, page", get(s, "/loaded"), "public, max-age=30"},
		{"Load, data request", get(s, "/loaded", "X-Tifa-Route", "app/loaded", "X-Tifa-Build", "b1"), "public, max-age=30"},
		{"method handler", send(s, "POST", "/handled", "{}"), "no-store"},
		{"handler writing by hand", get(s, "/own"), "max-age=5"},
	} {
		if got := cacheControl(tc.w); got != tc.want {
			t.Errorf("%s: Cache-Control %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Files the server serves itself are not dynamic answers. The bundle keeps its long cache and
// public files keep their plain no-cache. The error page and the busy answer keep no-store, see
// TestFailedRenderShowsTheErrorAtTheFirstLevelThatRenders and TestBusyRenderAnswers503.
func TestDefaultCacheControlLeavesFilesAlone(t *testing.T) {
	s, _ := newFrontServer(t, frontApp())
	for name, want := range map[string]string{
		"/assets/app-1.js": "public, max-age=31536000, immutable",
		"/robots.txt":      "no-cache",
		"/assets/logo.svg": "no-cache",
	} {
		if got := cacheControl(get(s, name)); got != want {
			t.Errorf("%s: Cache-Control %q, want %q", name, got, want)
		}
	}
}
