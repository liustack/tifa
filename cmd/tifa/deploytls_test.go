package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testCert writes a certificate for names that is valid until notAfter, and its key, into dir as
// name.pem and name.key, and returns the two PEM texts.
func testCert(t *testing.T, dir, name string, notAfter time.Time, names ...string) (cert, key string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		NotBefore:    notAfter.AddDate(-1, 0, 0),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cert = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	key = string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	writeFile(t, filepath.Join(dir, name+".pem"), cert)
	writeFile(t, filepath.Join(dir, name+".key"), key)
	return cert, key
}

// ownCertApp is an app with front none for shop.example.com whose .env names the files of its own
// certificate, valid for a year, on a prepared server without the app.
func ownCertApp(t *testing.T, yaml string) (root, cert, key string, srv *setupServer) {
	t.Helper()
	root = setupApp(t, yaml, nil)
	fakePorts(t)
	cert, key = testCert(t, root, "origin", time.Now().AddDate(1, 0, 0), "shop.example.com", "*.shop.example.com")
	writeFile(t, filepath.Join(root, ".env"), ownCertEnv+"=origin.pem\n"+ownKeyEnv+"=origin.key\n")
	srv = newSetupServer(t, "", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine vps-1234\nlowport 80\n"+hostKeysFact)
	return root, cert, key, srv
}

// factsOwnCert is the probe output of a prepared server where the app serves HTTPS for domain
// itself with the certificate cert and key that setup uploaded.
func factsOwnCert(app, domain, cert, key, extra string) string {
	u := unitsOf(app, httpsPort, domain, false, true)
	return "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nport 443\nhttps " + domain + "\nunit-socket " + sha(u.socket) +
		"\nunit-http " + sha(u.http) + "\nunit-service " + sha(u.service) + "\nmachine vps-1234\napt yes\ntls-cert " + sha(cert) + "\ntls-key " + sha(key) + "\n" + hostKeysFact + extra
}

// With TIFA_DEPLOY_TLS_CERT and TIFA_DEPLOY_TLS_KEY, setup uploads the files into shared/tls of the
// app, over ssh and never on a command line, and the service gets them in its environment in place
// of Let's Encrypt.
func TestSetupUploadsOwnCertificate(t *testing.T) {
	root, cert, key, srv := ownCertApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n")
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	until := time.Now().AddDate(1, 0, 0).UTC().Format(time.DateOnly)
	for _, want := range []string{
		"add     tls: your certificate for shop.example.com, *.shop.example.com, valid until " + until + ", in /srv/tifa/shop/shared/tls",
		"add     app: /srv/tifa/shop with an empty shared/.env, serving visitors itself on ports 80 and 443, with HTTPS for shop.example.com and your certificate in /srv/tifa/shop/shared/tls",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(srv.calls(), "tlsupload cert.pem tifa\ntlsupload key.pem tifa\napp tifa") {
		t.Errorf("calls:\n%s", srv.calls())
	}
	for name, want := range map[string]string{"cert.pem": cert, "key.pem": key} {
		if got, _ := os.ReadFile(filepath.Join(srv.dir, "tls-"+name)); string(got) != want {
			t.Errorf("%s on the server: %q", name, got)
		}
	}
	argv := srv.argv()
	if strings.Contains(argv, "PRIVATE KEY") || strings.Contains(out, "PRIVATE KEY") {
		t.Errorf("the key went on a command line or into the output:\n%s", out)
	}
	if !strings.Contains(argv, "Environment=SERVER_TLS_CERT=%h/shop/shared/tls/cert.pem SERVER_TLS_KEY=%h/shop/shared/tls/key.pem\n") || strings.Contains(argv, "SERVER_TLS_DOMAINS") {
		t.Errorf("the units of the app:\n%s", argv)
	}
	for _, want := range []string{"umask 077", "chmod 700", "mv "} {
		if !strings.Contains(srv.unquoted(), want) {
			t.Errorf("the upload lacks %q", want)
		}
	}

	// The next run finds the same files and units: nothing to do.
	setFacts(t, srv, factsOwnCert("shop", "shop.example.com", cert, key, "lowport 80\nappdir yes\nusock [::]:443 tifa-shop.socket tifa-shop.service\nusock [::]:80 tifa-shop-http.socket tifa-shop.service\n"))
	out, err = runSetup(t, root, false, "")
	if err != nil || !strings.Contains(out, "matches tifa.yaml") {
		t.Errorf("second run: %v\n%s", err, out)
	}
}

// Other files on this machine replace those on the server, in the plan with the names and dates
// of both, and the program loads them by itself: no restart.
func TestSetupReplacesOwnCertificate(t *testing.T) {
	root, _, _, srv := ownCertApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n")
	old := t.TempDir()
	oldCert, oldKey := testCert(t, old, "old", time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC), "shop.example.com")
	setFacts(t, srv, factsOwnCert("shop", "shop.example.com", oldCert, oldKey, "lowport 80\nappdir yes\nusock [::]:443 tifa-shop.socket tifa-shop.service\nusock [::]:80 tifa-shop-http.socket tifa-shop.service\n"))
	writeFile(t, filepath.Join(srv.dir, "tls-cert.pem"), oldCert)
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	until := time.Now().AddDate(1, 0, 0).UTC().Format(time.DateOnly)
	want := "change  tls: replace the certificate in /srv/tifa/shop/shared/tls (shop.example.com, valid until 2026-12-31) with yours (shop.example.com, *.shop.example.com, valid until " + until + "). The app loads it without a restart"
	if !strings.Contains(out, want) {
		t.Errorf("lacks %q:\n%s", want, out)
	}
	if strings.Contains(out, "app: ") && !strings.Contains(out, "Already right") {
		t.Errorf("the app changed:\n%s", out)
	}
	if c := srv.calls(); !strings.Contains(c, "tlsupload cert.pem tifa") || strings.Contains(c, "app tifa") {
		t.Errorf("calls:\n%s", c)
	}
}

// Without the inputs the app goes back to Let's Encrypt, and the files setup uploaded go.
func TestSetupBackToLetsEncrypt(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n", nil)
	fakePorts(t)
	cert, key := testCert(t, t.TempDir(), "old", time.Now().AddDate(1, 0, 0), "shop.example.com")
	srv := newSetupServer(t, "", factsOwnCert("shop", "shop.example.com", cert, key, "lowport 80\nappdir yes\nusock [::]:443 tifa-shop.socket tifa-shop.service\nusock [::]:80 tifa-shop-http.socket tifa-shop.service\n"))
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"change  app: serve HTTPS for shop.example.com with a certificate from Let's Encrypt kept in /srv/tifa/shop/certs, in place of yours. New service unit, and the app restarts",
		"remove  tls: /srv/tifa/shop/shared/tls, your certificate that setup uploaded",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if c := srv.calls(); !strings.Contains(c, "tlsdelete tifa") || strings.Index(c, "app tifa") > strings.Index(c, "tlsdelete tifa") {
		t.Errorf("calls:\n%s", c)
	}
}

// Going from Let's Encrypt to your own certificate uploads it first, then restarts the app with it.
func TestSetupFromLetsEncryptToOwnCertificate(t *testing.T) {
	root, _, _, srv := ownCertApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n")
	setFacts(t, srv, factsHTTPS("shop", "shop.example.com", "lowport 80\nappdir yes\nusock [::]:443 tifa-shop.socket tifa-shop.service\nusock [::]:80 tifa-shop-http.socket tifa-shop.service\n"))
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "change  app: serve HTTPS for shop.example.com with your certificate in /srv/tifa/shop/shared/tls, in place of one from Let's Encrypt. New service unit, and the app restarts") {
		t.Errorf("output:\n%s", out)
	}
	if c := srv.calls(); !strings.Contains(c, "tlsupload key.pem tifa\napp tifa") {
		t.Errorf("calls:\n%s", c)
	}
}

