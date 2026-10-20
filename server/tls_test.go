package server

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
)

// testDomain is the name the certificates of these tests are for. autocert wants a dot in it.
const testDomain = "app.example.test"

// selfSigned returns a certificate for testDomain that is its own CA, in PEM, and its serial.
func selfSigned(t *testing.T) (certPEM, keyPEM []byte, serial *big.Int) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err = rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: testDomain}, DNSNames: []string{testDomain},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), serial
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// httpsServer is a Server serving HTTPS on main and its plain HTTP address on plain, as Run
// does, on ports of the loopback.
type httpsServer struct {
	main, plain string
	stop        func() error // cancels serving and returns what serve returned
}

func startHTTPS(t *testing.T, s *Server) httpsServer {
	t.Helper()
	tlsConfig, plain, ready, err := s.https()
	if err != nil {
		t.Fatal(err)
	}
	if tlsConfig == nil {
		t.Fatal("HTTPS is off")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	plainLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- s.serveBoth(ctx, ln, tlsConfig, plainLn, plain) }()
	ready()
	var once sync.Once
	var serveErr error
	stop := func() error {
		once.Do(func() {
			cancel()
			select {
			case serveErr = <-served:
			case <-time.After(10 * time.Second):
				serveErr = errors.New("serve did not stop")
			}
		})
		return serveErr
	}
	t.Cleanup(func() { stop() })
	return httpsServer{main: ln.Addr().String(), plain: plainLn.Addr().String(), stop: stop}
}

// h2Client trusts roots, sends testDomain as the server name and speaks HTTP/2. Every request
// gets a new connection, so it sees the certificate the server has now.
func h2Client(roots *x509.CertPool) *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: roots, ServerName: testDomain},
		ForceAttemptHTTP2: true, DisableKeepAlives: true, DisableCompression: true,
	}}
}

func pool(t *testing.T, certPEM []byte) *x509.CertPool {
	t.Helper()
	p := x509.NewCertPool()
	if !p.AppendCertsFromPEM(certPEM) {
		t.Fatal("no certificate in PEM")
	}
	return p
}

// getTLS returns the answer to a GET of path, whose body is read, and the serial of the
// certificate the server showed.
func getTLS(t *testing.T, client *http.Client, url string) (*http.Response, string, *big.Int) {
	t.Helper()
	res, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, string(body), res.TLS.PeerCertificates[0].SerialNumber
}

func certConfig(t *testing.T) (cfg config.Config, certPath, keyPath string, certPEM []byte, serial *big.Int) {
	t.Helper()
	dir := t.TempDir()
	certPath, keyPath = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	certPEM, keyPEM, serial := selfSigned(t)
	writeFile(t, certPath, certPEM)
	writeFile(t, keyPath, keyPEM)
	cfg = config.Default()
	cfg.Server.Addr = "127.0.0.1:0" // HTTPS refuses the default address
	cfg.Server.TLSCert, cfg.Server.TLSKey = certPath, keyPath
	return cfg, certPath, keyPath, certPEM, serial
}

func hello(c *tifa.Ctx) error {
	_, err := io.WriteString(c.Writer(), "hello over "+c.Request().Proto)
	return err
}

// With server.tls_cert, browsers get HTTP/2, which they speak only over TLS.
func TestOwnCertificateServesHTTP2(t *testing.T) {
	cfg, _, _, certPEM, serial := certConfig(t)
	s := New(cfg, Hooks{})
	s.Handle("GET", "/hello", hello)
	srv := startHTTPS(t, s)
	res, body, got := getTLS(t, h2Client(pool(t, certPEM)), "https://"+srv.main+"/hello")
	if res.StatusCode != 200 || res.ProtoMajor != 2 || body != "hello over HTTP/2.0" {
		t.Fatalf("%d %s %q", res.StatusCode, res.Proto, body)
	}
	if got.Cmp(serial) != 0 {
		t.Fatalf("serial %v, want %v", got, serial)
	}
	if err := srv.stop(); err != nil {
		t.Fatal(err)
	}
}

