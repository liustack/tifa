package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
	writes := []struct {
		name  string
		write func(*Ctx) error
	}{
		{"JSON", func(c *Ctx) error { return c.JSON(200, map[string]int{"n": 1}) }},
		{"Text", func(c *Ctx) error { return c.Text(200, "hi") }},
		{"NoContent", func(c *Ctx) error { return c.NoContent(204) }},
	}
	for _, tc := range writes {
		// A fresh request each time: Use caches by Load, and these Loads share one code address.
		c, w := newCtx("GET", "", "", 1024)
		ran := false
		_, err := Use(c, func(c *Ctx) (int, error) {
			ran = true
			c.SetHeader("X-Load", "ok")
			return 0, tc.write(c)
		})
		if !ran || err == nil {
			t.Errorf("%s inside Load: ran %v, err %v", tc.name, ran, err)
		}
		if w.Body.Len() != 0 || w.Code != 200 || w.Header().Get("X-Load") != "ok" {
			t.Errorf("%s inside Load wrote %d %q or lost the header", tc.name, w.Code, w.Body.String())
		}
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
		if err := c.SetBodyLimit(16); err != nil {
			t.Fatal(err)
		}
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
	err := Redirect("/login", 302)
	var redirect *RedirectError
	if !errors.As(err, &redirect) || redirect.URL != "/login" || redirect.Status != 302 {
		t.Fatalf("got %v", err)
	}
	if msg := panicOf(func() { Redirect("/login", 200) }); !strings.Contains(msg, "200") {
		t.Fatalf("a non-3xx status must panic, recovered %q", msg)
	}
}

type userKey struct{}

func TestWithContextReturnsACopyAndKeepsTheOriginal(t *testing.T) {
	c, _ := newCtx("GET", "", "", 1024)
	d := c.WithContext(context.WithValue(c.Context(), userKey{}, "lin"))
	if d == c {
		t.Fatal("WithContext returned the same Ctx")
	}
	if c.Context().Value(userKey{}) != nil {
		t.Fatal("WithContext changed the original")
	}
	if d.Context().Value(userKey{}) != "lin" || d.Param("id") != c.Param("id") || d.Query("tab") != "all" || d.Pattern() != "/orders/{id}" {
		t.Fatal("the copy lost the request")
	}
}

var sharedLoadRuns int

func sharedLoad(*Ctx) (int, error) { sharedLoadRuns++; return 1, nil }

// A Load that ran for the Ctx before WithContext does not run again for the copy.
func TestWithContextCopiesShareTheRequestState(t *testing.T) {
	c, _ := newCtx("GET", "", "", 1024)
	sharedLoadRuns = 0
	d := c.WithContext(context.WithValue(c.Context(), userKey{}, "lin"))
	for _, x := range []*Ctx{c, d} {
		if _, err := Use(x, sharedLoad); err != nil {
			t.Fatal(err)
		}
	}
	if sharedLoadRuns != 1 {
		t.Fatalf("Load ran %d times", sharedLoadRuns)
	}
}

func TestBindJSONReadsToEndOfBody(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
	}{
		{"second value", `{"name":"a"} {"name":"b"}`, 400},
		{"garbage", `{"name":"a"} junk`, 400},
		{"over limit after value", `{"name":"a"}` + strings.Repeat(" ", 64), 413},
		{"trailing space", "{\"name\":\"a\"}\n  ", 0},
	}
	for _, tc := range cases {
		c, _ := newCtx("POST", tc.body, "application/json", 32)
		var in input
		err := c.Bind(&in)
		var e Error
		if tc.status == 0 && err != nil || tc.status != 0 && (!errors.As(err, &e) || e.Status != tc.status) {
			t.Errorf("%s: got %v, want status %d", tc.name, err, tc.status)
		}
	}
}

