package main

// deploy.front nginx: an nginx that tifa deploy setup installs and owns routes the visitors of
// every app of the server that has this front, by the host of its site.url. Each app runs on a
// loopback port, as it does behind the Cloudflare Tunnel.
//
// What is Tifa's on the server:
//
//	/etc/tifa/nginx                       the marker: setup installed nginx, and the packages apt added
//	/etc/nginx/conf.d/tifa-<app>.conf     the site of an app: the only configuration of this nginx
//	/etc/letsencrypt/                      certbot, which gets and renews the certificates (tifa-<app>)
//
// An nginx without the marker is not Tifa's: setup stops and changes nothing.

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// policyFile is where a program of the system says which services may start while apt installs.
// Tests point it at a file of their own.
var policyFile = "/usr/sbin/policy-rc.d"

// etcDir is /etc on the server. The scripts take it as a variable so a test runs them in a folder.
var etcDir = "/etc"

// nginxKeepalive is how many idle connections nginx keeps open to each app. The program closes an
// idle connection after 60 seconds (server.go, idleTimeout), so nginx gives up its own before that:
// a request never goes down a connection the program is closing.
const nginxKeepalive = "keepalive 16;\n\tkeepalive_timeout 50s;"

// nginxHostMarker is the comment in a site file that says which host it serves, "-" for none. Setup
// reads it back to find two apps that want the same name.
const nginxHostMarker = "# tifa:host "

// nginxLocation is how a site passes a request to its app. The comments say why each line is there.
//
//   - Host goes through as the browser sent it, and Origin passes by default, so the cross-site
//     check of the program sees the same names as the browser.
//   - X-Forwarded-For is the address of the visitor, set and not appended to, so a visitor cannot
//     make one up.
//   - The timeouts are a day: the program's own read and write timeouts, which a handler may lift
//     for a stream or an upload, decide when a request ends.
//   - A body is passed on as it arrives, with no size limit of nginx: the body limit of the program,
//     which a route may raise, answers 413. Nginx writes nothing to its disk for it.
//   - gzip off: the program compresses what it chooses to, and a program that sends
//     No-Gzip-Compression (against BREACH) must not be compressed by nginx instead. nginx leaves an
//     answer that has a Content-Encoding alone either way.
//   - X-Accel-Buffering: no of an answer, such as server-sent events, is honoured: nginx does that
//     unless told to ignore it.
const nginxLocation = `	location / {
		proxy_pass http://%s;
		proxy_http_version 1.1;
		proxy_set_header Connection "";
		proxy_set_header Host $http_host;
		proxy_set_header X-Forwarded-For $remote_addr;
		proxy_read_timeout 1d;
		proxy_send_timeout 1d;
		proxy_request_buffering off;
		client_max_body_size 0;
		gzip off;
	}
`

// nginxUpstream is the name of the upstream of an app.
func nginxUpstream(app string) string { return "tifa-" + app }