// A renewed certificate takes effect at the next connection, whether the files are written
// again in place or replaced, as certbot replaces the links of its live folder. Files that do not
// load, such as a certificate written before its key, leave the certificate loaded before.
func TestCertificateFilesLoadAgainWhenTheyChange(t *testing.T) {
	cfg, certPath, keyPath, firstPEM, first := certConfig(t)
	s := New(cfg, Hooks{})
	s.Handle("GET", "/hello", hello)
	srv := startHTTPS(t, s)
	url := "https://" + srv.main + "/hello"
	if _, _, got := getTLS(t, h2Client(pool(t, firstPEM)), url); got.Cmp(first) != 0 {
		t.Fatalf("serial %v, want %v", got, first)
	}

	// Written in place. The time moves on, as it would between two renewals.
	secondPEM, secondKey, second := selfSigned(t)
	writeFile(t, certPath, secondPEM)
	writeFile(t, keyPath, secondKey)
	later := time.Now().Add(time.Minute)
	for _, p := range []string{certPath, keyPath} {
		if err := os.Chtimes(p, later, later); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, got := getTLS(t, h2Client(pool(t, secondPEM)), url); got.Cmp(second) != 0 {
		t.Fatalf("written in place: serial %v, want %v", got, second)
	}

	// Half done: the new certificate is there, its key not yet.
	thirdPEM, thirdKey, third := selfSigned(t)
	writeFile(t, certPath+".new", thirdPEM)
	if err := os.Rename(certPath+".new", certPath); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, _, got := getTLS(t, h2Client(pool(t, secondPEM)), url); got.Cmp(second) != 0 {
			t.Fatalf("half replaced: serial %v, want the one before, %v", got, second)
		}
	}

	// Replaced by a rename, as certbot and Kubernetes secrets do.
	writeFile(t, keyPath+".new", thirdKey)
	if err := os.Rename(keyPath+".new", keyPath); err != nil {
		t.Fatal(err)
	}
	if _, _, got := getTLS(t, h2Client(pool(t, thirdPEM)), url); got.Cmp(third) != 0 {
		t.Fatalf("replaced: serial %v, want %v", got, third)
	}
}

// Files that do not load stop the program before OnStart, with the setting in the error.
func TestRunFailsOnCertificateFilesThatDoNotLoad(t *testing.T) {
	cfg, _, keyPath, _, _ := certConfig(t)
	_, otherKey, _ := selfSigned(t)
	writeFile(t, keyPath, otherKey)
	cfg.Server.Addr = "127.0.0.1:0"
	started := false
	err := New(cfg, Hooks{OnStart: func() error { started = true; return nil }}).Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "server.tls_cert") || started {
		t.Fatalf("err %v, OnStart ran: %v", err, started)
	}
	cfg.Server.TLSCert = filepath.Join(t.TempDir(), "missing.pem")
	err = New(cfg, Hooks{}).Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing.pem") {
		t.Fatalf("missing file: %v", err)
	}
}

// Settings that do not go together stop the program before OnStart.
func TestRunChecksTheHTTPSSettings(t *testing.T) {
	cfg := config.Default()
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Server.TLSDomains = []string{testDomain}
	started := false
	err := New(cfg, Hooks{OnStart: func() error { started = true; return nil }}).Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "server.tls_cache") || started {
		t.Fatalf("err %v, OnStart ran: %v", err, started)
	}
}

// server.http_addr redirects every request to the same URL over HTTPS, on port 443: the port
// browsers use, also when server.addr names another one, as when a container maps 443 to it.
func TestPlainHTTPRedirectsToHTTPS(t *testing.T) {
	cfg, _, _, _, _ := certConfig(t)
	cfg.Server.HSTS = true
	s := New(cfg, Hooks{})
	s.Handle("GET", "/hello", hello)
	srv := startHTTPS(t, s)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, tc := range []struct{ method, host, path, want string }{
		{"GET", testDomain, "/hello?a=1&b=%20", "https://" + testDomain + "/hello?a=1&b=%20"},
		{"HEAD", testDomain + ":8080", "/", "https://" + testDomain + "/"},
		{"POST", testDomain, "/orders", "https://" + testDomain + "/orders"},
		{"GET", "[2001:db8::1]:80", "/x", "https://[2001:db8::1]/x"},
		{"GET", "[2001:db8::1]", "/x", "https://[2001:db8::1]/x"},
	} {
		req, _ := http.NewRequest(tc.method, "http://"+srv.plain+tc.path, nil)
		req.Host = tc.host
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusPermanentRedirect || res.Header.Get("Location") != tc.want {
			t.Errorf("%s %s%s: %d %q, want 308 %q", tc.method, tc.host, tc.path, res.StatusCode, res.Header.Get("Location"), tc.want)
		}
		// HSTS over plain HTTP means nothing to browsers (RFC 6797, 8.1), so it is left out.
		if res.Header.Get("Strict-Transport-Security") != "" {
			t.Errorf("%s %s: HSTS over plain HTTP", tc.method, tc.path)
		}
	}
}

