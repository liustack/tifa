package server

import (
	"bufio"
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
	"os/exec"
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
			// An app can hold the pointer as a plain error and wrap it. vet rejects %w on *tifa.Error itself.
			var err error = &tifa.Error{Status: 409, Message: "订单已发货"}
			return fmt.Errorf("ship order: %w", err)
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

// A handler function of the browser sends X-Tifa-Call. A redirect then comes as 200 and JSON
// the function follows with the router, not as a 3xx fetch would follow to the HTML of the
// target. Other clients still get the 3xx.
func TestHandlerCallRedirectIsJSON(t *testing.T) {
	s := newServer(Hooks{
		OnRequest: func(c *tifa.Ctx) error {
			// An app that sets a cache header for every answer, which a redirect must not keep.
			c.SetHeader("Cache-Control", "public, max-age=60")
			if c.Request().URL.Path == "/private" {
				return c.Redirect("/login?next=%2Fprivate", 302)
			}
			return c.Next()
		},
	})
	s.Handle("POST", "/orders/{orderId}/ship", func(c *tifa.Ctx) error { return c.Redirect("../8", 303) })
	s.Handle("POST", "/orders/{orderId}/copy", func(c *tifa.Ctx) error { return c.Redirect("/orders/9", 303) })
	cases := []struct {
		target, location string
		status           int
	}{
		{"/orders/7/copy", "/orders/9", 303},
		{"/orders/7/ship", "/orders/8", 303}, // resolved against the URL of the handler, as for the 3xx
		{"/private", "/login?next=%2Fprivate", 302},
	}
	for _, tc := range cases {
		r := httptest.NewRequest("POST", tc.target, strings.NewReader("{}"))
		r.Header.Set("X-Tifa-Call", "1")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		want := fmt.Sprintf(`{"redirect":%q,"status":%d}`, tc.location, tc.status)
		if w.Code != 200 || w.Header().Get("X-Tifa-Redirect") != fmt.Sprint(tc.status) || w.Header().Get("Location") != "" || strings.TrimSpace(w.Body.String()) != want {
			t.Errorf("call %s: %d %v %s", tc.target, w.Code, w.Header(), w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Errorf("call %s: headers %v", tc.target, w.Header())
		}
		if w := send(s, "POST", tc.target, "{}"); w.Code != tc.status || w.Header().Get("Location") != tc.location || w.Header().Get("X-Tifa-Redirect") != "" {
			t.Errorf("plain %s: %d %v", tc.target, w.Code, w.Header())
		}
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

func serveForShutdown(t *testing.T, s *Server) (addr string, stop func(), served chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	served = make(chan error, 1)
	go func() { served <- s.Serve(ctx, ln) }()
	return ln.Addr().String(), stop, served
}

func TestRequestInTheEntryWindowNeverRunsAfterOnShutdown(t *testing.T) {
	reached, release, entryDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	testHookEntry = func(stage string) {
		switch stage {
		case "before":
			close(reached)
			<-release
		case "done":
			close(entryDone)
		}
	}
	defer func() { testHookEntry = nil }()
	var mu sync.Mutex
	var events []string
	record := func(e string) { mu.Lock(); events = append(events, e); mu.Unlock() }
	cfg := config.Default()
	cfg.Server.ShutdownTimeout = 50 * time.Millisecond
	s := New(cfg, Hooks{OnShutdown: func(context.Context) error { record("shutdown"); return nil }})
	s.Handle("GET", "/work", func(c *tifa.Ctx) error {
		record("business")
		return c.NoContent(204)
	})
	addr, stop, served := serveForShutdown(t, s)
	go func() {
		if resp, err := http.Get("http://" + addr + "/work"); err == nil {
			resp.Body.Close()
		}
	}()
	<-reached // net/http has handed the request over, the server has not admitted it yet
	stop()
	<-served
	close(release)
	<-entryDone
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(events, ",") != "shutdown" {
		t.Fatalf("events %v, handler code must not start once shutdown has begun waiting", events)
	}
}

func TestHijackedHandlerIsCanceledBeforeOnShutdown(t *testing.T) {
	var mu sync.Mutex
	var events []string
	record := func(e string) { mu.Lock(); events = append(events, e); mu.Unlock() }
	cfg := config.Default()
	cfg.Server.ShutdownTimeout = time.Second
	s := New(cfg, Hooks{OnShutdown: func(context.Context) error { record("shutdown"); return nil }})
	hijacked := make(chan struct{})
	s.Handle("GET", "/socket", func(c *tifa.Ctx) error {
		conn, _, err := http.NewResponseController(c.Writer()).Hijack()
		if err != nil {
			return err
		}
		defer conn.Close()
		close(hijacked)
		<-c.Context().Done()
		record("handler returned")
		return nil
	})
	addr, stop, served := serveForShutdown(t, s)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "GET /socket HTTP/1.1\r\nHost: test\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	<-hijacked
	stop()
	if err := <-served; err != nil {
		t.Errorf("Serve returned %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(events, ",") != "handler returned,shutdown" {
		t.Fatalf("events %v", events)
	}
}

// plainWriter supports nothing beyond http.ResponseWriter, like a writer wrapped by http.TimeoutHandler.
type plainWriter struct {
	header http.Header
	code   int
	body   bytes.Buffer
	calls  int // WriteHeader and Write calls
}

func newPlainWriter() *plainWriter { return &plainWriter{header: http.Header{}} }

func (p *plainWriter) Header() http.Header { return p.header }
func (p *plainWriter) WriteHeader(code int) {
	if code < 100 || code > 999 {
		panic(fmt.Sprintf("invalid WriteHeader code %v", code))
	}
	p.calls++
	if p.code == 0 {
		p.code = code
	}
}
func (p *plainWriter) Write(b []byte) (int, error) {
	p.calls++
	if p.code == 0 {
		p.code = 200
	}
	return p.body.Write(b)
}

// hijackWriter hands out one end of a pipe as the hijacked connection.
type hijackWriter struct {
	*plainWriter
	conn net.Conn
}

func (h *hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return h.conn, bufio.NewReadWriter(bufio.NewReader(h.conn), bufio.NewWriter(h.conn)), nil
}

func TestFailedCommitDoesNotCountAsWritten(t *testing.T) {
	s := New(config.Default(), Hooks{})
	s.Handle("GET", "/flush", func(c *tifa.Ctx) error {
		if err := http.NewResponseController(c.Writer()).Flush(); !errors.Is(err, http.ErrNotSupported) {
			t.Errorf("Flush returned %v", err)
		}
		return errDatabase
	})
	s.Handle("GET", "/bad-status", func(c *tifa.Ctx) error { return c.NoContent(0) })
	for _, target := range []string{"/flush", "/bad-status"} {
		w := newPlainWriter()
		s.ServeHTTP(w, httptest.NewRequest("GET", target, nil))
		if w.code != 500 || !strings.Contains(w.body.String(), "Internal Server Error") {
			t.Errorf("%s: got %d %q, want the 500 error response", target, w.code, w.body.String())
		}
	}
}

func TestErrorAfterHijackWritesNothing(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s := New(config.Default(), Hooks{})
	s.Handle("GET", "/socket", func(c *tifa.Ctx) error {
		if _, _, err := http.NewResponseController(c.Writer()).Hijack(); err != nil {
			return err
		}
		return errDatabase
	})
	w := &hijackWriter{plainWriter: newPlainWriter(), conn: server}
	s.ServeHTTP(w, httptest.NewRequest("GET", "/socket", nil))
	if w.calls != 0 {
		t.Fatalf("framework wrote to a hijacked connection: %d calls, %d %q", w.calls, w.code, w.body.String())
	}
}

func TestListenerFailureShutsDownLikeCancel(t *testing.T) {
	var mu sync.Mutex
	var events []string
	record := func(e string) { mu.Lock(); events = append(events, e); mu.Unlock() }
	cfg := config.Default()
	cfg.Server.ShutdownTimeout = 100 * time.Millisecond
	s := New(cfg, Hooks{OnShutdown: func(context.Context) error { record("shutdown"); return nil }})
	started := make(chan struct{})
	s.Handle("GET", "/slow", func(c *tifa.Ctx) error {
		close(started)
		<-c.Context().Done()
		record("handler returned")
		return nil
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- s.Serve(context.Background(), ln) }()
	go func() {
		if resp, err := http.Get("http://" + ln.Addr().String() + "/slow"); err == nil {
			resp.Body.Close()
		}
	}()
	<-started
	ln.Close()
	if err := <-served; !errors.Is(err, net.ErrClosed) {
		t.Errorf("Serve returned %v, want the listener error", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(events, ",") != "handler returned,shutdown" {
		t.Fatalf("events %v", events)
	}
}

// net/http reads GODEBUG=httpmuxgo121 once at startup, so a child test process runs Run with the
// old ServeMux semantics, under which {name} would be a literal path segment.
func TestRunRefusesLegacyServeMux(t *testing.T) {
	if os.Getenv("TIFA_TEST_LEGACY_MUX") == "1" {
		cfg := config.Default()
		cfg.Server.Addr = "127.0.0.1:0"
		started := false
		err := New(cfg, Hooks{OnStart: func() error { started = true; return nil }}).Run(context.Background())
		fmt.Printf("started=%t err=%v\n", started, err)
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		serveErr := New(cfg, Hooks{}).Serve(context.Background(), ln)
		conn, dialErr := net.Dial("tcp", ln.Addr().String())
		if dialErr == nil {
			conn.Close()
		}
		fmt.Printf("serve refused=%t listener open=%t\n", serveErr != nil, dialErr == nil)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run", "^TestRunRefusesLegacyServeMux$", "-test.count=1", "-test.v", "-test.timeout=10s")
	cmd.Env = append(os.Environ(), "TIFA_TEST_LEGACY_MUX=1", "GODEBUG=httpmuxgo121=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "started=false err=tifa:") || !strings.Contains(string(out), "httpmuxgo121") {
		t.Fatalf("Run did not refuse the old ServeMux semantics:\n%s", out)
	}
	if !strings.Contains(string(out), "serve refused=true listener open=false") {
		t.Fatalf("Serve must refuse and close the listener it was given:\n%s", out)
	}
}

// A root catch-all page, Path "/{rest...}", is registered the way every route is: one pattern
// without a method. It must coexist with the server's handling of unmatched URLs, and more
// specific routes still win.
func TestRootCatchAllCoexistsWithUnmatchedHandling(t *testing.T) {
	var patterns []string
	s := newServer(Hooks{OnRequest: func(c *tifa.Ctx) error {
		patterns = append(patterns, c.Pattern())
		return c.Next()
	}})
	s.Handle("GET", "/{rest...}", func(c *tifa.Ctx) error { return c.Text(200, "rest="+c.Param("rest")) })
	if w := send(s, "GET", "/any/where", ""); w.Code != 200 || w.Body.String() != "rest=any/where" {
		t.Errorf("/any/where: %d %q", w.Code, w.Body.String())
	}
	if w := send(s, "GET", "/orders/7", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"7"`) {
		t.Errorf("/orders/7: %d %q", w.Code, w.Body.String())
	}
	if w := send(s, "GET", "/orders//7", ""); w.Code != http.StatusTemporaryRedirect {
		t.Errorf("/orders//7: %d", w.Code)
	}
	want := "/{rest...},/orders/{orderId},"
	if got := strings.Join(patterns, ","); got != want {
		t.Errorf("OnRequest saw patterns %q, want %q", got, want)
	}
}
