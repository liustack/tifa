package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liustack/tifa"
)

// seen is what the code of the app saw of the request.
type seen struct {
	cookie, auth, header, rawCookie, rawAuth string
	hasCookie                                bool
	session                                  *wireUser
	setErr, clearErr                         error
}

// anonApp is wireApp with a page that has Cache (/news) and one that has not
// (/stats), a POST on /news, and an OnRequest that records what it sees and sets a cookie.
func anonApp(record func(path string, s seen)) App {
	app := wireApp()
	app.Routes = append(app.Routes, Route{
		ID: "app/news", Path: "/news", Page: true, SSR: true, Cache: time.Minute, Layouts: []string{"app/layout"},
		Load: func(c *tifa.Ctx) (any, error) { return 1, nil },
		POST: func(c *tifa.Ctx) error {
			ck, _ := c.Cookie("visit")
			return c.JSON(200, map[string]any{"cookie": ck != nil, "session": tifa.Session[wireUser](c) != nil})
		},
	})
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		if c.Pattern() == "/in" || c.Pattern() == "/__tifa/session" {
			return next(c)
		}
		var s seen
		ck, err := c.Cookie("visit")
		s.hasCookie = err == nil
		if ck != nil {
			s.cookie = ck.Value
		}
		s.auth = c.Header("Authorization")
		s.header = c.Header("Cookie")
		s.rawCookie = c.Request().Header.Get("Cookie")
		s.rawAuth = c.Request().Header.Get("Authorization")
		s.session = tifa.Session[wireUser](c)
		if c.Request().Method == "GET" || c.Request().Method == "HEAD" {
			s.setErr = tifa.SetSession(c, &wireUser{ID: 9})
			s.clearErr = tifa.ClearSession(c)
		}
		record(c.Pattern(), s)
		c.SetCookie(&http.Cookie{Name: "visitor", Value: "v1", Path: "/"})
		return next(c)
	}
	return app
}

type seenLog struct {
	mu sync.Mutex
	by map[string][]seen
}

func (l *seenLog) record(path string, s seen) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.by == nil {
		l.by = map[string][]seen{}
	}
	l.by[path] = append(l.by[path], s)
}

func (l *seenLog) last(t *testing.T, path string) seen {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.by[path]) == 0 {
		t.Fatalf("OnRequest never ran for %s", path)
	}
	return l.by[path][len(l.by[path])-1]
}

func TestPageWithCacheIsAnonymousToTheApp(t *testing.T) {
	var log seenLog
	s, _ := wireServer(t, anonApp(log.record))
	cookie := signedIn(t, s, "Ann") + "; visit=abc"
	data := dataURL("/news", "b1", "11")
	for _, tc := range []struct{ name, method, target string }{
		{"page", "GET", "/news"}, {"HEAD", "HEAD", "/news"}, {"data", "GET", data},
	} {
		r := httptest.NewRequest(tc.method, tc.target, nil)
		r.Header.Set("Cookie", cookie)
		r.Header.Set("Authorization", "Bearer secret")
		s.ServeHTTP(httptest.NewRecorder(), r)
		got := log.last(t, "/news")
		if got.hasCookie || got.cookie != "" || got.auth != "" || got.header != "" || got.rawCookie != "" || got.rawAuth != "" {
			t.Errorf("%s: the app saw %+v", tc.name, got)
		}
		if got.session != nil {
			t.Errorf("%s: Session is %+v", tc.name, got.session)
		}
	}
	// A page without the cache sees all of it.
	r := httptest.NewRequest("GET", "/stats", nil)
	r.Header.Set("Cookie", cookie)
	r.Header.Set("Authorization", "Bearer secret")
	s.ServeHTTP(httptest.NewRecorder(), r)
	if got := log.last(t, "/stats"); got.cookie != "abc" || got.auth != "Bearer secret" || got.session == nil || got.session.Name != "Ann" {
		t.Errorf("/stats: the app saw %+v", got)
	}
}

func TestPageWithCacheRefusesToChangeTheSession(t *testing.T) {
	var log seenLog
	s, _ := wireServer(t, anonApp(log.record))
	get(s, "/news")
	got := log.last(t, "/news")
	for name, err := range map[string]error{"SetSession": got.setErr, "ClearSession": got.clearErr} {
		if err == nil || !strings.Contains(err.Error(), "Cache") || !strings.Contains(err.Error(), "routes without Cache") {
			t.Errorf("%s in the flow of a page with Cache: %v", name, err)
		}
	}
	// The same calls on a page without it are not refused for that reason.
	get(s, "/stats")
	if got := log.last(t, "/stats"); got.setErr != nil && strings.Contains(got.setErr.Error(), "Cache") {
		t.Errorf("/stats: %v", got.setErr)
	}
}

func TestPageWithCacheNeverSetsACookieOnHTML(t *testing.T) {
	var log seenLog
	s, _ := wireServer(t, anonApp(log.record))
	for _, target := range []string{"/news", "/news?x=1"} {
		w := get(s, target)
		if w.Code != 200 || len(w.Result().Cookies()) != 0 || w.Header().Get("Set-Cookie") != "" {
			t.Errorf("%s: %d %v", target, w.Code, w.Header())
		}
	}
	if w := get(s, "/stats"); len(w.Result().Cookies()) == 0 {
		t.Error("a page without the cache lost its cookie")
	}
	// The answer to a navigation keeps it.
	w := getData(s, "/news", "b1", "11")
	if cookieOf(w, "visitor") == nil {
		t.Errorf("data answer: %v", w.Header())
	}
}

func TestPageWithCacheRendersWithoutTheSessionAndCarriesItInNavigation(t *testing.T) {
	var log seenLog
	s, r := wireServer(t, anonApp(log.record))
	cookie := signedIn(t, s, "Ann")
	w := get(s, "/news", "Cookie", cookie)
	if w.Code != 200 {
		t.Fatalf("%d", w.Code)
	}
	in := r.inputs[len(r.inputs)-1]
	if in.Session != nil || in.SessionKey == "" || !strings.Contains(in.SessionHead, "localStorage") {
		t.Errorf("render input: session %s key %q head %.40s", in.Session, in.SessionKey, in.SessionHead)
	}
	// A navigation gets the session of the browser it came from, built before the cookie was dropped.
	for cookie, want := range map[string]string{cookie: `{"signedIn":true,"data":{"name":"Ann"}}`, "": `{"signedIn":false}`} {
		w := getData(s, "/news", "b1", "11", "Cookie", cookie)
		m := decodeBody(t, w)
		if string(m["session"]) != want {
			t.Errorf("cookie %q: session %s, want %s", cookie, m["session"], want)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "private, no-cache" {
			t.Errorf("data Cache-Control %q", cc)
		}
	}
}

func TestPostOnAPageWithCacheIsNotAnonymous(t *testing.T) {
	var log seenLog
	s, _ := wireServer(t, anonApp(log.record))
	w := postJSON(s, "/news", `{}`, "Cookie", signedIn(t, s, "Ann")+"; visit=abc")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"cookie":true`) || !strings.Contains(w.Body.String(), `"session":true`) {
		t.Errorf("POST: %d %s", w.Code, w.Body.String())
	}
	if got := log.last(t, "/news"); got.cookie != "abc" || got.session == nil {
		t.Errorf("OnRequest of the POST saw %+v", got)
	}
}
