package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
	"golang.org/x/crypto/ssh"
)

// parseWorkflow reads the workflow as YAML, which fails the test when it is not.
func parseWorkflow(t *testing.T, text string) (doc map[string]any, steps []map[string]any) {
	t.Helper()
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("not YAML: %v\n%s", err, text)
	}
	job := doc["jobs"].(map[string]any)["deploy"].(map[string]any)
	for _, s := range job["steps"].([]any) {
		steps = append(steps, s.(map[string]any))
	}
	return doc, steps
}

func stepRuns(steps []map[string]any) string {
	var b strings.Builder
	for _, s := range steps {
		if r, ok := s["run"].(string); ok {
			b.WriteString(r + "\n")
		}
	}
	return b.String()
}

func uses(steps []map[string]any) []string {
	var out []string
	for _, s := range steps {
		if u, ok := s["uses"].(string); ok {
			out = append(out, u)
		}
	}
	return out
}

// The workflow of each way to connect is YAML, deploys on a push to the default branch and by
// hand, never cancels a deploy that runs, and gives the secrets to the steps that need them.
func TestGitHubWorkflow(t *testing.T) {
	direct := githubWorkflow(githubWorkflowSpec{Branch: "main", Mode: connDirect, Port: 22})
	doc, steps := parseWorkflow(t, direct)
	on := doc["on"].(map[string]any)
	if got := on["push"].(map[string]any)["branches"]; !slices.Equal(anyStrings(got), []string{"main"}) {
		t.Errorf("push branches %v", got)
	}
	action := on["workflow_dispatch"].(map[string]any)["inputs"].(map[string]any)["action"].(map[string]any)
	if action["type"] != "choice" || !slices.Equal(anyStrings(action["options"]), []string{"deploy", "rollback"}) {
		t.Errorf("action input %v", action)
	}
	if c := doc["concurrency"].(map[string]any); c["cancel-in-progress"] != false || c["group"] == "" {
		t.Errorf("concurrency %v", c)
	}
	if got := uses(steps); !slices.Equal(got, []string{"actions/checkout@v7", "actions/setup-go@v7"}) {
		t.Errorf("an app without pages uses %v", got)
	}
	runs := stepRuns(steps)
	for _, want := range []string{
		`go tool tifa deploy --host "$DEPLOY_HOST"`,
		`go tool tifa deploy rollback --host "$DEPLOY_HOST"`,
		"chmod 600 ~/.ssh/deploy_key",
		"StrictHostKeyChecking yes",
	} {
		if !strings.Contains(runs, want) {
			t.Errorf("lacks %q:\n%s", want, direct)
		}
	}
	for _, not := range []string{"Port ", "cloudflared", "ssh-keyscan", "pnpm"} {
		if strings.Contains(direct, not) {
			t.Errorf("a direct workflow of an app without pages holds %q:\n%s", not, direct)
		}
	}

	// A secret reaches a step through env, never inside the script.
	for _, s := range steps {
		if r, ok := s["run"].(string); ok && strings.Contains(r, "secrets.") {
			t.Errorf("a script holds a secret: %s", r)
		}
	}

	// Pages: pnpm and Node, with the version of pnpm only when package.json has none.
	pages := githubWorkflow(githubWorkflowSpec{Branch: "release/1", Mode: connDirect, Port: 2222, Node: true, PnpmVersion: "10"})
	_, steps = parseWorkflow(t, pages)
	if got := uses(steps); !slices.Equal(got, []string{"actions/checkout@v7", "actions/setup-go@v7", "pnpm/action-setup@v6", "actions/setup-node@v7"}) {
		t.Errorf("pages use %v", got)
	}
	if !strings.Contains(stepRuns(steps), "pnpm install --frozen-lockfile") || !strings.Contains(pages, "  Port 2222") || !strings.Contains(pages, "version: 10") {
		t.Errorf("pages:\n%s", pages)
	}
	if strings.Contains(githubWorkflow(githubWorkflowSpec{Branch: "main", Mode: connDirect, Port: 22, Node: true}), "          version:") {
		t.Error("pnpm version written though package.json names pnpm")
	}

	// Cloudflare: cloudflared from the signed repository, the token through the variables
	// cloudflared reads, and nothing about the token on a command line.
	cf := githubWorkflow(githubWorkflowSpec{Branch: "main", Mode: connCloudflare, Port: 22})
	_, steps = parseWorkflow(t, cf)
	for _, want := range []string{cloudflareKey, cloudflareRepo, "ProxyCommand cloudflared access ssh --hostname %h",
		"TUNNEL_SERVICE_TOKEN_ID: ${{ secrets.CF_ACCESS_CLIENT_ID }}", "TUNNEL_SERVICE_TOKEN_SECRET: ${{ secrets.CF_ACCESS_CLIENT_SECRET }}"} {
		if !strings.Contains(cf, want) {
			t.Errorf("cloudflare workflow lacks %q:\n%s", want, cf)
		}
	}
	if strings.Contains(cf, "--service-token") || strings.Contains(cf, "  Port ") {
		t.Errorf("cloudflare workflow:\n%s", cf)
	}

	// Tailscale.
	ts := githubWorkflow(githubWorkflowSpec{Branch: "main", Mode: connTailscale, Port: 22})
	_, steps = parseWorkflow(t, ts)
	if got := uses(steps); !slices.Contains(got, "tailscale/github-action@v4") {
		t.Errorf("tailscale uses %v", got)
	}
	for _, want := range []string{"oauth-client-id: ${{ secrets.TS_OAUTH_CLIENT_ID }}", "oauth-secret: ${{ secrets.TS_OAUTH_SECRET }}", "tags: tag:ci"} {
		if !strings.Contains(ts, want) {
			t.Errorf("tailscale workflow lacks %q", want)
		}
	}
}

func anyStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestCloudflareSSHHost(t *testing.T) {
	for _, c := range []struct{ domain, zone, want string }{
		{"deploy-a.tifago.com", "tifago.com", "deploy-a-ssh.tifago.com"},
		{"tifago.com", "tifago.com", "ssh.tifago.com"},
		{"example.co.uk", "example.co.uk", "ssh.example.co.uk"},
		{"shop.example.co.uk", "example.co.uk", "shop-ssh.example.co.uk"},
		{"a.b.example.com", "example.com", "a-ssh.b.example.com"},
	} {
		if got := cloudflareSSHHost(c.domain, c.zone); got != c.want {
			t.Errorf("%s in %s: got %s, want %s", c.domain, c.zone, got, c.want)
		}
	}
}

func TestKnownHostsLines(t *testing.T) {
	keys := []string{"ssh-ed25519 AAAA1", "ssh-rsa AAAA2"}
	if got := knownHostsLines("203.0.113.7", 22, keys); !slices.Equal(got, []string{"203.0.113.7 ssh-ed25519 AAAA1", "203.0.113.7 ssh-rsa AAAA2"}) {
		t.Errorf("port 22: %v", got)
	}
	if got := knownHostsLines("203.0.113.7", 2222, keys); got[0] != "[203.0.113.7]:2222 ssh-ed25519 AAAA1" {
		t.Errorf("port 2222: %v", got)
	}
	if got := knownHostsLines("deploy-a-ssh.tifago.com", 22, keys[:1]); got[0] != "deploy-a-ssh.tifago.com ssh-ed25519 AAAA1" {
		t.Errorf("cloudflare: %v", got)
	}
}

func TestParseServerFacts(t *testing.T) {
	f, err := parseServerFacts("tunnel abc-123\nhostkey ssh-ed25519 AAAAC3Nz\nhostkey ssh-rsa AAAAB3Nz\n")
	if err != nil || f.TunnelID != "abc-123" || !slices.Equal(f.HostKeys, []string{"ssh-ed25519 AAAAC3Nz", "ssh-rsa AAAAB3Nz"}) {
		t.Errorf("got %+v, %v", f, err)
	}
	if _, err := parseServerFacts("tunnel abc\n"); err == nil {
		t.Error("no host keys, no error")
	}
	if _, err := parseServerFacts("hostkey ssh-ed25519 not*base64\n"); err == nil {
		t.Error("a bad key, no error")
	}
}

func TestParseSSHConfig(t *testing.T) {
	r, err := parseSSHConfig("vps", "user root\nhostname 203.0.113.7\nport 2222\nidentityfile ~/.ssh/id\n")
	if err != nil || r.Host != "203.0.113.7" || r.Port != 2222 {
		t.Errorf("got %+v, %v", r, err)
	}
	if _, err := parseSSHConfig("vps", "hostname 203.0.113.7\nproxyjump bastion\n"); err == nil || !strings.Contains(err.Error(), "proxyjump") {
		t.Errorf("proxyjump: %v", err)
	}
	if _, err := parseSSHConfig("vps", "hostname 203.0.113.7\nport 22\nproxyjump none\n"); err != nil {
		t.Errorf("proxyjump none: %v", err)
	}
	if _, err := parseSSHConfig("vps", "hostname a@b\n"); err == nil {
		t.Error("an address deploy refuses, no error")
	}
	if _, err := parseSSHConfig("vps", "port 22\n"); err == nil {
		t.Error("no host name, no error")
	}
}

