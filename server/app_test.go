package server

import (
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
)

type site struct {
	Name string `json:"name"`
}

type project struct {
	ID   string `json:"id"`
	Site string `json:"site"`
}

// The Load functions below stand for package-level Load functions of an application. The
// registration wraps them the way generated code does, through tifa.Use.
var siteLoads atomic.Int32

func loadSite(c *tifa.Ctx) (site, error) {
	siteLoads.Add(1)
	if c.Query("site") == "missing" {
		return site{}, tifa.ErrNotFound
	}
	return site{Name: "shop"}, nil
}

func loadProject(c *tifa.Ctx) (project, error) {
	s, err := tifa.Use(c, loadSite)
	if err != nil {
		return project{}, err
	}
	if c.Param("projectId") == "broken" {
		return project{}, errDatabase
	}
	return project{ID: c.Param("projectId"), Site: s.Name}, nil
}

func loadOrders(c *tifa.Ctx) ([]string, error) {
	p, err := tifa.Use(c, loadProject)
	if err != nil {
		return nil, err
	}
	return []string{p.ID + "-1", p.ID + "-2"}, nil
}

func testApp() App {
	return App{
		Layouts: []Layout{
			{ID: "app/layout", SSR: true, Load: func(c *tifa.Ctx) (any, error) { return tifa.Use(c, loadSite) }},
			{ID: "app/projects", SSR: true, Load: func(c *tifa.Ctx) (any, error) { return tifa.Use(c, loadProject) }},
			{ID: "app/plain", SSR: true},
		},
		Routes: []Route{
			{ID: "app/home", Path: "/", Page: true, SSR: true, Layouts: []string{"app/layout"}},
			{
				ID: "app/projects/orders", Path: "/projects/{projectId}", Page: true, SSR: true, Layouts: []string{"app/layout", "app/projects"},
				Load: func(c *tifa.Ctx) (any, error) { return tifa.Use(c, loadOrders) },
				POST: func(c *tifa.Ctx) error { return c.JSON(201, map[string]string{"created": c.Param("projectId")}) },
			},
			{
				ID: "app/api/health", Path: "/api/health", SSR: true, Layouts: []string{"app/layout"},
				GET:    func(c *tifa.Ctx) error { return c.Text(200, "ok") },
				DELETE: func(c *tifa.Ctx) error { return tifa.Error{Status: 409, Message: "busy"} },
			},
			{ID: "app/api/nothing", Path: "/api/nothing", SSR: true, Layouts: []string{"app/layout"}},
		},
	}
}

func newAppServer(t *testing.T, app App) *Server {
	t.Helper()
	s := New(config.Default(), app.Hooks)
	s.register(app.Layouts, app.Routes)
	return s
}

func TestRegisterServesInterfaceRoutes(t *testing.T) {
	s := newAppServer(t, testApp())
	if w := send(s, "GET", "/api/health", ""); w.Code != 200 || w.Body.String() != "ok" {
		t.Errorf("GET /api/health: %d %s", w.Code, w.Body.String())
	}
	if w := send(s, "DELETE", "/api/health", ""); w.Code != 409 || !strings.Contains(w.Body.String(), "busy") {
		t.Errorf("DELETE /api/health: %d %s", w.Code, w.Body.String())
	}
	if w := send(s, "PUT", "/api/health", ""); w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD, DELETE" {
		t.Errorf("PUT /api/health: %d, Allow %q", w.Code, w.Header().Get("Allow"))
	}
	if w := send(s, "POST", "/projects/p1", ""); w.Code != 201 || !strings.Contains(w.Body.String(), `"created":"p1"`) {
		t.Errorf("POST on a page: %d %s", w.Code, w.Body.String())
	}
	if w := send(s, "GET", "/nowhere", ""); w.Code != 404 {
		t.Errorf("unmatched URL: %d", w.Code)
	}
}

