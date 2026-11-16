package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/ssr"
)

// These tests pin what an app is safe from by design, so a change that opens a hole fails here.

// After a write that changes what a Load returns, the next visit renders the page again. The memo
// holds the HTML by the data, so it has no old page to give, and the ETag of the old page gets no 304.
func TestPageAfterAWriteIsNotTheMemoizedOldPage(t *testing.T) {
	var stock atomic.Int32
	stock.Store(5)
	app := frontApp()
	app.Routes = append(app.Routes, Route{ID: "app/item", Path: "/item", Page: true, SSR: true, Layouts: []string{"app/layout"},
		Load: func(c *tifa.Ctx) (any, error) { return map[string]int32{"stock": stock.Load()}, nil }})
	s, r := newFrontServer(t, app)
	before := get(s, "/item")
	if again := get(s, "/item"); again.Header().Get("X-Tifa-Render") != "memo" || again.Body.String() != before.Body.String() {
		t.Fatalf("without a write the page comes from the memo: %v", again.Header())
	}
	stock.Store(4) // the write
	after := get(s, "/item")
	if after.Header().Get("X-Tifa-Render") != "vm" || after.Body.String() == before.Body.String() || !strings.Contains(after.Body.String(), `{"stock":4}`) {
		t.Errorf("after the write: %v\n%s", after.Header(), after.Body.String())
	}
	if after.Header().Get("ETag") == before.Header().Get("ETag") {
		t.Error("the ETag did not change with the data")
	}
	if w := get(s, "/item", "If-None-Match", before.Header().Get("ETag")); w.Code != 200 {
		t.Errorf("the ETag of the old page got %d, want 200", w.Code)
	}
	if r.count() != 2 {
		t.Errorf("%d renders, want 2", r.count())
	}
}

// With sessions in the data, each visitor gets the page of their own session from the memo, never
// another's, and every answer is private, so no cache in front hands it to someone else.
func TestMemoNeverGivesAPageToAnotherVisitor(t *testing.T) {
	s, r := wireServer(t, wireApp())
	r.result = func(in ssr.Input) (string, error) {
		return "<html>" + string(in.Session) + "</html>", nil
	}
	ann, bob := signedIn(t, s, "Ann"), signedIn(t, s, "Bob")
	visitors := []struct{ name, cookie string }{{"Ann", ann}, {"Bob", bob}, {"", ""}, {"Ann", ann}, {"Bob", bob}}
	for i, v := range visitors {
		var w *httptest.ResponseRecorder
		if v.cookie == "" {
			w = get(s, "/stats")
		} else {
			w = get(s, "/stats", "Cookie", v.cookie)
		}
		body := w.Body.String()
		for _, other := range []string{"Ann", "Bob"} {
			if has := strings.Contains(body, other); has != (other == v.name) {
				t.Errorf("visit %d of %q: page has %s = %t\n%s", i, v.name, other, has, body)
			}
		}
		if cc := w.Header().Get("Cache-Control"); cc != "private, no-cache" {
			t.Errorf("visit %d of %q: Cache-Control %q", i, v.name, cc)
		}
		// The last two visits are the same sessions again: the memo answers, with their own page.
		if i >= 3 && w.Header().Get("X-Tifa-Render") != "memo" {
			t.Errorf("visit %d of %q: %s, want memo", i, v.name, w.Header().Get("X-Tifa-Render"))
		}
	}
	if r.count() != 3 {
		t.Errorf("%d renders for three sessions", r.count())
	}
}

// A Load runs only for GET and HEAD. Another method on a page's URL is refused and the Load does
// not run, also when the request comes from the same origin.
func TestLoadIsNotReachedByAnotherMethod(t *testing.T) {
	var loads atomic.Int32
	app := frontApp()
	app.Routes = append(app.Routes, Route{ID: "app/ledger", Path: "/ledger", Page: true, SSR: true, Layouts: []string{"app/layout"},
		Load: func(c *tifa.Ctx) (any, error) { loads.Add(1); return 1, nil }})
	s, _ := newFrontServer(t, app)
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		r := httptest.NewRequest(method, "/ledger", strings.NewReader(`{}`))
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: %d, want 405\n%s", method, w.Code, w.Body.String())
		}
	}
	// A data request is a GET too: a POST with the navigation parameter is no data request.
	r := httptest.NewRequest("POST", dataURL("/ledger", "b1", "001"), strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST with _tifa: %d, want 405", w.Code)
	}
	if loads.Load() != 0 {
		t.Errorf("the Load ran %d times", loads.Load())
	}
	if get(s, "/ledger").Code != 200 || loads.Load() != 1 {
		t.Errorf("GET: the Load ran %d times", loads.Load())
	}
}
