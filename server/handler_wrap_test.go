package server

import (
	"context"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
)

// statusRecorder keeps the status a handler wrote, as a metrics middleware does.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// bundleWithFiles is a dist with one asset and one public file.
func bundleWithFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeBundle(t, dir, "page")
	for name, content := range map[string]string{"assets/app.js": "console.log(1)", "public/robots.txt": "User-agent: *"} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func post(t *testing.T, url string) int {
	t.Helper()
	req, err := http.NewRequest("POST", url, strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// A wrapper set in OnStart sees every request with the real ResponseWriter, the answers of the
// static files and of the cross-site check among them, and can serve paths of its own.
func TestSetHTTPHandlerWrapsEveryRequest(t *testing.T) {
	// Registered first, so it runs after the cleanup that stops the server.
	t.Cleanup(func() { tifa.SetHTTPHandler(nil) })
	var mu sync.Mutex
	seen := map[string]int{}
	app := frontApp()
	app.Hooks.OnStart = func() error {
		tifa.SetHTTPHandler(func(next http.Handler) http.Handler {
			mux := http.NewServeMux()
			mux.HandleFunc("/debug/pprof/", pprof.Index)
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				rec := &statusRecorder{ResponseWriter: w, status: 200}
				next.ServeHTTP(rec, r)
				mu.Lock()
				seen[r.Method+" "+r.URL.Path] = rec.status
				mu.Unlock()
			})
			return mux
		})
		return nil
	}
	base := serveApp(t, app, false, "--dist", bundleWithFiles(t))
	if code, _ := fetch(t, "GET", base+"/projects/p1"); code != 200 {
		t.Fatalf("page %d", code)
	}
	if code, _ := fetch(t, "GET", base+"/assets/app.js"); code != 200 {
		t.Fatalf("asset %d", code)
	}
	if code, body := fetch(t, "GET", base+"/debug/pprof/"); code != 200 || !strings.Contains(body, "goroutine") {
		t.Fatalf("pprof %d %.80s", code, body)
	}
	if code := post(t, base+"/projects/p1"); code != 403 {
		t.Fatalf("cross-site write %d", code)
	}
	mu.Lock()
	defer mu.Unlock()
	want := map[string]int{"GET /projects/p1": 200, "GET /assets/app.js": 200, "POST /projects/p1": 403}
	for k, v := range want {
		if seen[k] != v {
			t.Errorf("%s: the wrapper saw %d, want %d (all: %v)", k, seen[k], v, seen)
		}
	}
	if _, ok := seen["GET /debug/pprof/"]; ok {
		t.Errorf("pprof went through the app: %v", seen)
	}
}

// Without a wrapper the server serves as before, and a call after it listens panics.
func TestSetHTTPHandlerAfterListeningPanics(t *testing.T) {
	base := serveApp(t, frontApp(), false, "--dist", bundleWithFiles(t))
	if code, _ := fetch(t, "GET", base+"/projects/p1"); code != 200 {
		t.Fatalf("page %d", code)
	}
	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "OnStart") {
			t.Fatalf("recovered %v", r)
		}
	}()
	tifa.SetHTTPHandler(func(next http.Handler) http.Handler { return next })
	t.Fatal("no panic")
}

// A shutdown waits for a request the wrapper holds.
func TestShutdownWaitsForWrappedRequests(t *testing.T) {
	defer tifa.SetHTTPHandler(nil)
	inside, release := make(chan struct{}), make(chan struct{})
	tifa.SetHTTPHandler(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/slow" {
				close(inside)
				<-release
				w.WriteHeader(204)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	cfg := config.Default()
	cfg.Server.Addr = freeAddr(t)
	s := New(cfg, Hooks{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	got := make(chan int, 1)
	go func() {
		for i := 0; i < 500; i++ {
			resp, err := http.Get("http://" + cfg.Server.Addr + "/slow")
			if err == nil {
				resp.Body.Close()
				got <- resp.StatusCode
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		got <- 0
	}()
	select {
	case <-inside:
	case <-time.After(10 * time.Second):
		t.Fatal("the request never reached the wrapper")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("Run returned with the request in flight: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if code := <-got; code != 204 {
		t.Fatalf("the wrapped request got %d", code)
	}
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// Each running server holds the wrapper on its own: one that fails to listen, or stops first,
// does not let SetHTTPHandler through while another still listens.
func TestSetHTTPHandlerStaysSealedWhileAnyServerRuns(t *testing.T) {
	start := func(addr string) (stop func() error) {
		cfg := config.Default()
		cfg.Server.Addr = addr
		s := New(cfg, Hooks{})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- s.Run(ctx) }()
		return func() error { cancel(); return <-done }
	}
	listening := func(addr string) {
		t.Helper()
		for range 500 {
			if resp, err := http.Get("http://" + addr + "/"); err == nil {
				resp.Body.Close()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("%s does not listen", addr)
	}
	sealed := func() (panicked bool) {
		defer func() { panicked = recover() != nil }()
		tifa.SetHTTPHandler(nil)
		return
	}
	addrA, addrC := freeAddr(t), freeAddr(t)
	stopA := start(addrA)
	listening(addrA)
	// B wants the address of A and fails to listen.
	if err := start(addrA)(); err == nil {
		t.Fatal("a second server listened on the same address")
	}
	if !sealed() {
		t.Fatal("SetHTTPHandler worked after a server failed to listen, with another one running")
	}
	stopC := start(addrC)
	listening(addrC)
	if err := stopC(); err != nil {
		t.Fatal(err)
	}
	if !sealed() {
		t.Fatal("SetHTTPHandler worked after one of two servers stopped")
	}
	if err := stopA(); err != nil {
		t.Fatal(err)
	}
	if sealed() {
		t.Fatal("SetHTTPHandler panicked after every server stopped")
	}
}
