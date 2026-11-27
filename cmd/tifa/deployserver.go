package main

// What tifa deploy runs on the server, and the choices it makes from what the server says.
//
// The layout on the server, under the home of the deploy user (/srv/tifa when setup creates it):
//
//	<app>/port              the port visitors reach the app on: 80, or 443 with HTTPS, when it
//	                        serves them itself, else the loopback port behind the Cloudflare Tunnel
//	<app>/https             with HTTPS, the domain the app gets its certificate for
//	<app>/certs/            with HTTPS, the certificates from Let's Encrypt (server.tls_cache)
//	<app>/releases/<id>/<app>  each uploaded program, <id> the UTC time of the deploy
//	<app>/current           link to releases/<id>, the program systemd starts
//	<app>/shared/           the folder the app runs in: its .env and its data
//	<app>/deploys.log       one line per deploy or rollback: time, id, commit, action, result
//	.config/systemd/user/tifa-<app>.socket and .service, and tifa-<app>-http.socket with HTTPS
//	.config/tifa/cloudflare-purge  the purge token of each zone, see deploypurge.go
//
// Every script runs under sh -c, so the login shell of the user does not matter.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/mod/module"
)

const (
	// deployUser owns the apps on a server and runs them as systemd user units.
	deployUser = "tifa"
	// deployHome is the home of deployUser when setup creates it.
	deployHome = "/srv/tifa"
	// firstPort and lastPort bound the loopback ports setup gives apps.
	firstPort, lastPort = 9527, 9999
	// keepReleases is how many releases stay on the server after a deploy.
	keepReleases = 5
	// healthPath is what the server asks the new program for after a deploy.
	healthPath = "/healthz"
	// httpPort and httpsPort are the ports of an app that serves visitors itself, with
	// deploy.front none: plain HTTP on 80, and with a domain HTTPS on 443 as well.
	httpPort, httpsPort = 80, 443
	// portsConf lets the systemd of the deploy user open ports 80 and 443, see portsBody.
	portsConf = "/etc/sysctl.d/60-tifa-ports.conf"
)

// lowPortFile holds the first port a user other than root may listen on. Tests point it at a
// file of their own.
var lowPortFile = "/proc/sys/net/ipv4/ip_unprivileged_port_start"

// deployAppName names the app on the server after the last element of its module path that is not
// a major version, the name tifa build gives the program. It names the folder, the program and the
// units, so it must be safe in all three.
func deployAppName(modulePath string) (string, error) {
	if prefix, _, ok := module.SplitPathVersion(modulePath); ok {
		modulePath = prefix
	}
	name := path.Base(modulePath)
	ok := name != "" && isAlnum(rune(name[0]))
	for _, r := range name {
		ok = ok && (isAlnum(r) || r == '.' || r == '_' || r == '-')
	}
	if !ok {
		return "", fmt.Errorf("the app is named after its module %s, and %q is not a name tifa deploy can use: it needs letters, digits, dots, underscores and hyphens, starting with a letter or digit", modulePath, name)
	}
	return name, nil
}

func isAlnum(r rune) bool {
	return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9'
}

// serverTarget turns what uname -s and uname -m print into GOOS and GOARCH. The app runs under
// systemd, so the server must be Linux, on a machine the server render engine supports.
func serverTarget(system, machine string) (goos, goarch string, err error) {
	if system != "Linux" {
		return "", "", fmt.Errorf("the server runs %q, and tifa deploy needs Linux with systemd", system)
	}
	switch machine {
	case "x86_64", "amd64":
		return "linux", "amd64", nil
	case "aarch64", "arm64":
		return "linux", "arm64", nil
	}
	return "", "", fmt.Errorf("the server is a %q machine, and Tifa programs run on x86_64 (amd64) and aarch64 (arm64)", machine)
}

// listeningPorts reads the ports of ss -Hltn: the local address is the fourth column.
func listeningPorts(ss string) []int {
	var ports []int
	for _, line := range strings.Split(ss, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		addr := f[3]
		if p, err := strconv.Atoi(addr[strings.LastIndexByte(addr, ':')+1:]); err == nil {
			ports = append(ports, p)
		}
	}
	return ports
}