// The script puts the new line in authorized_keys, replaces the line of the same repository and
// leaves every other line, also a repository whose name ends the same way and a file without a
// final newline.
func TestAuthorizeScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the server scripts run under sh")
	}
	home := t.TempDir()
	keys := filepath.Join(home, ".ssh", "authorized_keys")
	run := func(repo, line string) {
		t.Helper()
		c := exec.Command("sh", "-c", authorizeScript(repo))
		c.Env = append(os.Environ(), "HOME="+home)
		c.Stdin = strings.NewReader(line + "\n")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	read := func() []string {
		data, err := os.ReadFile(keys)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}
	if err := os.MkdirAll(filepath.Dir(keys), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, keys, "ssh-ed25519 AAAAroot root@laptop\nssh-ed25519 AAAAother tifa-github acme/shop-extra\nssh-ed25519 AAAAx tifa-github other-acme/shop") // no final newline

	run("acme/shop", "restrict ssh-ed25519 AAAAone tifa-github acme/shop")
	want := []string{
		"ssh-ed25519 AAAAroot root@laptop",
		"ssh-ed25519 AAAAother tifa-github acme/shop-extra",
		"ssh-ed25519 AAAAx tifa-github other-acme/shop",
		"restrict ssh-ed25519 AAAAone tifa-github acme/shop",
	}
	if got := read(); !slices.Equal(got, want) {
		t.Fatalf("first run:\n%s", strings.Join(got, "\n"))
	}
	run("acme/shop", "restrict ssh-ed25519 AAAAtwo tifa-github acme/shop")
	want[3] = "restrict ssh-ed25519 AAAAtwo tifa-github acme/shop"
	if got := read(); !slices.Equal(got, want) {
		t.Fatalf("rotation:\n%s", strings.Join(got, "\n"))
	}
	if info, err := os.Stat(keys); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, %v", info.Mode(), err)
	}
	// A server where nobody has a key yet.
	fresh := t.TempDir()
	c := exec.Command("sh", "-c", authorizeScript("acme/shop"))
	c.Env = append(os.Environ(), "HOME="+fresh)
	c.Stdin = strings.NewReader("restrict ssh-ed25519 AAAAnew tifa-github acme/shop\n")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if data, _ := os.ReadFile(filepath.Join(fresh, ".ssh", "authorized_keys")); string(data) != "restrict ssh-ed25519 AAAAnew tifa-github acme/shop\n" {
		t.Errorf("fresh: %q", data)
	}
}

// The script that reads the facts of a server prints the tunnel and the host keys without their comments.
func TestFactsBody(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the server scripts run under sh")
	}
	// factsBody reads fixed paths, so check the pieces it relies on.
	for _, want := range []string{"/etc/tifa/tunnel", "/etc/ssh/ssh_host_*_key.pub", "cut -d' ' -f1,2"} {
		if !strings.Contains(factsBody, want) {
			t.Errorf("lacks %s", want)
		}
	}
}

func TestDeployKey(t *testing.T) {
	k, err := newDeployKey("acme/shop")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.ParsePrivateKey([]byte(k.Private))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))); got != k.Public {
		t.Errorf("public key %q, private key has %q", k.Public, got)
	}
	line := k.authorizedLine("acme/shop")
	if !strings.HasPrefix(line, "restrict ssh-ed25519 ") || !strings.HasSuffix(line, " tifa-github acme/shop") || strings.Contains(line, "\n") {
		t.Errorf("line %q", line)
	}
	if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line)); err != nil {
		t.Errorf("sshd cannot read the line: %v", err)
	}
	k2, _ := newDeployKey("acme/shop")
	if k2.Private == k.Private {
		t.Error("two runs made the same key")
	}
}

// fakeGH stands in for gh: it knows one repository and keeps the secrets it is given.
type fakeGH struct {
	loggedIn bool
	repo     string
	branch   string // empty: no default branch yet
	calls    []string
	secrets  map[string]string
}

func newFakeGH() *fakeGH {
	return &fakeGH{loggedIn: true, repo: "acme/shop", branch: "main", secrets: map[string]string{}}
}

