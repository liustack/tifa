package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeGH stands in for gh: one repository, its secrets and variables.
type fakeGH struct {
	loggedIn bool
	repo     string
	branch   string // empty: no default branch yet
	calls    []string
	secrets  map[string]string
	vars     map[string]string
}

func newFakeGH() *fakeGH {
	return &fakeGH{loggedIn: true, repo: "acme/shop", branch: "main", secrets: map[string]string{}, vars: map[string]string{}}
}

func (g *fakeGH) install(t *testing.T) {
	oldRun, oldHave := runGH, haveGH
	t.Cleanup(func() { runGH, haveGH = oldRun, oldHave })
	haveGH = func() bool { return true }
	runGH = func(_ context.Context, _ string, stdin io.Reader, args ...string) ([]byte, error) {
		fakeMu.Lock()
		defer fakeMu.Unlock()
		g.calls = append(g.calls, strings.Join(args, " "))
		switch {
		case slices.Equal(args, []string{"auth", "status"}):
			if !g.loggedIn {
				return nil, errors.New("gh auth status: You are not logged into any GitHub hosts")
			}
			return nil, nil
		case args[0] == "repo" && args[1] == "view":
			ref := "null"
			if g.branch != "" {
				ref = fmt.Sprintf(`{"name":%q}`, g.branch)
			}
			return []byte(fmt.Sprintf(`{"nameWithOwner":%q,"defaultBranchRef":%s}`, g.repo, ref)), nil
		case args[0] == "secret" && args[1] == "list":
			var out []map[string]string
			for n := range g.secrets {
				out = append(out, map[string]string{"name": n})
			}
			return json.Marshal(out)
		case args[0] == "variable" && args[1] == "list":
			var out []map[string]string
			for n, v := range g.vars {
				out = append(out, map[string]string{"name": n, "value": v})
			}
			return json.Marshal(out)
		case args[0] == "secret" && args[1] == "set", args[0] == "variable" && args[1] == "set":
			if stdin == nil || len(args) != 5 || args[3] != "--repo" || args[4] != g.repo {
				return nil, fmt.Errorf("fake gh: bad set %v", args)
			}
			data, _ := io.ReadAll(stdin)
			if args[0] == "secret" {
				g.secrets[args[2]] = string(data)
			} else {
				g.vars[args[2]] = string(data)
			}
			return nil, nil
		case args[0] == "secret" && args[1] == "delete":
			delete(g.secrets, args[2])
			return nil, nil
		case args[0] == "variable" && args[1] == "delete":
			delete(g.vars, args[2])
			return nil, nil
		}
		return nil, fmt.Errorf("fake gh does not know %v", args)
	}
}

// setupServer is a fake ssh. Facts is what the probe of the deploy user prints. A script that
// changes the server is logged by name with the user, and its input is kept.
type setupServer struct {
	dir string
}

func newSetupServer(t *testing.T, cfg, facts string) *setupServer {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake ssh is a shell script")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "cfg"), cfg)
	writeFile(t, filepath.Join(dir, "facts"), facts)
	prog := filepath.Join(dir, "ssh")
	writeFile(t, prog, `#!/bin/sh
d=`+shellQuote(dir)+`
if [ "$1" = -G ]; then cat "$d/cfg"; exit 0; fi
user=$2
while [ "$1" != "--" ]; do shift; done
script=$3
case "$script" in
*ssh_host_*) [ "$user" = tifa ] && [ -f "$d/denied" ] && exit 255; echo "probe $user" >> "$d/calls"; cat "$d/facts" ;;
*"deploy setup needs root"*) echo "probe $user" >> "$d/calls"; printf 'os Linux\narch x86_64\n' ;;
*useradd*) echo "server $user" >> "$d/calls"; rm -f "$d/denied"; printf 'bus yes\nlinger yes\n' >> "$d/facts" ;;
*'tifa-$app.socket'*) echo "app $user" >> "$d/calls" ;;
*'line=$(cat)'*) echo "authorize $user" >> "$d/calls"; cat >> "$d/stdin" ;;
*authorized_keys.XXXXXX*) echo "revoke $user" >> "$d/calls" ;;
*tunnel*) echo "tunnel $user" >> "$d/calls"; cat > /dev/null; echo "started tifa-tunnel.service" ;;
*) echo "other $user" >> "$d/calls" ;;
esac
`)
	if err := os.Chmod(prog, 0o755); err != nil {
		t.Fatal(err)
	}
	old := sshProgram
	sshProgram = prog
	t.Cleanup(func() { sshProgram = old })
	return &setupServer{dir: dir}
}

func (s *setupServer) deny(t *testing.T) { writeFile(t, filepath.Join(s.dir, "denied"), "") }
func (s *setupServer) calls() string {
	data, _ := os.ReadFile(filepath.Join(s.dir, "calls"))
	return string(data)
}
func (s *setupServer) stdin() string {
	data, _ := os.ReadFile(filepath.Join(s.dir, "stdin"))
	return string(data)
}

const hostKeysFact = "hostkey ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA\nhostkey ssh-rsa AAAAB3NzaC1yc2E=\n"

// factsOf is the probe output of a prepared server with the app set up on port.
func factsOf(app string, port int, extra string) string {
	return fmt.Sprintf("os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nport %d\nunit-socket %s\nunit-service %s\nmachine vps-1234\napt yes\n%s%s",
		port, sha(socketUnit(app, port)), sha(serviceUnit(app)), hostKeysFact, extra)
}

const freshFacts = "os Linux\narch x86_64\nhome /srv/tifa\nmachine vps-1234\napt yes\n" + hostKeysFact

func setupApp(t *testing.T, tifaYAML string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
	writeFile(t, filepath.Join(root, "tifa.yaml"), tifaYAML)
	for name, content := range files {
		writeFile(t, filepath.Join(root, name), content)
	}
	t.Setenv(deployHostEnv, "")
	// No cf unless the test installs one: the real cf is never run.
	oldHave := haveCF
	t.Cleanup(func() { haveCF = oldHave })
	haveCF = func() bool { return false }
	return root
}

