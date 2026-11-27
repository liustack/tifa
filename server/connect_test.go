package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa"
)

// connectApp is frontApp with an API route, served over HTTP/1.1, and its address.
func connectApp(t *testing.T, onStart func(context.Context) error) string {
	t.Helper()
	app := frontApp()
	app.Routes = append(app.Routes, Route{ID: "app/api/ping", Path: "/api/ping", SSR: true, Layouts: []string{"app/layout"},
		GET: func(c *tifa.Ctx) error { return c.Text(200, "pong") }})
	app.Hooks.OnStart = onStart
	return strings.TrimPrefix(serveApp(t, app, false, "--dist", bundleWithFiles(t)), "http://")
}

// sendConnect writes a CONNECT for target on a new connection and returns the status line and the
// headers of the answer, and the reader of the rest.
func sendConnect(t *testing.T, addr, target string) (net.Conn, *bufio.Reader, *http.Response) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, addr)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: "CONNECT"})
	if err != nil {
		t.Fatalf("CONNECT %s: %v", target, err)
	}
	return conn, br, resp
}

// CONNECT works on the connection, so an app takes it in the wrapper of tifa.SetHTTPHandler, which
// sees it before routing, whether it names a host and port, as a client asks a proxy, or a path.
// The wrapper takes the connection over and the bytes after the answer go through it.
func TestConnectReachesTheHTTPHandler(t *testing.T) {
	t.Cleanup(func() { tifa.SetHTTPHandler(nil) })
	addr := connectApp(t, func(context.Context) error {
		tifa.SetHTTPHandler(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodConnect {
					next.ServeHTTP(w, r)
					return
				}
				conn, rw, err := http.NewResponseController(w).Hijack()
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				defer conn.Close()
				fmt.Fprintf(rw, "HTTP/1.1 200 Connection established\r\n\r\n")
				rw.Flush()
				line, _ := rw.ReadString('\n')
				fmt.Fprintf(rw, "%s %s %s", r.Host, r.URL.Path, line)
				rw.Flush()
			})
		})
		return nil
	})
	for _, tc := range []struct{ target, want string }{
		{"db.internal:5432", "db.internal:5432  hello\n"},
		{"/api/ping", addr + " /api/ping hello\n"},
	} {
		conn, br, resp := sendConnect(t, addr, tc.target)
		if resp.StatusCode != 200 {
			t.Errorf("CONNECT %s: %d", tc.target, resp.StatusCode)
			continue
		}
		fmt.Fprintf(conn, "hello\n")
		got, err := br.ReadString('\n')
		if err != nil || got != tc.want {
			t.Errorf("CONNECT %s: tunnel read %q, %v, want %q", tc.target, got, err, tc.want)
		}
	}
}

// Without a wrapper, a CONNECT to the path of a route gets 405 and an Allow without CONNECT, and
// one for a host and port, which no route can match, gets 404.
func TestConnectWithoutAWrapper(t *testing.T) {
	addr := connectApp(t, nil)
	_, _, resp := sendConnect(t, addr, "/api/ping")
	if resp.StatusCode != 405 || resp.Header.Get("Allow") != "GET, HEAD, OPTIONS" {
		t.Errorf("CONNECT /api/ping: %d, Allow %q", resp.StatusCode, resp.Header.Get("Allow"))
	}
	_, _, resp = sendConnect(t, addr, "db.internal:5432")
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != 404 {
		t.Errorf("CONNECT db.internal:5432: %d", resp.StatusCode)
	}
}
