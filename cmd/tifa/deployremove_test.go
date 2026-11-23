package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runRemove runs deploy remove with the given terminal state and input.
func runRemove(t *testing.T, root string, terminal bool, input string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	old := stdinIsTerminal
	t.Cleanup(func() { stdinIsTerminal = old })
	stdinIsTerminal = func(io.Reader) bool { return terminal }
	var out, errOut bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(append([]string{"deploy", "remove", "--dir", root}, args...))
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

// removeApp is the app shop deployed on a fake server that runs the scripts, next to the app
// notes, which stays.
func removeApp(t *testing.T) (root, home string, calls func() string) {
	t.Helper()
	root, home, calls = envApp(t, "200")
	prepareFakeServer(t, home)
	writeFile(t, filepath.Join(home, "shop", "shared", "app.db"), "data")
	writeFile(t, filepath.Join(home, "shop", "deploys.log"), "2026-11-20T10:00:05Z\tr1\tabc\tdeploy\tok\n")
	units := filepath.Join(home, ".config", "systemd", "user")
	for _, app := range []string{"shop", "notes"} {
		writeFile(t, filepath.Join(units, "tifa-"+app+".socket"), socketUnit(app, 9531))
		writeFile(t, filepath.Join(units, "tifa-"+app+".service"), serviceUnit(app))
	}
	writeFile(t, filepath.Join(home, "notes", "port"), "9532\n")
	writeFile(t, filepath.Join(home, "notes", "shared", ".env"), "B=2\n")
	return root, home, calls
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// remove prints the plan, and takes the app away only once its name is typed on a terminal, or
// with --yes without one. It stops and disables the units, deletes them and the folder of the app,
// and leaves the other app alone. A second run finds nothing and says so in one line.
func TestDeployRemoveServer(t *testing.T) {
	root, home, calls := removeApp(t)
	shop := filepath.Join(home, "shop")
	units := filepath.Join(home, ".config", "systemd", "user")

	out, _, err := runRemove(t, root, false, "")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("no terminal, no --yes: %v\n%s", err, out)
	}
	for _, want := range []string{"Plan to remove shop from vps", "remove  server: stop and disable tifa-shop.socket and tifa-shop.service"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
	out, _, err = runRemove(t, root, true, "notes\n", "--yes")
	if err == nil || !strings.Contains(out, "type the name of the app") || !exists(shop) {
		t.Fatalf("a wrong name: %v\n%s", err, out)
	}
	if strings.Contains(calls(), "disable") {
		t.Fatalf("something ran:\n%s", calls())
	}

	out, _, err = runRemove(t, root, true, "shop\n")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if exists(shop) || exists(filepath.Join(units, "tifa-shop.socket")) || exists(filepath.Join(units, "tifa-shop.service")) {
		t.Errorf("shop is still there:\n%s", out)
	}
	if !exists(filepath.Join(home, "notes", "shared", ".env")) || !exists(filepath.Join(units, "tifa-notes.service")) {
		t.Error("notes was touched")
	}
	for _, want := range []string{"systemctl --user stop tifa-shop.socket tifa-shop.service", "systemctl --user disable --quiet tifa-shop.socket tifa-shop.service", "systemctl --user daemon-reload"} {
		if !strings.Contains(calls(), want) {
			t.Errorf("calls lack %q:\n%s", want, calls())
		}
	}
	if strings.Contains(calls(), "ssh root") {
		t.Errorf("root was used:\n%s", calls())
	}

	out, _, err = runRemove(t, root, false, "")
	if err != nil || out != "Nothing of shop is left to remove on vps.\n" {
		t.Errorf("second run: %v\n%q", err, out)
	}
}

// Without a terminal --yes removes.
func TestDeployRemoveYes(t *testing.T) {
	root, home, _ := removeApp(t)
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err != nil || exists(filepath.Join(home, "shop")) || !strings.Contains(out, "  done    server: stop and disable") {
		t.Errorf("%v\n%s", err, out)
	}
}

// removeFacts is the probe of a server with the app shop, the tunnel that tifa runs and the key of
// acme/shop, with more facts after.
func removeFacts(extra string) string {
	return factsOf("shop", 9531, tunnelFacts+"ghkey acme/shop\nappdir yes\ncloudflared-tifa yes\npurge-file yes\npurge-zone example.com\n"+extra)
}

// cloudSetup is the app shop with every part of setup in place: the tunnel, the route, the DNS
// record and rules of its domain, ssh through Cloudflare with Access, and GitHub.
func cloudSetup(t *testing.T) (root string, srv *setupServer, cf *cfEverything, gh *fakeGH) {
	t.Helper()
	root = setupApp(t, "site:\n  url: https://deploy-a.example.com\ndeploy:\n  front: cloudflare\n  ssh: cloudflare\n  ci: github\n", nil)
	srv = newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts))
	cf = cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	gh = newFakeGH()
	gh.install(t)
	if out, err := runSetup(t, root, false, "", "--yes"); err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	if len(cf.records["example.com"]) != 2 || len(cf.apps) != 1 || gh.vars[varMarker] == "" {
		t.Fatalf("setup made too little: records %v, apps %v, vars %v", cf.records, cf.apps, gh.vars)
	}
	return root, srv, cf, gh
}

