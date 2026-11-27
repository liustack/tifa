package main

// tifa deploy harden makes a server harder to get into, once per server and not per app: ssh takes
// keys only, ufw is on when nothing else guards the server, and security updates install by
// themselves. Like setup it reads the server first, prints what it found and a plan, and changes
// nothing until the developer agrees. It can run again at any time.
//
// It changes only a Debian or Ubuntu server (apt). It writes what it changed, and the state from
// before each change, to /etc/tifa/harden, and --undo puts back exactly that and nothing else.
// tifa deploy remove does not undo it: it belongs to the server, not to an app.
//
// What could lock the developer out never runs without a way back. The change to ssh and the
// switch of ufw hold the first ssh session open, then this machine logs in with a new connection.
// When that login fails, or this process dies, the session undoes the change.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

const (
	// opFound is a line of the report that is no change and no step: a fact the plan leaves as it is.
	opFound planOp = "found"

	// hardenRecord is the file on the server that records what harden changed.
	hardenRecord = "/etc/tifa/harden"
	// hardenSSHFile is the file harden writes for sshd. Its name starts with 00: sshd keeps the
	// first value it reads for a keyword, and reads the files of sshd_config.d in the order of
	// their names, so a file such as 50-cloud-init.conf, which many VPS images ship with
	// PasswordAuthentication yes, would win over a later one.
	hardenSSHFile = "/etc/ssh/sshd_config.d/00-tifa-harden.conf"
	// hardenUFWComment marks the rules harden adds to ufw.
	hardenUFWComment = "tifa harden"
	// hardenReady is the line a guarded script prints once it changed the server and waits for
	// the answer of this machine.
	hardenReady = "tifa-harden: changed"
)

// hardenLib starts every script of root. The functions keep the record: recset writes a key once,
// so the state from before is the first one written, recget reads it, and recdel and recdone take
// keys, and the file, away. It needs $record.
const hardenLib = `set -eu
export LC_ALL=C PATH="/usr/sbin:/usr/bin:/sbin:/bin:$PATH" DEBIAN_FRONTEND=noninteractive NEEDRESTART_SUSPEND=1
recset() {
	if [ -f "$record" ] && grep -q "^$1=" "$record"; then return 0; fi
	mkdir -p "$(dirname "$record")"
	if [ ! -f "$record" ]; then
		printf '%s\n' "# Written by tifa deploy harden: what it changed, and the state from before. tifa deploy harden --undo reads it." > "$record"
	fi
	printf '%s=%s\n' "$1" "$2" >> "$record"
}
recget() { if [ -f "$record" ]; then sed -n "s/^$1=//p" "$record" | head -n 1; fi; }
recdel() {
	if [ ! -f "$record" ]; then return 0; fi
	grep -v "^$1=" "$record" > "$record.part" || true
	mv "$record.part" "$record"
}
recdone() {
	if [ -f "$record" ] && ! grep -q '^[a-z_0-9]*=' "$record"; then
		rm -f "$record"
		rmdir "$(dirname "$record")" 2>/dev/null || true
	fi
}
reload_ssh() { systemctl reload ssh 2>/dev/null || systemctl reload sshd; }
apt_install() {
	apt-get update -q </dev/null >/dev/null
	apt-get install -y -q "$@" </dev/null >/dev/null
}
`

// hardenReadBody runs as root and prints what harden needs, one fact per line, a word and a value.
const hardenReadBody = `set -eu
# tifa: harden read
export LC_ALL=C PATH="/usr/sbin:/usr/bin:/sbin:/bin:$PATH"
echo "os-id $(. /etc/os-release >/dev/null 2>&1; echo "${ID:-}")"
echo "os-like $(. /etc/os-release >/dev/null 2>&1; echo "${ID_LIKE:-}")"
if command -v apt-get >/dev/null 2>&1; then echo "apt yes"; fi
echo "machine $(hostname -s)-$(head -c 8 /etc/machine-id 2>/dev/null)"
if out=$(sshd -T 2>&1); then
	printf '%s\n' "$out" | grep -E '^(passwordauthentication|kbdinteractiveauthentication|challengeresponseauthentication|permitrootlogin|port) ' | sed 's/^/sshd /'
else
	echo "sshd-error $(printf '%s' "$out" | head -n 1)"
fi
if [ -f "$ssh_file" ]; then echo "sshd-file yes"; fi
if command -v ufw >/dev/null 2>&1; then
	echo "ufw-installed yes"
	s=$(ufw status verbose 2>&1 || true)
	a=$(ufw show added 2>&1 || true)
	printf '%s\n' "$s" | sed 's/^/ufw-status /'
	printf '%s\n' "$a" | sed 's/^/ufw-added /'
else
	echo "ufw-installed no"
fi
if [ -r /etc/default/ufw ]; then
	echo "ufw-default-in $(sed -n 's/^DEFAULT_INPUT_POLICY="\(.*\)"/\1/p' /etc/default/ufw)"
	echo "ufw-default-out $(sed -n 's/^DEFAULT_OUTPUT_POLICY="\(.*\)"/\1/p' /etc/default/ufw)"
fi
ss -Hltnp | sed 's/^/listen /'
uid=$(id -u tifa 2>/dev/null || true)
if [ -n "$uid" ] && [ -S "/run/user/$uid/bus" ]; then
	runuser -u tifa -- env XDG_RUNTIME_DIR="/run/user/$uid" systemctl --user list-sockets --no-legend --plain 2>/dev/null | sed 's/^/usock /' || true
fi
if [ -f /etc/tifa/nginx ]; then echo "nginx-tifa yes"; fi
for f in /etc/nginx/conf.d/tifa-*.conf; do
	if [ -f "$f" ]; then a=$(basename "$f" .conf); echo "nginx-site ${a#tifa-}"; fi
done
if command -v docker >/dev/null 2>&1; then echo "docker yes"; fi
if command -v fail2ban-client >/dev/null 2>&1; then echo "fail2ban yes"; fi
if [ -e /var/run/reboot-required ]; then echo "reboot yes"; fi
if dpkg-query -W -f='${Status}' unattended-upgrades 2>/dev/null | grep -q 'install ok installed'; then echo "ua-pkg yes"; fi
f=/etc/apt/apt.conf.d/20auto-upgrades
if [ -f "$f" ]; then
	v=$(sed -n 's/^[[:space:]]*APT::Periodic::Unattended-Upgrade[[:space:]]*"\([^"]*\)".*/\1/p' "$f" | tail -n 1)
	echo "ua-conf ${v:-unset}"
else
	echo "ua-conf missing"
fi
t=$(systemctl is-enabled apt-daily-upgrade.timer 2>/dev/null) || true
echo "ua-timer ${t:-unknown}"
if [ -f "$record" ]; then sed 's/^/record /' "$record"; fi
exit 0
`

