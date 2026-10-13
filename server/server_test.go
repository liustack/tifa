package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
)

type order struct {
	ID   string `json:"id"`
	Note string `json:"note"`
}

var errDatabase = errors.New("database down")

// newServer registers the routes a generated main would register for an interface-only app.
func newServer(hooks Hooks) *Server {
	cfg := config.Default()
	cfg.Server.BodyLimit = 64
	s := New(cfg, hooks)
	s.Handle("GET", "/orders/{orderId}", func(c *tifa.Ctx) error {
		switch id := c.Param("orderId"); id {
		case "missing":
			return tifa.ErrNotFound
		case "wrapped":
			return fmt.Errorf("load order: %w", tifa.ErrNotFound)
		case "broken":
			return errDatabase
		case "panic":
			panic("nil order")
		case "public":
			return tifa.Error{Status: 409, Message: "订单已发货"}
		case "wrapped-public":
			return fmt.Errorf("ship order: %w", tifa.Error{Status: 409, Message: "订单已发货"})
		case "pointer-public":
			return &tifa.Error{Status: 409, Message: "订单已发货"}
		case "wrapped-pointer-public":
			return fmt.Errorf("ship order: %w", &tifa.Error{Status: 409, Message: "订单已发货"})
		default:
			return c.JSON(200, order{ID: id})
		}
	})
	s.Handle("POST", "/orders/{orderId}", func(c *tifa.Ctx) error {
		var in order
		if err := c.Bind(&in); err != nil {
			return err
		}
		return c.JSON(200, in)
	})
	s.Handle("POST", "/uploads", func(c *tifa.Ctx) error {
		var in order
		if err := c.Bind(&in); err != nil {
			return err
		}
		return c.NoContent(204)
	})
	s.Handle("GET", "/old", func(c *tifa.Ctx) error { return c.Redirect("/orders/1", 301) })
	return s
}

func send(s *Server, method, target, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestRedirectAndNotFoundSkipOnError(t *testing.T) {
	var onError atomic.Int32
	s := newServer(Hooks{
		OnRequest: func(c *tifa.Ctx) error {
			if c.Pattern() == "" && c.Request().URL.Path == "/legacy" {
				return c.Redirect("/orders/7", 308)
			}
			return c.Next()
		},
		OnError: func(c *tifa.Ctx, err error) tifa.Error {
			onError.Add(1)
			return tifa.Error{Status: 500, Message: "oops"}
		},
	})
	cases := []struct {
		method, target string
		status         int
		location       string
	}{
		{"GET", "/old", 301, "/orders/1"},
		{"GET", "/legacy", 308, "/orders/7"},
		{"GET", "/orders/missing", 404, ""},
		{"GET", "/orders/wrapped", 404, ""},
		{"GET", "/nowhere", 404, ""},
	}
	for _, tc := range cases {
		w := send(s, tc.method, tc.target, "")
		if w.Code != tc.status || w.Header().Get("Location") != tc.location {
			t.Errorf("%s %s: %d %q", tc.method, tc.target, w.Code, w.Header().Get("Location"))
		}
	}
	if n := onError.Load(); n != 0 {
		t.Fatalf("OnError ran %d times for expected results", n)
	}
	if w := send(s, "GET", "/orders/missing", ""); !strings.Contains(w.Body.String(), `"error":"Not Found"`) {
		t.Fatalf("404 body %s", w.Body.String())
	}
}

func TestUnexpectedErrorsGoThroughOnError(t *testing.T) {
	var seen []error
	s := newServer(Hooks{OnError: func(c *tifa.Ctx, err error) tifa.Error {
		seen = append(seen, err)
		return tifa.Error{Status: 503, Message: "稍后再试"}
	}})
	for _, target := range []string{"/orders/broken", "/orders/panic"} {
		w := send(s, "GET", target, "")
		if w.Code != 503 || !strings.Contains(w.Body.String(), "稍后再试") {
			t.Errorf("%s: %d %s", target, w.Code, w.Body.String())
		}
	}
	if len(seen) != 2 || !errors.Is(seen[0], errDatabase) || !strings.Contains(seen[1].Error(), "nil order") {
		t.Fatalf("OnError saw %v", seen)
	}
}

func TestPublicErrorsSkipOnError(t *testing.T) {
	var onError atomic.Int32
	withHook := newServer(Hooks{OnError: func(c *tifa.Ctx, err error) tifa.Error {
		onError.Add(1)
		return tifa.Error{Status: 503, Message: "稍后再试"}
	}})
	for _, s := range []*Server{withHook, newServer(Hooks{})} {
		for _, target := range []string{"/orders/public", "/orders/wrapped-public", "/orders/pointer-public", "/orders/wrapped-pointer-public"} {
			w := send(s, "GET", target, "")
			if w.Code != 409 || !strings.Contains(w.Body.String(), "订单已发货") {
				t.Errorf("%s: %d %s", target, w.Code, w.Body.String())
			}
		}
		large := `{"note":"` + strings.Repeat("x", 100) + `"}`
		if w := send(s, "POST", "/orders/1", large); w.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("Bind over the limit: %d %s", w.Code, w.Body.String())
		}
	}
	if n := onError.Load(); n != 0 {
		t.Fatalf("OnError ran %d times for tifa.Error results", n)
	}
}

