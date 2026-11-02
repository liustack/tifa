package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/liustack/tifa"
)

// wireUser is the session of the apps below. Browser gives the browser the name and nothing else.
type wireUser struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Secret string `json:"secret"`
}

type wireMe struct {
	Name string `json:"name"`
}

func (u wireUser) Browser() wireMe { return wireMe{Name: u.Name} }

type loginInput struct {
	Name string `json:"name"`
}

// wireApp is a front end app with sessions: a sign-in with an input, a sign-in that returns a
// result, a sign-out, a handler that does not touch the session, and one that redirects.
func wireApp() App {
	app := frontApp()
	app.Session = SessionOf[wireUser]
	app.Routes = append(app.Routes,
		Route{ID: "app/in", Path: "/in", POST: Input(func(c *tifa.Ctx, in loginInput) error {
			return tifa.SetSession(c, &wireUser{ID: 1, Name: in.Name, Secret: "s"})
		}, (*tifa.Ctx).Bind)},
		Route{ID: "app/inout", Path: "/inout", POST: InputOutput(func(c *tifa.Ctx, in loginInput) (map[string]string, error) {
			return map[string]string{"hello": in.Name}, tifa.SetSession(c, &wireUser{ID: 1, Name: in.Name})
		}, (*tifa.Ctx).Bind)},
		Route{ID: "app/out", Path: "/out", POST: Input(func(c *tifa.Ctx, in loginInput) error { return tifa.ClearSession(c) }, (*tifa.Ctx).Bind),
			GET: Output(func(c *tifa.Ctx) (int, error) { return 7, nil })},
		Route{ID: "app/same", Path: "/same", POST: Input(func(c *tifa.Ctx, in loginInput) error { return nil }, (*tifa.Ctx).Bind)},
		Route{ID: "app/go", Path: "/go", POST: func(c *tifa.Ctx) error {
			if err := tifa.SetSession(c, &wireUser{ID: 1, Name: "Ann"}); err != nil {
				return err
			}
			return tifa.Redirect("/", 303)
		}},
		Route{ID: "app/fail", Path: "/fail", POST: Input(func(c *tifa.Ctx, in loginInput) error {
			if err := tifa.SetSession(c, &wireUser{ID: 1, Name: "Ann"}); err != nil {
				return err
			}
			return tifa.Error{Status: 400, Message: "no"}
		}, (*tifa.Ctx).Bind)},
	)
	return app
}

// signedIn returns the Cookie header of a user who signed in on s.
func signedIn(t *testing.T, s *Server, name string) string {
	t.Helper()
	w := postJSON(s, "/in", `{"name":"`+name+`"}`)
	c := cookieOf(w, prodCookie)
	if w.Code != 204 || c == nil {
		t.Fatalf("sign in: %d %v", w.Code, w.Header())
	}
	return prodCookie + "=" + c.Value
}

func postJSON(s *Server, target, body string, header ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	for i := 0; i < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func wireServer(t *testing.T, app App) (*Server, *renders) {
	t.Helper()
	s, r := newFrontServer(t, app)
	s.enableSessions(app.Session)
	s.sess.store = newStore(t)
	return s, r
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("%d %s: %v", w.Code, w.Body.String(), err)
	}
	return m
}

func TestPageRenderGetsTheSessionAndWhereToKeepIt(t *testing.T) {
	s, r := wireServer(t, wireApp())
	get(s, "/projects/p1")
	out := r.inputs[0]
	if string(out.Session) != `{"signedIn":false}` || out.SessionKey != "__Host-tifa_session:v1" {
		t.Errorf("signed out: %s %q", out.Session, out.SessionKey)
	}
	if !strings.Contains(out.SessionHead, `"__Host-tifa_session:v1"`) || !strings.Contains(out.SessionHead, `html:not([data-session="in"]) [data-session="in"]`) {
		t.Errorf("head %s", out.SessionHead)
	}
	cookie := signedIn(t, s, "Ann")
	get(s, "/projects/p1", "Cookie", cookie)
	in := r.inputs[1]
	if string(in.Session) != `{"signedIn":true,"data":{"name":"Ann"}}` {
		t.Errorf("signed in: %s", in.Session)
	}
	if strings.Contains(string(in.Session), "secret") {
		t.Error("the secret went to the browser")
	}
}

