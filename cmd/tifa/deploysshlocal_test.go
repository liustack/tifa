package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// localSSH is an app with ssh through Cloudflare and no CI, on a fake server, with the host in its
// .env and a ssh config of the developer that has other lines.
type localSSH struct {
	root, home, cfgPath string
	srv                 *setupServer
	cf                  *cfEverything
}

const myConfig = "Host box\r\n  HostName 192.0.2.1\r\n"

func newLocalSSH(t *testing.T, dotenv string) *localSSH {
	t.Helper()
	root := setupApp(t, sshCloudflareNoCI, map[string]string{".env": dotenv})
	t.Setenv(deployHostEnv, "") // the host comes from .env
	l := &localSSH{root: root}
	home, _ := sshHome()
	l.home, l.cfgPath = home, filepath.Join(home, ".ssh", "config")
	l.srv = newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts))
	l.srv.watchSSHConfig(t, l.cfgPath)
	l.cf = cfSetup(t, "example.com")
	l.cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	return l
}

func (l *localSSH) writeConfig(t *testing.T, text string) {
	t.Helper()
	writeFile(t, l.cfgPath, text)
}

func (l *localSSH) config(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(l.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (l *localSSH) env(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(l.root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The first setup of a server reaches it by the address of the developer, makes the ssh host name,
// puts the block at the top of the ssh config, checks that ssh gets through the tunnel, and only
// then moves TIFA_DEPLOY_HOST in .env to the ssh host. A second run has nothing to do.
func TestSetupSSHLocalFirstRun(t *testing.T) {
	l := newLocalSSH(t, "# the server\nTIFA_DEPLOY_HOST=vps\nOTHER=1\n")
	l.writeConfig(t, myConfig)

	out, err := runSetup(t, l.root, false, "", "--yes")
	if err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("%v\n%s", err, out)
	}
	host := l.cf.apps[0]["domain"].(string)
	for _, want := range []string{
		"add     this machine: a block for deploy-a-ssh-<random>.example.com in " + l.cfgPath,
		"add     this machine: check that ssh reaches tifa@deploy-a-ssh-<random>.example.com through the tunnel (the first time, cloudflared opens the Cloudflare Access login in your browser and waits for you)",
		"change  this machine: TIFA_DEPLOY_HOST in .env, vps -> deploy-a-ssh-<random>.example.com",
		"Checking ssh to tifa@" + host + " through the tunnel. The first time, cloudflared opens the Cloudflare Access login in your browser and waits for you to finish it, for up to 4m0s.",
		"done    this machine: TIFA_DEPLOY_HOST in .env, vps -> " + host,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(l.srv.calls(), "check tifa") {
		t.Errorf("no check through the tunnel:\n%s", l.srv.calls())
	}
	if want := "# the server\nTIFA_DEPLOY_HOST=" + host + "\nOTHER=1\n"; l.env(t) != want {
		t.Errorf(".env:\n%s", l.env(t))
	}
	cfg := l.config(t)
	if !strings.HasPrefix(cfg, "# tifa shop: through the Cloudflare tunnel") || !strings.HasSuffix(cfg, "# end tifa shop\r\n"+myConfig) {
		t.Errorf("ssh config:\n%q", cfg)
	}
	// The file the developer had ends its lines with CRLF, so the block does too.
	if !strings.Contains(cfg, "Host "+host+"\r\n") || strings.Contains(strings.ReplaceAll(cfg, "\r\n", ""), "\n") || !strings.Contains(cfg, "ProxyCommand "+fakeCloudflared+" access ssh --hostname %h") {
		t.Errorf("ssh config:\n%s", cfg)
	}

	// A second run, now that .env names the ssh host: nothing to do, and nothing is checked again.
	setFacts(t, l.srv, factsOf("shop", 9531, tunnelFacts))
	l.cf.calls = nil
	out, err = runSetup(t, l.root, false, "", "--yes", "--all")
	if err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"ok      this machine: cloudflared at " + fakeCloudflared, "ok      this machine: a block for " + host, "ok      this machine: TIFA_DEPLOY_HOST is the ssh host"} {
		if !strings.Contains(out, want) {
			t.Errorf("second run lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "  add     ") || strings.Contains(out, "  change  ") || len(l.cf.writes()) != 0 || strings.Count(l.srv.calls(), "check tifa") != 1 {
		t.Errorf("a second run did something:\n%s\n%s", out, l.srv.calls())
	}
}

// When ssh through the tunnel fails, TIFA_DEPLOY_HOST stays where it was, and setup says what
// went wrong and what to do. The run after it tries again.
func TestSetupSSHLocalCheckFails(t *testing.T) {
	l := newLocalSSH(t, "TIFA_DEPLOY_HOST=vps\n")
	l.srv.failCheck(t)
	out, errOut, err := runSetupIO(t, l.root, false, "", "--yes")
	if err == nil || !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"warning this machine: check that ssh reaches tifa@", "authorized_keys", "skipped this machine: TIFA_DEPLOY_HOST in .env, vps -> "} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(errOut, "Permission denied") {
		t.Errorf("what ssh said is not shown:\n%s", errOut)
	}
	if l.env(t) != "TIFA_DEPLOY_HOST=vps\n" {
		t.Errorf(".env changed: %q", l.env(t))
	}
	if !strings.Contains(l.config(t), "# end tifa shop") {
		t.Error("the block was not written")
	}
}

// A TIFA_DEPLOY_HOST from the environment or --host is not setup's to change: setup says what to
// set, with the real name once it exists, and does not check the tunnel.
func TestSetupSSHLocalHostFromEnvironment(t *testing.T) {
	l := newLocalSSH(t, "")
	t.Setenv(deployHostEnv, "vps")
	out, err := runSetup(t, l.root, false, "", "--yes")
	if err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("%v\n%s", err, out)
	}
	host := l.cf.apps[0]["domain"].(string)
	for _, want := range []string{
		"set TIFA_DEPLOY_HOST=deploy-a-ssh-<random>.example.com to deploy through the tunnel (optional)",
		"environment variable TIFA_DEPLOY_HOST, which setup does not change",
		"To deploy from this machine through the tunnel, set TIFA_DEPLOY_HOST=" + host,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(l.srv.calls(), "check tifa") || strings.Contains(out, "check that ssh") {
		t.Errorf("the tunnel was checked:\n%s", out)
	}
	if data, _ := os.ReadFile(filepath.Join(l.root, ".env")); string(data) != "" {
		t.Errorf(".env: %q", data)
	}
	out, _ = runSetup(t, l.root, false, "", "--yes", "--host", "vps")
	if !strings.Contains(out, "flag --host, which setup does not change") {
		t.Errorf("the flag is not named:\n%s", out)
	}
}

// A second person, with their own cf login, names the server by its ssh host name. Their machine
// has no cloudflared block yet, so setup plans that first and reads the server after it: the ssh
// config is there at every ssh call. Their email joins the first one in the same policy.
func TestSetupSSHLocalSecondPerson(t *testing.T) {
	l := newLocalSSH(t, "TIFA_DEPLOY_HOST=vps\n")
	if out, err := runSetup(t, l.root, false, "", "--yes"); err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("first person: %v\n%s", err, out)
	}
	host := l.cf.apps[0]["domain"].(string)

	// The other machine: no block, another login, and the ssh host name in .env.
	if err := os.Remove(l.cfgPath); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(l.srv.dir, "cfgseen"))
	l.cf.email = "ana@example.com"
	writeFile(t, filepath.Join(l.root, ".env"), "TIFA_DEPLOY_HOST="+host+"\n")
	setFacts(t, l.srv, factsOf("shop", 9531, tunnelFacts))
	out, err := runSetup(t, l.root, false, "", "--yes")
	if err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("%v\n%s", err, out)
	}
	first, _, _ := strings.Cut(out, "Setup plans the rest once this machine can reach the server through the tunnel, and asks again.")
	if first == out {
		t.Fatalf("no first plan for the machine:\n%s", out)
	}
	for _, want := range []string{"add     this machine: a block for " + host, "change  cloudflare: the Access application of " + host + " admits ana@example.com, and keeps dev@example.com"} {
		if !strings.Contains(first, want) {
			t.Errorf("the first plan lacks %q:\n%s", want, first)
		}
	}
	if strings.Contains(first, "server:") || strings.Contains(first, "app:") {
		t.Errorf("the first plan reads the server:\n%s", first)
	}
	if seen := l.srv.configSeen(); len(seen) == 0 || strings.Contains(strings.Join(seen, " "), "missing") {
		t.Errorf("ssh ran before the block was there: %v", seen)
	}
	people := policyByNameIn(l.cf.fakeAccess, peopleName(host))
	if got := emailsOf(people); len(got) != 2 || got[0] != "dev@example.com" || got[1] != "ana@example.com" {
		t.Errorf("emails %v", got)
	}
	if !strings.Contains(l.config(t), "Host "+host) {
		t.Errorf("no block:\n%s", l.config(t))
	}
}

// cloudflared that is not there is installed with brew on macOS, as an item of the plan that --yes
// applies too, and the block then names the program where brew put it.
func TestSetupSSHLocalInstallsCloudflared(t *testing.T) {
	l := newLocalSSH(t, "TIFA_DEPLOY_HOST=vps\n")
	m := newFakeMachine("darwin", "brew", "cf")
	m.install(t)
	out, err := runSetup(t, l.root, false, "", "--yes")
	if err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "add     this machine: install cloudflared with brew install cloudflared") || !strings.Contains(strings.Join(m.ran, "\n"), "brew install cloudflared") {
		t.Errorf("ran %v\n%s", m.ran, out)
	}
	if !strings.Contains(l.config(t), "ProxyCommand /opt/homebrew/bin/cloudflared access ssh --hostname %h") {
		t.Errorf("ssh config:\n%s", l.config(t))
	}
}