// runSetup runs deploy setup with the given terminal state and answers.
func runSetup(t *testing.T, root string, terminal bool, input string, args ...string) (string, error) {
	t.Helper()
	out, _, err := runSetupIO(t, root, terminal, input, args...)
	return out, err
}

// runSetupIO is runSetup that also returns what went to stderr.
func runSetupIO(t *testing.T, root string, terminal bool, input string, args ...string) (string, string, error) {
	t.Helper()
	old := stdinIsTerminal
	t.Cleanup(func() { stdinIsTerminal = old })
	stdinIsTerminal = func(io.Reader) bool { return terminal }
	var out, errOut bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(append([]string{"deploy", "setup", "--dir", root}, args...))
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

// An unprepared server: tifa cannot log in, so the plan adds the server as root and then the app as
// tifa. Without a terminal and without --yes the plan is printed and nothing runs.
func TestSetupFreshServer(t *testing.T) {
	root := setupApp(t, "deploy:\n  host: vps\n", nil)
	srv := newSetupServer(t, "", freshFacts)
	srv.deny(t)

	out, err := runSetup(t, root, false, "")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("got %v", err)
	}
	for _, want := range []string{"Plan for shop on vps", "add     server: the user tifa", "add     app: /srv/tifa/shop with an empty shared/.env, port 9527"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(srv.calls(), "server root") || strings.Contains(srv.calls(), "app tifa") {
		t.Errorf("something ran:\n%s", srv.calls())
	}

	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(srv.calls(), "server root\nprobe tifa\napp tifa") {
		t.Errorf("order of calls:\n%s", srv.calls())
	}
	if strings.Count(out, "  done    ") != 2 {
		t.Errorf("one line per step:\n%s", out)
	}
}

// With a terminal setup asks: n changes nothing, y applies.
func TestSetupAsks(t *testing.T) {
	root := setupApp(t, "deploy:\n  host: vps\n", nil)
	srv := newSetupServer(t, "", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine m\n"+hostKeysFact)
	out, err := runSetup(t, root, true, "n\n")
	if err == nil || strings.Contains(srv.calls(), "app tifa") || !strings.Contains(out, "Apply this plan? [y/N]") {
		t.Fatalf("n: %v\n%s\n%s", err, out, srv.calls())
	}
	if _, err := runSetup(t, root, true, "y\n"); err != nil || !strings.Contains(srv.calls(), "app tifa") {
		t.Fatalf("y: %v\n%s", err, srv.calls())
	}
}

// A prepared server with the app in place: every line is ok and nothing runs, with no terminal and
// no --yes. A host that is an IP address gets the note about an ssh alias.
func TestSetupNothingToChange(t *testing.T) {
	root := setupApp(t, "deploy:\n  host: 203.0.113.7\n", nil)
	srv := newSetupServer(t, "", factsOf("shop", 9531, ""))
	out, err := runSetup(t, root, false, "", "--all")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"ok      server:", "ok      app: /srv/tifa/shop, port 9531", "Write an ssh alias instead"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(srv.calls(), "app ") {
		t.Errorf("calls:\n%s", srv.calls())
	}
	// An alias gets no note, and units that differ are written again.
	root = setupApp(t, "deploy:\n  host: vps\n", nil)
	newSetupServer(t, "", strings.Replace(factsOf("shop", 9531, ""), "unit-service "+sha(serviceUnit("shop")), "unit-service 00", 1))
	out, _ = runSetup(t, root, false, "")
	if strings.Contains(out, "alias") || !strings.Contains(out, "change  app: write the systemd units of shop again") {
		t.Errorf("out:\n%s", out)
	}
}

// cfEverything adds to fakeAccess what setup needs besides: the deletes.
type cfEverything struct {
	*fakeAccess
	deleted []string
}

func (c *cfEverything) install(t *testing.T) {
	c.fakeAccess.install(t)
	inner := runCF
	runCF = func(ctx context.Context, args ...string) ([]byte, error) {
		if strings.Contains(" "+strings.Join(args[:min(5, len(args))], " ")+" ", " delete ") {
			fakeMu.Lock()
			defer fakeMu.Unlock()
			c.calls = append(c.calls, strings.Join(args, " "))
			c.deleted = append(c.deleted, strings.Join(args, " "))
			c.applyDelete(args)
			return []byte(`{}`), nil
		}
		return inner(ctx, args...)
	}
}

func (c *cfEverything) applyDelete(args []string) {
	switch strings.Join(args[:min(3, len(args))], " ") {
	case "dns records delete":
		z := flag(args, "-z")
		c.records[z] = slices.DeleteFunc(c.records[z], func(r map[string]any) bool { return r["id"] == args[3] })
	case "rulesets rules delete":
		for _, rs := range c.rulesets {
			rs["rules"] = slices.DeleteFunc(rs["rules"].([]any), func(r any) bool { return r.(map[string]any)["id"] == args[3] })
		}
	case "zero-trust access service-tokens":
		c.tokens = slices.DeleteFunc(c.tokens, func(r map[string]any) bool { return r["id"] == args[4] })
	case "zero-trust access policies":
		c.policies = slices.DeleteFunc(c.policies, func(r map[string]any) bool { return r["id"] == args[4] })
	}
	if len(args) > 4 && args[2] == "applications" {
		c.apps = slices.DeleteFunc(c.apps, func(r map[string]any) bool { return r["id"] == args[4] })
	}
}

func setFacts(t *testing.T, srv *setupServer, facts string) {
	t.Helper()
	writeFile(t, filepath.Join(srv.dir, "facts"), facts)
}

// Everything of GitHub: with github true the plan adds the key, the connection and the workflow,
// secrets reach gh on its input and nowhere else, and a second run has nothing to do. With github
// false the plan removes what Tifa made and only that.
func TestSetupGitHub(t *testing.T) {
	root := setupApp(t, "deploy:\n  host: vps\n  ci: github\n", nil)
	srv := newSetupServer(t, "hostname 203.0.113.7\nport 2222\n", factsOf("shop", 9531, ""))
	gh := newFakeGH()
	gh.install(t)

	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"add     github: a key for acme/shop that may log in as tifa",
		"add     github: where CI logs in (direct)",
		"add     github: .github/workflows/tifa-deploy.yml",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if !strings.HasPrefix(gh.secrets[secretSSHKey], "-----BEGIN OPENSSH PRIVATE KEY-----") ||
		gh.secrets[secretHost] != "203.0.113.7" ||
		gh.secrets[secretKnownHosts] != "[203.0.113.7]:2222 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA\n[203.0.113.7]:2222 ssh-rsa AAAAB3NzaC1yc2E=" ||
		gh.vars[varMarker] == "" {
		t.Errorf("secrets %v, vars %v", gh.secrets, gh.vars)
	}
	if !strings.Contains(srv.stdin(), "restrict ssh-ed25519 ") || !strings.Contains(srv.stdin(), " tifa-github acme/shop") {
		t.Errorf("authorized line: %q", srv.stdin())
	}
	for _, v := range gh.secrets {
		if strings.Contains(out, v) || strings.Contains(strings.Join(gh.calls, "\n"), v) {
			t.Errorf("a secret value is in the output or on a command line")
		}
	}
	text, err := os.ReadFile(filepath.Join(root, githubWorkflowPath))
	if err != nil || !strings.HasPrefix(string(text), workflowMarker) || !strings.Contains(string(text), "push:") || !strings.Contains(string(text), "Port 2222") {
		t.Errorf("workflow %v:\n%s", err, text)
	}

	// Again: nothing to do.
	setFacts(t, srv, factsOf("shop", 9531, "ghkey acme/shop\n"))
	gh.calls = nil
	out, err = runSetup(t, root, false, "")
	if err != nil || !strings.Contains(out, "matches tifa.yaml") {
		t.Fatalf("second run: %v\n%s", err, out)
	}
	for _, c := range gh.calls {
		if strings.Contains(c, " set ") {
			t.Errorf("a second run set %s", c)
		}
	}

	// A host that moved: the connection changes, the key stays.
	newSetupServer(t, "hostname 198.51.100.9\nport 22\n", factsOf("shop", 9531, "ghkey acme/shop\n"))
	out, _ = runSetup(t, root, false, "")
	if !strings.Contains(out, "change  github: where CI logs in") || !strings.Contains(out, "Already right: server 1, app 1, GitHub 1") {
		t.Errorf("moved host:\n%s", out)
	}

	// github: false takes it away.
	writeFile(t, filepath.Join(root, "tifa.yaml"), "deploy:\n  host: vps\n")
	srv = newSetupServer(t, "hostname 203.0.113.7\nport 2222\n", factsOf("shop", 9531, "ghkey acme/shop\nghkey acme/other\n"))
	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"remove  github: .github/workflows/tifa-deploy.yml", "remove  server: the key of acme/shop", "remove  github: the secrets"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if len(gh.secrets) != 0 || len(gh.vars) != 0 || !strings.Contains(srv.calls(), "revoke tifa") {
		t.Errorf("left: %v %v\n%s", gh.secrets, gh.vars, srv.calls())
	}
	if _, err := os.Stat(filepath.Join(root, githubWorkflowPath)); err == nil {
		t.Error("the workflow is still there")
	}
}

// Only what Tifa made and marked goes: a workflow without the line of tifa, and secrets while the
// variable of tifa is missing, stay.
func TestSetupRemovesOnlyMarkedThings(t *testing.T) {
	root := setupApp(t, "deploy:\n  host: vps\n", map[string]string{githubWorkflowPath: "name: mine\non: push\n"})
	newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, "ghkey acme/other\n"))
	gh := newFakeGH()
	gh.secrets = map[string]string{secretHost: "mine", secretSSHKey: "mine"}
	gh.install(t)
	out, err := runSetup(t, root, false, "")
	if err != nil || !strings.Contains(out, "matches tifa.yaml") {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, githubWorkflowPath)); err != nil || len(gh.secrets) != 2 {
		t.Errorf("removed something: %v %v", err, gh.secrets)
	}
	// A workflow of the developer's own stays with github: true too.
	writeFile(t, filepath.Join(root, "tifa.yaml"), "deploy:\n  host: vps\n  ci: github\n")
	out, _ = runSetup(t, root, false, "", "--all")
	if !strings.Contains(out, "has no Tifa line at the top, so it is yours and stays") {
		t.Errorf("out:\n%s", out)
	}
}

