package server

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
	"github.com/liustack/tifa/tifatest"
)

// Exercise the public helper through middleware, tifatest and an HTTP page.
func TestRenderClockFromMiddleware(t *testing.T) {
	at := time.Date(2026, 10, 22, 12, 0, 0, 123456789, time.UTC)
	middleware := func(c *tifa.Ctx) error {
		c.SetContext(tifa.WithRenderClock(c.Context(), func() time.Time { return at }))
		return c.Next()
	}
	cfg := config.Default().SSR
	cfg.VMs = 1
	pool, err := ssr.NewPool(`globalThis.startRender = () => {
 Promise.resolve().then(() => {
  const f = new Intl.DateTimeFormat('en-US',{timeZone:'UTC',dateStyle:'short'});
  globalThis.renderResult = '<p>' + [Date.now() === 1792670400123, new Date().getTime() === 1792670400123, Temporal.Now.instant().epochNanoseconds === 1792670400123456789n, f.format() === f.format(1792670400123)].join('|') + '</p>';
 });
};`, nil, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Error(err)
		}
	})
	c, _ := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
	calls := 0
	tifatest.SetNext(c, func() error {
		calls++
		html, err := pool.Render(c.Context(), ssr.Input{URL: "/"})
		if err != nil || html != "<p>true|true|true|true</p>" {
			t.Fatalf("tifatest render = %q, %v", html, err)
		}
		return nil
	})
	if err := middleware(c); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("middleware called next %d times", calls)
	}
	app := frontApp()
	app.Hooks.OnRequest = middleware
	s := newAppServer(t, app)
	if err := s.setFront(dist, pool.Render); err != nil {
		t.Fatal(err)
	}
	w := get(s, "/projects/p1")
	if w.Code != 200 || w.Body.String() != "<p>true|true|true|true</p>" {
		t.Fatalf("HTTP = %d %s", w.Code, w.Body.String())
	}
	// Explicit nil must shadow an inherited fixed clock.
	ctx := tifa.WithRenderClock(tifa.WithRenderClock(context.Background(), func() time.Time { return at }), nil)
	html, err := pool.Render(ctx, ssr.Input{URL: "/"})
	if err != nil || html == "<p>true|true|true|true</p>" {
		t.Fatalf("reset render = %q, %v", html, err)
	}
}
