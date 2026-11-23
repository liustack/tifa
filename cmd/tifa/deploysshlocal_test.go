package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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
	if !strings.HasPrefix(cfg, "# tifa shop "+host+": through the Cloudflare tunnel") || !strings.HasSuffix(cfg, "# end tifa shop "+host+"\r\n"+myConfig) {
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

// The key hint is for ssh that got to authentication. A connection that closed before that says
// what ssh printed and does not blame the key.
func TestSetupSSHLocalCheckFailsBeforeAuth(t *testing.T) {
	l := newLocalSSH(t, "TIFA_DEPLOY_HOST=vps\n")
	l.srv.closeCheck(t)
	out, errOut, err := runSetupIO(t, l.root, false, "", "--yes")
	if err == nil || !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "through the tunnel failed (exit status 255), see above. Run go tool tifa deploy setup again once it works") {
		t.Errorf("message:\n%s", out)
	}
	if strings.Contains(out, "authorized_keys") || strings.Contains(out, "ssh key") {
		t.Errorf("blames the key:\n%s", out)
	}
	if !strings.Contains(errOut, "Connection closed by UNKNOWN") || l.env(t) != "TIFA_DEPLOY_HOST=vps\n" {
		t.Errorf("output not shown or .env changed:\n%s", errOut)
	}
}

// A TIFA_DEPLOY_HOST from the environment or --host is not setup's to change, but the tunnel is
// checked all the same. Only once the check passed does setup say what to set, with the real name.
func TestSetupSSHLocalHostFromEnvironment(t *testing.T) {
	l := newLocalSSH(t, "")
	t.Setenv(deployHostEnv, "vps")
	out, err := runSetup(t, l.root, false, "", "--yes")
	if err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("%v\n%s", err, out)
	}
	host := l.cf.apps[0]["domain"].(string)
	for _, want := range []string{
		"add     this machine: check that ssh reaches tifa@deploy-a-ssh-<random>.example.com through the tunnel",
		"To deploy from this machine through the tunnel, set TIFA_DEPLOY_HOST=" + host,
		"environment variable TIFA_DEPLOY_HOST, which setup does not change",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "TIFA_DEPLOY_HOST in .env") || !strings.Contains(l.srv.calls(), "check tifa") {
		t.Errorf("the check did not run, or .env was planned:\n%s", out)
	}
	if data, _ := os.ReadFile(filepath.Join(l.root, ".env")); string(data) != "" {
		t.Errorf(".env: %q", data)
	}
	// The flag is named as the source. When the check fails, setup does not say what to set.
	l.srv.failCheck(t)
	out, _ = runSetup(t, l.root, false, "", "--yes", "--host", "vps")
	if strings.Contains(out, "To deploy from this machine") || !strings.Contains(out, "warning this machine: check that ssh reaches") {
		t.Errorf("after a failed check:\n%s", out)
	}
	os.Remove(filepath.Join(l.srv.dir, "checkfail"))
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
	want := `ProxyCommand "C:/Program Files (x86)/cloudflared/cloudflared.exe" access ssh --hostname %h`
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

