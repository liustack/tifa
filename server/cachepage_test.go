package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
)

// sharedEnv is an app with the page /news, which has Cache = time.Minute and a Load of a constant,
// and /fresh, whose Load changes on every request.
type sharedEnv struct {
	s      *server
	r      *renders
	loads  atomic.Int64
	header func(h http.Header)
}

func newSharedEnv(t *testing.T) *sharedEnv {
	t.Helper()
	e := &sharedEnv{}
	app := wireApp()
	app.Routes = append(app.Routes,
		Route{ID: "app/news", Path: "/news", Page: true, SSR: true, Cache: time.Minute, Layouts: []string{"app/layout"},
			Load: func(c *tifa.Ctx) (any, error) {
				e.loads.Add(1)
				if e.header != nil {
					e.header(c.Writer().Header())
				}
				return 7, nil
			}},
	)
	e.s, e.r = wireServer(t, app)
	return e
}

const wantShared = "public, max-age=0, s-maxage=60, stale-if-error=86400"

// A page with Cache runs its Load on every request. Nothing keeps the page for a later request: the
// only saving is the render, which the memo skips for the same data.
func TestCachePageRunsItsLoadOnEveryRequest(t *testing.T) {
	e := newSharedEnv(t)
	for range 3 {
		if w := get(e.s, "/news"); w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	if e.loads.Load() != 3 {
		t.Errorf("%d Loads for 3 requests", e.loads.Load())
	}
	if e.r.count() != 1 {
		t.Errorf("%d renders, want 1: the same data renders once", e.r.count())
	}
	for _, target := range []string{"/news", dataURL("/news", "b1", "11")} {
		before := e.loads.Load()
		get(e.s, target)
		get(e.s, target)
		if got := e.loads.Load() - before; got != 2 {
			t.Errorf("%s: %d Loads for 2 requests", target, got)
		}
	}
}

// The headers say a shared cache may keep the page for its Cache time. Nothing in them comes from a
// store in the program: no Age, no Cache-Status.
func TestCachePageSaysWhatASharedCacheMayKeep(t *testing.T) {
	e := newSharedEnv(t)
	first, second := get(e.s, "/news"), get(e.s, "/news")
	for name, w := range map[string]*httptest.ResponseRecorder{"first": first, "second": second} {
		if cc := w.Header().Get("Cache-Control"); cc != wantShared {
			t.Errorf("%s: Cache-Control %q", name, cc)
		}
		if w.Header().Get("Age") != "" || w.Header().Get("Cache-Status") != "" {
			t.Errorf("%s: Age %q, Cache-Status %q", name, w.Header().Get("Age"), w.Header().Get("Cache-Status"))
		}
	}
	// The 304 of the memo carries the same policy, and HEAD is answered like GET.
	tag := second.Header().Get("ETag")
	w := get(e.s, "/news", "If-None-Match", tag)
	if w.Code != 304 || w.Header().Get("Cache-Control") != wantShared {
		t.Errorf("304: %d, Cache-Control %q", w.Code, w.Header().Get("Cache-Control"))
	}
	h := httptest.NewRecorder()
	e.s.ServeHTTP(h, httptest.NewRequest("HEAD", "/news", nil))
	if h.Code != 200 || h.Header().Get("Cache-Control") != wantShared {
		t.Errorf("HEAD: %d, Cache-Control %q", h.Code, h.Header().Get("Cache-Control"))
	}
	// Navigation data is for one browser: it carries its session and is never public.
	if d := getData(e.s, "/news", "b1", "11"); d.Code != 200 || strings.Contains(d.Header().Get("Cache-Control"), "public") {
		t.Errorf("data: %d, Cache-Control %q", d.Code, d.Header().Get("Cache-Control"))
	}
}

// A Load or Layout can still say the answer is not for everyone.
func TestCachePageStaysPrivateWhenALoadSaysSo(t *testing.T) {
	for _, value := range []string{"private", "no-store", "no-cache"} {
		e := newSharedEnv(t)
		e.header = func(h http.Header) { h.Set("Cache-Control", value) }
		w := get(e.s, "/news")
		cc := w.Header().Get("Cache-Control")
		if w.Code != 200 || !strings.Contains(cc, value) || strings.Contains(cc, "public") || strings.Contains(cc, "s-maxage") {
			t.Errorf("%s: %d, Cache-Control %q", value, w.Code, cc)
		}
	}
}

// serverWithCache builds a server whose cache_control.s_maxage is sMaxage (the default when zero) and
// that has the page /a with the Cache the route gives.
func serverWithCache(t *testing.T, sMaxage time.Duration, route Route) *server {
	t.Helper()
	cfg := config.Default()
	if sMaxage != 0 {
		cfg.CacheControl.SMaxage = sMaxage
	}
	app := frontApp()
	route.ID, route.Path, route.Page, route.SSR, route.Layouts = "app/a", "/a", true, true, []string{"app/layout"}
	app.Routes = append(app.Routes, route)
	s := newServer(cfg, app.Hooks)
	t.Cleanup(s.close)
	s.register(app.Layouts, app.Routes)
	r := &renders{}
	if err := s.setFront(dist, r.render); err != nil {
		t.Fatal(err)
	}
	return s
}

func sharedFor(seconds string) string {
	return "public, max-age=0, s-maxage=" + seconds + ", stale-if-error=86400"
}

// const Cache = true takes its time from cache_control.s_maxage, 1m unless the setting says another.
func TestCacheTrueUsesTheSettingForItsTime(t *testing.T) {
	if got := config.Default().CacheControl.SMaxage; got != time.Minute {
		t.Fatalf("the default s_maxage is %v, want 1m", got)
	}
	if cc := cacheControl(get(serverWithCache(t, 0, Route{CacheDefault: true}), "/a")); cc != sharedFor("60") {
		t.Errorf("Cache = true: Cache-Control %q", cc)
	}
	if cc := cacheControl(get(serverWithCache(t, 5*time.Minute, Route{CacheDefault: true}), "/a")); cc != sharedFor("300") {
		t.Errorf("Cache = true with s_maxage 5m: Cache-Control %q", cc)
	}
}

// A duration in const Cache is the page's own time, whatever the setting says.
func TestCacheDurationIsTheTimeOfThePage(t *testing.T) {
	if cc := cacheControl(get(serverWithCache(t, 5*time.Minute, Route{Cache: 10 * time.Minute}), "/a")); cc != sharedFor("600") {
		t.Errorf("Cache = 10m: Cache-Control %q", cc)
	}
	if cc := cacheControl(get(serverWithCache(t, 0, Route{Cache: time.Second}), "/a")); cc != sharedFor("1") {
		t.Errorf("Cache = 1s: Cache-Control %q", cc)
	}
	// No Cache, no shared policy.
	if cc := cacheControl(get(serverWithCache(t, 0, Route{}), "/a")); cc != defaultCache {
		t.Errorf("no Cache: Cache-Control %q", cc)
	}
}

// A page with Cache whose render fails goes out as an error, never with the policy that lets a
// shared cache keep the page: the one the framework wrote before the render is taken back. A policy
// the app set stays.
func TestCachePageFailureIsNeverPublic(t *testing.T) {
	timedOut := fmt.Errorf("ssr: render timed out after 3s: %w", context.DeadlineExceeded)
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"timeout, not tried again", timedOut},
		{"no levels to try", errors.New("ssr: render page: boom")},
		{"every try fails", pageError{levels: 2}},
	} {
		for _, own := range []string{"", "private"} {
			e := newSharedEnv(t)
			if own != "" {
				e.header = func(h http.Header) { h.Set("Cache-Control", own) }
			}
			e.r.result = func(ssr.Input) (string, error) { return "", tc.err }
			w := get(e.s, "/news")
			want := "no-store"
			if own != "" {
				want = own
			}
			if w.Code != 500 || cacheControl(w) != want {
				t.Errorf("%s, app sets %q: %d, Cache-Control %q, want %q", tc.name, own, w.Code, cacheControl(w), want)
			}
		}
	}
}