// Every check runs on this machine before setup reads the server: a mistake changes nothing.
func TestSetupOwnCertificateChecks(t *testing.T) {
	expired := t.TempDir()
	testCert(t, expired, "old", time.Now().AddDate(0, 0, -1), "shop.example.com")
	other := t.TempDir()
	testCert(t, other, "other", time.Now().AddDate(1, 0, 0), "blog.example.com")
	second := t.TempDir()
	testCert(t, second, "second", time.Now().AddDate(1, 0, 0), "shop.example.com")
	cases := []struct {
		yaml, env, want string
	}{
		{"site:\n  url: https://shop.example.com\ndeploy:\n", ownCertEnv + "=origin.pem\n", ownCertEnv + " is set and " + ownKeyEnv + " is not"},
		{"site:\n  url: https://shop.example.com\ndeploy:\n", ownKeyEnv + "=origin.key\n", ownKeyEnv + " is set and " + ownCertEnv + " is not"},
		{"site:\n  url: https://shop.example.com\ndeploy:\n  front: external\n", "", "deploy.front none"},
		{"deploy:\n", "", "site.url names no domain"},
		{"site:\n  url: https://shop.example.com\ndeploy:\n", ownCertEnv + "=missing.pem\n" + ownKeyEnv + "=origin.key\n", "missing.pem"},
		{"site:\n  url: https://shop.example.com\ndeploy:\n", ownCertEnv + "=origin.key\n" + ownKeyEnv + "=origin.key\n", "no certificate"},
		{"site:\n  url: https://shop.example.com\ndeploy:\n", ownCertEnv + "=origin.pem\n" + ownKeyEnv + "=" + filepath.Join(second, "second.key") + "\n", "the key does not belong to the certificate"},
		{"site:\n  url: https://shop.example.com\ndeploy:\n", ownCertEnv + "=" + filepath.Join(other, "other.pem") + "\n" + ownKeyEnv + "=" + filepath.Join(other, "other.key") + "\n", "is for blog.example.com, not shop.example.com"},
		{"site:\n  url: https://shop.example.com\ndeploy:\n", ownCertEnv + "=" + filepath.Join(expired, "old.pem") + "\n" + ownKeyEnv + "=" + filepath.Join(expired, "old.key") + "\n", "expired on"},
	}
	for _, c := range cases {
		root, _, _, srv := ownCertApp(t, c.yaml)
		if c.env != "" {
			writeFile(t, filepath.Join(root, ".env"), c.env)
		}
		out, err := runSetup(t, root, false, "", "--yes")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q, %q: got %v, want %q\n%s", c.yaml, c.env, err, c.want, out)
		}
		if strings.Contains(err.Error(), "PRIVATE KEY") {
			t.Errorf("the error shows the key: %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(srv.dir, "calls")); statErr == nil {
			t.Errorf("%q: setup went on:\n%s", c.env, srv.calls())
		}
	}
}