func TestAppWithoutSessionsRendersWithoutThem(t *testing.T) {
	s, r := newFrontServer(t, frontApp())
	get(s, "/projects/p1")
	if in := r.inputs[0]; in.Session != nil || in.SessionKey != "" || in.SessionHead != "" {
		t.Errorf("input %+v", in)
	}
	if w := get(s, "/__tifa/session"); w.Code != 404 {
		t.Errorf("/__tifa/session: %d", w.Code)
	}
}

func TestSessionIsPartOfTheETagAndTheRenderCache(t *testing.T) {
	s, r := wireServer(t, wireApp())
	anon := get(s, "/projects/p1")
	ann := signedIn(t, s, "Ann")
	bob := signedIn(t, s, "Bob")
	a := get(s, "/projects/p1", "Cookie", ann)
	b := get(s, "/projects/p1", "Cookie", bob)
	tags := map[string]bool{}
	for _, w := range []*httptest.ResponseRecorder{anon, a, b} {
		tags[w.Header().Get("ETag")] = true
	}
	if len(tags) != 3 || anon.Header().Get("ETag") == "" {
		t.Errorf("etags %v", tags)
	}
	// The ETag of the signed-out page does not match for the signed-in user: no 304.
	if w := get(s, "/projects/p1", "Cookie", ann, "If-None-Match", anon.Header().Get("ETag")); w.Code != 200 {
		t.Errorf("conditional request of another session: %d", w.Code)
	}
	if w := get(s, "/projects/p1", "Cookie", ann, "If-None-Match", a.Header().Get("ETag")); w.Code != 304 {
		t.Errorf("same session: %d", w.Code)
	}
	// The same URL and data in the HTML cache: one entry for each session, and a hit for the same one.
	before := r.count()
	get(s, "/stats")
	get(s, "/stats", "Cookie", ann)
	get(s, "/stats", "Cookie", bob)
	if r.count()-before != 3 {
		t.Fatalf("renders for three sessions: %d", r.count()-before)
	}
	w := get(s, "/stats", "Cookie", ann)
	if r.count()-before != 3 || w.Header().Get("X-Tifa-Render") != "cache" {
		t.Errorf("the same session did not hit the cache: %d %s", r.count()-before, w.Header().Get("X-Tifa-Render"))
	}
	if got := r.inputs[len(r.inputs)-1]; !strings.Contains(string(got.Session), "Bob") {
		t.Errorf("last render %s", got.Session)
	}
}

func TestPageWithoutSSRCarriesTheSession(t *testing.T) {
	s, _ := wireServer(t, wireApp())
	out := get(s, "/login")
	body := out.Body.String()
	if !strings.HasPrefix(body, `<!DOCTYPE html><html data-session="out"><head>`) || !strings.Contains(body, `"session":{"signedIn":false}`) ||
		!strings.Contains(body, `"sessionKey":"__Host-tifa_session:v1"`) || !strings.Contains(body, `html:not([data-session="in"])`) {
		t.Errorf("signed out: %s", body)
	}
	in := get(s, "/login", "Cookie", signedIn(t, s, "Ann"))
	if !strings.Contains(in.Body.String(), `<html data-session="in">`) || !strings.Contains(in.Body.String(), `"session":{"signedIn":true,"data":{"name":"Ann"}}`) {
		t.Errorf("signed in: %s", in.Body.String())
	}
	// The head script and the style come before everything the app puts in the head.
	body = in.Body.String()
	if i, j := strings.Index(body, "<script>"), strings.Index(body, "app-1.css"); i < 0 || j < 0 || i > j {
		t.Errorf("order: %s", body)
	}
}

