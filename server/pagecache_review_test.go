package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liustack/tifa"
)

type tenantKey struct{}

// Only a short list of request headers reaches the code of a page with Cache. A header a proxy or a
// login gateway sets cannot get to a Load through OnRequest and the context, and what OnRequest puts
// into the context for the Load, such as a value from the path, still can.
func TestPageWithCacheSeesOnlyAFewRequestHeaders(t *testing.T) {
	var mu sync.Mutex
	var seen http.Header
	var viaHeader string
	e := newPageEnv(t, func(app *App) {
		app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
			if c.Pattern() == "/news" {
				mu.Lock()
				seen = c.Request().Header.Clone()
				viaHeader = c.Header("X-User-ID") + c.Header("Accept-Language") + c.Header("User-Agent")
				mu.Unlock()
				return next(c.WithContext(context.WithValue(c.Context(), tenantKey{}, "acme")))
			}
			return next(c)
		}
	})
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) {
		return map[string]any{"tenant": c.Context().Value(tenantKey{})}, nil
	})
	headers := []string{
		"X-User-ID", "42", "X-Forwarded-User", "ann", "Accept-Language", "fr", "User-Agent", "curl", "Referer", "https://x.test/",
		"X-Forwarded-For", "10.0.0.1", "X-Forwarded-Host", "evil.test", "Forwarded", "for=10.0.0.1", "Cookie", "a=b", "Authorization", "Bearer x",
		"Accept-Encoding", "gzip", "If-None-Match", `"x"`, "Origin", "https://x.test", "Sec-Fetch-Site", "same-origin", "Cache-Control", "no-cache",
	}
	for _, target := range []string{"/news", dataURL("/news", "b1", "11")} {
		w := get(e.s, target, headers...)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "acme") {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body.String())
		}
		mu.Lock()
		for _, gone := range []string{"X-User-Id", "X-Forwarded-User", "Accept-Language", "User-Agent", "Referer", "X-Forwarded-For", "X-Forwarded-Host", "Forwarded", "Cookie", "Authorization"} {
			if seen.Get(gone) != "" {
				t.Errorf("%s: OnRequest saw %s", target, gone)
			}
		}
		for _, kept := range []string{"Accept-Encoding", "If-None-Match", "Origin", "Sec-Fetch-Site", "Cache-Control"} {
			if seen.Get(kept) == "" {
				t.Errorf("%s: OnRequest lost %s", target, kept)
			}
		}
		if viaHeader != "" {
			t.Errorf("%s: c.Header gave %q", target, viaHeader)
		}
		mu.Unlock()
	}
}

// The headers a Load or Layout sets go with the entry and come back on a hit, a 304, HEAD and an old page.
func TestHeadersOfLoadsAreReplayed(t *testing.T) {
	e := newPageEnv(t)
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) {
		c.SetHeader("X-Load", "made")
		c.SetHeader("Link", "</a.css>; rel=preload")
		c.SetCookie(&http.Cookie{Name: "load", Value: "1"})
		c.SetHeader("Cache-Control", "public, max-age=5")
		return n, nil
	})
	check := func(what string, w *httptest.ResponseRecorder) {
		t.Helper()
		if w.Header().Get("X-Load") != "made" || w.Header().Get("Link") != "</a.css>; rel=preload" {
			t.Errorf("%s: headers %v", what, w.Header())
		}
		if len(w.Result().Cookies()) != 0 || !strings.HasPrefix(w.Header().Get("Cache-Control"), "public, max-age=0, s-maxage=60") {
			t.Errorf("%s: the framework owns cookies and Cache-Control: %v", what, w.Header())
		}
	}
	first := get(e.s, "/news")
	check("miss", first)
	check("hit", get(e.s, "/news"))
	check("304", get(e.s, "/news", "If-None-Match", first.Header().Get("ETag")))
	head := httptest.NewRecorder()
	e.s.ServeHTTP(head, httptest.NewRequest("HEAD", "/news", nil))
	check("HEAD", head)
	e.clock.advance(61 * time.Second)
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return nil, context.DeadlineExceeded })
	stale := get(e.s, "/news")
	if stale.Header().Get("Cache-Status") != "tifa; fwd=stale" {
		t.Fatalf("not stale: %v", stale.Header())
	}
	check("stale", stale)
	if e.loads.Load() != 2 {
		t.Errorf("%d Loads", e.loads.Load())
	}
}

