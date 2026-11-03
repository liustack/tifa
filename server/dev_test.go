package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/deverrors"
)

// writeBundle writes a dist directory to dir whose SSR bundle renders <p>text</p>.
func writeBundle(t *testing.T, dir, text string) {
	t.Helper()
	files := map[string]string{
		"build.json": `{"buildId":"d1","head":"<script type=\"module\" src=\"/assets/app.js\"></script>"}`,
		"ssr.js":     fmt.Sprintf("globalThis.startRender = () => { globalThis.renderResult = '<p>%s</p>' }", text),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// serveApp runs the command line of app on a free address until the test ends and returns the
// base URL once the app listens. dev runs it the way DevMain does.
func serveApp(t *testing.T, app App, dev bool, args ...string) string {
	t.Helper()
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var entry func(*server, fs.FS, config.SSR) http.Handler
	if dev {
		entry = reloader
	}
	go func() {
		var out bytes.Buffer
		done <- run(ctx, app, append([]string{"--server-addr", addr}, args...), nil, &out, &out, entry)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("serve did not stop")
		}
	})
	base := "http://" + addr
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			conn.Close()
			return base
		}
		if time.Now().After(deadline) {
			t.Fatalf("the app did not listen: %v", err)
		}
		select {
		case err := <-done:
			t.Fatalf("serve: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func fetch(t *testing.T, method, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

// tifa dev asks the running app to load the SSR bundle it rebuilt. The app swaps the bundle in
// place: OnStart does not run again, and what it set up in memory stays.
func TestDevMainReloadsTheSSRBundle(t *testing.T) {
	dir := t.TempDir()
	writeBundle(t, dir, "first")
	var starts atomic.Int32
	app := frontApp()
	app.Hooks.OnStart = func(context.Context) error {
		starts.Add(1)
		return nil
	}
	base := serveApp(t, app, true, "--dist", dir)
	if code, body := fetch(t, "GET", base+"/"); code != 200 || body != "<p>first</p>" {
		t.Fatalf("first page: %d %s", code, body)
	}

	writeBundle(t, dir, "second")
	if code, body := fetch(t, "POST", base+reloadPath); code != http.StatusNoContent {
		t.Fatalf("reload: %d %s", code, body)
	}
	if code, body := fetch(t, "GET", base+"/"); code != 200 || body != "<p>second</p>" {
		t.Fatalf("page after the reload: %d %s", code, body)
	}

	// A bundle that does not load leaves the one that runs in place, and the answer says why.
	if err := os.WriteFile(filepath.Join(dir, "ssr.js"), []byte("globalThis.startRender = ("), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, body := fetch(t, "POST", base+reloadPath); code != http.StatusInternalServerError || !strings.Contains(body, "SyntaxError") {
		t.Fatalf("reload of a broken bundle: %d %s", code, body)
	}
	if code, body := fetch(t, "GET", base+"/"); code != 200 || body != "<p>second</p>" {
		t.Fatalf("page after a failed reload: %d %s", code, body)
	}
	if n := starts.Load(); n != 1 {
		t.Fatalf("OnStart ran %d times", n)
	}
}

// DevMain logs a render's Intl call that uses the default locale, Main does not.
func TestOnlyDevMainWarnsAboutIntlDefaults(t *testing.T) {
	var logs syncBuffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	for _, dev := range []bool{false, true} {
		dir := t.TempDir()
		writeBundle(t, dir, "' + (1234.5).toLocaleString() + '")
		base := serveApp(t, frontApp(), dev, "--dist", dir)
		if code, body := fetch(t, "GET", base+"/"); code != 200 || body != "<p>1,234.5</p>" {
			t.Fatalf("page: %d %s", code, body)
		}
		const warning = "Number.prototype.toLocaleString without a locale"
		if got := strings.Contains(logs.String(), warning); got != dev {
			t.Fatalf("dev %t: warned %t:\n%s", dev, got, logs.String())
		}
	}
}

// Under tifa dev the app listens on an address only tifa dev talks to, and tifa dev prints the
// one to open. The app keeps quiet about its own.
func TestOnlyMainLogsTheListeningAddress(t *testing.T) {
	var logs syncBuffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	for _, dev := range []bool{false, true} {
		logs.reset()
		dir := t.TempDir()
		writeBundle(t, dir, "page")
		base := serveApp(t, frontApp(), dev, "--dist", dir)
		// An answer comes once the app serves, after it logged or did not.
		if code, _ := fetch(t, "GET", base+"/"); code != 200 {
			t.Fatalf("dev %t: page %d", dev, code)
		}
		if got := strings.Contains(logs.String(), "tifa: listening on "+base); got == dev {
			t.Errorf("dev %t: logged the address %t:\n%s", dev, got, logs.String())
		}
	}
}

// syncBuffer is a bytes.Buffer the log package and the test can share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

// Main, the entry of production builds, has no reload entry: the path is an ordinary URL of the
// application.
func TestMainDoesNotReloadTheSSRBundle(t *testing.T) {
	dir := t.TempDir()
	writeBundle(t, dir, "first")
	base := serveApp(t, frontApp(), false, "--dist", dir)
	writeBundle(t, dir, "second")
	if code, _ := fetch(t, "POST", base+reloadPath); code == http.StatusNoContent {
		t.Fatal("a production app answered the reload request")
	}
	if code, body := fetch(t, "GET", base+"/"); code != 200 || body != "<p>first</p>" {
		t.Fatalf("page: %d %s", code, body)
	}
}

// Pages rendered while the bundle is swapped come from the old bundle or the new one, never
// from a pool that was closed under them.
func TestDevReloadKeepsServingPages(t *testing.T) {
	dir := t.TempDir()
	writeBundle(t, dir, "v0")
	base := serveApp(t, frontApp(), true, "--dist", dir)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	failures := make(chan string, 8)
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				resp, err := http.Get(base + "/")
				if err != nil {
					failures <- err.Error()
					return
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != 200 || !strings.HasPrefix(string(body), "<p>v") {
					failures <- fmt.Sprintf("%d %s", resp.StatusCode, body)
					return
				}
			}
		})
	}
	for i := 1; i <= 5; i++ {
		writeBundle(t, dir, fmt.Sprintf("v%d", i))
		if code, body := fetch(t, "POST", base+reloadPath); code != http.StatusNoContent {
			t.Errorf("reload %d: %d %s", i, code, body)
		}
	}
	close(stop)
	wg.Wait()
	close(failures)
	for f := range failures {
		t.Errorf("a page during the reloads: %s", f)
	}
}

