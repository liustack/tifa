package main

// tifa deploy github prepares deploys from GitHub Actions, once, from the machine of the developer.
// It writes the workflow, makes a key that only this repository uses and lets it log in as the
// deploy user, reads the host keys of the server over the ssh that is trusted already, and sets the
// secrets of the repository with gh. The private key and the other secrets go from memory to gh
// on its standard input: never to a file, a command line or the output.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
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
	"github.com/spf13/cobra"
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
)

// githubWorkflowPath is where the workflow goes, relative to the root of the app.
const githubWorkflowPath = ".github/workflows/deploy.yml"

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
	if _, err := runGH(ctx, dir, nil, "auth", "status"); err != nil {
		return githubRepo{}, fmt.Errorf("gh is not logged in: run gh auth login (%w)", err)
	}
	out, err := runGH(ctx, dir, nil, "repo", "view", "--json", "nameWithOwner,defaultBranchRef")
	if err != nil {
		return githubRepo{}, fmt.Errorf("%s is not the folder of a GitHub repository you can use: %w", dir, err)
	}
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

// printGitHubManual prints what to do by hand when gh cannot be used.
func printGitHubManual(w io.Writer, why error, mode string) {
	fmt.Fprintf(w, `
%v

Tifa sets up the deploys from GitHub Actions with gh, the GitHub CLI. Install it
(https://cli.github.com/), run gh auth login, and run this command again in the folder of
the repository. To do it by hand instead:
  1. Make a key for this repository: ssh-keygen -t ed25519 -N '' -C 'tifa-github <owner>/<repo>' -f deploy_key
  2. Add the public key (deploy_key.pub) to /srv/tifa/.ssh/authorized_keys on the server.
  3. In the repository, under Settings, Secrets and variables, Actions, add these secrets:
     %s  the private key (deploy_key)
     %s  the host keys of the server in known_hosts format, for the name CI connects to
     %s  the address CI connects to
`, why, secretSSHKey, secretKnownHosts, secretHost)
	switch mode {
	case connCloudflare:
		fmt.Fprintf(w, "     %s and %s  a Cloudflare Access service token that may reach the ssh host name\n", secretCFID, secretCFSecret)
	case connTailscale:
		fmt.Fprintf(w, "     %s and %s  an OAuth client of Tailscale\n", secretTSID, secretTSSecret)
	}
	fmt.Fprintln(w, "  4. Delete deploy_key and deploy_key.pub from your machine.")
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
				return r, fmt.Errorf("%s reaches the server through a %s in your ssh configuration, which GitHub cannot copy: use --cloudflare or --tailscale, or a host that ssh reaches directly", host, word)
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
}

var simpleBranch = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// githubWorkflow is the text of .github/workflows/deploy.yml.
func githubWorkflow(s githubWorkflowSpec) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	branch := s.Branch
	if !simpleBranch.MatchString(branch) {
		branch = strconv.Quote(branch)
	}
	w("# Written by go tool tifa deploy github. Edit it as you like: the command asks for --force")
	w("# before it writes this file again over a file that differs.")
	w("name: deploy")
	w("")
	w("on:")
	w("  push:")
	w("    branches: [%s]", branch)
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
	w("    runs-on: ubuntu-24.04")
	w("    timeout-minutes: 30")
	w("    steps:")
	w("      - uses: actions/checkout@v7")
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

// factsBody runs as the deploy user and prints the tunnel of the server, when it has one, and the
// public host keys of its sshd.
const factsBody = `if [ -f /etc/tifa/tunnel ]; then echo "tunnel $(cat /etc/tifa/tunnel)"; fi
for f in /etc/ssh/ssh_host_*_key.pub; do
	if [ -f "$f" ]; then echo "hostkey $(cut -d' ' -f1,2 "$f")"; fi
done
exit 0
`

// serverFacts is what factsBody prints.
type serverFacts struct {
	TunnelID string
	HostKeys []string // "<type> <base64>"
}