// A Load that says its answer is not for everyone makes the answer say so, whatever OnRequest set
// before and whichever way the page is answered.
func TestUnsharedHeadersOfLoadsReachTheAnswer(t *testing.T) {
	for name, tc := range map[string]struct {
		load func(c *tifa.Ctx, n int64) (any, error)
		code int
	}{
		"private": {func(c *tifa.Ctx, n int64) (any, error) { c.SetHeader("Cache-Control", "private"); return n, nil }, 200},
		"Vary":    {func(c *tifa.Ctx, n int64) (any, error) { c.SetHeader("Vary", "X-Tenant"); return n, nil }, 200},
		"no-store on a 404": {func(c *tifa.Ctx, n int64) (any, error) {
			c.SetHeader("Cache-Control", "no-store")
			return nil, tifa.ErrNotFound
		}, 404},
		"private on a redirect": {func(c *tifa.Ctx, n int64) (any, error) {
			c.SetHeader("Cache-Control", "private")
			return nil, c.Redirect("/to", 302)
		}, 302},
	} {
		t.Run(name, func(t *testing.T) {
			e := newPageEnv(t, func(app *App) {
				app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
					c.SetHeader("Cache-Control", "public, max-age=100")
					return next(c)
				}
			})
			e.setLoad(tc.load)
			for i := range 3 { // the first request makes the page, the next ones skip the cache
				w := get(e.s, "/news")
				cc, vary := w.Header().Get("Cache-Control"), strings.Join(w.Header().Values("Vary"), ",")
				if w.Code != tc.code || (name == "Vary" && !strings.Contains(vary, "X-Tenant")) ||
					(name != "Vary" && (strings.Contains(cc, "public") || !strings.Contains(cc, "private") && !strings.Contains(cc, "no-store"))) {
					t.Errorf("request %d: %d Cache-Control %q Vary %q", i, w.Code, cc, vary)
				}
			}
			head := httptest.NewRecorder()
			e.s.ServeHTTP(head, httptest.NewRequest("HEAD", "/news", nil))
			if name != "Vary" && strings.Contains(head.Header().Get("Cache-Control"), "public") {
				t.Errorf("HEAD: %q", head.Header().Get("Cache-Control"))
			}
			if e.loads.Load() != 4 {
				t.Errorf("%d Loads for 4 requests that cannot be shared", e.loads.Load())
			}
		})
	}
}

