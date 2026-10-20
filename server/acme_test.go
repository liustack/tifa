package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/config"
)

// fakeACME is an ACME server (RFC 8555) in the test process, standing in for Let's Encrypt. It
// does not check signatures or nonces, but it does what the real one does with the challenges:
// it connects to the app and checks the key authorization, over plain HTTP (http-01, RFC 8555
// 8.3) or over TLS with the acme-tls/1 protocol (tls-alpn-01, RFC 8737).
type fakeACME struct {
	t         *testing.T
	srv       *httptest.Server
	challenge string // the one challenge type offered
	reach     string // the address the domain resolves to for the challenge
	rootKey   *ecdsa.PrivateKey
	root      *x509.Certificate

	mu         sync.Mutex
	thumbprint string // of the account key, RFC 7638
	orders     []*fakeOrder
	authzs     []*fakeAuthz
}

type fakeOrder struct {
	status  string
	domains []string
	authzs  []int
	leaf    []byte
}

type fakeAuthz struct {
	domain, status, token string
}

func newFakeACME(t *testing.T, challenge string) *fakeACME {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fake ACME root"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(365 * 24 * time.Hour),
		KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := x509.ParseCertificate(der)
	ca := &fakeACME{t: t, challenge: challenge, rootKey: key, root: root}
	ca.srv = httptest.NewTLSServer(http.HandlerFunc(ca.handle))
	t.Cleanup(ca.srv.Close)
	return ca
}

func (ca *fakeACME) url(format string, args ...any) string {
	return ca.srv.URL + fmt.Sprintf(format, args...)
}

// roots trusts the certificates the fake server gives out.
func (ca *fakeACME) roots() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(ca.root)
	return p
}

