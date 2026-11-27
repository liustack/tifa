package main

// deploy.front nginx in setup and remove, against the fake ssh of the setup tests: the plan, what
// runs as root and in which order, a second run that changes nothing, the stop for an nginx or a
// program that is not Tifa's, the moves between fronts, and remove.

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// nginxFacts is what the root probe prints for a server whose nginx setup installed: one package
// and its dependency in the marker, an optional certbot, the sites given as "app host" and the ports
// nginx holds.
func nginxFacts(certbot bool, certs []string, sites ...nginxSiteInfo) string {
	var b strings.Builder
	b.WriteString("apt yes\nipv6 yes\npkg nginx ii\npkg nginx-common ii\nbin /usr/sbin/nginx\ntifa yes\ntifa-nginx nginx\ntifa-nginx nginx-common\nactive yes\n")
	if certbot {
		b.WriteString("tifa-certbot certbot\ntifa-certbot python3-certbot-nginx\ncertbot /usr/bin/certbot\nplugin yes\n")
	}
	for _, s := range sites {
		b.WriteString("site " + s.App + " " + s.Sum + " " + s.Host + "\n")
	}
	for _, c := range certs {
		b.WriteString("cert " + c + "\n")
	}
	b.WriteString("listen LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:((\"nginx\",pid=812,fd=6))\n")
	return b.String()
}

func (s *setupServer) nginx(t *testing.T, facts string) {
	t.Helper()
	writeFile(t, filepath.Join(s.dir, "nginxfacts"), facts)
}

// userFacts is the probe of the deploy user for the app shop behind a loopback port and nginx.
func nginxUserFacts(port int, sites ...string) string {
	extra := "appdir yes\nusock 127.0.0.1:9527 tifa-shop.socket tifa-shop.service\nlisten LISTEN 0 4096 127.0.0.1:9527 0.0.0.0:*\n"
	if len(sites) > 0 {
		extra += "nginx-tifa yes\nlisten LISTEN 0 511 0.0.0.0:80 0.0.0.0:*\n"
	}
	for _, s := range sites {
		extra += "nginx-site " + s + "\n"
	}
	return factsOf("shop", port, extra)
}

func siteOf(app, host string, port int, tls bool) nginxSiteInfo {
	return nginxSiteInfo{App: app, Sum: sha(nginxSiteFile(app, host, port, tls, true)), Host: pick(host == "", "-", host)}
}

// A fresh server, an app without a domain: setup makes the user and the app on a loopback port,
// then installs nginx as a line of the plan and writes the site as the default server of port 80.
// There is no certbot and no certificate, and the plan says which ports visitors need.
func TestSetupNginxWithoutDomain(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: nginx\n", nil)
	srv := newSetupServer(t, "", freshFacts)
	srv.deny(t)
	srv.nginx(t, "apt yes\n")
	out, err := runSetup(t, root, false, "")
	if err == nil {
		t.Fatal("applied without --yes")
	}
	for _, want := range []string{
		"add     app: /srv/tifa/shop with an empty shared/.env, port 9527, the systemd socket and service tifa-shop",
		"add     server: install nginx with apt, which routes the visitors of the apps of this server, and write /etc/tifa/nginx to mark it as Tifa's, as root",
		"add     nginx: the site of the visitors that name no domain -> 127.0.0.1:9527 in /etc/nginx/conf.d/tifa-shop.conf, which nginx checks with nginx -t before it reloads",
		"Visitors need ports 80 and 443 of the server open. Tifa does not change your provider's firewall or security groups: if they block them, open them there.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "certbot") || strings.Contains(out, "ip_unprivileged_port_start") {
		t.Errorf("certbot or the low ports:\n%s", out)
	}
	if strings.Contains(srv.calls(), "nginxinstall") || strings.Contains(srv.calls(), "app tifa") {
		t.Fatalf("changed something:\n%s", srv.calls())
	}

	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	calls := srv.calls()
	// The app first, on its loopback port, then nginx: install, then the site.
	if i, j, k := strings.Index(calls, "app tifa"), strings.Index(calls, "nginxinstall root"), strings.Index(calls, "nginxsite root"); i < 0 || j < i || k < j {
		t.Errorf("order of calls:\n%s", calls)
	}
	argv := srv.unquoted()
	for _, want := range []string{"ListenStream=127.0.0.1:9527", "listen 80 default_server;", "server 127.0.0.1:9527;", "nginx_pkgs='nginx'", "certbot_pkgs=''"} {
		if !strings.Contains(argv, want) {
			t.Errorf("the scripts lack %q", want)
		}
	}
	// The server of the fresh facts has no IPv6, which nginx cannot listen on.
	if strings.Contains(argv, "ListenStream=80") || strings.Contains(argv, "certonly") || strings.Contains(argv, "[::]") {
		t.Errorf("the app serves itself, or a certificate was asked for:\n%s", argv)
	}
	if !strings.Contains(out, "shop is served by nginx on port 80 of the server. If the firewall or a security group of your provider blocks it, open it there: Tifa does not change those.") {
		t.Errorf("ending:\n%s", out)
	}
}