// Requests that wait for a make whose answer turns out not to be for everyone make their own, and a
// request whose OnRequest already made the answer not for everyone does not join one.
func TestUnsharedResultsAreNotGivenToWaiters(t *testing.T) {
	e := newPageEnv(t)
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) {
		started <- struct{}{}
		<-release
		c.SetHeader("Vary", "X-Tenant")
		c.SetHeader("X-Id", strings.Repeat("i", int(n)))
		return n, nil
	})
	var wg sync.WaitGroup
	ids := make([]string, 3)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids[i] = get(e.s, "/news").Header().Get("X-Id")
		}()
	}
	<-started
	time.Sleep(100 * time.Millisecond) // the others are waiting for the make
	close(release)
	wg.Wait()
	if e.loads.Load() != 3 {
		t.Fatalf("%d Loads for 3 requests", e.loads.Load())
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if len(seen) != 3 || seen[""] {
		t.Errorf("the requests did not get answers of their own: %q", ids)
	}

	// A request that says, in OnRequest, that its answer is not for everyone joins nothing.
	e2 := newPageEnv(t, func(app *App) {
		app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
			if c.Header("Origin") != "" {
				c.SetHeader("Vary", "Origin")
			}
			return next(c)
		}
	})
	gate := make(chan struct{})
	var inLoad atomic.Int64
	e2.setLoad(func(c *tifa.Ctx, n int64) (any, error) {
		inLoad.Add(1)
		<-gate
		return n, nil
	})
	var wg2 sync.WaitGroup
	for _, origin := range []string{"", "https://a.test"} {
		wg2.Add(1)
		go func() { defer wg2.Done(); get(e2.s, "/news", "Origin", origin) }()
		time.Sleep(50 * time.Millisecond)
	}
	for deadline := time.Now().Add(2 * time.Second); inLoad.Load() < 2 && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
	}
	close(gate)
	wg2.Wait()
	if inLoad.Load() != 2 {
		t.Errorf("%d Loads, the request with its own Vary should not have shared the make", inLoad.Load())
	}
	// And it did not store: the plain request's page is the one in the cache.
	if w := get(e2.s, "/news"); w.Header().Get("X-Tifa-Render") != "cache" {
		t.Errorf("%v", w.Header())
	}
}

// A make that every request gave up on, and a newer one replaced, must not write or delete what the
// newer one decided.
func TestOldMakeDoesNotOverwriteANewerOne(t *testing.T) {
	e := newPageEnv(t)
	var calls atomic.Int64
	slowStarted, release := make(chan struct{}), make(chan struct{})
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) {
		if calls.Add(1) == 1 {
			close(slowStarted)
			<-release
			return n, nil // A: a good page, late
		}
		return nil, tifa.ErrNotFound // B: the page is gone
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = recover() }() // the connection of a client that went away is cut with a panic
		r := httptest.NewRequest("GET", "/news", nil).WithContext(ctx)
		e.s.ServeHTTP(httptest.NewRecorder(), r)
	}()
	<-slowStarted
	cancel()
	<-done
	if w := get(e.s, "/news"); w.Code != 404 {
		t.Fatalf("B: %d", w.Code)
	}
	close(release)
	// Let A finish. It ran with a canceled flight and no longer belongs to the key.
	time.Sleep(200 * time.Millisecond)
	e.s.cache.mu.Lock()
	var whole int
	for _, el := range e.s.cache.pages {
		if el.Value.(*cached).page.whole != nil {
			whole++
		}
	}
	bypassed := len(e.s.cache.bypass)
	e.s.cache.mu.Unlock()
	if whole != 0 {
		t.Errorf("the late page came back: %d whole pages in the cache", whole)
	}
	if bypassed != 1 {
		t.Errorf("the mark of B was lost: %d", bypassed)
	}
}

// The HTML and compressed forms that the render cache and the whole-page cache share are counted once.
func TestSharedBodyIsCountedOnce(t *testing.T) {
	e := newPageEnv(t)
	get(e.s, "/news")
	c := e.s.cache
	c.mu.Lock()
	defer c.mu.Unlock()
	var body, meta int64
	var whole, render *cachedPage
	for _, el := range c.pages {
		p := el.Value.(*cached).page
		if p.whole != nil {
			whole = &p
			meta = p.whole.size()
		} else {
			render = &p
		}
	}
	if whole == nil || render == nil {
		t.Fatalf("pages: whole %v, render %v", whole != nil, render != nil)
	}
	body = render.bodySize()
	if body == 0 || whole.body != render.body {
		t.Fatalf("body %d, shared %v", body, whole.body == render.body)
	}
	if c.used != body+meta {
		t.Errorf("used %d, want the body %d once and the metadata %d", c.used, body, meta)
	}
	// The body stays counted while one of the two pages is in the cache, and goes with the last.
	for key, el := range c.pages {
		if el.Value.(*cached).page.whole != nil {
			c.removeLocked(key)
		}
	}
	if c.used != body {
		t.Errorf("after the whole page left: %d, want %d", c.used, body)
	}
	for key := range c.pages {
		c.removeLocked(key)
	}
	if c.used != 0 || len(c.bodies) != 0 {
		t.Errorf("empty cache: used %d, %d bodies", c.used, len(c.bodies))
	}
}

