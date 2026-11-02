package server

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/liustack/tifa"
)

// The application never sees _tifa: not in the query a Load reads, not in the URL OnRequest gets,
// and not in the URL a page is rendered for. A preload (a trailing .p) is answered like any other
// data request.
func TestNavigationParameterIsInvisibleToTheApp(t *testing.T) {
	var mu sync.Mutex
	var urls, queries, all []string
	app := frontApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		mu.Lock()
		urls = append(urls, c.Request().URL.RequestURI())
		mu.Unlock()
		return next(c)
	}
	app.Routes = append(app.Routes, Route{ID: "app/seen", Path: "/seen", Page: true, SSR: true, Layouts: []string{"app/layout"},
		Load: func(c *tifa.Ctx) (any, error) {
			mu.Lock()
			defer mu.Unlock()
			queries = append(queries, c.Query("_tifa")+"|"+c.Query("a"))
			all = append(all, strings.Join(c.QueryAll("_tifa"), ",")+"|"+c.Request().URL.RawQuery)
			return 1, nil
		}})
	s, r := newFrontServer(t, app)
	page := get(s, "/seen?a=1&b=2")
	for _, target := range []string{
		dataURL("/seen?a=1&b=2", "b1", "01"),
		dataURL("/seen?a=1&b=2", "b1", "01") + ".p",
		"/seen?a=1&_tifa=b1.01&b=2",
		"/seen?%5Ftifa=b1.01.p&a=1&b=2",
	} {
		w := get(s, target)
		if w.Code != 200 || routesOf(t, w)["app/seen"].Status != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s: %d %s", target, w.Code, w.Body.String())
		}
	}
	if page.Code != 200 || r.inputs[0].URL != "/seen?a=1&b=2" {
		t.Errorf("page: %d %q", page.Code, r.inputs[0].URL)
	}
	mu.Lock()
	defer mu.Unlock()
	for i, u := range urls {
		if u != "/seen?a=1&b=2" {
			t.Errorf("OnRequest %d saw %q", i, u)
		}
	}
	for i, q := range queries {
		if q != "|1" {
			t.Errorf("Load %d read %q", i, q)
		}
	}
	for i, q := range all {
		if q != "|a=1&b=2" {
			t.Errorf("Load %d saw the query %q", i, q)
		}
	}
	if len(urls) != 5 {
		t.Errorf("OnRequest ran %d times", len(urls))
	}
}

// A request that carries only the old headers is a page request, and a malformed _tifa, a bitmap
// of another length or another build are refused.
func TestNavigationParameterIsChecked(t *testing.T) {
	s, _ := newFrontServer(t, frontApp())
	if w := get(s, "/projects/p1", "X-Tifa-Route", "app/layout", "X-Tifa-Build", "b1"); !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("the old headers made a data request: %v", w.Header())
	}
	for target, want := range map[string]int{
		dataURL("/projects/p1", "b1", "100"):    200,
		dataURL("/projects/p1", "b1", "000"):    200,
		dataURL("/projects/p1", "b1", "11"):     400,
		dataURL("/projects/p1", "b1", "1100"):   400,
		dataURL("/projects/p1", "b1", "10x"):    400,
		dataURL("/projects/p1", "b1", ""):       400,
		"/projects/p1?_tifa=":                   400,
		"/projects/p1?_tifa=b1":                 400,
		"/projects/p1?_tifa=.100":               400,
		dataURL("/projects/p1", "old", "100"):   409,
		dataURL("/projects/p1", "old", "1"):     409,
		dataURL("/projects/p1", "b1", "100.p"):  200,
		dataURL("/projects/p1", "b1", "100.pp"): 400,
		dataURL("/projects/p1", "b1", "2"):      400,
	} {
		if w := get(s, target); w.Code != want {
			t.Errorf("%s: %d, want %d: %s", target, w.Code, want, w.Body.String())
		}
	}
	w := getData(s, "/projects/p1", "b1", "101")
	routes := routesOf(t, w)
	if _, ok := routes["app/projects"]; len(routes) != 2 || ok {
		t.Errorf("101 answered %v", reflect.ValueOf(routes).MapKeys())
	}
	if w.Header().Get("Cache-Control") != "private, no-cache" || strings.Contains(strings.Join(w.Header().Values("Vary"), ","), "X-Tifa") {
		t.Errorf("headers %v", w.Header())
	}
}

// A cache that looks only at the URL tells a page from its navigation data, and an answer it
// stored for one is never handed out for the other.
func TestPageAndNavigationDataDoNotShareAURL(t *testing.T) {
	s, _ := newFrontServer(t, frontApp())
	type stored struct {
		contentType string
		body        string
	}
	cache := map[string]stored{}
	fetch := func(url string) stored {
		if hit, ok := cache[url]; ok {
			return hit
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		out := stored{w.Header().Get("Content-Type"), w.Body.String()}
		cache[url] = out
		return out
	}
	for round := 0; round < 2; round++ {
		page, data := fetch("/projects/p1?x=1"), fetch(dataURL("/projects/p1?x=1", "b1", "111"))
		if !strings.HasPrefix(page.contentType, "text/html") || !strings.HasPrefix(data.contentType, "application/json") || page.body == data.body {
			t.Fatalf("round %d: page %q, data %q", round, page.contentType, data.contentType)
		}
	}
	if len(cache) != 2 {
		t.Errorf("cache keys %v", cache)
	}
}

// A preload and the navigation after it ask for the same URL with another ending, and the server
// treats them alike. Only the log of a request tells them apart.
func TestPreloadMarkIsOnlyInTheURL(t *testing.T) {
	var mu sync.Mutex
	var seen []*http.Request
	s, _ := newFrontServer(t, frontApp())
	for _, target := range []string{dataURL("/projects/p1", "b1", "100"), dataURL("/projects/p1", "b1", "100") + ".p"} {
		r := httptest.NewRequest("GET", target, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		mu.Lock()
		seen = append(seen, r)
		mu.Unlock()
		if w.Code != 200 {
			t.Errorf("%s: %d", target, w.Code)
		}
	}
	if !strings.HasSuffix(seen[1].URL.RawQuery, ".p") || strings.HasSuffix(seen[0].URL.RawQuery, ".p") || seen[0].Pattern != seen[1].Pattern || seen[0].Pattern == "" {
		t.Errorf("the middleware saw %q %q, patterns %q %q", seen[0].URL.RawQuery, seen[1].URL.RawQuery, seen[0].Pattern, seen[1].Pattern)
	}
}
