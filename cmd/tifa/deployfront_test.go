package main

// With deploy.front none the app serves visitors itself: plain HTTP on port 80, or with a domain
// in site.url its own HTTPS on 443 and 80. These tests cover the units, the plan of setup, the stop
// when another program has the ports, the health check on the new sockets and what deploy says.

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/config"
)

// Port 80 alone for plain HTTP. With HTTPS, one socket for each address, named by its setting, and
// the settings of the built-in HTTPS in the environment of the service. Behind the tunnel the
// socket stays on the loopback, as before.
func TestDeployUnitsServeVisitors(t *testing.T) {
	has := func(name, unit string, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(unit, w) {
				t.Errorf("%s lacks %q:\n%s", name, w, unit)
			}
		}
	}
	plain := unitsOf("shop", httpPort, "", false)
	has("plain socket", plain.socket, "[Socket]\nListenStream=80\n\n[Install]")
	has("plain service", plain.service, "Requires=tifa-shop.socket\nAfter=tifa-shop.socket\n", "ExecStart=%h/shop/current/shop\n")
	if plain.http != "" || strings.Contains(plain.service, "Environment=") || strings.Contains(plain.socket, "127.0.0.1") {
		t.Errorf("plain HTTP: %+v", plain)
	}

	tls := unitsOf("shop", httpsPort, "shop.example.com", false)
	has("HTTPS socket", tls.socket, "ListenStream=443\nFileDescriptorName=server.addr\n")
	has("HTTP socket", tls.http, "ListenStream=80\nFileDescriptorName=server.http_addr\nService=tifa-shop.service\n", "WantedBy=sockets.target")
	has("HTTPS service", tls.service,
		"Requires=tifa-shop.socket tifa-shop-http.socket\nAfter=tifa-shop.socket tifa-shop-http.socket\n",
		"Sockets=tifa-shop.socket tifa-shop-http.socket\n",
		"Environment=SERVER_TLS_DOMAINS=shop.example.com SERVER_TLS_CACHE=%h/shop/certs\n",
		"WorkingDirectory=%h/shop/shared\n")

	tunnel := unitsOf("shop", 9531, "", false)
	has("tunnel socket", tunnel.socket, "[Socket]\nListenStream=127.0.0.1:9531\n")
	if tunnel.http != "" || strings.Contains(tunnel.service, "Environment=") {
		t.Errorf("behind the tunnel: %+v", tunnel)
	}
}

// The domain of HTTPS is the host of site.url, unless that is no domain: then plain HTTP.
func TestDeployOwnDomain(t *testing.T) {
	none, cf := config.Deploy{Front: "none"}, config.Deploy{Front: "cloudflare"}
	for _, tc := range []struct {
		d         config.Deploy
		url, want string
		fails     bool
	}{
		{none, "https://Shop.Example.com", "shop.example.com", false},
		{none, "https://shop.example.com:443/", "shop.example.com", false},
		{none, "", "", false},
		{none, "http://203.0.113.7", "", false},
		{none, "http://[2001:db8::1]:8080", "", false},
		{none, "http://localhost:3000", "", false},
		{none, "https://shop.example.com:8443", "", true},
		{cf, "https://shop.example.com", "", false},
	} {
		got, err := ownDomain(tc.d, tc.url)
		if got != tc.want || (err != nil) != tc.fails {
			t.Errorf("%s %q: %q, %v", tc.d.Front, tc.url, got, err)
		}
	}
}