// hardenSSHBody runs as root. It writes the file of sshd, checks that sshd takes it and that the
// settings are the ones in force, reloads sshd and then waits for the answer of this machine: keep
// when a new login worked. Anything else, an end of input too, undoes it.
const hardenSSHBody = `# tifa: harden ssh
ok=0
undo_ssh() {
	rm -f "$ssh_file" "$ssh_file.part"
	if sshd -t 2>/dev/null; then reload_ssh || true; fi
	recdel sshd_file
	recdone
}
trap 'if [ "$ok" != 1 ]; then undo_ssh; fi' EXIT
mkdir -p "$(dirname "$ssh_file")"
recset sshd_file yes
{
	printf '%s\n' "# Written by tifa deploy harden: ssh takes keys only. tifa deploy harden --undo deletes this file." \
		"# The name starts with 00 because sshd keeps the first value it reads for a keyword, and reads the files" \
		"# of this folder in the order of their names." \
		"PasswordAuthentication no" "KbdInteractiveAuthentication no"
	if [ "$root_login" = yes ]; then printf '%s\n' "PermitRootLogin prohibit-password"; fi
} > "$ssh_file.part"
mv "$ssh_file.part" "$ssh_file"
chmod 644 "$ssh_file"
sshd -t
eff=$(sshd -T)
if ! printf '%s\n' "$eff" | grep -qx 'passwordauthentication no'; then
	echo "tifa: sshd does not read $ssh_file: it has no Include of /etc/ssh/sshd_config.d/*.conf in /etc/ssh/sshd_config, so the file is deleted again" >&2
	exit 1
fi
reload_ssh
echo "` + hardenReady + `"
if IFS= read -r answer && [ "$answer" = keep ]; then ok=1; else
	echo "tifa: the login with a new connection did not work, so the file is deleted and sshd is reloaded" >&2
	exit 1
fi
`

// hardenUFWUndo is the function that puts ufw back as it was before harden, from the record: it
// deletes the rules of harden, turns ufw off, puts back the default policies and uninstalls ufw
// when harden installed it.
const hardenUFWUndo = `undo_ufw() {
	if command -v ufw >/dev/null 2>&1; then
		ufw --force disable >/dev/null || true
		ufw show added | grep "comment '$ufw_comment'" | sed -n 's/^ufw allow \([0-9]*\/tcp\) .*/\1/p' | while IFS= read -r r; do
			ufw --force delete allow "$r" comment "$ufw_comment" >/dev/null || true
		done
		for d in in out; do
			case "$(recget ufw_default_$d)" in
			ACCEPT) ufw default allow $(pick_dir "$d") >/dev/null || true ;;
			DROP) ufw default deny $(pick_dir "$d") >/dev/null || true ;;
			REJECT) ufw default reject $(pick_dir "$d") >/dev/null || true ;;
			esac
		done
	fi
	if [ "$(recget ufw_installed)" = yes ]; then apt-get purge -y -q ufw </dev/null >/dev/null || true; fi
	for k in ufw_before ufw_default_in ufw_default_out ufw_installed; do recdel "$k"; done
	recdone
}
pick_dir() { if [ "$1" = in ]; then echo incoming; else echo outgoing; fi; }
`

// hardenUFWBody runs as root. It installs ufw when it is not there, denies what comes in, allows
// what goes out and the ports in $ports, turns ufw on, and waits for the answer of this machine as
// the ssh script does. Anything but keep undoes it.
const hardenUFWBody = `# tifa: harden ufw
ok=0
` + hardenUFWUndo + `trap 'if [ "$ok" != 1 ]; then undo_ufw; fi' EXIT
recset ufw_before "$before"
if ! command -v ufw >/dev/null 2>&1; then
	recset ufw_installed yes
	apt_install ufw
fi
recset ufw_default_in "$default_in"
recset ufw_default_out "$default_out"
ufw default deny incoming >/dev/null
ufw default allow outgoing >/dev/null
for p in $ports; do
	ufw allow "$p/tcp" comment "$ufw_comment" >/dev/null
done
ufw --force enable >/dev/null
echo "` + hardenReady + `"
if IFS= read -r answer && [ "$answer" = keep ]; then ok=1; else
	echo "tifa: the login with a new connection did not work, so ufw is off again" >&2
	exit 1
fi
`

