package server

import (
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/liustack/tifa"
)

// A 409 for another build names the build the server runs, so the browser can reload once for
// that build. An app's own 409 does not carry the header.
func TestBuildMismatchNamesTheCurrentBuild(t *testing.T) {
	s, _ := newFrontServer(t, frontApp())
	w := getData(s, "/projects/p1", "old", "100")
	if w.Code != 409 || w.Header().Get("X-Tifa-Build") != "b1" {
		t.Errorf("data request of another build: %d %v", w.Code, w.Header())
	}
	if w := get(s, "/api/health", "X-Tifa-Call", "1"); w.Header().Get("X-Tifa-Build") != "" {
		t.Errorf("an answer that is no mismatch names a build: %v", w.Header())
	}
}

// A handler function of the browser sends the build its page was rendered with. A call from
// another build is refused with 409 before the handler runs, since the page may send an input
// the handler no longer reads as the page means it. A call that sends no build, from curl or
// another program, is not a page and reaches the handler.
func TestHandlerCallFromAnotherBuildIsRefused(t *testing.T) {
	var ran atomic.Int32
	app := frontApp()
	app.Routes = append(app.Routes, Route{ID: "app/api/ship", Path: "/api/ship", Layouts: []string{"app/layout"},
		POST:   func(c *tifa.Ctx) error { ran.Add(1); return c.JSON(200, map[string]bool{"ok": true}) },
		DELETE: func(c *tifa.Ctx) error { ran.Add(1); return tifa.Error{Status: 409, Message: "shipped"} },
	})
	s, _ := newFrontServer(t, app)
	call := func(method string, header ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/ship", strings.NewReader("{}"))
		for i := 0; i < len(header); i += 2 {
			r.Header.Set(header[i], header[i+1])
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}

	w := call("POST", "X-Tifa-Call", "1", "X-Tifa-Build", "old")
	if w.Code != 409 || w.Header().Get("X-Tifa-Build") != "b1" || w.Header().Get("Cache-Control") != "no-store" || ran.Load() != 0 {
		t.Errorf("call from another build: %d %v, handler ran %d times", w.Code, w.Header(), ran.Load())
	}
	if !strings.Contains(w.Body.String(), "reload the page") {
		t.Errorf("call from another build: body %s", w.Body.String())
	}
	if w := call("POST", "X-Tifa-Call", "1", "X-Tifa-Build", "b1"); w.Code != 200 || ran.Load() != 1 {
		t.Errorf("call from this build: %d, handler ran %d times", w.Code, ran.Load())
	}
	if w := call("POST"); w.Code != 200 || ran.Load() != 2 {
		t.Errorf("call without a build: %d, handler ran %d times", w.Code, ran.Load())
	}
	// The 409 of the handler itself is the app's, with no build named.
	if w := call("DELETE", "X-Tifa-Call", "1", "X-Tifa-Build", "b1"); w.Code != 409 || w.Header().Get("X-Tifa-Build") != "" {
		t.Errorf("409 of the handler: %d %v", w.Code, w.Header())
	}
}

// An app without pages has no build the browser could be on, so the header decides nothing.
func TestHandlerCallToAnAppWithoutPagesIgnoresTheBuild(t *testing.T) {
	s := newAppServer(t, testApp())
	r := httptest.NewRequest("POST", "/projects/p1", strings.NewReader("{}"))
	r.Header.Set("X-Tifa-Call", "1")
	r.Header.Set("X-Tifa-Build", "old")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Errorf("%d %s", w.Code, w.Body.String())
	}
}
