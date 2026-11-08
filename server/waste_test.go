package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
)

// The tests here count what a request costs: the Loads it runs, the renders in the VM, the JSON
// encodings of Load data and the gzip compressions of pages for the cache.

// countedValue counts how often JSON writes it.
type countedValue struct {
	n *atomic.Int64
	v int64
}

func (c countedValue) MarshalJSON() ([]byte, error) {
	c.n.Add(1)
	return []byte(fmt.Sprint(c.v)), nil
}

// workEnv is an app whose pages sit under a root Layout with a Load. version is the data of every
// Load that returns data, so a test changes the data by moving it on. failWith, by path, is the
// error the Load of that page returns. redirects, by path, makes the render redirect.
type workEnv struct {
	s        *server
	r        *renders
	loads    atomic.Int64
	encodes  atomic.Int64
	compress *atomic.Int64
	onError  atomic.Int64
	version  atomic.Int64
	mu       sync.Mutex
	failWith map[string]error
	redirect map[string]bool
}

// wastePadding makes a page large enough to be compressed.
var wastePadding = strings.Repeat("<p>padding text</p>", 200)

func newWorkEnv(t *testing.T, cacheSize int64) *workEnv {
	t.Helper()
	e := &workEnv{failWith: map[string]error{}, redirect: map[string]bool{}}
	load := func(path string) func(c *tifa.Ctx) (any, error) {
		return func(c *tifa.Ctx) (any, error) {
			e.loads.Add(1)
			e.mu.Lock()
			err := e.failWith[path]
			e.mu.Unlock()
			if err != nil {
				return nil, err
			}
			return map[string]any{"v": countedValue{&e.encodes, e.version.Load()}}, nil
		}
	}
	page := func(path string, change func(*Route)) Route {
		r := Route{ID: "app" + path, Path: path, Page: true, SSR: true, Layouts: []string{"app/layout"}, Load: load(path)}
		if change != nil {
			change(&r)
		}
		return r
	}
	app := App{
		Hooks: Hooks{OnError: func(c *tifa.Ctx, err error) tifa.Error {
			e.onError.Add(1)
			return tifa.Error{Status: 500, Message: "Internal Server Error"}
		}},
		Layouts: []Layout{{ID: "app/layout", Path: "/", SSR: true, Load: load("layout")}},
		Routes: []Route{
			page("/plain", nil),
			page("/live", func(r *Route) { r.NoRenderCache = true }),
			page("/cached", func(r *Route) { r.Cache = time.Minute }),
		},
	}
	cfg := config.Default()
	cfg.Cache.Size = cacheSize
	s := newServer(cfg, app.Hooks)
	t.Cleanup(s.close)
	s.register(app.Layouts, app.Routes)
	e.r = &renders{result: func(in ssr.Input) (string, error) {
		e.mu.Lock()
		redirect := e.redirect[strings.SplitN(in.URL, "?", 2)[0]]
		e.mu.Unlock()
		if redirect {
			return "", &ssr.RedirectError{Location: "/elsewhere", Status: http.StatusTemporaryRedirect}
		}
		return "<html>" + in.URL + " " + string(in.Data) + " " + strings.Join(in.Texts, " ") + wastePadding + "</html>", nil
	}}
	if err := s.setFront(dist, e.r.render); err != nil {
		t.Fatal(err)
	}
	e.compress = countCompressions(t)
	e.s = s
	return e
}

func (e *workEnv) fail(path string, err error) {
	e.mu.Lock()
	e.failWith[path] = err
	e.mu.Unlock()
}

func (e *workEnv) redirects(path string) {
	e.mu.Lock()
	e.redirect[path] = true
	e.mu.Unlock()
}

// work is what one request cost and what it answered.
type work struct {
	w                                         *httptest.ResponseRecorder
	loads, renders, encodes, compress, errors int64
}

func (k work) String() string {
	return fmt.Sprintf("%d, %d Loads, %d renders, %d encodings, %d compressions, X-Tifa-Render %q", k.w.Code, k.loads, k.renders, k.encodes, k.compress, k.w.Header().Get(renderHeader))
}

func (e *workEnv) do(method, target string, header ...string) work {
	l, r, j, c, o := e.loads.Load(), e.r.count(), e.encodes.Load(), e.compress.Load(), e.onError.Load()
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("Accept-Encoding", chrome)
	for i := 0; i < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	w := httptest.NewRecorder()
	e.s.ServeHTTP(w, req)
	return work{w: w, loads: e.loads.Load() - l, renders: int64(e.r.count() - r), encodes: e.encodes.Load() - j, compress: e.compress.Load() - c, errors: e.onError.Load() - o}
}