// A fresh server, no domain: setup creates the user, lets its systemd open the ports as root,
// then makes the app on port 80. The plan says which port visitors need and that Tifa does not
// open it at the provider.
func TestSetupServesVisitorsOverHTTP(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	srv := newSetupServer(t, "", freshFacts)
	srv.deny(t)
	writeFile(t, filepath.Join(srv.dir, "rootfacts"), "lowport 1024\nlisten LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:((\"sshd\",pid=7,fd=3))\n")
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"change  server: let the systemd of tifa open ports 80 and 443, with net.ipv4.ip_unprivileged_port_start = 80 (now 1024) in /etc/sysctl.d/60-tifa-ports.conf, as root",
		"add     app: /srv/tifa/shop with an empty shared/.env, serving visitors itself on port 80, over plain HTTP since site.url names no domain, the systemd socket and service tifa-shop",
		"Visitors need port 80 of the server open. Tifa does not change your provider's firewall or security groups: if they block it, open it there.",
		"shop serves visitors itself on port 80 of the server. If the firewall or a security group of your provider blocks it, open it there: Tifa does not change those.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(srv.calls(), "server root\nprobe tifa\nports root\napp tifa") {
		t.Errorf("order of calls:\n%s", srv.calls())
	}
	if argv := srv.argv(); !strings.Contains(argv, "ListenStream=80") || strings.Contains(argv, "127.0.0.1:9527") {
		t.Errorf("the units of the app:\n%s", argv)
	}
}

// With a domain the app gets ports 80 and 443 and its own HTTPS, its certificates kept next to the
// releases. A second run finds it all in place.
func TestSetupServesVisitorsOverHTTPS(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n", nil)
	srv := newSetupServer(t, "", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine vps-1234\nlowport 80\n"+hostKeysFact)
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"add     app: /srv/tifa/shop with an empty shared/.env, serving visitors itself on ports 80 and 443, with HTTPS for shop.example.com and its certificate from Let's Encrypt kept in /srv/tifa/shop/certs, the systemd sockets and service tifa-shop",
		"Visitors need ports 80 and 443 of the server open. Tifa does not change your provider's firewall or security groups: if they block them, open them there.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ip_unprivileged_port_start") || strings.Contains(srv.calls(), "ports root") {
		t.Errorf("the deploy user may open the ports already:\n%s\n%s", out, srv.calls())
	}
	for _, want := range []string{"ListenStream=443", "FileDescriptorName=server.http_addr", "SERVER_TLS_DOMAINS=shop.example.com SERVER_TLS_CACHE=%h/shop/certs"} {
		if !strings.Contains(srv.argv(), want) {
			t.Errorf("the app script lacks %q", want)
		}
	}
	setFacts(t, srv, factsHTTPS("shop", "shop.example.com", "lowport 80\nusock [::]:443 tifa-shop.socket tifa-shop.service\nusock [::]:80 tifa-shop-http.socket tifa-shop.service\nlisten LISTEN 0 4096 *:443 *:*\nlisten LISTEN 0 4096 *:80 *:*\n"))
	out, err = runSetup(t, root, false, "")
	if err != nil || !strings.Contains(out, "matches tifa.yaml") || !strings.Contains(out, "shop serves visitors itself on ports 80 and 443 of the server") {
		t.Errorf("second run: %v\n%s", err, out)
	}
}

