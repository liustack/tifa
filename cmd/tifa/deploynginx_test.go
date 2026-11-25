package main

// deploy.front nginx: the text of a site, and the scripts of setup and remove run for real under sh
// on this machine, in a folder that stands for /etc, with fake nginx, certbot, apt-get, dpkg-query,
// systemctl and ss in the PATH.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// nginxBox is a server for the scripts: etc is its /etc, calls what the fake programs were asked.
type nginxBox struct {
	t    *testing.T
	etc  string
	bin  string
	live string // programs that exist once apt installed them
	logf string
}

func newNginxBox(t *testing.T) *nginxBox {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the server scripts run under sh")
	}
	b := &nginxBox{t: t, etc: t.TempDir(), bin: t.TempDir(), live: t.TempDir()}
	b.logf = filepath.Join(t.TempDir(), "calls")
	old := etcDir
	etcDir = b.etc
	t.Cleanup(func() { etcDir = old })
	log := shellQuote(b.logf)
	tool := func(name, body string) {
		writeFile(t, filepath.Join(b.bin, name), "#!/bin/sh\n"+body+"\n")
		if err := os.Chmod(filepath.Join(b.bin, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	etc := shellQuote(b.etc)
	// nginx -t fails when a site holds the word BAD, as a real one fails on a directive it does not know.
	tool("nginx.fake", `echo "nginx $*" >> `+log+`
if grep -rq BAD `+etc+`/nginx/conf.d 2>/dev/null; then echo "nginx: [emerg] unknown directive BAD" >&2; exit 1; fi
echo "nginx: configuration file test is successful" >&2`)
	// systemctl: nginx is active when the file etc/active-nginx is there. Start makes it so.
	tool("systemctl", `echo "systemctl $*" >> `+log+`
case "$1 $2" in
"is-active --quiet") [ -f `+etc+`/active-$3 ] ;;
"start nginx") touch `+etc+`/active-nginx ;;
esac`)
	// apt-get: -s prints an Inst line for each package and its dependency, and install makes the
	// packages exist for dpkg-query.
	tool("apt-get", `echo "apt-get $*" >> `+log+`
case " $* " in
*" -s "*)
	for p in "$@"; do case "$p" in -*|install) ;; *) echo "Inst $p (1.0 Ubuntu:24.04/noble [amd64])"; [ "$p" = nginx ] && echo "Inst nginx-common (1.0 Ubuntu:24.04/noble [all])"; [ "$p" = certbot ] && echo "Inst python3-certbot (1.0 Ubuntu:24.04/noble [all])" ;; esac; done ;;
*" install "*)
	for p in "$@"; do case "$p" in -*|install) ;; *) grep -qx "$p" `+etc+`/dpkg-installed 2>/dev/null || echo "$p" >> `+etc+`/dpkg-installed; [ "$p" = nginx ] && { cp `+shellQuote(filepath.Join(b.bin, "nginx.fake"))+` `+shellQuote(filepath.Join(b.live, "nginx"))+`; mkdir -p `+etc+`/nginx/conf.d `+etc+`/nginx/sites-enabled; touch `+etc+`/nginx/sites-enabled/default `+etc+`/active-nginx; }; [ "$p" = certbot ] && { cp `+shellQuote(filepath.Join(b.bin, "certbot.fake"))+` `+shellQuote(filepath.Join(b.live, "certbot"))+`; touch `+etc+`/certbot-installed; }; [ "$p" = python3-certbot-nginx ] && touch `+etc+`/plugin ;; esac; done ;;
*" purge "*)
	for p in "$@"; do case "$p" in -*|purge) ;; *) grep -vx "$p" `+etc+`/dpkg-installed > `+etc+`/dpkg.new 2>/dev/null; mv `+etc+`/dpkg.new `+etc+`/dpkg-installed ;; esac; done
	rm -f `+etc+`/active-nginx `+etc+`/certbot-installed `+etc+`/plugin `+shellQuote(filepath.Join(b.live, "nginx"))+` `+shellQuote(filepath.Join(b.live, "certbot"))+` ;;
esac
exit 0`)
	tool("dpkg-query", `[ -f `+etc+`/dpkg-installed ] || exit 0
for p in $(cat `+etc+`/dpkg-installed); do case "$p" in nginx*) printf 'ii  %s\n' "$p" ;; esac; done`)
	tool("certbot.fake", `echo "certbot $*" >> `+log+`
case "$1" in
plugins) [ -f `+etc+`/plugin ] && echo "* nginx" ;;
certonly)
	app=""; host=""
	while [ $# -gt 0 ]; do case "$1" in --cert-name) app=${2#tifa-} ;; -d) host=$2 ;; esac; shift; done
	[ -f `+etc+`/certbot-fail ] && { echo "Challenge failed for domain $host"; exit 1; }
	mkdir -p `+etc+`/letsencrypt/live/tifa-$app `+etc+`/letsencrypt/renewal
	printf 'DNS:%s\n' "$host" > `+etc+`/letsencrypt/live/tifa-$app/cert.pem
	touch `+etc+`/letsencrypt/renewal/tifa-$app.conf ;;
delete)
	app=""; while [ $# -gt 0 ]; do case "$1" in --cert-name) app=${2#tifa-} ;; esac; shift; done
	rm -rf `+etc+`/letsencrypt/live/tifa-$app `+etc+`/letsencrypt/renewal/tifa-$app.conf ;;
esac`)
	tool("openssl", `cat "$6" | head -n 1`)
	tool("ss", `cat `+etc+`/ss 2>/dev/null; true`)
	if _, err := exec.LookPath("sha256sum"); err != nil {
		tool("sha256sum", `shasum -a 256 "$@"`)
	}
	return b
}

// run runs a script as root on the box and returns what it printed.
func (b *nginxBox) run(script string) (stdout, stderr string, err error) {
	b.t.Helper()
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+b.bin+string(os.PathListSeparator)+b.live+string(os.PathListSeparator)+"/usr/bin:/bin:/usr/sbin:/sbin")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

// have makes a program there, as if apt had installed it.
func (b *nginxBox) have(name string) {
	b.t.Helper()
	data, err := os.ReadFile(filepath.Join(b.bin, name+".fake"))
	if err != nil {
		b.t.Fatal(err)
	}
	writeFile(b.t, filepath.Join(b.live, name), string(data))
	if err := os.Chmod(filepath.Join(b.live, name), 0o755); err != nil {
		b.t.Fatal(err)
	}
}

func (b *nginxBox) calls() string {
	data, _ := os.ReadFile(b.logf)
	return string(data)
}

func (b *nginxBox) read(rel string) string {
	data, _ := os.ReadFile(filepath.Join(b.etc, rel))
	return string(data)
}

func (b *nginxBox) state() nginxState {
	b.t.Helper()
	out, _, err := b.run(nginxProbeScript())
	if err != nil {
		b.t.Fatal(err)
	}
	ns, err := parseNginxState(out)
	if err != nil {
		b.t.Fatalf("%v\n%s", err, out)
	}
	return ns
}

func (b *nginxBox) mustRun(script string) string {
	b.t.Helper()
	out, errOut, err := b.run(script)
	if err != nil {
		b.t.Fatalf("%v\n%s\n%s\ncalls:\n%s", err, out, errOut, b.calls())
	}
	return out
}

// A site passes Host and Origin as the browser sent them, keeps connections to the app, waits as
// long as the program lets a request run, and leaves compression, body size and buffering of
// events to the program. A domain gets its own server, HTTPS once it has a certificate. Without a
// domain the app is the default server of port 80.
func TestNginxSiteFile(t *testing.T) {
	has := func(name, text string, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(text, w) {
				t.Errorf("%s lacks %q:\n%s", name, w, text)
			}
		}
	}
	none := nginxSiteFile("shop", "", 9527, false)
	has("no domain", none, "# tifa:host -\n", "upstream tifa-shop {\n\tserver 127.0.0.1:9527;\n\tkeepalive 16;\n\tkeepalive_timeout 50s;\n}\n",
		"listen 80 default_server;", "listen [::]:80 default_server;", "proxy_pass http://tifa-shop;", "proxy_http_version 1.1;",
		`proxy_set_header Connection "";`, "proxy_set_header Host $http_host;", "proxy_set_header X-Forwarded-For $remote_addr;",
		"proxy_read_timeout 1d;", "proxy_send_timeout 1d;", "proxy_request_buffering off;", "client_max_body_size 0;", "gzip off;")
	if strings.Contains(none, "443") || strings.Contains(none, "ssl") {
		t.Errorf("no domain has no HTTPS:\n%s", none)
	}
	// Nothing in the site changes Origin or turns off the buffering that X-Accel-Buffering asks for.
	for _, no := range []string{"Origin", "proxy_ignore_headers", "proxy_buffering", "Upgrade", "add_header"} {
		if strings.Contains(none, no) {
			t.Errorf("the site has %q:\n%s", no, none)
		}
	}

	http := nginxSiteFile("shop", "shop.example.com", 9527, false)
	has("domain without certificate", http, "# tifa:host shop.example.com\n", "listen 80;", "server_name shop.example.com;", "proxy_pass http://tifa-shop;")
	if strings.Contains(http, "default_server") || strings.Contains(http, "443") {
		t.Errorf("domain without certificate:\n%s", http)
	}

	etcDir = "/etc"
	tls := nginxSiteFile("shop", "shop.example.com", 9527, true)
	has("domain with certificate", tls, "location / {\n\t\treturn 308 https://$host$request_uri;", "listen 443 ssl http2;", "listen [::]:443 ssl http2;",
		"ssl_certificate /etc/letsencrypt/live/tifa-shop/fullchain.pem;", "ssl_certificate_key /etc/letsencrypt/live/tifa-shop/privkey.pem;", "ssl_protocols TLSv1.2 TLSv1.3;")
	if strings.Count(tls, "proxy_pass") != 1 || strings.Contains(tls, "default_server") || strings.Contains(tls, "Strict-Transport") {
		t.Errorf("domain with certificate:\n%s", tls)
	}
}

// The probe tells a server without nginx from one with an nginx of someone else and one that
// setup installed, which it knows by the marker.
func TestNginxProbe(t *testing.T) {
	b := newNginxBox(t)
	ns := b.state()
	if ns.installed() || ns.foreign() || ns.Tifa || len(ns.Sites) != 0 {
		t.Fatalf("empty server: %+v", ns)
	}

	// A hand-installed nginx has no marker.
	writeFile(t, filepath.Join(b.etc, "dpkg-installed"), "nginx\nnginx-common\n")
	if ns = b.state(); !ns.foreign() || len(ns.Pkgs) != 2 || ns.Pkgs[0] != "nginx" {
		t.Fatalf("hand-installed nginx: %+v", ns)
	}

	// The marker makes it Tifa's, and says which packages apt added.
	writeFile(t, filepath.Join(b.etc, "tifa", "nginx"), "# header\ninstalled nginx nginx\ninstalled nginx nginx-common\ninstalled certbot certbot\n")
	writeFile(t, filepath.Join(b.etc, "nginx", "conf.d", "tifa-shop.conf"), nginxSiteFile("shop", "shop.example.com", 9527, false))
	writeFile(t, filepath.Join(b.etc, "nginx", "conf.d", "tifa-blog.conf"), nginxSiteFile("blog", "", 9528, false))
	writeFile(t, filepath.Join(b.etc, "nginx", "conf.d", "mine.conf"), "server {}\n")
	writeFile(t, filepath.Join(b.etc, "letsencrypt", "live", "tifa-shop", "cert.pem"), "DNS:shop.example.com\n")
	writeFile(t, filepath.Join(b.etc, "letsencrypt", "renewal", "tifa-shop.conf"), "")
	writeFile(t, filepath.Join(b.etc, "active-nginx"), "")
	writeFile(t, filepath.Join(b.etc, "ss"), "LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:((\"nginx\",pid=812,fd=6))\n")
	ns = b.state()
	if ns.foreign() || !ns.Tifa || len(ns.TifaPkgs) != 2 || len(ns.TifaCerts) != 1 || !ns.Active || !ns.nginxPrograms(80) || ns.nginxPrograms(443) {
		t.Errorf("Tifa's nginx: %+v", ns)
	}
	shop, ok := ns.site("shop")
	if !ok || shop.Host != "shop.example.com" || shop.Sum != sha(nginxSiteFile("shop", "shop.example.com", 9527, false)) {
		t.Errorf("shop: %+v", shop)
	}
	if blog, _ := ns.site("blog"); blog.Host != "-" || len(ns.Sites) != 2 {
		t.Errorf("sites: %+v", ns.Sites)
	}
	if c, ok := ns.cert("shop"); !ok || c.Host != "shop.example.com" {
		t.Errorf("certs: %+v", ns.Certs)
	}

	// Still Tifa's after the package changes: the marker is a file, not a version.
	writeFile(t, filepath.Join(b.etc, "dpkg-installed"), "nginx\nnginx-common\nnginx-extra\n")
	if ns = b.state(); ns.foreign() {
		t.Errorf("after an upgrade: %+v", ns)
	}
}

// Install writes the marker before it installs, records the packages apt adds and nothing else, and
// takes the welcome site down. Certbot comes with its nginx plugin and is marked the same way.
func TestNginxInstallMarksWhatItInstalls(t *testing.T) {
	b := newNginxBox(t)
	out := b.mustRun(nginxInstallScript([]string{"nginx"}, nil))
	if !strings.Contains(out, "installed nginx") {
		t.Errorf("out %q", out)
	}
	marker := b.read("tifa/nginx")
	for _, want := range []string{"installed nginx nginx\n", "installed nginx nginx-common\n"} {
		if !strings.Contains(marker, want) {
			t.Errorf("marker lacks %q:\n%s", want, marker)
		}
	}
	if strings.Contains(marker, "certbot") && strings.Contains(marker, "installed certbot") {
		t.Errorf("marker names certbot:\n%s", marker)
	}
	if exists(filepath.Join(b.etc, "nginx", "sites-enabled", "default")) {
		t.Error("the welcome site is still on")
	}
	if !strings.Contains(b.calls(), "systemctl enable --quiet nginx") {
		t.Errorf("calls:\n%s", b.calls())
	}
	if ns := b.state(); !ns.Tifa || ns.foreign() {
		t.Errorf("state %+v", ns)
	}

	b.mustRun(nginxInstallScript(nil, []string{"certbot", "python3-certbot-nginx"}))
	marker = b.read("tifa/nginx")
	for _, want := range []string{"installed certbot certbot\n", "installed certbot python3-certbot\n", "installed certbot python3-certbot-nginx\n"} {
		if !strings.Contains(marker, want) {
			t.Errorf("marker lacks %q:\n%s", want, marker)
		}
	}
	if strings.Count(marker, "installed nginx nginx\n") != 1 {
		t.Errorf("a line twice:\n%s", marker)
	}
}

// A site goes in only when nginx -t takes it. The file before it comes back otherwise, nginx says
// why, and no reload happens: the other apps never see the bad file. A new site that fails leaves no file.
func TestNginxSiteChecksBeforeItReloads(t *testing.T) {
	b := newNginxBox(t)
	b.mustRun(nginxInstallScript([]string{"nginx"}, nil))
	good := nginxSiteFile("shop", "", 9527, false)
	out := b.mustRun(nginxSiteScript("shop", good))
	if b.read("nginx/conf.d/tifa-shop.conf") != good || !strings.Contains(out, "nginx reloaded") {
		t.Fatalf("first site: %q\n%s", out, b.read("nginx/conf.d/tifa-shop.conf"))
	}
	// nginx -t comes before the reload.
	calls := b.calls()
	if i, j := strings.Index(calls, "nginx -t"), strings.Index(calls, "systemctl reload nginx"); i < 0 || j < i {
		t.Errorf("order:\n%s", calls)
	}
	if !exists(filepath.Join(b.etc, "nginx", "conf.d")) || exists(filepath.Join(b.etc, "nginx", "conf.d", "tifa-shop.conf.last")) {
		t.Error("a copy of the file is left behind")
	}

	reloads := strings.Count(b.calls(), "systemctl reload nginx")
	_, errOut, err := b.run(nginxSiteScript("shop", good+"BAD;\n"))
	if err == nil || !strings.Contains(errOut, "nginx said:") || !strings.Contains(errOut, "unknown directive BAD") {
		t.Fatalf("a bad site: %v\n%s", err, errOut)
	}
	if b.read("nginx/conf.d/tifa-shop.conf") != good || strings.Count(b.calls(), "systemctl reload nginx") != reloads {
		t.Errorf("the good file is not back, or nginx reloaded:\n%s", b.read("nginx/conf.d/tifa-shop.conf"))
	}

	_, _, err = b.run(nginxSiteScript("blog", "BAD\n"))
	if err == nil || exists(filepath.Join(b.etc, "nginx", "conf.d", "tifa-blog.conf")) {
		t.Errorf("a new bad site: %v", err)
	}
}

// A stopped nginx is started instead of reloaded.
func TestNginxSiteStartsAStoppedNginx(t *testing.T) {
	b := newNginxBox(t)
	b.mustRun(nginxInstallScript([]string{"nginx"}, nil))
	if err := os.Remove(filepath.Join(b.etc, "active-nginx")); err != nil {
		t.Fatal(err)
	}
	b.mustRun(nginxSiteScript("shop", nginxSiteFile("shop", "", 9527, false)))
	if !strings.Contains(b.calls(), "systemctl start nginx") || strings.Contains(b.calls(), "systemctl reload nginx") {
		t.Errorf("calls:\n%s", b.calls())
	}
}

// The certificate is made as tifa-<app> by the nginx plugin of certbot, with a hook that reloads nginx
// on renewal. A refusal of Let's Encrypt is the error, with what certbot said.
func TestNginxCertificate(t *testing.T) {
	b := newNginxBox(t)
	b.have("certbot")
	b.mustRun(nginxCertScript("shop", "shop.example.com", false))
	calls := b.calls()
	for _, want := range []string{"certbot certonly --nginx --non-interactive --agree-tos --register-unsafely-without-email --cert-name tifa-shop -d shop.example.com --deploy-hook systemctl reload nginx"} {
		if !strings.Contains(calls, want) {
			t.Errorf("calls lack %q:\n%s", want, calls)
		}
	}
	if strings.Contains(calls, "certbot delete") {
		t.Errorf("deleted:\n%s", calls)
	}
	if c, ok := b.state().cert("shop"); !ok || c.Host != "shop.example.com" {
		t.Errorf("cert %+v", c)
	}
	b.mustRun(nginxCertScript("shop", "shop.example.org", true))
	if !strings.Contains(b.calls(), "certbot delete --non-interactive --cert-name tifa-shop") {
		t.Errorf("the old certificate stays:\n%s", b.calls())
	}
	writeFile(t, filepath.Join(b.etc, "certbot-fail"), "")
	_, errOut, err := b.run(nginxCertScript("blog", "blog.example.com", false))
	if err == nil || !strings.Contains(errOut, "DNS must point blog.example.com at this server") || !strings.Contains(errOut, "Challenge failed") {
		t.Errorf("a refusal: %v\n%s", err, errOut)
	}
}

// Remove takes a site out the way it went in, then its certificate, and once no site is left purges
// what the marker names and its own configuration. The packages of certbot go only when setup
// installed it: here it did not, so its folder stays.
func TestNginxRemove(t *testing.T) {
	b := newNginxBox(t)
	b.mustRun(nginxInstallScript([]string{"nginx"}, nil))
	b.have("certbot")
	b.mustRun(nginxSiteScript("shop", nginxSiteFile("shop", "", 9527, false)))
	b.mustRun(nginxSiteScript("blog", nginxSiteFile("blog", "blog.example.com", 9528, false)))
	b.mustRun(nginxCertScript("blog", "blog.example.com", false))

	// A bad configuration without the site keeps the site.
	writeFile(t, filepath.Join(b.etc, "nginx", "conf.d", "mine.conf"), "BAD\n")
	_, errOut, err := b.run(nginxRemoveSiteScript("blog"))
	if err == nil || !strings.Contains(errOut, "the site is back") || !exists(filepath.Join(b.etc, "nginx", "conf.d", "tifa-blog.conf")) {
		t.Fatalf("a failed removal: %v\n%s", err, errOut)
	}
	os.Remove(filepath.Join(b.etc, "nginx", "conf.d", "mine.conf"))

	b.mustRun(nginxRemoveSiteScript("blog"))
	b.mustRun(nginxRemoveCertScript("blog"))
	ns := b.state()
	if _, ok := ns.site("blog"); ok || len(ns.Certs) != 0 || len(ns.Sites) != 1 {
		t.Errorf("after the first removal: %+v", ns)
	}
	if !strings.Contains(b.calls(), "certbot delete --non-interactive --cert-name tifa-blog") {
		t.Errorf("calls:\n%s", b.calls())
	}
	// Removing what is not there is no error.
	b.mustRun(nginxRemoveSiteScript("blog"))

	b.mustRun(nginxRemoveSiteScript("shop"))
	writeFile(t, filepath.Join(b.etc, "letsencrypt", "cli.ini"), "mine\n")
	out := b.mustRun(nginxUninstallScript())
	if !strings.Contains(out, "uninstalled nginx nginx-common") {
		t.Errorf("out %q", out)
	}
	if exists(filepath.Join(b.etc, "nginx")) || exists(filepath.Join(b.etc, "tifa")) {
		t.Error("nginx or the marker is still there")
	}
	if !exists(filepath.Join(b.etc, "letsencrypt", "cli.ini")) {
		t.Error("deleted the certbot of someone else")
	}
	if ns = b.state(); ns.installed() || ns.Tifa {
		t.Errorf("after the uninstall: %+v", ns)
	}

	// With certbot of Tifa, it goes with its configuration.
	b.mustRun(nginxInstallScript([]string{"nginx"}, []string{"certbot", "python3-certbot-nginx"}))
	b.mustRun(nginxUninstallScript())
	if exists(filepath.Join(b.etc, "letsencrypt")) || exists(filepath.Join(b.etc, "certbot-installed")) {
		t.Error("certbot of Tifa is still there")
	}
	// Nothing to do without a marker: the nginx of someone else is never purged.
	writeFile(t, filepath.Join(b.etc, "dpkg-installed"), "nginx\n")
	b.mustRun(nginxUninstallScript())
	if !strings.Contains(b.read("dpkg-installed"), "nginx") || strings.Count(b.calls(), "apt-get purge") != 2 {
		t.Errorf("purged an nginx without the marker:\n%s", b.calls())
	}
}

func testTarget() deployTarget { return deployTarget{app: "shop", host: "vps", front: "nginx"} }

// Setup stops, and changes nothing, for an nginx it did not install, for any other program on port
// 80 or 443, for an app of Tifa that holds them, and for two apps that want the same name.
func TestNginxGuard(t *testing.T) {
	nginxOf := "LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:((\"nginx\",pid=812,fd=6))"
	listen := func(line string) []listener {
		l, _ := parseListen(line)
		return []listener{l}
	}
	for name, tc := range map[string]struct {
		target deployTarget
		ns     nginxState
		socks  []userSocket
		want   string // empty: no error
	}{
		"nothing there":    {testTarget(), nginxState{}, nil, ""},
		"foreign package":  {testTarget(), nginxState{Pkgs: []string{"nginx", "nginx-common"}}, nil, "vps has an nginx that Tifa did not install (nginx, nginx-common). Tifa never changes the nginx of someone else"},
		"foreign program":  {testTarget(), nginxState{Bin: "/usr/local/sbin/nginx"}, nil, "an nginx that Tifa did not install (/usr/local/sbin/nginx)"},
		"Tifa's nginx":     {testTarget(), nginxState{Pkgs: []string{"nginx"}, Tifa: true, Listen: listen(nginxOf)}, nil, ""},
		"another program":  {testTarget(), nginxState{Listen: listen("LISTEN 0 4096 *:443 *:* users:((\"caddy\",pid=9,fd=7))")}, nil, "port 443 of vps is taken by caddy (pid 9). nginx needs ports 80 and 443"},
		"another app":      {testTarget(), nginxState{}, []userSocket{{Port: 80, Service: "tifa-notes.service"}}, "port 80 of vps is taken by notes, an app Tifa set up there that serves visitors itself"},
		"the app itself":   {testTarget(), nginxState{Listen: listen("LISTEN 0 4096 *:80 *:*")}, []userSocket{{Port: 80, Service: "tifa-shop.service"}}, ""},
		"a second default": {testTarget(), nginxState{Pkgs: []string{"nginx"}, Tifa: true, Sites: []nginxSiteInfo{{App: "blog", Host: "-"}}}, nil, "shop has no domain in site.url, and blog on vps already answers the visitors that name none"},
		"a domain and one without": {deployTarget{app: "shop", host: "vps", front: "nginx", own: "shop.example.com"},
			nginxState{Pkgs: []string{"nginx"}, Tifa: true, Sites: []nginxSiteInfo{{App: "blog", Host: "-"}}}, nil, ""},
		"the same host": {deployTarget{app: "shop", host: "vps", front: "nginx", own: "shop.example.com"},
			nginxState{Pkgs: []string{"nginx"}, Tifa: true, Sites: []nginxSiteInfo{{App: "old", Host: "shop.example.com"}}}, nil, "old on vps already answers shop.example.com through nginx"},
		"its own site": {testTarget(), nginxState{Pkgs: []string{"nginx"}, Tifa: true, Sites: []nginxSiteInfo{{App: "shop", Host: "-"}}}, nil, ""},
	} {
		err := nginxGuard(tc.target, tc.ns, tc.socks)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: %v", name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want) || !strings.HasSuffix(err.Error(), "Nothing was changed")):
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Nginx first, and for an app with a domain certbot and its plugin, each only when missing.
func TestNginxPackages(t *testing.T) {
	tifa := nginxState{Pkgs: []string{"nginx"}, Tifa: true}
	for name, tc := range map[string]struct {
		ns            nginxState
		domain        bool
		nginx, certbt string
	}{
		"fresh, no domain":       {nginxState{}, false, "nginx", ""},
		"fresh, domain":          {nginxState{}, true, "nginx", "certbot python3-certbot-nginx"},
		"nginx there, domain":    {tifa, true, "", "certbot python3-certbot-nginx"},
		"all there":              {nginxState{Pkgs: []string{"nginx"}, Tifa: true, Certbot: "/usr/bin/certbot", Plugin: true}, true, "", ""},
		"certbot without plugin": {nginxState{Pkgs: []string{"nginx"}, Tifa: true, Certbot: "/usr/bin/certbot"}, true, "", "python3-certbot-nginx"},
		"stale marker":           {nginxState{Tifa: true}, false, "nginx", ""},
	} {
		n, c := nginxPackages(tc.ns, tc.domain)
		if strings.Join(n, " ") != tc.nginx || strings.Join(c, " ") != tc.certbt {
			t.Errorf("%s: %v %v", name, n, c)
		}
	}
}
