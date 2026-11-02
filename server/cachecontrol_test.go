package server

import (
	"net/http/httptest"
	"strings"
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
		{"data request", getData(s, "/projects/p1", "b1", "100"), 200},
		{"404 page", get(s, "/nowhere"), 404},
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
		s.Handle("GET", "/redirect", func(c *tifa.Ctx) error { return tifa.Redirect("/json", 302) })
		s.Handle("GET", "/own", func(c *tifa.Ctx) error {
			_, err := c.Writer().Write([]byte("written by hand"))
			return err
		})
		s.Handle("GET", "/own-status", func(c *tifa.Ctx) error {
			c.Writer().WriteHeader(201)
			return nil
		})
		for _, tc := range []struct {
			method, target string
			code           int
		}{
			{"GET", "/json", 200}, {"GET", "/text", 200}, {"POST", "/none", 204}, {"GET", "/redirect", 302},
			{"GET", "/own", 200}, {"GET", "/own-status", 201}, {"GET", "/a//json", 308},
		} {
			w := send(s, tc.method, tc.target, "")
			if w.Code != tc.code || cacheControl(w) != defaultCache {
				t.Errorf("compress %v, %s %s: %d, Cache-Control %q", compress, tc.method, tc.target, w.Code, cacheControl(w))
			}
		}
	}
}

// JSON errors are not in the two tests above: they say no-store, see TestErrorAnswersSayNoStore.

// The default only fills a gap. A value the application set in OnRequest, a Load or a method
// handler stays, and so does one set before a handler writes by hand.
func TestApplicationCacheControlIsNotReplaced(t *testing.T) {
	app := frontApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		switch c.Query("hook") {
		case "":
		case "2":
			c.SetHeader("Cache-Control", "no-store")
		default:
			c.SetHeader("Cache-Control", "public, max-age=60")
		}
		return next(c)
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
	for _, tc := range []struct {
		name string
		w    *httptest.ResponseRecorder
		want string
	}{
		{"OnRequest, page", get(s, "/projects/p1?hook=1"), "public, max-age=60"},
		// A navigation answer is never public, whatever sets it and however it ends. no-store stays.
		{"OnRequest, data request", getData(s, "/projects/p1?hook=1", "b1", "100"), "private, no-cache"},
		{"OnRequest, data request, no-store", getData(s, "/projects/p1?hook=2", "b1", "100"), "no-store"},
		{"OnRequest, data request of an unknown URL", getData(s, "/nowhere?hook=1", "b1", "1"), "private, no-cache"},
		{"OnRequest, data request of another build", getData(s, "/projects/p1?hook=1", "old", "100"), "private, no-cache"},
		{"OnRequest, data request with wrong digits", getData(s, "/projects/p1?hook=1", "b1", "1"), "private, no-cache"},
		{"OnRequest, data request redirected by ServeMux", getData(s, "/./projects/p1?hook=1", "b1", "100"), "private, no-cache"},
		{"OnRequest, 404 page", get(s, "/nowhere?hook=1"), "public, max-age=60"},
		{"Load, page", get(s, "/loaded"), "public, max-age=30"},
		{"Load, data request", getData(s, "/loaded", "b1", "01"), "private, no-cache"},
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

// A failed answer is not meant to be kept by anyone: JSON errors, the plain page of Go and the 503
// of a server that is shutting down say no-store. A Cache-Control the application set before the
// error stays.
func TestErrorAnswersSayNoStore(t *testing.T) {
	for _, set := range []string{"", "public, max-age=60"} {
		want := "no-store"
		if set != "" {
			want = set
		}
		app := frontApp()
		app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
			if set != "" {
				c.SetHeader("Cache-Control", set)
			}
			return next(c)
		}
		app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error { return tifa.Error{Status: 500, Message: "oops"} }
		app.Routes = append(app.Routes,
			Route{ID: "app/api", Path: "/api", GET: func(c *tifa.Ctx) error { return tifa.Error{Status: 409, Message: "taken"} }, POST: func(c *tifa.Ctx) error {
				return tifa.Fields{"name": "required"}
			}, PUT: func(c *tifa.Ctx) error { return tifa.ErrNotFound }, PATCH: func(c *tifa.Ctx) error { return errDatabase }},
		)
		s, _ := newFrontServer(t, app)
		for _, tc := range []struct {
			name string
			w    *httptest.ResponseRecorder
			code int
		}{
			{"409", send(s, "GET", "/api", ""), 409},
			{"422", send(s, "POST", "/api", "{}"), 422},
			{"404", send(s, "PUT", "/api", "{}"), 404},
			{"500", send(s, "PATCH", "/api", "{}"), 500},
			{"405", send(s, "DELETE", "/api", ""), 405},
		} {
			if tc.w.Code != tc.code || cacheControl(tc.w) != want {
				t.Errorf("set %q, %s: %d, Cache-Control %q, want %q", set, tc.name, tc.code, cacheControl(tc.w), want)
			}
		}
		// A navigation answer is never public, so a public value of the application gives way.
		dataWant := want
		if set != "" {
			dataWant = "private, no-cache"
		}
		if w := getData(s, "/projects/p1", "old", "100"); w.Code != 409 || cacheControl(w) != dataWant {
			t.Errorf("set %q, data request of another build: %d, Cache-Control %q, want %q", set, w.Code, cacheControl(w), dataWant)
		}
		w := httptest.NewRecorder()
		if set != "" {
			w.Header().Set("Cache-Control", set)
		}
		writeFallback(w, tifa.Error{Status: 500, Message: "down"})
		if cacheControl(w) != want {
			t.Errorf("set %q, plain page: Cache-Control %q, want %q", set, cacheControl(w), want)
		}
		// The 503 serveBoth writes by itself while the server shuts down.
		w = httptest.NewRecorder()
		writeJSON(w, 503, map[string]string{"error": "Service Unavailable"})
		if cacheControl(w) != "no-store" {
			t.Errorf("shutdown 503: Cache-Control %q", cacheControl(w))
		}
	}
}

// The plain page a Go-only failure ends in, such as a failing OnRequest on a page.
func TestPlainErrorPageSaysNoStore(t *testing.T) {
	app := frontApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		return tifa.Error{Status: 429, Message: "slow down"}
	}
	s, _ := newFrontServer(t, app)
	w := get(s, "/projects/p1")
	if w.Code != 429 || !strings.Contains(w.Body.String(), "<h1>429") || cacheControl(w) != "no-store" {
		t.Errorf("%d, Cache-Control %q, %s", w.Code, cacheControl(w), w.Body.String())
	}
}
