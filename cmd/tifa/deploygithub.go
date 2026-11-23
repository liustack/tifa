package main

// The GitHub part of tifa deploy setup, for deploy.auto. It writes the workflow, makes a
// key that only this repository uses and lets it log in as the deploy user, reads the host keys of
// the server over the ssh that is trusted already, and sets the secrets of the repository through
// githubAPI (gh, or the REST API with a token). The private key and the other secrets go from
// memory to GitHub: never to a file, a command line or the output. deploysetup.go plans and applies it.

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
	secretSSHKey     = "TIFA_DEPLOY_SSH_KEY"
	secretKnownHosts = "TIFA_DEPLOY_KNOWN_HOSTS"
	secretHost       = deployHostEnv
	secretZoneID     = envZoneID
	secretCFID       = "TIFA_DEPLOY_CF_ACCESS_ID"
	secretCFSecret   = "TIFA_DEPLOY_CF_ACCESS_SECRET"
	secretTSID       = "TIFA_DEPLOY_TAILSCALE_CLIENT_ID"
	secretTSSecret   = "TIFA_DEPLOY_TAILSCALE_SECRET"
	secretPurge      = envPurgeToken
)

// Names of the variables of the repository that hold the Tailscale identity of CI. Neither value
// is secret. The OAuth secrets above are what an older setup kept.
const (
	varTSClient   = "TIFA_DEPLOY_TAILSCALE_ID"
	varTSAudience = "TIFA_DEPLOY_TAILSCALE_AUDIENCE"
)

// The cloudflared that CI runs for deploy.connect cloudflare: one fixed version of the amd64 package
// of ubuntu-24.04, and its SHA-256. A newer cloudflared once stopped reading service tokens for
// access ssh (issue 1673 of cloudflare/cloudflared, 2026.6.0), which would break every deploy
// through the tunnel, so the workflow never asks for the latest. Raise the version only after a
// deploy through the tunnel passed with it, and take the checksum of the same file from the
// release page of that version, as digest of cloudflared-linux-amd64.deb.
const (
	cloudflaredVersion   = "2026.11.1"
	cloudflaredDebSHA256 = "744ce90be92adb361103d51c85f78646314c47e4a68bbaadfe12ab5acc16c9b6"
	cloudflaredDebURL    = "https://github.com/cloudflare/cloudflared/releases/download/" + cloudflaredVersion + "/cloudflared-linux-amd64.deb"
)

// varMarker is the repository variable setup keeps: it marks the secrets as the ones tifa set, and
// holds a fingerprint of the connection they describe, so a second run can tell they are current.
const varMarker = "TIFA_DEPLOY_FINGERPRINT"

// githubWorkflowPath is where the workflow goes, relative to the root of the app.
const githubWorkflowPath = ".github/workflows/tifa-deploy.yml"

// runGH runs the gh of the user in dir with stdin as its input and returns what it printed. The
// error holds what gh printed on stderr, never its output. Tests replace it.
var runGH = func(ctx context.Context, dir string, stdin io.Reader, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, ghProgram, args...)
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

// haveGH reports whether gh is there: on the PATH, or where setup just installed it. Tests replace it.
var haveGH = func() bool {
	_, err := lookProgram(ghProgram)
	return err == nil
}

var repoName = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// githubRepo is the repository of the app.
type githubRepo struct {
	Name   string // owner/name
	Branch string // the default branch
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
				return r, fmt.Errorf("%s reaches the server through a %s in your ssh configuration, which GitHub cannot copy: set deploy.connect to cloudflare or tailscale, or use a host that ssh reaches directly", host, word)
			}
		}
	}
	if r.Host == "" {
		return r, fmt.Errorf("ssh -G %s gave no host name", host)
	}
	if err := checkResolvedHost(r.Host); err != nil {
		return r, fmt.Errorf("the address of %s: %w", host, err)
	}
	return r, nil
}

