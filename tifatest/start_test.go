package tifatest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/analyze"
	"github.com/liustack/tifa/internal/codegen"
	"github.com/liustack/tifa/server"
	"github.com/liustack/tifa/tifatest"
)

type credentials struct {
	Name string `json:"name"`
	Next string `json:"next"`
}

func (in credentials) Validate() error {
	if utf8.RuneCountInString(strings.TrimSpace(in.Name)) == 0 {
		return tifa.Fields{"name": "Write your name."}
	}
	return nil
}

// events records what the hooks of signInApp saw.
var events []string

// signInApp is an app as tifaapp.App describes one: a sign-in handler with an input, a page
// behind sign-in and a handler that reads the session.
func signInApp() server.App {
	return server.App{
		Hooks: server.Hooks{
			OnStart:    func(context.Context) error { events = append(events, "start"); return nil },
			OnShutdown: func(context.Context) error { events = append(events, "shutdown"); return nil },
			OnRequest: func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
				if strings.HasPrefix(c.Request().URL.Path, "/account") && tifa.Session[account](c) == nil {
					return tifa.Redirect("/login?next="+url.QueryEscape(c.Request().URL.Path), http.StatusFound)
				}
				return next(c)
			},
		},
		Routes: []server.Route{
			{ID: "app/login/route", Path: "/login", SSR: true, Layouts: []string{},
				POST: server.Input(func(c *tifa.Ctx, in credentials) error {
					if err := tifa.SetSession(c, &account{UserID: 7, Name: in.Name}); err != nil {
						return err
					}
					return tifa.Redirect(in.Next, http.StatusSeeOther)
				}, (*tifa.Ctx).Bind)},
			{ID: "app/account/route", Path: "/account", Page: true, SSR: true, Layouts: []string{},
				Load: func(c *tifa.Ctx) (any, error) {
					return tifa.Use(c, func(c *tifa.Ctx) (string, error) { return "hello " + tifa.Session[account](c).Name, nil })
				}},
		},
		Session: server.SessionOf[account],
	}
}

func postJSON(t *testing.T, srv *httptest.Server, path, body string) *http.Response {
	t.Helper()
	res, err := srv.Client().Post(srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func get(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	res, err := srv.Client().Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

// The whole request runs: OnRequest, decoding, Validate, the session cookie, redirects and the
// data of a page, and the hooks around the server.
func TestStartRunsTheWholeRequest(t *testing.T) {
	events = nil
	t.Run("requests", func(t *testing.T) {
		srv := tifatest.Start(t, signInApp())
		if !strings.HasPrefix(srv.URL, "https://") {
			t.Errorf("URL %s, want HTTPS for the __Host- session cookie", srv.URL)
		}
		if res := get(t, srv, "/account"); res.StatusCode != 302 || res.Header.Get("Location") != "/login?next=%2Faccount" {
			t.Fatalf("signed out: %d %q", res.StatusCode, res.Header.Get("Location"))
		}
		res := postJSON(t, srv, "/login", `{"name":" "}`)
		var failed struct {
			Fields map[string]string `json:"fields"`
		}
		if err := json.NewDecoder(res.Body).Decode(&failed); err != nil || res.StatusCode != 422 || failed.Fields["name"] != "Write your name." {
			t.Fatalf("an empty name: %d %+v %v", res.StatusCode, failed, err)
		}
		if res := postJSON(t, srv, "/login", `{"name":`); res.StatusCode != 400 {
			t.Errorf("a broken body: %d", res.StatusCode)
		}
		res = postJSON(t, srv, "/login", `{"name":"Ann","next":"/account"}`)
		if res.StatusCode != 303 || res.Header.Get("Location") != "/account" {
			t.Fatalf("sign-in: %d %q", res.StatusCode, res.Header.Get("Location"))
		}
		if cookies := res.Cookies(); len(cookies) != 1 || cookies[0].Name != "__Host-tifa_session" || !cookies[0].Secure {
			t.Fatalf("sign-in cookies %v", cookies)
		}
		res = get(t, srv, "/account")
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 || string(body) != `{"app/account/route":"hello Ann"}` {
			t.Fatalf("the page after sign-in: %d %s", res.StatusCode, body)
		}
		if len(events) != 1 || events[0] != "start" {
			t.Errorf("hooks while serving: %v", events)
		}
	})
	if strings.Join(events, ",") != "start,shutdown" {
		t.Errorf("hooks: %v, want OnStart and then OnShutdown when the test ends", events)
	}
}

// Every test gets a server of its own, with its own sessions, one after the other.
func TestStartGivesEachTestItsOwnServer(t *testing.T) {
	var cookie *http.Cookie
	t.Run("first", func(t *testing.T) {
		srv := tifatest.Start(t, signInApp())
		cookie = postJSON(t, srv, "/login", `{"name":"Ann","next":"/account"}`).Cookies()[0]
	})
	t.Run("second", func(t *testing.T) {
		srv := tifatest.Start(t, signInApp())
		r, _ := http.NewRequest("GET", srv.URL+"/account", nil)
		r.AddCookie(cookie)
		res, err := srv.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 302 {
			t.Errorf("the cookie of another test server signed in: %d", res.StatusCode)
		}
	})
}

// fatal records the first Fatalf of Start in place of failing the test.
type fatal struct {
	testing.TB
	msg string
}

func (f *fatal) Helper() {}
func (f *fatal) Fatalf(format string, args ...any) {
	if f.msg == "" {
		f.msg = strings.TrimSpace(fmt.Sprintf(format, args...))
	}
}

func TestStartFailsInAParallelTest(t *testing.T) {
	t.Run("parallel", func(t *testing.T) {
		t.Parallel()
		f := &fatal{TB: t}
		if srv := tifatest.Start(f, signInApp()); srv != nil || !strings.Contains(f.msg, "t.Parallel") {
			t.Errorf("Start in a parallel test: %v, %q", srv, f.msg)
		}
	})
}

func TestStartFailsWhenOnStartFails(t *testing.T) {
	app := signInApp()
	app.Hooks.OnStart = func(context.Context) error { return errors.New("no database") }
	f := &fatal{TB: t}
	if srv := tifatest.Start(f, app); srv != nil || !strings.Contains(f.msg, "OnStart: no database") {
		t.Errorf("Start: %v, %q", srv, f.msg)
	}
	// What the failed start set up is gone, so the next one runs.
	tifatest.Start(t, signInApp())
}

// A table generated before the routes changed fails the test, and the fingerprint of the routes
// as they are passes.
func TestStartChecksTheFingerprintOfTheTable(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("testdata", "requestapp"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := analyze.Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	fp, err := codegen.AppFingerprint(root, m)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(root, "app", "hello"))
	app := signInApp()
	app.Fingerprint = "0123456789abcdef0123456789abcdef"
	f := &fatal{TB: t}
	if srv := tifatest.Start(f, app); srv != nil || !strings.Contains(f.msg, "tifa check") {
		t.Errorf("an old table: %v, %q", srv, f.msg)
	}
	app.Fingerprint = fp
	if srv := tifatest.Start(t, app); srv == nil {
		t.Error("the table of the routes as they are did not start")
	}
}