// Windows gets winget, and a path with spaces is quoted in the block.
func TestSetupSSHLocalInstallsCloudflaredOnWindows(t *testing.T) {
	l := newLocalSSH(t, "TIFA_DEPLOY_HOST=vps\n")
	m := newFakeMachine("windows", "winget", "cf")
	m.install(t)
	out, err := runSetup(t, l.root, false, "", "--yes")
	if err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "install cloudflared with winget install --id Cloudflare.cloudflared -e") {
		t.Errorf("ran %v\n%s", m.ran, out)
	}
	want := `ProxyCommand "` + filepath.Join(`C:\Program Files (x86)`, "cloudflared", "cloudflared.exe") + `" access ssh --hostname %h`
	if !strings.Contains(l.config(t), want) {
		t.Errorf("want %s in\n%s", want, l.config(t))
	}
}

// Without brew or winget, or on Linux, setup does not try: the item says where to get it, and the
// ssh config and the switch wait.
func TestSetupSSHLocalCloudflaredByHand(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		l := newLocalSSH(t, "TIFA_DEPLOY_HOST=vps\n")
		m := newFakeMachine(goos, "cf")
		m.install(t)
		out, err := runSetup(t, l.root, false, "", "--yes")
		if err == nil || !strings.Contains(out, "this machine: cloudflared, which ssh runs to reach the server through the tunnel") || !strings.Contains(out, cloudflaredPage) {
			t.Errorf("%s: %v\n%s", goos, err, out)
		}
		if len(m.ran) != 0 || exists(l.cfgPath) || strings.Contains(out, "TIFA_DEPLOY_HOST in .env") {
			t.Errorf("%s: ran %v, config %v\n%s", goos, m.ran, exists(l.cfgPath), out)
		}
	}
}

