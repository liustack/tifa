package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/liustack/tifa"
)

// methodsApp has a route with HEAD alone, one with GET and its own HEAD, one with its own OPTIONS,
// and the routes of testApp: a page with POST and an API route with GET and DELETE.
func methodsApp(seen *[]string) App {
	app := testApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		*seen = append(*seen, c.Request().Method+" "+c.Pattern())
		return next(c)
	}
	app.Routes = append(app.Routes,
		Route{ID: "app/api/probe", Path: "/api/probe", SSR: true, Layouts: []string{"app/layout"},
			HEAD: func(c *tifa.Ctx) error { c.SetHeader("X-Probe", "head"); return c.NoContent(204) }},
		Route{ID: "app/api/file", Path: "/api/file", SSR: true, Layouts: []string{"app/layout"},
			GET:  func(c *tifa.Ctx) error { return c.Text(200, "body") },
			HEAD: func(c *tifa.Ctx) error { c.SetHeader("X-Probe", "own head"); return c.NoContent(200) }},
		Route{ID: "app/api/dav", Path: "/api/dav", SSR: true, Layouts: []string{"app/layout"},
			GET: func(c *tifa.Ctx) error { return c.Text(200, "dav") },
			OPTIONS: func(c *tifa.Ctx) error {
				c.SetHeader("DAV", "1")
				c.SetHeader("Allow", "GET, OPTIONS")
				return c.NoContent(200)
			}},
		Route{ID: "app/api/debug", Path: "/api/debug", SSR: true, Layouts: []string{"app/layout"},
			TRACE:   func(c *tifa.Ctx) error { return c.Text(200, c.Request().Method+" "+c.Request().URL.Path) },
			CONNECT: func(c *tifa.Ctx) error { return c.Text(200, "tunnel "+c.Request().URL.Path) }},
	)
	return app
}

// A route answers HEAD with its own HEAD when it has one, and with GET without the body when it
// has none. A route with HEAD alone answers GET with 405.
func TestHeadOfARoute(t *testing.T) {
	var seen []string
	s := newAppServer(t, methodsApp(&seen))
	if w := send(s, "HEAD", "/api/probe", ""); w.Code != 204 || w.Header().Get("X-Probe") != "head" {
		t.Errorf("HEAD /api/probe: %d, X-Probe %q", w.Code, w.Header().Get("X-Probe"))
	}
	if w := send(s, "GET", "/api/probe", ""); w.Code != 405 || w.Header().Get("Allow") != "HEAD, OPTIONS" {
		t.Errorf("GET /api/probe: %d, Allow %q", w.Code, w.Header().Get("Allow"))
	}
	if w := send(s, "HEAD", "/api/file", ""); w.Code != 200 || w.Header().Get("X-Probe") != "own head" {
		t.Errorf("HEAD /api/file: %d, X-Probe %q", w.Code, w.Header().Get("X-Probe"))
	}
	if w := send(s, "GET", "/api/file", ""); w.Code != 200 || w.Body.String() != "body" {
		t.Errorf("GET /api/file: %d %q", w.Code, w.Body.String())
	}
	if w := send(s, "HEAD", "/api/health", ""); w.Code != 200 || w.Header().Get("X-Probe") != "" {
		t.Errorf("HEAD /api/health, answered by GET: %d", w.Code)
	}
}

// Without an OPTIONS of its own, a route answers OPTIONS with 204 and Allow, after OnRequest. Every
// path a route has is such a resource: a page, an API route, one with no handlers.
func TestOptionsOfARoute(t *testing.T) {
	var seen []string
	s := newAppServer(t, methodsApp(&seen))
	for _, tc := range []struct{ path, allow string }{
		{"/api/health", "GET, HEAD, DELETE, OPTIONS"},
		{"/api/probe", "HEAD, OPTIONS"},
		{"/projects/p1", "GET, HEAD, POST, OPTIONS"},
		{"/", "GET, HEAD, OPTIONS"},
		{"/api/nothing", "OPTIONS"},
	} {
		w := send(s, "OPTIONS", tc.path, "")
		if w.Code != 204 || w.Header().Get("Allow") != tc.allow || w.Body.Len() != 0 {
			t.Errorf("OPTIONS %s: %d, Allow %q, body %q, want 204 and %q", tc.path, w.Code, w.Header().Get("Allow"), w.Body.String(), tc.allow)
		}
	}
	if want := "OPTIONS /api/health"; len(seen) == 0 || seen[0] != want {
		t.Errorf("OnRequest saw %v, want %q first", seen, want)
	}
	w := send(s, "OPTIONS", "/api/dav", "")
	if w.Code != 200 || w.Header().Get("DAV") != "1" || w.Header().Get("Allow") != "GET, OPTIONS" {
		t.Errorf("own OPTIONS: %d, DAV %q, Allow %q", w.Code, w.Header().Get("DAV"), w.Header().Get("Allow"))
	}
	if w := send(s, "PUT", "/api/dav", ""); w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD, OPTIONS" {
		t.Errorf("PUT /api/dav: %d, Allow %q", w.Code, w.Header().Get("Allow"))
	}
	if w := send(s, "OPTIONS", "/nowhere", ""); w.Code != 404 {
		t.Errorf("OPTIONS of an unmatched URL: %d", w.Code)
	}
}

