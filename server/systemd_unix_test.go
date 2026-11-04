//go:build unix

package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"golang.org/x/sys/unix"
)

// passSockets puts a copy of each listener at consecutive file descriptors, as systemd passes
// its sockets from 3 on, and returns the first. The test keeps its listeners, as systemd keeps
// its sockets while the program restarts. Every copy is closed when the test ends.
func passSockets(t *testing.T, lns ...net.Listener) int {
	t.Helper()
	first := 200
	for free := false; !free; first += 10 {
		free = true
		for i := range lns {
			if _, err := unix.FcntlInt(uintptr(first+i), unix.F_GETFD, 0); err == nil {
				free = false
			}
		}
	}
	first -= 10
	for i, ln := range lns {
		f, err := ln.(*net.TCPListener).File()
		if err != nil {
			t.Fatal(err)
		}
		if err := unix.Dup2(int(f.Fd()), first+i); err != nil {
			t.Fatal(err)
		}
		f.Close()
		t.Cleanup(func() { unix.Close(first + i) })
	}
	return first
}

func TestSystemdSocketsFollowTheirNames(t *testing.T) {
	pid := os.Getpid()
	env := func(vars ...string) func(string) string {
		m := map[string]string{}
		for i := 0; i < len(vars); i += 2 {
			m[vars[i]] = vars[i+1]
		}
		return func(k string) string { return m[k] }
	}
	t.Run("none", func(t *testing.T) {
		first := passSockets(t, listenLoopback(t))
		for _, getenv := range []func(string) string{
			env(),
			env("LISTEN_PID", strconv.Itoa(pid+1), "LISTEN_FDS", "1"),
			env("LISTEN_FDS", "1"),
		} {
			got, err := systemdSockets(getenv, pid, first)
			if got != nil || err != nil {
				t.Errorf("sockets of another process: %v, %v", got, err)
			}
		}
	})
	t.Run("one socket without the name of a setting serves server.addr", func(t *testing.T) {
		for _, names := range []string{"", "shop.socket"} {
			ln := listenLoopback(t)
			got, err := systemdSockets(env("LISTEN_PID", strconv.Itoa(pid), "LISTEN_FDS", "1", "LISTEN_FDNAMES", names), pid, passSockets(t, ln))
			if err != nil || len(got) != 1 || got["server.addr"].Addr().String() != ln.Addr().String() {
				t.Fatalf("names %q: %v, %v", names, got, err)
			}
			got["server.addr"].Close()
		}
	})
	t.Run("one socket named server.http_addr serves it", func(t *testing.T) {
		ln := listenLoopback(t)
		got, err := systemdSockets(env("LISTEN_PID", strconv.Itoa(pid), "LISTEN_FDS", "1", "LISTEN_FDNAMES", "server.http_addr"), pid, passSockets(t, ln))
		if err != nil || len(got) != 1 || got["server.http_addr"].Addr().String() != ln.Addr().String() {
			t.Fatalf("%v, %v", got, err)
		}
		got["server.http_addr"].Close()
	})
	t.Run("named sockets", func(t *testing.T) {
		https, http := listenLoopback(t), listenLoopback(t)
		got, err := systemdSockets(env("LISTEN_PID", strconv.Itoa(pid), "LISTEN_FDS", "2", "LISTEN_FDNAMES", "server.http_addr:server.addr"), pid, passSockets(t, http, https))
		if err != nil || len(got) != 2 || got["server.addr"].Addr().String() != https.Addr().String() || got["server.http_addr"].Addr().String() != http.Addr().String() {
			t.Fatalf("%v, %v", got, err)
		}
		for _, ln := range got {
			ln.Close()
		}
	})
	for _, c := range []struct{ fds, names, want string }{
		{"2", "", "name each with FileDescriptorName=server.addr or server.http_addr"},
		{"2", "shop.socket:shop.socket", "name each with FileDescriptorName=server.addr or server.http_addr"},
		{"2", "server.addr:server.addr", "two sockets for server.addr"},
		{"one", "", "LISTEN_FDS"},
	} {
		t.Run(c.want, func(t *testing.T) {
			first := passSockets(t, listenLoopback(t), listenLoopback(t))
			_, err := systemdSockets(env("LISTEN_PID", strconv.Itoa(pid), "LISTEN_FDS", c.fds, "LISTEN_FDNAMES", c.names), pid, first)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("LISTEN_FDS=%s LISTEN_FDNAMES=%s: %v, want %q", c.fds, c.names, err, c.want)
			}
		})
	}
}

