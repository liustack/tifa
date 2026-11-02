package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
)

// cacheClock replaces the clock of the whole-page cache and moves only when told to.
type cacheClock struct {
	mu  sync.Mutex
	now time.Time
}

func useCacheClock(t *testing.T) *cacheClock {
	t.Helper()
	c := &cacheClock{now: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)}
	old := cacheNow
	cacheNow = func() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
	t.Cleanup(func() { cacheNow = old })
	return c
}

func (c *cacheClock) advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

// pageEnv is an app with the page /news, which has the whole-page cache for a minute. What its
// Load does is set by the test.
type pageEnv struct {
	loads atomic.Int64
	mu    sync.Mutex
	load  func(c *tifa.Ctx, n int64) (any, error)
	clock *cacheClock
	s     *Server
	r     *renders
}

func newPageEnv(t *testing.T, prepare ...func(*App)) *pageEnv {
	t.Helper()
	e := &pageEnv{clock: useCacheClock(t)}
	e.load = func(c *tifa.Ctx, n int64) (any, error) { return map[string]int64{"n": n}, nil }
	app := wireApp()
	app.Routes = append(app.Routes,
		Route{ID: "app/news", Path: "/news", Page: true, SSR: true, Cache: time.Minute, Layouts: []string{"app/layout"},
			Query: []string{"page"},
			Load: func(c *tifa.Ctx) (any, error) {
				n := e.loads.Add(1)
				e.mu.Lock()
				load := e.load
				e.mu.Unlock()
				_ = c.Query("page")
				return load(c, n)
			}},
		Route{ID: "app/live", Path: "/livepage", Page: true, SSR: true, Cache: time.Minute, NoRenderCache: true, Layouts: []string{"app/layout"}},
	)
	for _, p := range prepare {
		p(&app)
	}
	e.s, e.r = wireServer(t, app)
	return e
}

func (e *pageEnv) setLoad(f func(c *tifa.Ctx, n int64) (any, error)) {
	e.mu.Lock()
	e.load = f
	e.mu.Unlock()
}

func (e *pageEnv) body(w *httptest.ResponseRecorder) string { return w.Body.String() }

func TestCachedPageIsServedWithoutLoadOrRender(t *testing.T) {
	e := newPageEnv(t)
	first := get(e.s, "/news")
	second := get(e.s, "/news")
	if first.Code != 200 || second.Code != 200 || e.loads.Load() != 1 || e.r.count() != 1 {
		t.Fatalf("%d %d, %d Loads, %d renders", first.Code, second.Code, e.loads.Load(), e.r.count())
	}
	if first.Header().Get("X-Tifa-Render") != "vm" || second.Header().Get("X-Tifa-Render") != "cache" {
		t.Errorf("X-Tifa-Render: %q then %q", first.Header().Get("X-Tifa-Render"), second.Header().Get("X-Tifa-Render"))
	}
	if first.Body.String() != second.Body.String() || first.Header().Get("ETag") == "" || first.Header().Get("ETag") != second.Header().Get("ETag") {
		t.Errorf("bodies or tags differ: %q %q", first.Header().Get("ETag"), second.Header().Get("ETag"))
	}
	// A hit still runs OnRequest, anonymous, each time.
	var runs atomic.Int64
	e2 := newPageEnv(t, func(app *App) {
		app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error { runs.Add(1); return next(c) }
	})
	get(e2.s, "/news")
	get(e2.s, "/news")
	get(e2.s, "/news")
	if runs.Load() != 3 || e2.loads.Load() != 1 {
		t.Errorf("OnRequest ran %d times, Load %d", runs.Load(), e2.loads.Load())
	}
}

