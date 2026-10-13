package httpapp

import (
	"encoding/json"
	"github.com/liustack/tifa/examples/shop/internal/pages"
	"github.com/liustack/tifa/examples/shop/internal/workspace"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newApp(t *testing.T) *App {
	t.Helper()
	app, err := New(workspace.NewStore())
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func request(app *App, method, url string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, url, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func TestDocumentContainsSSRAndBootAssets(t *testing.T) {
	app := newApp(t)
	w := request(app, "GET", "/projects/studio", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
	for _, value := range []string{"SH-1042", "林夏", "go-quick-config", "type=\"module\""} {
		if !strings.Contains(w.Body.String(), value) {
			t.Errorf("missing %q", value)
		}
	}
	if w.Header().Get("X-SSR-Engine") != "QuickJS" {
		t.Fatal("SSR engine header missing")
	}
	if !strings.Contains(w.Header().Get("Server-Timing"), "render") {
		t.Fatal("missing timing")
	}
}

func TestDocumentSupportsQueryStrings(t *testing.T) {
	app := newApp(t)
	for _, query := range []string{"?tab=all", "?q=hello%20world", "?q=hello+world&n=1"} {
		w := request(app, "GET", "/projects/studio"+query, nil)
		if w.Code != 200 {
			t.Errorf("%s returned %d", query, w.Code)
		}
	}
}

func TestDataNavigationDoesNotInvokeSSR(t *testing.T) {
	app := newApp(t)
	app.renderer = nil // A data navigation must never enter the JS renderer.
	w := request(app, "GET", "/_framework/page-data?url=/projects/studio/orders/SH-1042&build="+app.bundle.Build.BuildID, nil)
	if w.Code != 200 {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
	var snapshot pages.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Scopes) != 3 || snapshot.Kind != "data" {
		t.Fatalf("invalid data response %+v", snapshot)
	}
	if w.Header().Get("X-SSR-Engine") != "" {
		t.Fatal("data navigation unexpectedly rendered")
	}
}

func TestBuildMismatchRequestsReload(t *testing.T) {
	app := newApp(t)
	w := request(app, "GET", "/_framework/page-data?url=/projects/studio&build=stale", nil)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"kind":"reload"`) {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
}

func TestMissingPageAndCrossProjectOrderAre404(t *testing.T) {
	app := newApp(t)
	for _, path := range []string{"/missing", "/projects/missing", "/projects/weekend/orders/SH-1042"} {
		w := request(app, "GET", path, nil)
		if w.Code != 404 {
			t.Errorf("%s got %d", path, w.Code)
		}
	}
}

func TestOnlyBrowserAssetsArePublished(t *testing.T) {
	app := newApp(t)
	for _, path := range []string{app.bundle.Build.Script, app.bundle.Build.Style} {
		if w := request(app, "GET", path, nil); w.Code != 200 {
			t.Errorf("%s got %d", path, w.Code)
		}
	}
	for _, path := range []string{"/assets/server.js", "/assets/routes.json", "/assets/build.json", "/assets/"} {
		if w := request(app, "GET", path, nil); w.Code != 404 {
			t.Errorf("exposed private asset %s", path)
		}
	}
}

func TestBrowserMutationRefreshesGoData(t *testing.T) {
	app := newApp(t)
	path := "/api/projects/studio/orders/SH-1042/ship"
	if w := request(app, "POST", path, map[string]string{"Origin": "https://unrelated.example", "X-Requested-With": "go-quick"}); w.Code != 403 {
		t.Fatalf("unexpected cross-origin status %d", w.Code)
	}
	w := request(app, "POST", path, map[string]string{"X-Requested-With": "go-quick"})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "已发货") {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
	w = request(app, "GET", "/_framework/page-data?url=/projects/studio&build="+app.bundle.Build.BuildID, nil)
	if !strings.Contains(w.Body.String(), `"pending":1`) || !strings.Contains(w.Body.String(), `"shipped":3`) {
		t.Fatalf("stale data: %s", w.Body.String())
	}
}