// Without gh logged in the GitHub items cannot be done, and the plan says what is missing.
func TestSetupGitHubCannot(t *testing.T) {
	root := setupApp(t, "deploy:\n  host: vps\n  ci: github\n", nil)
	newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, ""))
	gh := newFakeGH()
	gh.loggedIn = false
	gh.install(t)
	out, err := runSetup(t, root, false, "")
	if err == nil || !strings.Contains(out, "1. github: deploys from GitHub Actions") || !strings.Contains(out, "gh is not logged in: run gh auth login") || !strings.Contains(out, "1 thing needs you") && !strings.Contains(err.Error(), "1 thing needs you") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func cfSetup(t *testing.T, zones ...string) *cfEverything {
	c := &cfEverything{fakeAccess: &fakeAccess{fakeCF: newFakeCF(zones...)}}
	c.install(t)
	return c
}

const tunnelFacts = "tunnel tunnel-1\ncloudflared /usr/bin/cloudflared\ntunnel-active yes\n"

// deploy.domain: the plan adds the tunnel, cloudflared, the route, the record and the rules, the
// token goes to the server and to nobody else, a second run has nothing to do, and removing the
// domain removes what was added for it.
func TestSetupDomain(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  host: vps\n  cdn: cloudflare\n", nil)
	srv := newSetupServer(t, "", factsOf("shop", 9531, ""))
	cf := cfSetup(t, "example.com")

	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"add     cloudflare: the tunnel of the server, tifa-vps-1234",
		"add     server: install cloudflared when missing and run the tunnel as the service tifa-tunnel, as root",
		"add     cloudflare: route shop.example.com -> http://127.0.0.1:9531",
		"add     cloudflare: DNS shop.example.com CNAME the tunnel, proxied",
		"add     cloudflare: cache rules for shop.example.com",
		"add     cloudflare: HTML rewriting off for shop.example.com",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret-of") || !strings.Contains(srv.calls(), "tunnel root") {
		t.Errorf("token in the output, or no tunnel script:\n%s\n%s", out, srv.calls())
	}
	wantWrites := []string{
		"tunnels create --name tifa-vps-1234 --config-source cloudflare",
		"tunnels config update tunnel-1",
		"dns records create -z example.com",
		"rulesets entrypoint update http_request_cache_settings -z example.com",
		"rulesets entrypoint update http_config_settings -z example.com",
	}
	if got := cf.writes(); strings.Join(got, "\n") != strings.Join(wantWrites, "\n") {
		t.Errorf("writes:\n%s", strings.Join(got, "\n"))
	}

	setFacts(t, srv, factsOf("shop", 9531, tunnelFacts))
	cf.calls = nil
	out, err = runSetup(t, root, false, "")
	if err != nil || !strings.Contains(out, "matches tifa.yaml") || len(cf.writes()) != 0 {
		t.Fatalf("second run: %v\n%s\n%v", err, out, cf.writes())
	}

	// A rule changed by hand is put back, and only it.
	cache := cf.rulesets["example.com http_request_cache_settings"]["rules"].([]any)[0].(map[string]any)
	cache["action_parameters"] = map[string]any{"cache": true}
	out, _ = runSetup(t, root, false, "")
	if !strings.Contains(out, "change  cloudflare: cache rules for shop.example.com") {
		t.Errorf("out:\n%s", out)
	}

	// Another domain for the app: the old one is removed, the other app's route stays.
	cf.configs["tunnel-1"]["ingress"] = []any{
		map[string]any{"hostname": "shop.example.com", "service": "http://127.0.0.1:9531"},
		map[string]any{"hostname": "notes.example.com", "service": "http://127.0.0.1:9600"},
		map[string]any{"service": "http_status:404"},
	}
	writeFile(t, filepath.Join(root, "tifa.yaml"), "deploy:\n  host: vps\n")
	cf.calls = nil
	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "remove  domain: shop.example.com, its route, DNS record and rules, which tifa made") {
		t.Fatalf("%v\n%s", err, out)
	}
	ingress, _ := json.Marshal(cf.configs["tunnel-1"]["ingress"])
	if strings.Contains(string(ingress), "shop.example.com") || !strings.Contains(string(ingress), "notes.example.com") || len(cf.records["example.com"]) != 0 {
		t.Errorf("ingress %s, records %v", ingress, cf.records)
	}
	for _, rs := range cf.rulesets {
		if n := len(rs["rules"].([]any)); n != 0 {
			t.Errorf("%d rules left", n)
		}
	}
}

