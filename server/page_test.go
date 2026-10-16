package server

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
)

// renders records the inputs of a fake SSR bundle, which echoes them into its HTML.
type renders struct {
	mu     sync.Mutex
	inputs []ssr.Input
	result func(in ssr.Input) (string, error)
}

func (r *renders) render(_ context.Context, in ssr.Input) (string, error) {
	r.mu.Lock()
	r.inputs = append(r.inputs, in)
	r.mu.Unlock()
	if r.result != nil {
		return r.result(in)
	}
	return "<html>" + in.URL + " " + string(in.Data) + "</html>", nil
}

func (r *renders) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.inputs)
}

func (r *renders) last(t *testing.T) map[string]routeResult {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.inputs) == 0 {
		t.Fatal("nothing was rendered")
	}
	var data map[string]routeResult
	if err := json.Unmarshal(r.inputs[len(r.inputs)-1].Data, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

var dist = fstest.MapFS{
	"build.json":             {Data: []byte(`{"buildId":"b1","head":"<link rel=\"stylesheet\" href=\"/assets/app-1.css\"><script type=\"module\" src=\"/assets/app-1.js\"></script>"}`)},
	"assets/app-1.js":        {Data: []byte("console.log(1)")},
	"assets/app-1.css":       {Data: []byte("body{}")},
	"public/robots.txt":      {Data: []byte("User-agent: *")},
	"public/assets/logo.svg": {Data: []byte("<svg/>")},
	"ssr.js":                 {Data: []byte("// bundle")},
}

// frontApp is testApp with a page that skips SSR and a page whose HTML is cached.
func frontApp() App {
	app := testApp()
	app.Routes = append(app.Routes,
		Route{ID: "app/login", Path: "/login", Page: true, SSR: false, Layouts: []string{"app/layout"}},
		Route{ID: "app/stats", Path: "/stats", Page: true, SSR: true, Cache: true, Layouts: []string{"app/layout"},
			Load: func(c *tifa.Ctx) (any, error) { return 42, nil }},
	)
	return app
}

func newFrontServer(t *testing.T, app App) (*Server, *renders) {
	t.Helper()
	s := newAppServer(t, app)
	r := &renders{}
	if err := s.setFront(dist, r.render); err != nil {
		t.Fatal(err)
	}
	return s, r
}

func get(s *Server, target string, header ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", target, nil)
	for i := 0; i < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestPageRendersWithTheResultsOfItsChain(t *testing.T) {
	s, r := newFrontServer(t, frontApp())
	w := get(s, "/projects/p1?x=1")
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || w.Header().Get("X-Tifa-Render") != "vm" {
		t.Fatalf("%d %v %s", w.Code, w.Header(), w.Body.String())
	}
	if w.Header().Get("Vary") != "X-Tifa-Route" || w.Header().Get("ETag") == "" {
		t.Errorf("headers %v", w.Header())
	}
	in := r.inputs[0]
	if in.BuildID != "b1" || in.URL != "/projects/p1?x=1" {
		t.Errorf("input %+v", in)
	}
	data := r.last(t)
	if len(data) != 3 || data["app/projects"].Status != 200 || data["app/projects/orders"].Status != 200 {
		t.Errorf("data %s", in.Data)
	}
	// The data of each route comes as the JSON text Go wrote, which the page carries as it is.
	if !strings.Contains(w.Body.String(), `"app/projects/orders":{"status":200,"json":"[\"p1-1\",\"p1-2\"]"}`) {
		t.Errorf("body %s", w.Body.String())
	}
}

func TestUnchangedPageAnswers304(t *testing.T) {
	s, r := newFrontServer(t, frontApp())
	etag := get(s, "/projects/p1").Header().Get("ETag")
	w := get(s, "/projects/p1", "If-None-Match", etag)
	if w.Code != 304 || w.Body.Len() != 0 || r.count() != 1 {
		t.Fatalf("%d, %d renders, body %q", w.Code, r.count(), w.Body.String())
	}
	if other := get(s, "/projects/p2").Header().Get("ETag"); other == etag {
		t.Error("two URLs share an ETag")
	}
	if w := get(s, "/projects/p1", "If-None-Match", `"stale"`); w.Code != 200 {
		t.Errorf("stale ETag: %d", w.Code)
	}
}

// tifa dev keeps the build ID when only the SSR bundle changes, so the ETag covers the bundle
// too: a reload after a change to page code gets the new render, not a 304 for the old one.
func TestPageETagCoversTheSSRBundle(t *testing.T) {
	s, _ := newFrontServer(t, frontApp())
	etag := get(s, "/projects/p1").Header().Get("ETag")
	changed := fstest.MapFS{}
	for name, file := range dist {
		changed[name] = file
	}
	changed["ssr.js"] = &fstest.MapFile{Data: []byte("// changed bundle")}
	s2 := newAppServer(t, frontApp())
	if err := s2.setFront(changed, (&renders{}).render); err != nil {
		t.Fatal(err)
	}
	if w := get(s2, "/projects/p1", "If-None-Match", etag); w.Code != 200 {
		t.Errorf("the ETag of the old bundle gets %d, want 200", w.Code)
	}
}

func TestCachedPageRendersOnce(t *testing.T) {
	s, r := newFrontServer(t, frontApp())
	first, second := get(s, "/stats"), get(s, "/stats")
	if first.Header().Get("X-Tifa-Render") != "vm" || second.Header().Get("X-Tifa-Render") != "cache" || r.count() != 1 {
		t.Fatalf("renders %d, headers %v then %v", r.count(), first.Header(), second.Header())
	}
	if first.Body.String() != second.Body.String() {
		t.Error("the cached page differs")
	}
	get(s, "/projects/p1")
	get(s, "/projects/p1")
	if r.count() != 3 {
		t.Errorf("a page without Cache was cached: %d renders", r.count())
	}
}

func TestPageWithoutSSRCarriesItsData(t *testing.T) {
	s, r := newFrontServer(t, frontApp())
	w := get(s, "/login")
	body := w.Body.String()
	if w.Code != 200 || r.count() != 0 || w.Header().Get("X-Tifa-Render") != "none" {
		t.Fatalf("%d, %d renders: %s", w.Code, r.count(), body)
	}
	for _, want := range []string{
		`<script type="module" src="/assets/app-1.js"></script>`,
		`<link rel="stylesheet" href="/assets/app-1.css">`,
		`<script type="application/json" id="tifa-data">`,
		`<body><div id="tifa-root"></div>`,
		`"buildId":"b1"`, `"app/layout":{"status":200,"json":"{\"name\":\"shop\"}"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("lacks %s:\n%s", want, body)
		}
	}
	// The browser writes the head of the routes, the viewport among it, and needs no assets.
	for _, unwanted := range []string{`name="viewport"`, `"assets"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("has %s:\n%s", unwanted, body)
		}
	}
}

func TestDataRequestAnswersTheListedRoutes(t *testing.T) {
	s, r := newFrontServer(t, frontApp())
	w := get(s, "/projects/p1", "X-Tifa-Route", "app/projects/orders, app/projects", "X-Tifa-Build", "b1")
	if w.Code != 200 || w.Header().Get("Vary") != "X-Tifa-Route" || r.count() != 0 {
		t.Fatalf("%d %v %s", w.Code, w.Header(), w.Body.String())
	}
	var got map[string]routeResult
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["app/projects"].Status != 200 || got["app/projects/orders"].Status != 200 {
		t.Errorf("%s", w.Body.String())
	}

	w = get(s, "/projects/p1?site=missing", "X-Tifa-Route", "app/layout,app/projects", "X-Tifa-Build", "b1")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"app/layout":{"status":404`) {
		t.Errorf("per-route status: %d %s", w.Code, w.Body.String())
	}
	if w := get(s, "/projects/p1", "X-Tifa-Route", "app/layout", "X-Tifa-Build", "old"); w.Code != 409 {
		t.Errorf("other build: %d %s", w.Code, w.Body.String())
	}
	if w := get(s, "/projects/p1", "X-Tifa-Route", "app/login", "X-Tifa-Build", "b1"); w.Code != 400 {
		t.Errorf("route outside the chain: %d %s", w.Code, w.Body.String())
	}
}

func TestFailedPagesRenderWithTheErrorStatus(t *testing.T) {
	var unexpected []error
	app := frontApp()
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error {
		unexpected = append(unexpected, err)
		return tifa.Error{Status: 503, Message: "later"}
	}
	app.Hooks.OnRequest = func(c *tifa.Ctx) error {
		if c.Query("deny") != "" {
			return tifa.Error{Status: 401, Message: "sign in"}
		}
		return c.Next()
	}
	s, r := newFrontServer(t, app)

	w := get(s, "/projects/p1?site=missing")
	if w.Code != 404 || w.Header().Get("X-Tifa-Render") != "vm" || r.last(t)["app/layout"].Status != 404 {
		t.Errorf("Layout 404: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	w = get(s, "/projects/broken")
	if data := r.last(t); w.Code != 503 || data["app/projects"].Status != 503 || data["app/layout"].Status != 200 {
		t.Errorf("page error: %d %s", w.Code, w.Body.String())
	}
	if len(unexpected) != 1 || !errors.Is(unexpected[0], errDatabase) {
		t.Errorf("OnError got %v, want the shared error once", unexpected)
	}
	w = get(s, "/nowhere")
	if data := r.last(t); w.Code != 404 || len(data) != 0 || !strings.Contains(w.Body.String(), "<html>/nowhere") {
		t.Errorf("unmatched URL: %d %s", w.Code, w.Body.String())
	}
	before := r.count()
	w = get(s, "/projects/p1?deny=1")
	if w.Code != 401 || r.count() != before || !strings.Contains(w.Body.String(), "sign in") {
		t.Errorf("OnRequest error: %d %s", w.Code, w.Body.String())
	}
	if w := send(s, "POST", "/nowhere", ""); w.Code != 404 || !strings.Contains(w.Body.String(), `"error"`) {
		t.Errorf("unmatched POST: %d %s", w.Code, w.Body.String())
	}
	if w := send(s, "DELETE", "/api/health", ""); w.Code != 409 || !strings.Contains(w.Body.String(), `"error"`) {
		t.Errorf("interface route: %d %s", w.Code, w.Body.String())
	}
}

func TestUnmatchedURLWithoutRootSSRCarriesNoData(t *testing.T) {
	app := frontApp()
	app.Layouts[0].SSR = false
	s, r := newFrontServer(t, app)
	w := get(s, "/nowhere")
	if w.Code != 404 || r.count() != 0 || !strings.Contains(w.Body.String(), `"data":{}`) {
		t.Errorf("%d, %d renders: %s", w.Code, r.count(), w.Body.String())
	}
}

func TestFailedRenderFallsBackToAPlainPage(t *testing.T) {
	s, r := newFrontServer(t, frontApp())
	r.result = func(ssr.Input) (string, error) { return "", errors.New("ReferenceError: window is not defined") }
	w := get(s, "/projects/p1")
	if w.Code != 500 || !strings.Contains(w.Body.String(), "<h1>500 Internal Server Error</h1>") {
		t.Errorf("%d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "window") {
		t.Error("the fallback page shows the internal error")
	}
}

// Under tifa dev the plain page names the request and shows the whole error, and loads the dev
// client, which reloads it once a rebuild serves a fix.
func TestDevFailedRenderShowsTheError(t *testing.T) {
	s, r := newFrontServer(t, frontApp())
	s.dev = http.NotFoundHandler()
	r.result = func(ssr.Input) (string, error) {
		return "", errors.New("ReferenceError: window is not defined\n    at Page (app/projects/route.tsx:3:9)")
	}
	w := get(s, "/projects/p1")
	body := w.Body.String()
	if w.Code != 500 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("%d %v %s", w.Code, w.Header(), body)
	}
	for _, want := range []string{
		"<h1>500 Internal Server Error</h1>",
		"GET /projects/p1 failed while rendering on the server.",
		"ReferenceError: window is not defined\n    at Page (app/projects/route.tsx:3:9)",
		"<script>window.__tifaDevErrorPage = true</script>",
		`<script src="/__tifa/dev/client.js"></script>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Users see:") {
		t.Errorf("the public message is the status text, the page repeats it: %s", body)
	}
}

func TestDevFailedRequestShowsTheError(t *testing.T) {
	app := frontApp()
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error { return tifa.Error{Status: 503, Message: "Try again later"} }
	app.Hooks.OnRequest = func(c *tifa.Ctx) error { return errors.New("open the session store: <dial> refused") }
	s, _ := newFrontServer(t, app)
	s.dev = http.NotFoundHandler()
	w := get(s, "/projects/p1")
	body := w.Body.String()
	for _, want := range []string{
		"<h1>503 Service Unavailable</h1>",
		"GET /projects/p1 failed.",
		"Users see: Try again later",
		"open the session store: &lt;dial&gt; refused",
		`<script src="/__tifa/dev/client.js"></script>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q: %s", want, body)
		}
	}
	if w.Code != 503 || strings.Contains(body, "failed while rendering") {
		t.Errorf("%d %s", w.Code, body)
	}
}

// When the page of a failed Load does not render either, the dev page shows both errors.
func TestDevFailedErrorPageShowsBothErrors(t *testing.T) {
	s, r := newFrontServer(t, frontApp())
	s.dev = http.NotFoundHandler()
	r.result = func(ssr.Input) (string, error) { return "", errors.New("TypeError: x is undefined") }
	w := get(s, "/projects/broken")
	body := w.Body.String()
	for _, want := range []string{"failed while rendering on the server.", errDatabase.Error(), "TypeError: x is undefined"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q: %s", want, body)
		}
	}
	if w.Code != 500 {
		t.Errorf("%d %s", w.Code, body)
	}
}

func TestRouterRedirectIsAnswered(t *testing.T) {
	s, r := newFrontServer(t, frontApp())
	r.result = func(ssr.Input) (string, error) { return "", &ssr.RedirectError{Location: "/projects/a", Status: 307} }
	w := get(s, "/projects/%61")
	if w.Code != 307 || w.Header().Get("Location") != "/projects/a" {
		t.Errorf("%d %v", w.Code, w.Header())
	}
}

// A redirect of the router carries no ETag, so a client never holds one that would turn the
// redirect into 304.
func TestRouterRedirectCarriesNoETag(t *testing.T) {
	s, r := newFrontServer(t, frontApp())
	r.result = func(in ssr.Input) (string, error) {
		if strings.Contains(in.URL, "%") {
			return "", &ssr.RedirectError{Location: "/projects/p1", Status: 307}
		}
		return "<html>" + in.URL + "</html>", nil
	}
	w := get(s, "/projects/%701")
	if w.Code != 307 || w.Header().Get("ETag") != "" {
		t.Fatalf("redirect: %d %v", w.Code, w.Header())
	}
	etag := get(s, "/projects/p1").Header().Get("ETag")
	if w := get(s, "/projects/%701", "If-None-Match", etag); w.Code != 307 || w.Header().Get("Location") != "/projects/p1" {
		t.Errorf("with the ETag of the target: %d %v", w.Code, w.Header())
	}
}

func TestStaticFilesSkipOnRequest(t *testing.T) {
	app := frontApp()
	var requests int
	app.Hooks.OnRequest = func(c *tifa.Ctx) error {
		requests++
		return c.Next()
	}
	s, _ := newFrontServer(t, app)
	w := get(s, "/assets/app-1.js")
	if w.Code != 200 || w.Body.String() != "console.log(1)" || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("asset: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	w = get(s, "/robots.txt")
	if w.Code != 200 || w.Body.String() != "User-agent: *" || w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("public file: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	if requests != 0 {
		t.Errorf("OnRequest ran for %d static files", requests)
	}
	w = get(s, "/assets/logo.svg")
	if w.Code != 200 || w.Body.String() != "<svg/>" || w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("public file under /assets/: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	if w := get(s, "/assets/gone.js"); w.Code != 404 || requests != 1 {
		t.Errorf("missing asset: %d, OnRequest %d", w.Code, requests)
	}
	if w := get(s, "/build.json"); w.Code != 404 {
		t.Errorf("build.json is served: %d", w.Code)
	}
	if w := send(s, "POST", "/robots.txt", ""); w.Code != 404 {
		t.Errorf("POST to a public file: %d", w.Code)
	}
}

func TestSetFrontNeedsBundlesForPages(t *testing.T) {
	s := newAppServer(t, frontApp())
	if err := s.setFront(dist, nil); err == nil {
		t.Error("pages without a render function")
	}
	if err := s.setFront(fstest.MapFS{}, (&renders{}).render); err == nil {
		t.Error("dist without build.json")
	}
}

// openFront serves pages from a dist directory with a real VM pool over its ssr.js.
func TestOpenFrontRendersWithTheBundle(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"build.json":      `{"buildId":"b9","head":"<script type=\"module\" src=\"/assets/app-9.js\"></script>"}`,
		"assets/app-9.js": "hydrate()",
		"ssr.js": `globalThis.startRender = (json) => {
  const input = JSON.parse(json)
  globalThis.renderResult = '<!DOCTYPE html><p>' + input.buildId + ' ' + input.url + ' ' + JSON.parse(input.data['app/layout'].json).name + '</p>'
}`,
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := newAppServer(t, frontApp())
	if err := s.openFront(os.DirFS(dir), config.Default().SSR); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.closeFront(); err != nil {
			t.Error(err)
		}
	})
	if w := get(s, "/"); w.Code != 200 || w.Body.String() != "<!DOCTYPE html><p>b9 / shop</p>" {
		t.Errorf("%d %s", w.Code, w.Body.String())
	}
	if w := get(s, "/assets/app-9.js"); w.Body.String() != "hydrate()" {
		t.Errorf("asset: %d %s", w.Code, w.Body.String())
	}
}

// --dist replaces the embedded front end, as tifa dev does to serve the files it rebuilds.
func TestDistFlagReadsTheFrontEndFromDisk(t *testing.T) {
	app := frontApp()
	app.Dist = fstest.MapFS{}
	_, _, err := runApp(t, context.Background(), app, nil, "--server-addr", "127.0.0.1:0", "--dist", filepath.Join(t.TempDir(), "missing"))
	if err == nil || !strings.Contains(err.Error(), "build.json") {
		t.Fatalf("got %v", err)
	}
}

// An application without pages serves its public files and answers unmatched URLs with JSON.
func TestFrontWithoutPagesKeepsJSONErrors(t *testing.T) {
	s := New(config.Default(), Hooks{})
	s.register(nil, []Route{{ID: "app/health", Path: "/health", SSR: true, GET: func(c *tifa.Ctx) error { return c.Text(200, "ok") }}})
	if err := s.setFront(fstest.MapFS{"build.json": {Data: []byte(`{"buildId":"b2"}`)}, "public/robots.txt": {Data: []byte("x")}}, nil); err != nil {
		t.Fatal(err)
	}
	if w := get(s, "/nowhere"); w.Code != 404 || !strings.Contains(w.Body.String(), `"error"`) {
		t.Errorf("unmatched URL: %d %s", w.Code, w.Body.String())
	}
	if w := get(s, "/robots.txt"); w.Code != 200 {
		t.Errorf("public file: %d", w.Code)
	}
}

// The page answer varies on X-Tifa-Route as well as on whatever the application varies on.
func TestPageKeepsTheVaryOfTheApplication(t *testing.T) {
	app := frontApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx) error {
		c.SetHeader("Vary", "Cookie")
		return c.Next()
	}
	s, _ := newFrontServer(t, app)
	for _, header := range [][]string{nil, {"X-Tifa-Route", "app/layout", "X-Tifa-Build", "b1"}} {
		w := get(s, "/projects/p1", header...)
		vary := strings.Join(w.Header().Values("Vary"), ", ")
		if !strings.Contains(vary, "Cookie") || !strings.Contains(vary, "X-Tifa-Route") {
			t.Errorf("%v: Vary %q", header, vary)
		}
	}
}

// A data request keeps the Cache-Control the application set, and gets no-cache otherwise.
func TestDataRequestKeepsTheCacheControlOfTheApplication(t *testing.T) {
	app := frontApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx) error {
		if c.Query("private") != "" {
			c.SetHeader("Cache-Control", "private, no-store")
		}
		return c.Next()
	}
	s, _ := newFrontServer(t, app)
	data := []string{"X-Tifa-Route", "app/layout", "X-Tifa-Build", "b1"}
	if w := get(s, "/projects/p1?private=1", data...); w.Header().Get("Cache-Control") != "private, no-store" {
		t.Errorf("data request: %v", w.Header())
	}
	if w := get(s, "/projects/p1?private=1"); w.Header().Get("Cache-Control") != "private, no-store" {
		t.Errorf("page: %v", w.Header())
	}
	if w := get(s, "/projects/p1", data...); w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("default: %v", w.Header())
	}
}

// A failed render of a Cache = true page goes to OnError and is never cached.
func TestFailedRenderIsNotCached(t *testing.T) {
	var reported []error
	app := frontApp()
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error {
		reported = append(reported, err)
		return tifa.Error{Status: 500}
	}
	s, r := newFrontServer(t, app)
	r.result = func(ssr.Input) (string, error) {
		return "", errors.New("Error: a component threw during the server render")
	}
	for i := range 2 {
		if w := get(s, "/stats"); w.Code != 500 || w.Header().Get("X-Tifa-Render") == "cache" {
			t.Errorf("request %d: %d %v", i, w.Code, w.Header())
		}
	}
	if r.count() != 2 || len(reported) != 2 {
		t.Errorf("%d renders, OnError got %v", r.count(), reported)
	}
}

// The render reads the data of a Load as the JSON text Go writes, and the page carries that text,
// so a -0 reaches the browser as the -0 the server rendered.
func TestInjectedResultsKeepTheJSONGoWrites(t *testing.T) {
	items, err := injectedResults(map[string]routeResult{
		"app/route":  {Status: 200, Data: map[string]any{"z": math.Copysign(0, -1), "list": []float64{math.Copysign(0, -1), 1}}},
		"app/layout": {Status: 200},
		"app/gone":   {Status: 404},
		"app/moved":  {Status: 303, Location: "/x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]routeResult{
		"app/route":  {Status: 200, JSON: `{"list":[-0,1],"z":-0}`},
		"app/layout": {Status: 200, JSON: "null"},
		"app/gone":   {Status: 404},
		"app/moved":  {Status: 303, Location: "/x"},
	}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("got %+v, want %+v", items, want)
	}
}