// pickPort returns the lowest port from firstPort that is not in used.
func pickPort(used []int) (int, error) {
	for p := firstPort; p <= lastPort; p++ {
		if !slices.Contains(used, p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("every port from %d to %d is taken on the server", firstPort, lastPort)
}

// deployRecord is a line of deploys.log.
type deployRecord struct {
	Time   string // UTC, RFC 3339
	ID     string // the release
	Commit string // git commit of the app, with -dirty for uncommitted changes, or none
	Action string // deploy or rollback
	Result string // ok, failed when the health check failed, or releaseCommandFailed
}

// serverState is what the probe script prints about the server and the app, as the deploy user.
type serverState struct {
	OS, Arch string
	Home     string // home of the deploy user
	Port     int    // 0 before the app exists on the server
	Used     []int  // ports of the other apps, and ports something listens on
	// OtherPorts are the ports of the other apps of the deploy user, which they keep while they
	// do not run.
	OtherPorts []int
	Current    string // the release current links to, empty before the first deploy
	Releases   []string
	Records    []deployRecord
	// Linger and Bus say the systemd of the deploy user runs without a login: what the units of
	// an app need.
	Linger, Bus bool
	// SocketSum and ServiceSum are the SHA-256 of the unit files of the app, empty when missing.
	SocketSum, ServiceSum string
	// TunnelID is the Cloudflare Tunnel the server runs, from /etc/tifa/tunnel, empty without one.
	TunnelID string
	// Machine is the host name of the server and the start of its machine ID, which name its tunnel.
	Machine string
	// Cloudflared is where cloudflared is on the server, empty without it. TunnelActive says the
	// service tifa-tunnel runs. Apt says the server has apt, to install cloudflared with.
	Cloudflared       string
	TunnelActive, Apt bool
	// HostKeys are the public host keys of its sshd, as "<type> <base64>".
	HostKeys []string
	// GitHubKeys are the repositories that have a tifa-github line in authorized_keys.
	GitHubKeys []string
	// AppDir says the folder of the app is there. Others are the other apps of the deploy user.
	AppDir bool
	Others []string
	// CloudflaredByTifa says setup installed cloudflared: it wrote the apt setting that keeps it
	// current.
	CloudflaredByTifa bool
	// Active is the state systemd gives the service of the app, such as active or failed, and
	// ActiveSince when it last became active, as systemd prints it in UTC. RSS is the resident
	// memory of its program in KiB, 0 when it does not run.
	Active, ActiveSince string
	RSS                 int64
	// Restarts is how many times systemd restarted the service by itself, from NRestarts.
	Restarts int
	// Health is the status GET healthPath answered just now, 000 for no answer, empty when it was
	// not asked: only the status script asks, and only an active service.
	Health string
	// PurgeFile says the server has the file of purge tokens, and PurgeZones are the zones it has
	// a token for. The probe prints the names only.
	PurgeFile  bool
	PurgeZones []string
	// HTTPS is the domain the app serves HTTPS for itself, empty without. HTTPSum is the SHA-256
	// of the unit of its port 80, tifa-<app>-http.socket, empty when missing.
	HTTPS, HTTPSum string
	// TLSCertSum and TLSKeySum are the SHA-256 of the certificate and key setup uploaded into
	// shared/tls, empty without them.
	TLSCertSum, TLSKeySum string
	// LowPort is the first port the deploy user may listen on, 0 when the server did not say.
	// PortsConf says the server has portsConf, which setup wrote.
	LowPort   int
	PortsConf bool
	// UFW is "on" when ufw is installed and on at boot, "off" when it is installed and off, and
	// empty without it. Only root reads its rules: see deployufw.go.
	UFW string
	// EnvServerAddr says the .env of the app on the server sets SERVER_ADDR, which the socket of
	// systemd overrides.
	EnvServerAddr bool
	// Listen are the TCP ports something listens on, with the programs ss names: all of them as
	// root, only those of the deploy user as the deploy user. UserSockets are the sockets the
	// systemd of the deploy user holds, with the service each one starts.
	Listen      []listener
	UserSockets []userSocket
	// NginxTifa says the marker /etc/tifa/nginx is there: setup installed the nginx of the server.
	// NginxSites are the apps that have a site in it. Both are readable by every user.
	NginxTifa  bool
	NginxSites []string
	// SSHPassword says sshd accepts passwords, as far as the files of its configuration say.
	// tifa deploy harden turns that off: see deployharden.go.
	SSHPassword bool
}

// listener is a port something listens on, and the programs ss names for it, such as "nginx (pid 812)".
type listener struct {
	Port     int
	Programs []string
}

// userSocket is a socket the systemd of the deploy user listens on, and the service it starts.
type userSocket struct {
	Port    int
	Service string
}

// ssPrograms reads the programs of a line of ss -p: users:(("nginx",pid=812,fd=6),...).
var ssPrograms = regexp.MustCompile(`\("([^"]+)",pid=([0-9]+)`)

// parseListen reads a line of ss -Hltnp. It returns false for a line without a port.
func parseListen(line string) (listener, bool) {
	f := strings.Fields(line)
	if len(f) < 4 {
		return listener{}, false
	}
	p, err := strconv.Atoi(f[3][strings.LastIndexByte(f[3], ':')+1:])
	if err != nil {
		return listener{}, false
	}
	// One program often listens from several processes, such as nginx and its workers: the first
	// pid names it.
	l := listener{Port: p}
	var seen []string
	for _, m := range ssPrograms.FindAllStringSubmatch(line, -1) {
		if !slices.Contains(seen, m[1]) {
			seen = append(seen, m[1])
			l.Programs = append(l.Programs, m[1]+" (pid "+m[2]+")")
		}
	}
	return l, true
}

// holder says who listens on port of the server besides app: another app of the deploy user, a
// program (programs names it when the probe could see it), or nobody.
func (s serverState) holder(app string, port int) (otherApp string, programs []string, taken bool) {
	for _, u := range s.UserSockets {
		if u.Port != port || u.Service == unitName(app)+".service" {
			continue
		}
		if name, ok := strings.CutPrefix(u.Service, "tifa-"); ok && strings.HasSuffix(name, ".service") {
			return strings.TrimSuffix(name, ".service"), nil, true
		}
		return "", []string{u.Service + " of the user " + deployUser}, true
	}
	for _, l := range s.Listen {
		if l.Port != port {
			continue
		}
		for _, u := range s.UserSockets {
			if u.Port == port {
				// The socket of app itself, which ss also lists.
				return "", nil, false
			}
		}
		return "", l.Programs, true
	}
	return "", nil, false
}

// freeFor says nothing but app listens on port, and no other app of the deploy user has it.
func (s serverState) freeFor(app string, port int) bool {
	_, _, taken := s.holder(app, port)
	return !taken && !slices.Contains(s.OtherPorts, port)
}

// Prepared reports whether the server can hold an app: the systemd of the deploy user runs.
func (s serverState) Prepared() bool { return s.Linger && s.Bus }

// parseServerState reads the output of probeScript: one fact per line, a word and a value.
func parseServerState(out string) (serverState, error) {
	var s serverState
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		word, value, _ := strings.Cut(line, " ")
		switch word {
		case "os":
			s.OS = value
		case "arch":
			s.Arch = value
		case "home":
			s.Home = value
		case "bus":
			s.Bus = true
		case "linger":
			s.Linger = value == "yes"
		case "port", "taken":
			p, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || p <= 0 || p > 65535 {
				return s, fmt.Errorf("the server gave the port %q", value)
			}
			if word == "port" {
				s.Port = p
			} else {
				s.Used = append(s.Used, p)
				s.OtherPorts = append(s.OtherPorts, p)
			}
		case "listen":
			s.Used = append(s.Used, listeningPorts(value)...)
			if l, ok := parseListen(value); ok {
				s.Listen = append(s.Listen, l)
			}
		case "usock":
			// A line of systemctl --user list-sockets: the address, the socket unit, the service.
			f := strings.Fields(value)
			if len(f) < 3 {
				continue
			}
			if p, err := strconv.Atoi(f[0][strings.LastIndexByte(f[0], ':')+1:]); err == nil {
				s.UserSockets = append(s.UserSockets, userSocket{Port: p, Service: f[len(f)-1]})
			}
		case "https":
			s.HTTPS = strings.TrimSpace(value)
		case "unit-http":
			s.HTTPSum = strings.TrimSpace(value)
		case "tls-cert":
			s.TLSCertSum = strings.TrimSpace(value)
		case "tls-key":
			s.TLSKeySum = strings.TrimSpace(value)
		case "lowport":
			p, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || p < 0 || p > 65535 {
				return s, fmt.Errorf("the server gave the first port users may listen on as %q", value)
			}
			s.LowPort = p
		case "ports-conf":
			s.PortsConf = true
		case "env-server-addr":
			s.EnvServerAddr = true
		case "ufw":
			if v := strings.TrimSpace(value); v == "on" || v == "off" {
				s.UFW = v
			} else {
				return s, fmt.Errorf("the server gave the state of ufw as %q", value)
			}
		case "unit-socket":
			s.SocketSum = strings.TrimSpace(value)
		case "unit-service":
			s.ServiceSum = strings.TrimSpace(value)
		case "tunnel":
			s.TunnelID = strings.TrimSpace(value)
		case "machine":
			s.Machine = strings.TrimSpace(value)
		case "cloudflared":
			s.Cloudflared = strings.TrimSpace(value)
		case "tunnel-active":
			s.TunnelActive = true
		case "apt":
			s.Apt = true
		case "hostkey":
			typ, b64, ok := strings.Cut(strings.TrimSpace(value), " ")
			if _, err := base64.StdEncoding.DecodeString(b64); !ok || !strings.HasPrefix(typ, "ssh-") && !strings.HasPrefix(typ, "ecdsa-") || err != nil {
				return s, fmt.Errorf("the server gave a host key tifa cannot read: %q", value)
			}
			s.HostKeys = append(s.HostKeys, typ+" "+b64)
		case "ghkey":
			s.GitHubKeys = append(s.GitHubKeys, strings.TrimSpace(value))
		case "appdir":
			s.AppDir = true
		case "other":
			s.Others = append(s.Others, strings.TrimSpace(value))
		case "cloudflared-tifa":
			s.CloudflaredByTifa = true
		case "ssh-password":
			s.SSHPassword = true
		case "nginx-tifa":
			s.NginxTifa = true
		case "nginx-site":
			s.NginxSites = append(s.NginxSites, strings.TrimSpace(value))
		case "unit-active":
			s.Active = strings.TrimSpace(value)
		case "unit-since":
			s.ActiveSince = strings.TrimSpace(value)
		case "unit-rss":
			n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil || n < 0 {
				return s, fmt.Errorf("the server gave the memory %q", value)
			}
			s.RSS = n
		case "unit-restarts":
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 {
				return s, fmt.Errorf("the server gave the number of restarts %q", value)
			}
			s.Restarts = n
		case "health":
			v := strings.TrimSpace(value)
			if _, err := strconv.Atoi(v); err != nil || len(v) != 3 {
				return s, fmt.Errorf("the server gave the health check status %q", value)
			}
			s.Health = v
		case "purge-file":
			s.PurgeFile = true
		case "purge-zone":
			s.PurgeZones = append(s.PurgeZones, strings.TrimSpace(value))
		case "current":
			s.Current = value
		case "release":
			s.Releases = append(s.Releases, value)
		case "record":
			f := strings.Split(value, "\t")
			if len(f) != 5 {
				return s, fmt.Errorf("deploys.log has a line tifa cannot read: %q", value)
			}
			s.Records = append(s.Records, deployRecord{Time: f[0], ID: f[1], Commit: f[2], Action: f[3], Result: f[4]})
		default:
			return s, fmt.Errorf("the server printed a line tifa cannot read: %q", line)
		}
	}
	slices.Sort(s.Releases)
	return s, nil
}

// good reports whether release id passed its health check and did not fail since, and returns
// its record: the last record of id says ok. A release that passed on this server and failed on
// another of the app has a failed record after its ok.
func (s serverState) good(id string) (deployRecord, bool) {
	r := s.latest(id)
	return r, r.ID != "" && r.Result == "ok"
}

// latest returns the last record of release id, or none.
func (s serverState) latest(id string) deployRecord {
	for _, r := range slices.Backward(s.Records) {
		if r.ID == id {
			return r
		}
	}
	return deployRecord{}
}

// rollbackTarget picks the newest release before the current one that passed its health check
// and is still on the server. Release ids are times, so they sort in the order of the deploys.
func rollbackTarget(s serverState) (deployRecord, error) {
	if s.Current == "" {
		return deployRecord{}, errors.New("nothing is deployed on the server yet")
	}
	for _, id := range slices.Backward(s.Releases) {
		if id >= s.Current {
			continue
		}
		if r, ok := s.good(id); ok {
			return r, nil
		}
	}
	return deployRecord{}, fmt.Errorf("there is no earlier release on the server that passed its health check (the current one is %s)", s.Current)
}

// releaseIDShape is the shape of a release ID: the UTC time of its deploy, as tifa deploy status
// lists it. It is checked on this machine before anything is sent to a server script.
var releaseIDShape = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z$`)

// rollbackTo checks that release id is a place a rollback can go: still on the server and
// passed its health check. current is true when it already runs, and then nothing is to be done.
// A release it cannot go to is refused with the ones it can go to.
func rollbackTo(s serverState, id string) (target deployRecord, current bool, err error) {
	if s.Current == "" {
		return deployRecord{}, false, errors.New("nothing is deployed on the server yet")
	}
	if id == s.Current {
		return deployRecord{}, true, nil
	}
	var options []string
	for _, kept := range slices.Backward(s.Releases) {
		if kept == s.Current {
			continue
		}
		if r, ok := s.good(kept); ok {
			if kept == id {
				return r, false, nil
			}
			options = append(options, fmt.Sprintf("  %s  commit %s", r.ID, r.Commit))
		}
	}
	why := "is not kept on the server"
	switch {
	case slices.Contains(s.Releases, id) && s.latest(id).Result == releaseCommandFailed:
		why = "failed deploy.release_command when it was deployed"
	case slices.Contains(s.Releases, id):
		why = "did not pass its health check when it was deployed"
	}
	if len(options) == 0 {
		return deployRecord{}, false, fmt.Errorf("release %s %s, and no other release can be rolled back to", id, why)
	}
	return deployRecord{}, false, fmt.Errorf("release %s %s. It can go to:\n%s", id, why, strings.Join(options, "\n"))
}

// fallback is the release to go back to when a new one fails its check: the one that runs now,
// if it ever passed. Empty means there is none, and the app is stopped.
func fallback(s serverState) string {
	if _, ok := s.good(s.Current); ok {
		return s.Current
	}
	return ""
}

// pruneReleases lists the releases to delete after the release next went live: all but the keep
// newest, and never current or next.
func pruneReleases(releases []string, current, next string, keep int) []string {
	all := slices.Sorted(slices.Values(append(slices.Clone(releases), next)))
	all = slices.Compact(all)
	var out []string
	for i, id := range all {
		if i < len(all)-keep && id != current && id != next {
			out = append(out, id)
		}
	}
	return out
}

// unitName is the name of the systemd units of an app, without the suffix.
func unitName(app string) string { return "tifa-" + app }

// appUnits are the systemd units of an app: the socket, the socket of port 80 with HTTPS (empty
// without), and the service.
type appUnits struct{ socket, http, service string }

// unitsOf writes the units of app for its port: 80 or 443 when it serves visitors itself, else a
// loopback port behind the Cloudflare Tunnel or nginx, or with open that port on every address of
// the server, for the load balancer or proxy of the user, plain HTTP whatever the port. domain is
// the domain it serves HTTPS for itself, on 443.
//
// The sockets hold the ports while the program restarts, see docs/build-and-deploy/deploy-by-hand.md. With
// HTTPS the program gets two, each named by the setting it serves, and the settings of the
// built-in HTTPS in its environment: the certificates stay in certs, next to the releases.
func unitsOf(app string, port int, domain string, open, cert bool) appUnits {
	return unitsAt(app, port, domain, open, cert, "")
}

// unitsAt is unitsOf with ip, the one address of the server an app that is open listens on: empty
// for every address. FreeBind lets the socket start at boot before the address is up.
func unitsAt(app string, port int, domain string, open, cert bool, ip string) appUnits {
	unit := unitName(app)
	itself := !open && servesVisitors(port)
	listen := fmt.Sprintf("ListenStream=127.0.0.1:%d\n", port)
	switch {
	case open && ip != "":
		listen = fmt.Sprintf("ListenStream=%s\nFreeBind=true\n", net.JoinHostPort(ip, strconv.Itoa(port)))
	case open:
		listen = fmt.Sprintf("ListenStream=%d\n", port)
	case port == httpPort:
		listen = "ListenStream=80\n"
	case port == httpsPort:
		listen = "ListenStream=443\nFileDescriptorName=server.addr\n"
	}
	mark := ""
	if open {
		mark = externalMark + "\n"
	}
	u := appUnits{socket: fmt.Sprintf(`# Written by tifa deploy setup, which writes it again when it differs.
%s[Unit]
Description=%s socket (Tifa)

[Socket]
%s
[Install]
WantedBy=sockets.target
`, mark, app, listen)}
	sockets, extra := unit+".socket", ""
	if itself && port == httpsPort {
		u.http = fmt.Sprintf(`# Written by tifa deploy setup, which writes it again when it differs.
[Unit]
Description=%s plain HTTP socket (Tifa): Let's Encrypt and the redirect to HTTPS

[Socket]
ListenStream=80
FileDescriptorName=server.http_addr
Service=%s.service

[Install]
WantedBy=sockets.target
`, app, unit)
		sockets += " " + unit + "-http.socket"
		extra = fmt.Sprintf("Sockets=%s\nEnvironment=SERVER_TLS_DOMAINS=%s SERVER_TLS_CACHE=%%h/%s/certs\n", sockets, domain, app)
		if cert {
			extra = fmt.Sprintf("Sockets=%s\nEnvironment=SERVER_TLS_CERT=%%h/%[2]s/%[3]s/cert.pem SERVER_TLS_KEY=%%h/%[2]s/%[3]s/key.pem\n", sockets, app, tlsDir)
		}
	}
	u.service = fmt.Sprintf(`# Written by tifa deploy setup, which writes it again when it differs.
[Unit]
Description=%[1]s (Tifa)
StartLimitIntervalSec=0
Requires=%[2]s
After=%[2]s

[Service]
WorkingDirectory=%%h/%[1]s/shared
ExecStart=%%h/%[1]s/current/%[1]s
%[3]sRestart=on-failure
RestartSec=2s

[Install]
WantedBy=default.target
`, app, sockets, extra)
	return u
}

// portsText names the ports visitors reach an app on.
func portsText(port int) string {
	if port == httpsPort {
		return "ports 80 and 443"
	}
	return "port " + strconv.Itoa(port)
}

// servesVisitors says the app on port serves visitors itself, as deploy.front none makes it.
func servesVisitors(port int) bool { return port == httpPort || port == httpsPort }

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// script puts the variables in front of body, each quoted. vars holds pairs of name and value.
func script(body string, vars ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(vars); i += 2 {
		fmt.Fprintf(&b, "%s=%s\n", vars[i], shellQuote(vars[i+1]))
	}
	return b.String() + body
}

// sudoCheckBody is the first step of root setup runs when the host names an admin user and the
// deploy user already logs in: sudoScript stops it when the admin user cannot run sudo.
const sudoCheckBody = "# tifa: check sudo\ntrue\n"

// setupProbeBody runs as root before the server is prepared. It checks the server has what the
// scripts need, and prints its system.
const setupProbeBody = `set -eu
if [ "$(id -u)" != 0 ]; then echo "tifa: deploy setup needs root on the server" >&2; exit 1; fi
for c in systemctl loginctl runuser useradd curl flock gzip sha256sum ss; do
	command -v "$c" >/dev/null 2>&1 || { echo "tifa: the server has no $c, which tifa deploy needs" >&2; exit 1; }
done
echo "os $(uname -s)"
echo "arch $(uname -m)"
echo "machine $(hostname -s)-$(head -c 8 /etc/machine-id 2>/dev/null)"
if [ -r "$lowport_file" ]; then echo "lowport $(cat "$lowport_file")"; fi
ss -Hltnp | sed 's/^/listen /'
` + ufwProbe

// setupProbeScript is setupProbeBody.
func setupProbeScript() string {
	return script(setupProbeBody, "lowport_file", lowPortFile, "ufw_conf", ufwConf, "ufw_program", ufwProgram)
}

// listenersBody runs as root and lists the TCP ports something listens on, with the programs, which
// the deploy user cannot see.
const listenersBody = `# tifa: who listens on the ports of the visitors
ss -Hltnp | sed 's/^/listen /'
`

// portsBody runs as root. It lets users other than root listen on ports from 80, so the systemd
// of the deploy user can open ports below 1024 for the apps that need them: 80 and 443 for the app
// that serves visitors itself, server.addr behind something of the user's. No right goes to the
// app. The value it found goes into the file, so remove can put it back.
const portsBody = `set -eu
# tifa: open ports 80 and 443 to the systemd of the deploy user
f=` + portsConf + `
before=$(cat "$lowport_file")
if [ -f "$f" ]; then
	kept=$(sed -n 's/^# before: \([0-9][0-9]*\)$/\1/p' "$f")
	if [ -n "$kept" ]; then before=$kept; fi
fi
printf '%s\n' "# Written by tifa deploy setup: users other than root may listen on ports from 80, so the systemd" \
	"# of the user tifa opens the ports below 1024 of its apps. tifa deploy remove of the last app" \
	"# that needs one deletes this file and puts back the value from before." \
	"# before: $before" "net.ipv4.ip_unprivileged_port_start = 80" > "$f.part"
mv "$f.part" "$f"
sysctl -q -w net.ipv4.ip_unprivileged_port_start=80
echo "net.ipv4.ip_unprivileged_port_start was $before, now 80, kept in $f"
`

// unportsBody runs as root and undoes portsBody.
const unportsBody = `set -eu
# tifa: close ports 80 and 443 to the systemd of the deploy user
f=` + portsConf + `
if [ ! -f "$f" ]; then exit 0; fi
before=$(sed -n 's/^# before: \([0-9][0-9]*\)$/\1/p' "$f")
rm -f "$f"
if [ -n "$before" ]; then
	sysctl -q -w net.ipv4.ip_unprivileged_port_start="$before"
	echo "net.ipv4.ip_unprivileged_port_start is $before again"
fi
`

// copyKeysBody runs as root, in serverBody. Who may log in as the admin user may deploy: the keys
// of $admin, root or the user of the host, go to the deploy user $user, whose home is $home. The
// home of another admin user is the one getent knows.
const copyKeysBody = `install -d -o "$user" -g "$user" -m 700 "$home/.ssh"
keys="$home/.ssh/authorized_keys"
touch "$keys"
chown "$user:$user" "$keys"
chmod 600 "$keys"
if [ "$admin" = root ]; then
	from=/root/.ssh/authorized_keys
else
	admin_home=$(getent passwd "$admin" | cut -d: -f6)
	if [ -z "$admin_home" ]; then echo "tifa: the server has no user $admin, whose keys the user $user gets" >&2; exit 1; fi
	from="$admin_home/.ssh/authorized_keys"
fi
if [ -f "$from" ]; then
	while IFS= read -r key || [ -n "$key" ]; do
		case "$key" in ''|'#'*) continue ;; esac
		grep -qxF -- "$key" "$keys" || printf '%s\n' "$key" >> "$keys"
	done < "$from"
