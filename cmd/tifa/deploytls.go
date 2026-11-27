package main

// Your own certificate for an app that serves HTTPS itself (deploy.front none with a domain), such
// as a Cloudflare Origin CA certificate: setup uploads the files that TIFA_DEPLOY_TLS_CERT and
// TIFA_DEPLOY_TLS_KEY name on this machine into shared/tls of the app, and the service gets them
// in place of Let's Encrypt. Only setup uploads them: a deploy, from CI too, never needs them.

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/liustack/tifa/internal/codegen"
	"github.com/liustack/tifa/internal/config"
)

const (
	// ownCertEnv and ownKeyEnv name the PEM files of the certificate and its key on this machine.
	ownCertEnv = "TIFA_DEPLOY_TLS_CERT"
	ownKeyEnv  = "TIFA_DEPLOY_TLS_KEY"
	// tlsDir holds them on the server, in the folder of the app.
	tlsDir = "shared/tls"
)

// ownCert is the certificate setup uploads, read and checked on this machine.
type ownCert struct {
	cert, key []byte
	leaf      *x509.Certificate
}

// loadOwnCert reads the certificate and key that the inputs name for the app of t, relative to the
// root of the app, and checks that they can serve HTTPS for its domain now. Nil when neither input
// is set. Errors name the files and what is wrong, never a byte of the key.
func loadOwnCert(t deployTarget) (*ownCert, error) {
	certPath, certFrom, err := readDeployInput(t.root, "", "", ownCertEnv)
	if err != nil {
		return nil, err
	}
	keyPath, keyFrom, err := readDeployInput(t.root, "", "", ownKeyEnv)
	if err != nil {
		return nil, err
	}
	switch {
	case certPath == "" && keyPath == "":
		return nil, nil
	case keyPath == "":
		return nil, fmt.Errorf("%s is set and %s is not, in %s: set both, the files of your certificate and of its key, or neither for a certificate from Let's Encrypt", ownCertEnv, ownKeyEnv, certFrom)
	case certPath == "":
		return nil, fmt.Errorf("%s is set and %s is not, in %s: set both, the files of your certificate and of its key, or neither for a certificate from Let's Encrypt", ownKeyEnv, ownCertEnv, keyFrom)
	case t.front != "none":
		return nil, fmt.Errorf("%s and %s are for deploy.front none, where the app serves HTTPS itself, and deploy.front is %s: remove them from %s", ownCertEnv, ownKeyEnv, t.front, certFrom)
	case t.own == "":
		return nil, fmt.Errorf("%s and %s give a certificate for the domain of the app, and site.url names no domain, so the app serves plain HTTP on port 80: set site.url, such as https://shop.example.com, or remove them from %s", ownCertEnv, ownKeyEnv, certFrom)
	}
	abs := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(t.root, p)
	}
	certPath, keyPath = abs(certPath), abs(keyPath)
	c := &ownCert{}
	if c.cert, err = os.ReadFile(certPath); err != nil {
		return nil, fmt.Errorf("%s: %w", ownCertEnv, err)
	}
	if c.key, err = os.ReadFile(keyPath); err != nil {
		return nil, fmt.Errorf("%s: %w", ownKeyEnv, err)
	}
	if c.leaf = firstCertificate(c.cert); c.leaf == nil {
		return nil, fmt.Errorf("%s holds no certificate in PEM (a block BEGIN CERTIFICATE), which %s names", certPath, ownCertEnv)
	}
	if _, err := tls.X509KeyPair(c.cert, c.key); err != nil {
		if strings.Contains(err.Error(), "does not match") {
			return nil, fmt.Errorf("the key does not belong to the certificate: %s (%s) is the key of another certificate than %s (%s)", keyPath, ownKeyEnv, certPath, ownCertEnv)
		}
		return nil, fmt.Errorf("%s holds no private key in PEM that the app can read, which %s names: %v", keyPath, ownKeyEnv, err)
	}
	now := time.Now()
	switch {
	case c.leaf.VerifyHostname(t.own) != nil:
		return nil, fmt.Errorf("the certificate in %s is for %s, not %s, the host of site.url", certPath, certNames(c.leaf), t.own)
	case now.After(c.leaf.NotAfter):
		return nil, fmt.Errorf("the certificate in %s expired on %s: get a new one", certPath, c.leaf.NotAfter.UTC().Format(time.DateOnly))
	case now.Before(c.leaf.NotBefore):
		return nil, fmt.Errorf("the certificate in %s is valid only from %s", certPath, c.leaf.NotBefore.UTC().Format(time.DateOnly))
	}
	return c, nil
}

// firstCertificate is the first certificate of the PEM data, the one the app serves, nil when it
// holds none that parses.
func firstCertificate(data []byte) *x509.Certificate {
	for {
		var b *pem.Block
		if b, data = pem.Decode(data); b == nil {
			return nil
		}
		if b.Type == "CERTIFICATE" {
			c, err := x509.ParseCertificate(b.Bytes)
			if err != nil {
				return nil
			}
			return c
		}
	}
}

// certNames are the names a certificate covers, for a message.
func certNames(c *x509.Certificate) string {
	if len(c.DNSNames) > 0 {
		return strings.Join(c.DNSNames, ", ")
	}
	return c.Subject.CommonName
}

