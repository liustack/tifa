package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
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

// heldRender is a fake SSR bundle whose renders wait until the test opens it, so the test can line up
// requests behind a render without timing them.
type heldRender struct {
	count  atomic.Int32
	open   chan struct{}
	result func(ctx context.Context, in ssr.Input) (string, error)
}

func newHeldRender() *heldRender { return &heldRender{open: make(chan struct{})} }

func (g *heldRender) render(ctx context.Context, in ssr.Input) (string, error) {
	g.count.Add(1)
	<-g.open
	if g.result != nil {
		return g.result(ctx, in)
	}
	return bigPage(in)
}

func (g *heldRender) renders() int { return int(g.count.Load()) }

func newHeldServer(t *testing.T, cfg config.Config, app App) (*Server, *heldRender) {
	t.Helper()
	s := New(cfg, app.Hooks)
	s.register(app.Layouts, app.Routes)
	g := newHeldRender()
	if err := s.setFront(dist, g.render); err != nil {
		t.Fatal(err)
	}
	return s, g
}

// waiting is the number of requests that wait for a render of the HTML cache.
func waiting(s *Server) int {
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	n := 0
	for _, f := range s.cache.flights {
		n += f.waiters
	}
	return n
}

// until fails the test when cond does not hold within 10 seconds.
func until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !cond(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// burst sends n requests for path at once, the headers of request i from header, and opens the
// heldRender once every request renders or waits for a render. It returns the answers in order.
func burst(t *testing.T, s *Server, g *heldRender, n int, path string, header func(i int) []string) []*httptest.ResponseRecorder {
	t.Helper()
	out := make([]*httptest.ResponseRecorder, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var h []string
			if header != nil {
				h = header(i)
			}
			out[i] = get(s, path, h...)
		}()
	}
	until(t, "the requests to reach the render", func() bool { return g.renders() == n || waiting(s) == n })
	close(g.open)
	wg.Wait()
	return out
}

func renderedBy(out []*httptest.ResponseRecorder) map[string]int {
	by := map[string]int{}
	for _, w := range out {
		by[w.Header().Get("X-Tifa-Render")]++
	}
	return by
}

// Requests for a Cache = true page that miss the cache at the same time wait for one render.
func TestBurstOfACachedPageRendersOnce(t *testing.T) {
	s, g := newHeldServer(t, config.Default(), frontApp())
	out := burst(t, s, g, 20, "/stats", nil)
	if g.renders() != 1 {
		t.Fatalf("%d renders for 20 requests", g.renders())
	}
	for i, w := range out {
		if w.Code != 200 || w.Body.String() != out[0].Body.String() || w.Header().Get("ETag") != out[0].Header().Get("ETag") {
			t.Errorf("request %d: %d %v", i, w.Code, w.Header())
		}
	}
	if by := renderedBy(out); by["vm"] != 1 || by["shared"] != 19 {
		t.Errorf("rendered by %v", by)
	}
	if w := get(s, "/stats"); w.Header().Get("X-Tifa-Render") != "cache" || g.renders() != 1 {
		t.Errorf("after the burst: %d renders, %v", g.renders(), w.Header())
	}
}

// Requests whose Loads return other data are answered with their own renders.
func TestBurstWithOtherDataRendersEach(t *testing.T) {
	app := frontApp()
	app.Routes = append(app.Routes, Route{ID: "app/report", Path: "/report", Page: true, SSR: true, Cache: true,
		Layouts: []string{"app/layout"}, Load: func(c *tifa.Ctx) (any, error) { return c.Header("X-Group"), nil }})
	s, g := newHeldServer(t, config.Default(), app)
	g.result = func(_ context.Context, in ssr.Input) (string, error) {
		return "<html>" + string(in.Data) + "</html>", nil
	}
	group := func(i int) string { return fmt.Sprintf("group-%d", i%2) }
	out := burst(t, s, g, 20, "/report", func(i int) []string { return []string{"X-Group", group(i)} })
	if g.renders() != 2 {
		t.Errorf("%d renders for two kinds of data", g.renders())
	}
	for i, w := range out {
		if w.Code != 200 || !strings.Contains(w.Body.String(), group(i)) || strings.Contains(w.Body.String(), group(i+1)) {
			t.Errorf("request %d of %s: %d %s", i, group(i), w.Code, w.Body.String())
		}
	}
}