fi
`

// serverBody runs as root. Each step leaves what is already there, so it can run again. It makes
// the deploy user, lets whoever logs in as the admin user log in as it, and starts its systemd
// without a login.
const serverBody = `set -eu
if ! id -u "$user" >/dev/null 2>&1; then
	useradd --create-home --home-dir "$home" --shell /bin/bash --user-group "$user"
	# No password, and not locked, which sshd refuses with UsePAM no.
	usermod -p '*' "$user"
	echo "created the user $user, home $home"
fi
home=$(getent passwd "$user" | cut -d: -f6)
uid=$(id -u "$user")
` + copyKeysBody + `# Linger starts the systemd of the user at boot, without a login.
loginctl enable-linger "$user"
i=0
while [ ! -S "/run/user/$uid/bus" ]; do
	i=$((i + 1))
	if [ "$i" -gt 30 ]; then echo "tifa: the systemd of $user did not start" >&2; exit 1; fi
	sleep 1
done
`

// serverScript is serverBody for the deploy user, with the keys of admin, the admin user of the host.
func serverScript(admin string) string {
	return script(serverBody, "user", deployUser, "home", deployHome, "admin", admin)
}

// appBody runs as the deploy user, on a server that serverBody prepared. It makes the folder of
// the app, its settings file, its port and its systemd units, and opens the sockets. Each step
// leaves what is already there, so it can run again.
//
// With $move the sockets listen elsewhere than before: the app and its old sockets stop first,
// since a socket opens its new address only when it starts again. With $restart the units changed.
// Either way an app that has a release runs again on the new units and must pass the health check
// of a deploy, which exits with 3 and the end of its log.
const appBody = `set -eu
d="$HOME/$app"
unit="tifa-$app"
install -d -m 755 "$d" "$d/releases"
install -d -m 750 "$d/shared"
if [ ! -e "$d/shared/.env" ]; then (umask 077; : > "$d/shared/.env"); fi
printf '%s\n' "$port" > "$d/port"
if [ -n "$domain" ]; then
	install -d -m 700 "$d/certs"
	printf '%s\n' "$domain" > "$d/https"