// HTTPS on a loopback address set by hand starts, since it may be a test on this machine, but
// the log says nobody else reaches it.
func TestHTTPSOnTheLoopbackWarns(t *testing.T) {
	cfg, _, _, _, _ := certConfig(t)
	cfg.Server.Addr, cfg.Server.HTTPAddr = freeAddr(t), "off"
	var out strings.Builder
	var mu sync.Mutex
	log.SetOutput(writerFunc(func(b []byte) (int, error) { mu.Lock(); defer mu.Unlock(); return out.Write(b) }))
	defer log.SetOutput(os.Stderr)
	ctx, cancel := context.WithCancel(context.Background())
	if err := New(cfg, Hooks{OnStart: func() error { cancel(); return nil }}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(out.String(), "loopback") || !strings.Contains(out.String(), "server.addr") {
		t.Fatalf("log:\n%s", out.String())
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(b []byte) (int, error) { return f(b) }

// server.hsts adds Strict-Transport-Security to every answer of server.addr, the 404 of an
// unknown URL included. Off, the default, it adds nothing.
func TestHSTS(t *testing.T) {
	for _, on := range []bool{false, true} {
		cfg, _, _, certPEM, _ := certConfig(t)
		cfg.Server.HSTS = on
		s := New(cfg, Hooks{})
		s.Handle("GET", "/hello", hello)
		srv := startHTTPS(t, s)
		for _, path := range []string{"/hello", "/nowhere"} {
			res, _, _ := getTLS(t, h2Client(pool(t, certPEM)), "https://"+srv.main+path)
			want := ""
			if on {
				want = "max-age=31536000"
			}
			if got := res.Header.Get("Strict-Transport-Security"); got != want {
				t.Errorf("hsts %v, %s: %q, want %q", on, path, got, want)
			}
		}
	}
}

// Over HTTP/2, pages are compressed as over HTTP/1.1, a stream that flushes before its body
// sends its header and first event at once, and a handler that hijacks gets an error, since an
// HTTP/2 connection carries other requests too.
func TestCompressionAndStreamsOverHTTP2(t *testing.T) {
	cfg, _, _, certPEM, _ := certConfig(t)
	s := newBigServer(t, cfg, frontApp())
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	s.Handle("GET", "/events", func(c *tifa.Ctx) error {
		c.SetHeader("Content-Type", "text/event-stream")
		if err := http.NewResponseController(c.Writer()).Flush(); err != nil {
			return err
		}
		io.WriteString(c.Writer(), "data: first\n\n")
		if err := http.NewResponseController(c.Writer()).Flush(); err != nil {
			return err
		}
		<-release
		return nil
	})
	hijack := make(chan error, 1)
	s.Handle("GET", "/hijack", func(c *tifa.Ctx) error {
		_, _, err := http.NewResponseController(c.Writer()).Hijack()
		hijack <- err
		return err
	})
	srv := startHTTPS(t, s)
	client := h2Client(pool(t, certPEM))
	do := func(path string) *http.Response {
		req, _ := http.NewRequest("GET", "https://"+srv.main+path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if res.ProtoMajor != 2 {
			t.Fatalf("%s over %s", path, res.Proto)
		}
		return res
	}

	page := do("/projects/p1")
	zr, err := gzip.NewReader(page.Body)
	if err != nil || page.Header.Get("Content-Encoding") != "gzip" || !strings.HasPrefix(page.Header.Get("ETag"), "W/") {
		t.Fatalf("page: %v %v", page.Header, err)
	}
	html, _ := io.ReadAll(zr)
	page.Body.Close()
	if !strings.Contains(string(html), "tifa renders this page") {
		t.Fatalf("page: %.100s", html)
	}

	events := do("/events")
	line, err := bufio.NewReader(events.Body).ReadString('\n')
	if err != nil || line != "data: first\n" || events.Header.Get("Content-Encoding") != "" {
		t.Fatalf("first event: %q %v %v", line, events.Header, err)
	}
	once.Do(func() { close(release) })
	events.Body.Close()

	res := do("/hijack")
	res.Body.Close()
	if err := <-hijack; !errors.Is(err, http.ErrNotSupported) || res.StatusCode != 500 {
		t.Fatalf("hijack over HTTP/2: %d, %v", res.StatusCode, err)
	}
}

// Stopping serves both addresses down: neither accepts connections afterwards, and requests in
// flight on server.addr finish first.
func TestShutdownStopsBothAddresses(t *testing.T) {
	cfg, _, _, certPEM, _ := certConfig(t)
	inFlight, finish := make(chan struct{}), make(chan struct{})
	s := New(cfg, Hooks{})
	s.Handle("GET", "/slow", func(c *tifa.Ctx) error {
		close(inFlight)
		<-finish
		_, err := io.WriteString(c.Writer(), "done")
		return err
	})
	srv := startHTTPS(t, s)
	answer := make(chan string, 1)
	go func() {
		res, err := h2Client(pool(t, certPEM)).Get("https://" + srv.main + "/slow")
		if err != nil {
			answer <- err.Error()
			return
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		answer <- string(body)
	}()
	<-inFlight
	stopped := make(chan error, 1)
	go func() { stopped <- srv.stop() }()
	time.Sleep(100 * time.Millisecond)
	close(finish)
	if got := <-answer; got != "done" {
		t.Fatalf("request in flight: %q", got)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	for _, addr := range []string{srv.main, srv.plain} {
		if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			conn.Close()
			t.Errorf("%s still accepts connections", addr)
		}
	}
}

// With HTTPS off, the default, nothing of it is set up and server.http_addr is not used.
func TestHTTPSOffSetsUpNothing(t *testing.T) {
	tlsConfig, plain, ready, err := New(config.Default(), Hooks{}).https()
	if tlsConfig != nil || plain != nil || err != nil {
		t.Fatalf("%v %v %v", tlsConfig, plain, err)
	}
	ready()
}

// Run serves HTTPS on server.addr and the redirect on server.http_addr.
func TestRunServesBothAddresses(t *testing.T) {
	cfg, _, _, certPEM, _ := certConfig(t)
	cfg.Server.Addr, cfg.Server.HTTPAddr = freeAddr(t), freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := New(cfg, Hooks{})
	s.Handle("GET", "/hello", hello)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	client := h2Client(pool(t, certPEM))
	var res *http.Response
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var err error
		if res, err = client.Get("https://" + cfg.Server.Addr + "/hello"); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatal(err)
		}
	}
	res.Body.Close()
	if res.ProtoMajor != 2 {
		t.Fatalf("over %s", res.Proto)
	}
	plain := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := plain.Get("http://" + cfg.Server.HTTPAddr + "/hello")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("plain HTTP: %d", res.StatusCode)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// server.http_addr off leaves only server.addr.
func TestHTTPAddrOff(t *testing.T) {
	cfg, _, _, _, _ := certConfig(t)
	cfg.Server.Addr, cfg.Server.HTTPAddr = freeAddr(t), "off"
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- New(cfg, Hooks{OnStart: func() error { close(started); return nil }}).Run(ctx)
	}()
	<-started
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A busy server.http_addr fails the start and names the setting.
func TestBusyHTTPAddrNamesTheSetting(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	cfg, _, _, _, _ := certConfig(t)
	cfg.Server.Addr, cfg.Server.HTTPAddr = freeAddr(t), busy.Addr().String()
	err = New(cfg, Hooks{}).Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "server.http_addr") {
		t.Fatalf("got %v", err)
	}
}

// tifa dev serves plain HTTP whatever the HTTPS settings say: its proxy talks to the app over
// plain HTTP, and certificates of the deployment do not belong on a laptop.
func TestDevServesPlainHTTP(t *testing.T) {
	app := App{ConfigName: "tifa.yaml", Config: []byte(fmt.Sprintf(
		"server:\n  tls_cert: %s\n  tls_key: %s\n  hsts: true\n", filepath.Join(t.TempDir(), "missing.pem"), filepath.Join(t.TempDir(), "missing.key")))}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.Hooks.OnStart = func() error { cancel(); return nil }
	if err := run(ctx, app, []string{"--server-addr", "127.0.0.1:0"}, nil, io.Discard, io.Discard, reloader); err != nil {
		t.Fatalf("dev: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	if err := run(ctx, app, []string{"--server-addr", "127.0.0.1:0"}, nil, io.Discard, io.Discard, nil); err == nil || !strings.Contains(err.Error(), "missing.pem") {
		t.Fatalf("a built program uses the settings: %v", err)
	}
}

// A port below 1024 that the program may not bind fails with what to do on Linux.
func TestListenErrorsNameTheSettingAndTheFix(t *testing.T) {
	denied := &net.OpError{Op: "listen", Net: "tcp", Err: os.NewSyscallError("bind", syscall.EACCES)}
	err := listenError("server.addr", denied)
	for _, want := range []string{"server.addr", "permission denied", "cap_net_bind_service"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%v lacks %q", err, want)
		}
	}
	if !errors.Is(err, syscall.EACCES) {
		t.Error("the error no longer wraps EACCES")
	}
	busy := &net.OpError{Op: "listen", Net: "tcp", Err: os.NewSyscallError("bind", syscall.EADDRINUSE)}
	if err := listenError("server.http_addr", busy); strings.Contains(err.Error(), "cap_net_bind_service") || !strings.Contains(err.Error(), "server.http_addr") {
		t.Errorf("busy: %v", err)
	}
}