// Requests for a page without Cache each render, since its HTML may differ by user.
func TestBurstOfAnUncachedPageRendersEach(t *testing.T) {
	s, g := newHeldServer(t, config.Default(), frontApp())
	out := burst(t, s, g, 10, "/projects/p1", nil)
	if g.renders() != 10 {
		t.Errorf("%d renders for 10 requests", g.renders())
	}
	if by := renderedBy(out); by["vm"] != 10 {
		t.Errorf("rendered by %v", by)
	}
}

// Requests that share a render keep the headers OnRequest gave each of them.
func TestBurstKeepsTheHeadersOfEachRequest(t *testing.T) {
	app := frontApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx) error {
		c.Writer().Header().Add("Set-Cookie", "visitor="+c.Header("X-Visitor"))
		c.Writer().Header().Add("Vary", "Cookie")
		c.SetHeader("X-Visitor", c.Header("X-Visitor"))
		return c.Next()
	}
	s, g := newHeldServer(t, config.Default(), app)
	visitor := func(i int) string { return fmt.Sprintf("v%d", i) }
	out := burst(t, s, g, 20, "/stats", func(i int) []string { return []string{"X-Visitor", visitor(i)} })
	if g.renders() != 1 {
		t.Errorf("%d renders", g.renders())
	}
	for i, w := range out {
		h := w.Header()
		if cookies := h.Values("Set-Cookie"); len(cookies) != 1 || cookies[0] != "visitor="+visitor(i) || h.Get("X-Visitor") != visitor(i) {
			t.Errorf("request %d: %v", i, h)
		}
		if !varies(w, "Cookie") || !varies(w, "X-Tifa-Route") || !varies(w, "Accept-Encoding") {
			t.Errorf("request %d: Vary %v", i, h.Values("Vary"))
		}
	}
}

// Requests that share a render take the encoding each of them accepts.
func TestBurstSendsEachRequestItsEncoding(t *testing.T) {
	s, g := newHeldServer(t, config.Default(), frontApp())
	accepts := []string{"", "gzip", "zstd", "br", "gzip, zstd"}
	wants := []string{"", "gzip", "zstd", "", "zstd"}
	out := burst(t, s, g, 25, "/stats", func(i int) []string { return []string{"Accept-Encoding", accepts[i%len(accepts)]} })
	if g.renders() != 1 {
		t.Errorf("%d renders", g.renders())
	}
	page, _ := bigPage(ssr.Input{URL: "/stats"})
	for i, w := range out {
		if w.Header().Get("Content-Encoding") != wants[i%len(wants)] || decoded(t, w) != page || !strings.HasPrefix(w.Header().Get("ETag"), `W/"`) {
			t.Errorf("request %d with %q: %v", i, accepts[i%len(accepts)], w.Header())
		}
	}
}

// A failed render is the answer of every request that waited for it, which each report to
// OnError. It is not cached, so the next request renders again.
func TestBurstSharesAFailedRender(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result func(context.Context, ssr.Input) (string, error)
		want   string
	}{
		{"error", func(context.Context, ssr.Input) (string, error) {
			return "", errors.New("Error: a component threw")
		}, "a component threw"},
		{"ssr.timeout", func(context.Context, ssr.Input) (string, error) {
			return "", fmt.Errorf("ssr: render took longer than 1s: %w", context.DeadlineExceeded)
		}, "render took longer"},
		{"panic", func(context.Context, ssr.Input) (string, error) {
			panic("the engine broke")
		}, "the engine broke"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var reported []error
			app := frontApp()
			app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error {
				mu.Lock()
				reported = append(reported, err)
				mu.Unlock()
				return tifa.Error{Status: 500}
			}
			s, g := newHeldServer(t, config.Default(), app)
			g.result = tc.result
			out := burst(t, s, g, 10, "/stats", nil)
			for i, w := range out {
				if w.Code != 500 || w.Header().Get("X-Tifa-Render") != "" {
					t.Errorf("request %d: %d %v", i, w.Code, w.Header())
				}
			}
			if g.renders() != 1 || len(reported) != 10 {
				t.Fatalf("%d renders, %d errors reported", g.renders(), len(reported))
			}
			for _, err := range reported {
				if !strings.Contains(err.Error(), tc.want) {
					t.Errorf("OnError got %v", err)
				}
			}
			g.result = nil
			if w := get(s, "/stats"); w.Code != 200 || w.Header().Get("X-Tifa-Render") != "vm" || g.renders() != 2 {
				t.Errorf("after the failure: %d renders, %d %v", g.renders(), w.Code, w.Header())
			}
		})
	}
}