// hardenUpdatesBody runs as root. $install says unattended-upgrades is to be installed, $conf_before
// what 20auto-upgrades said when it is to be set to 1 (missing, unset or 0), and $timer_before
// what systemd said of apt-daily-upgrade.timer when it is to be enabled.
const hardenUpdatesBody = `# tifa: harden updates
f=/etc/apt/apt.conf.d/20auto-upgrades
if [ "$install" = yes ]; then
	recset ua_installed yes
	apt_install unattended-upgrades
fi
if [ -n "$conf_before" ]; then
	recset ua_conf_before "$conf_before"
	cur=""
	if [ -f "$f" ]; then
		cur=$(sed -n 's/^[[:space:]]*APT::Periodic::Unattended-Upgrade[[:space:]]*"\([^"]*\)".*/\1/p' "$f" | tail -n 1)
	fi
	case "$cur" in
	''|0)
		if [ -n "$cur" ]; then
			sed -i 's/^\([[:space:]]*APT::Periodic::Unattended-Upgrade[[:space:]]*\)"[^"]*"/\1"1"/' "$f"
		else
			if [ ! -f "$f" ]; then printf '%s\n' 'APT::Periodic::Update-Package-Lists "1";' > "$f"; fi
			printf '%s\n' 'APT::Periodic::Unattended-Upgrade "1";' >> "$f"
		fi ;;
	esac
fi
if [ -n "$timer_before" ]; then
	recset ua_timer_before "$timer_before"
	systemctl enable --now apt-daily.timer apt-daily-upgrade.timer >/dev/null 2>&1
fi
echo "automatic security updates are on"
`

// hardenUndoSSHBody runs as root: it deletes the file of sshd and reloads it.
const hardenUndoSSHBody = `# tifa: harden undo ssh
rm -f "$ssh_file" "$ssh_file.part"
sshd -t
reload_ssh
recdel sshd_file
recdone
echo "deleted $ssh_file, and sshd reloaded"
`

// hardenUndoUFWBody runs as root.
const hardenUndoUFWBody = `# tifa: harden undo ufw
` + hardenUFWUndo + `undo_ufw
echo "ufw is as it was before"
`

// hardenUndoUpdatesBody runs as root. It puts back 20auto-upgrades, the timer and the package as
// the record has them.
const hardenUndoUpdatesBody = `# tifa: harden undo updates
f=/etc/apt/apt.conf.d/20auto-upgrades
case "$(recget ua_conf_before)" in
missing) rm -f "$f" ;;
unset) if [ -f "$f" ]; then sed -i '/APT::Periodic::Unattended-Upgrade/d' "$f"; fi ;;
0) if [ -f "$f" ]; then sed -i 's/^\([[:space:]]*APT::Periodic::Unattended-Upgrade[[:space:]]*\)"[^"]*"/\1"0"/' "$f"; fi ;;
esac
case "$(recget ua_timer_before)" in
disabled) systemctl disable --now apt-daily-upgrade.timer >/dev/null 2>&1 || true ;;
esac
if [ "$(recget ua_installed)" = yes ]; then apt-get purge -y -q unattended-upgrades </dev/null >/dev/null || true; fi
for k in ua_conf_before ua_timer_before ua_installed; do recdel "$k"; done
recdone
echo "automatic updates are as they were before"
`

// hardenCheckBody is what the new connection runs: it only has to log in.
const hardenCheckBody = "# tifa: harden check\ntrue\n"

// hardenScript is body for the server: the library of the record, the variables, then body.
func hardenScript(body string, vars ...string) string {
	vars = append(vars, "record", hardenRecord, "ssh_file", hardenSSHFile, "ufw_comment", hardenUFWComment)
	return script(hardenLib, vars...) + body
}

// hardenListener is a TCP port something listens on, on an address visitors may reach.
type hardenListener struct {
	port     int
	programs []string
}

// hardenState is what the root script read of the server.
type hardenState struct {
	osID, osLike string
	apt          bool
	machine      string
	// ssh holds the value of each keyword sshd -T printed, and sshPorts its ports.
	ssh      map[string]string
	sshPorts []int
	sshError string
	sshFile  bool
	// ufwInstalled says ufw is there. ufw is nil when it is not, or its status could not be read:
	// ufwError says why.
	ufwInstalled        bool
	ufw                 *ufwState
	ufwError            string
	ufwDefIn, ufwDefOut string
	listen              []hardenListener
	userSockets         []hardenListener
	nginxTifa           bool
	nginxSites          []string
	docker, fail2ban    bool
	reboot              bool
	uaPkg               bool
	uaConf, uaTimer     string
	// record is what /etc/tifa/harden holds, key to value.
	record map[string]string
	// ufwText is the lines of ufw status and ufw show added the server printed.
	ufwText string
}