func (g *fakeGH) install(t *testing.T) {
	oldRun, oldHave := runGH, haveGH
	t.Cleanup(func() { runGH, haveGH = oldRun, oldHave })
	haveGH = func() bool { return true }
	runGH = func(_ context.Context, _ string, stdin io.Reader, args ...string) ([]byte, error) {
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
		case args[0] == "secret" && args[1] == "set":
			if stdin == nil || len(args) != 5 || args[3] != "--repo" || args[4] != g.repo {
				return nil, fmt.Errorf("fake gh: bad secret set %v", args)
			}
			data, _ := io.ReadAll(stdin)
			g.secrets[args[2]] = string(data)
			return nil, nil
		}
		return nil, fmt.Errorf("fake gh does not know %v", args)
	}
}

// fakeSSH makes a program that stands in for ssh: ssh -G prints cfg, the facts script gets facts,
// and every other script has its standard input appended to stdin. calls logs the arguments.
func fakeSSH(t *testing.T, cfg, facts string) (calls, stdin func() string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake ssh is a shell script")
	}
	dir := t.TempDir()
	logf, inf := filepath.Join(dir, "calls"), filepath.Join(dir, "stdin")
	writeFile(t, filepath.Join(dir, "cfg"), cfg)
	writeFile(t, filepath.Join(dir, "facts"), facts)
	prog := filepath.Join(dir, "ssh")
	writeFile(t, prog, `#!/bin/sh
echo "ssh $*" >> `+shellQuote(logf)+`
if [ "$1" = -G ]; then cat `+shellQuote(filepath.Join(dir, "cfg"))+`; exit 0; fi
while [ "$1" != "--" ]; do shift; done
case "$3" in
*ssh_host_*) cat `+shellQuote(filepath.Join(dir, "facts"))+` ;;
*) cat >> `+shellQuote(inf)+` ;;
esac
`)
	if err := os.Chmod(prog, 0o755); err != nil {
		t.Fatal(err)
	}
	old := sshProgram
	sshProgram = prog
	t.Cleanup(func() { sshProgram = old })
	read := func(p string) func() string {
		return func() string { data, _ := os.ReadFile(p); return string(data) }
	}
	return read(logf), read(inf)
}

func githubApp(t *testing.T, tifaYAML string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
	writeFile(t, filepath.Join(root, "tifa.yaml"), tifaYAML)
	for name, content := range files {
		writeFile(t, filepath.Join(root, name), content)
	}
	t.Setenv(deployHostEnv, "")
	return root
}

const hostFacts = "hostkey ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA\nhostkey ssh-rsa AAAAB3NzaC1yc2E=\n"