func TestCachedPageIgnoresForcedRefresh(t *testing.T) {
	e := newPageEnv(t)
	get(e.s, "/news")
	for _, h := range [][]string{{"Cache-Control", "no-cache"}, {"Cache-Control", "max-age=0"}, {"Pragma", "no-cache"}} {
		if w := get(e.s, "/news", h...); w.Header().Get("X-Tifa-Render") != "cache" {
			t.Errorf("%v: %q", h, w.Header().Get("X-Tifa-Render"))
		}
	}
	if e.loads.Load() != 1 {
		t.Errorf("%d Loads", e.loads.Load())
	}
}

func TestCachedPageSharesTheEntryWithHEAD(t *testing.T) {
	e := newPageEnv(t)
	r := httptest.NewRequest("HEAD", "/news", nil)
	w := httptest.NewRecorder()
	e.s.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Cache-Status") != "tifa; fwd=miss" {
		t.Fatalf("HEAD: %d %v", w.Code, w.Header())
	}
	if g := get(e.s, "/news"); g.Header().Get("X-Tifa-Render") != "cache" || g.Body.Len() == 0 {
		t.Errorf("GET after HEAD: %v", g.Header())
	}
	w = httptest.NewRecorder()
	e.s.ServeHTTP(w, httptest.NewRequest("HEAD", "/news", nil))
	if w.Header().Get("Cache-Status") != "tifa; hit" || e.loads.Load() != 1 {
		t.Errorf("HEAD after GET: %v, %d Loads", w.Header(), e.loads.Load())
	}
}

func TestCachedPageHeaders(t *testing.T) {
	e := newPageEnv(t)
	w := get(e.s, "/news")
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=0, s-maxage=60, stale-if-error=86400" {
		t.Errorf("first Cache-Control %q", cc)
	}
	if w.Header().Get("Age") != "0" || w.Header().Get("Cache-Status") != "tifa; fwd=miss" {
		t.Errorf("first: Age %q, Cache-Status %q", w.Header().Get("Age"), w.Header().Get("Cache-Status"))
	}
	e.clock.advance(25 * time.Second)
	w = get(e.s, "/news")
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=0, s-maxage=60, stale-if-error=86400" {
		t.Errorf("hit Cache-Control %q", cc)
	}
	if w.Header().Get("Age") != "25" || w.Header().Get("Cache-Status") != "tifa; hit" {
		t.Errorf("hit: Age %q, Cache-Status %q", w.Header().Get("Age"), w.Header().Get("Cache-Status"))
	}
	// 304 from the entry, with the same headers and no render or Load.
	tag := w.Header().Get("ETag")
	n := e.r.count()
	w = get(e.s, "/news", "If-None-Match", tag)
	if w.Code != 304 || w.Body.Len() != 0 || e.r.count() != n || e.loads.Load() != 1 {
		t.Errorf("If-None-Match: %d, %d bytes, %d renders, %d Loads", w.Code, w.Body.Len(), e.r.count()-n, e.loads.Load())
	}
	if w.Header().Get("Cache-Status") != "tifa; hit" || !strings.HasPrefix(w.Header().Get("Cache-Control"), "public, max-age=0, s-maxage=60") || w.Header().Get("ETag") != tag {
		t.Errorf("304 headers %v", w.Header())
	}
	if w := get(e.s, "/news", "If-None-Match", `"other"`); w.Code != 200 {
		t.Errorf("another tag: %d", w.Code)
	}
}