// parseHardenState reads the output of hardenReadBody.
func parseHardenState(out string) (hardenState, error) {
	s := hardenState{ssh: map[string]string{}, record: map[string]string{}}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		word, value, _ := strings.Cut(line, " ")
		value = strings.TrimSpace(value)
		switch word {
		case "os-id":
			s.osID = value
		case "os-like":
			s.osLike = value
		case "apt":
			s.apt = true
		case "machine":
			s.machine = value
		case "sshd":
			k, v, _ := strings.Cut(value, " ")
			if k == "port" {
				p, err := strconv.Atoi(v)
				if err != nil {
					return s, fmt.Errorf("sshd gave the port %q", v)
				}
				s.sshPorts = append(s.sshPorts, p)
			} else {
				s.ssh[k] = v
			}
		case "sshd-error":
			s.sshError = value
		case "sshd-file":
			s.sshFile = true
		case "ufw-installed":
			s.ufwInstalled = value == "yes"
		case "ufw-status":
			s.ufwText += line + "\n"
		case "ufw-added":
			s.ufwText += line + "\n"
		case "ufw-default-in":
			s.ufwDefIn = value
		case "ufw-default-out":
			s.ufwDefOut = value
		case "listen":
			if l, ok := parseHardenListener(value); ok {
				s.listen = append(s.listen, l)
			}
		case "usock":
			f := strings.Fields(value)
			if len(f) >= 3 {
				if l, ok := hardenAddress(f[0]); ok {
					s.userSockets = append(s.userSockets, hardenListener{port: l, programs: []string{f[len(f)-1]}})
				}
			}
		case "nginx-tifa":
			s.nginxTifa = true
		case "nginx-site":
			s.nginxSites = append(s.nginxSites, value)
		case "docker":
			s.docker = true
		case "fail2ban":
			s.fail2ban = true
		case "reboot":
			s.reboot = true
		case "ua-pkg":
			s.uaPkg = true
		case "ua-conf":
			s.uaConf = value
		case "ua-timer":
			s.uaTimer = value
		case "record":
			k, v, ok := strings.Cut(value, "=")
			if ok && !strings.HasPrefix(k, "#") {
				s.record[k] = v
			}
		default:
			return s, fmt.Errorf("the server printed a line tifa cannot read: %q", line)
		}
	}
	if s.ufwInstalled {
		u, err := parseUFW(s.ufwText)
		if err != nil {
			s.ufwError = err.Error()
		} else {
			s.ufw = &u
		}
	}
	return s, nil
}

// hardenAddress reads the port of an address as ss and systemd print it, and says it is one visitors
// may reach: not a loopback address.
func hardenAddress(addr string) (port int, public bool) {
	i := strings.LastIndexByte(addr, ':')
	if i < 0 {
		return 0, false
	}
	p, err := strconv.Atoi(addr[i+1:])
	if err != nil {
		return 0, false
	}
	host := strings.Trim(addr[:i], "[]")
	if j := strings.IndexByte(host, '%'); j >= 0 {
		host = host[:j]
	}
	if host == "::1" || host == "localhost" || strings.HasPrefix(host, "127.") {
		return p, false
	}
	return p, true
}

// parseHardenListener reads a line of ss -Hltnp. It returns false for a line without a port and for
// a listener on a loopback address, which nothing outside the server reaches.
func parseHardenListener(line string) (hardenListener, bool) {
	f := strings.Fields(line)
	if len(f) < 4 {
		return hardenListener{}, false
	}
	port, public := hardenAddress(f[3])
	if !public {
		return hardenListener{}, false
	}
	l := hardenListener{port: port}
	for _, m := range ssPrograms.FindAllStringSubmatch(line, -1) {
		name := m[1] + " (pid " + m[2] + ")"
		if !slices.Contains(l.programs, name) {
			l.programs = append(l.programs, name)
		}
	}
	return l, true
}

// named says one of the programs of the listener is called name.
func (l hardenListener) named(name string) bool {
	return slices.ContainsFunc(l.programs, func(p string) bool { return strings.HasPrefix(p, name+" (") })
}

// supported says harden may change the server: Debian or Ubuntu, with apt.
func (s hardenState) supported() bool {
	if !s.apt {
		return false
	}
	for _, w := range append(strings.Fields(s.osLike), s.osID) {
		if w == "debian" || w == "ubuntu" {
			return true
		}
	}
	return false
}

// sshOnlyKeys says sshd takes no password and no keyboard-interactive login.
func (s hardenState) sshOnlyKeys() bool {
	kbd := s.ssh["kbdinteractiveauthentication"]
	if kbd == "" {
		kbd = s.ssh["challengeresponseauthentication"]
	}
	return s.ssh["passwordauthentication"] == "no" && kbd == "no"
}

// visitorPorts are the ports the apps of Tifa on the server serve visitors on: the public ports of
// the sockets of the user tifa (apps with front none), and 80 and 443 when the nginx of Tifa has a
// site.
func (s hardenState) visitorPorts() []int {
	var ports []int
	for _, u := range s.userSockets {
		ports = append(ports, u.port)
	}
	if s.nginxTifa && len(s.nginxSites) > 0 {
		ports = append(ports, nginxPorts...)
	}
	return ports
}

// firewallPlan splits the listeners the server has now: the ports ufw has to allow, with the ports
// of ssh (the ones sshd -T names, the one this machine connected on, and the ones sshd listens
// on) first, and the listeners that are none of ssh, Tifa's sockets or Tifa's nginx, and that no
// rule of ufw allows yet. Such a program may be a service the developer needs: harden names it and
// does not guess.
func (s hardenState) firewallPlan(connected int) (allow []int, unknown []hardenListener) {
	add := func(p int) {
		if p > 0 && !slices.Contains(allow, p) {
			allow = append(allow, p)
		}
	}
	ssh := append([]int(nil), s.sshPorts...)
	for _, l := range s.listen {
		if l.named("sshd") {
			ssh = append(ssh, l.port)
		}
	}
	for _, p := range append(ssh, connected) {
		add(p)
	}
	visitors := s.visitorPorts()
	for _, p := range visitors {
		add(p)
	}
	for _, l := range s.listen {
		if slices.Contains(allow, l.port) || (l.named("nginx") && s.nginxTifa) {
			continue
		}
		if s.ufw != nil {
			if r, ok := s.ufw.rule(l.port); ok && (r.action == "allow" || r.action == "limit") {
				continue
			}
		}
		if i := slices.IndexFunc(unknown, func(u hardenListener) bool { return u.port == l.port }); i >= 0 {
			continue
		}
		unknown = append(unknown, l)
	}
	slices.Sort(allow)
	slices.SortFunc(unknown, func(a, b hardenListener) int { return a.port - b.port })
	return allow, unknown
}