func parseServerFacts(out string) (serverFacts, error) {
	var f serverFacts
	for _, line := range strings.Split(out, "\n") {
		word, value, _ := strings.Cut(line, " ")
		switch word {
		case "tunnel":
			f.TunnelID = strings.TrimSpace(value)
		case "hostkey":
			typ, b64, ok := strings.Cut(strings.TrimSpace(value), " ")
			if _, err := base64.StdEncoding.DecodeString(b64); !ok || !strings.HasPrefix(typ, "ssh-") && !strings.HasPrefix(typ, "ecdsa-") || err != nil {
				return f, fmt.Errorf("the server gave a host key tifa cannot read: %q", value)
			}
			f.HostKeys = append(f.HostKeys, typ+" "+b64)
		}
	}
	if len(f.HostKeys) == 0 {
		return f, errors.New("the server has no /etc/ssh/ssh_host_*_key.pub file, so tifa cannot tell CI which server to trust")
	}
	return f, nil
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

type githubOptions struct {
	mode  string
	force bool
}

func newDeployGitHubCmd(dir, host *string) *cobra.Command {
	var cloudflare, tailscale, force bool
	cmd := &cobra.Command{
		Use:   "github",
		Short: "Prepare deploys from GitHub Actions: workflow, key, host keys and secrets",
		Long: `github writes .github/workflows/deploy.yml, which deploys the app when the default branch is
pushed and, from the Actions tab, deploys or rolls back by hand. It makes a key for this repository
alone, lets it log in as the user tifa on the server (a line in authorized_keys that names the
repository, replaced when you run this again), reads the host keys of the server over your ssh so
CI trusts that server and no other, and sets the secrets of the repository with gh (the GitHub CLI,
logged in). Secrets go to gh on its standard input, and are never written down or printed. Run
go tool tifa deploy setup first.

By default CI connects to the address ssh resolves for the server. --cloudflare adds an ssh route
to the Cloudflare Tunnel of the server, a DNS record, an Access application and a service token for
this repository with cf (the Cloudflare CLI, logged in), and CI connects through cloudflared.
--tailscale makes the workflow join your tailnet, and prints the two Tailscale secrets you add.
An existing workflow file that differs is kept unless you pass --force.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := loadDeployTarget(*dir, *host)
			if err != nil {
				return err
			}
			mode := connDirect
			if cloudflare {
				mode = connCloudflare
			} else if tailscale {
				mode = connTailscale
			}
			return deployGitHub(cmd.Context(), t, githubOptions{mode: mode, force: force}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().BoolVar(&cloudflare, "cloudflare", false, "connect through the Cloudflare Tunnel of the server, with an Access service token made with cf")
	cmd.Flags().BoolVar(&tailscale, "tailscale", false, "connect through your tailnet, with the OAuth client of Tailscale")
	cmd.Flags().BoolVar(&force, "force", false, "replace "+githubWorkflowPath+" when it differs from what this command writes")
	cmd.MarkFlagsMutuallyExclusive("cloudflare", "tailscale")
	return cmd
}

// workflowSpec reads what the workflow needs from the app.
func workflowSpec(t deployTarget, repo githubRepo, mode string, r sshResolved) (githubWorkflowSpec, error) {
	s := githubWorkflowSpec{Branch: repo.Branch, Mode: mode, Port: r.Port}
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

func deployGitHub(ctx context.Context, t deployTarget, o githubOptions, stdout, stderr io.Writer) error {
	say := func(format string, args ...any) { fmt.Fprintf(stdout, format+"\n", args...) }
	if o.mode == connCloudflare && t.domain == "" {
		return errors.New("--cloudflare needs deploy.domain in tifa.yaml: the ssh host name is made from it")
	}
	if o.mode == connCloudflare && !cfLoggedIn(ctx) {
		return errors.New("--cloudflare needs cf, the Cloudflare CLI, installed and logged in (cf auth login)")
	}
	repo, err := findGitHubRepo(ctx, t.root)
	if err != nil {
		printGitHubManual(stdout, err, o.mode)
		return errors.New("cannot use gh, the steps to do by hand are above")
	}
	say("github: %s, default branch %s", repo.Name, repo.Branch)

	// Everything that can fail before the first change comes first.
	var resolved sshResolved
	if o.mode != connCloudflare {
		if resolved, err = resolveSSH(ctx, t.host); err != nil {
			return err
		}
	}
	spec, err := workflowSpec(t, repo, o.mode, resolved)
	if err != nil {
		return err
	}
	text := githubWorkflow(spec)
	workflow := filepath.Join(t.root, filepath.FromSlash(githubWorkflowPath))
	have, err := os.ReadFile(workflow)
	if err == nil && string(have) != text && !o.force {
		return fmt.Errorf("%s differs from what this command writes: keep it and set the secrets by hand, or pass --force to replace it", githubWorkflowPath)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	var out bytes.Buffer
	if err := t.ssh(ctx, deployUser, factsBody, nil, &out, stderr); err != nil {
		return err
	}
	facts, err := parseServerFacts(out.String())
	if err != nil {
		return err
	}

	connect, port := resolved.Host, resolved.Port
	secrets := []secret{}
	if o.mode == connCloudflare {
		if facts.TunnelID == "" {
			return fmt.Errorf("%s has no Cloudflare Tunnel yet: run go tool tifa deploy setup first, with cf logged in", t.host)
		}
		access, err := sshAccessSetup(ctx, sshAccess{app: t.app, domain: t.domain, repo: repo.Name, tunnelID: facts.TunnelID}, stdout)
		if err != nil {
			return err
		}
		connect, port = access.host, 22
		secrets = append(secrets, secret{secretCFID, access.clientID}, secret{secretCFSecret, access.clientSecret})
	}

	key, err := newDeployKey(repo.Name)
	if err != nil {
		return err
	}
	out.Reset()
	if err := t.ssh(ctx, deployUser, authorizeScript(repo.Name), strings.NewReader(key.authorizedLine(repo.Name)+"\n"), &out, stderr); err != nil {
		return err
	}
	say("server: the key of %s may log in as %s on %s", repo.Name, deployUser, t.host)

	hosts := strings.Join(knownHostsLines(connect, port, facts.HostKeys), "\n")
	secrets = append([]secret{{secretSSHKey, key.Private}, {secretKnownHosts, hosts}, {secretHost, connect}}, secrets...)
	for _, s := range secrets {
		if _, err := runGH(ctx, t.root, strings.NewReader(s.value), "secret", "set", s.name, "--repo", repo.Name); err != nil {
			return fmt.Errorf("%w (the key on the server is new: run this command again)", err)
		}
	}
	names := make([]string, len(secrets))
	for i, s := range secrets {
		names[i] = s.name
	}
	say("github: secrets set: %s", strings.Join(names, ", "))

	if err := os.MkdirAll(filepath.Dir(workflow), 0o755); err != nil {
		return err
	}
	next := fmt.Sprintf("Every push to %s deploys.", repo.Branch)
	if string(have) == text {
		say("%s is up to date", githubWorkflowPath)
	} else {
		if err := os.WriteFile(workflow, []byte(text), 0o644); err != nil {
			return err
		}
		say("wrote %s", githubWorkflowPath)
		next = fmt.Sprintf("Commit and push %s: then every push to %s deploys.", githubWorkflowPath, repo.Branch)
	}
	if o.mode == connTailscale {
		fmt.Fprintf(stdout, `
Add two secrets by hand, which Tifa cannot make: in Tailscale, under Settings, OAuth clients,
make a client with the auth_keys scope for the tag tag:ci, and set %s and %s in the
repository. The tag tag:ci must exist in the policy of your tailnet (tagOwners), and may reach the
server on port 22.
`, secretTSID, secretTSSecret)
	}
	say("\n%s To roll back, run the deploy workflow by hand in the Actions tab and pick rollback.", next)
	return nil
}

// secret is a repository secret: never printed.
type secret struct{ name, value string }