// Pages with different HTML are counted apart: each render has a body of its own, however Go places
// small allocations, and the cache evicts when they add up.
func TestBodiesOfDifferentRendersAreCountedApart(t *testing.T) {
	c := newHTMLCache(25)
	bodies := map[*bodyID]bool{}
	for _, key := range []string{"a", "b", "c"} {
		b := new(bodyID)
		if bodies[b] {
			t.Fatal("two renders got the same body identity")
		}
		bodies[b] = true
		c.put(key, cachedPage{html: strings.Repeat(key, 10), body: b})
	}
	if c.used != 20 || len(c.bodies) != 2 {
		t.Errorf("used %d with %d bodies, want 20 and 2", c.used, len(c.bodies))
	}
	if _, ok := c.get("a"); ok {
		t.Error("the oldest page was not evicted")
	}
}

// The Cache-Control of OnRequest and of the Load and Layout functions merge by strictness, whichever
// came first, and Vary is the union.
func TestCacheControlsMergeByStrictness(t *testing.T) {
	for name, tc := range map[string]struct {
		hook, load http.Header
		cc         string
		vary       []string
	}{
		"hook public and Vary, Load no-store": {
			http.Header{"Cache-Control": {"public, max-age=60"}, "Vary": {"Accept-Encoding"}},
			http.Header{"Cache-Control": {"no-store"}}, "no-store", []string{"Accept-Encoding"}},
		"hook private, Load no-store": {
			http.Header{"Cache-Control": {"private"}}, http.Header{"Cache-Control": {"no-store"}}, "no-store", nil},
		"hook public, Load no-store and Vary": {
			http.Header{"Cache-Control": {"public"}, "Vary": {"Accept-Encoding"}},
			http.Header{"Cache-Control": {"no-store"}, "Vary": {"X-Tenant"}}, "no-store", []string{"Accept-Encoding", "X-Tenant"}},
		"hook no-store, Load private": {
			http.Header{"Cache-Control": {"no-store"}}, http.Header{"Cache-Control": {"private"}}, "no-store", nil},
		"private with no-cache beats private": {
			http.Header{"Cache-Control": {"private"}}, http.Header{"Cache-Control": {"private, no-cache"}}, "private, no-cache", nil},
		"hook no-cache, Load private": {
			http.Header{"Cache-Control": {"no-cache"}}, http.Header{"Cache-Control": {"private"}}, "private, no-cache", nil},
		// max-age stays: it is a limit of its own, and with private and no-cache nothing shared uses it.
		"hook private and max-age, Load no-cache": {
			http.Header{"Cache-Control": {"private, max-age=3600"}}, http.Header{"Cache-Control": {"no-cache"}}, "private, no-cache, max-age=3600", nil},
		"private with fields and bare private": {
			http.Header{"Cache-Control": {`private="X-A"`}}, http.Header{"Cache-Control": {"private"}}, "private", nil},
		"private with fields on both sides": {
			http.Header{"Cache-Control": {`private="X-A"`}}, http.Header{"Cache-Control": {`private="X-B"`}}, `private="X-A, X-B"`, nil},
		"smaller max-age and union of the rest": {
			http.Header{"Cache-Control": {"max-age=100, must-revalidate, s-maxage=50"}}, http.Header{"Cache-Control": {"max-age=10, no-transform, s-maxage=90"}},
			"must-revalidate, no-transform, max-age=10, s-maxage=50", nil},
		"a field that cannot be read": {
			http.Header{"Cache-Control": {"public"}}, http.Header{"Cache-Control": {`private="unclosed`}}, "no-store", nil},
		"a hook field that cannot be read": {
			http.Header{"Cache-Control": {`no-cache="x`}}, http.Header{"Cache-Control": {"private"}}, "no-store", nil},
		"public stays when the Load only varies": {
			http.Header{"Cache-Control": {"public"}}, http.Header{"Vary": {"X-Tenant"}}, "public", []string{"X-Tenant"}},
	} {
		t.Run(name, func(t *testing.T) {
			for range 30 { // the order of a map changes from run to run
				h := tc.hook.Clone()
				replayHeaders(h, tc.load.Clone())
				if got := h.Get("Cache-Control"); got != tc.cc {
					t.Fatalf("Cache-Control %q, want %q", got, tc.cc)
				}
				if got := h.Values("Vary"); !slices.Equal(got, tc.vary) {
					t.Fatalf("Vary %v, want %v", got, tc.vary)
				}
			}
		})
	}
}