func TestNavigationAnswerIsAnEnvelope(t *testing.T) {
	s, _ := wireServer(t, wireApp())
	w := getData(s, "/projects/p1", "b1", "100")
	m := decodeBody(t, w)
	if len(m) != 2 || string(m["session"]) != `{"signedIn":false}` || !strings.Contains(string(m["routes"]), `"app/layout":{"status":200`) {
		t.Errorf("signed out: %s", w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "private, no-cache" {
		t.Errorf("Cache-Control %q", cc)
	}
	w = getData(s, "/projects/p1", "b1", "100", "Cookie", signedIn(t, s, "Ann"))
	if m := decodeBody(t, w); string(m["session"]) != `{"signedIn":true,"data":{"name":"Ann"}}` {
		t.Errorf("signed in: %s", w.Body.String())
	}
	// An app without sessions has no session field.
	plain, _ := newFrontServer(t, frontApp())
	if m := decodeBody(t, getData(plain, "/projects/p1", "b1", "100")); len(m) != 1 || m["routes"] == nil {
		t.Errorf("no sessions: %v", m)
	}
}

func TestNavigationFailureAndRedirectCarryTheSessionToo(t *testing.T) {
	app := wireApp()
	app.Routes = append(app.Routes, Route{ID: "app/bounce", Path: "/bounce", Page: true, SSR: true, Layouts: []string{"app/layout"},
		Load: func(c *tifa.Ctx) (any, error) { return nil, tifa.Redirect("/elsewhere", 303) }})
	s, _ := wireServer(t, app)
	cookie := signedIn(t, s, "Ann")
	w := getData(s, "/bounce", "b1", "01", "Cookie", cookie)
	if m := decodeBody(t, w); !strings.Contains(string(m["routes"]), `"location":"/elsewhere"`) || !strings.Contains(string(m["session"]), `"signedIn":true`) {
		t.Errorf("redirect Load: %s", w.Body.String())
	}
	w = getData(s, "/projects/p9/missing", "b1", "01")
	if m := decodeBody(t, w); w.Code != 404 || m["session"] == nil || m["routes"] == nil {
		t.Errorf("unmatched URL: %d %s", w.Code, w.Body.String())
	}
}

func TestSessionEndpoint(t *testing.T) {
	var seen []string
	app := wireApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		seen = append(seen, c.Request().Method+" "+c.Request().URL.Path+" "+c.Pattern())
		if c.Query("deny") != "" {
			return tifa.Error{Status: 401, Message: "sign in"}
		}
		return next(c)
	}
	s, _ := wireServer(t, app)
	w := get(s, "/__tifa/session")
	if w.Code != 200 || w.Body.String() != `{"signedIn":false}` || w.Header().Get("Cache-Control") != "private, no-store" ||
		!strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") || w.Header().Get("ETag") != "" {
		t.Errorf("signed out: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	cookie := signedIn(t, s, "Ann")
	w = get(s, "/__tifa/session", "Cookie", cookie, "X-Tifa-Build", "b1")
	if w.Body.String() != `{"signedIn":true,"data":{"name":"Ann"}}` {
		t.Errorf("signed in: %s", w.Body.String())
	}
	h := httptest.NewRequest("HEAD", "/__tifa/session", nil)
	h.Header.Set("Cookie", cookie)
	hw := httptest.NewRecorder()
	s.ServeHTTP(hw, h)
	if hw.Code != 200 || hw.Header().Get("Cache-Control") != "private, no-store" {
		t.Errorf("HEAD: %d %v %q", hw.Code, hw.Header(), hw.Body.String())
	}
	if w := postJSON(s, "/__tifa/session", ""); w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST: %d %v", w.Code, w.Header())
	}
	w = get(s, "/__tifa/session", "X-Tifa-Build", "old")
	if w.Code != 409 || w.Header().Get("X-Tifa-Build") != "b1" {
		t.Errorf("another build: %d %v", w.Code, w.Header())
	}
	if w := get(s, "/__tifa/session?deny=1"); w.Code != 401 {
		t.Errorf("OnRequest did not guard it: %d", w.Code)
	}
	if len(seen) == 0 || !strings.Contains(strings.Join(seen, "\n"), "GET /__tifa/session ") {
		t.Errorf("OnRequest saw %v", seen)
	}
}

func TestSessionEndpointIsNoStoreEvenWhenTheAppSaysPublic(t *testing.T) {
	app := wireApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		c.SetHeader("Cache-Control", "public, max-age=600")
		return next(c)
	}
	s, _ := wireServer(t, app)
	if cc := get(s, "/__tifa/session").Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Errorf("Cache-Control %q", cc)
	}
}

