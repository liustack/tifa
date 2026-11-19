package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/liustack/tifa/internal/codegen"
	"github.com/liustack/tifa/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
)

// deployHostEnv overrides deploy.host of tifa.yaml, as CI sets it. It is the environment variable
// the naming rule of settings gives deploy.host.
const deployHostEnv = "DEPLOY_HOST"

// sshProgram is the ssh the deploy commands run: the one of the system, so ~/.ssh/config,
// ProxyCommand, agents and Tailscale names work as they do in a terminal. Tests point it at a fake.
var sshProgram = "ssh"

// deployNow is the clock that names releases. Tests set it.
var deployNow = time.Now

// buildForServer builds the app at root for goos/goarch into out. It runs this tifa again as tifa
// build with GOOS and GOARCH set, as docs/build.md says to build for another system, so the
// analysis and go build pick the files of the target. Tests replace it.
var buildForServer = func(ctx context.Context, root, goos, goarch, out string, stdout, stderr io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	build := exec.CommandContext(ctx, exe, "build", "--dir", root, "-o", out)
	// This tifa is already the one the app pins, so the build does not forward again.
	build.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, forwardedEnv+"=1")
	build.Stdout, build.Stderr = stdout, stderr
	return build.Run()
}

// deployTarget is an app and the server it goes to.
type deployTarget struct {
	root   string
	app    string
	host   string
	domain string
}

// loadDeployTarget reads the app at dir: its name from go.mod, deploy.domain from tifa.yaml, and
// the host from the flag, else DEPLOY_HOST, else deploy.host.
func loadDeployTarget(dir, hostFlag string) (deployTarget, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return deployTarget{}, err
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return deployTarget{}, fmt.Errorf("%s is not the root of an app: %w", root, err)
	}
	modulePath := modfile.ModulePath(data)
	if modulePath == "" {
		return deployTarget{}, fmt.Errorf("%s has no module line", filepath.Join(root, "go.mod"))
	}
	app, err := deployAppName(modulePath)
	if err != nil {
		return deployTarget{}, err
	}
	settings, err := codegen.ProjectSettings(root)
	if err != nil {
		return deployTarget{}, err
	}
	t := deployTarget{root: root, app: app, host: settings.Deploy.Host, domain: settings.Deploy.Domain}
	switch env := os.Getenv(deployHostEnv); {
	case hostFlag != "":
		if err := config.CheckDeployHost(hostFlag); err != nil {
			return t, fmt.Errorf("--host: %w", err)
		}
		t.host = hostFlag
	case env != "":
		if err := config.CheckDeployHost(env); err != nil {
			return t, fmt.Errorf("%s: %w", deployHostEnv, err)
		}
		t.host = env
	}
	if t.host == "" {
		return t, fmt.Errorf("no server to deploy to: set deploy.host in tifa.yaml (a host of ~/.ssh/config or an address), or pass --host or %s", deployHostEnv)
	}
	return t, nil
}

// ssh runs script with sh on the server, logged in as user, with stdin as its input.
func (t deployTarget) ssh(ctx context.Context, user, script string, stdin io.Reader, stdout, stderr io.Writer) error {
	c := exec.CommandContext(ctx, sshProgram, "-l", user, "--", t.host, "sh -c "+shellQuote(script))
	c.Stdin, c.Stdout, c.Stderr = stdin, stdout, stderr
	err := c.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 255 {
		hint := ""
		if user == deployUser {
			hint = fmt.Sprintf(". If the server has no user %s yet, run go tool tifa deploy setup first", deployUser)
		}
		return fmt.Errorf("ssh %s@%s failed%s: %w", user, t.host, hint, err)
	}
	return err
}

// state asks the server, as the deploy user, about the app.
func (t deployTarget) state(ctx context.Context, stderr io.Writer) (serverState, error) {
	var out bytes.Buffer
	if err := t.ssh(ctx, deployUser, probeScript(t.app), nil, &out, stderr); err != nil {
		return serverState{}, err
	}
	s, err := parseServerState(out.String())
	if err != nil {
		return s, err
	}
	if s.Port == 0 {
		return s, fmt.Errorf("%s is not set up on %s yet: run go tool tifa deploy setup", t.app, t.host)
	}
	return s, nil
}

