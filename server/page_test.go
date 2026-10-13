package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	"build.json":        {Data: []byte(`{"buildId":"b1","script":"/assets/app-1.js","style":"/assets/app-1.css"}`)},
	"assets/app-1.js":   {Data: []byte("console.log(1)")},
	"assets/app-1.css":  {Data: []byte("body{}")},
	"public/robots.txt": {Data: []byte("User-agent: *")},
	"ssr.js":            {Data: []byte("// bundle")},
}

// frontApp is testApp with a page that skips SSR and a page whose HTML is cached.
func frontApp() App {
	app := testApp()
	app.Routes = append(app.Routes,
		Route{ID: "app/login", Path: "/login", Page: true, SSR: false, Layouts: []string{"app"}},
		Route{ID: "app/stats", Path: "/stats", Page: true, SSR: true, Cache: true, Layouts: []string{"app"},
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
	if !strings.Contains(w.Body.String(), `"app/projects/orders":{"status":200,"data":["p1-1","p1-2"]}`) {
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
		`"buildId":"b1"`, `"app":{"status":200,"data":{"name":"shop"}}`, `"assets":{"script":"/assets/app-1.js"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("lacks %s:\n%s", want, body)
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

	w = get(s, "/projects/p1?site=missing", "X-Tifa-Route", "app,app/projects", "X-Tifa-Build", "b1")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"app":{"status":404`) {
		t.Errorf("per-route status: %d %s", w.Code, w.Body.String())
	}
	if w := get(s, "/projects/p1", "X-Tifa-Route", "app", "X-Tifa-Build", "old"); w.Code != 409 {
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
	if w.Code != 404 || w.Header().Get("X-Tifa-Render") != "vm" || r.last(t)["app"].Status != 404 {
		t.Errorf("Layout 404: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	w = get(s, "/projects/broken")
	if data := r.last(t); w.Code != 503 || data["app/projects"].Status != 503 || data["app"].Status != 200 {
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

func TestRouterRedirectIsAnswered(t *testing.T) {
	s, r := newFrontServer(t, frontApp())
	r.result = func(ssr.Input) (string, error) { return "", &ssr.RedirectError{Location: "/projects/a", Status: 307} }
	w := get(s, "/projects/%61")
	if w.Code != 307 || w.Header().Get("Location") != "/projects/a" {
		t.Errorf("%d %v", w.Code, w.Header())
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
		"build.json":      `{"buildId":"b9","script":"/assets/app-9.js"}`,
		"assets/app-9.js": "hydrate()",
		"ssr.js": `globalThis.startRender = (json) => {
  const input = JSON.parse(json)
  globalThis.renderResult = JSON.stringify({ html: '<!DOCTYPE html><p>' + input.buildId + ' ' + input.url + ' ' + input.data.app.data.name + '</p>' })
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
	pool, err := s.openFront(os.DirFS(dir), config.Default().SSR)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
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
	s.register(nil, []Route{{ID: "app/health", Path: "/health", SSR: true, OnGet: func(c *tifa.Ctx) error { return c.Text(200, "ok") }}})
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
