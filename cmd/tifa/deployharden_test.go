package main

// tifa deploy harden is tested against a fake ssh that stands for the server: it answers each script
// of root by the marker on its first line, keeps the facts it prints in a file, writes down each
// call, and plays the guarded scripts, which wait for the answer of this machine, as the real ones do.

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
)

type hardenServer struct{ dir string }

// newHardenServer makes the fake ssh with the facts the server prints for the read of harden.
func newHardenServer(t *testing.T, facts string) *hardenServer {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake ssh is a shell script")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "cfg"), "hostname vps\nport 22\n")
	writeFile(t, filepath.Join(dir, "facts"), facts)
	prog := filepath.Join(dir, "ssh")
	writeFile(t, prog, `#!/bin/sh
d=`+shellQuote(dir)+`
if [ "$1" = -G ]; then cat "$d/cfg"; exit 0; fi
printf '%s\n' "$*" >> "$d/argv"
user=$2
while [ "$1" != "--" ]; do shift; done
script=$3
printf '%s\n----\n' "$script" >> "$d/scripts"
case "$script" in
*"tifa: harden read"*) echo "read $user" >> "$d/calls"; cat "$d/facts" ;;
*"tifa: harden check"*) echo "check $user" >> "$d/calls"; [ -f "$d/checkfail" ] && { echo "root@vps: Permission denied (publickey)." >&2; exit 255; }; true ;;
*"tifa: harden ssh"*|*"tifa: harden ufw"*)
	case "$script" in *"harden ssh"*) w=ssh ;; *) w=ufw ;; esac
	echo "$w-apply $user" >> "$d/calls"
	echo "tifa-harden: changed"
	IFS= read -r a || a=eof
	echo "$w-answer $a" >> "$d/calls"
	[ "$a" = keep ] || { echo "tifa: the login with a new connection did not work" >&2; exit 1; } ;;
*"tifa: harden updates"*) echo "updates-apply $user" >> "$d/calls"; echo "automatic security updates are on" ;;
*"tifa: harden undo ssh"*) echo "undo-ssh $user" >> "$d/calls"; echo "deleted the file" ;;
*"tifa: harden undo ufw"*) echo "undo-ufw $user" >> "$d/calls"; echo "ufw is as it was before" ;;
*"tifa: harden undo updates"*) echo "undo-updates $user" >> "$d/calls"; echo "automatic updates are as they were before" ;;
*) echo "other $user" >> "$d/calls" ;;
esac
`)
	if err := os.Chmod(prog, 0o755); err != nil {
		t.Fatal(err)
	}
	old := sshProgram
	sshProgram = prog
	t.Cleanup(func() { sshProgram = old })
	return &hardenServer{dir: dir}
}

func (s *hardenServer) calls() string {
	data, _ := os.ReadFile(filepath.Join(s.dir, "calls"))
	return string(data)
}
func (s *hardenServer) scripts() string {
	data, _ := os.ReadFile(filepath.Join(s.dir, "scripts"))
	// ssh gets the script inside sh -c '...', its quotes written as '\'' .
	return strings.ReplaceAll(string(data), `'\''`, `'`)
}
func (s *hardenServer) argv() string {
	data, _ := os.ReadFile(filepath.Join(s.dir, "argv"))
	return string(data)
}
func (s *hardenServer) failCheck(t *testing.T) { writeFile(t, filepath.Join(s.dir, "checkfail"), "") }

// Facts of an Ubuntu server as a VPS image leaves it: sshd takes passwords, ufw is not installed,
// only sshd listens, and unattended-upgrades is on.
const hardenFacts = `os-id ubuntu
os-like debian
apt yes
machine vps-1234
sshd passwordauthentication yes
sshd kbdinteractiveauthentication no
sshd permitrootlogin yes
sshd port 22
ufw-installed no
listen LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=801,fd=3))
listen LISTEN 0 128 [::]:22 [::]:* users:(("sshd",pid=801,fd=4))
listen LISTEN 0 4096 127.0.0.53%lo:53 0.0.0.0:* users:(("systemd-resolve",pid=500,fd=14))
listen LISTEN 0 4096 127.0.0.1:9527 0.0.0.0:* users:(("shop",pid=900,fd=3))
ua-pkg yes
ua-conf 1
ua-timer enabled
fail2ban yes
`