// With nothing else of tifa on the server, remove takes back what setup made in Cloudflare, on the
// server and in GitHub: the route, record and rules of the domain, the Access of the repository,
// the app, cloudflared and the tunnel, the key and the secrets. The workflow, a file of the
// repository, stays, and the output says so.
func TestDeployRemoveEverything(t *testing.T) {
	root, srv, cf, gh := cloudSetup(t)
	setFacts(t, srv, removeFacts(""))
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"remove  domain: deploy-a.example.com, its route, DNS record and rules",
		"remove  cloudflare: the Access service token, policy and application of acme/shop",
		"remove  server: the key of acme/shop in authorized_keys of tifa",
		"remove  github: the secrets",
		"remove  server: stop and disable tifa-shop.socket and tifa-shop.service",
		"remove  server: stop cloudflared, the service tifa-tunnel, delete its units and /etc/tifa, and uninstall cloudflared, which setup installed, as root",
		"remove  cloudflare: the tunnel of the server, tifa-vps-1234, which no other app uses",
		"remove  server: delete /srv/tifa/.config/tifa/cloudflare-purge, the tokens that clear the CDN cache, which no other app uses",
		"Kept .github/workflows/tifa-deploy.yml, a file of your repository: the next push fails to deploy until you delete it.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if len(cf.tunnels) != 0 || len(cf.records["example.com"]) != 0 || len(cf.apps) != 0 || len(cf.tokens) != 0 || len(cf.policies) != 0 {
		t.Errorf("left in Cloudflare: tunnels %v records %v apps %v tokens %v policies %v", cf.tunnels, cf.records, cf.apps, cf.tokens, cf.policies)
	}
	for _, rs := range cf.rulesets {
		if n := len(rs["rules"].([]any)); n != 0 {
			t.Errorf("%d rules left", n)
		}
	}
	if len(gh.secrets) != 0 || len(gh.vars) != 0 {
		t.Errorf("left in GitHub: %v %v", gh.secrets, gh.vars)
	}
	calls := srv.calls()
	for _, want := range []string{"revoke tifa", "removeapp tifa", "untunnel root", "purgeforget tifa"} {
		if !strings.Contains(calls, want) {
			t.Errorf("calls lack %q:\n%s", want, calls)
		}
	}
	if strings.Index(calls, "removeapp tifa") > strings.Index(calls, "untunnel root") {
		t.Errorf("cloudflared stopped before the app:\n%s", calls)
	}
	if last := cf.deleted[len(cf.deleted)-1]; last != "tunnels delete tunnel-1 --force" {
		t.Errorf("the tunnel went before the rest: %v", cf.deleted)
	}
	if !exists(filepath.Join(root, githubWorkflowPath)) {
		t.Error("the workflow was deleted")
	}

	// Again: nothing is left, and one line says so.
	setFacts(t, srv, "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine vps-1234\napt yes\n"+hostKeysFact)
	out, _, err = runRemove(t, root, false, "")
	if err != nil || out != "Nothing of shop is left to remove on vps, in Cloudflare, in GitHub.\n" {
		t.Errorf("second run: %v\n%q", err, out)
	}
}