// Direct: the key reaches the server and the secrets, the workflow is written, and the secret
// values appear in no argument and no output. Running it again rotates the key and keeps the rest.
func TestGitHubDirect(t *testing.T) {
	root := githubApp(t, "deploy:\n  host: vps\n", map[string]string{
		"package.json": `{"name":"shop"}`, "pnpm-lock.yaml": "lockfileVersion: '9.0'\n"})
	gh := newFakeGH()
	gh.install(t)
	calls, stdin := fakeSSH(t, "user root\nhostname 203.0.113.7\nport 2222\n", hostFacts)
	target, err := loadDeployTarget(root, "")
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if err := deployGitHub(context.Background(), target, githubOptions{mode: connDirect}, &out, &errOut); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if gh.secrets[secretHost] != "203.0.113.7" {
		t.Errorf("host %q", gh.secrets[secretHost])
	}
	wantHosts := "[203.0.113.7]:2222 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA\n[203.0.113.7]:2222 ssh-rsa AAAAB3NzaC1yc2E="
	if gh.secrets[secretKnownHosts] != wantHosts {
		t.Errorf("known hosts %q", gh.secrets[secretKnownHosts])
	}
	if _, ok := gh.secrets[secretCFID]; ok || len(gh.secrets) != 3 {
		t.Errorf("secrets %v", gh.secrets)
	}
	// The public key the server was given is the one of the private key in the secret.
	signer, err := ssh.ParsePrivateKey([]byte(gh.secrets[secretSSHKey]))
	if err != nil {
		t.Fatal(err)
	}
	if want := "restrict " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " tifa-github acme/shop\n"; stdin() != want {
		t.Errorf("the server got %q, want %q", stdin(), want)
	}
	if !strings.Contains(calls(), "-l tifa -- vps") || !strings.Contains(calls(), "ssh -G -- vps") {
		t.Errorf("ssh calls:\n%s", calls())
	}
	// gh got the key on stdin: no secret value in an argument, in the output or in a log.
	for name, value := range gh.secrets {
		for _, seen := range []string{strings.Join(gh.calls, "\n"), out.String(), errOut.String(), calls()} {
			if strings.Contains(seen, value) {
				t.Errorf("the value of %s shows up: %.60q", name, seen)
			}
		}
	}
	for _, c := range gh.calls[2:] {
		if !strings.HasPrefix(c, "secret set DEPLOY_") || !strings.HasSuffix(c, " --repo acme/shop") {
			t.Errorf("gh call %q", c)
		}
	}
	wf, err := os.ReadFile(filepath.Join(root, githubWorkflowPath))
	if err != nil {
		t.Fatal(err)
	}
	if want := githubWorkflow(githubWorkflowSpec{Branch: "main", Mode: connDirect, Port: 2222, Node: true, PnpmVersion: "10"}); string(wf) != want {
		t.Errorf("workflow:\n%s", wf)
	}
	for _, f := range []string{".ssh", "deploy_key", "id_ed25519"} {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			t.Errorf("the key touched the disk: %s", f)
		}
	}

	// Again: a new key, the same workflow.
	firstKey := gh.secrets[secretSSHKey]
	out.Reset()
	if err := deployGitHub(context.Background(), target, githubOptions{mode: connDirect}, &out, &errOut); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if gh.secrets[secretSSHKey] == firstKey || !strings.Contains(out.String(), "is up to date") {
		t.Errorf("second run:\n%s", out.String())
	}

	// A workflow the user changed is kept, and nothing else happens.
	writeFile(t, filepath.Join(root, githubWorkflowPath), "name: mine\n")
	before, beforeGH := calls(), len(gh.calls)
	err = deployGitHub(context.Background(), target, githubOptions{mode: connDirect}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("a changed workflow: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, githubWorkflowPath)); string(got) != "name: mine\n" {
		t.Error("the workflow was overwritten")
	}
	if calls() != before+"ssh -G -- vps\n" || len(gh.calls) != beforeGH+2 {
		t.Errorf("side effects before the refusal:\n%s\n%v", calls(), gh.calls[beforeGH:])
	}
	if err := deployGitHub(context.Background(), target, githubOptions{mode: connDirect, force: true}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, githubWorkflowPath)); string(got) != string(wf) {
		t.Error("--force did not write the workflow")
	}
}

// Tailscale sets the ssh secrets and says which two the user sets.
func TestGitHubTailscale(t *testing.T) {
	root := githubApp(t, "deploy:\n  host: vps\n", nil)
	gh := newFakeGH()
	gh.install(t)
	fakeSSH(t, "hostname vps.tail1234.ts.net\nport 22\n", hostFacts)
	target, _ := loadDeployTarget(root, "")
	var out bytes.Buffer
	if err := deployGitHub(context.Background(), target, githubOptions{mode: connTailscale}, &out, io.Discard); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if gh.secrets[secretHost] != "vps.tail1234.ts.net" || len(gh.secrets) != 3 {
		t.Errorf("secrets %v", gh.secrets)
	}
	for _, want := range []string{secretTSID, secretTSSecret, "tag:ci"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %s:\n%s", want, out.String())
		}
	}
	wf, _ := os.ReadFile(filepath.Join(root, githubWorkflowPath))
	if !strings.Contains(string(wf), "tailscale/github-action@v4") {
		t.Errorf("workflow:\n%s", wf)
	}
}

// Without gh, not logged in, outside a GitHub repository or in an empty one, nothing changes on
// the server or in the app, and the steps to do by hand are printed.
func TestGitHubCannotUseGH(t *testing.T) {
	root := githubApp(t, "deploy:\n  host: vps\n", nil)
	calls, _ := fakeSSH(t, "hostname 203.0.113.7\n", hostFacts)
	target, _ := loadDeployTarget(root, "")
	for _, c := range []struct {
		name string
		set  func(*fakeGH)
		want string
	}{
		{"no gh", func(*fakeGH) { haveGH = func() bool { return false } }, "not installed"},
		{"not logged in", func(g *fakeGH) { g.loggedIn = false }, "gh auth login"},
		{"empty repo", func(g *fakeGH) { g.branch = "" }, "no default branch"},
	} {
		t.Run(c.name, func(t *testing.T) {
			gh := newFakeGH()
			gh.install(t)
			c.set(gh)
			var out bytes.Buffer
			err := deployGitHub(context.Background(), target, githubOptions{mode: connDirect}, &out, io.Discard)
			if err == nil || !strings.Contains(out.String(), c.want) || !strings.Contains(out.String(), "ssh-keygen -t ed25519") || !strings.Contains(out.String(), secretKnownHosts) {
				t.Errorf("%v\n%s", err, out.String())
			}
			if len(gh.secrets) != 0 || calls() != "" {
				t.Errorf("changed something: %v %q", gh.secrets, calls())
			}
			if _, err := os.Stat(filepath.Join(root, githubWorkflowPath)); err == nil {
				t.Error("wrote the workflow")
			}
		})
	}
}

