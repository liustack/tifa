package devserver

import (
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
	p := httptest.NewServer(proxy(appAddr, wait, down))
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

func TestProxyDoesNotRetryLargeBodies(t *testing.T) {
	appAddr, err := freeAddr()
	if err != nil {
		t.Fatal(err)
	}
	p := newProxy(t, appAddr, time.Minute, nil)
	status, got := post(t, p.URL, bytes.Repeat([]byte("x"), maxReplayBody+1))
	if status != http.StatusBadGateway || !strings.Contains(got, "cannot be sent again") {
		t.Fatalf("got %d %q, want 502 explaining the body is too large to retry", status, got)
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