// Headers of one connection are not kept or sent again from an entry, and others are.
func TestHopByHopHeadersAreNotReplayed(t *testing.T) {
	e := newPageEnv(t)
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) {
		c.SetHeader("Connection", "X-Hop, close")
		c.SetHeader("X-Hop", "1")
		c.SetHeader("Keep-Alive", "timeout=5")
		c.SetHeader("Upgrade", "websocket")
		c.SetHeader("Transfer-Encoding", "chunked")
		c.SetHeader("Proxy-Authenticate", "Basic")
		c.SetHeader("Proxy-Connection", "keep-alive")
		c.SetHeader("Proxy-Status", "tifa; error=none")
		c.SetHeader("TE", "trailers")
		c.SetHeader("X-Keep", "yes")
		return n, nil
	})
	get(e.s, "/news")
	w := get(e.s, "/news")
	for _, name := range []string{"Connection", "X-Hop", "Keep-Alive", "Upgrade", "Transfer-Encoding", "Proxy-Authenticate", "Te"} {
		if w.Header().Get(name) != "" {
			t.Errorf("%s was replayed: %q", name, w.Header().Get(name))
		}
	}
	if w.Header().Get("Proxy-Status") != "tifa; error=none" || w.Header().Get("Proxy-Connection") != "" {
		t.Errorf("Proxy-Status must be replayed and Proxy-Connection not: %v", w.Header())
	}
	if w.Header().Get("X-Keep") != "yes" {
		t.Errorf("X-Keep: %v", w.Header())
	}
}

// The headers an entry keeps count toward its size.
func TestKeptHeadersCountTowardTheSize(t *testing.T) {
	a := &wholePage{}
	b := &wholePage{headers: http.Header{"X-Long": {strings.Repeat("v", 1000)}}}
	if b.size()-a.size() < 1000 {
		t.Errorf("sizes %d and %d", a.size(), b.size())
	}
}

// This documents a boundary and does not promise more. The framework drops the request headers that
// identify a visitor, but a net/http middleware set with tifa.SetHTTPHandler runs outside tifa, and
// what it puts into the context of the request reaches OnRequest and the Load of a page with Cache.
// Do not recognize visitors there: do it in OnRequest, where the request is anonymous.
func TestContextValuesOfAnOuterMiddlewareReachThePageWithCache(t *testing.T) {
	e := newPageEnv(t)
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) {
		return map[string]any{"who": c.Context().Value(tenantKey{})}, nil
	})
	outer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.s.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tenantKey{}, "from-the-outer-middleware")))
	})
	w := httptest.NewRecorder()
	outer.ServeHTTP(w, httptest.NewRequest("GET", "/news", nil))
	if !strings.Contains(w.Body.String(), "from-the-outer-middleware") {
		t.Errorf("the value did not arrive: %s", w.Body.String())
	}
}