// A route without route.tsx and without method handlers still owns its Path: OnRequest sees the pattern and
// every method is answered 405.
func TestRegisterRouteWithoutHandlers(t *testing.T) {
	var pattern string
	app := testApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx) error {
		pattern = c.Pattern()
		return c.Next()
	}
	s := newAppServer(t, app)
	if w := send(s, "GET", "/api/nothing", ""); w.Code != 405 || pattern != "/api/nothing" {
		t.Fatalf("GET /api/nothing: %d, pattern %q", w.Code, pattern)
	}
}

// Path "/" is the exact home page, registered as /{$}, and c.Pattern still reports the Path.
func TestRegisterHomePage(t *testing.T) {
	var pattern string
	app := testApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx) error {
		pattern = c.Pattern()
		return c.Next()
	}
	s := newAppServer(t, app)
	if w := send(s, "GET", "/", ""); w.Code != 200 || pattern != "/" {
		t.Fatalf("GET /: %d %s, pattern %q", w.Code, w.Body.String(), pattern)
	}
	if w := send(s, "GET", "/other", ""); w.Code != 404 {
		t.Fatalf("GET /other: %d", w.Code)
	}
}

// Without a Renderer a page GET answers the results of every Load in its chain as JSON, by ID.
// The Layout Loads run concurrently with the page Load, and tifa.Use inside the page Load shares
// their runs, so each Load runs once.
func TestPageWithoutRendererAnswersLoadsAsJSON(t *testing.T) {
	s := newAppServer(t, testApp())
	siteLoads.Store(0)
	w := send(s, "GET", "/projects/p1", "")
	if w.Code != 200 {
		t.Fatalf("GET /projects/p1: %d %s", w.Code, w.Body.String())
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	want := map[string]string{
		"app/layout":          `{"name":"shop"}`,
		"app/projects":        `{"id":"p1","site":"shop"}`,
		"app/projects/orders": `["p1-1","p1-2"]`,
	}
	if len(got) != len(want) {
		t.Fatalf("got keys %v", got)
	}
	for id, data := range want {
		if string(got[id]) != data {
			t.Errorf("%s: %s, want %s", id, got[id], data)
		}
	}
	if n := siteLoads.Load(); n != 1 {
		t.Errorf("site Load ran %d times", n)
	}
	if w := send(s, "GET", "/", ""); w.Code != 200 || w.Body.String() != `{"app/layout":{"name":"shop"}}` {
		t.Errorf("GET /: %d %s", w.Code, w.Body.String())
	}
}

func TestPageLoadErrorsFollowTheErrorFlow(t *testing.T) {
	var unexpected []error
	app := testApp()
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error {
		unexpected = append(unexpected, err)
		return tifa.Error{Status: 503, Message: "later"}
	}
	s := newAppServer(t, app)
	if w := send(s, "GET", "/projects/p1?site=missing", ""); w.Code != 404 {
		t.Errorf("Layout ErrNotFound: %d %s", w.Code, w.Body.String())
	}
	if w := send(s, "GET", "/projects/broken", ""); w.Code != 503 || !strings.Contains(w.Body.String(), "later") {
		t.Errorf("page error: %d %s", w.Code, w.Body.String())
	}
	if len(unexpected) != 1 || !errors.Is(unexpected[0], errDatabase) {
		t.Errorf("OnError got %v", unexpected)
	}
}

func TestRegisterRejectsInconsistentApps(t *testing.T) {
	for name, app := range map[string]App{
		"unknown layout":   {Routes: []Route{{ID: "app/a", Path: "/a", Layouts: []string{"app/layout"}, GET: func(*tifa.Ctx) error { return nil }}}},
		"duplicate layout": {Layouts: []Layout{{ID: "app/layout"}, {ID: "app/layout"}}},
		"GET on a page":    {Routes: []Route{{ID: "app/a", Path: "/a", Page: true, GET: func(*tifa.Ctx) error { return nil }}}},
		"duplicate path": {Routes: []Route{
			{ID: "app/a", Path: "/a", GET: func(*tifa.Ctx) error { return nil }},
			{ID: "app/b", Path: "/a", POST: func(*tifa.Ctx) error { return nil }},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("register accepted the app")
				}
			}()
			newAppServer(t, app)
		})
	}
}
