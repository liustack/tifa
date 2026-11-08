package server

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa"
)

// dataAgeOf reads the age of the data of a page from the Server-Timing of an answer, in
// milliseconds, and whether the answer has one.
func dataAgeOf(t *testing.T, values []string) (int64, bool) {
	t.Helper()
	var ages []int64
	for _, value := range values {
		for _, metric := range strings.Split(value, ",") {
			name, params, _ := strings.Cut(strings.TrimSpace(metric), ";")
			if name != "tifa-age" {
				continue
			}
			dur, ok := strings.CutPrefix(params, "dur=")
			ms, err := strconv.ParseInt(dur, 10, 64)
			if !ok || err != nil {
				t.Fatalf("Server-Timing %q", values)
			}
			ages = append(ages, ms)
		}
	}
	if len(ages) > 1 {
		t.Fatalf("Server-Timing has %d ages: %q", len(ages), values)
	}
	if len(ages) == 0 {
		return 0, false
	}
	return ages[0], true
}

func slowLayout(c *tifa.Ctx) (string, error) {
	time.Sleep(20 * time.Millisecond)
	return "site", nil
}

// A page says how old its data may be when it goes out, counted from the start of its first Load:
// the first render, a render the render cache answers, a 304, and the 404 pages it renders. The
// age counts a Load that OnRequest ran before the others. Navigation data does not carry it.
func TestPageSendsTheAgeOfItsData(t *testing.T) {
	early := false
	app := App{
		Hooks: Hooks{OnRequest: func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
			if early {
				if _, err := tifa.Use(c, slowLayout); err != nil {
					return err
				}
				time.Sleep(30 * time.Millisecond)
			}
			return next(c)
		}},
		Layouts: []Layout{{ID: "app/layout", Path: "/", SSR: true, Load: func(c *tifa.Ctx) (any, error) { return tifa.Use(c, slowLayout) }}},
		Routes: []Route{
			{ID: "app/page", Path: "/p", Page: true, SSR: true, Layouts: []string{"app/layout"},
				Load: func(c *tifa.Ctx) (any, error) { return 1, nil }},
			{ID: "app/gone", Path: "/gone", Page: true, SSR: true, Layouts: []string{"app/layout"},
				Load: func(c *tifa.Ctx) (any, error) { return nil, tifa.ErrNotFound }},
		},
	}
	s, _ := newFrontServer(t, app)
	aged := func(name string, status int, render string, least int64, target string, header ...string) string {
		t.Helper()
		w := get(s, target, header...)
		if w.Code != status || w.Header().Get("X-Tifa-Render") != render {
			t.Fatalf("%s: %d %q, want %d %q", name, w.Code, w.Header().Get("X-Tifa-Render"), status, render)
		}
		age, ok := dataAgeOf(t, w.Header().Values("Server-Timing"))
		if !ok || age < least || age > 5000 {
			t.Errorf("%s: age %d %t, want at least %d", name, age, ok, least)
		}
		return w.Header().Get("ETag")
	}
	etag := aged("render", 200, "vm", 20, "/p")
	aged("render cache", 200, "cache", 20, "/p")
	aged("304", 304, "none", 20, "/p", "If-None-Match", etag)
	aged("404 of a Load", 404, "vm", 20, "/gone")
	aged("404 of a URL", 404, "vm", 20, "/nothing/here")
	early = true
	aged("Load in OnRequest", 200, "cache", 50, "/p")
	early = false

	w := getData(s, "/p", "b1", "11")
	if _, ok := dataAgeOf(t, w.Header().Values("Server-Timing")); w.Code != 200 || ok {
		t.Errorf("navigation data: %d %q", w.Code, w.Header().Values("Server-Timing"))
	}
}

// A page with Cache may be kept by a CDN, which would serve the age it was sent with minutes later,
// so it carries no age, on a first request, a repeat and a 304, and the browser takes its data as of
// unknown age. A page whose Load makes the answer for one visitor carries its age.
func TestPageWithCacheSendsNoAge(t *testing.T) {
	e := newSharedEnv(t)
	first := get(e.s, "/news")
	repeat := get(e.s, "/news")
	same := get(e.s, "/news", "If-None-Match", first.Header().Get("ETag"))
	for name, w := range map[string]*httptest.ResponseRecorder{"first": first, "repeat": repeat, "304": same} {
		if _, ok := dataAgeOf(t, w.Header().Values("Server-Timing")); ok || w.Header().Get("Cache-Control") == "" {
			t.Errorf("%s: %d %v", name, w.Code, w.Header())
		}
	}
	if same.Code != 304 {
		t.Errorf("304: %d", same.Code)
	}
	w := getData(e.s, "/news", "b1", "11")
	if _, ok := dataAgeOf(t, w.Header().Values("Server-Timing")); w.Code != 200 || ok {
		t.Errorf("navigation data: %d %q", w.Code, w.Header().Values("Server-Timing"))
	}

	own := newSharedEnv(t)
	own.header = func(h http.Header) { h.Set("Cache-Control", "private, no-cache") }
	w = get(own.s, "/news")
	if _, ok := dataAgeOf(t, w.Header().Values("Server-Timing")); w.Code != 200 || !ok {
		t.Errorf("private: %d %v", w.Code, w.Header())
	}
}

// A page carries its age only when no cache may reuse the answer without asking the server: its
// Cache-Control says no-cache or no-store, or the app set none and the answer goes out as private,
// no-cache.
func TestPageSendsItsAgeOnlyWhenEveryCacheAsksAgain(t *testing.T) {
	var cacheControl string
	app := App{
		Layouts: []Layout{{ID: "app/layout", Path: "/", SSR: true}},
		Routes: []Route{{ID: "app/page", Path: "/p", Page: true, SSR: true, Layouts: []string{"app/layout"},
			Load: func(c *tifa.Ctx) (any, error) {
				if cacheControl != "" {
					c.SetHeader("Cache-Control", cacheControl)
				}
				return 1, nil
			}}},
	}
	s, _ := newFrontServer(t, app)
	for _, tc := range []struct {
		cacheControl string
		aged         bool
	}{
		{"", true},
		{"no-cache", true},
		{"public, no-cache", true},
		{"No-Store", true},
		{"private, max-age=0, no-cache", true},
		{"public, max-age=60", false},
		{"private, max-age=60", false},
		{"public, s-maxage=600", false},
		{"max-age=0", false},
		{`no-cache="Set-Cookie", max-age=60`, false},
		{`no-cache="a`, false},
	} {
		cacheControl = tc.cacheControl
		w := get(s, "/p")
		if _, ok := dataAgeOf(t, w.Header().Values("Server-Timing")); w.Code != 200 || ok != tc.aged {
			t.Errorf("Cache-Control %q: %d, age %t, want %t", tc.cacheControl, w.Code, ok, tc.aged)
		}
	}
}
