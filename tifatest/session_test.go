package tifatest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/tifatest"
)

type account struct {
	UserID int64  `json:"userId"`
	Name   string `json:"name"`
}

func TestNewCtxCanSignInAndOut(t *testing.T) {
	c, w := tifatest.NewCtx(t, httptest.NewRequest("POST", "/login", nil))
	if tifa.Session[account](c) != nil {
		t.Fatal("a session before sign-in")
	}
	if err := tifa.SetSession(c, &account{UserID: 1, Name: "Ann"}); err != nil {
		t.Fatal(err)
	}
	if got := tifa.Session[account](c); got == nil || got.Name != "Ann" {
		t.Fatalf("session %+v", got)
	}
	if !strings.HasPrefix(w.Header().Get("Set-Cookie"), "__Host-tifa_session=") {
		t.Fatalf("Set-Cookie %q", w.Header().Get("Set-Cookie"))
	}
	if err := tifa.ClearSession(c); err != nil {
		t.Fatal(err)
	}
	if tifa.Session[account](c) != nil || !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("after sign-out: %q", w.Header().Get("Set-Cookie"))
	}
}

// The store belongs to the test, so a cookie from one Ctx signs in the next Ctx of the same test.
func TestNewCtxStoreSurvivesBetweenRequestsOfATest(t *testing.T) {
	c, w := tifatest.NewCtx(t, httptest.NewRequest("POST", "/login", nil))
	tifa.SetSession(c, &account{UserID: 3, Name: "Bo"})
	cookie := (&http.Response{Header: w.Header()}).Cookies()[0]
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookie)
	next, _ := tifatest.NewCtx(t, r)
	if got := tifa.Session[account](next); got == nil || got.UserID != 3 {
		t.Fatalf("session %+v", got)
	}
	// Another test does not see it.
	t.Run("sub", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.AddCookie(cookie)
		other, _ := tifatest.NewCtx(t, r)
		if tifa.Session[account](other) != nil {
			t.Error("the store is shared between tests")
		}
	})
}

func TestWithSessionOnlyInjectsRequestState(t *testing.T) {
	c, w := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
	in := &account{UserID: 9}
	tifatest.WithSession(c, in)
	if got := tifa.Session[account](c); got != in {
		t.Fatalf("session %p, want %p", got, in)
	}
	if len(w.Header().Values("Set-Cookie")) != 0 {
		t.Error("WithSession wrote a cookie")
	}
	// A fresh Ctx of the same test has no session: nothing reached the store.
	other, _ := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
	if tifa.Session[account](other) != nil {
		t.Error("WithSession reached the store")
	}
}

func TestMemoryStoreIsAValidStore(t *testing.T) {
	m := tifa.NewMemoryStore(1 << 20)
	defer m.Close()
	tifatest.TestSessionStore(t, m)
}

// brokenStore keeps the slice it is given and never forgets.
type brokenStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (s *brokenStore) Get(ctx context.Context, k string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.data[k]
	return d, ok, nil
}
func (s *brokenStore) Put(ctx context.Context, k string, d []byte, e time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[k] = d
	return nil
}
func (s *brokenStore) Delete(ctx context.Context, k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, k)
	return nil
}

// failures records the failures TestSessionStore reports, in place of failing the test.
type failures struct {
	testing.TB
	msgs []string
}

func (f *failures) Helper() {}
func (f *failures) Errorf(format string, a ...any) {
	f.msgs = append(f.msgs, strings.TrimSpace(format))
}
func (f *failures) Fatalf(format string, a ...any) {
	f.msgs = append(f.msgs, strings.TrimSpace(format))
}

func TestTestSessionStoreFindsABrokenStore(t *testing.T) {
	f := &failures{TB: t}
	tifatest.TestSessionStore(f, &brokenStore{data: map[string][]byte{}})
	joined := strings.Join(f.msgs, "\n")
	for _, want := range []string{"expired", "slice", "canceled"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no failure about %q in:\n%s", want, joined)
		}
	}
}

// A 1xx answer, such as 103 Early Hints, comes before the response and does not fix its headers,
// on the server and in the recorder: the session cookie set after it is in the result.
func TestInformationalStatusLeavesTheHeadersOpen(t *testing.T) {
	c, w := tifatest.NewCtx(t, httptest.NewRequest("POST", "/login", nil))
	c.Writer().Header().Set("Link", "</assets/app.css>; rel=preload; as=style")
	c.Writer().WriteHeader(http.StatusEarlyHints)
	if err := tifa.SetSession(c, &account{UserID: 1}); err != nil {
		t.Fatal(err)
	}
	if err := c.JSON(200, "in"); err != nil {
		t.Fatal(err)
	}
	res := w.Result()
	if res.StatusCode != 200 || len(res.Cookies()) != 1 || res.Cookies()[0].Name != "__Host-tifa_session" {
		t.Fatalf("status %d, cookies %v", res.StatusCode, res.Cookies())
	}
}

func TestSessionCannotChangeOnceTheResponseIsOut(t *testing.T) {
	writes := map[string]func(c *tifa.Ctx){
		"body":   func(c *tifa.Ctx) { c.Text(200, "hi") },
		"status": func(c *tifa.Ctx) { c.NoContent(204) },
		"flush":  func(c *tifa.Ctx) { http.NewResponseController(c.Writer()).Flush() },
	}
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			c, w := tifatest.NewCtx(t, httptest.NewRequest("POST", "/", nil))
			if err := tifa.SetSession(c, &account{UserID: 1}); err != nil {
				t.Fatal(err)
			}
			cookie := (&http.Response{Header: w.Header()}).Cookies()[0]
			r := httptest.NewRequest("POST", "/", nil)
			r.AddCookie(cookie)
			c, w = tifatest.NewCtx(t, r)
			write(c)
			if err := tifa.SetSession(c, &account{UserID: 2}); err == nil {
				t.Error("SetSession after the response")
			}
			if err := tifa.ClearSession(c); err == nil {
				t.Error("ClearSession after the response")
			}
			if got := tifa.Session[account](c); got == nil || got.UserID != 1 {
				t.Errorf("session %+v", got)
			}
			if len(w.Header().Values("Set-Cookie")) != 0 {
				t.Errorf("Set-Cookie %v", w.Header().Values("Set-Cookie"))
			}
		})
	}
}