func (ca *fakeACME) orderCount() int {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	return len(ca.orders)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// request reads the protected header and the payload of a JWS request body.
func request(r *http.Request) (header struct{ JWK json.RawMessage }, payload []byte, err error) {
	var jws struct{ Protected, Payload string }
	if err = json.NewDecoder(r.Body).Decode(&jws); err != nil {
		return header, nil, err
	}
	h, err := base64.RawURLEncoding.DecodeString(jws.Protected)
	if err != nil {
		return header, nil, err
	}
	if err = json.Unmarshal(h, &header); err != nil {
		return header, nil, err
	}
	payload, err = base64.RawURLEncoding.DecodeString(jws.Payload)
	return header, payload, err
}

func (ca *fakeACME) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Replay-Nonce", "nonce-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	if r.URL.Path == "/directory" {
		writeJSON(w, 200, map[string]any{
			"newNonce": ca.url("/nonce"), "newAccount": ca.url("/account"), "newOrder": ca.url("/order"),
			"meta": map[string]any{"termsOfService": ca.url("/terms")},
		})
		return
	}
	if r.URL.Path == "/nonce" {
		return
	}
	header, payload, err := request(r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ca.mu.Lock()
	defer ca.mu.Unlock()
	kind, id, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	n, _ := strconv.Atoi(id)
	switch kind {
	case "account":
		var jwk struct{ Crv, Kty, X, Y string }
		if err := json.Unmarshal(header.JWK, &jwk); err != nil || jwk.Kty != "EC" {
			http.Error(w, fmt.Sprintf("account key: %s %v", header.JWK, err), 400)
			return
		}
		sum := sha256.Sum256(fmt.Appendf(nil, `{"crv":%q,"kty":%q,"x":%q,"y":%q}`, jwk.Crv, jwk.Kty, jwk.X, jwk.Y))
		ca.thumbprint = b64(sum[:])
		w.Header().Set("Location", ca.url("/accounts/1"))
		writeJSON(w, 201, map[string]any{"status": "valid"})
	case "order":
		if id != "" {
			writeJSON(w, 200, ca.orderJSON(n))
			return
		}
		var req struct{ Identifiers []struct{ Value string } }
		if err := json.Unmarshal(payload, &req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		o := &fakeOrder{status: "pending"}
		for _, ident := range req.Identifiers {
			o.domains = append(o.domains, ident.Value)
			o.authzs = append(o.authzs, len(ca.authzs))
			ca.authzs = append(ca.authzs, &fakeAuthz{domain: ident.Value, status: "pending", token: b64([]byte(fmt.Sprintf("token-%d", len(ca.authzs))))})
		}
		ca.orders = append(ca.orders, o)
		w.Header().Set("Location", ca.url("/order/%d", len(ca.orders)-1))
		writeJSON(w, 201, ca.orderJSON(len(ca.orders)-1))
	case "authz":
		var req struct{ Status string }
		json.Unmarshal(payload, &req)
		if req.Status == "deactivated" {
			ca.authzs[n].status = "deactivated"
		}
		writeJSON(w, 200, ca.authzJSON(n))
	case "challenge":
		a := ca.authzs[n]
		if err := ca.validate(a); err != nil {
			ca.t.Logf("%s for %s fails: %v", ca.challenge, a.domain, err)
			a.status = "invalid"
		} else {
			a.status = "valid"
		}
		ca.update()
		writeJSON(w, 200, ca.authzJSON(n)["challenges"].([]any)[0])
	case "finalize":
		o := ca.orders[n]
		var req struct{ CSR string }
		json.Unmarshal(payload, &req)
		der, _ := base64.RawURLEncoding.DecodeString(req.CSR)
		csr, err := x509.ParseCertificateRequest(der)
		if o.status != "ready" || err != nil {
			http.Error(w, fmt.Sprintf("order %s, CSR %v", o.status, err), 403)
			return
		}
		serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
		leaf := &x509.Certificate{
			SerialNumber: serial, Subject: pkix.Name{CommonName: csr.DNSNames[0]}, DNSNames: csr.DNSNames,
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour),
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		if o.leaf, err = x509.CreateCertificate(rand.Reader, leaf, ca.root, csr.PublicKey, ca.rootKey); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		o.status = "valid"
		writeJSON(w, 200, ca.orderJSON(n))
	case "cert":
		w.Header().Set("Content-Type", "application/pem-certificate-chain")
		pem.Encode(w, &pem.Block{Type: "CERTIFICATE", Bytes: ca.orders[n].leaf})
		pem.Encode(w, &pem.Block{Type: "CERTIFICATE", Bytes: ca.root.Raw})
	default:
		http.Error(w, "unknown path "+r.URL.Path, 404)
	}
}

func (ca *fakeACME) orderJSON(n int) map[string]any {
	o := ca.orders[n]
	var ids []any
	var authzs []string
	for i, d := range o.domains {
		ids = append(ids, map[string]string{"type": "dns", "value": d})
		authzs = append(authzs, ca.url("/authz/%d", o.authzs[i]))
	}
	m := map[string]any{"status": o.status, "identifiers": ids, "authorizations": authzs, "finalize": ca.url("/finalize/%d", n)}
	if o.status == "valid" {
		m["certificate"] = ca.url("/cert/%d", n)
	}
	return m
}

func (ca *fakeACME) authzJSON(n int) map[string]any {
	a := ca.authzs[n]
	chal := map[string]any{"type": ca.challenge, "url": ca.url("/challenge/%d", n), "token": a.token, "status": a.status}
	if a.status == "deactivated" {
		chal["status"] = "invalid"
	}
	return map[string]any{"status": a.status, "identifier": map[string]string{"type": "dns", "value": a.domain}, "challenges": []any{chal}}
}

// update makes an order ready once all its authorizations are valid, invalid once one is not.
func (ca *fakeACME) update() {
	for _, o := range ca.orders {
		if o.status != "pending" {
			continue
		}
		valid := 0
		for _, i := range o.authzs {
			switch ca.authzs[i].status {
			case "valid":
				valid++
			case "invalid", "deactivated":
				o.status = "invalid"
			}
		}
		if valid == len(o.authzs) {
			o.status = "ready"
		}
	}
}

// validate connects to the app the way Let's Encrypt does and checks the key authorization.
func (ca *fakeACME) validate(a *fakeAuthz) error {
	keyAuth := a.token + "." + ca.thumbprint
	switch ca.challenge {
	case "http-01":
		client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, ca.reach)
			},
		}}
		// With the port in the URL, and so in Host, as Pebble, the test server of Let's Encrypt,
		// sends it.
		res, err := client.Get("http://" + a.domain + ":80/.well-known/acme-challenge/" + a.token)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 || string(body) != keyAuth {
			return fmt.Errorf("%d %q, want %q", res.StatusCode, body, keyAuth)
		}
		return nil
	case "tls-alpn-01":
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", ca.reach, &tls.Config{
			ServerName: a.domain, NextProtos: []string{"acme-tls/1"}, InsecureSkipVerify: true,
		})
		if err != nil {
			return err
		}
		defer conn.Close()
		state := conn.ConnectionState()
		if state.NegotiatedProtocol != "acme-tls/1" {
			return fmt.Errorf("protocol %q", state.NegotiatedProtocol)
		}
		sum := sha256.Sum256([]byte(keyAuth))
		want, _ := asn1.Marshal(sum[:])
		for _, ext := range state.PeerCertificates[0].Extensions {
			if ext.Id.Equal(asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 31}) && string(ext.Value) == string(want) {
				return nil
			}
		}
		return fmt.Errorf("no acmeIdentifier with the key authorization")
	}
	return fmt.Errorf("unknown challenge %s", ca.challenge)
}