// nginxSiteFile is the site of app: the host it answers to ("" for visitors that name no domain,
// which makes it the default server of port 80), the loopback port of the app, and with tls the
// HTTPS server for host, whose certificate certbot made as tifa-<app>. Without tls a domain is
// served over plain HTTP, which is what certbot needs to get the certificate.
//
// ipv6 says the server has IPv6: without it nginx cannot open a socket for [::] and refuses to start,
// so the listen lines for it are left out.
func nginxSiteFile(app, host string, port int, tls, ipv6 bool) string {
	up := nginxUpstream(app)
	listen := func(rest string) string {
		l := "\tlisten " + rest + ";\n"
		if ipv6 {
			l += "\tlisten [::]:" + rest + ";\n"
		}
		return l
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Written by tifa deploy setup for the app %s, which writes it again when it differs.\n", app)
	fmt.Fprintf(&b, "%s%s\n", nginxHostMarker, pick(host == "", "-", host))
	fmt.Fprintf(&b, "upstream %s {\n\tserver 127.0.0.1:%d;\n\t%s\n}\n", up, port, nginxKeepalive)
	location := fmt.Sprintf(nginxLocation, up)
	switch {
	case host == "":
		fmt.Fprintf(&b, "server {\n%s\tserver_name _;\n%s}\n", listen("80 default_server"), location)
	case !tls:
		fmt.Fprintf(&b, "server {\n%s\tserver_name %s;\n%s}\n", listen("80"), host, location)
	default:
		// The redirect sits in a location, so the more exact location certbot adds for its
		// challenge on renewal wins over it. It names the host and not $host, so a request that
		// reaches this server by another name, such as the address, is sent to the real one.
		fmt.Fprintf(&b, "server {\n%s\tserver_name %s;\n\tlocation / {\n\t\treturn 308 https://%s$request_uri;\n\t}\n}\n", listen("80"), host, host)
		fmt.Fprintf(&b, "server {\n%s\tserver_name %s;\n"+
			"\tssl_certificate %s/letsencrypt/live/tifa-%s/fullchain.pem;\n\tssl_certificate_key %s/letsencrypt/live/tifa-%s/privkey.pem;\n"+
			"\tssl_protocols TLSv1.2 TLSv1.3;\n%s}\n", listen("443 ssl http2"), host, etcDir, app, etcDir, app, location)
	}
	return b.String()
}

// nginxState is what the root probe says about nginx, certbot and the sites of Tifa.
type nginxState struct {
	// Apt says the server has apt. Pkgs are the nginx packages dpkg knows (installed, or with
	// their configuration left), Bin where an nginx program is.
	Apt  bool
	Pkgs []string
	// Broken are the packages of Pkgs that are not installed cleanly, such as one whose start failed.
	Broken []string
	Bin    string
	// IPv6 says the server has IPv6.
	IPv6 bool
	// Tifa says the marker is there: setup installed this nginx. TifaPkgs and TifaCertbot are the
	// packages apt added for nginx and for certbot, as the marker records them.
	Tifa                bool
	TifaPkgs, TifaCerts []string
	// Certbot is where certbot is, Plugin says it has the nginx plugin.
	Certbot string
	Plugin  bool
	// BrokenCertbot are the packages of certbot and its plugin that dpkg has but did not install cleanly.
	BrokenCertbot []string
	// Default says the welcome site of the package is on. Active says nginx runs.
	Default, Active bool
	// Sites are the site files of Tifa: the app, the hash of its file and its host, "-" for none.
	Sites []nginxSiteInfo
	// Certs are the certificates of Tifa: the app and the name the certificate is for, "?" when the
	// server could not read it.
	Certs  []nginxCert
	Listen []listener
}

type nginxSiteInfo struct{ App, Sum, Host string }
type nginxCert struct{ App, Host string }

// installed says nginx is on the server, whoever put it there.
func (s nginxState) installed() bool { return len(s.Pkgs) > 0 || s.Bin != "" }

// foreign says the server has an nginx that Tifa did not install.
func (s nginxState) foreign() bool { return s.installed() && !s.Tifa }

func (s nginxState) site(app string) (nginxSiteInfo, bool) {
	for _, x := range s.Sites {
		if x.App == app {
			return x, true
		}
	}
	return nginxSiteInfo{}, false
}

func (s nginxState) cert(app string) (nginxCert, bool) {
	for _, x := range s.Certs {
		if x.App == app {
			return x, true
		}
	}
	return nginxCert{}, false
}

// otherSites are the sites of apps other than app.
func (s nginxState) otherSites(app string) []nginxSiteInfo {
	var out []nginxSiteInfo
	for _, x := range s.Sites {
		if x.App != app {
			out = append(out, x)
		}
	}
	return out
}

// nginxPrograms says an nginx listens on port.
func (s nginxState) nginxPrograms(port int) bool {
	for _, l := range s.Listen {
		if l.Port == port && slices.ContainsFunc(l.Programs, func(p string) bool { return strings.HasPrefix(p, "nginx ") }) {
			return true
		}
	}
	return false
}

// parseNginxState reads the output of nginxProbeBody: a word and a value on each line.
func parseNginxState(out string) (nginxState, error) {
	var s nginxState
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		word, value, _ := strings.Cut(line, " ")
		value = strings.TrimSpace(value)
		switch word {
		case "apt":
			s.Apt = true
		case "pkg":
			name, status, _ := strings.Cut(value, " ")
			s.Pkgs = append(s.Pkgs, name)
			if status != "ii" {
				s.Broken = append(s.Broken, name)
			}
		case "ipv6":
			s.IPv6 = true
		case "bin":
			s.Bin = value
		case "tifa":
			s.Tifa = true
		case "tifa-nginx":
			s.TifaPkgs = append(s.TifaPkgs, value)
		case "tifa-certbot":
			s.TifaCerts = append(s.TifaCerts, value)
		case "certbot":
			s.Certbot = value
		case "plugin":
			s.Plugin = true
		case "certbot-broken":
			s.BrokenCertbot = append(s.BrokenCertbot, value)
		case "default":
			s.Default = true
		case "active":
			s.Active = true
		case "site":
			f := strings.Fields(value)
			if len(f) != 3 {
				return s, fmt.Errorf("the server gave an nginx site tifa cannot read: %q", line)
			}
			s.Sites = append(s.Sites, nginxSiteInfo{App: f[0], Sum: f[1], Host: f[2]})
		case "cert":
			f := strings.Fields(value)
			if len(f) != 2 {
				return s, fmt.Errorf("the server gave a certificate tifa cannot read: %q", line)
			}
			s.Certs = append(s.Certs, nginxCert{App: f[0], Host: f[1]})
		case "listen":
			if l, ok := parseListen(value); ok {
				s.Listen = append(s.Listen, l)
			}
		default:
			return s, fmt.Errorf("the server printed a line tifa cannot read: %q", line)
		}
	}
	return s, nil
}

