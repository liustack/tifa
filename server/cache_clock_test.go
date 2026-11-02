package server

import (
	"testing"
	"time"

	"github.com/liustack/tifa"
)

// A request with a render clock gets a render that depends on the time of that clock, so it
// neither reads the cache nor writes to it.
func TestRenderClockSkipsTheCache(t *testing.T) {
	app := frontApp()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		if c.Header("X-Clock") != "" {
			return next(c.WithContext(tifa.WithRenderClock(c.Context(), func() time.Time { return at })))
		}
		return next(c)
	}
	s, r := newFrontServer(t, app)
	for range 2 {
		if w := get(s, "/stats", "X-Clock", "1"); w.Header().Get("X-Tifa-Render") != "vm" {
			t.Errorf("clock request: %v", w.Header())
		}
	}
	if r.count() != 2 {
		t.Fatalf("%d renders for 2 clock requests", r.count())
	}
	// It did not store: the first request without a clock renders, and the second hits.
	if w := get(s, "/stats"); w.Header().Get("X-Tifa-Render") != "vm" {
		t.Errorf("first plain request after clock requests: %v", w.Header())
	}
	if w := get(s, "/stats"); w.Header().Get("X-Tifa-Render") != "cache" {
		t.Errorf("second plain request: %v", w.Header())
	}
	// It did not read: a clock request after a hit renders again.
	if w := get(s, "/stats", "X-Clock", "1"); w.Header().Get("X-Tifa-Render") != "vm" || r.count() != 4 {
		t.Errorf("clock request after a cached page: %v, %d renders", w.Header(), r.count())
	}
}

// The HTML of a request with a render clock, and of a page with RenderCache = false, is not a
// function of the key, so neither carries an ETag that could answer 304 without a render.
func TestVolatilePagesAreNeverAnsweredNotModified(t *testing.T) {
	app := frontApp()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		if c.Header("X-Clock") != "" {
			return next(c.WithContext(tifa.WithRenderClock(c.Context(), func() time.Time { return now })))
		}
		return next(c)
	}
	s, r := newFrontServer(t, app)
	// A cached page has an ETag, and the same page rendered under a clock has none.
	etag := get(s, "/stats").Header().Get("ETag")
	if etag == "" {
		t.Fatal("a cached page has no ETag")
	}
	before := r.count()
	w := get(s, "/stats", "X-Clock", "1", "If-None-Match", etag)
	if w.Code != 200 || w.Header().Get("ETag") != "" || r.count() != before+1 {
		t.Errorf("clock request with If-None-Match: %d, ETag %q, %d renders", w.Code, w.Header().Get("ETag"), r.count()-before)
	}
	// The clock moves: the new HTML is rendered and sent, never a 304 for the old one.
	now = now.Add(time.Hour)
	before = r.count()
	if w := get(s, "/stats", "X-Clock", "1", "If-None-Match", etag); w.Code != 200 || r.count() != before+1 {
		t.Errorf("clock moved: %d, %d renders", w.Code, r.count()-before)
	}
	// RenderCache = false: whatever the client sends, the page renders.
	live := get(s, "/live")
	if live.Header().Get("ETag") != "" {
		t.Errorf("a page with RenderCache = false has the ETag %q", live.Header().Get("ETag"))
	}
	before = r.count()
	if w := get(s, "/live", "If-None-Match", "*"); w.Code != 200 || r.count() != before+1 {
		t.Errorf("RenderCache = false with If-None-Match: %d, %d renders", w.Code, r.count()-before)
	}
}