// A server that reaches the target through a jump host cannot be copied into CI, and an app
// with a package.json needs its lock file.
func TestGitHubStopsBeforeChanging(t *testing.T) {
	gh := newFakeGH()
	gh.install(t)
	root := githubApp(t, "deploy:\n  host: vps\n", nil)
	calls, _ := fakeSSH(t, "hostname 10.0.0.5\nproxyjump bastion\n", hostFacts)
	target, _ := loadDeployTarget(root, "")
	if err := deployGitHub(context.Background(), target, githubOptions{mode: connDirect}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "proxyjump") {
		t.Errorf("jump host: %v", err)
	}
	root = githubApp(t, "deploy:\n  host: vps\n", map[string]string{"package.json": "{}"})
	fakeSSH(t, "hostname 203.0.113.7\n", hostFacts)
	target, _ = loadDeployTarget(root, "")
	if err := deployGitHub(context.Background(), target, githubOptions{mode: connDirect}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "pnpm-lock.yaml") {
		t.Errorf("no lock file: %v", err)
	}
	if len(gh.secrets) != 0 || strings.Contains(calls(), "-l tifa") {
		t.Errorf("changed something: %v", gh.secrets)
	}
	// --cloudflare needs the domain and cf before anything else.
	if err := deployGitHub(context.Background(), target, githubOptions{mode: connCloudflare}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "deploy.domain") {
		t.Errorf("no domain: %v", err)
	}
}

// fakeAccess adds the Zero Trust Access calls to a fakeCF.
type fakeAccess struct {
	*fakeCF
	tokens   []map[string]any
	policies []map[string]any
	apps     []map[string]any
	secrets  []string // client secrets made
}

func (a *fakeAccess) install(t *testing.T) {
	a.fakeCF.install(t)
	inner := runCF
	runCF = func(ctx context.Context, args ...string) ([]byte, error) {
		if len(args) < 4 || args[0] != "zero-trust" {
			return inner(ctx, args...)
		}
		a.calls = append(a.calls, strings.Join(args, " "))
		v, err := a.answer2(args)
		if err != nil {
			return nil, &cfError{args: args, stderr: err.Error(), err: err}
		}
		return json.Marshal(v)
	}
}

func page(list []map[string]any, args []string) []map[string]any {
	p, _ := strconv.Atoi(flag(args, "--page"))
	n, _ := strconv.Atoi(flag(args, "--per-page"))
	lo := min((p-1)*n, len(list))
	return append([]map[string]any{}, list[lo:min(lo+n, len(list))]...)
}

func (a *fakeAccess) answer2(args []string) (any, error) {
	cmd := strings.Join(args[:min(5, len(args))], " ")
	switch cmd {
	case "zero-trust access service-tokens list --name":
		var out []map[string]any
		for _, t := range a.tokens {
			if strings.Contains(t["name"].(string), flag(args, "--name")) {
				out = append(out, map[string]any{"id": t["id"], "name": t["name"]})
			}
		}
		return page(out, args), nil
	}
	switch {
	case strings.HasPrefix(cmd, "zero-trust access service-tokens delete"):
		before := len(a.tokens)
		a.tokens = slices.DeleteFunc(a.tokens, func(t map[string]any) bool { return t["id"] == args[4] })
		if len(a.tokens) == before {
			return nil, errors.New("404 Not Found")
		}
		return map[string]any{"id": args[4]}, nil
	case strings.HasPrefix(cmd, "zero-trust access service-tokens create"):
		b := body(args)
		if b["duration"] != "forever" {
			return nil, errors.New("the token must not expire")
		}
		n := a.id("tok")
		secret := "client-secret-" + n
		a.secrets = append(a.secrets, secret)
		a.tokens = append(a.tokens, map[string]any{"id": n, "name": b["name"]})
		return map[string]any{"id": n, "name": b["name"], "client_id": "client-id-" + n + ".access", "client_secret": secret}, nil
	case strings.HasPrefix(cmd, "zero-trust access policies list"):
		return page(a.policies, args), nil
	case strings.HasPrefix(cmd, "zero-trust access policies create"):
		b := body(args)
		b["id"] = a.id("pol")
		a.policies = append(a.policies, b)
		return b, nil
	case strings.HasPrefix(cmd, "zero-trust access policies update"):
		for i, p := range a.policies {
			if p["id"] == args[4] {
				b := body(args)
				b["id"] = args[4]
				a.policies[i] = b
				return b, nil
			}
		}
		return nil, errors.New("404 Not Found")
	case strings.HasPrefix(cmd, "zero-trust access applications list"):
		var out []map[string]any
		for _, app := range a.apps {
			if app["domain"] == flag(args, "--domain") {
				out = append(out, app)
			}
		}
		return page(out, args), nil
	case strings.HasPrefix(cmd, "zero-trust access applications create"):
		b := body(args)
		b["id"], b["aud"] = a.id("app"), "aud"
		a.apps = append(a.apps, b)
		return b, nil
	case strings.HasPrefix(cmd, "zero-trust access applications update"):
		for i, app := range a.apps {
			if app["id"] == args[4] {
				b := body(args)
				b["id"] = args[4]
				a.apps[i] = b
				return b, nil
			}
		}
		return nil, errors.New("404 Not Found")
	}
	return nil, fmt.Errorf("the fake cf does not know %q", strings.Join(args, " "))
}