// nginxProbeBody runs as root. It says what nginx and certbot the server has and whether Tifa
// installed them, the sites and certificates of Tifa, and who listens on the ports. It changes
// nothing and never fails. $etc is /etc.
const nginxProbeBody = `# tifa: read nginx
export PATH="$PATH:/usr/sbin:/usr/local/sbin:/sbin"
if command -v apt-get >/dev/null 2>&1; then echo "apt yes"; fi
if command -v dpkg-query >/dev/null 2>&1; then
	dpkg-query -W -f='${db:Status-Abbrev} ${Package}\n' 'nginx' 'nginx-*' 2>/dev/null | while read -r st rest; do
		case "$st" in ii|rc|hi|iU|iF|iH|iW|it|ip) ;; *) continue ;; esac
		echo "pkg $rest $st"
	done
fi
if [ -e "$inet6_file" ]; then echo "ipv6 yes"; fi
if b=$(command -v nginx); then echo "bin $b"; fi
m="$etc/tifa/nginx"
if [ -f "$m" ]; then
	echo "tifa yes"
	awk '$1 == "installed" && $2 == "nginx" { print "tifa-nginx " $3 } $1 == "installed" && $2 == "certbot" { print "tifa-certbot " $3 }' "$m"
fi
if command -v dpkg-query >/dev/null 2>&1; then
	dpkg-query -W -f='${db:Status-Abbrev} ${Package}\n' certbot python3-certbot-nginx 2>/dev/null | while read -r st rest; do
		case "$st" in ii|un|"") ;; *) echo "certbot-broken $rest" ;; esac
	done
fi
if c=$(command -v certbot); then
	echo "certbot $c"
	if certbot plugins 2>/dev/null | grep -q nginx; then echo "plugin yes"; fi
fi
if [ -e "$etc/nginx/sites-enabled/default" ]; then echo "default yes"; fi
if systemctl is-active --quiet nginx 2>/dev/null; then echo "active yes"; fi
for f in "$etc"/nginx/conf.d/tifa-*.conf; do
	[ -f "$f" ] || continue
	a=$(basename "$f" .conf); a=${a#tifa-}
	h=$(sed -n 's/^` + nginxHostMarker + `//p' "$f" | head -n 1)
	echo "site $a $(sha256sum < "$f" | cut -d' ' -f1) ${h:--}"
done
for f in "$etc"/letsencrypt/renewal/tifa-*.conf; do
	[ -f "$f" ] || continue
	a=$(basename "$f" .conf); a=${a#tifa-}
	h=""
	if command -v openssl >/dev/null 2>&1; then
		h=$(openssl x509 -noout -ext subjectAltName -in "$etc/letsencrypt/live/tifa-$a/cert.pem" 2>/dev/null | grep -o 'DNS:[^, ]*' | head -n 1 | cut -c5-)
	fi
	echo "cert $a ${h:-?}"
done
ss -Hltnp 2>/dev/null | sed 's/^/listen /'
exit 0
`

// inet6File exists when the server has IPv6. Tests point it at a file of their own.
var inet6File = "/proc/net/if_inet6"

func nginxProbeScript() string { return script(nginxProbeBody, "etc", etcDir, "inet6_file", inet6File) }