// hardenRun is one harden of one server.
type hardenRun struct {
	t              deployTarget
	stdout, stderr io.Writer
	st             hardenState
	// connected is the port this machine reaches the server by, 0 when it does not know.
	connected int
}

// readHarden asks the server, as root, what harden needs.
func (r *hardenRun) read(ctx context.Context) error {
	var out bytes.Buffer
	if err := r.t.ssh(ctx, "root", script(hardenReadBody, "record", hardenRecord, "ssh_file", hardenSSHFile), nil, &out, r.stderr); err != nil {
		return err
	}
	st, err := parseHardenState(out.String())
	if err != nil {
		return err
	}
	r.st = st
	return nil
}

// connectedPort is the port ssh reaches the server by, as its configuration says: the port of an
// ssh:// host, else what ssh -G prints.
func connectedPort(ctx context.Context, t deployTarget) int {
	if t.dest.port != 0 {
		return t.dest.port
	}
	out, err := exec.CommandContext(ctx, sshProgram, "-G", "--", t.host).Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if word, value, _ := strings.Cut(strings.TrimSpace(line), " "); strings.EqualFold(word, "port") {
			p, _ := strconv.Atoi(value)
			return p
		}
	}
	return 0
}

// hardenPlan reads the server and plans what harden would change. It changes nothing.
func (r *hardenRun) plan(ctx context.Context) (setupPlan, error) {
	p := setupPlan{who: "harden"}
	if err := r.read(ctx); err != nil {
		return p, err
	}
	r.connected = connectedPort(ctx, r.t)
	s := r.st
	if !s.supported() {
		r.reportOnly(&p)
		return p, nil
	}
	r.sshSteps(&p)
	r.firewallSteps(&p)
	r.updateSteps(&p)
	if !s.fail2ban {
		p.notes = append(p.notes, planNote{
			what: "ssh: fail2ban is not installed",
			why:  "With key-only logins a guessed password gets nobody in, so Tifa leaves it out. It still cuts the log noise and the load of login attempts, and bans an address that keeps trying: sudo apt install fail2ban",
		})
	}
	// A step that fails does not stop the others: they do not depend on it.
	for i := range p.steps {
		p.steps[i].soft = true
	}
	return p, nil
}

// reportOnly says what the server has, for a system harden does not change.
func (r *hardenRun) reportOnly(p *setupPlan) {
	s := r.st
	name := s.osID
	if name == "" {
		name = "this system"
	}
	p.add(opFound, fmt.Sprintf("system: %s, which is not Debian or Ubuntu with apt, so harden changes nothing here", name), nil)
	switch {
	case s.sshError != "":
		p.add(opFound, "ssh: sshd -T failed: "+s.sshError, nil)
	case s.sshOnlyKeys():
		p.add(opFound, "ssh: only key logins are accepted", nil)
	default:
		p.add(opFound, "ssh: password logins are accepted. Turn them off with PasswordAuthentication no and KbdInteractiveAuthentication no in the configuration of sshd", nil)
	}
	switch {
	case s.ufw != nil && s.ufw.active:
		p.add(opFound, "firewall: ufw is on", nil)
	case s.ufwInstalled:
		p.add(opFound, "firewall: ufw is installed and off", nil)
	default:
		p.add(opFound, "firewall: ufw is not installed", nil)
	}
	// The steps that are only facts have no do, and cannot fail.
	for i := range p.steps {
		p.steps[i].do = nil
	}
}

// sshSteps plans item 1: ssh takes keys only.
func (r *hardenRun) sshSteps(p *setupPlan) {
	s := r.st
	switch {
	case s.sshError != "":
		p.cannot("ssh: check how sshd is set", "sshd -T failed on the server: "+s.sshError+". Without it harden cannot tell what sshd accepts, and changes nothing there")
		return
	case s.sshOnlyKeys():
		p.ok("ssh: only key logins are accepted")
		return
	}
	rootLogin := pick(s.ssh["permitrootlogin"] == "yes", "yes", "no")
	text := "ssh: take key logins only: write " + hardenSSHFile + " with PasswordAuthentication no and KbdInteractiveAuthentication no"
	if rootLogin == "yes" {
		text += " and PermitRootLogin prohibit-password"
	}
	text += ", check it with sshd -t, reload sshd (open sessions stay) and log in again from this machine with a new connection, which undoes the change if it fails. Anyone who logs in with a password is refused after this: they need a key. The web console of your provider still works, as root"
	p.add(opChange, text, func(ctx context.Context) (string, error) {
		return r.guarded(ctx, hardenScript(hardenSSHBody, "root_login", rootLogin))
	})
}

