package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liustack/tifa"
)

type sessUser struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// countingStore wraps a store, counts the calls and can fail Get.
type countingStore struct {
	tifa.SessionStore
	gets    atomic.Int32
	failGet error
}

func (s *countingStore) Get(ctx context.Context, k string) ([]byte, bool, error) {
	s.gets.Add(1)
	if s.failGet != nil {
		return nil, false, s.failGet
	}
	return s.SessionStore.Get(ctx, k)
}

// sessionApp has a sign-in and a sign-out handler, a route that prints the session, and a page.
// The hooks it returns record what they saw.
type sessionLog struct {
	onRequestRan, onErrorRan atomic.Int32
	seenInOnRequest          atomic.Pointer[sessUser]
	seenInLoad               atomic.Pointer[sessUser]
	loadErr, afterErr        atomic.Value // error
}

func sessionApp(log *sessionLog) App {
	app := frontApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		log.onRequestRan.Add(1)
		log.seenInOnRequest.Store(tifa.Session[sessUser](c))
		switch c.Query("do") + c.Header("X-Do") {
		case "login-early", "in":
			if c.Header("X-Do") == "in" {
				if err := tifa.SetSession(c, &sessUser{ID: 1, Name: "Ann"}); err != nil {
					return err
				}
				break
			}
			if err := tifa.SetSession(c, &sessUser{ID: 1, Name: "Ann"}); err != nil {
				return err
			}
		case "out":
			if err := tifa.ClearSession(c); err != nil {
				return err
			}
		case "public":
			c.SetHeader("Cache-Control", "public, max-age=600")
		}
		err := next(c)
		log.afterErr.Store(errBox{tifa.SetSession(c, &sessUser{ID: 9})})
		return err
	}
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error {
		log.onErrorRan.Add(1)
		return tifa.Error{Status: 500, Message: "reported"}
	}
	app.Routes = append(app.Routes,
		Route{ID: "app/login", Path: "/signin", POST: func(c *tifa.Ctx) error {
			if err := tifa.SetSession(c, &sessUser{ID: 7, Name: "Ann"}); err != nil {
				return err
			}
			return tifa.Redirect("/", 303)
		}},
		Route{ID: "app/logout", Path: "/signout", POST: func(c *tifa.Ctx) error {
			if err := tifa.ClearSession(c); err != nil {
				return err
			}
			return c.NoContent(204)
		}},
		Route{ID: "app/me", Path: "/me", GET: func(c *tifa.Ctx) error {
			return c.JSON(200, tifa.Session[sessUser](c))
		}},
	)
	for i := range app.Routes {
		if app.Routes[i].ID == "app/stats" {
			// The generated code runs every Load through tifa.Use.
			app.Routes[i].Load = func(c *tifa.Ctx) (any, error) {
				return tifa.Use(c, func(c *tifa.Ctx) (any, error) {
					log.seenInLoad.Store(tifa.Session[sessUser](c))
					log.loadErr.Store(errBox{tifa.SetSession(c, &sessUser{ID: 2})})
					return 42, nil
				})
			}
		}
	}
	return app
}

type errBox struct{ err error }

func sessionServer(t *testing.T, app App, store tifa.SessionStore) *server {
	t.Helper()
	s, _ := newFrontServer(t, app)
	s.sess.store = store
	return s
}

func newStore(t *testing.T) *countingStore {
	m := tifa.NewMemoryStore(1 << 20)
	t.Cleanup(m.Close)
	return &countingStore{SessionStore: m}
}

