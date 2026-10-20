package server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// https sets up HTTPS from the settings: the TLS config of server.addr, the handler of
// server.http_addr, and ready, which Run calls once both listen. tlsConfig and plain are nil
// when HTTPS is off. Settings that do not go together and certificate files that do not load
// fail here, before OnStart.
func (s *Server) https() (tlsConfig *tls.Config, plain http.Handler, ready func(), err error) {
	c := s.cfg
	if !c.HTTPS() {
		return nil, nil, func() {}, nil
	}
	if err := c.CheckHTTPS(); err != nil {
		return nil, nil, nil, err
	}
	files, err := loadCertFiles(c.TLSCert, c.TLSKey)
	if err != nil {
		return nil, nil, nil, err
	}
	// net/http adds h2 and http/1.1 to NextProtos, so browsers get HTTP/2.
	return &tls.Config{GetCertificate: files.get}, http.HandlerFunc(redirectToHTTPS), func() {}, nil
}

// redirectToHTTPS answers the requests of server.http_addr with a permanent redirect to the
// same URL over HTTPS, keeping the method (308), as Caddy does. The URL has no port: browsers
// reach HTTPS on 443, also when server.addr listens on another port that 443 is mapped to.
func redirectToHTTPS(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if host == "" {
		http.Error(w, "missing Host header", http.StatusBadRequest)
		return
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	w.Header().Set("Connection", "close")
	http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusPermanentRedirect)
}

// certFiles serves the certificate of server.tls_cert and server.tls_key. Before each TLS
// handshake it looks at both files and loads them again when either changed (another file,
// time or size), so a renewed certificate takes effect at the next connection without a
// restart. Links are followed, as certbot's live folder and Kubernetes secrets use them. Files
// that do not load, such as a certificate written before its key, leave the certificate that
// loaded before, and the error is logged once for those files.
type certFiles struct {
	certPath, keyPath string

	mu                sync.Mutex
	cert              *tls.Certificate
	certInfo, keyInfo os.FileInfo // of the files cert came from
	failCert, failKey os.FileInfo // of the files that failed to load last
	statErr           string      // the last error looking at the files, logged once
}

func loadCertFiles(certPath, keyPath string) (*certFiles, error) {
	f := &certFiles{certPath: certPath, keyPath: keyPath}
	ci, ki, err := f.stat()
	if err == nil {
		err = f.load(ci, ki)
	}
	if err != nil {
		return nil, fmt.Errorf("tifa: server.tls_cert and server.tls_key: %w", err)
	}
	return f, nil
}

func (f *certFiles) stat() (cert, key os.FileInfo, err error) {
	if cert, err = os.Stat(f.certPath); err != nil {
		return nil, nil, err
	}
	if key, err = os.Stat(f.keyPath); err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

// load reads both files, which ci and ki describe, and serves their certificate from now on.
func (f *certFiles) load(ci, ki os.FileInfo) error {
	cert, err := tls.LoadX509KeyPair(f.certPath, f.keyPath)
	if err != nil {
		return err
	}
	if cert.Leaf == nil { // GODEBUG x509keypairleaf=0
		if cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0]); err != nil {
			return err
		}
	}
	f.cert, f.certInfo, f.keyInfo = &cert, ci, ki
	log.Printf("tifa: certificate %s for %s, valid until %s", f.certPath, certNames(cert.Leaf), cert.Leaf.NotAfter.UTC().Format(time.DateOnly))
	return nil
}

func (f *certFiles) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ci, ki, err := f.stat()
	switch {
	case err != nil:
		if err.Error() != f.statErr {
			f.statErr = err.Error()
			log.Printf("tifa: keeping the certificate loaded before: %v", err)
		}
	case sameFile(ci, f.certInfo) && sameFile(ki, f.keyInfo):
	case sameFile(ci, f.failCert) && sameFile(ki, f.failKey):
	default:
		f.statErr = ""
		if err := f.load(ci, ki); err != nil {
			f.failCert, f.failKey = ci, ki
			log.Printf("tifa: keeping the certificate loaded before, %s and %s do not load: %v", f.certPath, f.keyPath, err)
		}
	}
	return f.cert, nil
}

// sameFile reports whether a and b describe the same file with the same time and size.
func sameFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.ModTime().Equal(b.ModTime()) && a.Size() == b.Size()
}

func certNames(leaf *x509.Certificate) string {
	if len(leaf.DNSNames) > 0 {
		return strings.Join(leaf.DNSNames, ", ")
	}
	return leaf.Subject.CommonName
}