// An app with a domain also gets certbot with its nginx plugin, and its certificate once the site
// answers on port 80, then HTTPS in the site. The site is written first without HTTPS, which
// certbot needs, and again with it.
func TestSetupNginxWithDomain(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: nginx\n", nil)
	srv := newSetupServer(t, "", factsOf("shop", 9527, "appdir yes\n"))
	srv.nginx(t, "apt yes\n")
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"add     server: install nginx with apt",
		"add     server: install certbot and its nginx plugin with apt, which gets the certificates and renews them with its own timer, as root",
		"add     nginx: the site of shop.example.com -> 127.0.0.1:9527 in /etc/nginx/conf.d/tifa-shop.conf",
		"add     server: the certificate of shop.example.com from Let's Encrypt with certbot, which renews it with its own timer, and HTTPS in the site of shop",
		"shop is served by nginx on ports 80 and 443 of the server.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(srv.calls(), "nginxinstall root\nnginxinstall root\nnginxsite root\nnginxcert root\nnginxsite root") {
		t.Errorf("order of calls:\n%s", srv.calls())
	}
	argv := srv.unquoted()
	for _, want := range []string{"certbot_pkgs='certbot python3-certbot-nginx'", "server_name shop.example.com;", "host='shop.example.com'", "listen 443 ssl http2;"} {
		if !strings.Contains(argv, want) {
			t.Errorf("the scripts lack %q", want)
		}
	}
	// The first site has no HTTPS, the last has.
	first := strings.Index(argv, "app='shop'\nconf='")
	if first < 0 || strings.Contains(argv[first:strings.Index(argv[first:], "'\n")+first+1], "443") {
		t.Errorf("the first site:\n%s", argv)
	}
}

// Everything in place: a second run changes nothing, and nothing runs as root but the read.
func TestSetupNginxSecondRunChangesNothing(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: nginx\n", nil)
	srv := newSetupServer(t, "", nginxUserFacts(9527, "shop"))
	srv.nginx(t, nginxFacts(true, []string{"shop shop.example.com"}, siteOf("shop", "shop.example.com", 9527, true)))
	out, err := runSetup(t, root, false, "")
	if err != nil || !strings.Contains(out, "matches tifa.yaml") {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, c := range []string{"nginxinstall", "nginxsite", "nginxcert", "nginxremove", "nginxuninstall", "app tifa", "ufwallow"} {
		if strings.Contains(srv.calls(), c) {
			t.Errorf("ran %s:\n%s", c, srv.calls())
		}
	}

	// The site changed by hand is written again, and the welcome site that came back goes.
	srv.nginx(t, nginxFacts(true, []string{"shop shop.example.com"}, nginxSiteInfo{App: "shop", Sum: "0000", Host: "shop.example.com"})+"default yes\n")
	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "change  nginx: the site of shop.example.com") || !strings.Contains(out, "and the welcome site of nginx goes") || !strings.Contains(srv.calls(), "nginxsite root") {
		t.Errorf("a changed site: %v\n%s", err, out)
	}
}

// A site that is in place, with nginx not running, is not "right": setup starts nginx after nginx -t.
func TestSetupNginxStartsAStoppedNginx(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: nginx\n", nil)
	srv := newSetupServer(t, "", nginxUserFacts(9527, "shop"))
	srv.nginx(t, strings.Replace(nginxFacts(false, nil, siteOf("shop", "", 9527, false)), "active yes\n", "", 1))
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "change  nginx: start nginx, which is installed and not running") || !strings.Contains(srv.calls(), "nginxstart root") {
		t.Fatalf("%v\n%s", err, out)
	}
	srv.nginx(t, nginxFacts(false, nil, siteOf("shop", "", 9527, false)))
	out, err = runSetup(t, root, false, "")
	if err != nil || !strings.Contains(out, "matches tifa.yaml") {
		t.Errorf("running: %v\n%s", err, out)
	}
}