// autoConfig asks the fake server for the certificate of testDomain, kept in cache.
func autoConfig(ca *fakeACME, cache string) config.Config {
	cfg := config.Default()
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Server.TLSDomains = []string{testDomain}
	cfg.Server.TLSCache = cache
	cfg.Server.TLSCA = ca.url("/directory")
	cfg.Server.TLSEmail = "ops@example.test"
	return cfg
}

// trustACME makes the program trust the fake server's own HTTPS for the test.
func trustACME(t *testing.T, ca *fakeACME) {
	testACMEClient = ca.srv.Client()
	t.Cleanup(func() { testACMEClient = nil })
}

// With server.tls_domains the program gets its certificate from the ACME server, through the
// challenge on server.http_addr (http-01) or on server.addr itself (tls-alpn-01), and serves
// HTTP/2 with it. After a restart it takes the certificate from server.tls_cache and asks for
// none: a new one every start would soon hit the limits of Let's Encrypt.
func TestAutomaticCertificate(t *testing.T) {
	for _, challenge := range []string{"http-01", "tls-alpn-01"} {
		t.Run(challenge, func(t *testing.T) {
			ca := newFakeACME(t, challenge)
			trustACME(t, ca)
			cache := filepath.Join(t.TempDir(), "certs")
			cfg := autoConfig(ca, cache)

			s := New(cfg, Hooks{})
			s.Handle("GET", "/hello", hello)
			tlsConfig, plain, ready, err := s.https()
			if err != nil {
				t.Fatal(err)
			}
			ln, plainLn := listenLoopback(t), listenLoopback(t)
			ca.reach = plainLn.Addr().String()
			if challenge == "tls-alpn-01" {
				ca.reach = ln.Addr().String()
			}
			ctx, cancel := context.WithCancel(context.Background())
			served := make(chan error, 1)
			go func() { served <- s.serveBoth(ctx, ln, tlsConfig, plainLn, plain) }()
			ready()
			res, body, first := getTLS(t, h2Client(ca.roots()), "https://"+ln.Addr().String()+"/hello")
			if res.ProtoMajor != 2 || body != "hello over HTTP/2.0" {
				t.Fatalf("%s %q", res.Proto, body)
			}
			if n := ca.orderCount(); n != 1 {
				t.Fatalf("%d orders, want 1", n)
			}
			cancel()
			if err := <-served; err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(cache, testDomain)); err != nil {
				t.Fatalf("certificate not in the cache: %v", err)
			}

			s = New(cfg, Hooks{})
			s.Handle("GET", "/hello", hello)
			tlsConfig, plain, ready, err = s.https()
			if err != nil {
				t.Fatal(err)
			}
			ln, plainLn = listenLoopback(t), listenLoopback(t)
			ctx, cancel = context.WithCancel(context.Background())
			defer cancel()
			go func() { served <- s.serveBoth(ctx, ln, tlsConfig, plainLn, plain) }()
			ready()
			_, _, again := getTLS(t, h2Client(ca.roots()), "https://"+ln.Addr().String()+"/hello")
			if again.Cmp(first) != 0 || ca.orderCount() != 1 {
				t.Fatalf("after a restart: serial %v (was %v), %d orders", again, first, ca.orderCount())
			}
		})
	}
}