func TestWithoutOnErrorUnexpectedErrorsAre500(t *testing.T) {
	s := newServer(Hooks{})
	for _, target := range []string{"/orders/broken", "/orders/panic"} {
		w := send(s, "GET", target, "")
		if w.Code != 500 || strings.Contains(w.Body.String(), "database") || strings.Contains(w.Body.String(), "nil order") {
			t.Errorf("%s: %d %s", target, w.Code, w.Body.String())
		}
	}
}

func TestRootPathIsTheExactHomePage(t *testing.T) {
	var patterns []string
	s := newServer(Hooks{OnRequest: func(c *tifa.Ctx) error {
		patterns = append(patterns, c.Pattern())
		return c.Next()
	}})
	s.Handle("GET", "/", func(c *tifa.Ctx) error { return c.Text(200, "home") })
	if w := send(s, "GET", "/", ""); w.Code != 200 || w.Body.String() != "home" {
		t.Fatalf("home: %d %s", w.Code, w.Body.String())
	}
	if w := send(s, "GET", "/nowhere", ""); w.Code != 404 {
		t.Fatalf("unmatched URL under a home page: %d %s", w.Code, w.Body.String())
	}
	if strings.Join(patterns, ",") != "/," {
		t.Fatalf("patterns %q", patterns)
	}
}

func TestNextReturnsHandlerError(t *testing.T) {
	var got error
	s := newServer(Hooks{OnRequest: func(c *tifa.Ctx) error {
		got = c.Next()
		if errors.Is(got, errDatabase) {
			return c.JSON(502, map[string]string{"error": "handled by middleware"})
		}
		return got
	}})
	w := send(s, "GET", "/orders/broken", "")
	if !errors.Is(got, errDatabase) || w.Code != 502 || !strings.Contains(w.Body.String(), "handled by middleware") {
		t.Fatalf("Next returned %v, response %d %s", got, w.Code, w.Body.String())
	}
	send(s, "GET", "/orders/missing", "")
	if !errors.Is(got, tifa.ErrNotFound) {
		t.Fatalf("Next returned %v for a missing order", got)
	}
}

func TestPatternIsEmptyForUnmatchedURL(t *testing.T) {
	var patterns []string
	s := newServer(Hooks{OnRequest: func(c *tifa.Ctx) error {
		patterns = append(patterns, c.Pattern())
		err := c.Next()
		if c.Pattern() == "" && !errors.Is(err, tifa.ErrNotFound) {
			t.Errorf("Next on an unmatched URL returned %v", err)
		}
		return err
	}})
	if w := send(s, "GET", "/nowhere", ""); w.Code != 404 {
		t.Fatalf("unmatched URL got %d", w.Code)
	}
	if w := send(s, "DELETE", "/orders/1", ""); w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD, POST" {
		t.Fatalf("method mismatch got %d, Allow %q", w.Code, w.Header().Get("Allow"))
	}
	send(s, "GET", "/orders/1", "")
	want := []string{"", "/orders/{orderId}", "/orders/{orderId}"}
	if strings.Join(patterns, ",") != strings.Join(want, ",") {
		t.Fatalf("patterns %q", patterns)
	}
}

func TestBodyLimitAppliesToBind(t *testing.T) {
	s := newServer(Hooks{OnRequest: func(c *tifa.Ctx) error {
		if c.Pattern() == "/uploads" {
			if err := c.SetBodyLimit(1 << 10); err != nil {
				return err
			}
		}
		return c.Next()
	}})
	large := `{"note":"` + strings.Repeat("x", 100) + `"}`
	if w := send(s, "POST", "/orders/1", large); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over the configured limit: %d %s", w.Code, w.Body.String())
	}
	if w := send(s, "POST", "/orders/1", `{"id":"1"}`); w.Code != 200 {
		t.Fatalf("within limit: %d %s", w.Code, w.Body.String())
	}
	if w := send(s, "POST", "/uploads", large); w.Code != 204 {
		t.Fatalf("raised limit: %d %s", w.Code, w.Body.String())
	}
}

type fakeRenderer struct{ notFound, errors int }