// githubWorkflowSpec is what the text of the workflow depends on.
type githubWorkflowSpec struct {
	Branch string
	Mode   string
	// Node is set for an app with pages: it installs the packages with pnpm.
	Node bool
	// PnpmVersion is the major version of pnpm when package.json does not say which pnpm to use.
	PnpmVersion string
	// CIName is the name of the workflow that checks the app (deploy.auto.needs). The deploy runs
	// after it passed. Empty means the deploy waits for none, and a push to Branch deploys.
	CIName string
	// Purge says the deploy clears the CDN cache of the domain: the workflow hands Tifa the token
	// and the zone ID, both from secrets, since a public repository must not show the zone.
	Purge bool
	// TailscaleOAuth keeps the OAuth client of an older setup for the Tailscale step, until the
	// identity of workload identity federation is set.
	TailscaleOAuth bool
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
		// from the file on the default branch, and runs it on the last commit of that branch,
		// whatever branch Branch is, so the steps below check out the commit that was checked.
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
	if s.Mode == connTailscale && !s.TailscaleOAuth {
		// The Tailscale action asks GitHub for a token that says which repository this is.
		w("  id-token: write")
	}
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
		// tifa deploy builds, which writes .tifa/, so the prepare script is skipped: its output,
		// which pnpm prefixes, would hide the findings from GitHub.
		w("      - run: pnpm install --frozen-lockfile --ignore-scripts")
	}
	switch s.Mode {
	case connCloudflare:
		// A fixed version from the release page of cloudflared, and not Cloudflare's apt
		// repository: apt lists only the newest, so it cannot be pinned there.
		w("      - name: Install cloudflared")
		w("        run: |")
		w("          curl -fsSL -o \"$RUNNER_TEMP/cloudflared.deb\" %s", cloudflaredDebURL)
		w("          echo '%s  '\"$RUNNER_TEMP/cloudflared.deb\" | sha256sum -c -", cloudflaredDebSHA256)
		w("          sudo dpkg -i \"$RUNNER_TEMP/cloudflared.deb\"")
	case connTailscale:
		w("      - name: Connect to Tailscale")
		w("        uses: tailscale/github-action@v4")
		w("        with:")
		if s.TailscaleOAuth {
			w("          oauth-client-id: ${{ secrets.%s }}", secretTSID)
			w("          oauth-secret: ${{ secrets.%s }}", secretTSSecret)
		} else {
			w("          oauth-client-id: ${{ vars.%s }}", varTSClient)
			w("          audience: ${{ vars.%s }}", varTSAudience)
		}
		w("          tags: %s", tailscaleTag)
	}
	w("      - name: Set up ssh")
	w("        env:")
	w("          %s: ${{ secrets.%s }}", secretSSHKey, secretSSHKey)
	w("          %s: ${{ secrets.%s }}", secretKnownHosts, secretKnownHosts)
	w("        run: |")
	w("          install -d -m 700 ~/.ssh")
	w("          printf '%%s\\n' \"$TIFA_DEPLOY_SSH_KEY\" > ~/.ssh/deploy_key")
	w("          chmod 600 ~/.ssh/deploy_key")
	w("          printf '%%s\\n' \"$TIFA_DEPLOY_KNOWN_HOSTS\" > ~/.ssh/known_hosts")
	w("          cat > ~/.ssh/config <<'EOF'")
	w("          Host *")
	w("            IdentityFile ~/.ssh/deploy_key")
	w("            IdentitiesOnly yes")
	w("            StrictHostKeyChecking yes")
	if s.Mode == connCloudflare {
		w("            ProxyCommand cloudflared access ssh --hostname %%h")
	}
	w("          EOF")
	w("      - name: Deploy")
	w("        env:")
	w("          ACTION: ${{ inputs.action }}")
	w("          %s: ${{ secrets.%s }}", deployHostEnv, secretHost)
	if s.Purge {
		w("          %s: ${{ secrets.%s }}", envPurgeToken, secretPurge)
		w("          %s: ${{ secrets.%s }}", envZoneID, secretZoneID)
	}
	if s.Mode == connCloudflare {
		// cloudflared reads these two variables for the service token, see its access command.
		w("          TUNNEL_SERVICE_TOKEN_ID: ${{ secrets.%s }}", secretCFID)
		w("          TUNNEL_SERVICE_TOKEN_SECRET: ${{ secrets.%s }}", secretCFSecret)
	}
	w("        run: |")
	w("          if [ \"$ACTION\" = rollback ]; then")
	w("            go tool tifa deploy rollback --host \"$TIFA_DEPLOY_HOST\"")
	w("          else")
	w("            go tool tifa deploy --host \"$TIFA_DEPLOY_HOST\"")
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

// workflowSpec reads what the workflow needs from the app. branch is the one that deploys and
// ciName the name of the workflow it waits for, empty to wait for none.
func workflowSpec(t deployTarget, branch, ciName, mode string, purge bool) (githubWorkflowSpec, error) {
	s := githubWorkflowSpec{Branch: branch, Mode: mode, Purge: purge, CIName: ciName}
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
