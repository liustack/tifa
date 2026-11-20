package main

// The GitHub part of tifa deploy setup, for deploy.github: true. It writes the workflow, makes a
// key that only this repository uses and lets it log in as the deploy user, reads the host keys of
// the server over the ssh that is trusted already, and sets the secrets of the repository with gh.
// The private key and the other secrets go from memory to gh on its standard input: never to a
// file, a command line or the output. deploysetup.go plans and applies it.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/liustack/tifa/internal/config"
	"go.yaml.in/yaml/v3"
	"golang.org/x/crypto/ssh"
)

// The ways CI reaches the server.
const (
	connDirect     = "direct"
	connCloudflare = "cloudflare"
	connTailscale  = "tailscale"
)

// Names of the secrets of the repository.
const (
	secretSSHKey     = "DEPLOY_SSH_KEY"
	secretKnownHosts = "DEPLOY_KNOWN_HOSTS"
	secretHost       = "DEPLOY_HOST"
	secretCFID       = "CF_ACCESS_CLIENT_ID"
	secretCFSecret   = "CF_ACCESS_CLIENT_SECRET"
	secretTSID       = "TS_OAUTH_CLIENT_ID"
	secretTSSecret   = "TS_OAUTH_SECRET"
	secretPurge      = envPurgeToken
)

// varMarker is the repository variable setup keeps: it marks the secrets as the ones tifa set, and
// holds a fingerprint of the connection they describe, so a second run can tell they are current.
const varMarker = "TIFA_DEPLOY"

// githubWorkflowPath is where the workflow goes, relative to the root of the app.
const githubWorkflowPath = ".github/workflows/tifa-deploy.yml"

// runGH runs the gh of the user in dir with stdin as its input and returns what it printed. The
// error holds what gh printed on stderr, never its output. Tests replace it.
var runGH = func(ctx context.Context, dir string, stdin io.Reader, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, "gh", args...)
	c.Dir, c.Stdin = dir, stdin
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("gh %s: %s", strings.Join(args[:min(2, len(args))], " "), msg)
	}
	return out, nil
}

// haveGH reports whether gh is on the PATH. Tests replace it.
var haveGH = func() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

var repoName = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// githubRepo is the repository of the app.
type githubRepo struct {
	Name   string // owner/name
	Branch string // the default branch
}

// findGitHubRepo asks gh for the repository the folder belongs to. It stops with the steps to do
// by hand when gh is missing, not logged in, or the folder is not a GitHub repository.
func findGitHubRepo(ctx context.Context, dir string) (githubRepo, error) {
	if !haveGH() {
		return githubRepo{}, errors.New("gh, the GitHub CLI, is not installed (https://cli.github.com/)")
	}
	if _, err := readGH(ctx, dir, nil, "auth", "status"); err != nil {
		return githubRepo{}, fmt.Errorf("gh is not logged in: run gh auth login (%w)", err)
	}
	out, err := readRepoView(ctx, dir)
	if err != nil {
		return githubRepo{}, fmt.Errorf("%s is not the folder of a GitHub repository you can use: %w", dir, err)
	}
	return parseRepoView(out)
}

// readRepoView asks gh for the repository of the folder.
func readRepoView(ctx context.Context, dir string) ([]byte, error) {
	return readGH(ctx, dir, nil, "repo", "view", "--json", "nameWithOwner,defaultBranchRef")
}

// parseRepoView reads what gh repo view printed.
func parseRepoView(out []byte) (githubRepo, error) {
	var v struct {
		NameWithOwner    string `json:"nameWithOwner"`
		DefaultBranchRef *struct {
			Name string `json:"name"`
		} `json:"defaultBranchRef"`
	}
	if err := json.Unmarshal(out, &v); err != nil || !repoName.MatchString(v.NameWithOwner) {
		return githubRepo{}, fmt.Errorf("gh repo view gave no repository name tifa can read")
	}
	if v.DefaultBranchRef == nil || v.DefaultBranchRef.Name == "" {
		return githubRepo{}, fmt.Errorf("%s has no default branch yet: push the app to it first", v.NameWithOwner)
	}
	return githubRepo{Name: v.NameWithOwner, Branch: v.DefaultBranchRef.Name}, nil
}

// sshResolved is what ssh makes of a host: the name it connects to and the port.
type sshResolved struct {
	Host string
	Port int
}