func newDeployCmd() *cobra.Command {
	var dir, host string
	var cloudflare bool
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Build the app for its server, put it live over ssh and check it",
		Long: `deploy builds the app for the system of the server in deploy.host (tifa.yaml, --host or
DEPLOY_HOST), uploads the program over ssh as the user tifa into a new release, points current at
it and restarts the app through its systemd socket, so no request is refused. On the server it then
asks GET /healthz until the answer is below 500. When that fails, the release before runs again
and deploy exits with 1. It never uploads .env. Run tifa deploy setup once per app first.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := loadDeployTarget(dir, host)
			if err != nil {
				return err
			}
			return deploy(cmd.Context(), t, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", ".", "application root, the directory holding go.mod and app/")
	cmd.PersistentFlags().StringVar(&host, "host", "", "server to use in place of deploy.host of tifa.yaml and "+deployHostEnv)

	setup := &cobra.Command{
		Use:   "setup",
		Short: "Prepare the server for the app, once, as root over ssh",
		Long: `setup logs in as root and creates the user tifa (shared by the apps of the server) with the
keys root logs in with, the folder of the app with an empty shared/.env, a free loopback port, and a
systemd socket and service for the app that run as tifa. With deploy.domain set and cf, the
Cloudflare CLI, installed and logged in, it puts the app behind the Cloudflare Tunnel of the server:
it creates the tunnel and runs cloudflared on the server when the server has none, and adds the
route, a proxied DNS record and cache rules for the domain. Without cf it prints these steps.
Running it again changes nothing that is there and keeps the port.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := loadDeployTarget(dir, host)
			if err != nil {
				return err
			}
			return deploySetup(cmd.Context(), t, cloudflare, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	rollback := &cobra.Command{
		Use:   "rollback",
		Short: "Run the release before the current one again",
		Long: `rollback points current at the newest earlier release that passed its health check and
restarts the app through its socket. Run it again to go further back.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := loadDeployTarget(dir, host)
			if err != nil {
				return err
			}
			return deployRollback(cmd.Context(), t, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	setup.Flags().BoolVar(&cloudflare, "cloudflare", true, "set up the Cloudflare Tunnel, DNS and cache rules of deploy.domain with cf when it is logged in")
	cmd.AddCommand(setup, rollback)
	return cmd
}

// setupState is what setupProbeScript prints.
type setupState struct {
	OS, Arch string
	Home     string // home of the deploy user, empty before it exists
	Port     int    // port of the app, 0 before its setup
	Used     []int  // ports of the other apps, and ports something listens on
	// TunnelID is the Cloudflare Tunnel the server runs, from /etc/tifa/tunnel, empty without one.
	TunnelID string
	// Machine is the host name of the server and the start of its machine ID, which name its tunnel.
	Machine string
}

func parseSetupState(out string) (setupState, error) {
	var s setupState
	for _, line := range strings.Split(out, "\n") {
		word, value, _ := strings.Cut(line, " ")
		switch word {
		case "os":
			s.OS = value
		case "arch":
			s.Arch = value
		case "home":
			s.Home = value
		case "port", "taken":
			p, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return s, fmt.Errorf("the server gave the port %q", value)
			}
			if word == "port" {
				s.Port = p
			} else {
				s.Used = append(s.Used, p)
			}
		case "listen":
			s.Used = append(s.Used, listeningPorts(value)...)
		case "tunnel":
			s.TunnelID = strings.TrimSpace(value)
		case "machine":
			s.Machine = strings.TrimSpace(value)
		}
	}
	return s, nil
}

func deploySetup(ctx context.Context, t deployTarget, cloudflare bool, stdout, stderr io.Writer) error {
	var out bytes.Buffer
	if err := t.ssh(ctx, "root", setupProbeScript(t.app), nil, &out, stderr); err != nil {
		return err
	}
	s, err := parseSetupState(out.String())
	if err != nil {
		return err
	}
	if _, _, err := serverTarget(s.OS, s.Arch); err != nil {
		return err
	}
	port := s.Port
	if port == 0 {
		if port, err = pickPort(s.Used); err != nil {
			return err
		}
	}
	out.Reset()
	if err := t.ssh(ctx, "root", setupScript(t.app, port), nil, &out, stderr); err != nil {
		return err
	}
	home := deployHome
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if h, ok := strings.CutPrefix(line, "home "); ok {
			home = h
		} else if line != "" {
			fmt.Fprintln(stdout, line)
		}
	}
	dir := home + "/" + t.app
	fmt.Fprintf(stdout, `set up %[1]s on %[2]s
  folder      %[3]s, owned by the user %[4]s
  settings    %[3]s/shared/.env, empty: fill it on the server, deploys never touch it
  data        %[3]s/shared, the folder the app runs in
  listens on  127.0.0.1:%[5]d, held by the systemd socket %[6]s.socket of %[4]s
`, t.app, t.host, dir, deployUser, port, unitName(t.app))
	switch {
	case t.domain == "":
		fmt.Fprint(stdout, "\nSet deploy.domain in tifa.yaml and run setup again to put the app on the web.\n")
	case !cloudflare:
		printTunnelSteps(stdout, t.domain, port, "")
	case !cfLoggedIn(ctx):
		printTunnelSteps(stdout, t.domain, port, "cf, the Cloudflare CLI, is not installed or not logged in, so Tifa did not set up Cloudflare. With cf logged in (cf auth login), setup does these steps itself. ")
	default:
		fmt.Fprintln(stdout)
		err := cloudflareSetup(ctx, tunnelSetup{
			app: t.app, domain: t.domain, port: port, tunnelID: s.TunnelID, tunnelName: "tifa-" + s.Machine,
			server: func(ctx context.Context, id, token string) (string, error) {
				var out bytes.Buffer
				if err := t.ssh(ctx, "root", tunnelScript(id), strings.NewReader(token), &out, stderr); err != nil {
					return "", fmt.Errorf("running the tunnel on the server: %w", err)
				}
				lines := strings.Split(strings.TrimSpace(out.String()), "\n")
				return lines[len(lines)-1], nil
			},
		}, stdout)
		if err != nil {
			return err
		}
	}
	fmt.Fprint(stdout, "Then deploy it with go tool tifa deploy\n")
	return nil
}

// printTunnelSteps prints what to do by hand to put the app behind a Cloudflare Tunnel.
func printTunnelSteps(w io.Writer, domain string, port int, why string) {
	fmt.Fprintf(w, `
%[3]sTo put the app on the web through a Cloudflare Tunnel:
  1. Create a tunnel in Cloudflare (Zero Trust, Networks, Tunnels), or open the one of this server,
     and run its cloudflared on the server.
  2. Add a public hostname to it: %[1]s -> http://127.0.0.1:%[2]d
  3. Set up caching for %[1]s as docs/deploy.md, CDN settings, says.
`, domain, port, why)
}

func deploy(ctx context.Context, t deployTarget, stdout, stderr io.Writer) error {
	started := time.Now()
	s, err := t.state(ctx, stderr)
	if err != nil {
		return err
	}
	goos, goarch, err := serverTarget(s.OS, s.Arch)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "deploying %s to %s (%s/%s, port %d)\n", t.app, t.host, goos, goarch, s.Port)
	tmp, err := os.MkdirTemp("", "tifa-deploy-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	program := filepath.Join(tmp, t.app)
	if err := buildForServer(ctx, t.root, goos, goarch, program, stdout, stderr); err != nil {
		return fmt.Errorf("build for %s/%s: %w", goos, goarch, err)
	}
	id := deployNow().UTC().Format("20060102T150405Z")
	if err := t.upload(ctx, program, id, stdout, stderr); err != nil {
		return err
	}
	a := activation{App: t.app, ID: id, Commit: gitCommit(t.root), Port: s.Port, Action: "deploy",
		Back: fallback(s), Prune: pruneReleases(s.Releases, s.Current, id, keepReleases), Tries: healthTries}
	if err := t.activate(ctx, a, stdout, stderr); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "deployed %s release %s (commit %s) in %s\n", t.app, id, a.Commit, time.Since(started).Round(100*time.Millisecond))
	return nil
}

func deployRollback(ctx context.Context, t deployTarget, stdout, stderr io.Writer) error {
	started := time.Now()
	s, err := t.state(ctx, stderr)
	if err != nil {
		return err
	}
	target, err := rollbackTarget(s)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "rolling %s on %s back from %s to %s (commit %s)\n", t.app, t.host, s.Current, target.ID, target.Commit)
	a := activation{App: t.app, ID: target.ID, Commit: target.Commit, Port: s.Port, Action: "rollback",
		Back: s.Current, Tries: healthTries}
	if err := t.activate(ctx, a, stdout, stderr); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "rolled back to %s in %s\n", target.ID, time.Since(started).Round(100*time.Millisecond))
	return nil
}

// upload sends the program, gzipped, into the release id and checks on the server that it
// arrived whole.
func (t deployTarget) upload(ctx context.Context, program, id string, stdout, stderr io.Writer) error {
	f, err := os.Open(program)
	if err != nil {
		return err
	}
	defer f.Close()
	sum := sha256.New()
	size, err := io.Copy(sum, f)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "uploading release %s (%.1f MB, gzipped on the way)\n", id, float64(size)/1e6)
	r, w := io.Pipe()
	go func() {
		gz, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
		_, err := io.Copy(gz, f)
		w.CloseWithError(errors.Join(err, gz.Close()))
	}()
	err = t.ssh(ctx, deployUser, uploadScript(t.app, id, hex.EncodeToString(sum.Sum(nil))), r, stdout, stderr)
	r.Close() // the gzip goroutine stops when ssh ended early
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	return nil
}

// activate runs activateScript and turns its exit status into an error.
func (t deployTarget) activate(ctx context.Context, a activation, stdout, stderr io.Writer) error {
	err := t.ssh(ctx, deployUser, activateScript(a), nil, stdout, stderr)
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return err
	}
	switch exit.ExitCode() {
	case activateFailedBack:
		return &exitError{code: 1, err: fmt.Errorf("release %s failed its health check, and %s runs again", a.ID, a.Back)}
	case activateFailed:
		return &exitError{code: 1, err: fmt.Errorf("release %s failed its health check, and no release runs: look at the log above, then deploy again", a.ID)}
	}
	return err
}

// gitCommit names the commit the app is built from, with -dirty when the files of the app differ
// from it, or none outside a git repository.
func gitCommit(root string) string {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--short=12", "HEAD").Output()
	if err != nil {
		return "none"
	}
	commit := strings.TrimSpace(string(out))
	if st, err := exec.Command("git", "-C", root, "status", "--porcelain", "--", ".").Output(); err == nil && len(bytes.TrimSpace(st)) > 0 {
		commit += "-dirty"
	}
	return commit
}
