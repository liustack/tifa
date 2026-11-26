package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// seen is what a Load saw of its request.
type seen struct {
	request              *http.Request
	query, other         string
	order, project       string
	orderOfCopy, rawPath string
}

func look(c *Ctx) seen {
	r := c.Request()
	return seen{request: r, query: c.Query("sort"), other: c.Query("utm_source"),
		order: c.Param("order"), project: c.Param("project"), orderOfCopy: r.PathValue("order"), rawPath: r.URL.Path}
}

var layoutSaw, pageSaw, helperSaw seen

func viewLayout(c *Ctx) (int, error) { layoutSaw = look(c); return 0, nil }

func viewPage(c *Ctx) (int, error) {
	pageSaw = look(c)
	_, err := Use(c, viewHelper)
	return 0, err
}

func viewHelper(c *Ctx) (int, error) { helperSaw = look(c); return 0, nil }

func keep(names ...string) func(*url.URL) string {
	return func(u *url.URL) string {
		out := url.Values{}
		for _, n := range names {
			if v, ok := u.Query()[n]; ok {
				out[n] = v
			}
		}
		return out.Encode()
	}
}

func newViewCtx() (*Ctx, *http.Request) {
	r := httptest.NewRequest("POST", "/projects/a/orders/7?sort=name&utm_source=mail", strings.NewReader("secret=1"))
	r.SetPathValue("project", "a")
	r.SetPathValue("order", "7")
	c := New(httptest.NewRecorder(), r, "/projects/{project}/orders/{order}", 1024)
	SetLoadViews(c, map[uintptr]*LoadView{
		reflect.ValueOf(viewLayout).Pointer(): {RawQuery: keep("tab"), Hide: []string{"order"}},
		reflect.ValueOf(viewPage).Pointer():   {RawQuery: keep("sort")},
	})
	return c, r
}

// In a Load or Layout, the request is a copy with the query parameters the function declared, no
// body, and for a Layout none of the path parameters of the folders below it. A function the
// server has no view for sees what the Load that called it sees.
func TestALoadSeesACopyOfTheRequest(t *testing.T) {
	c, r := newViewCtx()
	if c.Request() != r {
		t.Fatal("a handler does not get the request itself")
	}
	if _, err := Use(c, viewLayout); err != nil {
		t.Fatal(err)
	}
	if _, err := Use(c, viewPage); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]seen{"Layout": layoutSaw, "page": pageSaw, "helper": helperSaw} {
		if s.request == r {
			t.Fatalf("%s: got the request itself", name)
		}
		if s.request.Body != http.NoBody || s.request.ContentLength != 0 || s.request.Form != nil {
			t.Errorf("%s: the copy has a body", name)
		}
		if s.rawPath != "/projects/a/orders/7" || s.project != "a" || s.other != "" {
			t.Errorf("%s: %+v", name, s)
		}
	}
	if layoutSaw.query != "" || layoutSaw.order != "" || layoutSaw.orderOfCopy != "" || layoutSaw.request.URL.RawQuery != "" {
		t.Errorf("the Layout saw what is not its own: %+v, query %q", layoutSaw, layoutSaw.request.URL.RawQuery)
	}
	for name, s := range map[string]seen{"page": pageSaw, "helper": helperSaw} {
		if s.query != "name" || s.order != "7" || s.orderOfCopy != "7" || s.request.URL.RawQuery != "sort=name" || s.request.RequestURI != "/projects/a/orders/7?sort=name" {
			t.Errorf("%s: %+v, query %q", name, s, s.request.URL.RawQuery)
		}
	}
	if r.URL.RawQuery != "sort=name&utm_source=mail" || r.PathValue("order") != "7" || c.Param("order") != "7" || c.Query("utm_source") != "mail" {
		t.Error("the copy changed the request")
	}
}

// A function the server has no view for, run from a handler, OnRequest or a test, sees the whole
// query, still in a copy without the body.
func TestALoadWithoutAViewSeesTheWholeQuery(t *testing.T) {
	c := newQueryCtx("/list?sort=name&page=2")
	var got *http.Request
	var sort []string
	if _, err := Use(c, func(c *Ctx) (int, error) {
		got, sort = c.Request(), c.QueryValues("sort")
		var v struct {
			Page int `json:"page"`
		}
		return v.Page, c.BindQuery(&v)
	}); err != nil {
		t.Fatal(err)
	}
	if got == c.Request() || got.URL.RawQuery != "sort=name&page=2" || len(sort) != 1 || got.Body != http.NoBody {
		t.Errorf("got %v %q %v", got.URL, sort, got.Body)
	}
}

// Loads that run at once each get their own copy: one that changes its copy does not change the
// copy of another, nor the request.
func TestLoadsDoNotShareTheirCopies(t *testing.T) {
	c, r := newViewCtx()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, _ = Use(c, func(c *Ctx) (int, error) {
				cp := c.Request()
				cp.SetPathValue("project", "b")
				cp.URL.RawQuery = "x=1"
				cp.Header.Set("X-Load", "1")
				return 0, nil
			})
		})
	}
	wg.Wait()
	for range 8 {
		wg.Go(func() { _, _ = Use(c, viewLayout) })
	}
	wg.Wait()
	if layoutSaw.project != "a" || r.PathValue("project") != "a" || r.URL.RawQuery != "sort=name&utm_source=mail" || r.Header.Get("X-Load") != "" {
		t.Errorf("a copy changed what another sees: %+v", layoutSaw)
	}
}

// WithContext in a Load gives a Ctx whose copy carries the new context.
func TestACopyFollowsWithContext(t *testing.T) {
	c, _ := newViewCtx()
	type key struct{}
	_, err := Use(c, func(c *Ctx) (int, error) {
		first := c.Request()
		d := c.WithContext(context.WithValue(c.Context(), key{}, "v"))
		if d.Request().Context().Value(key{}) != "v" || d.Request().Body != http.NoBody {
			t.Error("the copy of WithContext lost its context")
		}
		if c.Request() == d.Request() || c.Request().Context().Value(key{}) != nil || first.Context().Value(key{}) != nil {
			t.Error("WithContext changed the copy of the Ctx it came from")
		}
		return 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

var selfErr error

func selfLoad(c *Ctx) (int, error) {
	_, selfErr = Use(c, selfLoad)
	return 0, nil
}

// A Load cannot write the response, which tifa writes from what the Loads return, and cannot wait
// for its own result.
func TestALoadCannotWriteNorUseItself(t *testing.T) {
	w := httptest.NewRecorder()
	c := New(w, httptest.NewRequest("GET", "/", nil), "/", 1024)
	_, _ = Use(c, func(c *Ctx) (int, error) {
		for name, err := range map[string]error{"JSON": c.JSON(200, 1), "Text": c.Text(200, "x"), "NoContent": c.NoContent(204)} {
			if err == nil {
				t.Errorf("c.%s in a Load wrote the response", name)
			}
		}
		return 0, nil
	})
	if w.Body.Len() != 0 || w.Code != 200 || len(w.Header()) != 0 {
		t.Errorf("the response: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	if _, _ = Use(c, selfLoad); selfErr == nil || !strings.Contains(selfErr.Error(), "cannot Use itself") {
		t.Errorf("a Load that uses itself: %v", selfErr)
	}
}