// A redirect from a Load of a page with Cache is the same for everyone, so a shared cache may keep
// it. A Load that makes the answer private still does.
func TestCachePageRedirectIsShared(t *testing.T) {
	for _, tc := range []struct {
		status int
		own    string
		want   string
	}{
		{302, "", wantShared},
		{301, "", wantShared},
		{308, "", wantShared},
		{302, "private", "private"},
		{302, "no-store", "no-store"},
	} {
		app := wireApp()
		app.Routes = append(app.Routes, Route{ID: "app/old", Path: "/old", Page: true, SSR: true, Cache: time.Minute, Layouts: []string{"app/layout"},
			Load: func(c *tifa.Ctx) (any, error) {
				if tc.own != "" {
					c.SetHeader("Cache-Control", tc.own)
				}
				return nil, tifa.Redirect("/new", tc.status)
			}})
		s, _ := wireServer(t, app)
		w := get(s, "/old")
		if w.Code != tc.status || cacheControl(w) != tc.want {
			t.Errorf("%d, app sets %q: %d, Cache-Control %q, want %q", tc.status, tc.own, w.Code, cacheControl(w), tc.want)
		}
		d := getData(s, "/old", "b1", "11")
		if d.Code != 200 || !strings.Contains(d.Body.String(), fmt.Sprintf(`"status":%d,"location":"/new"`, tc.status)) || strings.Contains(cacheControl(d), "public") {
			t.Errorf("%d: the data answer is %d %q, Cache-Control %q", tc.status, d.Code, d.Body.String(), cacheControl(d))
		}
	}
}