else
	rm -f "$d/https"
fi
units="$HOME/.config/systemd/user"
install -d -m 755 "$HOME/.config" "$HOME/.config/systemd" "$units"
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
if [ -n "$move" ]; then
	# All at once: a service stopped alone while its sockets stay gets a warning.
	old=""
	for u in "$unit.service" "$unit.socket" "$unit-http.socket"; do
		if [ -f "$units/$u" ]; then old="$old $u"; fi
	done
	if [ -n "$old" ]; then systemctl --user stop $old; fi
fi
printf '%s' "$socket_unit" > "$units/tifa-$app.socket"
printf '%s' "$service_unit" > "$units/$unit.service"
if [ -n "$http_unit" ]; then
	printf '%s' "$http_unit" > "$units/$unit-http.socket"
elif [ -f "$units/$unit-http.socket" ]; then
	systemctl --user disable --quiet "$unit-http.socket"
	rm -f "$units/$unit-http.socket"
fi
systemctl --user daemon-reload
systemctl --user enable --now "$unit.socket"
if [ -n "$http_unit" ]; then systemctl --user enable --now "$unit-http.socket"; fi
if [ -z "$move$restart" ] || [ ! -L "$d/current" ]; then exit 0; fi
set +e
` + healthCheckFunc + `systemctl --user reset-failed "$unit.service" >/dev/null 2>&1
systemctl --user restart "$unit.service" || exit 1
if check; then exit 0; fi
echo "--- last lines of the log of $app ---"
journalctl --user -u "$unit.service" -n 30 --no-pager -o cat 2>/dev/null
echo "---"
exit 3
`

// appScript is appBody for app on port, with domain for HTTPS, and with open on every address of
// the server, or on ip only. move says its sockets listen elsewhere than before, restart that its units changed. It
// checks the restarted app for up to wait seconds.
func appScript(app string, port int, domain, ip string, open, cert, move, restart bool, wait int) string {
	u := unitsAt(app, port, domain, open, cert, ip)
	flag := map[bool]string{true: "1"}
	return script(appBody, "app", app, "port", strconv.Itoa(port), "domain", domain,
		"move", flag[move], "restart", flag[restart], "health", healthPath, "wait", strconv.Itoa(wait),
		"socket_unit", u.socket, "http_unit", u.http, "service_unit", u.service)
}

// probeBody runs as the deploy user and prints what parseServerState reads: the system, whether
// the systemd of the user runs without a login, the app and its service, the other apps and the
// ports taken, the tunnel of the server and the keys of its sshd. With $health set it also asks
// the service for that path, when it runs. Every line is optional, and it never fails.
const probeBody = `d="$HOME/$app"
` + askFunc + `rt="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
echo "os $(uname -s)"
echo "arch $(uname -m)"
echo "home $HOME"
if [ -e "$rt/bus" ]; then echo "bus yes"; fi
echo "linger $(loginctl show-user "$(id -un)" -p Linger --value 2>/dev/null || echo no)"
if [ -d "$d" ]; then echo "appdir yes"; fi
if [ -f "$d/port" ]; then echo "port $(cat "$d/port")"; fi
if [ -f "$d/shared/.env" ] && grep -Eq '^[[:space:]]*(export[[:space:]]+)?SERVER_ADDR=' "$d/shared/.env"; then echo "env-server-addr yes"; fi
for f in "$HOME"/*/port; do
	if [ -f "$f" ] && [ "$f" != "$d/port" ]; then
		echo "taken $(cat "$f")"
		echo "other $(basename "$(dirname "$f")")"
	fi
done
if [ -e "$rt/bus" ] && [ -f "$d/port" ]; then
	# TZ=UTC makes systemd print its times in UTC.
	props=$(XDG_RUNTIME_DIR="$rt" TZ=UTC systemctl --user show "tifa-$app.service" -p ActiveState -p ActiveEnterTimestamp -p MainPID -p NRestarts 2>/dev/null)
	state=""
	while IFS='=' read -r k v; do
		case "$k" in
		ActiveState) state=$v; echo "unit-active $v" ;;
		ActiveEnterTimestamp) if [ -n "$v" ]; then echo "unit-since $v"; fi ;;
		NRestarts) if [ -n "$v" ]; then echo "unit-restarts $v"; fi ;;
		MainPID)
			if [ -n "$v" ] && [ "$v" != 0 ]; then
				rss=$(ps -o rss= -p "$v" 2>/dev/null | tr -d ' ')
				if [ -n "$rss" ]; then echo "unit-rss $rss"; fi
			fi ;;
		esac
	done <<EOF
$props
EOF
	if [ -n "$health" ] && [ "$state" = active ]; then
		port=$(cat "$d/port")
		echo "health $(ask)"
	fi
fi
if [ -s "$d/https" ]; then echo "https $(cat "$d/https")"; fi
for n in cert key; do
	if [ -f "$d/` + tlsDir + `/$n.pem" ]; then echo "tls-$n $(sha256sum < "$d/` + tlsDir + `/$n.pem" | cut -d' ' -f1)"; fi
done
if [ -r "$lowport_file" ]; then echo "lowport $(cat "$lowport_file")"; fi
if [ -f ` + portsConf + ` ]; then echo "ports-conf yes"; fi
` + ufwProbe + `# ss names only the programs of this user. systemd names the sockets it holds for this user.
ss -Hltnp 2>/dev/null | sed 's/^/listen /'
if [ -e "$rt/bus" ]; then
	XDG_RUNTIME_DIR="$rt" systemctl --user list-sockets --no-legend --plain 2>/dev/null | sed 's/^/usock /'
fi
u="$HOME/.config/systemd/user"
for n in socket service; do
	if [ -f "$u/tifa-$app.$n" ]; then echo "unit-$n $(sha256sum < "$u/tifa-$app.$n" | cut -d' ' -f1)"; fi
done
if [ -f "$u/tifa-$app-http.socket" ]; then echo "unit-http $(sha256sum < "$u/tifa-$app-http.socket" | cut -d' ' -f1)"; fi
if c=$(readlink "$d/current" 2>/dev/null); then echo "current ${c#releases/}"; fi
for r in "$d"/releases/*/; do
	if [ -d "$r" ]; then echo "release $(basename "$r")"; fi
