//go:build unix

package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/codegen"
	"github.com/liustack/tifa/internal/deverrors"
)

// fakeDev holds the lock of a new app as tifa dev does, with the address of a server that answers
// report at deverrors.Path, and returns the app root.
func fakeDev(t *testing.T, report string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != deverrors.Path {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(report))
	}))
	t.Cleanup(srv.Close)
	root := t.TempDir()
	l, err := lockApp(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	if err := l.SetURL(srv.URL); err != nil {
		t.Fatal(err)
	}
	return root
}

func exitCodeOf(err error) int {
	var e *exitError
	if errors.As(err, &e) {
		return e.code
	}
	if err != nil {
		return 1
	}
	return 0
}

const fullReport = `{
  "failure": {"title": "Build failed", "error": "go build: app/store.go:3:2: undefined: x\n\u001b[31mexit status 1\u001b[39m"},
  "requests": [{"method": "GET", "url": "/orders?page=2", "status": 503, "message": "Come back later", "error": "the orders table is gone\n    at Orders (app/orders/page.tsx:12:7)", "count": 2},
    {"method": "POST", "url": "/api/ship", "status": 500, "message": "Internal Server Error", "error": "the disk is full", "count": 1}],
  "browser": [{"kind": "recoverable", "title": "Hydration mismatch", "url": "http://127.0.0.1:9527/orders", "text": "Hydration failed\n    at Clock", "count": 3}],
  "renders": [{"method": "GET", "url": "/stats", "text": "This page read the time or a random number while rendering", "count": 1}]
}`

// tifa errors prints every error with a heading and its text below, and exits with status 1.
func TestErrorsPrintsTheReport(t *testing.T) {
	root := fakeDev(t, fullReport)
	out, stderr, err := run(t, "errors", "--dir", root)
	if code := exitCodeOf(err); code != 1 || stderr != "" {
		t.Fatalf("exit %d (%v), stderr %q", code, err, stderr)
	}
	want := `Build failed
  go build: app/store.go:3:2: undefined: x
  exit status 1

Request failed: GET /orders?page=2, 503 Service Unavailable (×2)
  Users see: Come back later
  the orders table is gone
      at Orders (app/orders/page.tsx:12:7)

Request failed: POST /api/ship, 500 Internal Server Error
  the disk is full

Hydration mismatch in the browser: http://127.0.0.1:9527/orders (×3)
  Hydration failed
      at Clock

Renders differently each time: GET /stats
  This page read the time or a random number while rendering
`
	if out != want {
		t.Fatalf("output:\n%s\nwant:\n%s", out, want)
	}
}

// With --json the report is printed as tifa dev gives it, with every field, and the status is 1.
func TestErrorsJSON(t *testing.T) {
	root := fakeDev(t, fullReport)
	out, _, err := run(t, "errors", "--json", "--dir", root)
	if code := exitCodeOf(err); code != 1 {
		t.Fatalf("exit %d (%v)", code, err)
	}
	var r deverrors.Report
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if r.Failure == nil || len(r.Requests) != 2 || r.Requests[0].Count != 2 || len(r.Browser) != 1 || len(r.Renders) != 1 {
		t.Fatalf("report = %+v", r)
	}
	if !strings.Contains(out, `"message": "Come back later"`) {
		t.Fatalf("not indented JSON:\n%s", out)
	}
}

// Without errors one line says so, and the status is 0, also with --json.
func TestErrorsWithoutErrors(t *testing.T) {
	root := fakeDev(t, `{"failure":null,"requests":[],"browser":[],"renders":[]}`)
	out, _, err := run(t, "errors", "--dir", root)
	if err != nil || out != "No errors since the last build.\n" {
		t.Fatalf("%q, %v", out, err)
	}
	out, _, err = run(t, "errors", "--json", "--dir", root)
	if err != nil || strings.Join(strings.Fields(out), "") != `{"failure":null,"requests":[],"browser":[],"renders":[]}` {
		t.Fatalf("--json: %q, %v", out, err)
	}
}

// Without a tifa dev that listens in the app, tifa errors says so and exits with status 2: no
// lock file, a lock no one holds, a holder without an address (tifa build, or a tifa dev that does
// not listen yet), and an address that does not answer.
func TestErrorsWithoutTifaDev(t *testing.T) {
	const notRunning = "tifa dev is not running in this app. Start it with go tool tifa dev"
	noFile := t.TempDir()

	free := t.TempDir()
	l, err := lockApp(free)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.SetURL("http://127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	l.Close()

	building := t.TempDir()
	l, err = lockApp(building)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })

	gone := t.TempDir()
	l2, err := lockApp(gone)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l2.Close() })
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	if err := l2.SetURL(srv.URL); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, root, want string }{
		{"no lock file", noFile, notRunning},
		{"a free lock", free, notRunning},
		{"no address", building, notRunning},
		{"no answer", gone, "cannot reach tifa dev at " + srv.URL},
	} {
		out, _, err := run(t, "errors", "--dir", tc.root)
		if code := exitCodeOf(err); code != 2 || !strings.Contains(err.Error(), tc.want) || out != "" {
			t.Errorf("%s: exit %d, %v, stdout %q", tc.name, code, err, out)
		}
	}
	// Asking does not create .tifa.
	if _, err := os.Stat(filepath.Join(noFile, codegen.Dir)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".tifa after tifa errors: %v", err)
	}
	// Nor does it take the lock: tifa dev can start after it.
	again, err := lockApp(free)
	if err != nil {
		t.Fatalf("lock after tifa errors: %v", err)
	}
	again.Close()
}