func (f *fakeRenderer) NotFound(c *tifa.Ctx) error {
	f.notFound++
	return c.Text(404, "page not found")
}

func (f *fakeRenderer) Error(c *tifa.Ctx, e tifa.Error) error {
	f.errors++
	return c.Text(e.Status, e.Message)
}

func TestRendererDrawsWholePageResultsOfUnmatchedURLs(t *testing.T) {
	r := &fakeRenderer{}
	s := newServer(Hooks{OnRequest: func(c *tifa.Ctx) error {
		if c.Request().URL.Path == "/fail" {
			return errDatabase
		}
		return c.Next()
	}})
	s.Renderer = r
	if w := send(s, "GET", "/nowhere", ""); w.Code != 404 || w.Body.String() != "page not found" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w := send(s, "GET", "/fail", ""); w.Code != 500 || r.errors != 1 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w := send(s, "POST", "/nowhere", ""); w.Code != 404 || !strings.Contains(w.Body.String(), `"error"`) {
		t.Fatalf("non-page request: %d %s", w.Code, w.Body.String())
	}
	if w := send(s, "GET", "/orders/missing", ""); !strings.Contains(w.Body.String(), `"error"`) {
		t.Fatalf("interface route must answer JSON: %s", w.Body.String())
	}
	if r.notFound != 1 {
		t.Fatalf("renderer drew %d not-found pages", r.notFound)
	}
}

func TestServeFinishesInFlightRequestsBeforeOnShutdown(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var events []string
	cfg := config.Default()
	cfg.Server.ShutdownTimeout = 5 * time.Second
	s := New(cfg, Hooks{OnShutdown: func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("OnShutdown context has no deadline")
		}
		events = append(events, "shutdown")
		return nil
	}})
	s.Handle("GET", "/slow", func(c *tifa.Ctx) error {
		close(started)
		<-release
		events = append(events, "request")
		return c.Text(200, "done")
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- s.Serve(ctx, ln) }()
	body := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/slow")
		if err != nil {
			body <- err.Error()
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		body <- string(b)
	}()
	<-started
	stop()
	time.Sleep(50 * time.Millisecond)
	if len(events) != 0 {
		t.Fatalf("OnShutdown ran before the in-flight request finished: %v", events)
	}
	close(release)
	if got := <-body; got != "done" {
		t.Fatalf("in-flight request got %q", got)
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
	if strings.Join(events, ",") != "request,shutdown" {
		t.Fatalf("events %v", events)
	}
}

func TestRunStopsWhenOnStartFails(t *testing.T) {
	cfg := config.Default()
	cfg.Server.Addr = "127.0.0.1:0"
	s := New(cfg, Hooks{OnStart: func() error { return errDatabase }})
	if err := s.Run(context.Background()); !errors.Is(err, errDatabase) {
		t.Fatalf("Run returned %v", err)
	}
}

type ctxKey struct{}

func TestMultipartTempFilesAreRemovedAfterSetContext(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	cfg := config.Default()
	cfg.Server.BodyLimit = 64 << 20
	s := New(cfg, Hooks{OnRequest: func(c *tifa.Ctx) error {
		c.SetContext(context.WithValue(c.Context(), ctxKey{}, "user"))
		return c.Next()
	}})
	s.Handle("POST", "/bind", func(c *tifa.Ctx) error {
		var in order
		if err := c.Bind(&in); err != nil {
			return err
		}
		return c.NoContent(204)
	})
	s.Handle("POST", "/raw", func(c *tifa.Ctx) error {
		if err := c.Request().ParseMultipartForm(0); err != nil {
			return err
		}
		return c.NoContent(204)
	})
	srv := httptest.NewServer(s)
	defer srv.Close()
	for path, size := range map[string]int{"/bind": 33 << 20, "/raw": 1 << 10} {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		part, _ := mw.CreateFormFile("file", "a.bin")
		_, _ = part.Write(bytes.Repeat([]byte("x"), size))
		_ = mw.WriteField("id", "1")
		_ = mw.Close()
		resp, err := http.Post(srv.URL+path, mw.FormDataContentType(), &body)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 204 {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
		var left []os.DirEntry
		for range 50 {
			if left, _ = os.ReadDir(tmp); len(left) == 0 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if len(left) != 0 {
			t.Fatalf("%s left %d multipart temp files", path, len(left))
		}
	}
}

func TestInformationalResponseDoesNotCountAsWritten(t *testing.T) {
	s := New(config.Default(), Hooks{})
	s.Handle("GET", "/hint", func(c *tifa.Ctx) error {
		c.Writer().Header().Set("Link", "</app.css>; rel=preload")
		c.Writer().WriteHeader(http.StatusEarlyHints)
		return tifa.ErrNotFound
	})
	srv := httptest.NewServer(s)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/hint")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("got %d after 103 Early Hints", resp.StatusCode)
	}
}

func TestMuxRedirectsGoThroughOnRequest(t *testing.T) {
	var seen []string
	s := newServer(Hooks{OnRequest: func(c *tifa.Ctx) error {
		err := c.Next()
		seen = append(seen, c.Request().URL.Path+" "+c.Pattern())
		return err
	}})
	s.Handle("GET", "/docs/", func(c *tifa.Ctx) error { return c.Text(200, "docs") })
	cases := []struct{ target, location string }{
		{"/orders//1?tab=all", "/orders/1?tab=all"},
		{"/orders/../old", "/old"},
		{"/docs", "/docs/"},
	}
	for _, tc := range cases {
		w := send(s, "GET", tc.target, "")
		if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != tc.location {
			t.Errorf("%s: %d %q", tc.target, w.Code, w.Header().Get("Location"))
		}
	}
	want := []string{"/orders//1 ", "/orders/../old ", "/docs "}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Fatalf("OnRequest saw %q", seen)
	}
	if w := send(newServer(Hooks{}), "GET", "/orders//1", ""); w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("without OnRequest: %d", w.Code)
	}
}