// A record of the domain that tifa did not make is not removed, and nothing is removed for a
// route of another app.
func TestSetupLeavesForeignDNS(t *testing.T) {
	root := setupApp(t, "deploy:\n  host: vps\n", nil)
	newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, tunnelFacts))
	cf := cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	cf.configs["tunnel-1"] = map[string]any{"ingress": []any{
		map[string]any{"hostname": "shop.example.com", "service": "http://127.0.0.1:9531"},
		map[string]any{"service": "http_status:404"},
	}}
	cf.records["example.com"] = []map[string]any{{"id": "r1", "name": "shop.example.com", "type": "CNAME", "content": "tunnel-1.cfargotunnel.com"}}
	if _, err := runSetup(t, root, false, "", "--yes"); err != nil {
		t.Fatal(err)
	}
	if len(cf.records["example.com"]) != 1 {
		t.Error("a record without the mark of tifa was deleted")
	}
}

// Without cf the domain cannot be done, and the plan says so.
func TestSetupDomainNeedsCF(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  host: vps\n  cdn: cloudflare\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, ""))
	oldHave := haveCF
	t.Cleanup(func() { haveCF = oldHave })
	haveCF = func() bool { return false }
	out, err := runSetup(t, root, false, "")
	if err == nil || !strings.Contains(out, "1. domain: shop.example.com through the Cloudflare Tunnel") || !strings.Contains(out, "needs cf logged in: cf auth login") {
		t.Fatalf("%v\n%s", err, out)
	}
}

// purgeSetup is a setup with cdn cloudflare and ci github on a prepared server. Tokens are what
// the hidden prompt returns one after the other, and tried is what it was asked. The Cloudflare
// API accepts the token tok-good and answers 403 to any other.
type purgeSetup struct {
	root   string
	srv    *setupServer
	cf     *cfEverything
	gh     *fakeGH
	prompt []string
	asked  int
	checks []string // Bearer headers and bodies the API got
}

func newPurgeSetup(t *testing.T, tokens ...string) *purgeSetup {
	s := &purgeSetup{prompt: tokens}
	s.root = setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  host: vps\n  cdn: cloudflare\n  ci: github\n", nil)
	s.srv = newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, tunnelFacts))
	s.cf = cfSetup(t, "example.com")
	s.cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	s.gh = newFakeGH()
	s.gh.install(t)
	oldPrompt := promptSecret
	t.Cleanup(func() { promptSecret = oldPrompt })
	promptSecret = func(io.Reader, io.Writer, string) (string, error) {
		if s.asked >= len(s.prompt) {
			t.Fatalf("asked for a token %d times, the test has %d", s.asked+1, len(s.prompt))
		}
		s.asked++
		return s.prompt[s.asked-1], nil
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		s.checks = append(s.checks, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization")+" "+string(data))
		if r.Header.Get("Authorization") != "Bearer tok-good" {
			w.WriteHeader(403)
			io.WriteString(w, `{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`)
			return
		}
		io.WriteString(w, `{"success":true,"errors":[]}`)
	}))
	oldAPI := cloudflareAPI
	t.Cleanup(func() { api.Close(); cloudflareAPI = oldAPI })
	cloudflareAPI = api.URL
	return s
}

const purgeCheckBody = `{"files":["https://shop.example.com/.well-known/tifa-purge-check"]}`