func TestExpiredPageIsMadeAgainOnceWhileRequestsWait(t *testing.T) {
	e := newPageEnv(t)
	get(e.s, "/news")
	e.clock.advance(61 * time.Second)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) {
		once.Do(func() { close(started) })
		<-release
		return map[string]int64{"n": n}, nil
	})
	var wg sync.WaitGroup
	bodies := make([]string, 5)
	hows := make([]string, 5)
	for i := range bodies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := get(e.s, "/news")
			bodies[i], hows[i] = w.Body.String(), w.Header().Get("X-Tifa-Render")
		}()
	}
	<-started
	// The old page is not given while the new one is made: every request waits.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if e.loads.Load() != 2 {
		t.Fatalf("%d Loads, want the first and one new", e.loads.Load())
	}
	vm, shared := 0, 0
	for i, b := range bodies {
		if !strings.Contains(b, `"n":2`) {
			t.Errorf("request %d got %s", i, b)
		}
		switch hows[i] {
		case "vm":
			vm++
		case "shared":
			shared++
		}
	}
	if vm != 1 || shared != 4 {
		t.Errorf("vm %d, shared %d", vm, shared)
	}
	// Data that did not change: the render cache answers, so nothing is rendered again.
	e2 := newPageEnv(t)
	get(e2.s, "/news")
	n := e2.r.count()
	e2.clock.advance(61 * time.Second)
	e2.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return map[string]int64{"n": 1}, nil })
	w := get(e2.s, "/news")
	if w.Code != 200 || e2.loads.Load() != 2 || e2.r.count() != n {
		t.Errorf("same data: %d, %d Loads, %d renders", w.Code, e2.loads.Load(), e2.r.count()-n)
	}
}

// When making the page fails after its time is over, the old page is served: for a Load that
// fails, a render that fails, one that runs out of time and an overloaded pool.
func TestFailedRemakeServesTheOldPage(t *testing.T) {
	for name, fail := range map[string]func(e *pageEnv){
		"Load fails": func(e *pageEnv) {
			e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return nil, errors.New("database down") })
		},
		"Load fails with a 503": func(e *pageEnv) {
			e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return nil, tifa.Error{Status: 503, Message: "busy"} })
		},
		"render fails": func(e *pageEnv) {
			e.r.result = func(in ssr.Input) (string, error) { return "", errors.New("component threw") }
		},
		"render times out": func(e *pageEnv) {
			e.r.result = func(in ssr.Input) (string, error) { return "", fmt.Errorf("render: %w", context.DeadlineExceeded) }
		},
		"pool is busy": func(e *pageEnv) {
			e.r.result = func(in ssr.Input) (string, error) { return "", tifa.ErrSSRBusy }
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newPageEnv(t)
			old := get(e.s, "/news")
			oldData := getData(e.s, "/news", "b1", "11")
			e.clock.advance(61 * time.Second)
			fail(e)
			w := get(e.s, "/news")
			if w.Code != 200 || w.Body.String() != old.Body.String() || w.Header().Get("Cache-Status") != "tifa; fwd=stale" {
				t.Fatalf("%d %s, %v", w.Code, w.Body.String(), w.Header())
			}
			if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "s-maxage=60") || w.Header().Get("Age") != "61" {
				t.Errorf("stale: Cache-Control %q, Age %q", cc, w.Header().Get("Age"))
			}
			// The navigation gets the old data too.
			d := getData(e.s, "/news", "b1", "11")
			if d.Code != 200 {
				t.Errorf("data: %d %s", d.Code, d.Body.String())
			}
			if routesOf(t, d)["app/news"].Data.(map[string]any)["n"].(float64) != routesOf(t, oldData)["app/news"].Data.(map[string]any)["n"].(float64) {
				t.Errorf("data %s, was %s", d.Body.String(), oldData.Body.String())
			}
			// When it works again the page is new.
			e.r.result = nil
			e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return map[string]int64{"n": 100}, nil })
			if w := get(e.s, "/news"); w.Code != 200 || !strings.Contains(w.Body.String(), `"n":100`) || w.Header().Get("Cache-Status") != "tifa; fwd=miss" {
				t.Errorf("after the fix: %d %s %v", w.Code, w.Body.String(), w.Header())
			}
		})
	}
}

func TestFailedRemakeWithoutAnOldPageIsAnError(t *testing.T) {
	e := newPageEnv(t)
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return nil, errors.New("database down") })
	if w := get(e.s, "/news"); w.Code != 500 || w.Header().Get("Cache-Control") == "" || strings.Contains(w.Header().Get("Cache-Control"), "public") {
		t.Errorf("%d %v", w.Code, w.Header())
	}
	if w := get(e.s, "/news"); w.Code != 500 {
		t.Errorf("an error was kept: %d", w.Code)
	}
}