// server.tls_cert of tifa.yaml is for where the program runs. On this server the service
// environment sets it, and setup says so.
func TestSetupOwnCertificateOverridesTifaYAML(t *testing.T) {
	root, _, _, _ := ownCertApp(t, "site:\n  url: https://shop.example.com\nserver:\n  tls_cert: /etc/certs/shop.pem\n  tls_key: /etc/certs/shop.key\ndeploy:\n")
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "tifa.yaml sets server.tls_cert and server.tls_key, and on vps the service sets SERVER_TLS_CERT and SERVER_TLS_KEY to the files setup uploaded, which win") {
		t.Errorf("output:\n%s", out)
	}
}

// deploy run and the release command run the program as the service does, with the files of
// the certificate when setup uploaded them.
func TestDeployRunOwnCertificate(t *testing.T) {
	root, home, _ := runApp(t)
	writeFile(t, filepath.Join(home, "shop", "https"), "shop.example.com\n")
	writeFile(t, filepath.Join(home, "shop", "shared", "tls", "cert.pem"), "cert")
	writeFile(t, filepath.Join(home, "shop", "shared", "tls", "key.pem"), "key")
	out, _, err := runRun(t, false, "deploy", "run", "--dir", root, "x")
	tls := filepath.Join(home, "shop", "shared", "tls")
	want := "https <> <> <" + filepath.Join(tls, "cert.pem") + "> <" + filepath.Join(tls, "key.pem") + ">\n"
	if err != nil || !strings.Contains(out, want) {
		t.Errorf("%v\n%s\nwant %q", err, out, want)
	}
}