// With a terminal, setup prints the link of the Cloudflare form, reads the token hidden, checks it
// against the zone, and hands it to gh on its input. A token that fails the check is asked again.
func TestSetupPurgeTokenTerminal(t *testing.T) {
	s := newPurgeSetup(t, "tok-bad", "tok-good")
	out, err := runSetup(t, s.root, true, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"add     cloudflare: a token that can only clear the cache of shop.example.com, pasted by you and kept as CF_CACHE_PURGE_TOKEN",
		"https://dash.cloudflare.com/?to=/:account/api-tokens&permissionGroupKeys=%5B%7B%22key%22%3A%22cache%22%2C%22type%22%3A%22purge%22%7D%5D&name=tifa%20cache%20purge%20acme%2Fshop",
		"Zone Resources",
		"the token was refused",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if s.gh.secrets[secretPurge] != "tok-good" {
		t.Errorf("secret %q", s.gh.secrets[secretPurge])
	}
	if strings.Contains(out, "tok-bad") || strings.Contains(out, "tok-good") {
		t.Errorf("a token was printed:\n%s", out)
	}
	want := []string{"POST /zones/z/purge_cache Bearer tok-bad " + purgeCheckBody, "POST /zones/z/purge_cache Bearer tok-good " + purgeCheckBody}
	if !slices.Equal(s.checks, want) {
		t.Errorf("checks %q", s.checks)
	}
	for _, c := range s.gh.calls {
		if strings.Contains(c, "tok-") {
			t.Errorf("a token is in an argument of gh: %s", c)
		}
	}
	text, _ := os.ReadFile(filepath.Join(s.root, githubWorkflowPath))
	if !strings.Contains(string(text), "CF_ZONE_ID: z") {
		t.Errorf("workflow:\n%s", text)
	}
	// The secret is there now, so a second run only says ok and asks nothing.
	s.asked = len(s.prompt) - 1 // one more ask would fail the test
	out, err = runSetup(t, s.root, true, "", "--yes", "--all")
	if err != nil || !strings.Contains(out, "ok      github: the secret CF_CACHE_PURGE_TOKEN") || strings.Contains(out, "pasted by you") {
		t.Errorf("second run: %v\n%s", err, out)
	}
}

// An empty line skips the token: the token is optional, so the run still succeeds and says what
// skipping costs. Three refused tokens give up with a warning, since the developer wanted one.
// Either way the way by hand is printed, nothing is stored, and the steps after it still run.
func TestSetupPurgeTokenSkippedOrRefused(t *testing.T) {
	for name, tokens := range map[string][]string{"empty": {""}, "refused three times": {"tok-1", "tok-2", "tok-3"}} {
		s := newPurgeSetup(t, tokens...)
		out, err := runSetup(t, s.root, true, "", "--yes")
		if name == "empty" {
			if err != nil || !strings.Contains(out, "skipped cloudflare: a token that can only clear the cache") || !strings.Contains(out, "Cache time") {
				t.Fatalf("%s: %v\n%s", name, err, out)
			}
		} else if err == nil || !strings.Contains(err.Error(), "1 thing needs you") || !strings.Contains(out, "warning cloudflare: a token that can only clear the cache") {
			t.Fatalf("%s: a refused token must fail the run: %v\n%s", name, err, out)
		}
		if !strings.Contains(out, "tifa deploy setup --cache-purge-token") {
			t.Errorf("%s:\n%s", name, out)
		}
		if s.gh.secrets[secretPurge] != "" || s.asked != len(tokens) {
			t.Errorf("%s: secret %q, asked %d", name, s.gh.secrets[secretPurge], s.asked)
		}
		if s.gh.secrets[secretSSHKey] == "" {
			t.Errorf("%s: the steps after the token did not run", name)
		}
		if name == "empty" && len(s.checks) != 0 {
			t.Errorf("an empty line was checked: %q", s.checks)
		}
		// Setup asks on the first CI setup only. Run again, it notes the command that adds the
		// token, asks nothing and succeeds.
		out, err = runSetup(t, s.root, true, "", "--yes")
		if err != nil || s.asked != len(tokens) || !strings.Contains(out, "(optional)") || !strings.Contains(out, "tifa deploy setup --cache-purge-token") {
			t.Errorf("%s: second run: %v, asked %d\n%s", name, err, s.asked, out)
		}
		if strings.Contains(out, "gh secret") {
			t.Errorf("%s: setup tells to run gh:\n%s", name, out)
		}
	}
}

// --cache-purge-token asks for the token even when the secret is there, and replaces it. It needs a
// terminal, deploy.cdn cloudflare and deploy.ci github.
func TestSetupCachePurgeTokenFlag(t *testing.T) {
	s := newPurgeSetup(t, "tok-good")
	s.gh.secrets[secretPurge] = "set earlier"
	out, err := runSetup(t, s.root, true, "", "--yes", "--cache-purge-token")
	if err != nil || !strings.Contains(out, "change  cloudflare: a token that can only clear the cache") || s.gh.secrets[secretPurge] != "tok-good" {
		t.Fatalf("%v, secret %q\n%s", err, s.gh.secrets[secretPurge], out)
	}
	if _, err := runSetup(t, s.root, false, "", "--yes", "--cache-purge-token"); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Errorf("no terminal: %v", err)
	}
	writeFile(t, filepath.Join(s.root, "tifa.yaml"), "site:\n  url: https://shop.example.com\ndeploy:\n  host: vps\n  ci: github\n")
	if _, err := runSetup(t, s.root, true, "", "--yes", "--cache-purge-token"); err == nil || !strings.Contains(err.Error(), "deploy.cdn") {
		t.Errorf("cdn none: %v", err)
	}
}