done
if [ -f "$d/deploys.log" ]; then tail -n 200 "$d/deploys.log" | sed 's/^/record /'; fi
if [ -f /etc/tifa/tunnel ]; then echo "tunnel $(cat /etc/tifa/tunnel)"; fi
echo "cloudflared $(command -v cloudflared || true)"
if systemctl is-active --quiet tifa-tunnel.service 2>/dev/null; then echo "tunnel-active yes"; fi
if command -v apt-get >/dev/null 2>&1; then echo "apt yes"; fi
echo "machine $(hostname -s)-$(head -c 8 /etc/machine-id 2>/dev/null)"
for f in /etc/ssh/ssh_host_*_key.pub; do
	if [ -f "$f" ]; then echo "hostkey $(cut -d' ' -f1,2 "$f")"; fi
done
if [ -f "$HOME/.ssh/authorized_keys" ]; then
	sed -n 's/.* tifa-github \([^ ]*\)$/ghkey \1/p' "$HOME/.ssh/authorized_keys"
fi
if [ -f /etc/apt/apt.conf.d/52tifa-cloudflared ]; then echo "cloudflared-tifa yes"; fi
if [ -f /etc/tifa/nginx ]; then echo "nginx-tifa yes"; fi
# The first PasswordAuthentication sshd reads, as the files say (the drop-ins first, when the main
# file includes them), and yes when none sets it. A Match block ends a file: it is not for everyone.
if [ -r /etc/ssh/sshd_config ]; then
	files=/etc/ssh/sshd_config
	if grep -qi '^[[:space:]]*Include' /etc/ssh/sshd_config; then files="/etc/ssh/sshd_config.d/*.conf $files"; fi
	pw=$(for f in $files; do
		if [ -r "$f" ]; then awk 'tolower($1)=="match"{exit} tolower($1)=="passwordauthentication"{print tolower($2); exit}' "$f"; fi
	done | head -n 1)
	if [ "${pw:-yes}" = yes ]; then echo "ssh-password yes"; fi
