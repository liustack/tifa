package server

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liustack/tifa"
)

// A URL that matches no page shows its 404 inside the Layout of the route that matches the most
// of it, comparing segment by segment: a literal before a parameter, a parameter before the end.
// A page counts with the Layout it is in, a page in a Layout's own folder and a page for the rest
// of the path count as the Layout. The parameters of that Layout's path are set for its Loads.
func TestUnmatchedURLShowsItsNotFoundInsideTheDeepestLayout(t *testing.T) {
	page := func(id, path string, layouts ...string) Route {
		return Route{ID: id, Path: path, Page: true, SSR: true, Layouts: layouts}
	}
	s := newAppServer(t, App{
		Layouts: []Layout{
			{ID: "app/layout", Path: "/", SSR: true},
			{ID: "app/a/layout", Path: "/a", SSR: true},
			{ID: "app/teams/team+/layout", Path: "/teams/{team}", SSR: true},
			{ID: "app/files/path++/layout", Path: "/files/{path...}", SSR: true},
		},
		Routes: []Route{
			page("app/route", "/", "app/layout"),
			page("app/a/one/route", "/a/one", "app/layout", "app/a/layout"),
			page("app/teams/team+/route", "/teams/{team}", "app/layout", "app/teams/team+/layout"),
			page("app/teams/team+/one/route", "/teams/{team}/one", "app/layout", "app/teams/team+/layout"),
			page("app/teams/new/route", "/teams/new", "app/layout"),
			page("app/files/path++/route", "/files/{path...}", "app/layout", "app/files/path++/layout"),
		},
	})
	for _, c := range []struct{ url, layout, team string }{
		{"/nowhere/at/all", "app/layout", ""},
		{"/a", "app/a/layout", ""},
		{"/a/two", "app/a/layout", ""},
		{"/a/one/x", "app/a/layout", ""},
		{"/teams/x/missing", "app/teams/team+/layout", "x"},
		{"/teams/x/one/y", "app/teams/team+/layout", "x"},
		{"/teams/new/x", "app/layout", ""},
		{"/teams/%E4%B8%AD/x", "app/teams/team+/layout", "中"},
	} {
		r := httptest.NewRequest("GET", c.url, nil)
		if got := s.notFoundAt(r); got != c.layout || r.PathValue("team") != c.team {
			t.Errorf("%s: inside %q with team %q, want %q with %q", c.url, got, r.PathValue("team"), c.layout, c.team)
		}
	}
}

// An unmatched URL runs the Loads of the Layouts it shows its 404 inside, with the parameters of
// their paths, and renders with their results and status 404. OnRequest sees no pattern and
// tifa.ErrNotFound from Next. A navigation data request gets the listed results and status 404.
func TestUnmatchedURLRunsItsLayouts(t *testing.T) {
	var patterns []string
	var nexts []error
	app := frontApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		patterns = append(patterns, c.Pattern())
		err := next(c)
		nexts = append(nexts, err)
		return err
	}
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error { return tifa.Error{Status: 503, Message: "later"} }
	s, r := newFrontServer(t, app)

	w := get(s, "/projects/p9/missing")
	data := r.last(t)
	if w.Code != 404 || len(data) != 2 || data["app/layout"].Status != 200 || data["app/projects"].Status != 200 {
		t.Errorf("page: %d %v", w.Code, data)
	}
	if !strings.Contains(strings.Join(r.inputs[len(r.inputs)-1].Texts, " "), `"id":"p9"`) {
		t.Errorf("the Layout did not read the parameter: %v", r.inputs[len(r.inputs)-1].Texts)
	}
	if len(patterns) != 1 || patterns[0] != "" || !errors.Is(nexts[0], tifa.ErrNotFound) {
		t.Errorf("OnRequest saw patterns %q and %v", patterns, nexts)
	}
	if v := w.Header().Values("Vary"); !strings.Contains(strings.Join(v, ","), routeHeader) {
		t.Errorf("Vary %q", v)
	}

	w = get(s, "/projects/p9/missing", routeHeader, "app/projects", buildHeader, "b1")
	var items map[string]routeResult
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil || w.Code != 404 || len(items) != 1 || items["app/projects"].Status != 200 {
		t.Errorf("data request: %d %s", w.Code, w.Body.String())
	}
	if w := get(s, "/projects/p9/missing", routeHeader, "app/projects/orders", buildHeader, "b1"); w.Code != 400 {
		t.Errorf("data request for a route the URL does not load: %d %s", w.Code, w.Body.String())
	}
	if w := get(s, "/projects/p9/missing", routeHeader, "app/projects", buildHeader, "old"); w.Code != 409 {
		t.Errorf("data request of another build: %d %s", w.Code, w.Body.String())
	}

	// A Layout that fails is answered as on a page: with its status, the Layout's result failed.
	w = get(s, "/projects/broken/missing")
	if data := r.last(t); w.Code != 503 || data["app/projects"].Status != 503 || data["app/layout"].Status != 200 {
		t.Errorf("failed Layout: %d %v", w.Code, data)
	}
	w = get(s, "/projects/broken/missing", routeHeader, "app/layout,app/projects", buildHeader, "b1")
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil || w.Code != 200 || items["app/projects"].Status != 503 {
		t.Errorf("data request with a failed Layout: %d %s", w.Code, w.Body.String())
	}
}

// When OnRequest answers an unmatched URL with tifa.ErrNotFound without calling Next, no Load ran:
// each Load of the Layouts the URL shows its 404 inside counts as a 404.
func TestUnmatchedURLThatOnRequestRefuses(t *testing.T) {
	app := frontApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error { return tifa.ErrNotFound }
	s, r := newFrontServer(t, app)
	w := get(s, "/projects/p9/missing")
	if data := r.last(t); w.Code != 404 || len(data) != 2 || data["app/layout"].Status != 404 || data["app/projects"].Status != 404 {
		t.Errorf("%d %v", w.Code, data)
	}
}