// Without a terminal there is nobody to paste a token: the plan notes, as optional, how to make it
// and set it and what going without costs. The run succeeds.
func TestSetupPurgeTokenNoTerminal(t *testing.T) {
	s := newPurgeSetup(t)
	out, err := runSetup(t, s.root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "gh secret") {
		t.Errorf("setup tells to run gh:\n%s", out)
	}
	for _, want := range []string{"1. cloudflare: a token that can only clear the cache of shop.example.com", "(optional)", "Cache time", "tifa deploy setup --cache-purge-token"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if s.asked != 0 || len(s.checks) != 0 || s.gh.secrets[secretPurge] != "" {
		t.Errorf("asked %d, checks %q, secret %q", s.asked, s.checks, s.gh.secrets[secretPurge])
	}
}

// A secret that is already there is ok, whatever its value: GitHub does not show it.
func TestSetupPurgeTokenPresent(t *testing.T) {
	s := newPurgeSetup(t)
	s.gh.secrets[secretPurge] = "set earlier"
	out, err := runSetup(t, s.root, true, "", "--yes", "--all")
	if err != nil || !strings.Contains(out, "ok      github: the secret CF_CACHE_PURGE_TOKEN") || s.gh.secrets[secretPurge] != "set earlier" || s.asked != 0 {
		t.Errorf("%v\n%s", err, out)
	}
}

// A secret under the old name CF_PURGE_TOKEN is removed, and the new one is asked for.
func TestSetupPurgeTokenOldName(t *testing.T) {
	s := newPurgeSetup(t, "tok-good")
	if _, err := runSetup(t, s.root, true, "", "--yes"); err != nil {
		t.Fatal(err)
	}
	s.gh.secrets["CF_PURGE_TOKEN"] = "old"
	out, err := runSetup(t, s.root, true, "", "--yes")
	if err != nil || !strings.Contains(out, "remove  github: the secret CF_PURGE_TOKEN, the old name of CF_CACHE_PURGE_TOKEN") {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, ok := s.gh.secrets["CF_PURGE_TOKEN"]; ok {
		t.Error("the old secret is still there")
	}
}

// Without cdn, or without ci, the secret tifa set goes, and the plan says to delete the token in
// Cloudflare, which tifa cannot do.
func TestSetupPurgeTokenRemoved(t *testing.T) {
	for name, yaml := range map[string]string{
		"cdn none": "site:\n  url: https://shop.example.com\ndeploy:\n  host: vps\n  ci: github\n",
		"ci none":  "site:\n  url: https://shop.example.com\ndeploy:\n  host: vps\n  cdn: cloudflare\n",
	} {
		s := newPurgeSetup(t, "tok-good")
		if _, err := runSetup(t, s.root, true, "", "--yes"); err != nil || s.gh.secrets[secretPurge] != "tok-good" {
			t.Fatalf("%s: setup: %v", name, err)
		}
		writeFile(t, filepath.Join(s.root, "tifa.yaml"), yaml)
		setFacts(t, s.srv, factsOf("shop", 9531, tunnelFacts+"ghkey acme/shop\n"))
		out, err := runSetup(t, s.root, true, "", "--yes")
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		if !strings.Contains(out, "remove  github: the secret CF_CACHE_PURGE_TOKEN, which tifa set") || !strings.Contains(out, `delete the token "tifa cache purge acme/shop" in the Cloudflare dashboard`) {
			t.Errorf("%s:\n%s", name, out)
		}
		if _, ok := s.gh.secrets[secretPurge]; ok {
			t.Errorf("%s: the secret is still there", name)
		}
	}
}

// deploy.ssh cloudflare: the ssh host name goes on the tunnel with an Access application that
// admits the token of the repository. Back to direct, the Access objects go.
func TestSetupCloudflareSSH(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://deploy-a.example.com\ndeploy:\n  host: vps\n  cdn: cloudflare\n  ssh: cloudflare\n  ci: github\n", nil)
	srv := newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts))
	cf := cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	gh := newFakeGH()
	gh.install(t)
	// The cache token needs a terminal to paste on. It is optional, so setup only notes it.
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "(optional)") {
		t.Fatalf("%v\n%s", err, out)
	}
	if gh.secrets[secretHost] != "deploy-a-ssh.example.com" || gh.secrets[secretCFID] == "" || gh.secrets[secretCFSecret] == "" {
		t.Errorf("secrets %v", gh.secrets)
	}
	if !strings.Contains(gh.secrets[secretKnownHosts], "deploy-a-ssh.example.com ssh-ed25519") {
		t.Errorf("known hosts %q", gh.secrets[secretKnownHosts])
	}
	if len(cf.apps) != 1 || cf.apps[0]["domain"] != "deploy-a-ssh.example.com" {
		t.Errorf("apps %v", cf.apps)
	}
	text, _ := os.ReadFile(filepath.Join(root, githubWorkflowPath))
	if !strings.Contains(string(text), "ProxyCommand cloudflared access ssh") {
		t.Errorf("workflow:\n%s", text)
	}

	// A second run: nothing, and no new token.
	setFacts(t, srv, factsOf("shop", 9531, tunnelFacts+"ghkey acme/shop\n"))
	cf.calls = nil
	secret := gh.secrets[secretCFSecret]
	out, _ = runSetup(t, root, false, "", "--yes")
	if gh.secrets[secretCFSecret] != secret || !strings.Contains(out, "Already right: server 2, app 1, Cloudflare 8, GitHub 3") {
		t.Errorf("a second run changed the token:\n%s", out)
	}

	// Back to direct.
	writeFile(t, filepath.Join(root, "tifa.yaml"), "site:\n  url: https://deploy-a.example.com\ndeploy:\n  host: vps\n  cdn: cloudflare\n  ci: github\n")
	newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, tunnelFacts+"ghkey acme/shop\n"))
	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "remove  cloudflare: the Access service token, policy and application of acme/shop") {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(cf.apps) != 0 || len(cf.tokens) != 0 || len(cf.policies) != 0 || gh.secrets[secretCFID] != "" {
		t.Errorf("left: apps %v tokens %v policies %v secrets %v", cf.apps, cf.tokens, cf.policies, gh.secrets)
	}
	ingress, _ := json.Marshal(cf.configs["tunnel-1"]["ingress"])
	if strings.Contains(string(ingress), "deploy-a-ssh") {
		t.Errorf("the ssh route is still there: %s", ingress)
	}
}

// Tailscale secrets are for the developer to add.
func TestSetupSSHModesNeedThings(t *testing.T) {
	root := setupApp(t, "deploy:\n  host: vps\n  ssh: tailscale\n  ci: github\n", nil)
	newSetupServer(t, "hostname vps.tail1234.ts.net\n", factsOf("shop", 9531, ""))
	newFakeGH().install(t)
	out, _ := runSetup(t, root, false, "")
	if !strings.Contains(out, "1. github: the Tailscale secrets TS_OAUTH_CLIENT_ID and TS_OAUTH_SECRET") || !strings.Contains(out, "tifa deploy setup --tailscale-oauth") || strings.Contains(out, "gh secret") {
		t.Errorf("out:\n%s", out)
	}
}

