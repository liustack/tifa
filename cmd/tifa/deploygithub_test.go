package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		// The real cf asks "Continue?" before a delete and fails without a terminal, and Cloudflare
		// refuses to delete a token a policy names.
		if !slices.Contains(args, "--force") {
			return nil, errors.New("? This permanently deletes the resource. Continue?")
		}
		for _, p := range a.policies {
			for _, inc := range p["include"].([]any) {
				if inc.(map[string]any)["service_token"].(map[string]any)["token_id"] == args[4] {
					return nil, errors.New("[12139] access.api.error.service_token_in_use")
				}
			}
		}
		before := len(a.tokens)
		a.tokens = slices.DeleteFunc(a.tokens, func(t map[string]any) bool { return t["id"] == args[4] })
		if len(a.tokens) == before {
			return nil, errors.New("404 Not Found")
		}
		return map[string]any{"id": args[4]}, nil
	case strings.HasPrefix(cmd, "zero-trust access service-tokens rotate"):
		for _, t := range a.tokens {
			if t["id"] == args[4] {
				secret := "client-secret-" + a.id("rot")
				a.secrets = append(a.secrets, secret)
				return map[string]any{"id": t["id"], "name": t["name"], "client_id": "client-id-" + args[4] + ".access", "client_secret": secret}, nil
			}
		}
		return nil, errors.New("404 Not Found")
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
			if flag(args, "--domain") == "" || app["domain"] == flag(args, "--domain") {
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

// ingressOf returns the ingress rules the fake tunnel was given.
func ingressOf(cf *fakeAccess, id string) []map[string]any {
	var out []map[string]any
	for _, r := range cf.configs[id]["ingress"].([]any) {
		out = append(out, r.(map[string]any))
	}
	return out
}

// After the CI workflow, the deploy follows it: only a passed push, on the commit that was
// checked, from the default branch. Without a CI workflow a push deploys.
func TestWorkflowAfterCI(t *testing.T) {
	text := githubWorkflow(githubWorkflowSpec{Branch: "main", Mode: connDirect, Port: 22, CIName: "CI", ZoneID: "zone-9"})
	doc, steps := parseWorkflow(t, text)
	if !strings.HasPrefix(text, workflowMarker) {
		t.Error("no marker line")
	}
	on := doc["on"].(map[string]any)
	if _, ok := on["push"]; ok {
		t.Error("deploys on a push as well")
	}
	run := on["workflow_run"].(map[string]any)
	if !slices.Equal(anyStrings(run["workflows"]), []string{"CI"}) || !slices.Equal(anyStrings(run["types"]), []string{"completed"}) || !slices.Equal(anyStrings(run["branches"]), []string{"main"}) {
		t.Errorf("workflow_run %v", run)
	}
	if _, ok := on["workflow_dispatch"]; !ok {
		t.Error("no manual run")
	}
	job := doc["jobs"].(map[string]any)["deploy"].(map[string]any)
	cond := job["if"].(string)
	for _, want := range []string{"workflow_run.conclusion == 'success'", "workflow_run.event == 'push'", "workflow_dispatch"} {
		if !strings.Contains(cond, want) {
			t.Errorf("if lacks %q: %s", want, cond)
		}
	}
	with := steps[0]["with"].(map[string]any)
	if with["ref"] != "${{ github.event.workflow_run.head_sha || github.sha }}" {
		t.Errorf("checkout ref %v", with["ref"])
	}
	if doc["permissions"].(map[string]any)["contents"] != "read" {
		t.Error("permissions")
	}
	for _, want := range []string{"CF_CACHE_PURGE_TOKEN: ${{ secrets.CF_CACHE_PURGE_TOKEN }}", "CF_ZONE_ID: zone-9"} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}

	push := githubWorkflow(githubWorkflowSpec{Branch: "main", Mode: connDirect, Port: 22})
	doc, _ = parseWorkflow(t, push)
	on = doc["on"].(map[string]any)
	if _, ok := on["push"]; !ok {
		t.Error("no push trigger without CI")
	}
	if _, ok := on["workflow_run"]; ok || strings.Contains(push, "CF_CACHE_PURGE_TOKEN") {
		t.Errorf("fallback:\n%s", push)
	}
}

// The name of the CI workflow comes from the file of the app.
func TestCIWorkflowName(t *testing.T) {
	root := t.TempDir()
	if got := ciWorkflowName(root); got != "" {
		t.Errorf("no file: %q", got)
	}
	writeFile(t, filepath.Join(root, ciPath), "name: Checks\non: push\n")
	if got := ciWorkflowName(root); got != "Checks" {
		t.Errorf("got %q", got)
	}
}