// A panic of a render keeps its stack, so OnError shows where it happened.
func TestBurstKeepsTheStackOfAPanic(t *testing.T) {
	var reported error
	app := frontApp()
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error { reported = err; return tifa.Error{Status: 500} }
	s, g := newHeldServer(t, config.Default(), app)
	g.result = func(context.Context, ssr.Input) (string, error) { panic("the engine broke") }
	burst(t, s, g, 1, "/stats", nil)
	if reported == nil || !strings.Contains(reported.Error(), "tifa: panic: the engine broke") || !strings.Contains(reported.Error(), "(*heldRender).render") {
		t.Errorf("OnError got %v", reported)
	}
}

// A request whose client goes away or whose deadline passes stops waiting at once. The render
// goes on for the requests that still wait. A client that went away has its connection cut:
// OnRequest sees context.Canceled from Next, but OnError is not called and nothing is logged. A
// deadline is an error like any other.
func TestRequestThatStopsWaitingLeavesTheRender(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ctx      func() (context.Context, context.CancelFunc)
		leave    bool  // the test cancels the context, else its deadline passes
		next     error // what Next returns to OnRequest
		reported error // what OnError gets, nil when it is not called
	}{
		{"canceled", func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			true, context.Canceled, nil},
		{"deadline", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), time.Second)
		}, false, context.DeadlineExceeded, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logged bytes.Buffer
			log.SetOutput(&logged)
			defer log.SetOutput(os.Stderr)
			var reported, next error
			app := frontApp()
			app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error { reported = err; return tifa.Error{Status: 500} }
			app.Hooks.OnRequest = func(c *tifa.Ctx) error {
				err := c.Next()
				if c.Request().Header.Get("X-First") != "" {
					next = err
				}
				return err
			}
			s, g := newHeldServer(t, config.Default(), app)
			var renderErr error
			g.result = func(ctx context.Context, in ssr.Input) (string, error) {
				renderErr = ctx.Err()
				return bigPage(in)
			}
			// The first request starts the render, so the render does not belong to it.
			ctx, cancel := tc.ctx()
			defer cancel()
			type answer struct {
				w        *httptest.ResponseRecorder
				panicked any
			}
			left := make(chan answer)
			go func() {
				r := httptest.NewRequest("GET", "/stats", nil).WithContext(ctx)
				r.Header.Set("X-First", "1")
				w, p := serve(s, r)
				left <- answer{w, p}
			}()
			until(t, "the first request to wait", func() bool { return waiting(s) == 1 })
			out := make([]*httptest.ResponseRecorder, 5)
			var wg sync.WaitGroup
			for i := range out {
				wg.Add(1)
				go func() { defer wg.Done(); out[i] = get(s, "/stats") }()
			}
			until(t, "the others to wait", func() bool { return waiting(s) == 6 })
			if tc.leave {
				cancel()
			}
			a := <-left
			if !errors.Is(next, tc.next) || !errors.Is(reported, tc.reported) || (reported == nil) != (tc.reported == nil) {
				t.Errorf("Next returned %v, OnError got %v", next, reported)
			}
			// A client that went away has its connection cut: net/http catches ErrAbortHandler
			// and sends nothing (TestClientThatLeavesIsCutOff).
			if tc.reported == nil && a.panicked != http.ErrAbortHandler {
				t.Errorf("the request that left panicked with %v, want http.ErrAbortHandler", a.panicked)
			}
			if tc.reported != nil && (a.panicked != nil || a.w.Code != 500) {
				t.Errorf("the request that left: %v, %d %v", a.panicked, a.w.Code, a.w.Header())
			}
			if tc.reported == nil && logged.Len() > 0 {
				t.Errorf("logged %q", logged.String())
			}
			if n := waiting(s); n != 5 {
				t.Errorf("%d requests wait", n)
			}
			close(g.open)
			wg.Wait()
			if g.renders() != 1 || renderErr != nil {
				t.Errorf("%d renders, the render's context: %v", g.renders(), renderErr)
			}
			for i, w := range out {
				if w.Code != 200 || w.Header().Get("X-Tifa-Render") != "shared" {
					t.Errorf("request %d: %d %v", i, w.Code, w.Header())
				}
			}
		})
	}
}

