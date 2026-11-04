package server

import (
	"bufio"
	"compress/gzip"
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

// protos are the protocols a deadline test runs over: browsers get HTTP/2 when HTTPS is on.
var protos = []string{"HTTP/1.1", "HTTP/2.0"}

// protoConfig returns the settings and a client to serve and ask over proto. Neither the client
// nor the transport undoes compression.
func protoConfig(t *testing.T, proto string) (config.Config, *http.Client) {
	t.Helper()
	if proto == "HTTP/1.1" {
		return config.Default(), &http.Client{Transport: &http.Transport{DisableCompression: true}}
	}
	cfg, _, _, certPEM, _ := certConfig(t)
	return cfg, h2Client(pool(t, certPEM))
}

// serveProto serves s until the test ends, over HTTPS when its settings have a certificate, and
// returns its URL.
func serveProto(t *testing.T, s *server) string {
	t.Helper()
	if s.cfg.TLSCert == "" {
		return serveOn(t, s)
	}
	return "https://" + startHTTPS(t, s).main
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

// A stream that waits longer than server.write_timeout before a piece, as for the first words of
// a language model, sends every piece when it clears the write deadline while it waits and sets
// one only around each write. Over HTTP/2 a deadline that passes while the handler waits ends the
// stream, and setting a new one afterwards does not bring it back. Over HTTP/1.1 it does.
func TestStreamsThatWaitLongerThanTheWriteTimeout(t *testing.T) {
	const limit = 200 * time.Millisecond
	for _, proto := range protos {
		for _, accept := range []string{"gzip", ""} {
			for _, clear := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s,accept=%q,clear=%v", proto, accept, clear), func(t *testing.T) {
					t.Parallel()
					streamThatWaits(t, proto, accept, clear, limit)
				})
			}
		}
	}
}

func streamThatWaits(t *testing.T, proto, accept string, clear bool, limit time.Duration) {
	cfg, client := protoConfig(t, proto)
	cfg.Server.WriteTimeout = limit
	s := newServer(cfg, Hooks{})
	s.handle("GET", "/ask", func(c *tifa.Ctx) error {
		w := c.Writer()
		rc := http.NewResponseController(w)
		c.SetHeader("Content-Type", "text/plain; charset=utf-8")
		noDeadline := func() error {
			if !clear {
				return nil
			}
			return rc.SetWriteDeadline(time.Time{})
		}
		if err := noDeadline(); err != nil {
			return err
		}
		for _, piece := range []string{"a", "b", "c"} {
			time.Sleep(2 * limit) // the model thinks
			if err := rc.SetWriteDeadline(time.Now().Add(limit)); err != nil {
				return err
			}
			if _, err := io.WriteString(w, piece); err != nil {
				return err
			}
			if err := rc.Flush(); err != nil {
				return err
			}
			if err := noDeadline(); err != nil {
				return err
			}
		}
		return nil
	})
	req, _ := http.NewRequest("GET", serveProto(t, s)+"/ask", nil)
	if accept != "" {
		req.Header.Set("Accept-Encoding", accept)
	}
	body, err := func() (string, error) {
		res, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer res.Body.Close()
		if res.Proto != proto {
			t.Fatalf("answered over %s", res.Proto)
		}
		var r io.Reader = res.Body
		if accept == "gzip" {
			if r, err = gzip.NewReader(res.Body); err != nil {
				return "", err
			}
		}
		b, err := io.ReadAll(r)
		return string(b), err
	}()
	if clear || proto == "HTTP/1.1" {
		if body != "abc" || err != nil {
			t.Errorf("%q, %v", body, err)
		}
	} else if err == nil {
		t.Errorf("deadline passed while waiting over HTTP/2: %q, want the stream ended", body)
	}
}

// A read deadline that passed while the handler did something else can still be moved over
// HTTP/1.1. Over HTTP/2 the rest of the body then fails to read, though the answer still goes out.
func TestReadDeadlinesThatPassedOverHTTP2(t *testing.T) {
	const limit = 200 * time.Millisecond
	for _, proto := range protos {
		t.Run(proto, func(t *testing.T) {
			t.Parallel()
			cfg, client := protoConfig(t, proto)
			cfg.Server.ReadTimeout = limit
			cfg.Server.WriteTimeout = 10 * limit
			s := newServer(cfg, Hooks{})
			s.handle("POST", "/upload", func(c *tifa.Ctx) error {
				time.Sleep(2 * limit) // too late
				if err := http.NewResponseController(c.Writer()).SetReadDeadline(time.Now().Add(10 * limit)); err != nil {
					return err
				}
				b, err := io.ReadAll(c.Request().Body)
				if err != nil {
					return c.Text(200, err.Error())
				}
				return c.Text(200, fmt.Sprint(len(b)))
			})
			res, err := client.Post(serveProto(t, s)+"/upload", "text/plain", slowBody([]string{"a", "b", "c", "d", "e"}, limit/2))
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if want := map[string]string{"HTTP/1.1": "5", "HTTP/2.0": "i/o timeout"}[proto]; res.Proto != proto || string(body) != want {
				t.Errorf("%s: %q, want %q", res.Proto, body, want)
			}
		})
	}
}

// server.read_timeout and server.write_timeout limit every request, and a handler that writes its
// own response lifts them for itself with http.NewResponseController, through the writers of
// tifa and of compression.
func TestHandlersLiftTheTimeoutsOfTheSettings(t *testing.T) {
	const limit = 200 * time.Millisecond
	for _, proto := range protos {
		for _, compress := range []bool{true, false} {
			for _, accept := range []string{"gzip", "zstd", ""} {
				t.Run(fmt.Sprintf("%s,compress=%v,accept=%q", proto, compress, accept), func(t *testing.T) {
					t.Parallel()
					cfg, client := protoConfig(t, proto)
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
					url := serveProto(t, s)

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
					} else if res.Proto != proto {
						t.Errorf("answered over %s", res.Proto)
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
}