// Only the value of the line the program reads changes: the export in front, the quotes, the
// comment after it, every line ending and a missing final newline stay.
func TestSetEnvHost(t *testing.T) {
	const h = "ssh-x.example.com"
	for name, tc := range map[string]struct{ in, want string }{
		"lf":        {"# server\nTIFA_DEPLOY_HOST=vps\nA=1\n", "# server\nTIFA_DEPLOY_HOST=" + h + "\nA=1\n"},
		"crlf":      {"A=1\r\nTIFA_DEPLOY_HOST=ssh://203.0.113.7:2222\r\n# end\r\n", "A=1\r\nTIFA_DEPLOY_HOST=" + h + "\r\n# end\r\n"},
		"mixed":     {"A=1\r\nTIFA_DEPLOY_HOST=vps\nB=2\r\n", "A=1\r\nTIFA_DEPLOY_HOST=" + h + "\nB=2\r\n"},
		"effective": {"TIFA_DEPLOY_HOST=a\nB=2\nTIFA_DEPLOY_HOST=b\n", "TIFA_DEPLOY_HOST=a\nB=2\nTIFA_DEPLOY_HOST=" + h + "\n"},
		"export":    {"export TIFA_DEPLOY_HOST=vps\n", "export TIFA_DEPLOY_HOST=" + h + "\n"},
		"comment":   {"TIFA_DEPLOY_HOST=vps   # my box\nA=1\n", "TIFA_DEPLOY_HOST=" + h + "   # my box\nA=1\n"},
		"spaces":    {"  TIFA_DEPLOY_HOST = vps\n", "  TIFA_DEPLOY_HOST = " + h + "\n"},
		"double":    {`TIFA_DEPLOY_HOST="vps" # box` + "\n", `TIFA_DEPLOY_HOST="` + h + `" # box` + "\n"},
		"single":    {"TIFA_DEPLOY_HOST='vps'\n", "TIFA_DEPLOY_HOST='" + h + "'\n"},
		"no final":  {"A=1\nTIFA_DEPLOY_HOST=vps", "A=1\nTIFA_DEPLOY_HOST=" + h},
		"a hash":    {"TIFA_DEPLOY_HOST=vps#1\n", "TIFA_DEPLOY_HOST=" + h + "\n"},
		"missing":   {"A=1", "A=1\nTIFA_DEPLOY_HOST=" + h + "\n"},
	} {
		path := filepath.Join(t.TempDir(), ".env")
		writeFile(t, path, tc.in)
		if err := setEnvHost(path, h); err != nil {
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

// sshRoutePublished says the tunnel has a route to ssh, or a DNS record is there for one.
func (l *localSSH) sshRoutePublished() bool {
	for _, cfg := range l.cf.configs {
		b, _ := json.Marshal(cfg)
		if strings.Contains(string(b), sshService) {
			return true
		}
	}
	for _, r := range l.cf.records["example.com"] {
		if strings.Contains(r["name"].(string), "-ssh-") {
			return true
		}
	}
	return false
}

// The route and the record of the ssh host name are made only after the Access application and a
// policy are: with no email to admit and no CI, or when the policy cannot be made, nothing is
// published, and setup says why.
func TestSetupSSHAccessBeforeRoute(t *testing.T) {
	// No email, no CI.
	l := newLocalSSH(t, "TIFA_DEPLOY_HOST=vps\n")
	l.cf.email = ""
	out, err := runSetup(t, l.root, false, "", "--yes")
	if err == nil || !strings.Contains(out, "Nothing admits anyone to the Access application yet") || l.sshRoutePublished() || len(l.cf.apps) != 0 {
		t.Errorf("no email: %v, published %v\n%s", err, l.sshRoutePublished(), out)
	}
	if strings.Contains(out, "add     cloudflare: route deploy-a-ssh") {
		t.Errorf("the plan has the route:\n%s", out)
	}

	// The policy cannot be made.
	l = newLocalSSH(t, "TIFA_DEPLOY_HOST=vps\n")
	l.cf.failPolicyCreate = true
	out, err = runSetup(t, l.root, false, "", "--yes")
	if err == nil || l.sshRoutePublished() || len(l.cf.apps) != 0 {
		t.Errorf("a failed policy: %v, published %v\n%s", err, l.sshRoutePublished(), out)
	}

	// In the plan, Access comes before the route.
	l = newLocalSSH(t, "TIFA_DEPLOY_HOST=vps\n")
	out, _ = runSetup(t, l.root, false, "")
	if a, r := strings.Index(out, "the Access application of deploy-a-ssh"), strings.Index(out, "route deploy-a-ssh"); a < 0 || r < 0 || a > r {
		t.Errorf("Access at %d, route at %d:\n%s", a, r, out)
	}
}

// Remove takes the route and the record away before the application and its policy, so a partial
// failure never leaves a route without Access. A rerun finds the host name from the route when the
// application is already gone.
func TestDeployRemoveSSHOrderAndLeftover(t *testing.T) {
	l := newLocalSSH(t, "TIFA_DEPLOY_HOST=vps\n")
	if out, err := runSetup(t, l.root, false, "", "--yes"); err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	host := l.cf.apps[0]["domain"].(string)
	setFacts(t, l.srv, removeFacts(""))
	l.cf.calls = nil
	out, _, err := runRemove(t, l.root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	order := func(calls []string, words ...string) []int {
		var at []int
		for _, w := range words {
			at = append(at, slices.IndexFunc(calls, func(c string) bool { return strings.Contains(c, w) }))
		}
		return at
	}
	at := order(l.cf.calls, "dns records delete", "zero-trust access applications delete", "zero-trust access policies delete")
	if at[0] < 0 || at[0] > at[1] || at[1] > at[2] {
		t.Errorf("order of the calls %v:\n%s", at, strings.Join(l.cf.calls, "\n"))
	}

	// A leftover: the route and the record are there, the application and policy are gone.
	l2 := newLocalSSH(t, "TIFA_DEPLOY_HOST=vps\n")
	if out, err := runSetup(t, l2.root, false, "", "--yes"); err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	l2.cf.apps, l2.cf.policies = nil, nil
	setFacts(t, l2.srv, removeFacts(""))
	out, _, err = runRemove(t, l2.root, false, "", "--yes")
	if err != nil || l2.sshRoutePublished() || !strings.Contains(out, "remove  cloudflare: the Access application of the ssh host name of deploy-a.example.com") {
		t.Errorf("the leftover route of %s stayed: %v\n%s", host, err, out)
	}
}

// When the server does not give up its app, the ssh host name stays: it is the way in for the
// run that finishes the work. So do the tunnel and the block of the ssh config.
func TestDeployRemoveKeepsTheWayInAfterAFailure(t *testing.T) {
	l := newLocalSSH(t, "TIFA_DEPLOY_HOST=vps\n")
	if out, err := runSetup(t, l.root, false, "", "--yes"); err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	setFacts(t, l.srv, removeFacts(""))
	l.srv.failRemoveApp(t)
	out, _, err := runRemove(t, l.root, false, "", "--yes")
	if err == nil || !l.sshRoutePublished() || len(l.cf.apps) != 1 || len(l.cf.tunnels) != 1 || !strings.Contains(l.config(t), "# end tifa shop") {
		t.Errorf("too much was removed after a failure: %v\n%s", err, out)
	}
}

// Through the tunnel, everything that needs ssh comes first, the purge tokens too, then the stop of
// the tunnel, then the ssh host name and the tunnel entry in Cloudflare.
func TestDeployRemoveThroughTheTunnelOrder(t *testing.T) {
	l := newLocalSSH(t, "TIFA_DEPLOY_HOST=vps\n")
	if out, err := runSetup(t, l.root, false, "", "--yes"); err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	t.Setenv(deployHostEnv, l.cf.apps[0]["domain"].(string))
	old := untunnelWait
	untunnelWait = 0
	t.Cleanup(func() { untunnelWait = old })
	setFacts(t, l.srv, removeFacts(""))
	out, _, err := runRemove(t, l.root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	calls := l.srv.calls()
	if p, u := strings.Index(calls, "purgeforget"), strings.Index(calls, "untunnel"); p < 0 || u < 0 || p > u {
		t.Errorf("the purge tokens went after the stop of the tunnel:\n%s", calls)
	}
	idx := func(s string) int { return strings.Index(out, s) }
	if !(idx("done    server: delete") < idx("done    server: stop cloudflared") && idx("done    server: stop cloudflared") < idx("done    cloudflare: the Access application of the ssh host name") && idx("done    cloudflare: the Access application of the ssh host name") < idx("done    cloudflare: the tunnel of the server")) {
		t.Errorf("order:\n%s", out)
	}
}

// With cf logged out, remove cannot take the ssh host name away: when the developer reaches the
// server by it, the block of the ssh config stays, and remove says so.
func TestDeployRemoveKeepsTheBlockWithoutCF(t *testing.T) {
	l := newLocalSSH(t, "TIFA_DEPLOY_HOST=vps\n")
	if out, err := runSetup(t, l.root, false, "", "--yes"); err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	t.Setenv(deployHostEnv, l.cf.apps[0]["domain"].(string))
	l.cf.loggedOut = true
	setFacts(t, l.srv, removeFacts(""))
	out, _, err := runRemove(t, l.root, false, "", "--yes")
	if err == nil || !strings.Contains(l.config(t), "# end tifa shop") || !strings.Contains(out, "stays") {
		t.Errorf("the block went with cf logged out: %v\n%s", err, out)
	}
	// With another way to the server, the block is only the developer's: it goes.
	t.Setenv(deployHostEnv, "vps")
	out, _, err = runRemove(t, l.root, false, "", "--yes")
	if strings.Contains(l.config(t), "# end tifa shop") {
		t.Errorf("the block stayed: %v\n%s", err, out)
	}
}

// Switching ssh back to direct while TIFA_DEPLOY_HOST is the ssh host name does not take the way
// away: setup says to set the address first. With the address, the next run removes the
// route, the application and the block.
func TestSetupSSHBackToDirectNeedsTheAddress(t *testing.T) {
	l := newLocalSSH(t, "TIFA_DEPLOY_HOST=vps\n")
	if out, err := runSetup(t, l.root, false, "", "--yes"); err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	host := l.cf.apps[0]["domain"].(string)
	if !strings.Contains(l.env(t), host) {
		t.Fatalf(".env %q", l.env(t))
	}
	writeFile(t, filepath.Join(l.root, "tifa.yaml"), "site:\n  url: https://deploy-a.example.com\ndeploy:\n  front: cloudflare\n")
	setFacts(t, l.srv, factsOf("shop", 9531, tunnelFacts))
	out, err := runSetup(t, l.root, false, "", "--yes")
	if err == nil || !strings.Contains(out, "Set TIFA_DEPLOY_HOST to the address of the server") || len(l.cf.apps) != 1 || !l.sshRoutePublished() || !strings.Contains(l.config(t), "# end tifa shop") {
		t.Errorf("the way in was removed: %v\n%s", err, out)
	}
	writeFile(t, filepath.Join(l.root, ".env"), "TIFA_DEPLOY_HOST=vps\n")
	out, err = runSetup(t, l.root, false, "", "--yes")
	if err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(l.cf.apps) != 0 || l.sshRoutePublished() || strings.Contains(l.config(t), "# end tifa shop") {
		t.Errorf("left: apps %v, published %v\n%s\n%s", l.cf.apps, l.sshRoutePublished(), l.config(t), out)
	}
}

// A second person whose machine has no cf login yet and no block: with a terminal, setup logs cf in,
// plans the machine, and reads the server only then. Without a terminal it says what to do first.
func TestSetupSSHSecondPersonWithoutCFLogin(t *testing.T) {
	first := newLocalSSH(t, "TIFA_DEPLOY_HOST=vps\n")
	if out, err := runSetup(t, first.root, false, "", "--yes"); err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("first person: %v\n%s", err, out)
	}
	host := first.cf.apps[0]["domain"].(string)
	os.Remove(first.cfgPath)
	os.Remove(filepath.Join(first.srv.dir, "cfgseen"))
	writeFile(t, filepath.Join(first.root, ".env"), "TIFA_DEPLOY_HOST="+host+"\n")
	first.cf.email = "ana@example.com"
	first.cf.loggedOut = true
	setFacts(t, first.srv, factsOf("shop", 9531, tunnelFacts))

	// No terminal: nothing can log in, and the server is not read.
	out, err := runSetup(t, first.root, false, "", "--yes")
	if err == nil || !strings.Contains(out, "this machine: a way to reach "+host+" through the tunnel") || len(first.srv.configSeen()) != 0 {
		t.Fatalf("no terminal: %v\n%s", err, out)
	}

	// A terminal: cf logs in, then the machine, then the server.
	m := newFakeMachine("darwin", "cf", "cloudflared")
	m.cf = first.cf.fakeCF
	m.install(t)
	out, err = runSetup(t, first.root, true, "y\ny\n")
	if err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(strings.Join(m.ran, "\n"), "cf auth login") || !strings.Contains(first.config(t), "Host "+host) {
		t.Errorf("ran %v\n%s", m.ran, out)
	}
	if seen := first.srv.configSeen(); len(seen) == 0 || strings.Contains(strings.Join(seen, " "), "missing") {
		t.Errorf("ssh ran before the block was there: %v", seen)
	}
	if got := emailsOf(policyByNameIn(first.cf.fakeAccess, peopleName(host))); len(got) != 2 {
		t.Errorf("emails %v", got)
	}
}