// nginxInstallBody runs as root. It writes the marker first, so a run that stops halfway still
// knows this nginx is Tifa's, then records the packages apt would add for $nginx_pkgs and
// $certbot_pkgs (each a list, maybe empty), installs them, and takes down the welcome site of nginx.
const nginxInstallBody = `set -eu
# tifa: install nginx
export DEBIAN_FRONTEND=noninteractive NEEDRESTART_SUSPEND=1
umask 022
marker="$etc/tifa/nginx"
install -d -m 755 "$etc/tifa"
if [ ! -f "$marker" ]; then
	printf '%s\n' "# Written by tifa deploy setup, which installed nginx here for the Tifa apps." \
		"# The sites in $etc/nginx/conf.d/tifa-*.conf are its only configuration. Without this file" \
		"# setup takes nginx for one of yours and stops. tifa deploy remove of the last app deletes it." > "$marker"
fi
# apt-get runs quietly, and what it said comes back when it fails.
log=$(mktemp)
policy_made=""
trap 'rm -f "$log" "$policy_made"' EXIT
apt-get update -q </dev/null >"$log" 2>&1 || { tail -n 15 "$log" >&2; exit 1; }
# mark records what apt would add, so remove purges those packages and nothing else. An Inst line
# of a package that is already there reads "Inst name [old version] (new version ...)": an
# upgrade, which is not ours, so only the lines with no old version count.
mark() {
	kind=$1
	shift
	if [ "$#" -eq 0 ]; then return 0; fi
	apt-get install -s -y -q "$@" </dev/null | sed -n 's/^Inst \([^ ]*\) (.*/\1/p' | while read -r p; do
		grep -qx "installed $kind $p" "$marker" || echo "installed $kind $p" >> "$marker"
	done
}
mark nginx $nginx_pkgs
mark certbot $certbot_pkgs
# The package starts nginx with its welcome site, which fails on a server without IPv6 and holds
# port 80 for a moment on any other. Until the site of an app is in, nginx is not started.
if [ ! -e "$policy_file" ]; then
	printf '#!/bin/sh\nexit 101\n' > "$policy_file"
	chmod 755 "$policy_file"
	policy_made=$policy_file
fi
apt-get install -y -q -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold $nginx_pkgs $certbot_pkgs </dev/null >"$log" 2>&1 || { tail -n 15 "$log" >&2; exit 1; }
rm -f "$policy_made" "$etc/nginx/sites-enabled/default"
if [ -n "$nginx_pkgs" ]; then systemctl enable --quiet nginx; fi
# certbot renews with its own timer, which the block above kept from starting with the package.
# A certbot that was there before is not ours to start.
case " $certbot_pkgs " in *" certbot "*) systemctl enable --quiet --now certbot.timer ;; esac
echo "installed $(echo $nginx_pkgs $certbot_pkgs)"
`

func nginxInstallScript(nginxPkgs, certbotPkgs []string) string {
	return script(nginxInstallBody, "etc", etcDir, "policy_file", policyFile, "nginx_pkgs", strings.Join(nginxPkgs, " "), "certbot_pkgs", strings.Join(certbotPkgs, " "))
}

// nginxReload is the shell that makes nginx read its configuration: a reload keeps every
// connection, and a stopped nginx is started.
const nginxReload = `if systemctl is-active --quiet nginx; then systemctl reload nginx; else systemctl start nginx; fi`

// nginxSiteBody runs as root. It puts the site in $conf in place, checks it with nginx -t and only
// then reloads nginx. When nginx does not take it, the file that was there before is back (or none),
// and what nginx said is the error. Nothing but a reload ever touches the connections of the other
// apps, and a bad file never reaches them.
const nginxSiteBody = `set -eu
# tifa: nginx site
f="$etc/nginx/conf.d/tifa-$app.conf"
install -d -m 755 "$etc/nginx/conf.d"
had=""
if [ -f "$f" ]; then cp -p "$f" "$f.last"; had=1; fi
printf '%s' "$conf" > "$f.new"
mv "$f.new" "$f"
rm -f "$etc/nginx/sites-enabled/default"
if out=$(nginx -t 2>&1); then
	` + nginxReload + `
	rm -f "$f.last"
	echo "wrote $f, and nginx reloaded"
else
	if [ -n "$had" ]; then mv "$f.last" "$f"; else rm -f "$f"; fi
	echo "tifa: nginx did not accept the site of $app, so the file before it is back. nginx said:" >&2
	printf '%s\n' "$out" >&2
	exit 1
fi
`