// tailscaleSetup is a setup with ssh tailscale and ci github. The hidden prompt returns answers
// one after the other. The Tailscale API gives a token for the client id-1 with the secret
// ts-good, asked for the scope auth_keys and the tag tag:ci, and answers 401 otherwise.
func tailscaleSetup(t *testing.T, answers ...string) (root string, gh *fakeGH, asked *int, checks *[]string) {
	root = setupApp(t, "deploy:\n  host: vps\n  ssh: tailscale\n  ci: github\n", nil)
	newSetupServer(t, "hostname vps.tail1234.ts.net\n", factsOf("shop", 9531, ""))
	gh = newFakeGH()
	gh.install(t)
	asked, checks = new(int), new([]string)
	oldPrompt := promptSecret
	t.Cleanup(func() { promptSecret = oldPrompt })
	promptSecret = func(io.Reader, io.Writer, string) (string, error) {
		if *asked >= len(answers) {
			t.Fatalf("asked %d times, the test has %d answers", *asked+1, len(answers))
		}
		*asked++
		return answers[*asked-1], nil
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		*checks = append(*checks, r.Method+" "+r.URL.Path+" "+r.PostForm.Encode())
		if r.PostForm.Get("client_id") != "id-1" || r.PostForm.Get("client_secret") != "ts-good" || r.PostForm.Get("scope") != "auth_keys" || r.PostForm.Get("tags") != "tag:ci" {
			w.WriteHeader(401)
			io.WriteString(w, `{"message":"invalid client"}`)
			return
		}
		io.WriteString(w, `{"access_token":"tskey-api-x","token_type":"Bearer","expires_in":3600,"scope":"auth_keys"}`)
	}))
	old := tailscaleAPI
	t.Cleanup(func() { api.Close(); tailscaleAPI = old })
	tailscaleAPI = api.URL
	return root, gh, asked, checks
}

// On a terminal setup asks for the OAuth client, checks it can make keys for tag:ci, and keeps it.
// A client that fails the check is asked again. A second run asks nothing, and --tailscale-oauth
// asks again and replaces it.
func TestSetupTailscaleOAuth(t *testing.T) {
	root, gh, asked, checks := tailscaleSetup(t, "id-1", "ts-bad", "id-1", "ts-good", "id-1", "ts-good")
	out, err := runSetup(t, root, true, "", "--yes")
	if err != nil || gh.secrets[secretTSID] != "id-1" || gh.secrets[secretTSSecret] != "ts-good" || *asked != 4 {
		t.Fatalf("%v, secrets %v, asked %d\n%s", err, gh.secrets, *asked, out)
	}
	if !strings.Contains(out, "add     github: the Tailscale OAuth client") || !strings.Contains(out, "was refused") || strings.Contains(out, "ts-good") || strings.Contains(out, "ts-bad") {
		t.Errorf("out:\n%s", out)
	}
	if len(*checks) != 2 || !strings.HasPrefix((*checks)[0], "POST /api/v2/oauth/token ") {
		t.Errorf("checks %q", *checks)
	}
	for _, c := range gh.calls {
		if strings.Contains(c, "ts-good") {
			t.Errorf("the secret is in an argument of gh: %s", c)
		}
	}
	out, err = runSetup(t, root, true, "", "--yes", "--all")
	if err != nil || *asked != 4 || !strings.Contains(out, "ok      github: the Tailscale secrets") || !strings.Contains(out, "--tailscale-oauth") {
		t.Errorf("second run: %v, asked %d\n%s", err, *asked, out)
	}
	gh.secrets[secretTSSecret] = "old"
	out, err = runSetup(t, root, true, "", "--yes", "--tailscale-oauth")
	if err != nil || gh.secrets[secretTSSecret] != "ts-good" || *asked != 6 || !strings.Contains(out, "change  github: the Tailscale OAuth client") {
		t.Errorf("--tailscale-oauth: %v, asked %d\n%s", err, *asked, out)
	}
}

// An empty client ID skips, which leaves CI unable to reach the server, so the run fails. The flag
// needs ssh tailscale and a terminal.
func TestSetupTailscaleOAuthSkippedOrMisused(t *testing.T) {
	root, gh, _, _ := tailscaleSetup(t, "")
	out, err := runSetup(t, root, true, "", "--yes")
	if err == nil || !strings.Contains(err.Error(), "1 thing needs you") || gh.secrets[secretTSID] != "" || !strings.Contains(out, "tifa deploy setup --tailscale-oauth") {
		t.Errorf("skipped: %v\n%s", err, out)
	}
	if _, err := runSetup(t, root, false, "", "--yes", "--tailscale-oauth"); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Errorf("no terminal: %v", err)
	}
	writeFile(t, filepath.Join(root, "tifa.yaml"), "deploy:\n  host: vps\n  ci: github\n")
	if _, err := runSetup(t, root, true, "", "--yes", "--tailscale-oauth"); err == nil || !strings.Contains(err.Error(), "deploy.ssh") {
		t.Errorf("ssh direct: %v", err)
	}
}

// While it plans, setup says which part it is reading, on stderr, one line each.
func TestSetupProgressOnStderr(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://deploy-a.example.com\ndeploy:\n  host: vps\n  cdn: cloudflare\n  ssh: cloudflare\n  ci: github\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts))
	cfSetup(t, "example.com")
	newFakeGH().install(t)
	out, errOut, err := runSetupIO(t, root, false, "")
	if err == nil {
		t.Fatalf("no --yes and no terminal, but no error:\n%s", out)
	}
	want := "Reading the server...\nReading Cloudflare...\nReading GitHub...\n"
	if errOut != want {
		t.Errorf("stderr:\n%q\nwant\n%q", errOut, want)
	}
	if strings.Contains(out, "Reading") {
		t.Errorf("progress went to stdout:\n%s", out)
	}

	// Nothing of Cloudflare or GitHub is asked for here, so nothing is said about them.
	root = setupApp(t, "deploy:\n  host: vps\n", nil)
	_, errOut, _ = runSetupIO(t, root, false, "")
	if errOut != "Reading the server...\n" {
		t.Errorf("stderr: %q", errOut)
	}
}