fi
for f in /etc/nginx/conf.d/tifa-*.conf; do
	if [ -f "$f" ]; then a=$(basename "$f" .conf); echo "nginx-site ${a#tifa-}"; fi
done
# The zones of the purge tokens, never a token: only what comes before the first tab of a line.
f="$HOME/` + purgeFile + `"
if [ -d "$HOME/` + purgeDir + `" ]; then echo "purge-file yes"; fi
if [ -f "$f" ]; then
	t=$(printf '\t')
	while IFS= read -r l || [ -n "$l" ]; do
		case "$l" in '#'*) ;; *"$t"*) echo "purge-zone ${l%%"$t"*}" ;; esac
	done < "$f"
fi
exit 0
`

// probeScript is probeBody for app.
func probeScript(app string) string {
	return script(probeBody, "app", app, "health", "", "lowport_file", lowPortFile, "ufw_conf", ufwConf, "ufw_program", ufwProgram)
}

// statusScript is probeBody for app that also asks the service for healthPath.
func statusScript(app string) string {
	return script(probeBody, "app", app, "health", healthPath, "lowport_file", lowPortFile, "ufw_conf", ufwConf, "ufw_program", ufwProgram)
}

// uploadBody runs as the deploy user with the program, gzipped, on its input. It writes the new
// release, and removes it again unless the program arrived whole.
const uploadBody = `set -eu
r="$HOME/$app/releases/$id"
mkdir "$r"
trap 'rm -rf "$r"' EXIT
gzip -dc > "$r/$app.part"
got=$(sha256sum < "$r/$app.part" | cut -d' ' -f1)
if [ "$got" != "$sum" ]; then echo "tifa: the program arrived damaged (sha256 $got, sent $sum)" >&2; exit 1; fi
chmod 755 "$r/$app.part"
mv "$r/$app.part" "$r/$app"
trap - EXIT
`

// uploadScript is uploadBody for the release id of app, whose program has the SHA-256 sum.
func uploadScript(app, id, sum string) string {
	return script(uploadBody, "app", app, "id", id, "sum", sum)
}

// Exit statuses of activateBody past 1, which is any other error.
const (
	activateFailedBack = 3 // the release failed its check, and the one before is back
	activateFailed     = 4 // the release failed its check, and there was nothing to go back to, or that failed too
)

// askFunc is the shell function that asks the app on the server for $health and prints the
// status, 000 for no answer. It waits $1 seconds for the answer, 5 without. It needs $d and $port.
// An app with HTTPS is asked over HTTPS for its domain on the loopback, as a visitor would ask it,
// without checking the certificate: the check is about the app. Without, it is asked over plain
// HTTP on its port.
const askFunc = `ask() {
	if [ -s "$d/https" ]; then
		h=$(cat "$d/https")
		curl -s -k -o /dev/null -w '%{http_code}' --max-time "${1:-5}" --resolve "$h:443:127.0.0.1" "https://$h$health" || true
	else
		curl -s -o /dev/null -w '%{http_code}' --max-time "${1:-5}" "http://127.0.0.1:$port$health" || true
	fi
}
`

// healthCheckFunc is the shell function that asks the app for healthPath, shared by the scripts that
// restart it, about once a second until $wait seconds have passed. A request waits at most the
// time that is left, so a socket that takes the connection and never answers, as the socket of
// systemd does while the app fails to start again and again, cannot make the wait longer. It needs
// $d, $health, $port, $wait and $unit.
const healthCheckFunc = askFunc + `check() {
	end=$(( $(date +%s) + wait ))
	while :; do
		left=$(( end - $(date +%s) ))
		if [ "$left" -gt 5 ]; then left=5; fi
		if [ "$left" -lt 1 ]; then left=1; fi
		code=$(ask "$left")
		if [ "$code" -ge 100 ] && [ "$code" -lt 500 ] && systemctl --user is-active --quiet "$unit.service"; then
			echo "health check: GET $health answered $code"
			return 0
		fi
		if [ $(( $(date +%s) + 1 )) -ge "$end" ]; then break; fi
		sleep 1
	done
	if [ "$code" != 000 ]; then
		echo "health check: GET $health answered $code"
	elif [ -s "$d/https" ]; then
		h=$(cat "$d/https")
		echo "health check: GET https://$h$health got no answer within $wait seconds. Without a certificate for $h the program answers no HTTPS: Let's Encrypt must reach this server at $h on port 80 or 443, so DNS must point here and the firewall must let it in. The log says why"
	else
		echo "health check: GET $health got no answer within $wait seconds"
	fi
	return 1
}
`

// recordFunc is the shell function that adds a line to deploys.log: the time, the release in $1,
// the commit in $2, $action and the result in $3. It needs $d and $action.
const recordFunc = `record() { printf '%s\t%s\t%s\t%s\t%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" "$2" "$action" "$3" >> "$d/deploys.log"; }
`

// activateBody runs as the deploy user. It points current at a release, restarts the program
// through its socket and asks the new program for healthPath until it answers below 500. Then it
// records the release as ok and deletes the old releases in prune. Otherwise it records the
// release as failed, prints the last lines of the log of the app, and points current back at the
// release in back and restarts that one, or stops the app when back is empty. A lost ssh
// connection does not stop it halfway.
const activateBody = `set -u
trap '' HUP PIPE
d="$HOME/$app"
unit="tifa-$app"
export XDG_RUNTIME_DIR="/run/user/$(id -u)"
cd "$d" || exit 1
exec 9>"$d/.lock"
if ! flock -n 9; then echo "tifa: another deploy of $app runs on the server" >&2; exit 1; fi
if [ ! -x "releases/$id/$app" ]; then echo "tifa: the server has no release $id of $app" >&2; exit 1; fi
` + recordFunc + `start() {
	ln -sfn "releases/$1" current
	systemctl --user reset-failed "$unit.socket" "$unit.service" >/dev/null 2>&1
	systemctl --user start "$unit.socket" && systemctl --user restart "$unit.service"
}
` + healthCheckFunc + `echo "starting $id"
if start "$id" && check; then
	record "$id" "$commit" ok
	for old in $prune; do rm -rf "releases/$old"; done
	exit 0
fi
record "$id" "$commit" failed
echo "--- last lines of the log of $app ---"
journalctl --user -u "$unit.service" -n 30 --no-pager -o cat 2>/dev/null
echo "---"
if [ -z "$back" ]; then
	systemctl --user stop "$unit.service"
	echo "stopped $app: there is no release that passed its check to go back to"
	exit 4
fi
echo "going back to $back"
if start "$back" && check; then exit 3; fi
echo "$back fails its check too"
exit 4
`

// activation is what activateScript needs.
type activation struct {
	App, ID, Commit string
	Port            int
	Action          string   // deploy or rollback
	Back            string   // the release to go back to when ID fails, or empty
	Prune           []string // releases to delete when ID passes
	Wait            int      // seconds the health check of ID, and of Back, may take
}

func activateScript(a activation) string {
	return script(activateBody, "app", a.App, "id", a.ID, "commit", a.Commit, "port", strconv.Itoa(a.Port),
		"action", a.Action, "back", a.Back, "prune", strings.Join(a.Prune, " "), "health", healthPath,
		"wait", strconv.Itoa(a.Wait))
}

// tunnelUnit runs cloudflared for the tunnel of the server as a system service. The token reaches
// cloudflared as a systemd credential, so it is in no command line, and cloudflared runs as a
// user of its own with no rights on the apps.
func tunnelUnit(cloudflared string) string {
	return fmt.Sprintf(`# Written by tifa deploy setup, which writes it again when it differs.
[Unit]
Description=Cloudflare Tunnel of the Tifa apps
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=%s --no-autoupdate tunnel run --token-file %%d/token
LoadCredential=token:/etc/tifa/tunnel-token
DynamicUser=yes
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
`, cloudflared)
}

// tunnelUpgradeUnits restart tifa-tunnel when apt, or anything else, replaces cloudflared, so
// the tunnel runs the new version and not only the next boot.
func tunnelUpgradeUnits(cloudflared string) (path, service string) {
	path = fmt.Sprintf(`# Written by tifa deploy setup, which writes it again when it differs.
[Unit]
Description=Restart the Cloudflare Tunnel of the Tifa apps when cloudflared changes

[Path]
PathChanged=%s

[Install]
WantedBy=paths.target
`, cloudflared)
	service = `# Written by tifa deploy setup, which writes it again when it differs.
[Unit]
Description=Restart the Cloudflare Tunnel of the Tifa apps on a new cloudflared

[Service]
Type=oneshot
ExecStart=systemctl try-restart tifa-tunnel.service
`
	return path, service
}

// The package repository of Cloudflare. apt checks the packages against its key, and the
// apt setting lets unattended-upgrades install new versions of cloudflared from it.
const (
	cloudflareKey     = "https://pkg.cloudflare.com/cloudflare-main.gpg"
	cloudflareRepo    = "deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared any main"
	cloudflareUpgrade = `// Written by tifa deploy setup: lets unattended-upgrades keep cloudflared current.
Unattended-Upgrade::Origins-Pattern { "site=pkg.cloudflare.com,origin=cloudflared"; };`
)

// tunnelBody runs as root with the token of the tunnel on its input. When the server has no
// cloudflared it installs it with apt from the package repository of Cloudflare, and refuses
// without apt. It keeps the ID and the token of the tunnel in /etc/tifa, and runs the service
// tifa-tunnel, restarting it only when something changed. It prints one line that says what it
// did.
const tunnelBody = `set -eu
token=$(cat)
if [ -z "$token" ]; then echo "tifa: no tunnel token arrived" >&2; exit 1; fi
umask 022
did=""
bin=$(command -v cloudflared || true)
if [ -z "$bin" ]; then
	if ! command -v apt-get >/dev/null 2>&1; then
		echo "tifa: the server has neither cloudflared nor apt, install cloudflared from https://pkg.cloudflare.com/ or the packages of the system" >&2
		exit 1
	fi
	install -d -m 755 /usr/share/keyrings
	curl -fsSL -o /usr/share/keyrings/cloudflare-main.gpg.part "$key_url"
	mv /usr/share/keyrings/cloudflare-main.gpg.part /usr/share/keyrings/cloudflare-main.gpg
	printf '%s\n' "$repo_line" > /etc/apt/sources.list.d/cloudflared.list
	printf '%s\n' "$upgrade_text" > /etc/apt/apt.conf.d/52tifa-cloudflared
	# needrestart would restart other services of the server after the install, and say so.
	export DEBIAN_FRONTEND=noninteractive NEEDRESTART_SUSPEND=1
	apt-get update -q </dev/null >/dev/null
	apt-get install -y -q cloudflared </dev/null >/dev/null
	bin=$(command -v cloudflared)
	did="installed $("$bin" --version | head -n 1) from pkg.cloudflare.com"
fi
# put writes $2 to the file $1 when it holds something else, and says whether it did.
put() {
	if [ "$(cat "$1" 2>/dev/null)" = "$2" ]; then return 1; fi
	printf '%s\n' "$2" > "$1"
	return 0
}
changed=""
units=""
if put /etc/systemd/system/tifa-tunnel.service "$(printf '%s' "$unit_text" | sed "s|@CLOUDFLARED@|$bin|")"; then changed=1; units=1; fi
if put /etc/systemd/system/tifa-tunnel-upgrade.path "$(printf '%s' "$path_text" | sed "s|@CLOUDFLARED@|$bin|")"; then units=1; fi
if put /etc/systemd/system/tifa-tunnel-upgrade.service "$restart_text"; then units=1; fi
install -d -m 755 /etc/tifa
if put /etc/tifa/tunnel "$tunnel"; then changed=1; fi
umask 077
if [ "$(cat /etc/tifa/tunnel-token 2>/dev/null)" != "$token" ]; then printf '%s' "$token" > /etc/tifa/tunnel-token; changed=1; fi
if [ -n "$units" ]; then systemctl daemon-reload; fi
systemctl enable --quiet tifa-tunnel.service tifa-tunnel-upgrade.path
if [ -n "$units" ]; then systemctl restart tifa-tunnel-upgrade.path; else systemctl start tifa-tunnel-upgrade.path; fi
if [ -n "$changed" ] || ! systemctl is-active --quiet tifa-tunnel.service; then
	systemctl restart tifa-tunnel.service
	did="${did:+$did, }started tifa-tunnel.service"
fi
i=0
until systemctl is-active --quiet tifa-tunnel.service; do
	i=$((i + 1))
	if [ "$i" -gt 15 ]; then echo "tifa: tifa-tunnel.service does not run, see journalctl -u tifa-tunnel" >&2; exit 1; fi
	sleep 1
done
echo "${did:-already running}"
`

// tunnelScript is tunnelBody for the tunnel id.
func tunnelScript(id string) string {
	path, service := tunnelUpgradeUnits("@CLOUDFLARED@")
	return script(tunnelBody, "tunnel", id,
		"key_url", cloudflareKey, "repo_line", cloudflareRepo, "upgrade_text", cloudflareUpgrade,
		"unit_text", strings.TrimSuffix(tunnelUnit("@CLOUDFLARED@"), "\n"),
		"path_text", strings.TrimSuffix(path, "\n"), "restart_text", strings.TrimSuffix(service, "\n"))
}