func nginxSiteScript(app, conf string) string {
	return script(nginxSiteBody, "etc", etcDir, "app", app, "conf", conf)
}

// nginxCertBody runs as root. It gets the certificate of $host with certbot and its nginx plugin,
// as the name tifa-$app, which certbot renews with its own timer and which reloads nginx on each
// renewal. The plugin only answers the challenge: it is not the installer, so certbot never
// rewrites the site, which is Tifa's file. $delete_old deletes a certificate of that name for another host first.
const nginxCertBody = `set -eu
# tifa: nginx certificate
if [ -n "$delete_old" ]; then certbot delete --non-interactive --cert-name "tifa-$app" >/dev/null 2>&1 || true; fi
if ! out=$(certbot certonly --authenticator nginx --non-interactive --agree-tos --register-unsafely-without-email --cert-name "tifa-$app" -d "$host" --deploy-hook 'systemctl reload nginx' 2>&1); then
	echo "tifa: certbot could not get a certificate for $host. DNS must point $host at this server, and port 80 must be open to the internet. certbot said:" >&2
	printf '%s\n' "$out" | tail -n 15 >&2
	exit 1
fi
echo "certificate for $host from Let's Encrypt"
`

func nginxCertScript(app, host string, deleteOld bool) string {
	return script(nginxCertBody, "app", app, "host", host, "delete_old", pick(deleteOld, "1", ""))
}

// nginxRemoveSiteBody runs as root. It takes the site of $app out of nginx the way nginxSiteBody
// puts one in: nginx -t first, then the reload, and the file back when nginx says no.
const nginxRemoveSiteBody = `set -eu
# tifa: nginx remove site
f="$etc/nginx/conf.d/tifa-$app.conf"
if [ ! -f "$f" ]; then exit 0; fi
mv "$f" "$f.removed"
if out=$(nginx -t 2>&1); then
	` + nginxReload + `
	rm -f "$f.removed"
	echo "deleted $f, and nginx reloaded"
else
	mv "$f.removed" "$f"
	echo "tifa: nginx did not accept the configuration without the site of $app, so the site is back. nginx said:" >&2
	printf '%s\n' "$out" >&2
	exit 1
fi
`

func nginxRemoveSiteScript(app string) string {
	return script(nginxRemoveSiteBody, "etc", etcDir, "app", app)
}

// nginxRemoveCertBody runs as root and deletes the certificate tifa-$app with certbot.
const nginxRemoveCertBody = `set -eu
# tifa: nginx remove certificate
certbot delete --non-interactive --cert-name "tifa-$app" >/dev/null
echo "deleted the certificate tifa-$app"
`

func nginxRemoveCertScript(app string) string { return script(nginxRemoveCertBody, "app", app) }

// nginxUninstallBody runs as root once no site of Tifa is left. It purges the packages the marker
// names, which setup installed, with the configuration of nginx, and of certbot when setup
// installed that. The marker goes last: a run that stopped halfway finds it again.
const nginxUninstallBody = `set -eu
# tifa: nginx uninstall
export DEBIAN_FRONTEND=noninteractive NEEDRESTART_SUSPEND=1
marker="$etc/tifa/nginx"
if [ ! -f "$marker" ]; then exit 0; fi
nginx_pkgs=$(awk '$1 == "installed" && $2 == "nginx" { print $3 }' "$marker")
certbot_pkgs=$(awk '$1 == "installed" && $2 == "certbot" { print $3 }' "$marker")
# Only a certbot that setup installed is ours: its timer, its folder and its account go. When setup
# added only the nginx plugin to the certbot of someone else, the plugin is all that goes.
certbot_own=$(awk '$1 == "installed" && $2 == "certbot" && $3 == "certbot" { print $3 }' "$marker")
if [ -n "$nginx_pkgs" ]; then systemctl disable --now nginx 2>/dev/null || true; fi
if [ -n "$certbot_own" ]; then systemctl disable --now certbot.timer 2>/dev/null || true; fi
if [ -n "$nginx_pkgs$certbot_pkgs" ]; then
	log=$(mktemp)
	apt-get purge -y -q $nginx_pkgs $certbot_pkgs </dev/null >"$log" 2>&1 || { tail -n 15 "$log" >&2; rm -f "$log"; exit 1; }
	rm -f "$log"
fi
if [ -n "$nginx_pkgs" ]; then rm -rf "$etc/nginx"; fi
if [ -n "$certbot_own" ]; then rm -rf "$etc/letsencrypt" /var/lib/letsencrypt /var/log/letsencrypt; fi
rm -f "$marker"
rmdir "$etc/tifa" 2>/dev/null || true
echo "uninstalled $(echo $nginx_pkgs $certbot_pkgs)"
`

