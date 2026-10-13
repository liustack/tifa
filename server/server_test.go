package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestWithoutOnErrorPublicErrorsKeepTheirStatus(t *testing.T) {
	s := newServer(Hooks{})
	cases := map[string]int{"/orders/broken": 500, "/orders/panic": 500, "/orders/public": 409}
	for target, status := range cases {
		w := send(s, "GET", target, "")
		if w.Code != status {
			t.Errorf("%s: %d %s", target, w.Code, w.Body.String())
		}
		if status == 500 && strings.Contains(w.Body.String(), "database") {
			t.Errorf("%s leaked the internal error: %s", target, w.Body.String())
		}
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
			c.SetBodyLimit(1 << 10)
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