func cookieOf(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func do(s *server, method, target string, header ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for i := 0; i < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

const prodCookie = "__Host-tifa_session"

func TestSessionSignInAndRead(t *testing.T) {
	var log sessionLog
	s := sessionServer(t, sessionApp(&log), newStore(t))
	w := do(s, "POST", "/signin")
	if w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	ck := cookieOf(w, prodCookie)
	if ck == nil || len(ck.Value) != 43 || !ck.HttpOnly || !ck.Secure || ck.SameSite != http.SameSiteLaxMode || ck.MaxAge != 30*24*3600 || ck.Path != "/" {
		t.Fatalf("cookie %+v", ck)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", w.Header().Get("Cache-Control"))
	}
	// OnRequest, a method handler and a Load all read it on the next request.
	w = do(s, "GET", "/me", "Cookie", prodCookie+"="+ck.Value)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"Ann"`) {
		t.Fatalf("/me %d %s", w.Code, w.Body.String())
	}
	if u := log.seenInOnRequest.Load(); u == nil || u.ID != 7 {
		t.Errorf("OnRequest saw %+v", u)
	}
	do(s, "GET", "/stats", "Cookie", prodCookie+"="+ck.Value)
	if u := log.seenInLoad.Load(); u == nil || u.ID != 7 {
		t.Errorf("Load saw %+v", u)
	}
	// Without the cookie nobody is signed in, and the store is not asked.
	store := s.sess.store.(*countingStore)
	before := store.gets.Load()
	w = do(s, "GET", "/me")
	if strings.TrimSpace(w.Body.String()) != "null" || store.gets.Load() != before {
		t.Errorf("anonymous: %s, gets %d", w.Body.String(), store.gets.Load()-before)
	}
}

func TestSessionSignOut(t *testing.T) {
	var log sessionLog
	s := sessionServer(t, sessionApp(&log), newStore(t))
	ck := cookieOf(do(s, "POST", "/signin"), prodCookie)
	w := do(s, "POST", "/signout", "Cookie", prodCookie+"="+ck.Value)
	if w.Code != 204 || cookieOf(w, prodCookie).MaxAge != -1 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	if w = do(s, "GET", "/me", "Cookie", prodCookie+"="+ck.Value); strings.TrimSpace(w.Body.String()) != "null" {
		t.Errorf("after sign-out: %s", w.Body.String())
	}
}

func TestSessionStoreFailureIsAnUnexpectedError(t *testing.T) {
	var log sessionLog
	store := newStore(t)
	s := sessionServer(t, sessionApp(&log), store)
	store.failGet = errors.New("redis down")
	w := do(s, "GET", "/me", "Cookie", prodCookie+"="+strings.Repeat("A", 43))
	if w.Code != 500 || log.onRequestRan.Load() != 0 || log.onErrorRan.Load() != 1 {
		t.Fatalf("%d, OnRequest ran %d, OnError ran %d", w.Code, log.onRequestRan.Load(), log.onErrorRan.Load())
	}
	if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "redis") {
		t.Errorf("%v %s", w.Header(), w.Body.String())
	}
	// A page answers with the error page, and no-store as well.
	w = do(s, "GET", "/stats", "Cookie", prodCookie+"="+strings.Repeat("A", 43))
	if w.Code != 500 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" {
		t.Errorf("page: %d %v", w.Code, w.Header())
	}
}

// A client that hung up while the store was read gets no answer and no OnError.
func TestSessionStoreFailureOfAGoneClientCutsTheConnection(t *testing.T) {
	var log sessionLog
	store := newStore(t)
	s := sessionServer(t, sessionApp(&log), store)
	store.failGet = context.Canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("GET", "/me", nil).WithContext(ctx)
	r.Header.Set("Cookie", prodCookie+"="+strings.Repeat("A", 43))
	defer func() {
		if p := recover(); p != http.ErrAbortHandler {
			t.Fatalf("recovered %v", p)
		}
		if log.onErrorRan.Load() != 0 || log.onRequestRan.Load() != 0 {
			t.Error("hooks ran")
		}
	}()
	s.ServeHTTP(httptest.NewRecorder(), r)
}

func TestSessionMalformedCookieIsDroppedWithoutAskingTheStore(t *testing.T) {
	var log sessionLog
	store := newStore(t)
	s := sessionServer(t, sessionApp(&log), store)
	w := do(s, "GET", "/me", "Cookie", prodCookie+"=not-a-token")
	if store.gets.Load() != 0 {
		t.Error("the store was asked")
	}
	if ck := cookieOf(w, prodCookie); ck == nil || ck.MaxAge != -1 || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("%v", w.Header())
	}
	if w.Code != 200 {
		t.Errorf("%d", w.Code)
	}
}

// An answer that writes the cookie is never a 304, keeps its status, and cannot be stored. The URL and
// the data stay the same, so the ETag of the first answer matches each time and only the cookie
// logic can stop the 304.
func TestSessionCookieAnswerIsNeverNotModified(t *testing.T) {
	var log sessionLog
	s := sessionServer(t, sessionApp(&log), newStore(t))
	etag := do(s, "GET", "/projects/p1").Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	ck := cookieOf(do(s, "POST", "/signin"), prodCookie)
	valid := prodCookie + "=" + ck.Value
	if w := do(s, "GET", "/projects/p1", "If-None-Match", etag); w.Code != 304 {
		t.Fatalf("control: %d", w.Code)
	}
	for _, tc := range []struct {
		name   string
		header []string
	}{
		{"sign in", []string{"X-Do", "in"}},
		{"sign out", []string{"X-Do", "out", "Cookie", valid}},
		{"bad cookie", []string{"Cookie", prodCookie + "=junk"}},
		{"unknown token", []string{"Cookie", prodCookie + "=" + strings.Repeat("Z", 43)}},
		{"sign in, any ETag", []string{"X-Do", "in", "If-None-Match", "*"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			header := append([]string{"If-None-Match", etag}, tc.header...)
			w := do(s, "GET", "/projects/p1", header...)
			if w.Code != 200 || w.Header().Get("ETag") != "" || w.Header().Get("Last-Modified") != "" || w.Header().Get("Cache-Control") != "no-store" || cookieOf(w, prodCookie) == nil {
				t.Fatalf("%d %v", w.Code, w.Header())
			}
		})
	}
}

func TestSessionCookieAnswerOverridesPublicCacheControl(t *testing.T) {
	var log sessionLog
	app := sessionApp(&log)
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		c.SetHeader("Cache-Control", "public, max-age=600")
		if err := tifa.SetSession(c, &sessUser{ID: 1}); err != nil {
			return err
		}
		return next(c)
	}
	s := sessionServer(t, app, newStore(t))
	for _, target := range []string{"/projects/p1", "/me", "/stats", "/nowhere"} {
		w := do(s, "GET", target)
		if w.Header().Get("Cache-Control") != "no-store" || cookieOf(w, prodCookie) == nil {
			t.Errorf("%s: %d %v", target, w.Code, w.Header())
		}
	}
}

func TestWhereSessionCanBeChangedInTheServer(t *testing.T) {
	var log sessionLog
	s := sessionServer(t, sessionApp(&log), newStore(t))
	w := do(s, "GET", "/stats")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if box, _ := log.loadErr.Load().(errBox); box.err == nil || !strings.Contains(box.err.Error(), "Load") {
		t.Errorf("in Load: %v", box.err)
	}
	if box, _ := log.afterErr.Load().(errBox); box.err == nil || !strings.Contains(box.err.Error(), "next") {
		t.Errorf("after next: %v", box.err)
	}
	if cookieOf(w, prodCookie) != nil {
		t.Error("a refused call set the cookie")
	}
}

func TestSessionInOnErrorIsRefused(t *testing.T) {
	var got atomic.Value
	var log sessionLog
	app := sessionApp(&log)
	app.Hooks.OnRequest = nil
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error {
		got.Store(errBox{tifa.SetSession(c, &sessUser{ID: 1})})
		return tifa.Error{Status: 500, Message: "reported"}
	}
	app.Routes = append(app.Routes, Route{ID: "app/boom", Path: "/boom", GET: func(c *tifa.Ctx) error { return errDatabase }})
	s := sessionServer(t, app, newStore(t))
	w := do(s, "GET", "/boom")
	if box, _ := got.Load().(errBox); box.err == nil || !strings.Contains(box.err.Error(), "OnError") || cookieOf(w, prodCookie) != nil {
		t.Errorf("%v %v", box.err, w.Header())
	}
}

func TestSessionCookieNameUnderDev(t *testing.T) {
	var log sessionLog
	s, _ := newFrontServer(t, sessionApp(&log))
	s.sess.store = newStore(t)
	s.dev = http.NotFoundHandler()
	ck := cookieOf(do(s, "POST", "/signin"), "tifa_session")
	if ck == nil || ck.Secure || !ck.HttpOnly {
		t.Fatalf("dev cookie %+v", ck)
	}
	if w := do(s, "GET", "/me", "Cookie", "tifa_session="+ck.Value); !strings.Contains(w.Body.String(), "Ann") {
		t.Errorf("%s", w.Body.String())
	}
}

func TestSessionCookieNameFromConfig(t *testing.T) {
	var log sessionLog
	s, _ := newFrontServer(t, sessionApp(&log))
	s.sess.store = newStore(t)
	s.sess.cfg.Cookie = "sid"
	s.sess.cfg.MaxAge = time.Hour
	ck := cookieOf(do(s, "POST", "/signin"), "sid")
	if ck == nil || ck.MaxAge != 3600 || !ck.Secure {
		t.Fatalf("cookie %+v", ck)
	}
}

// Without a store from OnStart the server keeps sessions in memory.
func TestSessionDefaultsToMemory(t *testing.T) {
	var log sessionLog
	before := runtime.NumGoroutine()
	s, _ := newFrontServer(t, sessionApp(&log))
	ck := cookieOf(do(s, "POST", "/signin"), prodCookie)
	if w := do(s, "GET", "/me", "Cookie", prodCookie+"="+ck.Value); !strings.Contains(w.Body.String(), "Ann") {
		t.Errorf("%s", w.Body.String())
	}
	// Close stops the cleanup goroutine of the store the server made.
	s.close()
	until(t, "the store's goroutine to stop", func() bool { return runtime.NumGoroutine() <= before })
}

func TestStartupUsesTheStoreOfOnStartAndLogsTheDefault(t *testing.T) {
	t.Cleanup(func() { tifa.SetSessionStore(nil) })
	var log syncBuffer
	oldDefault := logDefault(&log)
	defer oldDefault()
	runUntilListening(t, func() { log.Write([]byte("onstart-returns\n")) }, &log)
	out := log.String()
	i, j := strings.Index(out, "onstart-returns"), strings.Index(out, "sessions are kept in memory")
	if i < 0 || j < i {
		t.Fatalf("the memory line is missing or came before OnStart returned:\n%s", out)
	}
	var log2 syncBuffer
	oldDefault2 := logDefault(&log2)
	defer oldDefault2()
	runUntilListening(t, func() { tifa.SetSessionStore(tifa.NewMemoryStore(1 << 20)) }, &log2)
	if strings.Contains(log2.String(), "sessions are kept in memory") {
		t.Fatalf("the default was logged though OnStart set a store:\n%s", log2.String())
	}
}