// Cloudflare: the ssh route joins the tunnel next to the route of the app and ahead of the rule for
// the rest, the DNS record is proxied, one Access application admits one token by a policy that
// needs no login, and the secrets reach gh and nothing else. Running it again replaces the token,
// and a second repository adds its own policy to the same application.
func TestGitHubCloudflare(t *testing.T) {
	root := githubApp(t, "deploy:\n  host: vps\n  domain: deploy-a.example.com\n", nil)
	cf := &fakeAccess{fakeCF: newFakeCF("example.com")}
	cf.install(t)
	cf.tunnels["tunnel-7"] = "tifa-vps"
	cf.configs["tunnel-7"] = map[string]any{"ingress": []any{
		map[string]any{"hostname": "deploy-a.example.com", "service": "http://127.0.0.1:9527"},
		map[string]any{"service": "http_status:404"},
	}}
	gh := newFakeGH()
	gh.install(t)
	calls, stdin := fakeSSH(t, "", "tunnel tunnel-7\n"+hostFacts)
	target, _ := loadDeployTarget(root, "")
	run := func() string {
		t.Helper()
		var out, errOut bytes.Buffer
		if err := deployGitHub(context.Background(), target, githubOptions{mode: connCloudflare}, &out, &errOut); err != nil {
			t.Fatalf("%v\n%s", err, out.String())
		}
		for _, secret := range append(cf.secrets, gh.secrets[secretSSHKey]) {
			for _, seen := range []string{out.String(), errOut.String(), strings.Join(cf.calls, "\n"), strings.Join(gh.calls, "\n"), calls(), stdin()} {
				if strings.Contains(seen, secret) {
					t.Errorf("a secret shows up in %.80q", seen)
				}
			}
		}
		return out.String()
	}
	run()

	ingress := ingressOf(cf, "tunnel-7")
	if len(ingress) != 3 || ingress[0]["hostname"] != "deploy-a.example.com" || ingress[1]["hostname"] != "deploy-a-ssh.example.com" ||
		ingress[1]["service"] != "ssh://localhost:22" || ingress[2]["hostname"] != nil {
		t.Errorf("ingress %v", ingress)
	}
	dns := cf.records["example.com"]
	if len(dns) != 1 || dns[0]["name"] != "deploy-a-ssh.example.com" || dns[0]["content"] != "tunnel-7.cfargotunnel.com" || dns[0]["proxied"] != true {
		t.Errorf("dns %v", dns)
	}
	if len(cf.tokens) != 1 || cf.tokens[0]["name"] != "tifa-github-acme-shop" {
		t.Errorf("tokens %v", cf.tokens)
	}
	if len(cf.policies) != 1 || cf.policies[0]["decision"] != "non_identity" {
		t.Fatalf("policies %v", cf.policies)
	}
	include := cf.policies[0]["include"].([]any)
	if len(include) != 1 || include[0].(map[string]any)["service_token"].(map[string]any)["token_id"] != cf.tokens[0]["id"] {
		t.Errorf("policy include %v", include)
	}
	if len(cf.apps) != 1 || cf.apps[0]["domain"] != "deploy-a-ssh.example.com" || cf.apps[0]["type"] != "self_hosted" {
		t.Fatalf("apps %v", cf.apps)
	}
	if got := cf.apps[0]["policies"].([]any)[0].(map[string]any)["id"]; got != cf.policies[0]["id"] {
		t.Errorf("the application has the policy %v, not %v", got, cf.policies[0]["id"])
	}
	if gh.secrets[secretHost] != "deploy-a-ssh.example.com" || gh.secrets[secretKnownHosts] != "deploy-a-ssh.example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA\ndeploy-a-ssh.example.com ssh-rsa AAAAB3NzaC1yc2E=" {
		t.Errorf("secrets %q", gh.secrets)
	}
	if gh.secrets[secretCFID] != "client-id-"+cf.tokens[0]["id"].(string)+".access" || gh.secrets[secretCFSecret] != cf.secrets[0] {
		t.Errorf("token secrets %q", gh.secrets)
	}
	wf, _ := os.ReadFile(filepath.Join(root, githubWorkflowPath))
	if want := githubWorkflow(githubWorkflowSpec{Branch: "main", Mode: connCloudflare, Port: 22}); string(wf) != want {
		t.Errorf("workflow:\n%s", wf)
	}

	// The same repository again: the old token is deleted, one new token, one policy that names it, one application.
	oldID := cf.tokens[0]["id"]
	run()
	if len(cf.tokens) != 1 || cf.tokens[0]["id"] == oldID || len(cf.policies) != 1 || len(cf.apps) != 1 || len(cf.records["example.com"]) != 1 {
		t.Errorf("rerun: tokens %v policies %v apps %v", cf.tokens, cf.policies, cf.apps)
	}
	if got := cf.policies[0]["include"].([]any)[0].(map[string]any)["service_token"].(map[string]any)["token_id"]; got != cf.tokens[0]["id"] {
		t.Errorf("the policy still names %v", got)
	}
	if len(cf.apps[0]["policies"].([]any)) != 1 {
		t.Errorf("application policies %v", cf.apps[0]["policies"])
	}
	if len(ingressOf(cf, "tunnel-7")) != 3 {
		t.Errorf("ingress grew: %v", cf.configs["tunnel-7"]["ingress"])
	}

	// Another repository on the same host name adds its policy and leaves the first.
	gh.repo = "acme/shop-fork"
	run()
	if len(cf.apps) != 1 || len(cf.policies) != 2 || len(cf.tokens) != 2 || len(cf.apps[0]["policies"].([]any)) != 2 {
		t.Errorf("second repo: tokens %v policies %v apps %v", cf.tokens, cf.policies, cf.apps)
	}

	// Nothing in the calls writes anything but what is named above.
	for _, c := range cf.calls {
		if strings.Contains(c, "client-secret") {
			t.Errorf("secret in a cf call: %s", c)
		}
	}
}