func TestCallAnswerCarriesTheSessionOnlyWhenTheCallChangedIt(t *testing.T) {
	s, _ := wireServer(t, wireApp())
	call := []string{"X-Tifa-Call", "1"}

	// A call with an input and no result: 204 normally, 200 and the session when it changed it.
	w := postJSON(s, "/same", `{}`, call...)
	if w.Code != 204 || w.Body.Len() != 0 {
		t.Errorf("unchanged: %d %q", w.Code, w.Body.String())
	}
	w = postJSON(s, "/in", `{"name":"Ann"}`, call...)
	if m := decodeBody(t, w); w.Code != 200 || len(m) != 1 || string(m["session"]) != `{"signedIn":true,"data":{"name":"Ann"}}` || cookieOf(w, prodCookie) == nil {
		t.Errorf("sign in: %d %s", w.Code, w.Body.String())
	}
	cookie := signedIn(t, s, "Bo")
	w = postJSON(s, "/out", `{}`, append(call, "Cookie", cookie)...)
	if m := decodeBody(t, w); w.Code != 200 || string(m["session"]) != `{"signedIn":false}` {
		t.Errorf("sign out: %d %s", w.Code, w.Body.String())
	}

	// A result goes in result, with the session beside it when the call changed it.
	w = postJSON(s, "/inout", `{"name":"Cy"}`, call...)
	if m := decodeBody(t, w); string(m["result"]) != `{"hello":"Cy"}` || string(m["session"]) != `{"signedIn":true,"data":{"name":"Cy"}}` {
		t.Errorf("result and session: %s", w.Body.String())
	}
	w = get(s, "/out", call...)
	if m := decodeBody(t, w); string(m["result"]) != `7` || len(m) != 1 {
		t.Errorf("result only: %s", w.Body.String())
	}
}

func TestCallWithoutTheHeaderKeepsItsShape(t *testing.T) {
	s, _ := wireServer(t, wireApp())
	w := postJSON(s, "/in", `{"name":"Ann"}`)
	if w.Code != 204 || w.Body.Len() != 0 {
		t.Errorf("input only: %d %q", w.Code, w.Body.String())
	}
	if w := postJSON(s, "/inout", `{"name":"Cy"}`); w.Code != 200 || w.Body.String() != `{"hello":"Cy"}` {
		t.Errorf("input and result: %d %q", w.Code, w.Body.String())
	}
	if w := get(s, "/out"); w.Code != 200 || w.Body.String() != "7" {
		t.Errorf("result: %d %q", w.Code, w.Body.String())
	}
}