// The .env of the developer keeps its other lines, its comments and its line endings.
func TestSetEnvHost(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"lf":   {"# server\nTIFA_DEPLOY_HOST=vps\nA=1\n", "# server\nTIFA_DEPLOY_HOST=ssh-x.example.com\nA=1\n"},
		"crlf": {"A=1\r\nTIFA_DEPLOY_HOST=ssh://203.0.113.7:2222\r\n# end\r\n", "A=1\r\nTIFA_DEPLOY_HOST=ssh-x.example.com\r\n# end\r\n"},
		"dup":  {"TIFA_DEPLOY_HOST=a\nB=2\nTIFA_DEPLOY_HOST=b\n", "TIFA_DEPLOY_HOST=ssh-x.example.com\nB=2\n"},
	} {
		path := filepath.Join(t.TempDir(), ".env")
		writeFile(t, path, tc.in)
		if err := setEnvHost(path, "ssh-x.example.com"); err != nil {
			t.Fatal(err)
		}
		if data, _ := os.ReadFile(path); string(data) != tc.want {
			t.Errorf("%s: got %q, want %q", name, data, tc.want)
		}
	}
}

// The ssh host names of a domain are told from other names without asking Cloudflare.
func TestLooksLikeSSHHost(t *testing.T) {
	for host, want := range map[string]bool{
		"deploy-a-ssh-k3j6x2m5q7wt.example.com": true,
		"deploy-a-ssh.example.com":              false,
		"vps":                                   false,
		"203.0.113.7":                           false,
		"other-ssh-k3j6x2m5q7wt.example.com":    false,
	} {
		if got := looksLikeSSHHost(host, "deploy-a.example.com"); got != want {
			t.Errorf("%s: %v", host, got)
		}
	}
	if !looksLikeSSHHost("ssh-k3j6x2m5q7wt.example.com", "example.com") {
		t.Error("the apex")
	}
}