// Another app of tifa on the server keeps the tunnel and cloudflared, which serve it too, and the
// file of purge tokens, which it may use.
func TestDeployRemoveKeepsTheSharedTunnel(t *testing.T) {
	root, srv, cf, _ := cloudSetup(t)
	ingress := cf.configs["tunnel-1"]["ingress"].([]any)
	cf.configs["tunnel-1"]["ingress"] = append([]any{map[string]any{"hostname": "notes.example.com", "service": "http://127.0.0.1:9600"}}, ingress...)
	setFacts(t, srv, removeFacts("taken 9600\nother notes\n"))
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "tifa-tunnel") || strings.Contains(srv.calls(), "untunnel") || cf.tunnels["tunnel-1"] == "" || strings.Contains(out, "cloudflare-purge") || strings.Contains(srv.calls(), "purgeforget") {
		t.Errorf("the shared tunnel went:\n%s\n%s", out, srv.calls())
	}
	routes, _ := json.Marshal(cf.configs["tunnel-1"]["ingress"])
	if !strings.Contains(string(routes), "notes.example.com") || strings.Contains(string(routes), "deploy-a.example.com") {
		t.Errorf("routes %s", routes)
	}
}

// Without the logins of cf and gh, what needs them is listed with the login to run, the server
// part is still removed, and remove then fails.
func TestDeployRemoveWithoutLogins(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://deploy-a.example.com\ndeploy:\n  front: cloudflare\n  ci: github\n", nil)
	srv := newSetupServer(t, "", removeFacts(""))
	gh := newFakeGH()
	gh.loggedIn = false
	gh.install(t)
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err == nil || !strings.Contains(err.Error(), "2 things need you") {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"You need to do 2 things that remove cannot:",
		"cf is not installed: pnpm add --global cf, then cf auth login",
		"gh is not logged in: run gh auth login",
		"  done    server: stop and disable tifa-shop.socket",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(srv.calls(), "removeapp tifa") || strings.Contains(out, "gh secret") || strings.Contains(out, "dashboard") {
		t.Errorf("calls:\n%s\nout:\n%s", srv.calls(), out)
	}
}

// With cf installed but not logged in, remove asks for the login only.
func TestDeployRemoveCFNotLoggedIn(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://deploy-a.example.com\ndeploy:\n  front: cloudflare\n", nil)
	newSetupServer(t, "", removeFacts(""))
	oldRun, oldHave := runCF, haveCF
	t.Cleanup(func() { runCF, haveCF = oldRun, oldHave })
	haveCF = func() bool { return true }
	runCF = func(_ context.Context, args ...string) ([]byte, error) {
		return nil, &cfError{args: args, stderr: "not logged in", err: errors.New("exit status 1")}
	}
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err == nil || !strings.Contains(out, "needs cf logged in: cf auth login") || strings.Contains(out, "not installed") {
		t.Fatalf("%v\n%s", err, out)
	}
}

// The Tailscale secrets setup set go with the others.
func TestDeployRemoveTailscaleSecrets(t *testing.T) {
	eachBackend(t, func(t *testing.T, rest bool) {
		root := setupApp(t, "deploy:\n  ssh: tailscale\n  ci: github\n", nil)
		newSetupServer(t, "", factsOf("shop", 9531, "ghkey acme/shop\nappdir yes\n"))
		gh := newFakeGH()
		for _, n := range []string{secretSSHKey, secretKnownHosts, secretHost, secretZoneID, secretTSID, secretTSSecret} {
			gh.secrets[n] = "set by setup"
		}
		gh.vars[varMarker] = "fingerprint"
		gh.installBackend(t, rest)
		out, _, err := runRemove(t, root, false, "", "--yes")
		if err != nil || len(gh.secrets) != 0 || len(gh.vars) != 0 {
			t.Errorf("%v, left %v %v\n%s", err, gh.secrets, gh.vars, out)
		}
	})
}

// A first run without cf removed the app from the server. Once cf is logged in, a run again finds
// the route, the record and the rules of the domain without the app and its port, and the tunnel.
func TestDeployRemoveCloudflareLater(t *testing.T) {
	root, srv, cf, _ := cloudSetup(t)
	writeFile(t, filepath.Join(root, "tifa.yaml"), "site:\n  url: https://deploy-a.example.com\ndeploy:\n  front: cloudflare\n")
	setFacts(t, srv, factsOf("shop", 9531, tunnelFacts+"appdir yes\n"))
	have := haveCF
	haveCF = func() bool { return false }
	if out, _, err := runRemove(t, root, false, "", "--yes"); err == nil || !strings.Contains(srv.calls(), "removeapp tifa") {
		t.Fatalf("without cf: %v\n%s", err, out)
	}
	haveCF = have
	// The app is gone, the tunnel is still on the server.
	setFacts(t, srv, "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine vps-1234\napt yes\n"+tunnelFacts+hostKeysFact)
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "remove  domain: deploy-a.example.com, its route, DNS record and rules") {
		t.Fatalf("with cf: %v\n%s", err, out)
	}
	routes, _ := json.Marshal(cf.configs["tunnel-1"]["ingress"])
	if strings.Contains(string(routes), "deploy-a.example.com") || len(cf.tunnels) != 0 {
		t.Errorf("routes %s, tunnels %v", routes, cf.tunnels)
	}
	for _, r := range cf.records["example.com"] {
		if r["name"] == "deploy-a.example.com" {
			t.Errorf("the record is still there: %v", r)
		}
	}
	for _, rs := range cf.rulesets {
		for _, r := range rs["rules"].([]any) {
			if strings.Contains(r.(map[string]any)["description"].(string), "deploy-a.example.com") {
				t.Errorf("a rule is still there: %v", r)
			}
		}
	}
}