// The preflight of docs/guides/cors.md: OnRequest answers OPTIONS before the route, so its answer
// is the one the browser gets, for a route with an OPTIONS of its own too.
func TestOnRequestAnswersThePreflight(t *testing.T) {
	var routed atomic.Int32
	app := methodsApp(new([]string))
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		r := c.Request()
		if r.Header.Get("Origin") != "https://app.example.com" {
			return next(c)
		}
		c.SetHeader("Access-Control-Allow-Origin", "https://app.example.com")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			c.SetHeader("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
			c.SetHeader("Access-Control-Allow-Headers", "Content-Type, Authorization")
			c.SetHeader("Access-Control-Max-Age", "600")
			return c.NoContent(http.StatusNoContent)
		}
		routed.Add(1)
		return next(c)
	}
	s := newAppServer(t, app)
	for _, path := range []string{"/api/health", "/api/dav"} {
		w := crossSiteRequest(s, "OPTIONS", path, "", map[string]string{
			"Origin": "https://app.example.com", "Sec-Fetch-Site": "cross-site", "Access-Control-Request-Method": "DELETE",
		})
		if w.Code != 204 || w.Header().Get("Access-Control-Allow-Methods") != "GET, POST, PUT, PATCH, DELETE" || w.Header().Get("DAV") != "" {
			t.Errorf("preflight of %s: %d %v", path, w.Code, w.Header())
		}
	}
	if routed.Load() != 0 {
		t.Errorf("the preflight reached the route %d times", routed.Load())
	}
}

// HEAD and OPTIONS are safe methods: a browser on another site may send them, and they reach the
// route.
func TestCrossSiteHeadAndOptionsReachTheRoute(t *testing.T) {
	s := newAppServer(t, methodsApp(new([]string)))
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"HEAD", "/api/probe", 204},
		{"OPTIONS", "/api/dav", 200},
		{"OPTIONS", "/api/health", 204},
	} {
		w := crossSiteRequest(s, tc.method, tc.path, "", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.test"})
		if w.Code != tc.status {
			t.Errorf("cross-site %s %s: %d, want %d\n%s", tc.method, tc.path, w.Code, tc.status, strings.TrimSpace(w.Body.String()))
		}
	}
}

// CONNECT and TRACE reach the handlers of a route like the other methods, and are in its Allow. A
// CONNECT for a host and port, as a client asks a proxy, has no path, so it matches no route.
func TestConnectAndTraceOfARoute(t *testing.T) {
	var seen []string
	s := newAppServer(t, methodsApp(&seen))
	if w := send(s, "TRACE", "/api/debug", ""); w.Code != 200 || w.Body.String() != "TRACE /api/debug" {
		t.Errorf("TRACE /api/debug: %d %q", w.Code, w.Body.String())
	}
	if w := send(s, "CONNECT", "/api/debug", ""); w.Code != 200 || w.Body.String() != "tunnel /api/debug" {
		t.Errorf("CONNECT /api/debug: %d %q", w.Code, w.Body.String())
	}
	if w := send(s, "OPTIONS", "/api/debug", ""); w.Header().Get("Allow") != "OPTIONS, CONNECT, TRACE" {
		t.Errorf("OPTIONS /api/debug: Allow %q", w.Header().Get("Allow"))
	}
	if w := send(s, "TRACE", "/api/health", ""); w.Code != 405 {
		t.Errorf("TRACE of a route without it: %d", w.Code)
	}
	r := httptest.NewRequest("CONNECT", "shop.example:443", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Errorf("CONNECT shop.example:443: %d, want 404", w.Code)
	}
}
