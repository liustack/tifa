package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
)

// serveOn serves s on a fresh listener until the test ends and returns its URL.
func serveOn(t *testing.T, s *server) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- s.serveListener(ctx, ln) }()
	t.Cleanup(func() {
		stop()
		<-served
	})
	return "http://" + ln.Addr().String()
}

// slowBody sends parts one by one, pause apart, as a phone on a slow line uploads.
func slowBody(parts []string, pause time.Duration) io.Reader {
	r, w := io.Pipe()
	go func() {
		for _, p := range parts {
			time.Sleep(pause)
			if _, err := io.WriteString(w, p); err != nil {
				return
			}
		}
		w.Close()
	}()
	return r
}

// A stream that ends when tifa.ShuttingDown closes lets the program stop at once, where one that
// waits for its request context holds the stop for server.shutdown_timeout and fails it. Outside
// a server the channel is nil, which never fires.
func TestStreamsEndWhenTheServerShutsDown(t *testing.T) {
	if tifa.ShuttingDown(context.Background()) != nil {
		t.Fatal("ShuttingDown of a context that no server made is not nil")
	}
	const timeout = time.Second
	for _, watch := range []bool{true, false} {
		cfg := config.Default()
		cfg.Server.ShutdownTimeout = timeout
		var shutdown atomic.Bool
		s := newServer(cfg, Hooks{OnShutdown: func(context.Context) error { shutdown.Store(true); return nil }})
		s.handle("GET", "/events", func(c *tifa.Ctx) error {
			c.SetHeader("Content-Type", "text/event-stream")
			rc := http.NewResponseController(c.Writer())
			if err := rc.SetWriteDeadline(time.Time{}); err != nil {
				return err
			}
			stopping := tifa.ShuttingDown(c.Context())
			if !watch {
				stopping = nil
			}
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-c.Context().Done():
					return nil
				case <-stopping:
					io.WriteString(c.Writer(), "event: bye\ndata: restarting\n\n")
					return rc.Flush()
				case <-tick.C:
					io.WriteString(c.Writer(), "data: tick\n\n")
					if err := rc.Flush(); err != nil {
						return err
					}
				}
			}
		})
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ctx, stop := context.WithCancel(context.Background())
		served := make(chan error, 1)
		go func() { served <- s.serveListener(ctx, ln) }()
		res, err := http.Get("http://" + ln.Addr().String() + "/events")
		if err != nil {
			t.Fatal(err)
		}
		body := bufio.NewReader(res.Body)
		if line, err := body.ReadString('\n'); line != "data: tick\n" {
			t.Fatalf("first event %q, %v", line, err)
		}
		start := time.Now()
		stop()
		err = <-served
		took := time.Since(start)
		rest, _ := io.ReadAll(body)
		res.Body.Close()
		if watch {
			if err != nil || took > timeout/2 || !shutdown.Load() || !strings.HasSuffix(string(rest), "event: bye\ndata: restarting\n\n") {
				t.Errorf("stream that watches ShuttingDown: stopped in %v with %v, OnShutdown %v, last events %q", took, err, shutdown.Load(), rest[max(0, len(rest)-40):])
			}
		} else if err == nil || took < timeout {
			t.Errorf("stream that waits for its request: stopped in %v with %v, want the shutdown_timeout and its error", took, err)
		}
	}
}

// server.read_timeout and server.write_timeout limit every request, and a handler that writes its
// own response lifts them for itself with http.NewResponseController, through the writers of
// tifa and of compression.
func TestHandlersLiftTheTimeoutsOfTheSettings(t *testing.T) {
	const limit = 200 * time.Millisecond
	for _, compress := range []bool{true, false} {
		for _, accept := range []string{"gzip", "zstd", ""} {
			t.Run(fmt.Sprintf("compress=%v,accept=%q", compress, accept), func(t *testing.T) {
				t.Parallel()
				cfg := config.Default()
				cfg.Server.Compress = compress
				cfg.Server.ReadTimeout = limit
				cfg.Server.WriteTimeout = limit
				cfg.Server.BodyLimit = 1 << 20
				s := newServer(cfg, Hooks{})
				export := func(lift bool) func(*tifa.Ctx) error {
					return func(c *tifa.Ctx) error {
						if lift {
							if err := http.NewResponseController(c.Writer()).SetWriteDeadline(time.Now().Add(10 * limit)); err != nil {
								return err
							}
						}
						time.Sleep(2 * limit)
						c.SetHeader("Content-Type", "text/csv")
						_, err := io.WriteString(c.Writer(), strings.Repeat("id,total\n", 500))
						return err
					}
				}
				upload := func(lift bool) func(*tifa.Ctx) error {
					return func(c *tifa.Ctx) error {
						if lift {
							// The answer goes out after the body, so it needs the time too.
							rc := http.NewResponseController(c.Writer())
							if err := rc.SetReadDeadline(time.Now().Add(10 * limit)); err != nil {
								return err
							}
							if err := rc.SetWriteDeadline(time.Now().Add(10 * limit)); err != nil {
								return err
							}
						}
						body, err := io.ReadAll(c.Request().Body)
						if err != nil {
							return tifa.Error{Status: 400, Message: err.Error()}
						}
						return c.Text(200, fmt.Sprint(len(body)))
					}
				}
				s.handle("GET", "/export", export(false))
				s.handle("GET", "/export-lifted", export(true))
				s.handle("POST", "/upload", upload(false))
				s.handle("POST", "/upload-lifted", upload(true))
				url := serveOn(t, s)
				client := &http.Client{Transport: &http.Transport{DisableCompression: true}}

				do := func(method, path string, body io.Reader) (*http.Response, string, error) {
					req, _ := http.NewRequest(method, url+path, body)
					if accept != "" {
						req.Header.Set("Accept-Encoding", accept)
					}
					res, err := client.Do(req)
					if err != nil {
						return nil, "", err
					}
					defer res.Body.Close()
					b, err := io.ReadAll(res.Body)
					return res, string(b), err
				}
				if res, _, err := do("GET", "/export", nil); err == nil {
					t.Errorf("export past write_timeout: %d, want the connection cut", res.StatusCode)
				}
				res, body, err := do("GET", "/export-lifted", nil)
				if err != nil || res.StatusCode != 200 || len(body) == 0 {
					t.Errorf("export with the deadline lifted: %v", err)
				} else if want := map[bool]string{true: accept}[compress]; res.Header.Get("Content-Encoding") != want {
					t.Errorf("export with the deadline lifted: Content-Encoding %q, want %q", res.Header.Get("Content-Encoding"), want)
				}

				parts := []string{"a", "b", "c", "d", "e"}
				if res, body, err := do("POST", "/upload", slowBody(parts, limit/2)); err == nil && res.StatusCode == 200 {
					t.Errorf("upload past read_timeout: %d %q, want it to fail", res.StatusCode, body)
				}
				if res, body, err := do("POST", "/upload-lifted", slowBody(parts, limit/2)); err != nil || res.StatusCode != 200 || body != "5" {
					t.Errorf("upload with the deadline lifted: %v %q", err, body)
				}
			})
		}
	}
}
