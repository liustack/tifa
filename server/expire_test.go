package server

import (
	"errors"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa"
)

// ExpirePages makes every kept page of a path again on its next request: each query, host and
// scheme of it, the HTML and its navigation data. Other paths stay.
func TestExpirePagesMakesEveryVariantOfThePathAgain(t *testing.T) {
	e := newPageEnv(t)
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return map[string]string{"v": "old"}, nil })
	variants := []func() *httptest.ResponseRecorder{
		func() *httptest.ResponseRecorder { return get(e.s, "/news") },
		func() *httptest.ResponseRecorder { return get(e.s, "/news?page=2") },
		func() *httptest.ResponseRecorder { return get(e.s, "https://example.com/news") },
		func() *httptest.ResponseRecorder { return get(e.s, "http://other.example/news") },
	}
	for _, v := range variants {
		v()
	}
	get(e.s, "/livepage")
	loads := e.loads.Load()
	for i, v := range variants {
		if w := v(); w.Header().Get("Cache-Status") != "tifa; hit" {
			t.Fatalf("variant %d before: %q", i, w.Header().Get("Cache-Status"))
		}
	}
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return map[string]string{"v": "new"}, nil })
	e.s.ExpirePages([]string{"/news"})
	for i, v := range variants {
		w := v()
		if w.Header().Get("Cache-Status") != "tifa; fwd=miss" || !strings.Contains(w.Body.String(), `"v":"new"`) {
			t.Errorf("variant %d after: %q\n%s", i, w.Header().Get("Cache-Status"), w.Body.String())
		}
		if w := v(); w.Header().Get("Cache-Status") != "tifa; hit" {
			t.Errorf("variant %d made again is not kept: %q", i, w.Header().Get("Cache-Status"))
		}
	}
	if made := e.loads.Load() - loads; made != 4 {
		t.Errorf("%d Loads after the expiry, want 4", made)
	}
	if w := get(e.s, "/livepage"); w.Header().Get("Cache-Status") != "tifa; hit" {
		t.Errorf("another path: %q", w.Header().Get("Cache-Status"))
	}
	// The navigation data comes from the entry made again.
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return map[string]string{"v": "newer"}, nil })
	e.s.ExpirePages([]string{"/news"})
	d := getData(e.s, "/news", "b1", "11")
	if !strings.Contains(d.Body.String(), `"v":"newer"`) || d.Header().Get("Cache-Status") != "tifa; fwd=miss" {
		t.Errorf("navigation data: %q %s", d.Header().Get("Cache-Status"), d.Body.String())
	}
	if w := get(e.s, "/news"); w.Header().Get("Cache-Status") != "tifa; hit" || !strings.Contains(w.Body.String(), `"v":"newer"`) {
		t.Errorf("the page after its data: %q %s", w.Header().Get("Cache-Status"), w.Body.String())
	}
}

// The path is the one of the URL as decoded: a page asked for with escapes is expired by its
// path with the letters, and a path that differs by a slash is another page.
func TestExpirePagesComparesDecodedPaths(t *testing.T) {
	e := newPageEnv(t, func(app *App) {
		app.Routes = append(app.Routes, Route{ID: "app/word", Path: "/w/{word}", Page: true, SSR: true, Cache: time.Minute, Layouts: []string{"app/layout"},
			Load: func(c *tifa.Ctx) (any, error) { e := c.Param("word"); return e, nil }})
	})
	get(e.s, "/w/caf%C3%A9")
	get(e.s, "/w/a%2Fb")
	get(e.s, "/w/x%3Fy")
	e.s.ExpirePages([]string{"/w/café", "/w/x?y"})
	if w := get(e.s, "/w/caf%C3%A9"); w.Header().Get("Cache-Status") != "tifa; fwd=miss" {
		t.Errorf("café: %q", w.Header().Get("Cache-Status"))
	}
	if w := get(e.s, "/w/x%3Fy"); w.Header().Get("Cache-Status") != "tifa; fwd=miss" {
		t.Errorf("x?y: %q", w.Header().Get("Cache-Status"))
	}
	if w := get(e.s, "/w/a%2Fb"); w.Header().Get("Cache-Status") != "tifa; hit" {
		t.Errorf("a/b, not named: %q", w.Header().Get("Cache-Status"))
	}
}

// After the expiry, a browser that sends the ETag of the old page gets the new page when the data
// changed, and a 304 when it did not, without a render.
func TestExpiredPageAndTheETag(t *testing.T) {
	e := newPageEnv(t)
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return "same", nil })
	tag := get(e.s, "/news").Header().Get("ETag")
	renders := e.r.count()
	e.s.ExpirePages([]string{"/news"})
	w := get(e.s, "/news", "If-None-Match", tag)
	if w.Code != 304 || w.Header().Get("Cache-Status") != "tifa; fwd=miss" || e.r.count() != renders || e.loads.Load() != 2 {
		t.Errorf("same data: %d %q, %d renders, %d Loads", w.Code, w.Header().Get("Cache-Status"), e.r.count()-renders, e.loads.Load())
	}
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return "changed", nil })
	e.s.ExpirePages([]string{"/news"})
	w = get(e.s, "/news", "If-None-Match", tag)
	if w.Code != 200 || w.Header().Get("ETag") == tag || !strings.Contains(w.Body.String(), "changed") {
		t.Errorf("changed data: %d, ETag %q, was %q", w.Code, w.Header().Get("ETag"), tag)
	}
}