// Under tifa dev a page that cached its render is rendered twice, and when the two differ the
// page says so in the overlay of the dev client, the terminal says so, and the page is not cached.
// A built app renders once and keeps the page. A page with RenderCache = false is rendered once.
func TestDevWarnsAboutAPageThatRendersDifferently(t *testing.T) {
	defer log.SetOutput(os.Stderr)
	const warning = "This page read the time or a random number while rendering"
	app := frontApp()
	for _, tc := range []struct {
		name       string
		dev        bool
		text, path string
		warned     bool
		render     string // X-Tifa-Render of the second request
		// read is where the warning says the app read the time, from the source map of the bundle,
		// which maps the bundle to app/page.tsx when read is set.
		read string
	}{
		{"dev, random", true, `' + Math.random() + '`, "/stats", true, "vm", ""},
		{"dev, random, with a source map", true, `' + Math.random() + '`, "/stats", true, "vm", "Math.random() at app/page.tsx:1:1"},
		// A page that shows the time to the minute renders the same twice in a row, almost always: the
		// second render runs with the clock moved on.
		{"dev, the time to the minute", true, `' + new Date().toISOString().slice(0, 16) + '`, "/stats", true, "vm", "new Date() at app/page.tsx:1:1"},
		{"dev, steady", true, "same", "/stats", false, "cache", ""},
		{"dev, RenderCache = false", true, `' + Math.random() + '`, "/live", false, "vm", ""},
		{"built, random", false, `' + Math.random() + '`, "/stats", false, "cache", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs syncBuffer
			log.SetOutput(&logs)
			dir := t.TempDir()
			writeBundle(t, dir, tc.text)
			if tc.read != "" {
				sourceMap := `{"version":3,"sources":["app/page.tsx"],"names":[],"mappings":"AAAA"}`
				if err := os.WriteFile(filepath.Join(dir, "ssr.js.map"), []byte(sourceMap), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			base := serveApp(t, app, tc.dev, "--dist", dir)
			var second *http.Response
			var body string
			for range 2 {
				resp, err := http.Get(base + tc.path)
				if err != nil {
					t.Fatal(err)
				}
				b, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				second, body = resp, string(b)
			}
			if got := strings.Contains(body, "__tifaDevPageError") && strings.Contains(body, warning); got != tc.warned {
				t.Errorf("overlay warning %t, want %t:\n%s", got, tc.warned, body)
			}
			if got := strings.Contains(logs.String(), warning); got != tc.warned {
				t.Errorf("terminal warning %t, want %t:\n%s", got, tc.warned, logs.String())
			}
			if tc.warned && (!strings.Contains(body, "RenderCache = false") || !strings.Contains(logs.String(), "<p>")) {
				t.Errorf("the warning names no fix or no place:\n%s\n%s", body, logs.String())
			}
			if tc.read != "" && (!strings.Contains(body, tc.read) || !strings.Contains(logs.String(), tc.read)) {
				t.Errorf("the warning does not say %q:\n%s\n%s", tc.read, body, logs.String())
			}
			if got := second.Header.Get("X-Tifa-Render"); got != tc.render {
				t.Errorf("second request rendered by %q, want %q", got, tc.render)
			}
			if second.StatusCode != 200 || strings.Contains(body, "__tifaDevErrorPage") {
				t.Errorf("%d, a warning is no error page:\n%s", second.StatusCode, body)
			}
		})
	}
}

// askDevErrors reads what the app under DevMain answers tifa dev at deverrors.Path.
func askDevErrors(t *testing.T, base string) deverrors.App {
	t.Helper()
	code, body := fetch(t, "GET", base+deverrors.Path)
	if code != 200 {
		t.Fatalf("GET %s: %d %s", deverrors.Path, code, body)
	}
	var got deverrors.App
	if err := json.Unmarshal([]byte(body), &got); err != nil || got.Requests == nil || got.Renders == nil {
		t.Fatalf("GET %s: %v\n%s", deverrors.Path, err, body)
	}
	return got
}

// Under DevMain the app keeps its unexpected request errors, with the request, the status OnError
// gave and the error, and the pages whose two renders differ, for tifa dev to ask for. The same
// error counts up. DELETE forgets them. Expected errors, such as a tifa.Error, are not kept. An
// app without pages keeps its errors too.
func TestDevMainKeepsRequestErrors(t *testing.T) {
	defer log.SetOutput(os.Stderr)
	log.SetOutput(io.Discard)
	app := frontApp()
	app.Routes = append(app.Routes,
		Route{ID: "app/api/disk", Path: "/api/disk", SSR: true, Layouts: []string{"app/layout"},
			GET: func(c *tifa.Ctx) error { return errors.New("the disk is full") }},
		Route{ID: "app/broken", Path: "/broken", Page: true, SSR: true, Layouts: []string{"app/layout"},
			Load: func(c *tifa.Ctx) (any, error) { return nil, errors.New("the orders table is gone") }},
	)
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error {
		if strings.Contains(err.Error(), "orders table") {
			return tifa.Error{Status: 503, Message: "Come back later"}
		}
		return tifa.Error{Status: 500}
	}
	dir := t.TempDir()
	writeBundle(t, dir, `' + Math.random() + '`)
	base := serveApp(t, app, true, "--dist", dir)
	if got := askDevErrors(t, base); len(got.Requests) != 0 || len(got.Renders) != 0 {
		t.Fatalf("errors before any request: %+v", got)
	}
	for _, path := range []string{"/api/disk", "/api/disk", "/broken?x=1", "/stats"} {
		fetch(t, "GET", base+path)
	}
	fetch(t, "DELETE", base+"/api/health") // a tifa.Error 409
	got := askDevErrors(t, base)
	if len(got.Requests) != 2 {
		t.Fatalf("requests = %+v", got.Requests)
	}
	disk, broken := got.Requests[0], got.Requests[1]
	if disk.Method != "GET" || disk.URL != "/api/disk" || disk.Status != 500 || disk.Message != "Internal Server Error" || disk.Error != "the disk is full" || disk.Count != 2 {
		t.Errorf("first = %+v", disk)
	}
	if broken.URL != "/broken?x=1" || broken.Status != 503 || broken.Message != "Come back later" || !strings.Contains(broken.Error, "the orders table is gone") || broken.Count != 1 {
		t.Errorf("second = %+v", broken)
	}
	if len(got.Renders) != 1 || got.Renders[0].URL != "/stats" || got.Renders[0].Method != "GET" ||
		!strings.Contains(got.Renders[0].Text, "read the time or a random number") || got.Renders[0].Count != 1 {
		t.Errorf("renders = %+v", got.Renders)
	}
	if code, body := fetch(t, "DELETE", base+deverrors.Path); code != http.StatusNoContent {
		t.Fatalf("DELETE: %d %s", code, body)
	}
	if got := askDevErrors(t, base); len(got.Requests) != 0 || len(got.Renders) != 0 {
		t.Fatalf("errors after DELETE: %+v", got)
	}

	api := App{Routes: []Route{{ID: "app/api/disk", Path: "/api/disk", SSR: true,
		GET: func(c *tifa.Ctx) error { return errors.New("the disk is full") }}}}
	base = serveApp(t, api, true)
	fetch(t, "GET", base+"/api/disk")
	if got := askDevErrors(t, base); len(got.Requests) != 1 || got.Requests[0].Error != "the disk is full" {
		t.Fatalf("app without pages: %+v", got)
	}
}

// Main keeps no errors: the path is an ordinary URL of the application.
func TestMainKeepsNoRequestErrors(t *testing.T) {
	dir := t.TempDir()
	writeBundle(t, dir, "page")
	base := serveApp(t, frontApp(), false, "--dist", dir)
	if code, body := fetch(t, "GET", base+deverrors.Path); code == 200 && strings.Contains(body, "requests") {
		t.Fatalf("a production app answered %s: %s", deverrors.Path, body)
	}
}