// A certificate that exists, with a site that has no HTTPS yet, is a site to write, not a certificate
// to ask for. A certificate for another host is replaced.
func TestSetupNginxCertificateStates(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: nginx\n", nil)
	srv := newSetupServer(t, "", nginxUserFacts(9527, "shop"))
	srv.nginx(t, nginxFacts(true, []string{"shop shop.example.com"}, siteOf("shop", "shop.example.com", 9527, false)))
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "change  nginx: the site of shop.example.com") || strings.Contains(srv.calls(), "nginxcert") {
		t.Errorf("certificate there: %v\n%s\n%s", err, out, srv.calls())
	}
	if !strings.Contains(srv.unquoted(), "listen 443 ssl http2;") {
		t.Errorf("the site has no HTTPS:\n%s", srv.unquoted())
	}

	// A certificate whose name the server could not read is asked for again, not taken as a match.
	srv3 := newSetupServer(t, "", nginxUserFacts(9527, "shop"))
	srv3.nginx(t, nginxFacts(true, []string{"shop ?"}, siteOf("shop", "shop.example.com", 9527, true)))
	out, err = runSetup(t, root, false, "")
	if err == nil || !strings.Contains(out, "change  server: the certificate of shop.example.com") {
		t.Errorf("unreadable certificate: %v\n%s", err, out)
	}

	srv2 := newSetupServer(t, "", nginxUserFacts(9527, "shop"))
	srv2.nginx(t, nginxFacts(true, []string{"shop old.example.com"}, siteOf("shop", "old.example.com", 9527, true)))
	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "change  server: the certificate of shop.example.com") || !strings.Contains(srv2.unquoted(), "delete_old='1'") {
		t.Errorf("another host: %v\n%s", err, out)
	}
}