// firewallSteps plans item 2: ufw, only when it is off or not there.
func (r *hardenRun) firewallSteps(p *setupPlan) {
	s := r.st
	if s.ufwError != "" {
		p.cannot("firewall: read ufw", "ufw is installed, and its status could not be read: "+s.ufwError+". harden changes nothing in it")
		return
	}
	allow, unknown := s.firewallPlan(r.connected)
	if s.ufw != nil && s.ufw.active {
		p.add(opFound, "firewall: ufw is on, so harden leaves its rules as they are", nil)
		var open []int
		for _, port := range s.sshPorts {
			if _, ok := s.ufw.rule(port); !ok && !s.ufw.allowIn {
				open = append(open, port)
			}
		}
		if len(open) > 0 {
			p.notes = append(p.notes, planNote{
				what: "firewall: ufw has no rule for ssh",
				why:  fmt.Sprintf("ufw is on and no rule allows %s, so a new ssh connection may be refused. Allow it before this server restarts: sudo ufw allow %d/tcp", portList(open), open[0]),
			})
		}
		r.dockerNote(p)
		return
	}
	if len(unknown) > 0 {
		var names []string
		for _, l := range unknown {
			who := "a program ss cannot name"
			if len(l.programs) > 0 {
				who = strings.Join(l.programs, ", ")
			}
			names = append(names, fmt.Sprintf("%s listens on port %d", who, l.port))
		}
		p.cannot("firewall: turn ufw on",
			strings.Join(names, ". ")+". It is not sshd, a socket of Tifa or the nginx of Tifa, and harden does not guess whether it should stay open, so ufw stays off and nothing changed. Allow its port in ufw (sudo ufw allow <port>/tcp) or stop the program, then run harden again")
		r.dockerNote(p)
		return
	}
	text := "firewall: "
	if !s.ufwInstalled {
		text += "install ufw with apt, then "
	}
	text += fmt.Sprintf("turn it on: deny what comes in, allow what goes out, and allow %s (tcp) in rules with the comment '%s'. The ports are those of ssh and of the apps of Tifa that serve visitors. Existing connections stay", portList(allow), hardenUFWComment)
	ports := make([]string, len(allow))
	for i, a := range allow {
		ports[i] = strconv.Itoa(a)
	}
	before := pick(s.ufwInstalled, "inactive", "absent")
	p.add(opChange, text, func(ctx context.Context) (string, error) {
		return r.guarded(ctx, hardenScript(hardenUFWBody, "ports", strings.Join(ports, " "), "before", before,
			"default_in", s.ufwDefIn, "default_out", s.ufwDefOut))
	})
	r.dockerNote(p)
}

// dockerNote says what ufw does not do on a server with Docker.
func (r *hardenRun) dockerNote(p *setupPlan) {
	if r.st.docker {
		p.notes = append(p.notes, planNote{
			what: "firewall: Docker is installed",
			why:  "ufw does not filter the ports Docker publishes: Docker writes its own rules before those of ufw. Publish a port on 127.0.0.1 (-p 127.0.0.1:8080:80) when only this server should reach it",
		})
	}
}

// updateSteps plans item 3: automatic security updates, and tells of a reboot that waits.
func (r *hardenRun) updateSteps(p *setupPlan) {
	s := r.st
	if s.reboot {
		p.add(opFound, "updates: the server waits for a reboot (/var/run/reboot-required) to finish updates it installed. Reboot it when it suits you: harden never does", nil)
	}
	switch s.uaTimer {
	case "masked", "not-found", "unknown":
		p.cannot("updates: turn on automatic security updates",
			"apt-daily-upgrade.timer is "+strings.ReplaceAll(s.uaTimer, "-", " ")+" on the server, so unattended-upgrades would never run. Unmask or install it (systemctl unmask apt-daily-upgrade.timer), then run harden again")
		return
	}
	install := !s.uaPkg
	confBad := s.uaConf == "missing" || s.uaConf == "unset" || s.uaConf == "0" || s.uaConf == ""
	timerBad := s.uaTimer != "enabled"
	if !install && !confBad && !timerBad {
		p.ok("updates: unattended-upgrades is installed and on")
		return
	}
	var parts []string
	if install {
		parts = append(parts, "install unattended-upgrades")
	}
	conf := ""
	if confBad {
		conf = s.uaConf
		switch s.uaConf {
		case "0":
			parts = append(parts, "set Unattended-Upgrade to 1 in /etc/apt/apt.conf.d/20auto-upgrades, where it is 0 now (set to 0 by someone on purpose, perhaps)")
		case "unset":
			parts = append(parts, "add Unattended-Upgrade 1 to /etc/apt/apt.conf.d/20auto-upgrades, which does not set it")
		default:
			conf = "missing"
			parts = append(parts, "write /etc/apt/apt.conf.d/20auto-upgrades with Unattended-Upgrade 1")
		}
	}
	timer := ""
	if timerBad {
		timer = s.uaTimer
		parts = append(parts, "enable apt-daily-upgrade.timer, which is "+s.uaTimer)
	}
	text := "updates: " + strings.Join(parts, ", ") + ", so security updates install by themselves. It never reboots the server"
	p.add(opChange, text, func(ctx context.Context) (string, error) {
		return r.run(ctx, hardenScript(hardenUpdatesBody, "install", pick(install, "yes", "no"), "conf_before", conf, "timer_before", timer))
	})
}

// run runs a script as root and says what it printed, a line to a phrase.
func (r *hardenRun) run(ctx context.Context, body string) (string, error) {
	var out bytes.Buffer
	err := r.t.ssh(ctx, "root", body, nil, &out, r.stderr)
	said := strings.ReplaceAll(strings.TrimSpace(out.String()), "\n", ", ")
	if err != nil && said != "" {
		return "", fmt.Errorf("%w: %s", err, said)
	}
	return said, err
}

// readyWriter collects the output of a guarded script and closes ready when the line that says it
// changed the server is there.
type readyWriter struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	ready chan struct{}
	seen  bool
}

