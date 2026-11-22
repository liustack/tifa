package main

// What tifa deploy runs on the server, and the choices it makes from what the server says.
//
// The layout on the server, under the home of the deploy user (/srv/tifa when setup creates it):
//
//	<app>/port              the loopback port of the app, chosen once by setup
//	<app>/releases/<id>/<app>  each uploaded program, <id> the UTC time of the deploy
//	<app>/current           link to releases/<id>, the program systemd starts
//	<app>/shared/           the folder the app runs in: its .env and its data
//	<app>/deploys.log       one line per deploy or rollback: time, id, commit, action, result
//	.config/systemd/user/tifa-<app>.socket and .service
//	.config/tifa/cloudflare-purge  the purge token of each zone, see deploypurge.go
//
// Every script runs under sh -c, so the login shell of the user does not matter.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"path"
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
	// healthTries is how many times, a second apart, it asks before the release counts as failed.
	healthTries = 30
)

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
	Result string // ok, or failed when the health check failed
}

// serverState is what the probe script prints about the server and the app, as the deploy user.
type serverState struct {
	OS, Arch string
	Home     string // home of the deploy user
	Port     int    // 0 before the app exists on the server
	Used     []int  // ports of the other apps, and ports something listens on
	Current  string // the release current links to, empty before the first deploy
	Releases []string
	Records  []deployRecord
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
	// Health is the status GET healthPath answered just now, 000 for no answer, empty when it was
	// not asked: only the status script asks, and only an active service.
	Health string
	// PurgeFile says the server has the file of purge tokens, and PurgeZones are the zones it has
	// a token for. The probe prints the names only.
	PurgeFile  bool
	PurgeZones []string
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
			}
		case "listen":
			s.Used = append(s.Used, listeningPorts(value)...)
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