// When a step fails, the tunnel and cloudflared stay, so the next run still finds the tunnel what
// was left points at.
func TestDeployRemoveKeepsTheTunnelAfterAFailure(t *testing.T) {
	root, srv, cf, _ := cloudSetup(t)
	setFacts(t, srv, removeFacts(""))
	inner := runCF
	runCF = func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "dns" && args[2] == "delete" {
			return nil, errors.New("cf dns records delete: 500 Internal Server Error")
		}
		return inner(ctx, args...)
	}
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err == nil || !strings.Contains(out, "warning domain: deploy-a.example.com") {
		t.Fatalf("%v\n%s", err, out)
	}
	if cf.tunnels["tunnel-1"] == "" || strings.Contains(srv.calls(), "untunnel") || !strings.Contains(srv.calls(), "removeapp tifa") {
		t.Errorf("the tunnel went after a failure:\n%s\n%s", out, srv.calls())
	}
	if !strings.Contains(out, "warning cloudflare: the tunnel of the server") {
		t.Errorf("out:\n%s", out)
	}
}

// The last app on the server takes the file of purge tokens with it, as the scripts do it on a
// server: the file and its folder go, whatever front the app has. Another app keeps it.
func TestDeployRemoveThePurgeFile(t *testing.T) {
	root, home, _ := envApp(t, "200")
	prepareFakeServer(t, home)
	writePurgeFile(t, home, purgeFileHeader+"\nexample.com\tzone-com\ttok-com\n")
	writeFile(t, filepath.Join(home, "notes", "port"), "9532\n")
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err != nil || strings.Contains(out, "cloudflare-purge") || !exists(filepath.Join(home, filepath.FromSlash(purgeFile))) {
		t.Fatalf("another app: %v\n%s", err, out)
	}
	if err := os.RemoveAll(filepath.Join(home, "notes")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, "shop", "port"), "9531\n")
	out, _, err = runRemove(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "done    server: delete "+home+"/.config/tifa/cloudflare-purge, the tokens that clear the CDN cache, which no other app uses") {
		t.Fatalf("last app: %v\n%s", err, out)
	}
	if exists(filepath.Join(home, filepath.FromSlash(purgeFile))) || exists(filepath.Join(home, ".config", "tifa")) {
		t.Error("the file or its folder is still there")
	}
	if strings.Contains(fakeServerArgv(), "tok-com") || strings.Contains(out, "tok-com") {
		t.Error("the token showed")
	}
}