// startOnSystemdSockets runs s as systemd starts it with the sockets lns, and waits until it
// answers on the first.
func startOnSystemdSockets(t *testing.T, s *server, names string, get func(addr string) error, lns ...net.Listener) (stop func() error) {
	t.Helper()
	listenFDsStart = passSockets(t, lns...)
	t.Cleanup(func() { listenFDsStart = 3 })
	t.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()))
	t.Setenv("LISTEN_FDS", strconv.Itoa(len(lns)))
	t.Setenv("LISTEN_FDNAMES", names)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.run(ctx) }()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		err := get(lns[0].Addr().String())
		if err == nil {
			break
		}
		select {
		case err := <-done:
			cancel()
			t.Fatalf("run: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal(err)
		}
	}
	// The program does not hand the variables on to what it starts.
	for _, name := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
		if v, ok := os.LookupEnv(name); ok {
			t.Errorf("%s=%s is still set", name, v)
		}
	}
	return func() error {
		cancel()
		return <-done
	}
}

// While the program restarts on a socket systemd keeps, the kernel takes the connections that
// arrive, and the next program answers them: a request only waits.
func TestRestartOnASystemdSocketKeepsTheConnections(t *testing.T) {
	ln := listenLoopback(t)
	// A socket that nobody serves takes the connection and never answers.
	client := &http.Client{Timeout: time.Second}
	plainGet := func(addr string) error {
		res, err := client.Get("http://" + addr + "/run")
		if err == nil {
			res.Body.Close()
		}
		return err
	}
	serve := func(run int) func() error {
		s := newServer(configWithShutdown(time.Second), Hooks{})
		s.handle("GET", "/run", func(c *tifa.Ctx) error { return c.Text(200, fmt.Sprint(run)) })
		return startOnSystemdSockets(t, s, "", plainGet, ln)
	}
	stop := serve(1)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	// No program runs now. The connection is taken all the same, and the request waits.
	conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("connection while no program runs: %v", err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "GET /run HTTP/1.1\r\nHost: shop\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	stop = serve(2)
	defer stop()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || string(body) != "2" {
		t.Fatalf("waiting request: %d %q, want the answer of the second program", res.StatusCode, body)
	}
}

// With HTTPS, the sockets named server.addr and server.http_addr serve HTTPS and the redirect.
// A socket the settings have no use for stops the start.
func TestSystemdSocketsServeHTTPSAndTheRedirect(t *testing.T) {
	cfg, _, _, certPEM, _ := certConfig(t)
	https, plain := listenLoopback(t), listenLoopback(t)
	client := h2Client(pool(t, certPEM))
	client.Timeout = time.Second
	tlsGet := func(addr string) error {
		res, err := client.Get("https://" + addr + "/hello")
		if err == nil {
			res.Body.Close()
		}
		return err
	}
	s := newServer(cfg, Hooks{})
	s.handle("GET", "/hello", hello)
	stop := startOnSystemdSockets(t, s, "server.addr:server.http_addr", tlsGet, https, plain)
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", "http://"+plain.Addr().String()+"/hello", nil)
	req.Host = testDomain
	res, err := noFollow.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("server.http_addr: %d", res.StatusCode)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}

	// Without HTTPS there is no server.http_addr to serve.
	plainCfg := configWithShutdown(time.Second)
	listenFDsStart = passSockets(t, listenLoopback(t), listenLoopback(t))
	t.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()))
	t.Setenv("LISTEN_FDS", "2")
	t.Setenv("LISTEN_FDNAMES", "server.addr:server.http_addr")
	err = newServer(plainCfg, Hooks{OnStart: func(context.Context) error { t.Error("OnStart ran"); return nil }}).run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "server.http_addr") {
		t.Fatalf("socket for server.http_addr without HTTPS: %v", err)
	}
}

func configWithShutdown(timeout time.Duration) config.Config {
	cfg := config.Default()
	cfg.Server.ShutdownTimeout = timeout
	return cfg
}