// resolveSSH asks ssh, with ssh -G, where it connects to for host. CI has to reach the same place
// without the ~/.ssh/config of the developer, so a ProxyJump or ProxyCommand stops it.
func resolveSSH(ctx context.Context, host string) (sshResolved, error) {
	var out bytes.Buffer
	c := exec.CommandContext(ctx, sshProgram, "-G", "--", host)
	c.Stdout = &out
	if err := c.Run(); err != nil {
		return sshResolved{}, fmt.Errorf("ssh -G %s: %w", host, err)
	}
	return parseSSHConfig(host, out.String())
}

func parseSSHConfig(host, out string) (sshResolved, error) {
	r := sshResolved{Port: 22}
	for _, line := range strings.Split(out, "\n") {
		word, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch strings.ToLower(word) {
		case "hostname":
			r.Host = value
		case "port":
			p, err := strconv.Atoi(value)
			if err != nil || p < 1 || p > 65535 {
				return r, fmt.Errorf("ssh -G %s gave the port %q", host, value)
			}
			r.Port = p
		case "proxyjump", "proxycommand":
			if value != "" && value != "none" {
				return r, fmt.Errorf("%s reaches the server through a %s in your ssh configuration, which GitHub cannot copy: set deploy.ssh to cloudflare or tailscale, or use a host that ssh reaches directly", host, word)
			}
		}
	}
	if r.Host == "" {
		return r, fmt.Errorf("ssh -G %s gave no host name", host)
	}
	if err := config.CheckDeployHost(r.Host); err != nil {
		return r, fmt.Errorf("the address of %s: %w", host, err)
	}
	return r, nil
}

// githubWorkflowSpec is what the text of the workflow depends on.
type githubWorkflowSpec struct {
	Branch string
	Mode   string
	// Port is the port ssh connects to, written when it is not 22. Not used with Cloudflare.
	Port int
	// Node is set for an app with pages: it installs the packages with pnpm.
	Node bool
	// PnpmVersion is the major version of pnpm when package.json does not say which pnpm to use.
	PnpmVersion string
	// CIName is the name of the workflow that checks the app. The deploy runs after it passed.
	// Empty means the app has none, and a push to the default branch deploys.
	CIName string
	// ZoneID is the Cloudflare zone of deploy.domain, whose host name the deploy clears from the
	// CDN cache. Empty means no cache to clear.
	ZoneID string
}

// workflowMarker is the first line of the workflow tifa writes. A file without it is the
// developer's own, and setup leaves it alone.
const workflowMarker = "# Written by go tool tifa deploy setup."