// When making an expired page fails, the old page stands in, and says to a cache in front that it
// is no longer fresh: its s-maxage is not above its Age.
func TestExpiredPageStandsInForAFailure(t *testing.T) {
	e := newPageEnv(t)
	old := get(e.s, "/news")
	e.clock.advance(10 * time.Second)
	e.s.ExpirePages([]string{"/news"})
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return nil, errors.New("database down") })
	w := get(e.s, "/news")
	if w.Code != 200 || w.Body.String() != old.Body.String() || w.Header().Get("Cache-Status") != "tifa; fwd=stale" {
		t.Fatalf("%d %q\n%s", w.Code, w.Header().Get("Cache-Status"), w.Body.String())
	}
	age, _ := strconv.Atoi(w.Header().Get("Age"))
	cc := w.Header().Get("Cache-Control")
	if age != 10 || cc != "public, max-age=0, s-maxage=10, stale-if-error=86400" {
		t.Errorf("Age %q, Cache-Control %q", w.Header().Get("Age"), cc)
	}
	// Working again, the page is made and kept.
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return "back", nil })
	if w := get(e.s, "/news"); w.Header().Get("Cache-Status") != "tifa; fwd=miss" || !strings.Contains(w.Body.String(), "back") {
		t.Errorf("after the failure: %q %s", w.Header().Get("Cache-Status"), w.Body.String())
	}
	if w := get(e.s, "/news"); w.Header().Get("Cache-Status") != "tifa; hit" || w.Header().Get("Cache-Control") != "public, max-age=0, s-maxage=60, stale-if-error=86400" {
		t.Errorf("kept again: %q %q", w.Header().Get("Cache-Status"), w.Header().Get("Cache-Control"))
	}
}

// A make that started before the expiry read the data as it was. A request after the expiry does
// not wait for it, and its page is not kept, whether it ends before or after the new one.
func TestMakeBeforeTheExpiryIsNotKept(t *testing.T) {
	for _, order := range []string{"old ends last", "old ends first"} {
		t.Run(order, func(t *testing.T) {
			e := newPageEnv(t)
			started, release := make(chan struct{}), make(chan struct{})
			e.setLoad(func(c *tifa.Ctx, n int64) (any, error) {
				close(started)
				<-release
				return map[string]string{"v": "old"}, nil
			})
			before := make(chan *httptest.ResponseRecorder)
			go func() { before <- get(e.s, "/news") }()
			<-started
			e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return map[string]string{"v": "new"}, nil })
			e.s.ExpirePages([]string{"/news"})
			if order == "old ends first" {
				close(release)
				if w := <-before; !strings.Contains(w.Body.String(), `"v":"old"`) {
					t.Fatalf("the request before the expiry:\n%s", w.Body.String())
				}
				if w := get(e.s, "/news"); w.Header().Get("X-Tifa-Render") != "vm" || !strings.Contains(w.Body.String(), `"v":"new"`) {
					t.Fatalf("the old make was kept: %q\n%s", w.Header().Get("X-Tifa-Render"), w.Body.String())
				}
			} else {
				after := make(chan *httptest.ResponseRecorder)
				go func() { after <- get(e.s, "/news") }()
				var w *httptest.ResponseRecorder
				select {
				case w = <-after:
				case <-time.After(5 * time.Second):
					close(release)
					t.Fatal("the request after the expiry waits for the make that started before")
				}
				if !strings.Contains(w.Body.String(), `"v":"new"`) || w.Header().Get("X-Tifa-Render") != "vm" {
					t.Errorf("the request after the expiry: %q\n%s", w.Header().Get("X-Tifa-Render"), w.Body.String())
				}
				close(release)
				if w := <-before; !strings.Contains(w.Body.String(), `"v":"old"`) {
					t.Errorf("the request before the expiry:\n%s", w.Body.String())
				}
			}
			if w := get(e.s, "/news"); w.Header().Get("X-Tifa-Render") != "cache" || !strings.Contains(w.Body.String(), `"v":"new"`) {
				t.Errorf("the kept page: %q\n%s", w.Header().Get("X-Tifa-Render"), w.Body.String())
			}
		})
	}
}

// A page whose answer could not be kept is skipped by the cache for a while. An expiry ends that,
// since the app says what the page reads has changed.
func TestExpirePagesEndsTheSkip(t *testing.T) {
	e := newPageEnv(t)
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) {
		if n == 1 {
			c.SetHeader("Cache-Control", "private")
		}
		return n, nil
	})
	get(e.s, "/news")
	if w := get(e.s, "/news"); w.Header().Get("Cache-Status") != "tifa; fwd=bypass" {
		t.Fatalf("not skipped: %q", w.Header().Get("Cache-Status"))
	}
	e.s.ExpirePages([]string{"/news"})
	get(e.s, "/news")
	if w := get(e.s, "/news"); w.Header().Get("Cache-Status") != "tifa; hit" {
		t.Errorf("after the expiry: %q", w.Header().Get("Cache-Status"))
	}
}

// ExpirePages wants paths, which start with a slash.
func TestExpirePagesPanicsForAPathWithoutASlash(t *testing.T) {
	defer func() {
		if p := recover(); p == nil || !strings.Contains(p.(string), `"news"`) {
			t.Errorf("recovered %v", p)
		}
	}()
	tifa.ExpirePages("news")
}
