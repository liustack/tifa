package server

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/ssr"
)

type flowKey struct{}

// The error flow of a request runs with the Ctx that OnRequest gave to next, so OnError sees the
// values OnRequest put into the context, as the route did.
func TestOnErrorSeesTheContextOfNext(t *testing.T) {
	var got any
	app := secretApp(func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		return next(c.WithContext(context.WithValue(c.Context(), flowKey{}, "lin")))
	}, new([]error))
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error {
		got = c.Context().Value(flowKey{})
		return tifa.Error{Status: 500, Message: "reported"}
	}
	s, _ := newFrontServer(t, app)
	get(s, "/secret?deny=1")
	if got != "lin" {
		t.Fatalf("OnError saw %v", got)
	}
}

// The page that shows a failed Load renders with the context of next as well, so a render clock
// set by OnRequest applies to it.
func TestErrorPageRendersWithTheContextOfNext(t *testing.T) {
	var got any
	app := secretApp(func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		return next(c.WithContext(context.WithValue(c.Context(), flowKey{}, "lin")))
	}, new([]error))
	s := newAppServer(t, app)
	err := s.setFront(dist, htmlOnly(func(ctx context.Context, in ssr.Input) (string, error) {
		got = ctx.Value(flowKey{})
		return "<html>failed</html>", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	get(s, "/secret?deny=1")
	if got != "lin" {
		t.Fatalf("the render saw %v", got)
	}
}

// Two goroutines that call next at once run the route once, and the other gets an error.
func TestNextRunsTheRouteOnceWhenCalledConcurrently(t *testing.T) {
	var routes, failed atomic.Int32
	app := testApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := next(c); err != nil && !errors.Is(err, tifa.ErrNotFound) {
					failed.Add(1)
				}
			}()
		}
		wg.Wait()
		return nil
	}
	app.Routes = append(app.Routes, Route{ID: "app/once", Path: "/once",
		GET: func(c *tifa.Ctx) error { routes.Add(1); return c.NoContent(204) }})
	s := newAppServer(t, app)
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/once", nil))
	if routes.Load() != 1 || failed.Load() != 7 {
		t.Fatalf("the route ran %d times, %d calls failed", routes.Load(), failed.Load())
	}
}