// runHarden runs tifa deploy harden with the terminal state and the flags.
func runHarden(t *testing.T, terminal bool, input string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	old := stdinIsTerminal
	t.Cleanup(func() { stdinIsTerminal = old })
	stdinIsTerminal = func(io.Reader) bool { return terminal }
	var out, errOut bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(append([]string{"deploy", "harden"}, args...))
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

// factsWith is hardenFacts with the lines of drop taken out and those of add put in.
func factsWith(add string, drop ...string) string {
	var keep []string
	for _, l := range strings.Split(strings.TrimSuffix(hardenFacts, "\n"), "\n") {
		skip := false
		for _, d := range drop {
			skip = skip || strings.HasPrefix(l, d)
		}
		if !skip {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n") + "\n" + add
}

// Each state of the server has its line in the plan, and nothing runs without the agreement: without a
// terminal that is --yes.
func TestHardenPlanAndConsent(t *testing.T) {
	srv := newHardenServer(t, hardenFacts)
	out, _, err := runHarden(t, false, "", "--host", "vps")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("got %v\n%s", err, out)
	}
	for _, want := range []string{
		"Plan to harden vps",
		"change  ssh: take key logins only: write /etc/ssh/sshd_config.d/00-tifa-harden.conf with PasswordAuthentication no and KbdInteractiveAuthentication no and PermitRootLogin prohibit-password",
		"Anyone who logs in with a password is refused",
		"The web console of your provider still works",
		"change  firewall: install ufw with apt, then turn it on: deny what comes in and allow what goes out, and allow port 22 (tcp) in rules with the comment 'tifa harden'",
		"ok      updates: unattended-upgrades is installed and on",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "fail2ban") {
		t.Errorf("advises fail2ban, which is installed:\n%s", out)
	}
	if strings.Contains(srv.calls(), "apply") {
		t.Fatalf("changed something without consent:\n%s", srv.calls())
	}

	// On a terminal, an answer other than yes changes nothing.
	_, _, err = runHarden(t, true, "n\n", "--host", "vps")
	if err == nil || !strings.Contains(err.Error(), "nothing was changed") {
		t.Errorf("got %v", err)
	}
	if strings.Contains(srv.calls(), "apply") {
		t.Fatalf("changed something after no:\n%s", srv.calls())
	}
}

// fail2ban is advice only, once, and only when it is absent.
func TestHardenAdvisesFail2ban(t *testing.T) {
	newHardenServer(t, factsWith("", "fail2ban"))
	out, _, _ := runHarden(t, false, "", "--host", "vps")
	if strings.Count(out, "fail2ban is not installed") != 1 || !strings.Contains(out, "sudo apt install fail2ban") {
		t.Errorf("advice:\n%s", out)
	}
}

// The change to ssh is one file whose name sorts before every other, with the settings in force
// checked before the reload, and a login with a new connection before it stays. The session of the
// change is held open until that login answers.
func TestHardenWritesTheSSHFile(t *testing.T) {
	srv := newHardenServer(t, hardenFacts)
	out, _, err := runHarden(t, false, "", "--host", "vps", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	calls := srv.calls()
	order := []string{"read root", "ssh-apply root", "check root", "ssh-answer keep", "ufw-apply root", "check root", "ufw-answer keep"}
	pos := 0
	for _, want := range order {
		i := strings.Index(calls[pos:], want)
		if i < 0 {
			t.Fatalf("calls lack %q in order:\n%s", want, calls)
		}
		pos += i + len(want)
	}
	sc := srv.scripts()
	for _, want := range []string{
		"ssh_file='/etc/ssh/sshd_config.d/00-tifa-harden.conf'",
		`"PasswordAuthentication no" "KbdInteractiveAuthentication no"`,
		`if [ "$root_login" = yes ]`,
		"root_login='yes'",
		"sshd -t",
		"sshd -T",
		"ufw allow \"$p/tcp\" comment \"$ufw_comment\"",
		"ports='22'",
	} {
		if !strings.Contains(sc, want) {
			t.Errorf("the scripts lack %q", want)
		}
	}
	if !strings.Contains(out, "done    ssh: take key logins only") || !strings.Contains(out, "done    firewall:") {
		t.Errorf("output:\n%s", out)
	}
	// The check never rides on a connection that is open.
	var checks []string
	for _, l := range strings.Split(srv.argv(), "\n") {
		if strings.Contains(l, "harden check") {
			checks = append(checks, l)
		}
	}
	if len(checks) != 2 {
		t.Fatalf("checks: %v", checks)
	}
	for _, l := range checks {
		if !strings.Contains(l, "ControlPath=none") || !strings.Contains(l, "BatchMode=yes") {
			t.Errorf("the check may reuse a connection: %s", l)
		}
	}
}

// PermitRootLogin only changes when the value in force is yes.
func TestHardenLeavesRootLoginAlone(t *testing.T) {
	srv := newHardenServer(t, strings.Replace(hardenFacts, "permitrootlogin yes", "permitrootlogin prohibit-password", 1))
	if out, _, err := runHarden(t, false, "", "--host", "vps", "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(srv.scripts(), "root_login='no'") {
		t.Errorf("scripts:\n%s", srv.scripts())
	}
}

// When the new login fails, the session that is still open is told to undo, the failure says what ssh
// said, and the command fails. The other steps still run.
func TestHardenRollsBackWhenTheNewLoginFails(t *testing.T) {
	srv := newHardenServer(t, hardenFacts)
	srv.failCheck(t)
	out, _, err := runHarden(t, false, "", "--host", "vps", "--yes")
	if err == nil {
		t.Fatalf("no error\n%s", out)
	}
	if !strings.Contains(srv.calls(), "ssh-answer undo") || strings.Contains(srv.calls(), "ssh-answer keep") {
		t.Errorf("calls:\n%s", srv.calls())
	}
	for _, want := range []string{"warning ssh: take key logins only", "the server is as it was", "Permission denied (publickey)"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
}

// An already key-only server has nothing to do for ssh, and with all else right nothing runs.
func TestHardenAlreadyRight(t *testing.T) {
	srv := newHardenServer(t, factsWith("ufw-installed yes\nufw-status Status: active\nufw-status Default: deny (incoming), allow (outgoing), disabled (routed)\nufw-added ufw allow 22/tcp\n",
		"sshd passwordauthentication", "ufw-installed")+"sshd passwordauthentication no\n")
	out, _, err := runHarden(t, false, "", "--host", "vps")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"ok      ssh: only key logins are accepted", "found   firewall: ufw is on, so harden leaves its rules as they are", "ok      updates:"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(srv.calls(), "apply") || strings.Contains(out, "Apply this plan") {
		t.Errorf("changed or asked:\n%s\n%s", srv.calls(), out)
	}
}

// ufw on already: report only, with no change to it, and a word when no rule allows ssh.
func TestHardenReportsActiveUFW(t *testing.T) {
	srv := newHardenServer(t, factsWith("ufw-installed yes\nufw-status Status: active\nufw-status Default: deny (incoming), allow (outgoing), disabled (routed)\nufw-added ufw allow 443/tcp\n", "ufw-installed"))
	out, _, _ := runHarden(t, false, "", "--host", "vps")
	if !strings.Contains(out, "found   firewall: ufw is on, so harden leaves its rules as they are") {
		t.Errorf("output:\n%s", out)
	}
	if !strings.Contains(out, "ufw has no rule for ssh") || !strings.Contains(out, "sudo ufw allow 22/tcp") {
		t.Errorf("no word on ssh:\n%s", out)
	}
	if strings.Contains(out, "change  firewall") {
		t.Errorf("plans to change ufw:\n%s", out)
	}
	_, _, _ = runHarden(t, false, "", "--host", "vps", "--yes")
	if strings.Contains(srv.calls(), "ufw-apply") {
		t.Errorf("changed ufw:\n%s", srv.calls())
	}
}

// A listener that is not sshd, a socket of Tifa or the nginx of Tifa stops ufw: harden names it, does
// not turn ufw on, and says what to do. The rest of the plan still runs. A rule that allows its
// port, or a listener on a loopback address, is no reason to stop.
func TestHardenRefusesUFWForAnUnknownListener(t *testing.T) {
	srv := newHardenServer(t, factsWith(`listen LISTEN 0 4096 0.0.0.0:5432 0.0.0.0:* users:(("postgres",pid=77,fd=5))
listen LISTEN 0 4096 [::]:5432 [::]:* users:(("postgres",pid=77,fd=6))
listen LISTEN 0 4096 127.0.0.1:6379 0.0.0.0:* users:(("redis-server",pid=78,fd=6))
`))
	out, _, err := runHarden(t, false, "", "--host", "vps", "--yes")
	if err == nil {
		t.Fatalf("no error\n%s", out)
	}
	for _, want := range []string{"postgres (pid 77) listens on port 5432", "ufw stays off and nothing changed", "sudo ufw allow <port>/tcp"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "redis") {
		t.Errorf("names a loopback listener:\n%s", out)
	}
	if strings.Contains(srv.calls(), "ufw-apply") {
		t.Errorf("turned ufw on:\n%s", srv.calls())
	}
	if !strings.Contains(srv.calls(), "ssh-apply") {
		t.Errorf("the ssh step did not run:\n%s", srv.calls())
	}

	// With ufw off but a rule that allows 5432 the program is accounted for.
	srv = newHardenServer(t, factsWith(`listen LISTEN 0 4096 0.0.0.0:5432 0.0.0.0:* users:(("postgres",pid=77,fd=5))
ufw-installed yes
ufw-status Status: inactive
ufw-added ufw allow 5432/tcp
`, "ufw-installed"))
	out, _, err = runHarden(t, false, "", "--host", "vps", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(srv.calls(), "ufw-apply") || !strings.Contains(srv.scripts(), "ports='22'") {
		t.Errorf("calls:\n%s", srv.calls())
	}
	if strings.Contains(out, "install ufw") {
		t.Errorf("ufw is installed:\n%s", out)
	}
}

// The ports of the apps of Tifa that serve visitors are allowed with ssh: the public sockets of the
// user tifa, and the ports of the nginx of Tifa. The port the connection used counts too.
func TestHardenAllowsVisitorsAndTheConnectedPort(t *testing.T) {
	srv := newHardenServer(t, factsWith(`listen LISTEN 0 4096 *:443 *:* users:(("systemd",pid=700,fd=9))
listen LISTEN 0 4096 *:80 *:* users:(("systemd",pid=700,fd=10))
listen LISTEN 0 4096 0.0.0.0:2222 0.0.0.0:* users:(("systemd",pid=1,fd=11))
usock [::]:443 tifa-shop.socket tifa-shop.service
usock 0.0.0.0:80 tifa-shop-http.socket tifa-shop.service
usock 127.0.0.1:9530 tifa-blog.socket tifa-blog.service
`))
	writeFile(t, filepath.Join(srv.dir, "cfg"), "hostname vps\nport 2222\n")
	out, _, err := runHarden(t, false, "", "--host", "vps", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(srv.scripts(), "ports='22 80 443 2222'") {
		t.Errorf("scripts:\n%s", srv.scripts())
	}

	// The nginx of Tifa with a site holds 80 and 443 and needs no listener in the plan.
	srv = newHardenServer(t, factsWith(`listen LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:(("nginx",pid=60,fd=6))
nginx-tifa yes
nginx-site shop
`))
	if out, _, err = runHarden(t, false, "", "--host", "vps", "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(srv.scripts(), "ports='22 80 443'") {
		t.Errorf("scripts:\n%s", srv.scripts())
	}

	// An nginx that Tifa did not install is an unknown program.
	srv = newHardenServer(t, factsWith(`listen LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:(("nginx",pid=60,fd=6))
`))
	out, _, _ = runHarden(t, false, "", "--host", "vps", "--yes")
	if !strings.Contains(out, "nginx (pid 60) listens on port 80") || strings.Contains(srv.calls(), "ufw-apply") {
		t.Errorf("output:\n%s\ncalls:\n%s", out, srv.calls())
	}
}

// An app with deploy.front external listens on a port that only the load balancer of the user may
// reach. harden does not allow it to everyone and keeps ufw off until a rule allows it, from the
// address of the load balancer too. On 80 it is no visitor port either.
func TestHardenLeavesTheExternalPortToTheUser(t *testing.T) {
	for _, port := range []string{"8080", "80"} {
		t.Run(port, func(t *testing.T) {
			facts := `listen LISTEN 0 4096 *:` + port + ` *:* users:(("systemd",pid=700,fd=9))
usock [::]:` + port + ` tifa-shop.socket tifa-shop.service
external tifa-shop.socket
`
			srv := newHardenServer(t, factsWith(facts))
			out, _, err := runHarden(t, false, "", "--host", "vps", "--yes")
			if err == nil {
				t.Fatalf("no error\n%s", out)
			}
			for _, want := range []string{
				"port " + port + " is the app shop, with deploy.front external",
				"ufw stays off and nothing changed",
				"sudo ufw allow from <address of the load balancer> to any port " + port + " proto tcp",
				"then run harden again",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("lacks %q:\n%s", want, out)
				}
			}
			if strings.Contains(out, "listens on port "+port) {
				t.Errorf("calls the app an unknown program:\n%s", out)
			}
			if strings.Contains(srv.calls(), "ufw-apply") {
				t.Errorf("turned ufw on:\n%s", srv.calls())
			}

			// A rule that allows the port from the load balancer accounts for it, and harden adds none.
			srv = newHardenServer(t, factsWith(facts+`ufw-installed yes
ufw-status Status: inactive
ufw-added ufw allow 22/tcp
ufw-added ufw allow from 10.0.0.5 to any port `+port+` proto tcp comment 'load balancer'
`, "ufw-installed"))
			out, _, err = runHarden(t, false, "", "--host", "vps", "--yes")
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			if !strings.Contains(srv.calls(), "ufw-apply") || !strings.Contains(srv.scripts(), "ports=''") {
				t.Errorf("calls:\n%s\nscripts:\n%s", srv.calls(), srv.scripts())
			}
		})
	}
}

// The socket of an app with deploy.front external says so, and the read of harden finds it there.
func TestHardenFindsExternalSockets(t *testing.T) {
	if !strings.Contains(unitsOf("shop", 8080, "", true, false).socket, externalMark+"\n") {
		t.Errorf("the socket of an external app lacks %q", externalMark)
	}
	if strings.Contains(unitsOf("shop", 80, "", false, false).socket, externalMark) {
		t.Errorf("the socket of a front none app has %q", externalMark)
	}
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	home := t.TempDir()
	units := filepath.Join(home, ".config", "systemd", "user")
	writeFile(t, filepath.Join(units, "tifa-shop.socket"), unitsOf("shop", 8080, "", true, false).socket)
	writeFile(t, filepath.Join(units, "tifa-blog.socket"), unitsOf("blog", 443, "blog.example.com", false, false).socket)
	writeFile(t, filepath.Join(units, "tifa-blog-http.socket"), unitsOf("blog", 443, "blog.example.com", false, false).http)
	out, err := exec.Command("sh", "-c", hardenExternalFunc+"external_sockets "+shellQuote(home)).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if string(out) != "external tifa-shop.socket\n" {
		t.Errorf("got %q", out)
	}
	st, err := parseHardenState("usock [::]:8080 tifa-shop.socket tifa-shop.service\nusock [::]:443 tifa-blog.socket tifa-blog.service\n" + string(out))
	if err != nil {
		t.Fatal(err)
	}
	if got := st.visitorPorts(); !slices.Equal(got, []int{443}) {
		t.Errorf("visitor ports %v", got)
	}
}

// Docker publishes ports around ufw: the plan says so.
func TestHardenWarnsOfDocker(t *testing.T) {
	newHardenServer(t, factsWith("docker yes\n"))
	out, _, _ := runHarden(t, false, "", "--host", "vps")
	if !strings.Contains(out, "ufw does not filter the ports Docker publishes") {
		t.Errorf("output:\n%s", out)
	}
}

// Updates: each thing that is missing is in the one line, with an explicit 0 shown as such, and a
// reboot that waits is reported and never done.
func TestHardenPlansUpdates(t *testing.T) {
	cases := map[string]struct {
		facts string
		want  []string
	}{
		"package missing": {factsWith("", "ua-pkg", "ua-conf", "ua-timer") + "ua-conf missing\nua-timer enabled\n",
			[]string{"change  updates: install unattended-upgrades, write /etc/apt/apt.conf.d/20auto-upgrades with Unattended-Upgrade 1,"}},
		"set to 0": {factsWith("", "ua-conf") + "ua-conf 0\n",
			[]string{"change  updates: set Unattended-Upgrade to 1 in /etc/apt/apt.conf.d/20auto-upgrades, where it is 0 now"}},
		"timer off": {factsWith("", "ua-timer") + "ua-timer disabled\n",
			[]string{"change  updates: enable apt-daily-upgrade.timer, which is disabled"}},
		"reboot": {factsWith("reboot yes\n"),
			[]string{"found   updates: the server waits for a reboot (/var/run/reboot-required)", "harden never does"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			newHardenServer(t, tc.facts)
			out, _, _ := runHarden(t, false, "", "--host", "vps")
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("lacks %q:\n%s", want, out)
				}
			}
		})
	}
	srv := newHardenServer(t, factsWith("", "ua-conf")+"ua-conf 0\n")
	if out, _, err := runHarden(t, false, "", "--host", "vps", "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(srv.calls(), "updates-apply root") || !strings.Contains(srv.scripts(), "conf_before='0'") || !strings.Contains(srv.scripts(), "install='no'") {
		t.Errorf("calls:\n%s", srv.calls())
	}
	if strings.Contains(srv.scripts(), "reboot\n") {
		t.Errorf("reboots")
	}
}

// A system that is not Debian or Ubuntu gets the report and no change.
func TestHardenReportsOnlyOnOtherSystems(t *testing.T) {
	srv := newHardenServer(t, "os-id alpine\nos-like \nmachine vps-1\nsshd passwordauthentication yes\nsshd kbdinteractiveauthentication no\nsshd port 22\nufw-installed no\n")
	out, _, err := runHarden(t, false, "", "--host", "vps", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"Found on vps", "not Debian or Ubuntu with apt, so harden changes nothing here", "password logins are accepted"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(srv.calls(), "apply") {
		t.Errorf("changed it:\n%s", srv.calls())
	}
	// Debian without apt is no Debian for harden either.
	newHardenServer(t, strings.Replace(hardenFacts, "apt yes\n", "", 1))
	out, _, _ = runHarden(t, false, "", "--host", "vps", "--yes")
	if !strings.Contains(out, "changes nothing here") {
		t.Errorf("output:\n%s", out)
	}
}

// Several hosts are each read, planned and asked about in turn, and one that cannot be reached
// does not hide the others.
func TestHardenSeveralHosts(t *testing.T) {
	srv := newHardenServer(t, hardenFacts)
	out, _, err := runHarden(t, false, "", "--host", "one", "--host", "two", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Count(srv.calls(), "read root") != 2 || !strings.Contains(out, "Plan to harden one") || !strings.Contains(out, "Plan to harden two") {
		t.Errorf("calls:\n%s\n%s", srv.calls(), out)
	}
	if !strings.Contains(srv.argv(), "one") || !strings.Contains(srv.argv(), "two") {
		t.Errorf("argv:\n%s", srv.argv())
	}
}

// The admin user in front of the host runs the steps of root through sudo.
func TestHardenAsAdminUser(t *testing.T) {
	srv := newHardenServer(t, hardenFacts)
	// The fake ssh runs the sudo wrapper as a script: it has no sudo, and the marker is inside.
	if out, _, err := runHarden(t, false, "", "--host", "ubuntu@vps", "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(srv.calls(), "read ubuntu") || !strings.Contains(srv.argv(), "-l ubuntu") {
		t.Errorf("calls:\n%s", srv.calls())
	}
}

const hardenRecordFacts = `record # Written by tifa deploy harden
record sshd_file=yes
record ufw_before=absent
record ufw_installed=yes
record ufw_default_in=DROP
record ua_conf_before=missing
record ua_installed=yes
`

// --undo reads the record and nothing else: each item is put back, and the login is checked again
// after ssh.
func TestHardenUndoFromTheRecord(t *testing.T) {
	srv := newHardenServer(t, factsWith(hardenRecordFacts))
	out, _, err := runHarden(t, false, "", "--host", "vps", "--undo")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("got %v\n%s", err, out)
	}
	for _, want := range []string{
		"Plan to undo what harden did on vps",
		"remove  ssh: delete /etc/ssh/sshd_config.d/00-tifa-harden.conf, which harden wrote, and reload sshd",
		"remove  firewall: turn ufw off, delete the rules it added (with the comment 'tifa harden') and put back its default policies, and uninstall ufw, which harden installed",
		"remove  updates: delete /etc/apt/apt.conf.d/20auto-upgrades, uninstall unattended-upgrades, which harden installed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(srv.calls(), "undo-") {
		t.Fatalf("undid without consent:\n%s", srv.calls())
	}
	out, _, err = runHarden(t, false, "", "--host", "vps", "--undo", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	calls := srv.calls()
	pos := 0
	for _, want := range []string{"undo-ssh root", "check root", "undo-ufw root", "undo-updates root"} {
		i := strings.Index(calls[pos:], want)
		if i < 0 {
			t.Fatalf("calls lack %q in order:\n%s", want, calls)
		}
		pos += i + len(want)
	}
	if strings.Contains(calls, "-apply") {
		t.Errorf("changed instead of undoing:\n%s", calls)
	}
}

// --undo changes only what the record has.
func TestHardenUndoOnlyWhatIsRecorded(t *testing.T) {
	srv := newHardenServer(t, factsWith("record ua_conf_before=0\nrecord ua_timer_before=disabled\n"))
	out, _, err := runHarden(t, false, "", "--host", "vps", "--undo", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(srv.calls(), "undo-ssh") || strings.Contains(srv.calls(), "undo-ufw") || !strings.Contains(srv.calls(), "undo-updates") {
		t.Errorf("calls:\n%s", srv.calls())
	}
	if !strings.Contains(out, "set Unattended-Upgrade back to 0") || !strings.Contains(out, "disable apt-daily-upgrade.timer") {
		t.Errorf("output:\n%s", out)
	}
}

// Without a record there is nothing of harden to undo, and nothing runs.
func TestHardenUndoWithoutARecord(t *testing.T) {
	srv := newHardenServer(t, hardenFacts)
	out, _, err := runHarden(t, false, "", "--host", "vps", "--undo", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "Nothing of harden to undo on vps") {
		t.Errorf("output:\n%s", out)
	}
	if strings.Contains(srv.calls(), "undo-") || strings.Contains(srv.calls(), "check") {
		t.Errorf("calls:\n%s", srv.calls())
	}
}

// When a login with a new connection fails after the undo, the step says so: the file is gone either
// way, and the failure is something else.
func TestHardenUndoChecksTheLogin(t *testing.T) {
	srv := newHardenServer(t, factsWith("record sshd_file=yes\n"))
	srv.failCheck(t)
	out, _, err := runHarden(t, false, "", "--host", "vps", "--undo", "--yes")
	if err == nil || !strings.Contains(out, "a new ssh login from this machine failed") {
		t.Errorf("got %v\n%s", err, out)
	}
}

// The scripts of root are valid shell.
func TestHardenScriptsParse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	for name, body := range map[string]string{
		"read": script(hardenReadBody, "record", hardenRecord, "ssh_file", hardenSSHFile), "ssh": hardenScript(hardenSSHBody, "root_login", "yes"),
		"ufw":      hardenScript(hardenUFWBody, "ports", "22 80", "before", "absent", "default_in", "DROP", "default_out", "ACCEPT"),
		"updates":  hardenScript(hardenUpdatesBody, "install", "yes", "conf_before", "0", "timer_before", "disabled"),
		"undo ssh": hardenScript(hardenUndoSSHBody), "undo ufw": hardenScript(hardenUndoUFWBody), "undo updates": hardenScript(hardenUndoUpdatesBody),
		"sudo": sudoScript(hardenScript(hardenSSHBody, "root_login", "yes")),
	} {
		if out, err := exec.Command("sh", "-n", "-c", body).CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", name, err, out)
		}
	}
}

// The record keeps the first value of a key, which is the state from before, and the file goes with
// its last key.
func TestHardenRecordFunctions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	dir := t.TempDir()
	rec := filepath.Join(dir, "tifa", "harden")
	run := func(body string) string {
		t.Helper()
		out, err := exec.Command("sh", "-c", script(hardenLib, "record", rec)+body).CombinedOutput()
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(`recset ufw_before inactive; recset ufw_before absent; recset ua_installed yes`)
	if got := run(`recget ufw_before; recget ua_installed`); got != "inactive\nyes" {
		t.Errorf("got %q", got)
	}
	run(`recdel ufw_before; recdone`)
	data, err := os.ReadFile(rec)
	if err != nil || strings.Contains(string(data), "ufw_before") || !strings.Contains(string(data), "ua_installed=yes") {
		t.Errorf("record: %q %v", data, err)
	}
	run(`recdel ua_installed; recdone`)
	if _, err := os.Stat(rec); err == nil {
		t.Error("the record stays with no key")
	}
	if _, err := os.Stat(filepath.Dir(rec)); err == nil {
		t.Error("the empty folder stays")
	}
}

// Setup says in one line that the server takes ssh passwords.
func TestSetupAdvisesHardenForSSHPasswords(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	newSetupServer(t, "", factsOf("shop", 9527, "ssh-password yes\n"))
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "The server accepts ssh passwords. go tool tifa deploy harden turns that off") {
		t.Errorf("output:\n%s", out)
	}

	root = setupApp(t, "deploy:\n", nil)
	newSetupServer(t, "", factsOf("shop", 9527, ""))
	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "ssh passwords") {
		t.Errorf("output:\n%s", out)
	}
}

// The guarded script of ssh, run for real with stand-ins for sshd and systemctl: the file stays only
// on the answer keep. Any other answer, an end of input too, deletes it and the record of it, and
// so does an sshd that does not read the file.
func TestHardenSSHScriptKeepsOrUndoes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	run := func(t *testing.T, sshdSays, input string) (file, record, log string, err error) {
		t.Helper()
		dir := t.TempDir()
		bin := filepath.Join(dir, "bin")
		writeFile(t, filepath.Join(bin, "sshd"), "#!/bin/sh\nif [ \"$1\" = -T ]; then echo \"passwordauthentication "+sshdSays+"\"; fi\n")
		writeFile(t, filepath.Join(bin, "systemctl"), "#!/bin/sh\necho \"systemctl $*\" >> "+shellQuote(filepath.Join(dir, "log"))+"\n")
		for _, n := range []string{"sshd", "systemctl"} {
			if err := os.Chmod(filepath.Join(bin, n), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		sshFile, rec := filepath.Join(dir, "sshd_config.d", "00-tifa-harden.conf"), filepath.Join(dir, "tifa", "harden")
		// The library puts the system folders first: the stand-ins go in front of them after it.
		body := script(hardenLib, "record", rec, "ssh_file", sshFile, "ufw_comment", hardenUFWComment, "root_login", "yes") + "PATH=" + shellQuote(bin) + ":$PATH\n" + hardenSSHBody
		c := exec.Command("sh", "-c", body)
		c.Stdin = strings.NewReader(input)
		out, err := c.CombinedOutput()
		if !strings.Contains(string(out), hardenReady) && sshdSays == "no" {
			t.Fatalf("never said it changed: %s", out)
		}
		f, _ := os.ReadFile(sshFile)
		r, _ := os.ReadFile(rec)
		l, _ := os.ReadFile(filepath.Join(dir, "log"))
		return string(f), string(r), string(l), err
	}

	file, record, log, err := run(t, "no", "keep\n")
	if err != nil {
		t.Fatalf("keep: %v", err)
	}
	for _, want := range []string{"PasswordAuthentication no\n", "KbdInteractiveAuthentication no\n", "PermitRootLogin prohibit-password\n"} {
		if !strings.Contains(file, want) {
			t.Errorf("the file lacks %q:\n%s", want, file)
		}
	}
	if !strings.Contains(record, "sshd_file=yes") || !strings.Contains(log, "systemctl reload ssh") {
		t.Errorf("record %q, log %q", record, log)
	}

	for name, input := range map[string]string{"undo": "undo\n", "end of input": ""} {
		file, record, log, err = run(t, "no", input)
		if err == nil || file != "" || record != "" {
			t.Errorf("%s: err %v, file %q, record %q", name, err, file, record)
		}
		if strings.Count(log, "systemctl reload ssh") != 2 {
			t.Errorf("%s: sshd reloaded %d times, want 2 (the change, the undo):\n%s", name, strings.Count(log, "reload"), log)
		}
	}

	// sshd still says yes: it does not read the folder, so nothing is kept and nothing reloaded for it.
	file, record, log, err = run(t, "yes", "keep\n")
	if err == nil || file != "" || record != "" || strings.Contains(log, "reload") && strings.Count(log, "reload") > 1 {
		t.Errorf("err %v, file %q, record %q, log %q", err, file, record, log)
	}
}

// A dropped ssh connection sends SIGHUP to the script while it waits for the answer. dash, which is
// sh on Ubuntu, runs no EXIT trap when a signal kills it, so the change would stay: the guarded
// scripts trap the signals too. Run under dash, with the connection still open as it is then.
func TestHardenGuardedScriptsUndoOnSignals(t *testing.T) {
	dash, err := exec.LookPath("dash")
	if err != nil {
		t.Skip("dash is not installed")
	}
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			writeFile(t, filepath.Join(bin, "sshd"), "#!/bin/sh\nif [ \"$1\" = -T ]; then echo 'passwordauthentication no'; fi\n")
			writeFile(t, filepath.Join(bin, "systemctl"), "#!/bin/sh\nexit 0\n")
			for _, n := range []string{"sshd", "systemctl"} {
				if err := os.Chmod(filepath.Join(bin, n), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			sshFile, rec := filepath.Join(dir, "sshd_config.d", "00-tifa-harden.conf"), filepath.Join(dir, "tifa", "harden")
			body := script(hardenLib, "record", rec, "ssh_file", sshFile, "ufw_comment", hardenUFWComment, "root_login", "no") +
				"PATH=" + shellQuote(bin) + ":$PATH\n" + hardenSSHBody
			c := exec.Command(dash, "-c", body)
			in, err := c.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			out, err := c.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			line, _ := bufio.NewReader(out).ReadString('\n')
			if !strings.Contains(line, hardenReady) {
				t.Fatalf("got %q", line)
			}
			if _, err := os.Stat(sshFile); err != nil {
				t.Fatalf("the change is not there before the signal: %v", err)
			}
			if err := c.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			werr := c.Wait()
			if werr == nil {
				t.Error("exits 0 after a signal")
			}
			if _, err := os.Stat(sshFile); err == nil {
				t.Error("the file stays after the signal")
			}
			if _, err := os.Stat(rec); err == nil {
				t.Error("the record stays after the signal")
			}
		})
	}
}

const hardenUserRules = `ufw-installed yes
ufw-status Status: inactive
ufw-added ufw allow 22/tcp
ufw-added ufw allow 80/tcp comment 'tifa nginx front'
ufw-added ufw allow 443/tcp comment 'tifa nginx front'
nginx-tifa yes
nginx-site shop
usock 0.0.0.0:9528 tifa-blog.socket tifa-blog.service
`

// ufw updates the comment of a rule that is there instead of adding a second one, so harden adds a rule
// only for a port that no rule allows yet, says which ports have rules, and records what it added.
func TestHardenAddsOnlyPortsWithoutARule(t *testing.T) {
	srv := newHardenServer(t, factsWith(hardenUserRules, "ufw-installed"))
	out, _, err := runHarden(t, false, "", "--host", "vps", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(srv.scripts(), "ports='9528'") {
		t.Errorf("scripts:\n%s", srv.scripts())
	}
	for _, want := range []string{"allow port 9528 (tcp) in rules with the comment 'tifa harden'", "Ports 22, 80 and 443 have rules already, which stay as they are"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
}

// A rule that denies a port is left alone and said. When it is the port of ssh, ufw stays off.
func TestHardenLeavesDenyingRules(t *testing.T) {
	srv := newHardenServer(t, factsWith("ufw-installed yes\nufw-status Status: inactive\nufw-added ufw deny 22/tcp\n", "ufw-installed"))
	out, _, err := runHarden(t, false, "", "--host", "vps", "--yes")
	if err == nil || !strings.Contains(out, "deny 22/tcp") || strings.Contains(srv.calls(), "ufw-apply") {
		t.Errorf("got %v\n%s\n%s", err, out, srv.calls())
	}
	srv = newHardenServer(t, factsWith("ufw-installed yes\nufw-status Status: inactive\nufw-added ufw allow 22/tcp\nufw-added ufw limit 9528/tcp\nusock 0.0.0.0:9528 tifa-blog.socket tifa-blog.service\n", "ufw-installed"))
	out, _, err = runHarden(t, false, "", "--host", "vps", "--yes")
	if err != nil || !strings.Contains(out, "limit 9528/tcp") || !strings.Contains(srv.scripts(), "ports=''") {
		t.Errorf("got %v\n%s\n%s", err, out, srv.scripts())
	}
}

// The script, run for real with a ufw that keeps its rules in a file and updates the comment of a rule
// for the same port as ufw does: after harden and --undo the rules of the user and of the nginx of
// Tifa are as they were, and the one harden added is gone.
func TestHardenUndoLeavesOtherRules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	dir := t.TempDir()
	bin, rules := filepath.Join(dir, "bin"), filepath.Join(dir, "rules")
	before := "22/tcp|\n80/tcp|tifa nginx front\n443/tcp|tifa nginx front\n"
	writeFile(t, rules, before)
	writeFile(t, filepath.Join(bin, "ufw"), `#!/bin/sh
r=`+shellQuote(rules)+`
[ "$1" = --force ] && shift
case "$1" in
allow) p=$2; c=""; [ "$3" = comment ] && c=$4
	if grep -q "^$p|" "$r"; then sed "s/^$p|.*/$p|$c/" "$r" > "$r.new"; mv "$r.new" "$r"; else echo "$p|$c" >> "$r"; fi ;;
delete) p=$3; c=""; [ "$4" = comment ] && c=$5; grep -v "^$p|$c\$" "$r" > "$r.new"; mv "$r.new" "$r" ;;
show) while IFS='|' read -r p c; do if [ -n "$c" ]; then echo "ufw allow $p comment '$c'"; else echo "ufw allow $p"; fi; done < "$r" ;;
esac
exit 0
`)
	if err := os.Chmod(filepath.Join(bin, "ufw"), 0o755); err != nil {
		t.Fatal(err)
	}
	rec := filepath.Join(dir, "tifa", "harden")
	vars := []string{"record", rec, "ssh_file", filepath.Join(dir, "x.conf"), "ufw_comment", hardenUFWComment}
	pathFirst := "PATH=" + shellQuote(bin) + ":$PATH\n"
	apply := script(hardenLib, append(vars, "ports", "9528", "before", "inactive", "default_in", "DROP", "default_out", "ACCEPT")...) + pathFirst + hardenUFWBody
	c := exec.Command("sh", "-c", apply)
	c.Stdin = strings.NewReader("keep\n")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got, _ := os.ReadFile(rules); string(got) != before+"9528/tcp|tifa harden\n" {
		t.Fatalf("rules after harden:\n%s", got)
	}
	undo := script(hardenLib, vars...) + pathFirst + hardenUndoUFWBody
	if out, err := exec.Command("sh", "-c", undo).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got, _ := os.ReadFile(rules); string(got) != before {
		t.Errorf("rules after undo:\n%s", got)
	}
}