func TestBindFormUsesConfiguredLimitAboveTenMiB(t *testing.T) {
	body := "name=" + strings.Repeat("x", 11<<20)
	c, _ := newCtx("POST", body, "application/x-www-form-urlencoded", 16<<20)
	var in input
	if err := c.Bind(&in); err != nil || len(in.Name) != 11<<20 {
		t.Fatalf("form within the configured limit: %v", err)
	}
	c, _ = newCtx("POST", body, "application/x-www-form-urlencoded", 16<<20)
	if err := c.SetBodyLimit(1 << 20); err != nil {
		t.Fatal(err)
	}
	var e Error
	if err := c.Bind(&in); !errors.As(err, &e) || e.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("form over a lowered limit: %v", err)
	}
}

func TestSetBodyLimitFailsOnceReadingStarted(t *testing.T) {
	c, _ := newCtx("POST", `{"name":"a"}`, "application/json", 1024)
	if err := c.SetBodyLimit(64); err != nil {
		t.Fatalf("before reading: %v", err)
	}
	if _, err := c.Request().Body.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	if err := c.SetBodyLimit(1 << 20); err == nil {
		t.Fatal("SetBodyLimit after reading started must fail")
	}
	r := httptest.NewRequest("POST", "/", nil)
	r.Body = io.NopCloser(strings.NewReader("")) // a body that ends at once, unlike http.NoBody
	c = New(httptest.NewRecorder(), r, "/", 1024)
	if n, err := c.Request().Body.Read(make([]byte, 8)); n != 0 || err != io.EOF {
		t.Fatalf("empty body read %d, %v", n, err)
	}
	if err := c.SetBodyLimit(1 << 20); err == nil {
		t.Fatal("a Read that hit EOF with no bytes still starts reading")
	}
	c, _ = newCtx("GET", "", "", 1024)
	if err := c.SetBodyLimit(64); err != nil {
		t.Fatalf("request without a body: %v", err)
	}
}

type filter struct {
	Status string   `json:"status"`
	Page   int      `json:"page"`
	Tags   []string `json:"tags"`
	Done   bool
	Skip   string    `json:"-"`
	Ptr    uintptr   `json:"ptr"`
	Ptrs   []uintptr `json:"ptrs"`
}

func TestBindQuery(t *testing.T) {
	r := httptest.NewRequest("GET", "/orders?status=open&page=2&tags=a&tags=b&Done=true&Skip=x&other=1&ptr=7&ptrs=8", nil)
	c := New(httptest.NewRecorder(), r, "/orders", 1024)
	var in filter
	if err := c.BindQuery(&in); err != nil || in.Status != "open" || in.Page != 2 || len(in.Tags) != 2 || !in.Done || in.Skip != "" ||
		in.Ptr != 7 || len(in.Ptrs) != 1 || in.Ptrs[0] != 8 {
		t.Fatalf("%v %+v", err, in)
	}
	r = httptest.NewRequest("GET", "/orders?page=two", nil)
	c = New(httptest.NewRecorder(), r, "/orders", 1024)
	var e Error
	if err := c.BindQuery(&in); !errors.As(err, &e) || e.Status != 400 || e.Message != `invalid query parameter "page"` {
		t.Fatalf("got %v", err)
	}
	// url.URL.Query drops what it cannot parse, which would decode as a missing field.
	for _, query := range []string{"status=%ZZ", "status=a;b", "page=1&%zz"} {
		r = httptest.NewRequest("GET", "/orders", nil)
		r.URL.RawQuery = query
		c = New(httptest.NewRecorder(), r, "/orders", 1024)
		if err := c.BindQuery(&filter{}); !errors.As(err, &e) || e.Status != 400 || e.Message != "invalid query string" {
			t.Errorf("?%s: got %v", query, err)
		}
	}
}

func TestQueryAll(t *testing.T) {
	r := httptest.NewRequest("GET", "/notes?tag=a&tag=b&empty=&tag=c", nil)
	c := New(httptest.NewRecorder(), r, "/notes", 1024)
	if got := c.QueryAll("tag"); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("QueryAll(tag) = %q", got)
	}
	if got := c.QueryAll("empty"); !slices.Equal(got, []string{""}) {
		t.Errorf("QueryAll(empty) = %q", got)
	}
	if got := c.QueryAll("none"); got != nil {
		t.Errorf("QueryAll(none) = %#v, want nil", got)
	}
	if got := c.Query("tag"); got != "a" {
		t.Errorf("Query(tag) = %q, want the first value", got)
	}
}