// A name not in server.tls_domains gets no certificate and the ACME server is not asked, so a
// client that sends any name cannot use up the limits of Let's Encrypt.
func TestOtherNamesGetNoCertificate(t *testing.T) {
	ca := newFakeACME(t, "tls-alpn-01")
	trustACME(t, ca)
	s := New(autoConfig(ca, t.TempDir()), Hooks{})
	tlsConfig, plain, _, err := s.https()
	if err != nil {
		t.Fatal(err)
	}
	ln := listenLoopback(t)
	ca.reach = ln.Addr().String()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.serveBoth(ctx, ln, tlsConfig, nil, plain)
	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{ServerName: "other.example.test", RootCAs: ca.roots()})
	if err == nil {
		conn.Close()
		t.Fatal("handshake for another name succeeded")
	}
	if n := ca.orderCount(); n != 0 {
		t.Fatalf("%d orders", n)
	}
}

// ready asks for the certificates once both addresses listen, so the first browser does not
// wait for them and a mistake shows in the log at start.
func TestCertificatesAreAskedForAtStart(t *testing.T) {
	ca := newFakeACME(t, "http-01")
	trustACME(t, ca)
	s := New(autoConfig(ca, t.TempDir()), Hooks{})
	tlsConfig, plain, ready, err := s.https()
	if err != nil {
		t.Fatal(err)
	}
	ln, plainLn := listenLoopback(t), listenLoopback(t)
	ca.reach = plainLn.Addr().String()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.serveBoth(ctx, ln, tlsConfig, plainLn, plain)
	ready()
	for deadline := time.Now().Add(10 * time.Second); ca.orderCount() == 0 || !orderValid(ca); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("no certificate asked for")
		}
	}
}

func orderValid(ca *fakeACME) bool {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	return ca.orders[0].status == "valid"
}

func listenLoopback(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

// server.tls_cache is created when missing, and a cache that cannot be created stops the program
// before OnStart.
func TestTLSCacheThatCannotBeCreatedFails(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	writeFile(t, file, nil)
	cfg := config.Default()
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Server.TLSDomains, cfg.Server.TLSCache = []string{testDomain}, filepath.Join(file, "certs")
	_, _, _, err := New(cfg, Hooks{}).https()
	if err == nil || !strings.Contains(err.Error(), "server.tls_cache") {
		t.Fatalf("got %v", err)
	}
}

// With server.tls_domains, plain HTTP redirects only the names of tls_domains, and answers
// others with 421, while the challenges of Let's Encrypt still get through.
func TestAutomaticCertificatesRedirectOnlyTheirDomains(t *testing.T) {
	ca := newFakeACME(t, "http-01")
	trustACME(t, ca)
	s := New(autoConfig(ca, t.TempDir()), Hooks{})
	tlsConfig, plain, _, err := s.https()
	if err != nil {
		t.Fatal(err)
	}
	ln, plainLn := listenLoopback(t), listenLoopback(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.serveBoth(ctx, ln, tlsConfig, plainLn, plain)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for host, want := range map[string]int{testDomain: 308, testDomain + ":80": 308, "other.example.test": 421, "127.0.0.1": 421} {
		req, _ := http.NewRequest("GET", "http://"+plainLn.Addr().String()+"/x", nil)
		req.Host = host
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != want {
			t.Errorf("%s: %d, want %d", host, res.StatusCode, want)
		}
	}
}