// Only what the framework wrote is taken back from a failed page. A directive the app adds after the
// page ran stays, and so does every other value.
func TestCachePageFailureKeepsWhatTheAppAdded(t *testing.T) {
	app := wireApp()
	app.Routes = append(app.Routes, Route{ID: "app/news", Path: "/news", Page: true, SSR: true, Cache: time.Minute, Layouts: []string{"app/layout"},
		Load: func(c *tifa.Ctx) (any, error) { return 7, nil }})
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		err := next(c)
		c.Writer().Header().Add("Cache-Control", "must-revalidate")
		return err
	}
	s, r := wireServer(t, app)
	r.result = func(ssr.Input) (string, error) { return "", errors.New("ssr: render page: boom") }
	w := get(s, "/news")
	if got := w.Header().Values("Cache-Control"); w.Code != 500 || len(got) != 1 || got[0] != "must-revalidate" {
		t.Errorf("%d, Cache-Control %q, want only the directive the app added", w.Code, got)
	}
}

// Only the GET and HEAD that the framework made anonymous are shared. A POST to the same URL reads
// the visitor, so its redirect is not for everyone.
func TestCachePagePostRedirectIsNotShared(t *testing.T) {
	app := wireApp()
	app.Routes = append(app.Routes, Route{ID: "app/news", Path: "/news", Page: true, SSR: true, Cache: time.Minute, Layouts: []string{"app/layout"},
		Load: func(c *tifa.Ctx) (any, error) { return 7, nil },
		POST: func(c *tifa.Ctx) error { return tifa.Redirect("/to/"+c.Header("Authorization"), 302) }})
	s, _ := wireServer(t, app)
	r := httptest.NewRequest("POST", "/news", nil)
	r.Header.Set("Authorization", "alice")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 302 || strings.Contains(cacheControl(w), "public") {
		t.Errorf("POST: %d, Location %q, Cache-Control %q", w.Code, w.Header().Get("Location"), cacheControl(w))
	}
}
