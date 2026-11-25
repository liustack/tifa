package server

import (
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/liustack/tifa"
)

// A browser reads every JSON number as a double. Tifa refuses to send a number a browser would
// read as another one, so the failure is loud where the data is made, not a wrong id on a page.

type bigRow struct {
	ID int64 `json:"id"`
}

type bigRowString struct {
	ID int64 `json:"id,string"`
}

type snowflake int64

type nested struct {
	Rows []struct {
		Ref map[string]snowflake `json:"ref"`
	} `json:"rows"`
}

func roundedApp(values map[string]any) (App, *[]error) {
	app := jsonApp(values)
	var seen []error
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error {
		seen = append(seen, err)
		return tifa.Error{Status: 500, Message: "failed"}
	}
	return app, &seen
}

func TestLoadRefusesANumberABrowserRounds(t *testing.T) {
	big := int64(1<<53 + 1)
	values := map[string]any{
		"big":   bigRow{ID: big},
		"max":   uint64(math.MaxUint64),
		"named": snowflake(big),
		"any":   []any{1, "x", big},
		"nested": nested{Rows: []struct {
			Ref map[string]snowflake `json:"ref"`
		}{{Ref: map[string]snowflake{"a": snowflake(big)}}}},
		"nano": int64(1763300000123456789),
	}
	app, seen := roundedApp(values)
	s, r := newFrontServer(t, app)
	for v := range values {
		for _, target := range []string{"/p?v=" + v, "/plain?v=" + v, dataURL("/p?v="+v, "b1", "1")} {
			w := get(s, target)
			if w.Code != 500 {
				t.Errorf("%s: %d, want 500: %s", target, w.Code, w.Body.String())
			}
		}
	}
	if r.count() != 0 {
		t.Errorf("a page with a rounded number was rendered %d times", r.count())
	}
	if len(*seen) == 0 {
		t.Fatal("OnError saw nothing")
	}
	// The values above are asked in the order of a map, so the message is read from a request of its own.
	*seen = nil
	get(s, "/p?v=big")
	if len(*seen) == 0 {
		t.Fatal("OnError saw nothing for the row")
	}
	msg := (*seen)[0].Error()
	for _, want := range []string{"the Load of /page", "at id", "9007199254740993", "9007199254740992", `json:",string"`, "9007199254740991", "milliseconds"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error lacks %q: %s", want, msg)
		}
	}
}

func TestLoadSendsNumbersABrowserReadsAsTheyAre(t *testing.T) {
	values := map[string]any{
		"safe":   bigRow{ID: 1<<53 - 1},
		"ms":     bigRow{ID: 1763300000123},
		"float":  1e17,
		"floats": []float64{1e17, 1.2345678901234567e17, 1e300, -1e17},
		"string": bigRowString{ID: 1<<53 + 1},
		"keys":   map[int64]string{1<<53 + 1: "a"},
	}
	app, _ := roundedApp(values)
	s, r := newFrontServer(t, app)
	for v := range values {
		if w := get(s, "/p?v="+v); w.Code != 200 {
			t.Errorf("%s: %d %s", v, w.Code, w.Body.String())
		}
		if w := get(s, "/plain?v="+v); w.Code != 200 {
			t.Errorf("%s without SSR: %d %s", v, w.Code, w.Body.String())
		}
		if w := getData(s, "/p?v="+v, "b1", "1"); w.Code != 200 {
			t.Errorf("%s data: %d %s", v, w.Code, w.Body.String())
		}
	}
	// A field with the string option arrives whole, as a string, in the render and in the data.
	found := false
	for _, in := range r.inputs {
		found = found || len(in.Texts) == 1 && in.Texts[0] == `{"id":"9007199254740993"}`
	}
	if !found {
		t.Errorf("no render got the id as a string: %v", r.inputs)
	}
	if body := getData(s, "/p?v=string", "b1", "1").Body.String(); !strings.Contains(body, `"data":{"id":"9007199254740993"}`) {
		t.Errorf("navigation data %s", body)
	}
	if body := get(s, "/plain?v=string").Body.String(); !strings.Contains(body, `\"id\":\"9007199254740993\"`) {
		t.Errorf("the page without SSR carries %s", body)
	}
}