func (w *readyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	if !w.seen && strings.Contains(w.buf.String(), hardenReady+"\n") {
		w.seen = true
		close(w.ready)
	}
	return len(p), nil
}

// said is what the script printed, without the line that tells it waits.
func (w *readyWriter) said() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.TrimSpace(strings.ReplaceAll(w.buf.String(), hardenReady+"\n", ""))
}

// answer is the input of a guarded script: nothing until this machine knows, then one line.
type answer struct {
	ch  chan string
	buf []byte
	got bool
}

func (a *answer) Read(p []byte) (int, error) {
	if !a.got {
		s, ok := <-a.ch
		a.got = true
		if ok {
			a.buf = []byte(s)
		}
	}
	if len(a.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, a.buf)
	a.buf = a.buf[n:]
	return n, nil
}

// check logs in to the server with a connection of its own, as root or the admin user with sudo,
// and returns what ssh said when that fails. ssh never reuses a connection that is open: a login
// that works through one proves nothing about the keys.
func (r *hardenRun) check(ctx context.Context) error {
	t := r.t
	t.newConn = true
	var said tailWriter
	if err := t.ssh(ctx, "root", hardenCheckBody, nil, io.Discard, &said); err != nil {
		if s := strings.TrimSpace(said.String()); s != "" {
			return fmt.Errorf("%w (ssh said: %s)", err, s)
		}
		return err
	}
	return nil
}

// guarded runs a script that changes the server and then waits for this machine, on a session that
// stays open. When a login with a new connection works it answers keep and the change stays. When
// it fails it answers undo, and the script puts the server back through the session that is
// still open. A script that ends before it changed anything just failed.
func (r *hardenRun) guarded(ctx context.Context, body string) (string, error) {
	in := &answer{ch: make(chan string, 1)}
	out := &readyWriter{ready: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- r.t.ssh(ctx, "root", body, in, out, r.stderr) }()
	select {
	case err := <-done:
		close(in.ch)
		said := out.said()
		if err != nil && said != "" {
			return "", fmt.Errorf("%w: %s", err, said)
		}
		return said, err
	case <-out.ready:
	}
	checkErr := r.check(ctx)
	in.ch <- pick(checkErr == nil, "keep\n", "undo\n")
	close(in.ch)
	err := <-done
	switch {
	case checkErr != nil:
		return "", fmt.Errorf("a new ssh login from this machine failed after the change, so the change was undone and the server is as it was: %v", checkErr)
	case err != nil:
		return "", err
	}
	return out.said(), nil
}

// undoPlan plans what --undo does, from the record: each item back to the state before.
func (r *hardenRun) undoPlan(ctx context.Context) (setupPlan, error) {
	p := setupPlan{who: "harden --undo"}
	if err := r.read(ctx); err != nil {
		return p, err
	}
	rec := r.st.record
	has := func(keys ...string) bool {
		for _, k := range keys {
			if _, ok := rec[k]; ok {
				return true
			}
		}
		return false
	}
	if has("sshd_file") {
		p.add(opRemove, "ssh: delete "+hardenSSHFile+", which harden wrote, and reload sshd. Password logins come back as they were. Then log in again from this machine with a new connection", func(ctx context.Context) (string, error) {
			res, err := r.run(ctx, hardenScript(hardenUndoSSHBody))
			if err != nil {
				return "", err
			}
			if cerr := r.check(ctx); cerr != nil {
				return "", fmt.Errorf("%s, but a new ssh login from this machine failed, which something other than harden causes: %v", res, cerr)
			}
			return res, nil
		})
	}
	if has("ufw_before", "ufw_installed", "ufw_default_in", "ufw_default_out") {
		text := "firewall: turn ufw off, delete its rules with the comment '" + hardenUFWComment + "' and put back its default policies"
		if rec["ufw_installed"] == "yes" {
			text += ", and uninstall ufw, which harden installed"
		}
		p.add(opRemove, text, func(ctx context.Context) (string, error) {
			return r.run(ctx, hardenScript(hardenUndoUFWBody))
		})
	}
	if has("ua_conf_before", "ua_timer_before", "ua_installed") {
		var parts []string
		switch rec["ua_conf_before"] {
		case "missing":
			parts = append(parts, "delete /etc/apt/apt.conf.d/20auto-upgrades")
		case "unset":
			parts = append(parts, "take Unattended-Upgrade out of /etc/apt/apt.conf.d/20auto-upgrades")
		case "0":
			parts = append(parts, "set Unattended-Upgrade back to 0 in /etc/apt/apt.conf.d/20auto-upgrades")
		}
		if rec["ua_timer_before"] == "disabled" {
			parts = append(parts, "disable apt-daily-upgrade.timer")
		}
		if rec["ua_installed"] == "yes" {
			parts = append(parts, "uninstall unattended-upgrades, which harden installed")
		}
		if len(parts) == 0 {
			parts = append(parts, "drop the record of it")
		}
		p.add(opRemove, "updates: "+strings.Join(parts, ", "), func(ctx context.Context) (string, error) {
			return r.run(ctx, hardenScript(hardenUndoUpdatesBody))
		})
	}
	for i := range p.steps {
		p.steps[i].soft = true
	}
	return p, nil
}

// hardenFlags are the flags of tifa deploy harden.
type hardenFlags struct {
	yes, undo bool
}