// A 404 page and an error page from a failed Load are a function of their render input as every
// page is, so the render cache keeps them. They carry no ETag, and a request with If-None-Match gets
// the page, since a precondition only applies to an answer that would be 2xx (RFC 9110 13.2.1).
// OnError still sees every failure.
func TestErrorPagesComeFromTheRenderCache(t *testing.T) {
	e := newWorkEnv(t, 64<<20)
	e.fail("/plain", tifa.ErrNotFound)
	e.fail("/live", tifa.ErrNotFound)
	for _, tc := range []struct {
		name, target string
		status       int
		errors       int64
	}{
		{"a Load returns 404", "/plain", 404, 0},
		{"no route matches", "/nope", 404, 0},
	} {
		first, second := e.do("GET", tc.target), e.do("GET", tc.target)
		if first.w.Code != tc.status || first.renders != 1 || first.w.Header().Get(renderHeader) != "vm" {
			t.Errorf("%s: first %s", tc.name, first)
		}
		if second.w.Code != tc.status || second.renders != 0 || second.w.Header().Get(renderHeader) != "cache" || second.w.Body.String() != first.w.Body.String() {
			t.Errorf("%s: second %s", tc.name, second)
		}
		if etag := first.w.Header().Get("ETag") + second.w.Header().Get("ETag"); etag != "" {
			t.Errorf("%s: ETag %q", tc.name, etag)
		}
		if w := e.do("GET", tc.target, "If-None-Match", "*"); w.w.Code != tc.status || w.renders != 0 || w.w.Body.Len() == 0 {
			t.Errorf("%s: with If-None-Match: %s", tc.name, w)
		}
		if w := e.do("HEAD", tc.target); w.w.Code != tc.status || w.renders != 0 {
			t.Errorf("%s: HEAD %s", tc.name, w)
		}
	}
	// An unexpected error: OnError runs each time, the page renders once.
	e.fail("/plain", fmt.Errorf("db down"))
	first, second := e.do("GET", "/plain"), e.do("GET", "/plain")
	if first.w.Code != 500 || first.renders != 1 || first.errors != 1 || second.w.Code != 500 || second.renders != 0 || second.errors != 1 || second.w.Body.String() != first.w.Body.String() {
		t.Errorf("Load fails: %s, OnError %d, then %s, OnError %d", first, first.errors, second, second.errors)
	}
	// What the page shows of the error is in the key: another status or message is another page.
	e.fail("/plain", tifa.Error{Status: 503, Message: "come back later"})
	if w := e.do("GET", "/plain", "Accept-Encoding", "identity"); w.w.Code != 503 || w.renders != 1 || !strings.Contains(w.w.Body.String(), "come back later") {
		t.Errorf("another error: %s", w)
	}
	e.fail("/plain", tifa.Error{Status: 503, Message: "come back tomorrow"})
	if w := e.do("GET", "/plain", "Accept-Encoding", "identity"); w.w.Code != 503 || w.renders != 1 || !strings.Contains(w.w.Body.String(), "come back tomorrow") {
		t.Errorf("another message: %s", w)
	}
	// A page with Cache has the 404 page of the render cache like any page.
	e.fail("/cached", tifa.ErrNotFound)
	first, second = e.do("GET", "/cached"), e.do("GET", "/cached")
	if first.w.Code != 404 || first.renders != 1 || second.w.Code != 404 || second.renders != 0 {
		t.Errorf("404 of a page with Cache: %s, then %s", first, second)
	}
	// RenderCache = false keeps its 404 page out of the cache too.
	for range 2 {
		if w := e.do("GET", "/live"); w.w.Code != 404 || w.renders != 1 || w.w.Header().Get(renderHeader) != "vm" {
			t.Errorf("404 of a page with RenderCache = false: %s", w)
		}
	}
}