func TestLayoutRefusesARoundedNumber(t *testing.T) {
	app, seen := roundedApp(nil)
	app.Layouts = append(app.Layouts, Layout{ID: "app/big/layout", Path: "/big",
		Load: func(c *tifa.Ctx) (any, error) { return bigRow{ID: 1<<53 + 1}, nil }})
	app.Routes = append(app.Routes, Route{ID: "app/big/page", Path: "/big", Page: true, SSR: true, Layouts: []string{"app/big/layout"}})
	s, _ := newFrontServer(t, app)
	if w := get(s, "/big"); w.Code != 500 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if len(*seen) == 0 || !strings.Contains((*seen)[0].Error(), "Layout") {
		t.Errorf("OnError saw %v", *seen)
	}
}

// A call from the generated function of a page carries X-Tifa-Call. Any other client reads
// int64 fine, so its answer is the result as Go wrote it.
func TestHandlerResultIsCheckedOnlyForACallFromThePage(t *testing.T) {
	app, seen := roundedApp(nil)
	app.Routes = append(app.Routes, Route{ID: "app/api/x", Path: "/api/x",
		POST: Output(func(c *tifa.Ctx) (bigRow, error) { return bigRow{ID: 1<<53 + 1}, nil }),
		PUT:  Output(func(c *tifa.Ctx) (bigRow, error) { return bigRow{ID: 1<<53 - 1}, nil }),
	})
	s := newAppServer(t, app)
	w := send(s, "POST", "/api/x", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"id":9007199254740993}` {
		t.Errorf("a client without the call header: %d %s", w.Code, w.Body.String())
	}
	rec := do(s, "POST", "/api/x", callHeader, "1")
	if rec.Code != 500 {
		t.Fatalf("a call from the page: %d %s", rec.Code, rec.Body.String())
	}
	if len(*seen) != 1 || !strings.Contains((*seen)[0].Error(), "POST /api/x") || !strings.Contains((*seen)[0].Error(), "result.id") {
		t.Errorf("OnError saw %v", *seen)
	}
	if rec := do(s, "PUT", "/api/x", callHeader, "1"); rec.Code != 200 {
		t.Errorf("a safe result: %d %s", rec.Code, rec.Body.String())
	}
}

type bigUser struct {
	ID int64 `json:"id"`
}

type bigMe struct {
	ID int64 `json:"id"`
}

func (u bigUser) Exposed() bigMe { return bigMe{ID: u.ID} }

func TestSessionRefusesARoundedNumber(t *testing.T) {
	app, seen := roundedApp(map[string]any{"n": 1})
	app.Session = SessionOf[bigUser]
	app.Routes = append(app.Routes, Route{ID: "app/in", Path: "/in", POST: Input(func(c *tifa.Ctx, in loginInput) error {
		return tifa.SetSession(c, &bigUser{ID: 1<<53 + 1})
	}, (*tifa.Ctx).Bind)})
	s, _ := wireServer(t, app)
	w := postJSON(s, "/in", `{"name":"a"}`)
	c := cookieOf(w, prodCookie)
	if c == nil {
		t.Fatalf("no cookie: %d %v", w.Code, w.Header())
	}
	cookie := prodCookie + "=" + c.Value
	if w := get(s, "/p?v=n", "Cookie", cookie); w.Code != 500 {
		t.Errorf("a page: %d %s", w.Code, w.Body.String())
	}
	if len(*seen) == 0 || !strings.Contains((*seen)[0].Error(), "session") {
		t.Errorf("OnError saw %v", *seen)
	}
}

func TestDevPageShowsTheRoundedNumber(t *testing.T) {
	app, _ := roundedApp(map[string]any{"big": bigRow{ID: 1<<53 + 1}})
	s, _ := newFrontServer(t, app)
	s.dev = http.NotFoundHandler()
	w := get(s, "/p?v=big")
	if body := w.Body.String(); w.Code != 500 || !strings.Contains(body, "the Load of /page writes the number 9007199254740993 at id") {
		t.Errorf("%d %s", w.Code, body)
	}
}
