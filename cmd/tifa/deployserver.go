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
//
// Every script runs under sh -c, so the login shell of the user does not matter.

import (
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

// serverState is what the probe script prints about an app.
type serverState struct {
	OS, Arch string
	Port     int    // 0 before setup
	Current  string // the release current links to, empty before the first deploy
	Releases []string
	Records  []deployRecord
}

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
		case "port":
			p, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || p <= 0 || p > 65535 {
				return s, fmt.Errorf("the server gave the port %q", value)
			}
			s.Port = p
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
	return fmt.Sprintf(`# Written by tifa deploy setup, which writes it again each time it runs.
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
	return fmt.Sprintf(`# Written by tifa deploy setup, which writes it again each time it runs.
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

// setupProbeBody runs as root before setup. It checks the server has what the units and the
// scripts need, and prints the system, the home of the deploy user if it exists, the port of the
// app if it has one, the ports of the other apps and the ports something listens on.
const setupProbeBody = `set -eu
if [ "$(id -u)" != 0 ]; then echo "tifa: deploy setup needs root on the server" >&2; exit 1; fi
for c in systemctl loginctl runuser useradd curl flock gzip sha256sum ss; do
	command -v "$c" >/dev/null 2>&1 || { echo "tifa: the server has no $c, which tifa deploy needs" >&2; exit 1; }
done
echo "os $(uname -s)"
echo "arch $(uname -m)"
home=$(getent passwd "$user" | cut -d: -f6)
if [ -n "$home" ]; then
	echo "home $home"
	if [ -f "$home/$app/port" ]; then echo "port $(cat "$home/$app/port")"; fi
	for f in "$home"/*/port; do
		if [ -f "$f" ]; then echo "taken $(cat "$f")"; fi
	done
fi
ss -Hltn | sed 's/^/listen /'
`

// setupProbeScript is setupProbeBody for app.
func setupProbeScript(app string) string {
	return script(setupProbeBody, "user", deployUser, "app", app)
}

// setupBody runs as root. Each step leaves what is already there, so it can run again.
const setupBody = `set -eu
if ! id -u "$user" >/dev/null 2>&1; then
	useradd --create-home --home-dir "$home" --shell /bin/bash --user-group "$user"
	# No password, and not locked, which sshd refuses with UsePAM no.
	usermod -p '*' "$user"
	echo "created the user $user, home $home"
fi
home=$(getent passwd "$user" | cut -d: -f6)
uid=$(id -u "$user")
d="$home/$app"
install -d -o "$user" -g "$user" -m 755 "$d" "$d/releases"
install -d -o "$user" -g "$user" -m 750 "$d/shared"
if [ ! -e "$d/shared/.env" ]; then install -o "$user" -g "$user" -m 600 /dev/null "$d/shared/.env"; fi
printf '%s\n' "$port" > "$d/port"
chown "$user:$user" "$d/port"
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
units="$home/.config/systemd/user"
install -d -o "$user" -g "$user" "$home/.config" "$home/.config/systemd" "$units"
printf '%s' "$socket_unit" > "$units/tifa-$app.socket"
printf '%s' "$service_unit" > "$units/tifa-$app.service"
chown "$user:$user" "$units/tifa-$app.socket" "$units/tifa-$app.service"
# Linger starts the systemd of the user at boot, without a login.
loginctl enable-linger "$user"
i=0
while [ ! -S "/run/user/$uid/bus" ]; do
	i=$((i + 1))
	if [ "$i" -gt 30 ]; then echo "tifa: the systemd of $user did not start" >&2; exit 1; fi
	sleep 1
done
as_user() { runuser -u "$user" -- env XDG_RUNTIME_DIR="/run/user/$uid" "$@"; }
as_user systemctl --user daemon-reload
as_user systemctl --user enable --now "tifa-$app.socket"
echo "home $home"
`

// setupScript is setupBody for app on port.
func setupScript(app string, port int) string {
	return script(setupBody, "user", deployUser, "home", deployHome, "app", app, "port", strconv.Itoa(port),
		"socket_unit", socketUnit(app, port), "service_unit", serviceUnit(app))
}

// probeBody runs as the deploy user and prints what parseServerState reads.
const probeBody = `d="$HOME/$app"
echo "os $(uname -s)"
echo "arch $(uname -m)"
if [ -f "$d/port" ]; then echo "port $(cat "$d/port")"; fi
if c=$(readlink "$d/current" 2>/dev/null); then echo "current ${c#releases/}"; fi
for r in "$d"/releases/*/; do
	if [ -d "$r" ]; then echo "release $(basename "$r")"; fi
done
if [ -f "$d/deploys.log" ]; then tail -n 200 "$d/deploys.log" | sed 's/^/record /'; fi
exit 0
`

// probeScript is probeBody for app.
func probeScript(app string) string { return script(probeBody, "app", app) }

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
check() {
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
echo "starting $id"
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