// A file under /assets/ that the build does not have, such as a chunk of an old build that a tab
// opened before a deploy asks for, is a plain 404: no Load, no render.
func TestMissingAssetIsAPlainNotFound(t *testing.T) {
	e := newWorkEnv(t, 64<<20)
	for _, method := range []string{"GET", "HEAD"} {
		w := e.do(method, "/assets/old-123.js")
		if w.w.Code != 404 || w.loads != 0 || w.renders != 0 || !strings.HasPrefix(w.w.Header().Get("Content-Type"), "text/plain") {
			t.Errorf("%s: %s, Content-Type %q", method, w, w.w.Header().Get("Content-Type"))
		}
		if cc := w.w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: Cache-Control %q", method, cc)
		}
	}
	// The files of the build and a public file under /assets/ are still served.
	for _, target := range []string{"/assets/app-1.js", "/assets/logo.svg"} {
		if w := e.do("GET", target); w.w.Code != 200 {
			t.Errorf("%s: %s", target, w)
		}
	}
}

// A redirect of the router during the render is a result of the render input like a page, so the
// render cache keeps it.
func TestRenderRedirectComesFromTheRenderCache(t *testing.T) {
	e := newWorkEnv(t, 64<<20)
	e.redirects("/plain")
	first, second := e.do("GET", "/plain"), e.do("GET", "/plain")
	for i, w := range []work{first, second} {
		if w.w.Code != 307 || w.w.Header().Get("Location") != "/elsewhere" || w.w.Header().Get("ETag") != "" {
			t.Errorf("answer %d: %s, Location %q, ETag %q", i, w, w.w.Header().Get("Location"), w.w.Header().Get("ETag"))
		}
	}
	if first.renders != 1 || second.renders != 0 {
		t.Errorf("renders: %d then %d", first.renders, second.renders)
	}
	// A page with Cache redirects like any page: the render cache answers every request.
	e.redirects("/cached")
	var renders int64
	for _, target := range []string{"/cached", "/cached", dataURL("/cached", "b1", "01"), "/cached"} {
		w := e.do("GET", target)
		renders += w.renders
		if target == "/cached" && (w.w.Code != 307 || w.w.Header().Get("Location") != "/elsewhere") {
			t.Errorf("GET %s: %s", target, w)
		}
	}
	if renders != 1 {
		t.Errorf("a page with Cache that redirects rendered %d times", renders)
	}
}

// The key of a render hashes the input as it is: it copies none of the data, however long.
func TestRenderKeyCopiesNothing(t *testing.T) {
	long := strings.Repeat("x", 100_000)
	in := ssr.Input{BuildID: "b1", URL: "/table", Data: []byte(`{"app/table/route":{"status":200,"text":0}}` + long), Texts: []string{long, long}, Session: []byte(long), Language: "en", Alternates: []byte(long)}
	renderKey(in, "bundle", "policy")
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 10 {
		renderKey(in, "bundle", "policy")
	}
	runtime.ReadMemStats(&after)
	// Other tests that run meanwhile may allocate too, hence the margin: a copy is 100 KB.
	if perCall := (after.TotalAlloc - before.TotalAlloc) / 10; perCall > 8<<10 {
		t.Errorf("renderKey allocates %d bytes for 500 KB of input", perCall)
	}
}

// A page larger than the cache is compressed once, and a client that takes gzip gets that form.
func TestPageLargerThanTheCacheIsCompressedOnce(t *testing.T) {
	e := newWorkEnv(t, 100) // the page gzips to a few hundred bytes
	for range 2 {
		w := e.do("GET", "/plain")
		if w.w.Code != 200 || w.renders != 1 || w.compress != 1 || w.w.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("%s, Content-Encoding %q", w, w.w.Header().Get("Content-Encoding"))
		}
		// Compressing the HTML as it goes out gives the same bytes, without a length.
		if html := e.r.inputs[len(e.r.inputs)-1]; w.w.Body.String() != string(compressPage(e.lastHTML(t, html))) || w.w.Header().Get("Content-Length") != fmt.Sprint(w.w.Body.Len()) {
			t.Errorf("the answer is not the gzip form the render made: Content-Length %q", w.w.Header().Get("Content-Length"))
		}
	}
	// A client that takes no gzip gets the HTML.
	if w := e.do("GET", "/plain", "Accept-Encoding", "identity"); w.w.Header().Get("Content-Encoding") != "" || !strings.HasPrefix(w.w.Body.String(), "<html>") {
		t.Errorf("identity: %s, %q", w, w.w.Header().Get("Content-Encoding"))
	}
}

// lastHTML renders in again with the fake bundle, for a test to compare.
func (e *workEnv) lastHTML(t *testing.T, in ssr.Input) string {
	t.Helper()
	html, err := e.r.result(in)
	if err != nil {
		t.Fatal(err)
	}
	return html
}