// An answer the app gives on purpose is the page, not a failure, so the old one is not served
// in its place, and it does not come back later.
func TestAnswersOfTheAppReplaceTheOldPage(t *testing.T) {
	e := newPageEnv(t)
	get(e.s, "/news")
	e.clock.advance(61 * time.Second)
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return nil, tifa.ErrNotFound })
	if w := get(e.s, "/news"); w.Code != 404 || w.Header().Get("Cache-Status") != "" || strings.Contains(w.Header().Get("Cache-Control"), "public") {
		t.Fatalf("404: %d %v", w.Code, w.Header())
	}
	// The page then fails: the deleted page does not return as the old one.
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return nil, errors.New("database down") })
	if w := get(e.s, "/news"); w.Code != 500 {
		t.Errorf("after a 404 the failure gave %d", w.Code)
	}
	// A redirect is an answer too.
	e2 := newPageEnv(t)
	get(e2.s, "/news")
	e2.clock.advance(61 * time.Second)
	e2.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return nil, tifa.Redirect("/moved", 307) })
	if w := get(e2.s, "/news"); w.Code != 307 || w.Header().Get("Location") != "/moved" {
		t.Errorf("redirect: %d %v", w.Code, w.Header())
	}
}

func TestOldPageStandsInForADayOnly(t *testing.T) {
	e := newPageEnv(t)
	get(e.s, "/news")
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return nil, errors.New("database down") })
	e.clock.advance(time.Minute + 24*time.Hour - time.Second)
	if w := get(e.s, "/news"); w.Code != 200 || w.Header().Get("Cache-Status") != "tifa; fwd=stale" {
		t.Errorf("just inside the day: %d %v", w.Code, w.Header())
	}
	e.clock.advance(2 * time.Second)
	if w := get(e.s, "/news"); w.Code != 500 {
		t.Errorf("past the day: %d", w.Code)
	}
}

func TestPageIsNotKeptWhenTheAnswerCannotBeShared(t *testing.T) {
	for name, set := range map[string]func(c *tifa.Ctx){
		"private":          func(c *tifa.Ctx) { c.SetHeader("Cache-Control", "private, max-age=60") },
		"no-store":         func(c *tifa.Ctx) { c.SetHeader("Cache-Control", "no-store") },
		"no-cache":         func(c *tifa.Ctx) { c.SetHeader("Cache-Control", "public, no-cache") },
		"Vary":             func(c *tifa.Ctx) { c.SetHeader("Vary", "Accept-Language") },
		"Vary star":        func(c *tifa.Ctx) { c.SetHeader("Vary", "*") },
		"Vary with others": func(c *tifa.Ctx) { c.SetHeader("Vary", "Accept-Encoding, Cookie") },
	} {
		t.Run(name, func(t *testing.T) {
			e := newPageEnv(t)
			e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { set(c); return n, nil })
			get(e.s, "/news")
			w := get(e.s, "/news")
			if e.loads.Load() != 2 || w.Header().Get("X-Tifa-Render") == "cache" {
				t.Errorf("%d Loads, %q", e.loads.Load(), w.Header().Get("X-Tifa-Render"))
			}
			if strings.Contains(w.Header().Get("Cache-Control"), "public, max-age=0") {
				t.Errorf("an answer that is not kept says %q", w.Header().Get("Cache-Control"))
			}
		})
	}
	// OnRequest can say it too.
	var calls atomic.Int64
	e := newPageEnv(t, func(app *App) {
		app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
			if calls.Add(1) == 1 {
				c.SetHeader("Cache-Control", "no-store")
			}
			return next(c)
		}
	})
	get(e.s, "/news")
	if w := get(e.s, "/news"); w.Header().Get("X-Tifa-Render") == "cache" || e.loads.Load() != 2 {
		t.Errorf("OnRequest no-store: %d Loads", e.loads.Load())
	}
	// Vary: Accept-Encoding is the framework's own and does not stop it.
	e = newPageEnv(t)
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { c.SetHeader("Vary", "Accept-Encoding"); return n, nil })
	get(e.s, "/news")
	if w := get(e.s, "/news"); w.Header().Get("X-Tifa-Render") != "cache" {
		t.Errorf("Vary: Accept-Encoding: %q", w.Header().Get("X-Tifa-Render"))
	}
}