// certText says what a certificate is for and how long, for the plan.
func certText(c *x509.Certificate) string {
	return certNames(c) + ", valid until " + c.NotAfter.UTC().Format(time.DateOnly)
}

// sayOwnCertWins says, when tifa.yaml sets the files of a certificate too, that the service of
// the server sets its own: the environment comes before tifa.yaml.
func sayOwnCertWins(t deployTarget, w interface{ Write([]byte) (int, error) }) {
	settings, err := codegen.ProjectSettings(t.root)
	if err != nil || (settings.Server.TLSCert == "" && settings.Server.TLSKey == "") {
		return
	}
	fmt.Fprintf(w, "tifa.yaml sets server.tls_cert and server.tls_key, and on %s the service sets SERVER_TLS_CERT and SERVER_TLS_KEY to the files setup uploaded, which win.\n", t.host)
}

// sayServerAddrUnused says that server.addr of tifa.yaml is not used on the server when it is not
// the default and the front picks the port: the systemd socket sets where the app listens.
func sayServerAddrUnused(t deployTarget, w interface{ Write([]byte) (int, error) }) {
	if t.front == "external" {
		return
	}
	settings, err := codegen.ProjectSettings(t.root)
	if err != nil || settings.Server.Addr == config.DefaultAddr {
		return
	}
	fmt.Fprintf(w, "server.addr is %s in tifa.yaml, and with deploy.front %s the server is not told to use it: setup picks the port and the systemd socket of the app decides where it listens.\n", settings.Server.Addr, t.front)
}

// tlsUploadBody runs as the deploy user with the file on its input. It writes $name in the folder
// of the certificate, which only the deploy user may read, through a file next to it that it then
// renames, so the app never reads half a file.
const tlsUploadBody = `set -eu
# tifa: tls upload
mkdir -p "$HOME/$app/shared"
umask 077
d="$HOME/$app/` + tlsDir + `"
mkdir -p "$d"
chmod 700 "$d"
cat > "$d/.$name.new"
chmod 600 "$d/.$name.new"
mv -f "$d/.$name.new" "$d/$name"
echo "wrote $name"
`

// tlsReadBody runs as the deploy user and prints the certificate setup uploaded, which is public.
const tlsReadBody = `# tifa: tls read
cat "$HOME/$app/` + tlsDir + `/cert.pem" 2>/dev/null
true
`

// tlsDeleteBody runs as the deploy user and deletes the certificate and key setup uploaded.
const tlsDeleteBody = `set -eu
# tifa: tls delete
rm -rf "$HOME/$app/` + tlsDir + `"
echo "deleted $HOME/$app/` + tlsDir + `"
`

// ownCertSteps plans the upload of the certificate of the app, before the app step: the app may
// restart to use it.
func (r *setupRun) ownCertSteps(ctx context.Context, p *setupPlan) {
	c, app := r.cert, r.t.app
	where := fmt.Sprintf("%s/%s/%s", deployHome, app, tlsDir)
	if c == nil {
		return
	}
	if sha(string(c.cert)) == r.st.TLSCertSum && sha(string(c.key)) == r.st.TLSKeySum {
		p.ok(fmt.Sprintf("tls: your certificate in %s", where))
		return
	}
	upload := func(ctx context.Context) (string, error) {
		for _, f := range []struct {
			name string
			data []byte
		}{{"cert.pem", c.cert}, {"key.pem", c.key}} {
			var out strings.Builder
			if err := r.t.ssh(ctx, deployUser, script(tlsUploadBody, "app", app, "name", f.name), bytes.NewReader(f.data), &out, r.stderr); err != nil {
				return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(out.String()))
			}
		}
		return "wrote cert.pem and key.pem", nil
	}
	if r.st.TLSCertSum == "" {
		p.add(opAdd, fmt.Sprintf("tls: your certificate for %s, in %s", certText(c.leaf), where), upload)
		return
	}
	old := "a certificate tifa cannot read"
	var out strings.Builder
	if err := r.t.ssh(ctx, deployUser, script(tlsReadBody, "app", app), nil, &out, r.stderr); err == nil {
		if leaf := firstCertificate([]byte(out.String())); leaf != nil {
			old = certText(leaf)
		}
	}
	p.add(opChange, fmt.Sprintf("tls: replace the certificate in %s (%s) with yours (%s). The app loads it without a restart", where, old, certText(c.leaf)), upload)
}

// ownCertRemoval plans, after the app step, the deletion of the certificate setup uploaded once
// the app no longer uses it.
func (r *setupRun) ownCertRemoval(p *setupPlan) {
	if r.cert != nil || r.st.TLSCertSum == "" {
		return
	}
	app := r.t.app
	p.add(opRemove, fmt.Sprintf("tls: %s/%s/%s, your certificate that setup uploaded", deployHome, app, tlsDir), func(ctx context.Context) (string, error) {
		var out strings.Builder
		if err := r.t.ssh(ctx, deployUser, script(tlsDeleteBody, "app", app), nil, &out, r.stderr); err != nil {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(out.String()))
		}
		return strings.TrimSpace(out.String()), nil
	})
}
