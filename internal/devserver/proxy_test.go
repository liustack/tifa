package devserver

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func echoApp(t *testing.T, ln net.Listener) {
	t.Helper()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Host", r.Host)
		body, err := io.ReadAll(r.Body) // HTTP/1 handlers read the body before answering
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, _ = w.Write(body)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
}

func newProxy(t *testing.T, appAddr string, wait time.Duration, down func() error) *httptest.Server {
	t.Helper()
	p := httptest.NewServer(proxy(appAddr, wait, down, func() string { return devTiming("v7", 3) }))
	t.Cleanup(p.Close)
	return p
}

func post(t *testing.T, url string, body []byte) (int, string) {
	t.Helper()
	resp, err := http.Post(url, "text/plain", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(data)
}

func TestProxyRetriesWhileTheAppRestarts(t *testing.T) {
	appAddr, err := freeAddr()
	if err != nil {
		t.Fatal(err)
	}
	refused := make(chan struct{}, 1)
	testHookRetry = func() {
		select {
		case refused <- struct{}{}:
		default:
		}
	}
	t.Cleanup(func() { testHookRetry = nil })
	p := newProxy(t, appAddr, 10*time.Second, nil)

	type result struct {
		resp *http.Response
		err  error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := http.Post(p.URL+"/orders", "text/plain", strings.NewReader("ship order 7"))
		got <- result{resp, err}
	}()
	<-refused // the first attempt found no app, which now comes up
	ln, err := net.Listen("tcp", appAddr)
	if err != nil {
		t.Fatal(err)
	}
	echoApp(t, ln)
	r := <-got
	if r.err != nil {
		t.Fatal(r.err)
	}
	defer r.resp.Body.Close()
	body, err := io.ReadAll(r.resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if r.resp.StatusCode != 200 || string(body) != "ship order 7" {
		t.Fatalf("got %d %q, want the replayed body", r.resp.StatusCode, body)
	}
}

func TestProxyStreamsLargeBodies(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	echoApp(t, ln)
	p := newProxy(t, ln.Addr().String(), time.Second, nil)
	body := bytes.Repeat([]byte("x"), maxReplayBody+1)
	status, got := post(t, p.URL, body)
	if status != 200 || len(got) != len(body) {
		t.Fatalf("got %d with %d bytes, want 200 with %d", status, len(got), len(body))
	}
}

// A body too large to keep in memory is not read until the app listens, then it is sent once.
func TestProxyWaitsForTheAppBeforeALargeBody(t *testing.T) {
	appAddr, err := freeAddr()
	if err != nil {
		t.Fatal(err)
	}
	refused := make(chan struct{}, 1)
	testHookRetry = func() {
		select {
		case refused <- struct{}{}:
		default:
		}
	}
	t.Cleanup(func() { testHookRetry = nil })
	p := newProxy(t, appAddr, 10*time.Second, nil)
	body := bytes.Repeat([]byte("x"), maxReplayBody+1)
	type result struct {
		status int
		size   int
		err    error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := http.Post(p.URL, "text/plain", bytes.NewReader(body))
		if err != nil {
			got <- result{err: err}
			return
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		got <- result{resp.StatusCode, len(data), err}
	}()
	<-refused
	ln, err := net.Listen("tcp", appAddr)
	if err != nil {
		t.Fatal(err)
	}
	echoApp(t, ln)
	r := <-got
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.status != 200 || r.size != len(body) {
		t.Fatalf("got %d with %d bytes, want 200 with %d", r.status, r.size, len(body))
	}
}

func TestProxyLargeBodyStopsWaitingWhenTheAppIsDown(t *testing.T) {
	appAddr, err := freeAddr()
	if err != nil {
		t.Fatal(err)
	}
	p := newProxy(t, appAddr, time.Minute, func() error { return errors.New("go build failed") })
	status, got := post(t, p.URL, bytes.Repeat([]byte("x"), maxReplayBody+1))
	if status != http.StatusBadGateway || !strings.Contains(got, "go build failed") {
		t.Fatalf("got %d %q", status, got)
	}
}

func TestProxyGivesUpAfterWait(t *testing.T) {
	appAddr, err := freeAddr()
	if err != nil {
		t.Fatal(err)
	}
	p := newProxy(t, appAddr, 50*time.Millisecond, nil)
	status, got := post(t, p.URL, []byte("x"))
	if status != http.StatusBadGateway || !strings.Contains(got, "did not accept connections within 50ms") {
		t.Fatalf("got %d %q", status, got)
	}
	if !strings.Contains(got, `<script src="/__tifa/dev/client.js"></script>`) {
		t.Fatalf("error page %q does not load the dev client, so it would not reload", got)
	}
}

func TestProxyStopsWaitingWhenTheAppIsDown(t *testing.T) {
	appAddr, err := freeAddr()
	if err != nil {
		t.Fatal(err)
	}
	p := newProxy(t, appAddr, time.Minute, func() error { return errors.New("go build: <undefined: x>") })
	status, got := post(t, p.URL, []byte("x"))
	if status != http.StatusBadGateway || !strings.Contains(got, "go build: &lt;undefined: x&gt;") {
		t.Fatalf("got %d %q, want the escaped build error", status, got)
	}
}

func TestProxyKeepsTheHost(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	echoApp(t, ln)
	p := newProxy(t, ln.Addr().String(), time.Second, nil)
	resp, err := http.Get(p.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if host := resp.Header.Get("X-Host"); host != strings.TrimPrefix(p.URL, "http://") {
		t.Fatalf("app saw host %q, want the proxy's %q", host, p.URL)
	}
}

// Every answer names the build that made it, so the page can tell whether it is current.
func TestProxyTagsAnswersWithTheVersion(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	echoApp(t, ln)
	p := newProxy(t, ln.Addr().String(), time.Second, nil)
	resp, err := http.Get(p.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Server-Timing"); got != `tifa-dev;desc="v7", tifa-dev-render;desc="3"` {
		t.Fatalf("Server-Timing %q", got)
	}

	down := newProxy(t, ln.Addr().String()+"0", time.Second, func() error { return errors.New("build failed") })
	resp, err = http.Get(down.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Server-Timing"); got != `tifa-dev;desc="v7", tifa-dev-render;desc="3"` {
		t.Fatalf("error page Server-Timing %q", got)
	}
}

// The Vite proxy passes module requests and the upgrade of Vite's HMR websocket through.
func TestViteProxyPassesModulesAndWebsockets(t *testing.T) {
	vite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = io.WriteString(w, "module "+r.URL.Path+" for "+r.Host)
			return
		}
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = rw.Flush()
		line, err := rw.ReadString('\n')
		if err != nil {
			return
		}
		_, _ = rw.WriteString("echo " + line)
		_ = rw.Flush()
	}))
	defer vite.Close()
	front := httptest.NewServer(viteProxy(strings.TrimPrefix(vite.URL, "http://")))
	defer front.Close()

	resp, err := http.Get(front.URL + VitePath + "app/x/route.tsx")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	host := strings.TrimPrefix(front.URL, "http://")
	if want := "module " + VitePath + "app/x/route.tsx for " + host; string(body) != want {
		t.Errorf("got %q, want %q", body, want)
	}

	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, err = io.WriteString(conn, "GET "+VitePath+" HTTP/1.1\r\nHost: "+host+"\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n"+
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(conn)
	switched, err := http.ReadResponse(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if switched.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade answered %s", switched.Status)
	}
	if _, err := io.WriteString(conn, "ping\n"); err != nil {
		t.Fatal(err)
	}
	if line, err := r.ReadString('\n'); err != nil || line != "echo ping\n" {
		t.Fatalf("got %q, %v through the upgraded connection", line, err)
	}
}