// Only 200, 301, 302 and 308 are kept. A result that is not kept makes the requests that follow
// skip the cache for a while, so they do not queue behind each other.
func TestStatusesAreKeptOrNot(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code int
		kept bool
	}{
		{"301", nil, 301, true}, {"302", nil, 302, true}, {"308", nil, 308, true},
		{"307", nil, 307, false}, {"303", nil, 303, false},
		{"404", tifa.ErrNotFound, 404, false}, {"409", tifa.Error{Status: 409}, 409, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newPageEnv(t)
			e.setLoad(func(c *tifa.Ctx, n int64) (any, error) {
				if tc.err != nil {
					return nil, tc.err
				}
				return nil, tifa.Redirect("/to", tc.code)
			})
			first := get(e.s, "/news")
			second := get(e.s, "/news")
			if first.Code != tc.code || second.Code != tc.code {
				t.Fatalf("%d %d", first.Code, second.Code)
			}
			if kept := e.loads.Load() == 1; kept != tc.kept {
				t.Errorf("Load ran %d times, kept %v", e.loads.Load(), tc.kept)
			}
			if tc.err == nil && (second.Header().Get("Location") != "/to") {
				t.Errorf("Location %q", second.Header().Get("Location"))
			}
		})
	}
	// HTML cached for the redirect page also serves the navigation, as a redirect.
	e := newPageEnv(t)
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return nil, tifa.Redirect("/to", 302) })
	get(e.s, "/news")
	w := getData(e.s, "/news", "b1", "11")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"redirect":"/to"`) || !strings.Contains(w.Body.String(), `"status":302`) || e.loads.Load() != 1 {
		t.Errorf("data: %d %s, %d Loads", w.Code, w.Body.String(), e.loads.Load())
	}
}

func TestResultsThatAreNotKeptDoNotQueue(t *testing.T) {
	e := newPageEnv(t)
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return nil, tifa.ErrNotFound })
	get(e.s, "/news") // not kept: the key is marked
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) {
		started <- struct{}{}
		<-release
		return nil, tifa.ErrNotFound
	})
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() { get(e.s, "/news") })
	}
	for range 3 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("requests wait for each other")
		}
	}
	close(release)
	wg.Wait()
	// Two minutes later the cache is used again.
	e.clock.advance(hitForMiss + time.Second)
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return n, nil })
	get(e.s, "/news")
	if w := get(e.s, "/news"); w.Header().Get("X-Tifa-Render") != "cache" {
		t.Errorf("after the mark: %q", w.Header().Get("X-Tifa-Render"))
	}
}

func TestTrackingParametersShareTheEntry(t *testing.T) {
	e := newPageEnv(t)
	get(e.s, "/news?a=1&b=2")
	for _, target := range []string{"/news?b=2&a=1", "/news?a=1&b=2&utm_source=x&fbclid=y", "/news?utm_medium=z&b=2&a=1&gclid=1"} {
		if w := get(e.s, target); w.Header().Get("X-Tifa-Render") != "cache" {
			t.Errorf("%s: %q", target, w.Header().Get("X-Tifa-Render"))
		}
	}
	if e.loads.Load() != 1 || e.r.count() != 1 {
		t.Errorf("%d Loads, %d renders", e.loads.Load(), e.r.count())
	}
	if got := e.r.inputs[0].URL; got != "/news?a=1&b=2" {
		t.Errorf("rendered for %q", got)
	}
	get(e.s, "/news?a=1&b=3")
	get(e.s, "/news?page=2&utm_source=x")
	if e.loads.Load() != 3 {
		t.Errorf("%d Loads for 3 different pages", e.loads.Load())
	}
}

func TestNavigationDataComesFromTheEntry(t *testing.T) {
	e := newPageEnv(t)
	html := get(e.s, "/news").Body.String()
	signed := signedIn(t, e.s, "Ann")
	for _, cookie := range []string{signed, ""} {
		w := getData(e.s, "/news", "b1", "11", "Cookie", cookie)
		if w.Code != 200 || e.loads.Load() != 1 || w.Header().Get("X-Tifa-Render") != "cache" {
			t.Fatalf("%d %v, %d Loads", w.Code, w.Header(), e.loads.Load())
		}
		routes := routesOf(t, w)
		if routes["app/news"].Data.(map[string]any)["n"].(float64) != 1 || routes["app/layout"].Status != 200 {
			t.Errorf("routes %s", w.Body.String())
		}
		// The data is the data in the HTML.
		if !strings.Contains(html, `{"n":1}`) {
			t.Errorf("html %s", html)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "private, no-cache" {
			t.Errorf("Cache-Control %q", cc)
		}
		m := decodeBody(t, w)
		want := `{"signedIn":false}`
		if cookie != "" {
			want = `{"signedIn":true,"data":{"name":"Ann"}}`
		}
		if string(m["session"]) != want {
			t.Errorf("session %s, want %s", m["session"], want)
		}
	}
	// Only the Loads asked for.
	if w := getData(e.s, "/news", "b1", "01"); len(routesOf(t, w)) != 1 {
		t.Errorf("one layer: %s", w.Body.String())
	}
	if w := getData(e.s, "/news", "b1", "0"); w.Code != 400 {
		t.Errorf("wrong digits: %d", w.Code)
	}
	if w := getData(e.s, "/news", "b0", "11"); w.Code != 409 {
		t.Errorf("wrong build: %d", w.Code)
	}
	// A navigation that comes first makes the entry the page then uses.
	e2 := newPageEnv(t)
	getData(e2.s, "/news", "b1", "11")
	if w := get(e2.s, "/news"); w.Header().Get("X-Tifa-Render") != "cache" || e2.loads.Load() != 1 {
		t.Errorf("page after navigation: %v, %d Loads", w.Header(), e2.loads.Load())
	}
}

func TestNavigationDataOfAFailedPageIsTheError(t *testing.T) {
	e := newPageEnv(t)
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return nil, tifa.ErrNotFound })
	w := getData(e.s, "/news", "b1", "11")
	routes := routesOf(t, w)
	if w.Code != 200 || routes["app/news"].Status != 404 || routes["app/layout"].Status != 200 {
		t.Errorf("%d %s", w.Code, w.Body.String())
	}
	// Only the layer asked for: the layout is not asked for and the page fails.
	if w := getData(e.s, "/news", "b1", "01"); routesOf(t, w)["app/news"].Status != 404 {
		t.Errorf("%s", w.Body.String())
	}
	// A failure of a layer that was not asked for is not the answer of this navigation.
	e2 := newPageEnv(t, func(app *App) {
		app.Layouts[0].Load = func(c *tifa.Ctx) (any, error) { return nil, tifa.ErrNotFound }
	})
	if w := getData(e2.s, "/news", "b1", "01"); w.Code != 200 || routesOf(t, w)["app/news"].Status != 200 {
		t.Errorf("%d %s", w.Code, w.Body.String())
	}
}

// A cache that only knows the URL, as a CDN does, never mixes up the HTML of a page and its data.
func TestAUrlOnlyCacheNeverMixesHTMLAndData(t *testing.T) {
	e := newPageEnv(t)
	type stored struct{ contentType, body string }
	cdn := map[string]stored{}
	fetch := func(target string) stored {
		if v, ok := cdn[target]; ok {
			return v
		}
		w := get(e.s, target)
		v := stored{w.Header().Get("Content-Type"), w.Body.String()}
		if strings.Contains(w.Header().Get("Cache-Control"), "public") {
			cdn[target] = v
		}
		return v
	}
	page := dataURL("/news", "b1", "11")
	for range 3 {
		if v := fetch("/news"); !strings.HasPrefix(v.contentType, "text/html") {
			t.Fatalf("page: %v", v.contentType)
		}
		if v := fetch(page); !strings.HasPrefix(v.contentType, "application/json") {
			t.Fatalf("data: %v", v.contentType)
		}
	}
	if _, ok := cdn[page]; ok {
		t.Error("the navigation data is public")
	}
	if _, ok := cdn["/news"]; !ok {
		t.Error("the page is not public")
	}
}

func TestPageWithoutRenderCacheStillHasATagAndAnEntry(t *testing.T) {
	e := newPageEnv(t)
	first := get(e.s, "/livepage")
	second := get(e.s, "/livepage")
	if first.Header().Get("ETag") == "" || second.Header().Get("X-Tifa-Render") != "cache" || e.r.count() != 1 {
		t.Errorf("%v, %d renders", second.Header(), e.r.count())
	}
	if w := get(e.s, "/livepage", "If-None-Match", first.Header().Get("ETag")); w.Code != 304 {
		t.Errorf("304: %d", w.Code)
	}
}

func TestWholePageCacheFollowsCacheSize(t *testing.T) {
	// With cache.size 0 nothing is kept, and requests that come at the same time still share a render.
	clock := useCacheClock(t)
	_ = clock
	cfg := config.Default()
	cfg.Cache.Size = 0
	app := wireApp()
	var loads atomic.Int64
	release := make(chan struct{})
	app.Routes = append(app.Routes, Route{ID: "app/news", Path: "/news", Page: true, SSR: true, Cache: time.Minute, Layouts: []string{"app/layout"},
		Load: func(c *tifa.Ctx) (any, error) { loads.Add(1); <-release; return 1, nil }})
	s := New(cfg, app.Hooks)
	t.Cleanup(s.Close)
	s.register(app.Layouts, app.Routes)
	if err := s.setFront(dist, (&renders{}).render); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() { get(s, "/news") })
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()
	get(s, "/news")
	if loads.Load() != 2 {
		t.Errorf("%d Loads: 3 at once make one, the next one makes another", loads.Load())
	}
}

// The render cache and the whole-page cache are one LRU: a page in either counts against the size.
func TestWholePagesCountAgainstTheCacheSize(t *testing.T) {
	e := newPageEnv(t)
	get(e.s, "/news")
	if e.s.cache.used <= 0 {
		t.Fatal("nothing counted")
	}
	whole := 0
	e.s.cache.mu.Lock()
	for _, el := range e.s.cache.pages {
		if el.Value.(*cached).page.whole != nil {
			whole++
		}
	}
	e.s.cache.mu.Unlock()
	if whole != 1 {
		t.Errorf("%d whole pages in the cache", whole)
	}
}

func TestSetCookieOfAHitIsNeverSent(t *testing.T) {
	var n atomic.Int64
	e := newPageEnv(t, func(app *App) {
		app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
			c.SetCookie(&http.Cookie{Name: "v", Value: strconv.FormatInt(n.Add(1), 10), Path: "/"})
			err := next(c)
			c.SetCookie(&http.Cookie{Name: "after", Value: "1", Path: "/"})
			return err
		}
	})
	for range 2 {
		if w := get(e.s, "/news"); len(w.Result().Cookies()) != 0 {
			t.Errorf("cookies %v", w.Result().Cookies())
		}
	}
}
