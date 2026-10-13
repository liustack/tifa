package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func newCtx(method, body, contentType string, limit int64) (*Ctx, *httptest.ResponseRecorder) {
	r := httptest.NewRequest(method, "/orders/1?tab=all", strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	return New(w, r, "/orders/{id}", limit), w
}

func TestLoadCannotWriteResponseBody(t *testing.T) {
	c, w := newCtx("GET", "", "", 1024)
	writes := map[string]func(*Ctx) error{
		"JSON":      func(c *Ctx) error { return c.JSON(200, map[string]int{"n": 1}) },
		"Text":      func(c *Ctx) error { return c.Text(200, "hi") },
		"NoContent": func(c *Ctx) error { return c.NoContent(204) },
	}
	for name, write := range writes {
		_, err := Use(c, func(c *Ctx) (int, error) {
			c.SetHeader("X-Load", "ok")
			return 0, write(c)
		})
		if err == nil {
			t.Errorf("%s inside Load returned nil", name)
		}
	}
	if w.Body.Len() != 0 || w.Header().Get("X-Load") != "ok" {
		t.Fatalf("Load wrote body %q or lost header", w.Body.String())
	}
}

func TestUseRunsEachLoadOncePerRequest(t *testing.T) {
	c, _ := newCtx("GET", "", "", 1024)
	var calls atomic.Int32
	release := make(chan struct{})
	load := func(c *Ctx) (string, error) {
		calls.Add(1)
		<-release
		return c.Param("id") + "loaded", nil
	}
	var wg sync.WaitGroup
	results := make([]string, 4)
	for i := range results {
		wg.Go(func() {
			v, err := Use(c, load)
			if err != nil {
				t.Error(err)
			}
			results[i] = v
		})
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("load ran %d times", calls.Load())
	}
	for _, v := range results {
		if v != "loaded" {
			t.Fatalf("waiter got %q", v)
		}
	}
}

func TestUseRejectsOwnLoad(t *testing.T) {
	c, _ := newCtx("GET", "", "", 1024)
	var self func(*Ctx) (int, error)
	self = func(c *Ctx) (int, error) { return Use(c, self) }
	if _, err := Use(c, self); err == nil {
		t.Fatal("Use of the running Load must fail")
	}
}

func TestUseTurnsPanicIntoError(t *testing.T) {
	c, _ := newCtx("GET", "", "", 1024)
	load := func(*Ctx) (int, error) { panic("boom") }
	if _, err := Use(c, load); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("got %v", err)
	}
	if _, err := Use(c, load); err == nil {
		t.Fatal("second Use must see the stored error")
	}
}

type input struct {
	Name  string   `json:"name"`
	Count int      `json:"count"`
	Tags  []string `form:"tag"`
	Ready bool     `json:"ready"`
}

func TestBindJSONAndForm(t *testing.T) {
	c, _ := newCtx("POST", `{"name":"林夏","count":2}`, "application/json; charset=utf-8", 1024)
	var in input
	if err := c.Bind(&in); err != nil || in.Name != "林夏" || in.Count != 2 {
		t.Fatalf("%v %+v", err, in)
	}
	c, _ = newCtx("POST", "name=a&count=3&tag=x&tag=y&ready=true", "application/x-www-form-urlencoded", 1024)
	in = input{}
	if err := c.Bind(&in); err != nil || in.Name != "a" || in.Count != 3 || len(in.Tags) != 2 || !in.Ready {
		t.Fatalf("%v %+v", err, in)
	}
}

func TestBindReportsClientErrors(t *testing.T) {
	cases := []struct {
		body, contentType string
		status            int
	}{
		{`{"name":`, "application/json", 400},
		{"count=many", "application/x-www-form-urlencoded", 400},
		{"name", "text/plain", 415},
	}
	for _, tc := range cases {
		c, _ := newCtx("POST", tc.body, tc.contentType, 1024)
		var in input
		var e Error
		if err := c.Bind(&in); !errors.As(err, &e) || e.Status != tc.status {
			t.Errorf("%s %q: got %v", tc.contentType, tc.body, err)
		}
	}
}

func TestBindRespectsBodyLimit(t *testing.T) {
	body := `{"name":"` + strings.Repeat("x", 64) + `"}`
	for _, contentType := range []string{"application/json", "application/x-www-form-urlencoded"} {
		c, _ := newCtx("POST", body, contentType, 1024)
		c.SetBodyLimit(16)
		var in input
		var e Error
		if err := c.Bind(&in); !errors.As(err, &e) || e.Status != http.StatusRequestEntityTooLarge {
			t.Errorf("%s: got %v", contentType, err)
		}
	}
	c, _ := newCtx("POST", body, "application/json", 1024)
	var in input
	if err := c.Bind(&in); err != nil {
		t.Fatalf("within limit: %v", err)
	}
}

func TestRedirectIsAnErrorValue(t *testing.T) {
	c, _ := newCtx("GET", "", "", 1024)
	err := c.Redirect("/login", 302)
	var redirect *RedirectError
	if !errors.As(err, &redirect) || redirect.URL != "/login" || redirect.Code != 302 {
		t.Fatalf("got %v", err)
	}
	if err := c.Redirect("/login", 200); errors.As(err, &redirect) {
		t.Fatal("non-3xx code must not become a redirect")
	}
}

func TestNextOnlyInsideMiddleware(t *testing.T) {
	c, _ := newCtx("GET", "", "", 1024)
	if err := c.Next(); err == nil {
		t.Fatal("Next outside OnRequest must fail")
	}
	want := errors.New("handler failed")
	SetNext(c, func() error { return want })
	if err := c.Next(); !errors.Is(err, want) {
		t.Fatalf("Next returned %v", err)
	}
	if err := c.Next(); err == nil {
		t.Fatal("second Next must fail")
	}
}

type userKey struct{}

func TestSetContextIsSeenByHandler(t *testing.T) {
	c, _ := newCtx("GET", "", "", 1024)
	SetNext(c, func() error {
		if c.Context().Value(userKey{}) != "lin" || c.Param("id") != "" || c.Query("tab") != "all" {
			return errors.New("context lost")
		}
		return nil
	})
	c.SetContext(context.WithValue(c.Context(), userKey{}, "lin"))
	if err := c.Next(); err != nil {
		t.Fatal(err)
	}
	if c.Pattern() != "/orders/{id}" {
		t.Fatalf("pattern %q", c.Pattern())
	}
}