// On a server that has no deploy user yet, a port that something listens on is taken before the app
// gets one: the app and the site that points at it both use 9528 when 9527 is busy. And a site
// uses the port the app has once its step runs, not the one the plan guessed.
func TestSetupNginxSiteUsesThePortTheAppGets(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: nginx\n", nil)
	// Once the user exists, its own probe sees the listener too.
	srv := newSetupServer(t, "", freshFacts+"listen LISTEN 0 4096 127.0.0.1:9527 0.0.0.0:*\n")
	srv.deny(t)
	srv.nginx(t, "apt yes\n")
	writeFile(t, filepath.Join(srv.dir, "rootfacts"), "listen LISTEN 0 4096 127.0.0.1:9527 0.0.0.0:* users:((\"mydb\",pid=5,fd=3))\n")
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "port 9528, the systemd socket") || !strings.Contains(out, "127.0.0.1:9528 in /etc/nginx/conf.d/tifa-shop.conf") {
		t.Fatalf("%v\n%s", err, out)
	}
	argv := srv.unquoted()
	if !strings.Contains(argv, "ListenStream=127.0.0.1:9528") || !strings.Contains(argv, "server 127.0.0.1:9528;") || strings.Contains(argv, "9527;") {
		t.Errorf("ports in the scripts:\n%s", argv)
	}

	// The plan said 9527, and the app got 9600 while the steps ran: the site follows.
	srv = newSetupServer(t, "", nginxUserFacts(9527))
	srv.nginx(t, nginxFacts(true, nil))
	tg, err := loadDeployTarget(root, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	plan, run, err := buildSetupPlan(t.Context(), tg, setupFlags{skipMachine: true}, false, strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	run.port = 9600
	for _, st := range plan.steps {
		if st.do != nil && strings.Contains(st.text, "nginx: the site") {
			if _, err := st.do(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !strings.Contains(srv.unquoted(), "server 127.0.0.1:9600;") {
		t.Errorf("the site has the port of the plan:\n%s", srv.unquoted())
	}
}

// A nginx package that did not finish installing, which a failed start leaves, is installed again by
// the next setup: the marker says it is Tifa's.
func TestSetupNginxFinishesABrokenInstall(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: nginx\n", nil)
	srv := newSetupServer(t, "", nginxUserFacts(9527))
	srv.nginx(t, "apt yes\npkg nginx iF\npkg nginx-common ii\nbin /usr/sbin/nginx\ntifa yes\ntifa-nginx nginx\n")
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "add     server: install nginx with apt") || !strings.Contains(srv.calls(), "nginxinstall root\nnginxsite root") {
		t.Errorf("%v\n%s", err, out)
	}
}

// An nginx that setup did not install, or another program on port 80 or 443, stops setup before
// it changes anything, and names what is in the way.
func TestSetupNginxStopsForOthers(t *testing.T) {
	for name, tc := range map[string]struct {
		facts string
		want  []string
	}{
		"hand-installed nginx": {
			"apt yes\npkg nginx ii\npkg nginx-common ii\nbin /usr/sbin/nginx\nactive yes\nlisten LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:((\"nginx\",pid=812,fd=6))\n",
			[]string{"vps has an nginx that Tifa did not install (nginx, nginx-common), which listens on port 80. Tifa never changes the nginx of someone else", "Nothing was changed"},
		},
		"an nginx that is off": {
			"apt yes\npkg nginx ii\nbin /usr/sbin/nginx\n",
			[]string{"an nginx that Tifa did not install (nginx)"},
		},
		"another program": {
			"apt yes\nlisten LISTEN 0 4096 *:443 *:* users:((\"caddy\",pid=9,fd=7))\n",
			[]string{"port 443 of vps is taken by caddy (pid 9). nginx needs ports 80 and 443", "Nothing was changed"},
		},
	} {
		root := setupApp(t, "deploy:\n  front: nginx\n", nil)
		srv := newSetupServer(t, "", factsOf("shop", 9527, "appdir yes\n"))
		srv.nginx(t, tc.facts)
		out, err := runSetup(t, root, false, "", "--yes")
		if err == nil {
			t.Errorf("%s: no error\n%s", name, out)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: %q lacks %q", name, err, w)
			}
		}
		for _, c := range []string{"nginxinstall", "nginxsite", "app tifa", "server root"} {
			if strings.Contains(srv.calls(), c) {
				t.Errorf("%s: ran %s", name, c)
			}
		}
	}
}

// Another app of Tifa that serves visitors itself holds 80, and an app that already has the default
// server of nginx keeps it: setup says which.
func TestSetupNginxStopsForOtherApps(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: nginx\n", nil)
	srv := newSetupServer(t, "", factsOf("shop", 9527, "appdir yes\nusock [::]:80 tifa-notes.socket tifa-notes.service\nlisten LISTEN 0 4096 *:80 *:*\n"))
	srv.nginx(t, "apt yes\nlisten LISTEN 0 4096 *:80 *:*\n")
	_, err := runSetup(t, root, false, "", "--yes")
	if err == nil || !strings.Contains(err.Error(), "port 80 of vps is taken by notes, an app Tifa set up there that serves visitors itself") {
		t.Errorf("another app: %v", err)
	}

	srv = newSetupServer(t, "", factsOf("shop", 9527, "appdir yes\n"))
	srv.nginx(t, nginxFacts(false, nil, siteOf("blog", "", 9528, false)))
	_, err = runSetup(t, root, false, "", "--yes")
	if err == nil || !strings.Contains(err.Error(), "shop has no domain in site.url, and blog on vps already answers the visitors that name none") || strings.Contains(srv.calls(), "nginxsite") {
		t.Errorf("two default servers: %v", err)
	}
}

// A site nginx does not take is an error that carries what nginx said, and setup does not go on to
// ask for a certificate.
func TestSetupNginxFailureSaysWhatNginxSaid(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: nginx\n", nil)
	srv := newSetupServer(t, "", nginxUserFacts(9527))
	srv.nginx(t, nginxFacts(true, nil))
	writeFile(t, filepath.Join(srv.dir, "nginxfail"), "")
	out, err := runSetup(t, root, false, "", "--yes")
	if err == nil || !strings.Contains(err.Error(), "nginx said:") || !strings.Contains(err.Error(), "unknown directive") || strings.Contains(srv.calls(), "nginxcert") {
		t.Errorf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "failed  nginx: the site of shop.example.com") {
		t.Errorf("out:\n%s", out)
	}
}

// ufw on and closed: nginx needs 80 and 443, in rules marked as Tifa's and named for nginx.
func TestSetupNginxOpensUFW(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: nginx\n", nil)
	srv := newSetupServer(t, "", factsOf("shop", 9527, "appdir yes\nufw on\n"))
	srv.nginx(t, "apt yes\n")
	srv.ufw(t, ufwRead(ufwVerbose, ufwAdded))
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "change  firewall: allow ports 80 and 443 (tcp) in ufw, so visitors reach nginx, in rules with the comment 'tifa nginx front', as root") {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(srv.unquoted(), "ufw allow 80/tcp comment 'tifa nginx front'") || !strings.Contains(srv.unquoted(), "ufw allow 443/tcp comment 'tifa nginx front'") {
		t.Errorf("the rules:\n%s", srv.unquoted())
	}
	// Let's Encrypt must reach port 80 when certbot asks, so ufw is opened before the certificate.
	calls := srv.calls()
	if i, j := strings.Index(calls, "ufwallow root"), strings.Index(calls, "nginxcert root"); i < 0 || j < i {
		t.Errorf("the certificate came before the firewall:\n%s", calls)
	}
}

// An app that served visitors itself moves behind nginx: it goes to a loopback port first, which
// frees port 80, its own ufw rules go, and then nginx comes. Its old rules are not nginx's.
func TestSetupMovesFromNoneToNginx(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: nginx\n", nil)
	srv := newSetupServer(t, "", factsOf("shop", httpPort, "lowport 80\nufw on\nappdir yes\nusock [::]:80 tifa-shop.socket tifa-shop.service\nlisten LISTEN 0 4096 *:80 *:*\n"))
	srv.nginx(t, "apt yes\nlisten LISTEN 0 4096 *:80 *:*\n")
	srv.ufw(t, ufwRead(ufwVerbose, ufwAdded+"ufw allow 80/tcp comment 'tifa shop'\n"))
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"change  app: move shop from port 80 to port 9527",
		"add     server: install nginx with apt",
		"change  firewall: allow ports 80 and 443 (tcp) in ufw, so visitors reach nginx, in rules with the comment 'tifa nginx front'",
		"remove  firewall: the rules of ufw for port 80 (tcp) with the comment 'tifa shop', which shop no longer needs",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	calls := srv.calls()
	if !strings.Contains(calls, "app tifa") || strings.Index(calls, "app tifa") > strings.Index(calls, "nginxinstall") {
		t.Errorf("nginx came before the app moved:\n%s", calls)
	}
}

// Behind Cloudflare to nginx: the Cloudflare side goes as it does for any move away. Back from
// nginx to Cloudflare, the last app takes the site, the certificate, nginx and certbot with it.
func TestSetupMovesFromNginxToCloudflareAndNone(t *testing.T) {
	cf := cfSetup(t, "example.com")
	_ = cf
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n", nil)
	srv := newSetupServer(t, "", nginxUserFacts(9527, "shop"))
	srv.nginx(t, nginxFacts(true, []string{"shop shop.example.com"}, siteOf("shop", "shop.example.com", 9527, true)))
	out, err := runSetup(t, root, false, "")
	for _, want := range []string{
		"remove  nginx: the site of shop in /etc/nginx/conf.d/tifa-shop.conf, which tifa wrote, since deploy.front is not nginx any more, and nginx reloads after nginx -t",
		"remove  server: the certificate tifa-shop of shop.example.com, made by certbot, since deploy.front is not nginx any more",
		"remove  server: uninstall nginx and certbot, which setup installed, with their configuration, and delete /etc/tifa/nginx, as root, since no other app uses nginx",
	} {
		if err == nil || !strings.Contains(out, want) {
			t.Errorf("to cloudflare: lacks %q (%v):\n%s", want, err, out)
		}
	}
	if strings.Contains(srv.calls(), "nginxremovesite") {
		t.Errorf("ran without --yes:\n%s", srv.calls())
	}

	// To none, the only app of nginx: the plan removes nginx first, then the app takes port 80.
	root = setupApp(t, "deploy:\n  front: none\n", nil)
	srv = newSetupServer(t, "", nginxUserFacts(9527, "shop"))
	srv.nginx(t, nginxFacts(false, nil, siteOf("shop", "", 9527, false)))
	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("to none: %v\n%s", err, out)
	}
	calls := srv.calls()
	if !strings.Contains(calls, "nginxremovesite root\nnginxuninstall root") || strings.Index(calls, "nginxuninstall") > strings.Index(calls, "app tifa") {
		t.Errorf("order of calls:\n%s", calls)
	}
	if !strings.Contains(out, "remove  server: uninstall nginx, which setup installed") {
		t.Errorf("plan:\n%s", out)
	}
}

// With other apps behind nginx, moving one to front none is refused, since nginx holds the ports.
// Moving one to Cloudflare takes only its own site and certificate.
func TestSetupNginxHoldsThePortsOfOthers(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: none\n", nil)
	srv := newSetupServer(t, "", nginxUserFacts(9527, "shop", "blog"))
	srv.nginx(t, nginxFacts(false, nil, siteOf("shop", "", 9527, false), siteOf("blog", "blog.example.com", 9528, false)))
	_, err := runSetup(t, root, false, "", "--yes")
	if err == nil || !strings.Contains(err.Error(), "port 80 of vps is held by the nginx of Tifa, which serves shop, blog") || !strings.HasSuffix(err.Error(), "Nothing was changed") {
		t.Fatalf("%v", err)
	}
	for _, c := range []string{"nginxremove", "nginxuninstall", "app tifa"} {
		if strings.Contains(srv.calls(), c) {
			t.Errorf("ran %s:\n%s", c, srv.calls())
		}
	}

	cfSetup(t, "example.com")
	root = setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n", nil)
	srv = newSetupServer(t, "", nginxUserFacts(9527, "shop", "blog"))
	srv.nginx(t, nginxFacts(false, nil, siteOf("shop", "", 9527, false), siteOf("blog", "blog.example.com", 9528, false)))
	out, _ := runSetup(t, root, false, "")
	if !strings.Contains(out, "remove  nginx: the site of shop") || strings.Contains(out, "uninstall nginx") {
		t.Errorf("to cloudflare with another app:\n%s", out)
	}
}

// Remove takes the site and the certificate of the app, and nginx with certbot when it is the last.
// With another app behind nginx the site goes and nginx stays.
func TestDeployRemoveNginx(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: nginx\n", nil)
	srv := newSetupServer(t, "", nginxUserFacts(9527, "shop"))
	srv.nginx(t, nginxFacts(true, []string{"shop shop.example.com"}, siteOf("shop", "shop.example.com", 9527, true)))
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"remove  nginx: the site of shop in /etc/nginx/conf.d/tifa-shop.conf, which tifa wrote, and nginx reloads after nginx -t",
		"remove  server: the certificate tifa-shop of shop.example.com, made by certbot",
		"remove  server: uninstall nginx and certbot, which setup installed, with their configuration, and delete /etc/tifa/nginx, as root, since no other app uses nginx",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(srv.calls(), "nginxremovesite root\nnginxremovecert root\nnginxuninstall root") {
		t.Errorf("order of calls:\n%s", srv.calls())
	}

	srv = newSetupServer(t, "", nginxUserFacts(9527, "shop", "blog"))
	srv.nginx(t, nginxFacts(true, []string{"shop shop.example.com"}, siteOf("shop", "shop.example.com", 9527, true), siteOf("blog", "", 9528, false)))
	out, _, err = runRemove(t, root, false, "", "--yes")
	if err != nil || strings.Contains(out, "uninstall") || !strings.Contains(srv.calls(), "nginxremovesite root\nnginxremovecert root\n") || strings.Contains(srv.calls(), "nginxuninstall") {
		t.Errorf("another app stays: %v\n%s\n%s", err, out, srv.calls())
	}
}

// The ufw rules of the nginx of Tifa carry a comment that no app can have: "tifa <app>" has one word
// after tifa, so an app called nginx has "tifa nginx", and the shared rules say "tifa nginx front".
func TestNginxUFWCommentsCannotMeetAnApp(t *testing.T) {
	if ufwNginxComment == ufwComment("nginx") || len(strings.Fields(ufwNginxComment)) == 2 {
		t.Errorf("%q can be the comment of an app", ufwNginxComment)
	}
	for _, app := range []string{"nginx", "front", "nginx-front", "a.b_c"} {
		if _, err := deployAppName("example.com/" + app); err != nil {
			t.Fatal(err)
		}
		if ufwComment(app) == ufwNginxComment {
			t.Errorf("the app %s has the comment of nginx", app)
		}
	}
	if _, err := deployAppName("example.com/nginx front"); err == nil {
		t.Error("an app name with a space")
	}
}

// A server set up by an earlier version has the rules of nginx with the comment "tifa nginx". Setup
// takes them over: it allows the same ports with the new comment, which ufw writes over the old one,
// and deletes nothing, so the ports stay open all the time. When the last app goes, the new rules go.
func TestSetupNginxTakesOverTheOldUFWComment(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: nginx\n", nil)
	srv := newSetupServer(t, "", nginxUserFacts(9527, "shop")+"ufw on\n")
	srv.nginx(t, nginxFacts(false, nil, siteOf("shop", "", 9527, false)))
	srv.ufw(t, ufwRead(ufwVerbose, ufwAdded+"ufw allow 80/tcp comment 'tifa nginx'\nufw allow 443/tcp comment 'tifa nginx'\n"))
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "firewall: allow ports 80 and 443 (tcp) in ufw, so visitors reach nginx, in rules with the comment 'tifa nginx front'") || strings.Contains(out, "remove  firewall") {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(srv.unquoted(), "ufw allow 80/tcp comment 'tifa nginx front'") || strings.Contains(srv.unquoted(), "delete allow") {
		t.Errorf("the rules:\n%s", srv.unquoted())
	}

	// Removing the last app deletes the rules of either comment.
	srv = newSetupServer(t, "", nginxUserFacts(9527, "shop")+"ufw on\n")
	srv.nginx(t, nginxFacts(false, nil, siteOf("shop", "", 9527, false)))
	srv.ufw(t, ufwRead(ufwVerbose, ufwAdded+"ufw allow 80/tcp comment 'tifa nginx'\nufw allow 443/tcp comment 'tifa nginx front'\n"))
	out, _, err = runRemove(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(srv.unquoted(), "ufw delete allow 80/tcp comment 'tifa nginx'") || !strings.Contains(srv.unquoted(), "ufw delete allow 443/tcp comment 'tifa nginx front'") {
		t.Errorf("%v\n%s\n%s", err, out, srv.unquoted())
	}
}

// An app called nginx owns the comment "tifa nginx": the shared rules never take it over or delete it.
func TestSetupNginxLeavesTheRulesOfAnAppCalledNginx(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: nginx\n", nil)
	srv := newSetupServer(t, "", nginxUserFacts(9527, "shop")+"other nginx\nufw on\n")
	srv.nginx(t, nginxFacts(false, nil, siteOf("shop", "", 9527, false)))
	srv.ufw(t, ufwRead(ufwVerbose, ufwAdded+"ufw allow 8443/tcp comment 'tifa nginx'\n"))
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil || strings.Contains(out, "'tifa nginx', which") || strings.Contains(srv.unquoted(), "delete allow") {
		t.Errorf("%v\n%s\n%s", err, out, srv.unquoted())
	}
}

// A site that nginx would not let go stays, and then nothing else of it goes: its certificate, which the
// configuration on disk still names, nginx itself and its ufw rules stay too, so nginx restarts in the
// state it was in. The failure is the error, and the run again finishes the job.
func TestDeployRemoveNginxKeepsWhatTheSiteNeeds(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: nginx\n", nil)
	srv := newSetupServer(t, "", nginxUserFacts(9527, "shop")+"ufw on\n")
	srv.nginx(t, nginxFacts(true, []string{"shop shop.example.com"}, siteOf("shop", "shop.example.com", 9527, true)))
	srv.ufw(t, ufwRead(ufwVerbose, ufwAdded+"ufw allow 80/tcp comment 'tifa nginx front'\nufw allow 443/tcp comment 'tifa nginx front'\n"))
	writeFile(t, filepath.Join(srv.dir, "removesitefail"), "")
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err == nil || !strings.Contains(out, "warning nginx: the site of shop") || !strings.Contains(out, "boom") {
		t.Fatalf("%v\n%s", err, out)
	}
	calls := srv.calls()
	for _, c := range []string{"nginxremovecert", "nginxuninstall", "ufwdelete"} {
		if strings.Contains(calls, c) {
			t.Errorf("ran %s after the site stayed:\n%s", c, calls)
		}
	}
	if !strings.Contains(out, "kept, since the site of shop is still in nginx") {
		t.Errorf("out:\n%s", out)
	}
	// The app itself still goes: it does not depend on nginx.
	if !strings.Contains(calls, "removeapp tifa") {
		t.Errorf("the app stayed:\n%s", calls)
	}
}

// An app that appeared while the last one was removed keeps nginx, and the ports of nginx in ufw.
func TestDeployRemoveNginxKeepsPortsForANewSite(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: nginx\n", nil)
	srv := newSetupServer(t, "", nginxUserFacts(9527, "shop")+"ufw on\n")
	srv.nginx(t, nginxFacts(false, nil, siteOf("shop", "", 9527, false)))
	srv.ufw(t, ufwRead(ufwVerbose, ufwAdded+"ufw allow 80/tcp comment 'tifa nginx front'\nufw allow 443/tcp comment 'tifa nginx front'\n"))
	writeFile(t, filepath.Join(srv.dir, "uninstallkept"), "")
	out, _, err := runRemove(t, root, false, "", "--yes")
	if strings.Contains(srv.calls(), "ufwdelete") || !strings.Contains(out, "nginx has a site again") {
		t.Errorf("%v\n%s\n%s", err, out, srv.calls())
	}
}

// The ufw rules of nginx go with nginx and no other rule does.
func TestDeployRemoveNginxDeletesItsUFWRules(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: nginx\n", nil)
	srv := newSetupServer(t, "", nginxUserFacts(9527, "shop"))
	srv.nginx(t, nginxFacts(false, nil, siteOf("shop", "", 9527, false)))
	srv.ufw(t, ufwRead(ufwVerbose, ufwAdded+"ufw allow 80/tcp comment 'tifa nginx front'\nufw allow 443/tcp comment 'tifa nginx front'\nufw allow 80/tcp comment 'tifa other'\n"))
	setFacts(t, srv, nginxUserFacts(9527, "shop")+"ufw on\n")
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "remove  firewall: the rules of ufw for ports 80 and 443 (tcp) with the comment 'tifa nginx front', which nginx no longer needs, as root") {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(srv.unquoted(), "ufw delete allow 80/tcp comment 'tifa nginx front'") || strings.Contains(srv.unquoted(), "tifa other") {
		t.Errorf("rules:\n%s", srv.unquoted())
	}
}

// The tunnel script leaves the marker of nginx where it is: it deletes the two tunnel files and the
// folder only when nothing else is in it.
func TestUntunnelKeepsTheNginxMarker(t *testing.T) {
	if strings.Contains(untunnelBody, "rm -rf /etc/tifa") || !strings.Contains(untunnelBody, "rm -f /etc/tifa/tunnel /etc/tifa/tunnel-token") || !strings.Contains(untunnelBody, "rmdir /etc/tifa") {
		t.Errorf("untunnel:\n%s", untunnelBody)
	}
}

// A deploy of an app behind nginx that has no site yet says to run setup, and with a site says where
// visitors reach it.
func TestDeployNginxPrintsVisitors(t *testing.T) {
	newSetupServer(t, "hostname 203.0.113.7\n", "")
	var out strings.Builder
	t1 := deployTarget{app: "shop", host: "vps", front: "nginx", own: "shop.example.com", dest: deployHost{name: "203.0.113.7"}}
	t1.printVisitors(t.Context(), &out, serverState{Port: 9527})
	if want := "visitors: none yet. shop listens on 127.0.0.1:9527, which only the server reaches: run go tool tifa deploy setup, and nginx serves it\n"; out.String() != want {
		t.Errorf("no site: %q", out.String())
	}
	out.Reset()
	old := portAnswers
	t.Cleanup(func() { portAnswers = old })
	portAnswers = func(_ context.Context, host string, port int) bool { return port == 80 }
	t1.printVisitors(t.Context(), &out, serverState{Port: 9527, NginxSites: []string{"shop"}})
	if !strings.Contains(out.String(), "visitors: https://shop.example.com, once ports 80 and 443 are open to them. Port 443 did not answer") {
		t.Errorf("site: %q", out.String())
	}
}