func nginxUninstallScript() string { return script(nginxUninstallBody, "etc", etcDir) }

// nginxGuard is the check that comes before anything changes: the server has no nginx that Tifa
// did not install, nothing but Tifa's nginx holds ports 80 and 443, and no other app of the nginx
// wants the host or the default server of this one. host is the domain of the app, "" for none.
// userSockets are the sockets of the apps that serve visitors themselves, which a probe of the
// deploy user lists.
func nginxGuard(t deployTarget, ns nginxState, userSockets []userSocket) error {
	if ns.foreign() {
		what := strings.Join(ns.Pkgs, ", ")
		if what == "" {
			what = ns.Bin
		}
		var ports []int
		for _, port := range []int{httpPort, httpsPort} {
			if ns.nginxPrograms(port) {
				ports = append(ports, port)
			}
		}
		listens := ""
		if len(ports) > 0 {
			listens = ", which listens on " + portList(ports)
		}
		return fmt.Errorf("%s has an nginx that Tifa did not install (%s)%s. Tifa never changes the nginx of someone else, so deploy.front: nginx cannot use it. Remove that nginx, or set deploy.front to cloudflare in tifa.yaml. Nothing was changed", t.host, what, listens)
	}
	for _, port := range []int{httpPort, httpsPort} {
		for _, u := range userSockets {
			if u.Port != port || u.Service == unitName(t.app)+".service" {
				continue
			}
			name := strings.TrimSuffix(strings.TrimPrefix(u.Service, "tifa-"), ".service")
			return fmt.Errorf("port %d of %s is taken by %s, an app Tifa set up there that serves visitors itself. nginx needs ports 80 and 443: remove %s from the server (go tool tifa deploy remove in its folder), or set deploy.front: nginx for it too. Nothing was changed", port, t.host, name, name)
		}
		for _, l := range ns.Listen {
			if l.Port != port || (ns.Tifa && ns.nginxPrograms(port)) || slices.ContainsFunc(userSockets, func(u userSocket) bool { return u.Port == port }) {
				continue
			}
			who := "a program"
			if len(l.Programs) > 0 {
				who = strings.Join(l.Programs, ", ")
			}
			return fmt.Errorf("port %d of %s is taken by %s. nginx needs ports 80 and 443, and only one program can listen on a port: stop it on the server and keep it from starting at boot, or set deploy.front to cloudflare in tifa.yaml. Nothing was changed", port, t.host, who)
		}
	}
	for _, o := range ns.otherSites(t.app) {
		switch {
		case t.own == "" && o.Host == "-":
			return fmt.Errorf("%s has no domain in site.url, and %s on %s already answers the visitors that name none: nginx can send those to one app only. Give %s a domain in site.url, or %s one. Nothing was changed", t.app, o.App, t.host, t.app, o.App)
		case t.own != "" && o.Host == t.own:
			return fmt.Errorf("%s on %s already answers %s through nginx: two apps cannot have one host name. Change site.url, or remove %s. Nothing was changed", o.App, t.host, t.own, o.App)
		}
	}
	return nil
}

var errNoAptForNginx = errors.New("the server has no apt: install nginx from the packages of your system, or set deploy.front to cloudflare")

// nginxPackages are the packages setup installs: nginx when it is not there, and for an app with a
// domain certbot and its nginx plugin, each only when missing.
func nginxPackages(ns nginxState, domain bool) (nginxPkgs, certbotPkgs []string) {
	if !slices.Contains(ns.Pkgs, "nginx") || slices.Contains(ns.Broken, "nginx") || !ns.Tifa {
		nginxPkgs = []string{"nginx"}
	}
	if domain {
		if ns.Certbot == "" || slices.Contains(ns.BrokenCertbot, "certbot") {
			certbotPkgs = append(certbotPkgs, "certbot")
		}
		if !ns.Plugin || slices.Contains(ns.BrokenCertbot, "python3-certbot-nginx") {
			certbotPkgs = append(certbotPkgs, "python3-certbot-nginx")
		}
	}
	return
}