// Without CI, remove still takes the Access application of the ssh host name with its people
// policy, route and record. They go after the steps on the server, which may reach it through
// that very host name.
func TestDeployRemoveSSHWithoutCI(t *testing.T) {
	root := setupApp(t, sshCloudflareNoCI, nil)
	srv := newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts))
	cf := cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	if out, err := runSetup(t, root, false, "", "--yes"); err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	if len(cf.apps) != 1 || len(cf.policies) != 1 {
		t.Fatalf("setup made apps %v policies %v", cf.apps, cf.policies)
	}
	host := cf.apps[0]["domain"].(string)
	// The developer has the block of setup next to lines of their own.
	cfg, _ := sshConfigPath()
	writeFile(t, cfg, "Host mine\n  User me\n")
	if err := writeSSHBlock(cfg, "shop", host, fakeCloudflared); err != nil {
		t.Fatal(err)
	}
	setFacts(t, srv, removeFacts(""))
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if data, _ := os.ReadFile(cfg); string(data) != "Host mine\n  User me\n" || !strings.Contains(out, "remove  this machine: the block of tifa shop in "+cfg) {
		t.Errorf("the block of the ssh config is not gone:\n%s\n%s", data, out)
	}
	access := strings.Index(out, "remove  cloudflare: the Access application of the ssh host name of deploy-a.example.com")
	server := strings.Index(out, "remove  server: stop and disable tifa-shop")
	if access < 0 || server < 0 || access < server {
		t.Errorf("the Access application must go after the app (%d, %d):\n%s", access, server, out)
	}
	ingress, _ := json.Marshal(cf.configs["tunnel-1"])
	if len(cf.apps) != 0 || len(cf.policies) != 0 || strings.Contains(string(ingress), host) {
		t.Errorf("left: apps %v policies %v config %s", cf.apps, cf.policies, ingress)
	}
	for _, r := range cf.records["example.com"] {
		t.Errorf("a record is left: %v", r)
	}
}

// When the developer reaches the server through the tunnel, the step that stops the tunnel would cut
// its own connection: it runs detached, a few seconds after ssh left, and remove waits before it
// asks Cloudflare to delete the tunnel.
func TestDeployRemoveThroughTheTunnel(t *testing.T) {
	root := setupApp(t, sshCloudflareNoCI, nil)
	srv := newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts))
	cf := cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	if out, err := runSetup(t, root, false, "", "--yes"); err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	t.Setenv(deployHostEnv, cf.apps[0]["domain"].(string))
	old := untunnelWait
	untunnelWait = 0
	t.Cleanup(func() { untunnelWait = old })
	setFacts(t, srv, removeFacts(""))
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(srv.argv(), "TIFA_DETACHED_EOF") || len(cf.tunnels) != 0 {
		t.Errorf("the tunnel was stopped on the connection that rides it:\n%s\ntunnels %v", srv.argv(), cf.tunnels)
	}
}
