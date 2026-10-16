package server

import (
	"bytes"
	"context"
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

	"github.com/liustack/tifa/internal/config"
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
	var entry func(*Server, fs.FS, config.SSR) http.Handler
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
	app.Hooks.OnStart = func() error {
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