// A server without a tunnel is sent to setup, before the key is added.
func TestGitHubCloudflareNeedsTunnel(t *testing.T) {
	root := githubApp(t, "deploy:\n  host: vps\n  domain: deploy-a.example.com\n", nil)
	cf := &fakeAccess{fakeCF: newFakeCF("example.com")}
	cf.install(t)
	gh := newFakeGH()
	gh.install(t)
	_, stdin := fakeSSH(t, "", hostFacts)
	target, _ := loadDeployTarget(root, "")
	err := deployGitHub(context.Background(), target, githubOptions{mode: connCloudflare}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "deploy setup") || stdin() != "" || len(gh.secrets) != 0 {
		t.Errorf("%v, %q, %v", err, stdin(), gh.secrets)
	}
}

// The ingress rule for ssh goes in like any other route.
func TestWithRouteSSH(t *testing.T) {
	in := []map[string]any{
		{"hostname": "a.example.com", "service": "http://127.0.0.1:9527"},
		{"service": "http_status:404"},
	}
	out, changed := withRoute(in, "a-ssh.example.com", sshService)
	if !changed || len(out) != 3 || out[1]["service"] != "ssh://localhost:22" || out[2]["service"] != "http_status:404" {
		t.Errorf("got %v", out)
	}
	if _, changed := withRoute(out, "a-ssh.example.com", sshService); changed {
		t.Error("the same route changed")
	}
}

func TestGitHubCommand(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"deploy", "github", "--cloudflare", "--tailscale"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "none of the others can be") {
		t.Errorf("both modes: %v", err)
	}
}

// ingressOf returns the ingress rules the fake tunnel was given.
func ingressOf(cf *fakeAccess, id string) []map[string]any {
	var out []map[string]any
	for _, r := range cf.configs[id]["ingress"].([]any) {
		out = append(out, r.(map[string]any))
	}
	return out
}