// A render that calls runtime.Goexit fails every request that waits for it, and none hangs.
func TestBurstGoexit(t *testing.T) {
	s, g := newHeldServer(t, config.Default(), frontApp())
	g.result = func(context.Context, ssr.Input) (string, error) {
		runtime.Goexit()
		return "", nil
	}
	for i, w := range burst(t, s, g, 10, "/stats", nil) {
		if w.Code != 500 {
			t.Errorf("request %d: %d %v", i, w.Code, w.Header())
		}
	}
	if g.renders() != 1 {
		t.Errorf("%d renders", g.renders())
	}
}

// When the last request stops waiting, the render stops waiting for a VM, and the next request
// starts a render of its own.
func TestRenderStopsWhenNoRequestWaits(t *testing.T) {
	s := New(config.Default(), frontApp().Hooks)
	app := frontApp()
	s.register(app.Layouts, app.Routes)
	var count atomic.Int32
	stopped := make(chan error, 1)
	if err := s.setFront(dist, func(ctx context.Context, in ssr.Input) (string, error) {
		if count.Add(1) == 1 {
			<-ctx.Done() // waits for a VM that never comes
			stopped <- ctx.Err()
			return "", ctx.Err()
		}
		return bigPage(in)
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	left := make(chan struct{})
	var panicked any
	go func() {
		_, panicked = serve(s, httptest.NewRequest("GET", "/stats", nil).WithContext(ctx))
		close(left)
	}()
	until(t, "the request to wait", func() bool { return waiting(s) == 1 })
	cancel()
	<-left
	if err := <-stopped; !errors.Is(err, context.Canceled) || panicked != http.ErrAbortHandler {
		t.Errorf("the render's context: %v, the request panicked with %v", err, panicked)
	}
	if w := get(s, "/stats"); w.Code != 200 || w.Header().Get("X-Tifa-Render") != "vm" || count.Load() != 2 {
		t.Errorf("the next request: %d renders, %d %v", count.Load(), w.Code, w.Header())
	}
}

// A request that comes once the render is stored gets the cache, not a render of its own: the
// render leaves the requests waiting for it and enters the cache in one step.
func TestRenderIsStoredBeforeItEnds(t *testing.T) {
	s, g := newHeldServer(t, config.Default(), frontApp())
	close(g.open)
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() { defer wg.Done(); get(s, "/stats") }()
	}
	wg.Wait()
	if g.renders() != 1 {
		t.Errorf("%d renders for 50 requests in a row", g.renders())
	}
}

// A request that comes while a render lands, between storing the page and ending its flight,
// finds the page: both happen in one step under the lock of the cache. The hook tells the two
// apart: when it runs with the lock held, the request can only take the lock after the render
// landed, else the request runs to its end while the render is half landed.
func TestRequestDuringLandingFindsThePage(t *testing.T) {
	s, g := newHeldServer(t, config.Default(), frontApp())
	close(g.open)
	arrived := make(chan struct{}, 2)
	s.cache.fetching = func() { arrived <- struct{}{} }
	var late *httptest.ResponseRecorder
	done := make(chan struct{})
	s.cache.landing = func() {
		s.cache.landing = nil
		<-arrived // the first request
		held := !s.cache.mu.TryLock()
		if !held {
			s.cache.mu.Unlock()
		}
		go func() { late = get(s, "/stats"); close(done) }()
		if held {
			<-arrived
		} else {
			<-done
		}
	}
	first := get(s, "/stats")
	<-done
	if g.renders() != 1 || first.Header().Get("X-Tifa-Render") != "vm" || late.Header().Get("X-Tifa-Render") != "cache" {
		t.Errorf("%d renders, the request during the landing rendered by %q", g.renders(), late.Header().Get("X-Tifa-Render"))
	}
}

// HEAD and GET share a render. The HEAD that started it gets the headers of the page.
func TestBurstAnswersHEAD(t *testing.T) {
	s, g := newHeldServer(t, config.Default(), frontApp())
	answers := map[string]chan *httptest.ResponseRecorder{http.MethodHead: make(chan *httptest.ResponseRecorder), http.MethodGet: make(chan *httptest.ResponseRecorder)}
	send := func(method string) {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(method, "/stats", nil))
		answers[method] <- w
	}
	go send(http.MethodHead)
	until(t, "HEAD to wait", func() bool { return waiting(s) == 1 })
	go send(http.MethodGet)
	until(t, "GET to wait", func() bool { return waiting(s) == 2 })
	close(g.open)
	head, get := <-answers[http.MethodHead], <-answers[http.MethodGet]
	if head.Code != 200 || head.Header().Get("X-Tifa-Render") != "vm" || head.Header().Get("Content-Encoding") != "" ||
		head.Header().Get("Content-Length") != strconv.Itoa(len(decoded(t, get))) {
		t.Errorf("HEAD: %d %v", head.Code, head.Header())
	}
	if get.Code != 200 || get.Header().Get("X-Tifa-Render") != "shared" || g.renders() != 1 {
		t.Errorf("GET: %d renders, %d %v", g.renders(), get.Code, get.Header())
	}
}