// good reports whether release id passed its health check once, and returns its record.
func (s serverState) good(id string) (deployRecord, bool) {
	for _, r := range slices.Backward(s.Records) {
		if r.ID == id && r.Result == "ok" {
			return r, true
		}
	}
	return deployRecord{}, false
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

// socketUnit holds the port of the app while the program restarts, see docs/deploy.md.
func socketUnit(app string, port int) string {
	return fmt.Sprintf(`# Written by tifa deploy setup, which writes it again when it differs.
[Unit]
Description=%[1]s socket (Tifa)

[Socket]
ListenStream=127.0.0.1:%[2]d

[Install]
WantedBy=sockets.target
`, app, port)
}

// serviceUnit runs the program of the current release in the shared folder, which holds its .env
// and its data. %h is the home of the deploy user.
func serviceUnit(app string) string {
	return fmt.Sprintf(`# Written by tifa deploy setup, which writes it again when it differs.
[Unit]
Description=%[1]s (Tifa)
Requires=%[2]s.socket
After=%[2]s.socket

[Service]
WorkingDirectory=%%h/%[1]s/shared
ExecStart=%%h/%[1]s/current/%[1]s
Restart=on-failure
RestartSec=1s

[Install]
WantedBy=default.target
`, app, unitName(app))
}

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

// setupProbeBody runs as root before the server is prepared. It checks the server has what the
// scripts need, and prints its system.
const setupProbeBody = `set -eu
if [ "$(id -u)" != 0 ]; then echo "tifa: deploy setup needs root on the server" >&2; exit 1; fi
for c in systemctl loginctl runuser useradd curl flock gzip sha256sum ss; do
	command -v "$c" >/dev/null 2>&1 || { echo "tifa: the server has no $c, which tifa deploy needs" >&2; exit 1; }
done
echo "os $(uname -s)"
echo "arch $(uname -m)"
`

// setupProbeScript is setupProbeBody.
func setupProbeScript() string { return setupProbeBody }

// serverBody runs as root. Each step leaves what is already there, so it can run again. It makes
// the deploy user, lets whoever logs in as root log in as it, and starts its systemd without a login.
const serverBody = `set -eu
if ! id -u "$user" >/dev/null 2>&1; then
	useradd --create-home --home-dir "$home" --shell /bin/bash --user-group "$user"
	# No password, and not locked, which sshd refuses with UsePAM no.
	usermod -p '*' "$user"
	echo "created the user $user, home $home"
fi
home=$(getent passwd "$user" | cut -d: -f6)
uid=$(id -u "$user")
# Who may log in as root may deploy: the keys of root go to the deploy user.
install -d -o "$user" -g "$user" -m 700 "$home/.ssh"
keys="$home/.ssh/authorized_keys"
touch "$keys"
chown "$user:$user" "$keys"
chmod 600 "$keys"
if [ -f /root/.ssh/authorized_keys ]; then
	while IFS= read -r key || [ -n "$key" ]; do
		case "$key" in ''|'#'*) continue ;; esac
		grep -qxF -- "$key" "$keys" || printf '%s\n' "$key" >> "$keys"
	done < /root/.ssh/authorized_keys
fi
# Linger starts the systemd of the user at boot, without a login.
loginctl enable-linger "$user"
i=0
while [ ! -S "/run/user/$uid/bus" ]; do
	i=$((i + 1))
	if [ "$i" -gt 30 ]; then echo "tifa: the systemd of $user did not start" >&2; exit 1; fi
	sleep 1
done
`

// serverScript is serverBody for the deploy user.
func serverScript() string {
	return script(serverBody, "user", deployUser, "home", deployHome)
}

// appBody runs as the deploy user, on a server that serverBody prepared. It makes the folder of
// the app, its settings file, its port and its systemd units, and opens the socket. Each step
// leaves what is already there, so it can run again.
const appBody = `set -eu
d="$HOME/$app"
install -d -m 755 "$d" "$d/releases"
install -d -m 750 "$d/shared"
if [ ! -e "$d/shared/.env" ]; then (umask 077; : > "$d/shared/.env"); fi
printf '%s\n' "$port" > "$d/port"
units="$HOME/.config/systemd/user"
install -d -m 755 "$HOME/.config" "$HOME/.config/systemd" "$units"
printf '%s' "$socket_unit" > "$units/tifa-$app.socket"
printf '%s' "$service_unit" > "$units/tifa-$app.service"
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
systemctl --user daemon-reload
systemctl --user enable --now "tifa-$app.socket"
`

// appScript is appBody for app on port.
func appScript(app string, port int) string {
	return script(appBody, "app", app, "port", strconv.Itoa(port),
		"socket_unit", socketUnit(app, port), "service_unit", serviceUnit(app))
}

// probeBody runs as the deploy user and prints what parseServerState reads: the system, whether
// the systemd of the user runs without a login, the app and its service, the other apps and the
// ports taken, the tunnel of the server and the keys of its sshd. With $health set it also asks
// the service for that path, when it runs. Every line is optional, and it never fails.
const probeBody = `d="$HOME/$app"
rt="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
echo "os $(uname -s)"
echo "arch $(uname -m)"
echo "home $HOME"
if [ -e "$rt/bus" ]; then echo "bus yes"; fi
echo "linger $(loginctl show-user "$(id -un)" -p Linger --value 2>/dev/null || echo no)"
if [ -d "$d" ]; then echo "appdir yes"; fi
if [ -f "$d/port" ]; then echo "port $(cat "$d/port")"; fi
for f in "$HOME"/*/port; do
	if [ -f "$f" ] && [ "$f" != "$d/port" ]; then
		echo "taken $(cat "$f")"
		echo "other $(basename "$(dirname "$f")")"
	fi
done
if [ -e "$rt/bus" ] && [ -f "$d/port" ]; then
	# TZ=UTC makes systemd print its times in UTC.
	props=$(XDG_RUNTIME_DIR="$rt" TZ=UTC systemctl --user show "tifa-$app.service" -p ActiveState -p ActiveEnterTimestamp -p MainPID 2>/dev/null)
	state=""
	while IFS='=' read -r k v; do
		case "$k" in
		ActiveState) state=$v; echo "unit-active $v" ;;
		ActiveEnterTimestamp) if [ -n "$v" ]; then echo "unit-since $v"; fi ;;
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
		code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://127.0.0.1:$(cat "$d/port")$health") || code=000
		echo "health $code"
	fi
fi
ss -Hltn 2>/dev/null | sed 's/^/listen /'
u="$HOME/.config/systemd/user"
for n in socket service; do
	if [ -f "$u/tifa-$app.$n" ]; then echo "unit-$n $(sha256sum < "$u/tifa-$app.$n" | cut -d' ' -f1)"; fi
done
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
func probeScript(app string) string { return script(probeBody, "app", app, "health", "") }

// statusScript is probeBody for app that also asks the service for healthPath.
func statusScript(app string) string { return script(probeBody, "app", app, "health", healthPath) }

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

// healthCheckFunc is the shell function that asks the app for healthPath, shared by the scripts that
// restart it. It needs $health, $port, $tries and $unit.
const healthCheckFunc = `check() {
	i=0
	while [ "$i" -lt "$tries" ]; do
		code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://127.0.0.1:$port$health") || code=000
		if [ "$code" -ge 100 ] && [ "$code" -lt 500 ] && systemctl --user is-active --quiet "$unit.service"; then
			echo "health check: GET $health answered $code"
			return 0
		fi
		i=$((i + 1))
		sleep 1
	done
	if [ "$code" = 000 ]; then echo "health check: GET $health got no answer in $tries tries"; else echo "health check: GET $health answered $code"; fi
	return 1
}
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
record() { printf '%s\t%s\t%s\t%s\t%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" "$2" "$action" "$3" >> deploys.log; }
start() {
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
	Tries           int
}

func activateScript(a activation) string {
	return script(activateBody, "app", a.App, "id", a.ID, "commit", a.Commit, "port", strconv.Itoa(a.Port),
		"action", a.Action, "back", a.Back, "prune", strings.Join(a.Prune, " "), "health", healthPath,
		"tries", strconv.Itoa(a.Tries))
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