// startSlow serves a single /slow route on a fresh listener and starts one request to it.
func startSlow(t *testing.T, timeout time.Duration, slow func(c *tifa.Ctx), onShutdown func()) (stop func(), served chan error) {
	t.Helper()
	cfg := config.Default()
	cfg.Server.ShutdownTimeout = timeout
	s := New(cfg, Hooks{OnShutdown: func(context.Context) error { onShutdown(); return nil }})
	started := make(chan struct{})
	s.Handle("GET", "/slow", func(c *tifa.Ctx) error {
		close(started)
		slow(c)
		return c.Text(200, "done")
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	served = make(chan error, 1)
	go func() { served <- s.Serve(ctx, ln) }()
	go func() {
		if resp, err := http.Get("http://" + ln.Addr().String() + "/slow"); err == nil {
			resp.Body.Close()
		}
	}()
	<-started
	return stop, served
}

func TestOnShutdownWaitsForHandlersPastDrainTimeout(t *testing.T) {
	var mu sync.Mutex
	var events []string
	record := func(e string) { mu.Lock(); events = append(events, e); mu.Unlock() }
	stop, served := startSlow(t, 100*time.Millisecond, func(c *tifa.Ctx) {
		<-c.Context().Done()
		time.Sleep(50 * time.Millisecond) // cleanup that outlives the drain timeout
		record("handler returned")
	}, func() { record("shutdown") })
	stop()
	err := <-served
	if err == nil || !strings.Contains(err.Error(), "in-flight") {
		t.Errorf("Serve returned %v, want the drain timeout reported", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(events, ",") != "handler returned,shutdown" {
		t.Fatalf("events %v", events)
	}
}

func TestOnShutdownIsSkippedWhileHandlersNeverReturn(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	var shutdown atomic.Bool
	stop, served := startSlow(t, 50*time.Millisecond, func(*tifa.Ctx) { <-release }, func() { shutdown.Store(true) })
	stop()
	err := <-served
	if err == nil || !strings.Contains(err.Error(), "OnShutdown skipped") {
		t.Errorf("Serve returned %v", err)
	}
	if shutdown.Load() {
		t.Fatal("OnShutdown ran while a handler was still running")
	}
}

func TestFlushCountsAsWritten(t *testing.T) {
	s := New(config.Default(), Hooks{})
	s.Handle("GET", "/stream", func(c *tifa.Ctx) error {
		if err := http.NewResponseController(c.Writer()).Flush(); err != nil {
			return err
		}
		return errDatabase
	})
	s.Handle("GET", "/legacy-flush", func(c *tifa.Ctx) error {
		c.Writer().(http.Flusher).Flush()
		return tifa.ErrNotFound
	})
	for _, target := range []string{"/stream", "/legacy-flush"} {
		w := send(s, "GET", target, "")
		if w.Code != 200 || w.Body.Len() != 0 {
			t.Errorf("%s: framework wrote %d %q after the handler flushed", target, w.Code, w.Body.String())
		}
	}
}

func TestMuxNotFoundIsAnUnmatchedURL(t *testing.T) {
	var patterns []string
	s := newServer(Hooks{OnRequest: func(c *tifa.Ctx) error {
		patterns = append(patterns, c.Pattern())
		return c.Next()
	}})
	r := httptest.NewRequest("CONNECT", "example.com:443", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 404 || len(patterns) != 1 || patterns[0] != "" {
		t.Fatalf("CONNECT with an empty path: %d %s, OnRequest saw %q", w.Code, w.Body.String(), patterns)
	}
}