// A whole body states its length, so net/http sends it without chunked framing and a
// compressing writer above it can decide at the first write.
func TestWholeBodiesCarryTheirLength(t *testing.T) {
	for name, write := range map[string]func(*Ctx) error{
		"JSON": func(c *Ctx) error { return c.JSON(200, map[string]string{"note": strings.Repeat("长", 3000)}) },
		"Text": func(c *Ctx) error { return c.Text(200, strings.Repeat("x", 5000)) },
	} {
		c, w := newCtx("GET", "", "", 1024)
		if err := write(c); err != nil {
			t.Fatal(err)
		}
		if got, want := w.Header().Get("Content-Length"), strconv.Itoa(w.Body.Len()); got != want {
			t.Errorf("%s: Content-Length %q, body %s bytes", name, got, want)
		}
	}
}

// The copies of a Ctx share one body limit, whichever of them SetBodyLimit is called on and
// whichever reads.
func TestBodyLimitIsSharedByCopies(t *testing.T) {
	for _, tc := range []struct {
		name          string
		bodySize      int
		limit         int64
		setOnOriginal bool
		wantErr       bool
	}{
		{"lower on the original, read from the copy", 20, 10, true, true},
		{"lower on the copy, read from the original", 20, 10, false, true},
		{"higher on the original, read from the copy", 100, 1000, true, false},
		{"higher on the copy, read from the original", 100, 1000, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newCtx("POST", strings.Repeat("x", tc.bodySize), "", 50)
			d := c.WithContext(context.WithValue(c.Context(), userKey{}, "lin"))
			set, read := d, c
			if tc.setOnOriginal {
				set, read = c, d
			}
			if err := set.SetBodyLimit(tc.limit); err != nil {
				t.Fatal(err)
			}
			_, err := io.ReadAll(read.Request().Body)
			var tooLarge *http.MaxBytesError
			if tc.wantErr != errors.As(err, &tooLarge) || tc.wantErr && tooLarge.Limit != tc.limit {
				t.Fatalf("read: %v", err)
			}
		})
	}
	// Reading from either copy ends the time to change the limit, for both.
	c, _ := newCtx("POST", strings.Repeat("x", 100), "", 50)
	d := c.WithContext(context.WithValue(c.Context(), userKey{}, "lin"))
	_, _ = io.ReadAll(d.Request().Body)
	if err := c.SetBodyLimit(1000); err == nil {
		t.Fatal("SetBodyLimit worked after the copy read")
	}
}

// WithContext and SetBodyLimit may run at the same time on different goroutines (a Load that
// calls WithContext, say). -race finds no unsynchronized read of the body, and every copy made
// after SetBodyLimit returned has the new reader.
func TestWithContextRacesWithSetBodyLimit(t *testing.T) {
	c, _ := newCtx("POST", strings.Repeat("x", 100), "", 50)
	var set atomic.Bool
	var lateMade atomic.Int64
	stop := make(chan struct{})
	made := make(chan []*Ctx, 1)
	go func() {
		var late []*Ctx // copies that began after SetBodyLimit returned
		for {
			select {
			case <-stop:
				made <- late
				return
			default:
			}
			wasSet := set.Load()
			d := c.WithContext(context.WithValue(c.Context(), userKey{}, "lin"))
			if wasSet {
				late = append(late, d)
				lateMade.Add(1)
			}
		}
	}()
	time.Sleep(time.Millisecond)
	if err := c.SetBodyLimit(1000); err != nil {
		t.Fatal(err)
	}
	set.Store(true)
	// Wait for a copy made after SetBodyLimit instead of sleeping a fixed time: on a loaded
	// machine the goroutine may not run for a while, and a fixed sleep then finds no late copy.
	for deadline := time.Now().Add(5 * time.Second); lateMade.Load() == 0; {
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Microsecond)
	}
	close(stop)
	late := <-made
	if len(late) == 0 {
		t.Fatal("no copy was made after SetBodyLimit")
	}
	for i, d := range late {
		if d.Request().Body != c.Request().Body {
			t.Fatalf("copy %d of %d made after SetBodyLimit has an older body reader", i, len(late))
		}
	}
}