// One plan reads each thing once, however many steps look at it.
func TestSetupPlanReadsOnce(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://deploy-a.example.com\ndeploy:\n  host: vps\n  cdn: cloudflare\n  ssh: cloudflare\n  ci: github\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts))
	cf := cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	gh := newFakeGH()
	gh.install(t)
	if _, err := runSetup(t, root, false, ""); err == nil {
		t.Fatal("no --yes and no terminal, but no error")
	}
	seen := map[string]int{}
	for _, c := range append(slices.Clone(cf.calls), gh.calls...) {
		seen[c]++
	}
	if len(seen) < 10 {
		t.Fatalf("too few calls, did the plan run?\n%v", seen)
	}
	for c, n := range seen {
		if n != 1 {
			t.Errorf("%q ran %d times in one plan", c, n)
		}
	}
}

// The reads that do not depend on each other run together: with every call slow, the plan takes
// far less than all its calls one after the other.
func TestSetupPlanReadsTogether(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://deploy-a.example.com\ndeploy:\n  host: vps\n  cdn: cloudflare\n  ssh: cloudflare\n  ci: github\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts))
	cf := cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	gh := newFakeGH()
	gh.install(t)
	const delay = 100 * time.Millisecond
	innerCF, innerGH := runCF, runGH
	runCF = func(ctx context.Context, args ...string) ([]byte, error) {
		time.Sleep(delay)
		return innerCF(ctx, args...)
	}
	runGH = func(ctx context.Context, dir string, in io.Reader, args ...string) ([]byte, error) {
		time.Sleep(delay)
		return innerGH(ctx, dir, in, args...)
	}
	start := time.Now()
	if _, err := runSetup(t, root, false, ""); err == nil {
		t.Fatal("no --yes and no terminal, but no error")
	}
	calls := len(cf.calls) + len(gh.calls)
	took := time.Since(start)
	t.Logf("%d calls, %v", calls, took)
	if took > time.Duration(calls)*delay/2 {
		t.Errorf("%d calls of %v took %v: they ran one after the other", calls, delay, took)
	}
}

// Hidden ok lines: only the changes show, and one line counts what is right, by area.
func TestSetupHidesOKLines(t *testing.T) {
	root := setupApp(t, "deploy:\n  host: vps\n", nil)
	newSetupServer(t, "", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine m\n"+hostKeysFact)
	out, err := runSetup(t, root, false, "")
	if err == nil {
		t.Fatalf("no --yes and no terminal, but no error:\n%s", out)
	}
	for _, want := range []string{"Plan for shop on vps", "add     app:", "Already right: server 1 (--all lists them)"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ok      ") {
		t.Errorf("an ok line shows:\n%s", out)
	}
	if strings.Index(out, "add     app:") > strings.Index(out, "Already right") {
		t.Errorf("order:\n%s", out)
	}
}

// --all lists the ok lines and drops the summary.
func TestSetupAllListsOKLines(t *testing.T) {
	root := setupApp(t, "deploy:\n  host: vps\n", nil)
	newSetupServer(t, "", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine m\n"+hostKeysFact)
	out, _ := runSetup(t, root, false, "", "--all")
	if !strings.Contains(out, "ok      server:") || strings.Contains(out, "Already right") {
		t.Errorf("out:\n%s", out)
	}
}

// Everything is right and nobody has to do anything: one line, no plan, no question.
func TestSetupAllInOrderOneLine(t *testing.T) {
	root := setupApp(t, "deploy:\n  host: vps\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, ""))
	out, err := runSetup(t, root, true, "")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out != "shop on vps matches tifa.yaml: 2 things checked (--all lists them).\n" {
		t.Errorf("out: %q", out)
	}
}

// What setup cannot do comes last, numbered, with the reason below each item. It makes the exit
// non-zero, after everything else was done.
func TestSetupCannotListedLast(t *testing.T) {
	root := setupApp(t, "deploy:\n  host: vps\n  ci: github\n", nil)
	newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, ""))
	gh := newFakeGH()
	gh.loggedIn = false
	gh.install(t)
	out, err := runSetup(t, root, false, "")
	if err == nil || !strings.Contains(err.Error(), "1 thing needs you, see above") {
		t.Fatalf("err %v\n%s", err, out)
	}
	want := "Plan for shop on vps\n\nAlready right: server 1, app 1 (--all lists them)\n\nYou need to do 1 thing that setup cannot:\n  1. github: deploys from GitHub Actions\n     gh is not logged in: run gh auth login (gh auth status: You are not logged into any GitHub hosts)\n\n"
	if out != want || strings.Contains(out, "cannot  ") || strings.Contains(out, "Apply this plan") {
		t.Errorf("out:\n%s", out)
	}
}

// With changes and a cannot item, setup asks, applies, and then fails.
func TestSetupCannotAfterApply(t *testing.T) {
	root := setupApp(t, "deploy:\n  host: vps\n  ci: github\n", nil)
	srv := newSetupServer(t, "hostname 203.0.113.7\n", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine m\n"+hostKeysFact)
	gh := newFakeGH()
	gh.loggedIn = false
	gh.install(t)
	out, err := runSetup(t, root, false, "", "--yes")
	if err == nil || !strings.Contains(err.Error(), "needs you") || !strings.Contains(srv.calls(), "app tifa") {
		t.Fatalf("err %v\n%s\n%s", err, out, srv.calls())
	}
	if strings.Index(out, "  done    app:") < strings.Index(out, "You need to do") || strings.Contains(out, "could not be done") {
		t.Errorf("out:\n%s", out)
	}
}

// The IP address note is an optional item. Alone it does not make setup fail.
func TestSetupIPNoteIsOptionalItem(t *testing.T) {
	root := setupApp(t, "deploy:\n  host: 203.0.113.7\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, ""))
	out, err := runSetup(t, root, false, "")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"You need to do 1 thing that setup cannot:", "  1. deploy.host is the address 203.0.113.7 (optional)", "Write an ssh alias instead", "Already right: server 1, app 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Note:") || strings.Contains(out, "matches tifa.yaml") {
		t.Errorf("out:\n%s", out)
	}
}