// serve is ServeHTTP that returns what the handler panicked with, which net/http would catch.
func serve(s *Server, r *http.Request) (w *httptest.ResponseRecorder, panicked any) {
	w = httptest.NewRecorder()
	defer func() { panicked = recover() }()
	s.ServeHTTP(w, r)
	return w, nil
}

// A client that closes its side of the connection while it waits for a shared render gets no
// answer at all, not an empty 200 with the page's ETag that a browser or a CDN could keep. Go
// cancels the request when the client half-closes, though the connection can still be written.
func TestClientThatLeavesIsCutOff(t *testing.T) {
	for _, accept := range []string{"", "gzip"} {
		t.Run("Accept-Encoding "+accept, func(t *testing.T) {
			s, g := newHeldServer(t, config.Default(), frontApp())
			srv := httptest.NewServer(s)
			defer srv.Close()
			first := make(chan *httptest.ResponseRecorder)
			go func() { first <- get(s, "/stats") }()
			until(t, "the first request to wait", func() bool { return waiting(s) == 1 })
			conn, err := net.Dial("tcp", srv.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			fmt.Fprintf(conn, "GET /stats HTTP/1.1\r\nHost: tifa.test\r\nAccept-Encoding: %s\r\n\r\n", accept)
			until(t, "the client to wait", func() bool { return waiting(s) == 2 })
			if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
				t.Fatal(err)
			}
			_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			got, err := io.ReadAll(conn)
			if len(got) != 0 || err != nil {
				t.Errorf("the client read %q, %v, want nothing and the connection closed", got, err)
			}
			close(g.open)
			if w := <-first; w.Code != 200 || g.renders() != 1 {
				t.Errorf("the request that stayed: %d renders, %d", g.renders(), w.Code)
			}
		})
	}
}

// A render that fails once no request waits for it is logged, a panic with its stack, since no
// request reports it to OnError. A render stopped because no request waits is not.
func TestFailureNoRequestWaitsForIsLogged(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result func(context.Context, ssr.Input) (string, error)
		want   []string
	}{
		{"error", func(context.Context, ssr.Input) (string, error) { return "", errors.New("Error: a component threw") },
			[]string{"tifa: GET /stats: ", "a component threw"}},
		{"panic", func(context.Context, ssr.Input) (string, error) { panic("the engine broke") },
			[]string{"tifa: GET /stats: ", "tifa: panic: the engine broke", "(*heldRender).render"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var logged bytes.Buffer
			log.SetOutput(writerFunc(func(b []byte) (int, error) { mu.Lock(); defer mu.Unlock(); return logged.Write(b) }))
			defer log.SetOutput(os.Stderr)
			read := func() string { mu.Lock(); defer mu.Unlock(); return logged.String() }
			s, g := newHeldServer(t, config.Default(), frontApp())
			g.result = tc.result
			ctx, cancel := context.WithCancel(context.Background())
			left := make(chan struct{})
			go func() {
				serve(s, httptest.NewRequest("GET", "/stats", nil).WithContext(ctx))
				close(left)
			}()
			until(t, "the request to wait", func() bool { return waiting(s) == 1 && g.renders() == 1 })
			cancel()
			<-left
			if got := read(); got != "" {
				t.Fatalf("logged before the render failed: %q", got)
			}
			close(g.open)
			until(t, "the failure to be logged", func() bool { return strings.Contains(read(), tc.want[0]) })
			for _, want := range tc.want {
				if !strings.Contains(read(), want) {
					t.Errorf("the log lacks %q:\n%s", want, read())
				}
			}
		})
	}
}