// hardenOne reads, prints the plan, asks and applies, for one server. It returns how many things
// are left to the developer.
func hardenOne(ctx context.Context, t deployTarget, f hardenFlags, in io.Reader, stdout, stderr io.Writer) (int, error) {
	r := &hardenRun{t: t, stdout: stdout, stderr: stderr}
	fmt.Fprintf(stderr, "Reading %s...\n", t.host)
	var p setupPlan
	var err error
	title := "Plan to harden " + t.host
	if f.undo {
		title = "Plan to undo what harden did on " + t.host
		p, err = r.undoPlan(ctx)
		if err == nil && p.todo() == 0 {
			fmt.Fprintf(stdout, "Nothing of harden to undo on %s: it left no record in %s.\n", t.host, hardenRecord)
			return 0, nil
		}
	} else {
		p, err = r.plan(ctx)
	}
	if err != nil {
		return 0, fmt.Errorf("%s: %w", t.host, err)
	}
	if !f.undo && !r.st.supported() {
		title = "Found on " + t.host
	}
	p.print(stdout, title, true)
	cannot := p.count(opCannot)
	if p.todo() == 0 {
		return cannot, nil
	}
	if err := confirm(in, stdout, f.yes); err != nil {
		return 0, err
	}
	warnings, err := p.apply(ctx, stdout)
	if err != nil {
		return 0, err
	}
	return cannot + warnings, nil
}

// deployHarden runs harden on each server in turn.
func deployHarden(ctx context.Context, hosts []deployHost, f hardenFlags, in io.Reader, stdout, stderr io.Writer) error {
	left := 0
	var failed []string
	for _, h := range hosts {
		t := deployTarget{host: h.addr(), dest: h}
		n, err := hardenOne(ctx, t, f, in, stdout, stderr)
		var exit *exitError
		switch {
		case errors.As(err, &exit):
			return err
		case err != nil && len(hosts) == 1:
			return err
		case err != nil:
			fmt.Fprintf(stderr, "%v\n", err)
			failed = append(failed, h.raw)
		}
		left += n
	}
	if len(failed) > 0 {
		return fmt.Errorf("harden did not reach %s", strings.Join(failed, ", "))
	}
	return needsYou(left)
}

// hardenHosts finds the servers to harden: each --host, else TIFA_DEPLOY_HOST of the environment or
// of the .env in dir, else the only server of ~/.tifa.
func hardenHosts(dir string, flagHosts []string) ([]deployHost, error) {
	var hosts []deployHost
	add := func(raw, from string) error {
		h, err := parseDeployHost(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", from, err)
		}
		h.from = from
		if !slices.ContainsFunc(hosts, func(o deployHost) bool { return o.raw == h.raw }) {
			hosts = append(hosts, h)
		}
		return nil
	}
	for _, raw := range flagHosts {
		if err := add(raw, "--host"); err != nil {
			return nil, err
		}
	}
	if len(hosts) > 0 {
		return hosts, nil
	}
	value, from, err := readDeployInput(dir, "--host", "", deployHostEnv)
	if err != nil {
		return nil, err
	}
	if value != "" {
		return hosts, add(value, from)
	}
	servers, err := readServers()
	if err != nil {
		return nil, fmt.Errorf("no server is named, and ~/.tifa could not be read: %w", err)
	}
	switch len(servers) {
	case 0:
		return nil, noServerError()
	case 1:
		if err := add(servers[0].Host, shownServersPath()); err != nil {
			return nil, err
		}
		h := &hosts[0]
		h.fromHome, h.knownID = true, servers[0].ID
		return hosts, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "no server is named, and %s lists several:\n", shownServersPath())
	for _, s := range servers {
		fmt.Fprintf(&b, "  %s  %s\n", s.name(), s.Host)
	}
	b.WriteString("Name each one to harden with --host")
	return nil, errors.New(b.String())
}

func newDeployHardenCmd(dir *string) *cobra.Command {
	var f hardenFlags
	var hosts []string
	cmd := &cobra.Command{
		Use:   "harden",
		Short: "Make the server harder to get into: ssh keys only, ufw, automatic security updates",
		Long: `harden reads a server, prints what it found and a plan, asks, and applies it. It works on the server,
not on an app, and on each server you name with --host (repeat it for several). Without --host it uses
TIFA_DEPLOY_HOST, else the only server of ~/.tifa. It logs in as root, or as the user in front of the
host with sudo, as setup does. It changes only Debian and Ubuntu (apt) and can run again at any time.

  ssh        key logins only: a file in /etc/ssh/sshd_config.d, then a login with a new connection from
             this machine. When that fails, the file goes and ssh is as it was
  ufw        when it is off or not installed: deny what comes in, allow ssh and the apps of Tifa that
             serve visitors. It does not turn ufw on when a program listens on a port it cannot account
             for, and names it. When ufw is on already, harden leaves it alone
  updates    unattended-upgrades installed and on. It never reboots, and says when a reboot waits
  advice     fail2ban, when it is not installed. Tifa does not install it

What it changed, and the state from before, is in /etc/tifa/harden on the server. --undo puts back
exactly that. On a terminal harden asks before it changes anything. Without a terminal, pass --yes.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			hs, err := hardenHosts(*dir, hosts)
			if err != nil {
				return err
			}
			return deployHarden(cmd.Context(), hs, f, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	// A flag of its own: unlike the --host of the other commands it may repeat.
	cmd.Flags().StringArrayVar(&hosts, "host", nil, "the server to harden: an ssh alias or ssh://host[:port], with user@ in front when root cannot log in. Repeat it for several")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "apply the plan without asking")
	cmd.Flags().BoolVar(&f.undo, "undo", false, "put back what harden changed, from the record on the server")
	return cmd
}