var simpleBranch = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// githubWorkflow is the text of .github/workflows/tifa-deploy.yml.
func githubWorkflow(s githubWorkflowSpec) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	branch := s.Branch
	if !simpleBranch.MatchString(branch) {
		branch = strconv.Quote(branch)
	}
	w("%s Delete this line before you edit the file: setup then leaves it alone.", workflowMarker)
	w("name: tifa-deploy")
	w("")
	w("on:")
	if s.CIName != "" {
		// A push is deployed once the workflow that checks it has passed. GitHub starts this only
		// from the file on the default branch, and runs it on the last commit of that branch, so
		// the steps below check out the commit that was checked.
		w("  workflow_run:")
		w("    workflows: [%s]", strconv.Quote(s.CIName))
		w("    types: [completed]")
		w("    branches: [%s]", branch)
	} else {
		w("  push:")
		w("    branches: [%s]", branch)
	}
	w("  workflow_dispatch:")
	w("    inputs:")
	w("      action:")
	w("        description: What to do on the server")
	w("        type: choice")
	w("        options: [deploy, rollback]")
	w("        default: deploy")
	w("")
	w("# One deploy at a time. A deploy that runs is never cancelled.")
	w("concurrency:")
	w("  group: deploy")
	w("  cancel-in-progress: false")
	w("")
	w("permissions:")
	w("  contents: read")
	w("")
	w("jobs:")
	w("  deploy:")
	if s.CIName != "" {
		// Only a push that passed: a run from a fork has the name of a branch too.
		w("    if: github.event_name == 'workflow_dispatch' || (github.event.workflow_run.conclusion == 'success' && github.event.workflow_run.event == 'push')")
	}
	w("    runs-on: ubuntu-24.04")
	w("    timeout-minutes: 30")
	w("    steps:")
	w("      - uses: actions/checkout@v7")
	if s.CIName != "" {
		w("        with:")
		w("          ref: ${{ github.event.workflow_run.head_sha || github.sha }}")
	}
	w("      - uses: actions/setup-go@v7")
	w("        with:")
	w("          go-version-file: go.mod")
	if s.Node {
		w("      - uses: pnpm/action-setup@v6")
		if s.PnpmVersion != "" {
			w("        with:")
			w("          version: %s", s.PnpmVersion)
		}
		w("      - uses: actions/setup-node@v7")
		w("        with:")
		w("          node-version: 24")
		w("          cache: pnpm")
		w("      - run: pnpm install --frozen-lockfile")
	}
	switch s.Mode {
	case connCloudflare:
		w("      - name: Install cloudflared")
		w("        run: |")
		w("          sudo install -d -m 755 /usr/share/keyrings")
		w("          curl -fsSL %s | sudo tee /usr/share/keyrings/cloudflare-main.gpg >/dev/null", cloudflareKey)
		w("          echo '%s' | sudo tee /etc/apt/sources.list.d/cloudflared.list >/dev/null", cloudflareRepo)
		w("          sudo apt-get update -q")
		w("          sudo apt-get install -y -q cloudflared")
	case connTailscale:
		w("      - name: Connect to Tailscale")
		w("        uses: tailscale/github-action@v4")
		w("        with:")
		w("          oauth-client-id: ${{ secrets.%s }}", secretTSID)
		w("          oauth-secret: ${{ secrets.%s }}", secretTSSecret)
		w("          tags: tag:ci")
	}
	w("      - name: Set up ssh")
	w("        env:")
	w("          DEPLOY_SSH_KEY: ${{ secrets.%s }}", secretSSHKey)
	w("          DEPLOY_KNOWN_HOSTS: ${{ secrets.%s }}", secretKnownHosts)
	w("        run: |")
	w("          install -d -m 700 ~/.ssh")
	w("          printf '%%s\\n' \"$DEPLOY_SSH_KEY\" > ~/.ssh/deploy_key")
	w("          chmod 600 ~/.ssh/deploy_key")
	w("          printf '%%s\\n' \"$DEPLOY_KNOWN_HOSTS\" > ~/.ssh/known_hosts")
	w("          cat > ~/.ssh/config <<'EOF'")
	w("          Host *")
	w("            IdentityFile ~/.ssh/deploy_key")
	w("            IdentitiesOnly yes")
	w("            StrictHostKeyChecking yes")
	switch {
	case s.Mode == connCloudflare:
		w("            ProxyCommand cloudflared access ssh --hostname %%h")
	case s.Port != 22:
		w("            Port %d", s.Port)
	}
	w("          EOF")
	w("      - name: Deploy")
	w("        env:")
	w("          ACTION: ${{ inputs.action }}")
	w("          DEPLOY_HOST: ${{ secrets.%s }}", secretHost)
	if s.ZoneID != "" {
		w("          %s: ${{ secrets.%s }}", envPurgeToken, secretPurge)
		w("          %s: %s", envZoneID, s.ZoneID)
	}
	if s.Mode == connCloudflare {
		// cloudflared reads these two variables for the service token, see its access command.
		w("          TUNNEL_SERVICE_TOKEN_ID: ${{ secrets.%s }}", secretCFID)
		w("          TUNNEL_SERVICE_TOKEN_SECRET: ${{ secrets.%s }}", secretCFSecret)
	}
	w("        run: |")
	w("          if [ \"$ACTION\" = rollback ]; then")
	w("            go tool tifa deploy rollback --host \"$DEPLOY_HOST\"")
	w("          else")
	w("            go tool tifa deploy --host \"$DEPLOY_HOST\"")
	w("          fi")
	return b.String()
}