func TestCallRedirectCarriesTheSessionAndAnErrorDoesNot(t *testing.T) {
	s, _ := wireServer(t, wireApp())
	call := []string{"X-Tifa-Call", "1"}
	w := postJSON(s, "/go", "", call...)
	m := decodeBody(t, w)
	if w.Code != 200 || w.Header().Get("X-Tifa-Redirect") != "303" || string(m["redirect"]) != `"/"` || string(m["session"]) != `{"signedIn":true,"data":{"name":"Ann"}}` {
		t.Errorf("redirect: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	// Without the header it is a plain redirect.
	if w := postJSON(s, "/go", ""); w.Code != http.StatusSeeOther || w.Body.Len() > 0 && strings.Contains(w.Body.String(), "session") {
		t.Errorf("plain redirect: %d %s", w.Code, w.Body.String())
	}
	w = postJSON(s, "/fail", `{}`, call...)
	if w.Code != 400 || strings.Contains(w.Body.String(), "session") {
		t.Errorf("error: %d %s", w.Code, w.Body.String())
	}
}

// The script at the start of the head sets data-session="in" from the mirror, and only when the
// server wrote no data-session. It runs in Node against a fake document and localStorage.
func TestSessionHeadScript(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	head := sessionHead("__Host-tifa_session:v1")
	script := strings.TrimSuffix(strings.TrimPrefix(head[:strings.Index(head, "</script>")], "<script>"), "</script>")
	if strings.Contains(script, "<") {
		t.Fatalf("script %s", script)
	}
	cases := []struct {
		name, attribute, stored string // stored "THROW" makes localStorage throw, "" has no record
		want                    string
	}{
		{"mirror says signed in", "", `{"signedIn":true,"data":{"name":"Ann"}}`, "in"},
		{"signed in without data", "", `{"signedIn":true}`, "in"},
		{"no record", "", "", ""},
		{"signed out record", "", `{"signedIn":false}`, ""},
		{"broken record", "", `not json`, ""},
		{"not an envelope", "", `[true]`, ""},
		{"null record", "", `null`, ""},
		{"localStorage throws", "", "THROW", ""},
		{"the server wrote in", "in", "", "in"},
		{"the server wrote something else", "out", `{"signedIn":true}`, "out"},
	}
	for _, tc := range cases {
		program := `
const attrs = new Map(); if (process.env.ATTR) attrs.set("data-session", process.env.ATTR)
const stored = process.env.STORED
globalThis.document = { documentElement: {
  hasAttribute: (n) => attrs.has(n), setAttribute: (n, v) => attrs.set(n, v), getAttribute: (n) => attrs.get(n) ?? null } }
globalThis.localStorage = { getItem: (k) => {
  if (k !== "__Host-tifa_session:v1") throw new Error("wrong key " + k)
  if (stored === "THROW") throw new Error("denied")
  return stored === "" ? null : stored } }
eval(process.env.SCRIPT)
process.stdout.write(attrs.get("data-session") ?? "")`
		cmd := exec.Command(node, "-e", program)
		cmd.Env = append(os.Environ(), "ATTR="+tc.attribute, "STORED="+tc.stored, "SCRIPT="+script)
		out, err := cmd.CombinedOutput()
		if err != nil || string(out) != tc.want {
			t.Errorf("%s: got %q, want %q (%v)", tc.name, out, tc.want, err)
		}
	}
}

// The CSP hashes in docs/app.md are those of the script and the style the server writes. This
// fails when either changes, so the docs get the new hashes.
func TestSessionHeadHashesAreInTheDocs(t *testing.T) {
	doc, err := os.ReadFile("../docs/app.md")
	if err != nil {
		t.Fatal(err)
	}
	zh, err := os.ReadFile("../docs/app.zh-CN.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"__Host-tifa_session:v1", "tifa_session:v1"} {
		head := sessionHead(key)
		script := head[len("<script>"):strings.Index(head, "</script>")]
		style := head[strings.Index(head, "<style>")+len("<style>") : strings.Index(head, "</style>")]
		for _, text := range []string{script, style} {
			sum := sha256.Sum256([]byte(text))
			hash := "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
			for name, d := range map[string][]byte{"app.md": doc, "app.zh-CN.md": zh} {
				if !strings.Contains(string(d), hash) {
					t.Errorf("docs/%s lacks %s of %.60s", name, hash, text)
				}
			}
		}
	}
}

func TestSessionAnswerCacheControlRules(t *testing.T) {
	for value, want := range map[string]bool{
		"":                                   true,
		"no-store":                           true,
		"private, no-cache":                  true,
		"No-Cache, PRIVATE":                  true,
		"private, no-cache, max-age=0":       true,
		"public, no-store":                   true,
		"private":                            false,
		"no-cache":                           false,
		"public, max-age=600":                false,
		"s-maxage=60, private, no-cache":     true,
		`private="Set-Cookie", no-cache`:     false,
		`private, no-cache="Set-Cookie"`:     false,
		"private, no-cache, must-understand": false,
		"no-store, must-understand":          false,
		`private, no-cache, x="a,b`:          false,
		"private, no-cache, =1":              false,
		"private, , no-cache":                false,
		"private no-cache":                   false,
	} {
		h := http.Header{}
		if value != "" {
			h.Set("Cache-Control", value)
		}
		if got := cacheControlHoldsSession(h); got != want {
			t.Errorf("%q: got %v, want %v", value, got, want)
		}
	}
	// Several fields merge into one list.
	h := http.Header{}
	h.Add("Cache-Control", "private")
	h.Add("Cache-Control", "no-cache")
	if !cacheControlHoldsSession(h) {
		t.Error("two fields that together say private, no-cache")
	}
	h = http.Header{}
	h.Add("Cache-Control", "no-cache")
	h.Add("Cache-Control", `private="x"`)
	if cacheControlHoldsSession(h) {
		t.Error("a private with a field list")
	}
}

// An app that makes an answer carrying the session shareable gets an unexpected error instead.
func TestSharableAnswerWithSessionIsAnError(t *testing.T) {
	var reported atomic.Int32
	app := wireApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		if c.Query("share") != "" || c.Header("X-Share") != "" {
			c.SetHeader("Cache-Control", c.Query("share")+c.Header("X-Share"))
			c.SetHeader("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		}
		return next(c)
	}
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error {
		reported.Add(1)
		c.SetHeader("Cache-Control", "public, max-age=60") // OnError cannot make it shareable again
		c.SetHeader("ETag", `"x"`)
		return tifa.Error{Status: 500, Message: "reported"}
	}
	s, _ := wireServer(t, app)
	check := func(name string, w *httptest.ResponseRecorder, html bool) {
		t.Helper()
		ct := w.Header().Get("Content-Type")
		if w.Code != 500 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" || w.Header().Get("Last-Modified") != "" ||
			strings.HasPrefix(ct, "text/html") != html {
			t.Errorf("%s: %d %v %s", name, w.Code, w.Header(), w.Body.String())
		}
	}
	for _, page := range []string{"/projects/p1", "/login"} {
		reported.Store(0)
		check("page "+page, get(s, page+"?share=public,+max-age=600"), true)
		if reported.Load() != 1 {
			t.Errorf("%s: OnError ran %d times", page, reported.Load())
		}
	}
	check("navigation", getData(s, "/projects/p1?share=private", "b1", "100"), false)
	check("call", postJSON(s, "/in", `{"name":"Ann"}`, "X-Tifa-Call", "1", "X-Share", `private="x", no-cache`), false)
	// Compliant values pass, and an app without sessions is not checked.
	if w := get(s, "/projects/p1?share=private,+no-cache"); w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-cache" {
		t.Errorf("compliant: %d %v", w.Code, w.Header())
	}
	plain, _ := newFrontServer(t, frontApp())
	plain.hooks.OnRequest = app.Hooks.OnRequest
	if w := get(plain, "/projects/p1?share=public"); w.Code != 200 || w.Header().Get("Cache-Control") != "public" {
		t.Errorf("app without sessions: %d %v", w.Code, w.Header())
	}
}

// An OnRequest that sets a shareable header and then redirects: the JSON answer to a navigation
// carries the session, so it is never shareable and keeps no validator.
func TestNavigationRedirectWithSessionIsNotShareable(t *testing.T) {
	app := wireApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		if c.Query("go") != "" {
			c.SetHeader("Cache-Control", "public, max-age=600")
			c.SetHeader("ETag", `"v"`)
			c.SetHeader("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
			return tifa.Redirect("/elsewhere", 302)
		}
		return next(c)
	}
	s, _ := wireServer(t, app)
	for _, method := range []string{"GET", "HEAD"} {
		r := httptest.NewRequest(method, dataURL("/projects/p1?go=1", "b1", "100"), nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" || w.Header().Get("Last-Modified") != "" {
			t.Errorf("%s: %d %v", method, w.Code, w.Header())
		}
		if method == "GET" && !strings.Contains(w.Body.String(), `"session":{"signedIn":false}`) {
			t.Errorf("body %s", w.Body.String())
		}
	}
}

// A page for someone signed out says so: the head script reads the copy only on a page that
// carries no session.
func TestPageWithSessionWritesOutWhenSignedOut(t *testing.T) {
	s, _ := wireServer(t, wireApp())
	body := get(s, "/login").Body.String()
	if !strings.HasPrefix(body, `<!DOCTYPE html><html data-session="out"><head>`) {
		t.Errorf("boot page: %.120s", body)
	}
}

func TestCallAnswersVaryOnTheCallHeader(t *testing.T) {
	s, _ := wireServer(t, wireApp())
	for _, call := range [][]string{nil, {"X-Tifa-Call", "1"}} {
		for name, w := range map[string]*httptest.ResponseRecorder{
			"result": get(s, "/out", call...), "input": postJSON(s, "/same", `{}`, call...), "both": postJSON(s, "/inout", `{"name":"a"}`, call...),
		} {
			if !varies(w, "X-Tifa-Call") {
				t.Errorf("%s %v: Vary %v", name, call, w.Header().Values("Vary"))
			}
		}
	}
	// A Vary the app set stays.
	app := wireApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		c.Writer().Header().Add("Vary", "Cookie")
		return next(c)
	}
	s2, _ := wireServer(t, app)
	if w := get(s2, "/out"); !varies(w, "Cookie") || !varies(w, "X-Tifa-Call") {
		t.Errorf("Vary %v", w.Header().Values("Vary"))
	}
}

func TestSessionEndpointAnswersAreNeverStored(t *testing.T) {
	app := wireApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		c.SetHeader("Cache-Control", "public, max-age=600")
		c.SetHeader("ETag", `"v"`)
		c.SetHeader("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		switch c.Query("do") {
		case "deny":
			return tifa.Error{Status: 401, Message: "sign in"}
		case "boom":
			return errors.New("boom")
		case "redirect":
			return tifa.Redirect("/login", 302)
		}
		return next(c)
	}
	s, _ := wireServer(t, app)
	cases := []struct {
		name, method, target string
		header               []string
		code                 int
	}{
		{"GET", "GET", "/__tifa/session", nil, 200},
		{"HEAD", "HEAD", "/__tifa/session", nil, 200},
		{"405", "POST", "/__tifa/session", nil, 405},
		{"409", "GET", "/__tifa/session", []string{"X-Tifa-Build", "old"}, 409},
		{"early 401", "GET", "/__tifa/session?do=deny", nil, 401},
		{"error", "GET", "/__tifa/session?do=boom", nil, 500},
		{"redirect", "GET", "/__tifa/session?do=redirect", nil, 302},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, tc.target, nil)
		for i := 0; i < len(tc.header); i += 2 {
			r.Header.Set(tc.header[i], tc.header[i+1])
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.code || w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("ETag") != "" || w.Header().Get("Last-Modified") != "" {
			t.Errorf("%s: %d %v", tc.name, w.Code, w.Header())
		}
	}
}

// A middleware that writes the answer to /__tifa/session itself reaches neither the handler nor the
// error flow. The answer still goes out as private, no-store.
func TestSessionEndpointAnswerOfTheMiddlewareIsNeverStored(t *testing.T) {
	app := wireApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		if c.Request().URL.Path == "/__tifa/session" {
			c.SetHeader("Cache-Control", "public, max-age=600")
			c.SetHeader("ETag", `"v"`)
			c.SetHeader("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
			return c.JSON(401, map[string]string{"error": "sign in"})
		}
		return next(c)
	}
	s, _ := wireServer(t, app)
	for _, method := range []string{"GET", "HEAD"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(method, "/__tifa/session", nil))
		if w.Code != 401 || w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("ETag") != "" || w.Header().Get("Last-Modified") != "" {
			t.Errorf("%s: %d %v", method, w.Code, w.Header())
		}
	}
}
