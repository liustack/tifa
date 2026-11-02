package core

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

type testSession struct {
	UserID int64  `json:"userId"`
	Name   string `json:"name"`
}

// logStore records the keys it is asked for and can be told to fail.
type logStore struct {
	*MemoryStore
	mu                           sync.Mutex
	gets, puts, deletes          []string
	failGet, failPut, failDelete error
	failDeleteKey                string // when set, only this key fails to delete
}

func newLogStore(t *testing.T) *logStore {
	m := NewMemoryStore(1 << 20)
	t.Cleanup(m.Close)
	return &logStore{MemoryStore: m}
}

func (s *logStore) Get(ctx context.Context, k string) ([]byte, bool, error) {
	s.mu.Lock()
	s.gets = append(s.gets, k)
	err := s.failGet
	s.mu.Unlock()
	if err != nil {
		return nil, false, err
	}
	return s.MemoryStore.Get(ctx, k)
}

func (s *logStore) Put(ctx context.Context, k string, d []byte, e time.Time) error {
	s.mu.Lock()
	s.puts = append(s.puts, k)
	err := s.failPut
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.MemoryStore.Put(ctx, k, d, e)
}

func (s *logStore) Delete(ctx context.Context, k string) error {
	s.mu.Lock()
	s.deletes = append(s.deletes, k)
	err := s.failDelete
	if s.failDeleteKey != "" && k != s.failDeleteKey {
		err = nil
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.MemoryStore.Delete(ctx, k)
}

func prodEnv(store SessionStore) *SessionEnv {
	return &SessionEnv{Store: store, Cookie: "__Host-tifa_session", Secure: true, MaxAge: 30 * 24 * time.Hour}
}

// sessionCtx builds a Ctx the way the server does, with cookie as the Cookie header when not empty.
func sessionCtx(t *testing.T, env *SessionEnv, cookie string) (*Ctx, *httptest.ResponseRecorder) {
	t.Helper()
	r := httptest.NewRequest("GET", "/", nil)
	if cookie != "" {
		r.Header.Set("Cookie", cookie)
	}
	w := httptest.NewRecorder()
	c := New(w, r, "/", 1<<20)
	if err := AttachSession(c, env); err != nil {
		t.Fatal(err)
	}
	return c, w
}

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func hashOf(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// setCookies returns the Set-Cookie headers of w for the cookie name.
func setCookies(w *httptest.ResponseRecorder, name string) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range (&http.Response{Header: w.Header()}).Cookies() {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

func signIn(t *testing.T, store SessionStore) string {
	t.Helper()
	c, w := sessionCtx(t, prodEnv(store), "")
	if err := SetSession(c, &testSession{UserID: 7, Name: "Ann"}); err != nil {
		t.Fatal(err)
	}
	return setCookies(w, "__Host-tifa_session")[0].Value
}

func TestSetSessionTokenAndStoreKey(t *testing.T) {
	store := newLogStore(t)
	c, w := sessionCtx(t, prodEnv(store), "")
	if Session[testSession](c) != nil {
		t.Fatal("a session before sign-in")
	}
	if err := SetSession(c, &testSession{UserID: 7, Name: "Ann"}); err != nil {
		t.Fatal(err)
	}
	cookies := setCookies(w, "__Host-tifa_session")
	if len(cookies) != 1 || !tokenPattern.MatchString(cookies[0].Value) {
		t.Fatalf("cookies %v", cookies)
	}
	token := cookies[0].Value
	if len(store.puts) != 1 || store.puts[0] != hashOf(token) {
		t.Fatalf("store keys %v, want the hash of the token", store.puts)
	}
	for _, k := range append(append([]string{}, store.puts...), store.deletes...) {
		if k == token {
			t.Fatal("the store saw the token")
		}
	}
	data, found, _ := store.MemoryStore.Get(context.Background(), hashOf(token))
	if !found || !strings.HasPrefix(string(data), `{"v":1,"expires":"`) || !strings.Contains(string(data), `"data":{"userId":7,"name":"Ann"}`) {
		t.Fatalf("record %s", data)
	}
	if got := Session[testSession](c); got == nil || got.UserID != 7 || got.Name != "Ann" {
		t.Fatalf("session after SetSession: %+v", got)
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	c, w := sessionCtx(t, prodEnv(newLogStore(t)), "")
	SetSession(c, &testSession{UserID: 1})
	h := w.Header().Get("Set-Cookie")
	for _, want := range []string{"__Host-tifa_session=", "Path=/", "Max-Age=2592000", "HttpOnly", "Secure", "SameSite=Lax"} {
		if !strings.Contains(h, want) {
			t.Errorf("%q lacks %q", h, want)
		}
	}
	if strings.Contains(h, "Domain") || strings.Contains(h, "Expires") {
		t.Errorf("%q has Domain or Expires", h)
	}
	dev := &SessionEnv{Store: newLogStore(t), Cookie: "tifa_session", MaxAge: 30 * 24 * time.Hour}
	c, w = sessionCtx(t, dev, "")
	SetSession(c, &testSession{UserID: 1})
	h = w.Header().Get("Set-Cookie")
	if !strings.HasPrefix(h, "tifa_session=") || strings.Contains(h, "Secure") || !strings.Contains(h, "HttpOnly") {
		t.Errorf("dev cookie %q", h)
	}
}

func TestSetSessionReplacesTheOldSession(t *testing.T) {
	store := newLogStore(t)
	old := signIn(t, store)
	c, w := sessionCtx(t, prodEnv(store), "__Host-tifa_session="+old)
	if got := Session[testSession](c); got == nil || got.UserID != 7 {
		t.Fatalf("loaded %+v", got)
	}
	if err := SetSession(c, &testSession{UserID: 8}); err != nil {
		t.Fatal(err)
	}
	fresh := setCookies(w, "__Host-tifa_session")[0].Value
	if fresh == old {
		t.Fatal("the token did not change")
	}
	if _, found, _ := store.MemoryStore.Get(context.Background(), hashOf(old)); found {
		t.Error("the old record stayed")
	}
	if got := Session[testSession](c); got.UserID != 8 {
		t.Errorf("session %+v", got)
	}
	// A second call in the same request replaces the first one's record, and leaves one cookie.
	if err := SetSession(c, &testSession{UserID: 9}); err != nil {
		t.Fatal(err)
	}
	third := setCookies(w, "__Host-tifa_session")
	if len(third) != 1 || third[0].Value == fresh {
		t.Fatalf("cookies after two calls: %v", third)
	}
	if _, found, _ := store.MemoryStore.Get(context.Background(), hashOf(fresh)); found {
		t.Error("the record of the first call stayed")
	}
	if got := Session[testSession](c); got.UserID != 9 {
		t.Errorf("session %+v", got)
	}
}

func TestSetSessionFailures(t *testing.T) {
	t.Run("Put fails", func(t *testing.T) {
		store := newLogStore(t)
		old := signIn(t, store)
		store.failPut = errors.New("store down")
		c, w := sessionCtx(t, prodEnv(store), "__Host-tifa_session="+old)
		if err := SetSession(c, &testSession{UserID: 8}); err == nil || !strings.Contains(err.Error(), "store down") {
			t.Fatalf("error %v", err)
		}
		if len(w.Header().Values("Set-Cookie")) != 0 || NoStore(c) {
			t.Error("a failed Put changed the response")
		}
		if _, found, _ := store.MemoryStore.Get(context.Background(), hashOf(old)); !found {
			t.Error("the old record is gone")
		}
		if got := Session[testSession](c); got == nil || got.UserID != 7 {
			t.Errorf("session %+v", got)
		}
	})
	t.Run("Delete of the old record fails", func(t *testing.T) {
		store := newLogStore(t)
		old := signIn(t, store)
		store.failDelete, store.failDeleteKey = errors.New("store down"), hashOf(old)
		c, w := sessionCtx(t, prodEnv(store), "__Host-tifa_session="+old)
		err := SetSession(c, &testSession{UserID: 8})
		if err == nil || !strings.Contains(err.Error(), "not revoked") {
			t.Fatalf("error %v", err)
		}
		if len(w.Header().Values("Set-Cookie")) != 0 {
			t.Error("a cookie went out")
		}
		if got := Session[testSession](c); got == nil || got.UserID != 7 {
			t.Errorf("session %+v", got)
		}
		if n := len(store.MemoryStore.entries); n != 1 {
			t.Errorf("%d records, want only the old one", n)
		}
	})
	t.Run("record over 4 KiB", func(t *testing.T) {
		store := newLogStore(t)
		c, w := sessionCtx(t, prodEnv(store), "")
		err := SetSession(c, &testSession{Name: strings.Repeat("x", 4096)})
		if err == nil || !strings.Contains(err.Error(), "4 KiB") {
			t.Fatalf("error %v", err)
		}
		if len(store.puts) != 0 || len(w.Header().Values("Set-Cookie")) != 0 {
			t.Error("a refused session reached the store or the response")
		}
	})
	t.Run("value that does not encode", func(t *testing.T) {
		c, _ := sessionCtx(t, prodEnv(newLogStore(t)), "")
		if err := SetSession(c, &struct{ F func() }{}); err == nil {
			t.Fatal("no error")
		}
	})
}

func TestClearSession(t *testing.T) {
	store := newLogStore(t)
	token := signIn(t, store)
	c, w := sessionCtx(t, prodEnv(store), "__Host-tifa_session="+token)
	if err := ClearSession(c); err != nil {
		t.Fatal(err)
	}
	if Session[testSession](c) != nil {
		t.Error("session after ClearSession")
	}
	if _, found, _ := store.MemoryStore.Get(context.Background(), hashOf(token)); found {
		t.Error("record stayed")
	}
	cookies := setCookies(w, "__Host-tifa_session")
	if len(cookies) != 1 || cookies[0].MaxAge != -1 || cookies[0].Value != "" {
		t.Errorf("cookies %+v", cookies)
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Errorf("%q", w.Header().Get("Set-Cookie"))
	}
}

func TestClearSessionWithoutSessionDoesNothing(t *testing.T) {
	store := newLogStore(t)
	c, w := sessionCtx(t, prodEnv(store), "")
	if err := ClearSession(c); err != nil {
		t.Fatal(err)
	}
	if len(store.deletes) != 0 || len(w.Header().Values("Set-Cookie")) != 0 || NoStore(c) {
		t.Error("ClearSession did something")
	}
}

func TestClearSessionDeleteFails(t *testing.T) {
	store := newLogStore(t)
	token := signIn(t, store)
	store.failDelete = errors.New("store down")
	c, w := sessionCtx(t, prodEnv(store), "__Host-tifa_session="+token)
	err := ClearSession(c)
	if err == nil || !strings.Contains(err.Error(), "until it expires") {
		t.Fatalf("error %v", err)
	}
	if cookies := setCookies(w, "__Host-tifa_session"); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Errorf("cookie not cleared: %+v", cookies)
	}
	if Session[testSession](c) != nil {
		t.Error("session stays for this request")
	}
}

func TestMalformedCookieIsNotLookedUp(t *testing.T) {
	for _, value := range []string{"short", strings.Repeat("a", 44), strings.Repeat("a", 42) + "=", strings.Repeat("a", 42) + "+"} {
		store := newLogStore(t)
		c, w := sessionCtx(t, prodEnv(store), "__Host-tifa_session="+value)
		if len(store.gets) != 0 {
			t.Errorf("%q: the store was asked", value)
		}
		if Session[testSession](c) != nil {
			t.Errorf("%q: a session", value)
		}
		if cookies := setCookies(w, "__Host-tifa_session"); len(cookies) != 1 || cookies[0].MaxAge != -1 {
			t.Errorf("%q: cookie not cleared: %+v", value, cookies)
		}
		if !NoStore(c) {
			t.Errorf("%q: the answer may be stored", value)
		}
	}
}

func TestNoCookieMeansNoLookup(t *testing.T) {
	store := newLogStore(t)
	c, w := sessionCtx(t, prodEnv(store), "other=1")
	if len(store.gets) != 0 || Session[testSession](c) != nil || len(w.Header().Values("Set-Cookie")) != 0 {
		t.Error("a request without the cookie touched the session")
	}
}

func TestUnknownTokenIsAnonymousAndDropsTheCookie(t *testing.T) {
	store := newLogStore(t)
	token := strings.Repeat("A", 43)
	c, w := sessionCtx(t, prodEnv(store), "__Host-tifa_session="+token)
	if len(store.gets) != 1 || store.gets[0] != hashOf(token) {
		t.Fatalf("gets %v", store.gets)
	}
	if Session[testSession](c) != nil {
		t.Error("a session")
	}
	if cookies := setCookies(w, "__Host-tifa_session"); len(cookies) != 1 || cookies[0].MaxAge != -1 || !NoStore(c) {
		t.Errorf("cookie not cleared: %+v", cookies)
	}
}

func TestStoreErrorOnGet(t *testing.T) {
	store := newLogStore(t)
	store.failGet = errors.New("store down")
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Cookie", "__Host-tifa_session="+strings.Repeat("A", 43))
	c := New(httptest.NewRecorder(), r, "/", 1<<20)
	err := AttachSession(c, prodEnv(store))
	if err == nil || !strings.Contains(err.Error(), "store down") {
		t.Fatalf("error %v", err)
	}
	if !NoStore(c) {
		t.Error("the error answer may be stored")
	}
}

func TestInvalidRecordIsDeletedAndTheCookieCleared(t *testing.T) {
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	cases := map[string]string{
		"not json":     `nope`,
		"unknown v":    `{"v":2,"expires":"` + future + `","data":{"userId":1}}`,
		"no v":         `{"expires":"` + future + `","data":{"userId":1}}`,
		"no expires":   `{"v":1,"data":{"userId":1}}`,
		"bad expires":  `{"v":1,"expires":"tomorrow","data":{"userId":1}}`,
		"expired":      `{"v":1,"expires":"` + past + `","data":{"userId":1}}`,
		"no data":      `{"v":1,"expires":"` + future + `"}`,
		"null data":    `{"v":1,"expires":"` + future + `","data":null}`,
		"wrong S type": `{"v":1,"expires":"` + future + `","data":"a string"}`,
	}
	for name, record := range cases {
		t.Run(name, func(t *testing.T) {
			store := newLogStore(t)
			token := strings.Repeat("B", 43)
			store.MemoryStore.Put(context.Background(), hashOf(token), []byte(record), time.Now().Add(time.Hour))
			c, w := sessionCtx(t, prodEnv(store), "__Host-tifa_session="+token)
			if Session[testSession](c) != nil {
				t.Fatal("a session")
			}
			if _, found, _ := store.MemoryStore.Get(context.Background(), hashOf(token)); found {
				t.Error("the record stayed")
			}
			if cookies := setCookies(w, "__Host-tifa_session"); len(cookies) != 1 || cookies[0].MaxAge != -1 {
				t.Errorf("cookie not cleared: %+v", cookies)
			}
			if !NoStore(c) {
				t.Error("answer may be stored")
			}
			// Clearing again has nothing left to do.
			if err := ClearSession(c); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestSessionExpiresAfterMaxAge(t *testing.T) {
	store := newLogStore(t)
	token := signIn(t, store)
	defer func(f func() time.Time) { sessionNow = f }(sessionNow)
	sessionNow = func() time.Time { return time.Now().Add(30*24*time.Hour - time.Minute) }
	c, _ := sessionCtx(t, prodEnv(store), "__Host-tifa_session="+token)
	if Session[testSession](c) == nil {
		t.Fatal("gone before 30 days")
	}
	sessionNow = func() time.Time { return time.Now().Add(30*24*time.Hour + time.Minute) }
	c, _ = sessionCtx(t, prodEnv(store), "__Host-tifa_session="+token)
	if Session[testSession](c) != nil {
		t.Fatal("still there after 30 days")
	}
}

func TestSessionIsOnePointerForConcurrentLoads(t *testing.T) {
	store := newLogStore(t)
	token := signIn(t, store)
	c, _ := sessionCtx(t, prodEnv(store), "__Host-tifa_session="+token)
	var wg sync.WaitGroup
	got := make([]*testSession, 16)
	for i := range got {
		wg.Add(1)
		go func() { defer wg.Done(); got[i] = Session[testSession](c) }()
	}
	wg.Wait()
	for _, p := range got {
		if p == nil || p != got[0] {
			t.Fatalf("pointers differ: %p %p", p, got[0])
		}
	}
}

func TestSessionOfAnotherTypePanics(t *testing.T) {
	store := newLogStore(t)
	token := signIn(t, store)
	c, _ := sessionCtx(t, prodEnv(store), "__Host-tifa_session="+token)
	Session[testSession](c)
	type other struct{ UserID int64 }
	if msg := panicOf(func() { Session[other](c) }); !strings.Contains(msg, "testSession") || !strings.Contains(msg, "other") {
		t.Fatalf("panic %q", msg)
	}
}

func TestWhereSessionCanBeChanged(t *testing.T) {
	store := newLogStore(t)
	token := signIn(t, store)
	cookie := "__Host-tifa_session=" + token
	// Reading works everywhere, writing only before the response is out and outside Load.
	t.Run("Load", func(t *testing.T) {
		c, w := sessionCtx(t, prodEnv(store), cookie)
		_, err := Use(c, func(lc *Ctx) (int, error) {
			if Session[testSession](lc) == nil {
				t.Error("Load cannot read")
			}
			if err := SetSession(lc, &testSession{UserID: 2}); err == nil || !strings.Contains(err.Error(), "Load") {
				t.Errorf("SetSession in Load: %v", err)
			}
			if err := ClearSession(lc); err == nil || !strings.Contains(err.Error(), "Load") {
				t.Errorf("ClearSession in Load: %v", err)
			}
			return 0, nil
		})
		if err != nil || len(w.Header().Values("Set-Cookie")) != 0 || Session[testSession](c) == nil {
			t.Errorf("err %v, the session changed", err)
		}
	})
	t.Run("after Next", func(t *testing.T) {
		c, _ := sessionCtx(t, prodEnv(store), cookie)
		SetSessionPhase(c, PhaseAfterNext)
		if Session[testSession](c) == nil {
			t.Error("cannot read")
		}
		if err := SetSession(c, &testSession{}); err == nil || !strings.Contains(err.Error(), "next") {
			t.Errorf("SetSession: %v", err)
		}
		if err := ClearSession(c); err == nil || !strings.Contains(err.Error(), "next") {
			t.Errorf("ClearSession: %v", err)
		}
	})
	t.Run("OnError", func(t *testing.T) {
		c, _ := sessionCtx(t, prodEnv(store), cookie)
		SetSessionPhase(c, PhaseError)
		if err := SetSession(c, &testSession{}); err == nil || !strings.Contains(err.Error(), "OnError") {
			t.Errorf("SetSession: %v", err)
		}
	})
	t.Run("committed", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Cookie", cookie)
		c := New(&committedWriter{ResponseRecorder: httptest.NewRecorder()}, r, "/", 1<<20)
		AttachSession(c, prodEnv(store))
		if err := SetSession(c, &testSession{}); err == nil || !strings.Contains(err.Error(), "response") {
			t.Errorf("SetSession: %v", err)
		}
		if err := ClearSession(c); err == nil || !strings.Contains(err.Error(), "response") {
			t.Errorf("ClearSession: %v", err)
		}
	})
	t.Run("handler and before Next", func(t *testing.T) {
		c, _ := sessionCtx(t, prodEnv(store), cookie)
		if err := SetSession(c, &testSession{UserID: 3}); err != nil {
			t.Error(err)
		}
	})
}

type committedWriter struct{ *httptest.ResponseRecorder }

func (committedWriter) Committed() bool { return true }

func TestNoSessionStoreIsAnError(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	c := New(httptest.NewRecorder(), r, "/", 1<<20)
	if Session[testSession](c) != nil {
		t.Error("session without a store")
	}
	if err := SetSession(c, &testSession{}); err == nil {
		t.Error("SetSession without a store")
	}
}

func TestInjectedSession(t *testing.T) {
	c, w := sessionCtx(t, prodEnv(newLogStore(t)), "")
	in := &testSession{UserID: 5}
	InjectSession(c, in)
	if got := Session[testSession](c); got != in {
		t.Fatalf("session %p, want the injected %p", got, in)
	}
	if err := ClearSession(c); err != nil {
		t.Fatal(err)
	}
	if Session[testSession](c) != nil || len(w.Header().Values("Set-Cookie")) != 0 {
		t.Error("injected session not cleared quietly")
	}
}

// A record that does not fit S is found when Session is first called, which may be after the
// response went out. The headers are final then: only the record is deleted.
func TestFirstReadAfterTheResponseIsOutLeavesTheHeaders(t *testing.T) {
	store := newLogStore(t)
	token := strings.Repeat("C", 43)
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	store.MemoryStore.Put(context.Background(), hashOf(token), []byte(`{"v":1,"expires":"`+future+`","data":"a string"}`), time.Now().Add(time.Hour))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Cookie", "__Host-tifa_session="+token)
	rec := httptest.NewRecorder()
	c := New(&committedWriter{ResponseRecorder: rec}, r, "/", 1<<20)
	if err := AttachSession(c, prodEnv(store)); err != nil {
		t.Fatal(err)
	}
	if Session[testSession](c) != nil {
		t.Fatal("a session")
	}
	if _, found, _ := store.MemoryStore.Get(context.Background(), hashOf(token)); found {
		t.Error("the record stayed")
	}
	if len(rec.Header().Values("Set-Cookie")) != 0 || NoStore(c) {
		t.Errorf("headers changed after the response was out: %v", rec.Header())
	}
	// The next request holds the same cookie, finds no record and drops it.
	c2, w2 := sessionCtx(t, prodEnv(store), "__Host-tifa_session="+token)
	if cookies := setCookies(w2, "__Host-tifa_session"); Session[testSession](c2) != nil || len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Errorf("the next request did not drop the cookie: %+v", cookies)
	}
}
