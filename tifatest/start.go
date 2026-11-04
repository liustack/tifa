package tifatest

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/liustack/tifa/internal/analyze"
	"github.com/liustack/tifa/internal/bundle"
	"github.com/liustack/tifa/internal/codegen"
	"github.com/liustack/tifa/server"
)

// Start runs app in the test process and returns a server that answers its requests, as the
// program does: OnRequest, the decoding of inputs, Validate, sessions, redirects and OnError all
// run. app is tifaapp.App, which tifa build, tifa check and tifa dev generate: import
// <module>/.tifa/tifaapp in a test of package <name>_test.
//
// Start runs OnStart with the context of t, and when t ends stops the server: it waits for the
// requests in flight, then runs OnShutdown. The settings of tifa are the defaults with those of the
// project tifa.yaml over them, as in the program, except the address and HTTPS: the server listens
// on HTTPS on this machine with a certificate of its own. The settings of the app keep their
// defaults in a test binary, so set the variables before Start, such as the path of the database.
//
// srv.Client() trusts the certificate, keeps the cookies the answers set and does not follow
// redirects, so the test sees each 3xx. There is no front end: a page answers the results of its
// Load and Layout functions as JSON by route ID. StartPages renders pages.
//
// Start fails the test when .tifa/tifaapp was generated from other routes than the app has now, and
// when it runs in a parallel test: one app runs in a test binary at a time, since the session
// store, SetHTTPHandler and the content collections are those of the process.
func Start(t testing.TB, app server.App) *httptest.Server {
	t.Helper()
	if !checkTable(t, "Start", app) {
		return nil
	}
	return start(t, "Start", app)
}

// StartPages is Start with the front end that tifa build or tifa dev wrote to .tifa/dist: pages
// render to HTML as in the program, and the files of the bundle and of the public directory are
// served. The VMs are made once per test binary and shared by every StartPages of it, two of them
// whatever ssr.vms says. tifa check does not bundle, so run tifa build before the test, or keep tifa
// dev running.
//
// StartPages fails the test when .tifa/dist is missing, and when a file it was built from changed
// since: a page, a component, a stylesheet, package.json, the lock file, the Vite config,
// tifa.yaml, a public file or a file of content/.
func StartPages(t testing.TB, app server.App) *httptest.Server {
	t.Helper()
	if !checkTable(t, "StartPages", app) {
		return nil
	}
	root, err := appRoot()
	if err != nil {
		t.Fatalf("tifatest.StartPages: %v", err)
		return nil
	}
	dir := filepath.Join(root, ".tifa")
	dist := filepath.Join(dir, "dist")
	if _, err := os.Stat(filepath.Join(dist, "build.json")); err != nil {
		t.Fatalf("tifatest.StartPages: no front end in %s (%v): it renders pages with the one tifa build or tifa dev writes there, run tifa build first", dist, err)
		return nil
	}
	changed, err := bundle.ChangedInputs(root, dist)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		t.Fatalf("tifatest.StartPages: %s has no %s, which tells what it was built from: run tifa build again", dist, bundle.InputsFile)
		return nil
	case err != nil:
		t.Fatalf("tifatest.StartPages: %v", err)
		return nil
	case len(changed) > 0:
		names := changed
		more := ""
		if len(names) > 5 {
			names, more = names[:5], fmt.Sprintf(" and %d more", len(changed)-5)
		}
		t.Fatalf("tifatest.StartPages: the front end in %s was built before these files changed: %s%s: run tifa build again, or keep tifa dev running", dist, strings.Join(names, ", "), more)
		return nil
	}
	app.Dist = os.DirFS(dir)
	return start(t, "StartPages", app)
}

// tables holds what checkTable found for each application root, as a tableCheck.
var tables sync.Map

type tableCheck struct {
	once        sync.Once
	fingerprint string
	err         error
}

// checkTable fails the test of fn when the fingerprint of app is not that of the routes in the
// folder of the go.mod that holds the current directory. An App without one, written by hand,
// passes. The analysis runs once per root.
func checkTable(t testing.TB, fn string, app server.App) bool {
	t.Helper()
	if app.Fingerprint == "" {
		return true
	}
	root, err := appRoot()
	if err != nil {
		t.Fatalf("tifatest.%s: %v", fn, err)
		return false
	}
	v, _ := tables.LoadOrStore(root, &tableCheck{})
	check := v.(*tableCheck)
	check.once.Do(func() {
		m, err := analyze.Analyze(root)
		if err != nil {
			check.err = fmt.Errorf("analyzing the app at %s: %w", root, err)
			return
		}
		check.fingerprint, check.err = codegen.AppFingerprint(root, m)
	})
	switch {
	case check.err != nil:
		t.Fatalf("tifatest.%s: %v", fn, check.err)
		return false
	case check.fingerprint != app.Fingerprint:
		t.Fatalf("tifatest.%s: .tifa/tifaapp was generated from other routes than the app at %s has now: run tifa check, or keep tifa dev running, and run the test again", fn, root)
		return false
	}
	return true
}

// start serves app on a test server that stops when t ends.
func start(t testing.TB, fn string, app server.App) *httptest.Server {
	t.Helper()
	if inParallel(t) {
		t.Fatalf("tifatest.%s: the app runs in the whole test binary, so a test that starts it cannot run in parallel: remove t.Parallel from this test and its parents", fn)
		return nil
	}
	// The server listens from here on, so every way out closes it.
	srv := httptest.NewUnstartedServer(nil)
	stop, err := server.StartTest(t.Context(), app, srv.Config)
	if err != nil {
		srv.Close()
		t.Fatalf("tifatest.%s: %v", fn, err)
		return nil
	}
	srv.StartTLS()
	t.Cleanup(func() {
		if err := stop(srv.Close); err != nil {
			t.Errorf("tifatest.%s: stopping the app: %v", fn, err)
		}
	})
	client := srv.Client()
	client.Jar, err = cookiejar.New(nil)
	if err != nil {
		t.Fatalf("tifatest.%s: %v", fn, err)
		return nil
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return srv
}

// inParallel reports whether t or a parent of it called t.Parallel, which t.Setenv refuses.
func inParallel(t testing.TB) (parallel bool) {
	defer func() {
		if recover() != nil {
			parallel = true
		}
	}()
	t.Setenv("TIFA_START_TEST", "1")
	return false
}