// authorizeBody runs as the deploy user with the line of the new key on its input. It writes
// authorized_keys again without the older line of the same repository, whose comment is $comment,
// and with the new one. Other lines stay as they are.
const authorizeBody = `set -eu
line=$(cat)
umask 077
d="$HOME/.ssh"
keys="$d/authorized_keys"
mkdir -p "$d"
touch "$keys"
tmp=$(mktemp "$d/authorized_keys.XXXXXX")
trap 'rm -f "$tmp"' EXIT
while IFS= read -r l || [ -n "$l" ]; do
	case "$l" in *" $comment") continue ;; esac
	printf '%s\n' "$l" >> "$tmp"
done < "$keys"
printf '%s\n' "$line" >> "$tmp"
chmod 600 "$tmp"
mv "$tmp" "$keys"
trap - EXIT
`

// githubComment marks the key of a repository in authorized_keys.
func githubComment(repo string) string { return "tifa-github " + repo }

func authorizeScript(repo string) string {
	return script(authorizeBody, "comment", githubComment(repo))
}

// knownHostsLines writes the host keys for the name CI connects to, with the port in brackets
// when it is not 22, as ssh looks them up.
func knownHostsLines(host string, port int, keys []string) []string {
	name := host
	if port != 22 {
		name = "[" + host + "]:" + strconv.Itoa(port)
	}
	var out []string
	for _, k := range keys {
		out = append(out, name+" "+k)
	}
	return out
}

// deployKey is a key made for one repository. The private part exists only in memory.
type deployKey struct {
	Private string // the OpenSSH private key file
	Public  string // the authorized_keys line, without a comment
}

func newDeployKey(repo string) (deployKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return deployKey{}, err
	}
	block, err := ssh.MarshalPrivateKey(priv, githubComment(repo))
	if err != nil {
		return deployKey{}, err
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		return deployKey{}, err
	}
	return deployKey{
		Private: string(pem.EncodeToMemory(block)),
		Public:  strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))),
	}, nil
}

// authorizedLine is the line of authorized_keys for the key: it may run commands but has no
// terminal, no forwarding and no agent.
func (k deployKey) authorizedLine(repo string) string {
	return "restrict " + k.Public + " " + githubComment(repo)
}

// workflowSpec reads what the workflow needs from the app.
func workflowSpec(t deployTarget, repo githubRepo, mode string, r sshResolved, zoneID string) (githubWorkflowSpec, error) {
	s := githubWorkflowSpec{Branch: repo.Branch, Mode: mode, Port: r.Port, ZoneID: zoneID, CIName: ciWorkflowName(t.root)}
	pkg, err := os.ReadFile(filepath.Join(t.root, "package.json"))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return s, err
	}
	if _, err := os.Stat(filepath.Join(t.root, "pnpm-lock.yaml")); err != nil {
		return s, errors.New("the app has a package.json and no pnpm-lock.yaml: run pnpm install and commit the lock file, which CI installs from")
	}
	var p struct {
		PackageManager string `json:"packageManager"`
	}
	if err := json.Unmarshal(pkg, &p); err != nil {
		return s, fmt.Errorf("package.json: %w", err)
	}
	s.Node = true
	if !strings.HasPrefix(p.PackageManager, "pnpm@") {
		// pnpm/action-setup reads packageManager, and needs a version without it.
		s.PnpmVersion = "10"
	}
	return s, nil
}

// secret is a repository secret: never printed.
type secret struct{ name, value string }

// ciWorkflowName is the name of the workflow tifa new writes to check the app, read from the file,
// or empty when the app has none, or one without a name.
func ciWorkflowName(root string) string {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ciPath)))
	if err != nil {
		return ""
	}
	var doc struct {
		Name string `yaml:"name"`
	}
	if yaml.Unmarshal(data, &doc) != nil {
		return ""
	}
	return doc.Name
}

// revokeBody runs as the deploy user and writes authorized_keys again without the line of the
// repository whose comment is $comment. Other lines stay as they are.
const revokeBody = `set -eu
umask 077
d="$HOME/.ssh"
keys="$d/authorized_keys"
if [ ! -f "$keys" ]; then exit 0; fi
tmp=$(mktemp "$d/authorized_keys.XXXXXX")
trap 'rm -f "$tmp"' EXIT
while IFS= read -r l || [ -n "$l" ]; do
	case "$l" in *" $comment") continue ;; esac
	printf '%s\n' "$l" >> "$tmp"
done < "$keys"
chmod 600 "$tmp"
mv "$tmp" "$keys"
trap - EXIT
`

func revokeScript(repo string) string {
	return script(revokeBody, "comment", githubComment(repo))
}