// An app of an earlier setup listens on the loopback. Setup moves it to port 80 in its plan, and
// the script stops the old socket before it opens the new one, then checks the app again.
func TestSetupMovesTheLoopbackApp(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	srv := newSetupServer(t, "", factsOf("shop", 9531, "lowport 1024\nappdir yes\nusock 127.0.0.1:9531 tifa-shop.socket tifa-shop.service\nlisten LISTEN 0 4096 127.0.0.1:9531 0.0.0.0:*\n"))
	out, err := runSetup(t, root, false, "")
	if err == nil {
		t.Fatal("applied without --yes")
	}
	for _, want := range []string{
		"change  server: let the systemd of tifa open ports 80 and 443",
		"change  app: move shop from 127.0.0.1:9531, which only the server reaches, to serving visitors itself on port 80, over plain HTTP since site.url names no domain. New systemd socket, and the app restarts on it",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(srv.calls(), "app tifa") || strings.Contains(srv.calls(), "ports root") {
		t.Fatalf("changed something:\n%s", srv.calls())
	}
	if out, err = runSetup(t, root, false, "", "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// ssh gets the script as one argument in single quotes, so a quote in it reads '\''.
	if !strings.Contains(srv.calls(), "ports root\napp tifa") || !strings.Contains(srv.argv(), `move='\''1'\''`) || !strings.Contains(srv.argv(), `restart='\''1'\''`) {
		t.Errorf("calls:\n%s", srv.calls())
	}

	// The script itself: the old socket stops before the units change, and the app restarts on the
	// new one and passes its check.
	body := appScript("shop", httpPort, "", false, true, true, 30)
	stop, write := strings.Index(body, `systemctl --user stop $old`), strings.Index(body, `printf '%s' "$socket_unit"`)
	if stop < 0 || write < 0 || stop > write || !strings.Contains(body, `systemctl --user restart "$unit.service"`) || !strings.Contains(body, "if check; then exit 0; fi") {
		t.Errorf("app script:\n%s", body)
	}
}

// Only one program can listen on a port. Setup stops before it changes anything when another app
// of Tifa or a program has port 80, or 443 for HTTPS, and names it.
func TestSetupStopsWhenPortsAreTaken(t *testing.T) {
	prepared := "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine vps-1234\nlowport 1024\n" + hostKeysFact
	for name, tc := range map[string]struct {
		yaml, facts, rootFacts string
		rootDenied             bool
		want                   []string // empty: setup goes on
	}{
		"another app": {
			yaml:  "deploy:\n",
			facts: "usock [::]:80 tifa-notes.socket tifa-notes.service\nlisten LISTEN 0 4096 *:80 *:*\n",
			want:  []string{"port 80 of vps is taken by notes, an app Tifa set up there", "remove notes from the server (go tool tifa deploy remove in its folder), or put shop behind Cloudflare with deploy.front: cloudflare in tifa.yaml", "Nothing was changed"},
		},
		"nginx": {
			yaml:      "deploy:\n",
			facts:     "listen LISTEN 0 511 0.0.0.0:80 0.0.0.0:*\n",
			rootFacts: "listen LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:((\"nginx\",pid=812,fd=6),(\"nginx\",pid=811,fd=6))\n",
			want:      []string{"port 80 of vps is taken by nginx (pid 812). Only one program can listen on a port: stop it on the server and keep it from starting at boot, or put shop behind Cloudflare", "Nothing was changed"},
		},
		"no root": {
			yaml:       "deploy:\n",
			facts:      "listen LISTEN 0 511 0.0.0.0:80 0.0.0.0:*\n",
			rootDenied: true,
			want:       []string{"port 80 of vps is taken by a program that is not a Tifa app", "run sudo ss -ltnp on the server"},
		},
		"caddy on 443 for HTTPS": {
			yaml:      "site:\n  url: https://shop.example.com\ndeploy:\n",
			facts:     "listen LISTEN 0 4096 *:443 *:*\n",
			rootFacts: "listen LISTEN 0 4096 *:443 *:* users:((\"caddy\",pid=55,fd=7))\n",
			want:      []string{"port 443 of vps is taken by caddy (pid 55)"},
		},
		"caddy on 443 without a domain": {
			yaml:      "deploy:\n",
			facts:     "listen LISTEN 0 4096 *:443 *:*\n",
			rootFacts: "listen LISTEN 0 4096 *:443 *:* users:((\"caddy\",pid=55,fd=7))\n",
		},
		"behind the tunnel": {
			yaml:  "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n",
			facts: "usock [::]:80 tifa-notes.socket tifa-notes.service\nlisten LISTEN 0 4096 *:80 *:*\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := setupApp(t, tc.yaml, nil)
			srv := newSetupServer(t, "", prepared+tc.facts)
			writeFile(t, filepath.Join(srv.dir, "rootfacts"), tc.rootFacts)
			if tc.rootDenied {
				writeFile(t, filepath.Join(srv.dir, "rootdenied"), "")
			}
			out, err := runSetup(t, root, false, "", "--yes")
			if len(tc.want) == 0 {
				if err != nil && strings.Contains(err.Error(), "taken") {
					t.Fatalf("%v\n%s", err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("no error\n%s", out)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("%q lacks %q", err, w)
				}
			}
			if strings.Contains(srv.calls(), "app tifa") || strings.Contains(srv.calls(), "ports root") || strings.Contains(out, "Plan for") {
				t.Errorf("changed something or planned:\n%s\n%s", srv.calls(), out)
			}
		})
	}
}

// serveApp is shop on a fake server on port, with domain for HTTPS. Its releases are made an hour
// apart from 10:00, and health is what each answers.
func serveApp(t *testing.T, port, domain string, health map[string]string) (root, home string, calls func() string) {
	t.Helper()
	root = t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n\ngo 1.27.0\n")
	t.Setenv(deployHostEnv, "vps")
	home = t.TempDir()
	writeFile(t, filepath.Join(home, "shop", "port"), port+"\n")
	if domain != "" {
		writeFile(t, filepath.Join(root, "tifa.yaml"), "site:\n  url: https://"+domain+"\n")
		writeFile(t, filepath.Join(home, "shop", "https"), domain+"\n")
	}
	if err := os.MkdirAll(filepath.Join(home, "shop", "releases"), 0o755); err != nil {
		t.Fatal(err)
	}
	calls = fakeServer(t, home, health)
	oldNow, oldBuild := deployNow, buildForServer
	t.Cleanup(func() { deployNow, buildForServer = oldNow, oldBuild })
	clock := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	deployNow = func() time.Time { c := clock; clock = clock.Add(time.Hour); return c }
	buildForServer = func(_ context.Context, _, _, _, out string, _, _ io.Writer) error {
		return os.WriteFile(out, []byte("program"), 0o755)
	}
	return root, home, calls
}

// Plain HTTP: the health check asks port 80 on the loopback. Deploy then names the address of the
// server and asks port 80 from this machine: closed, it says the provider may block it.
func TestDeployServesVisitorsOverHTTP(t *testing.T) {
	root, home, calls := serveApp(t, "80", "", map[string]string{"20261120T100000Z": "200", "20261120T110000Z": "200"})
	out, _, err := run(t, "deploy", "--dir", root)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(calls(), "http://127.0.0.1:80/healthz") || !strings.Contains(out, "health check: GET /healthz answered 200") {
		t.Errorf("health check:\n%s\n%s", calls(), out)
	}
	want := "visitors: http://203.0.113.7, once port 80 is open to them. Port 80 did not answer from this machine within 3 seconds: the firewall or a security group of your provider may block it. Open port 80 (tcp) there, which Tifa does not change\n"
	if !strings.HasSuffix(out, want) {
		t.Errorf("output does not end with %q:\n%s", want, out)
	}

	// Port 80 open: one line, no word about the firewall.
	writeFile(t, filepath.Join(home, ".run", "open-80"), "")
	out, _, err = run(t, "deploy", "--dir", root)
	if err != nil || !strings.HasSuffix(out, "visitors: http://203.0.113.7 (port 80 answers from this machine)\n") {
		t.Errorf("%v\n%s", err, out)
	}
}

// HTTPS: the health check asks the app over HTTPS for its domain on the loopback, as a visitor
// would. Without an answer, it says what a missing certificate needs.
func TestDeployServesVisitorsOverHTTPS(t *testing.T) {
	root, home, calls := serveApp(t, "443", "shop.example.com", map[string]string{"20261120T100000Z": "200", "20261120T110000Z": "000"})
	writeFile(t, filepath.Join(home, ".run", "open-80"), "")
	writeFile(t, filepath.Join(home, ".run", "open-443"), "")
	out, _, err := run(t, "deploy", "--dir", root)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(calls(), "curl -s -k -o /dev/null -w %{http_code} --max-time 5 --resolve shop.example.com:443:127.0.0.1 https://shop.example.com/healthz") {
		t.Errorf("health check:\n%s", calls())
	}
	if !strings.HasSuffix(out, "visitors: https://shop.example.com (ports 80 and 443 answer from this machine)\n") {
		t.Errorf("output:\n%s", out)
	}

	// The next release gets no answer: the check says what HTTPS needs, and the one before runs.
	out, _, err = run(t, "deploy", "--dir", root)
	if err == nil || !strings.Contains(out, "health check: GET https://shop.example.com/healthz got no answer within 30 seconds. Without a certificate for shop.example.com the program answers no HTTPS: Let's Encrypt must reach this server at shop.example.com on port 80 or 443") {
		t.Errorf("%v\n%s", err, out)
	}
	if strings.Contains(out, "visitors:") {
		t.Errorf("a failed deploy names where visitors go:\n%s", out)
	}
}

// A deploy that makes the app on a server where only root may open port 80 yet says to run setup,
// and changes nothing.
func TestDeployCreateNeedsSetupForPort80(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n\ngo 1.27.0\n")
	t.Setenv(deployHostEnv, "vps")
	home := t.TempDir()
	prepareFakeServer(t, home)
	calls := fakeServer(t, home, nil)
	writeFile(t, filepath.Join(home, ".run", "lowport"), "1024\n")
	_, _, err := run(t, "deploy", "--dir", root)
	if err == nil || !strings.Contains(err.Error(), "shop serves visitors on port 80 itself, which only root may open on vps yet: run go tool tifa deploy setup") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(calls(), "enable --now") {
		t.Errorf("made the app:\n%s", calls())
	}
}

// remove of the app that served visitors itself takes its port 80 socket and the setting setup made
// for it, as root.
func TestDeployRemoveServesVisitors(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n", nil)
	srv := newSetupServer(t, "", factsHTTPS("shop", "shop.example.com", "appdir yes\nports-conf yes\n"))
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"remove  server: stop and disable tifa-shop.socket, tifa-shop-http.socket and tifa-shop.service, delete their unit files and /srv/tifa/shop with its releases, shared/.env, the data in shared/ and deploys.log, and its certificates in certs/",
		"remove  server: /etc/sysctl.d/60-tifa-ports.conf, which let the systemd of tifa open ports below 1024, and the value from before it, as root",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(srv.calls(), "removeapp tifa") || !strings.Contains(srv.calls(), "unports root") {
		t.Errorf("calls:\n%s", srv.calls())
	}
	if !strings.Contains(removeAppBody, `-http.socket`) {
		t.Error("the removal script leaves the socket of port 80")
	}
}

// This machine asks the app over HTTP: an answer below 500 counts, a redirect too. A connection that
// a proxy on this machine accepts and then drops is no answer, and neither is an error page of 502.
func TestDeployPortAnswers(t *testing.T) {
	ask := func(url string) bool {
		t.Helper()
		u, err := neturl.Parse(url)
		if err != nil {
			t.Fatal(err)
		}
		port, _ := strconv.Atoi(u.Port())
		return askPort(context.Background(), u.Scheme, u.Hostname(), port)
	}
	status := func(code int) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != healthPath {
				t.Errorf("asked for %s", r.URL.Path)
			}
			if code == http.StatusPermanentRedirect {
				http.Redirect(w, r, "https://shop.example.com/", code)
				return
			}
			w.WriteHeader(code)
		}))
		t.Cleanup(s.Close)
		return s
	}
	for code, want := range map[int]bool{http.StatusNotFound: true, http.StatusPermanentRedirect: true, http.StatusBadGateway: false} {
		if got := ask(status(code).URL); got != want {
			t.Errorf("%d: %v", code, got)
		}
	}
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	t.Cleanup(tls.Close)
	if !ask(tls.URL) {
		t.Error("HTTPS with a certificate this machine does not trust: no answer")
	}
	drop, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { drop.Close() })
	go func() {
		for {
			c, err := drop.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if ask("http://" + drop.Addr().String()) {
		t.Error("a connection that is dropped counts as an answer")
	}
}
