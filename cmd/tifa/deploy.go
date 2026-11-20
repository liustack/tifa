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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
	root string
	app  string
	host string
	// domain is the host of site.url when deploy.cdn is cloudflare, else empty.
	domain string
	// sshMode is how CI reaches the server: direct, cloudflare or tailscale. github says
	// deploy.ci is github: the GitHub Actions of the repository deploy the app.
	sshMode string
	github  bool
}

// deployDomain checks the deploy settings against each other and returns the domain of the app:
// the host of site.url when deploy.cdn is cloudflare, else empty.
func deployDomain(d config.Deploy, siteURL string) (string, error) {
	domain := ""
	if d.CDN == "cloudflare" {
		if siteURL == "" {
			return "", errors.New("deploy.cdn is cloudflare, which needs site.url: set it in tifa.yaml, such as https://shop.example.com")
		}
		u, err := url.Parse(siteURL)
		if err != nil {
			return "", fmt.Errorf("site.url: %w", err)
		}
		if u.Port() != "" {
			return "", fmt.Errorf("site.url has the port %s, and Cloudflare serves the host name only: write https://%s, or set deploy.cdn to none", u.Port(), u.Hostname())
		}
		domain = u.Hostname()
	}
	switch {
	case d.SSH == "cloudflare" && d.CDN != "cloudflare":
		return "", errors.New("deploy.ssh is cloudflare, and the ssh host name is made from the domain of site.url: set deploy.cdn to cloudflare")
	case d.SSH != "direct" && d.CI != "github":
		return "", fmt.Errorf("deploy.ssh is %s, which is how GitHub Actions reaches the server: set deploy.ci to github, or deploy.ssh to direct (deploys from this machine use ~/.ssh/config)", d.SSH)
	}
	return domain, nil
}

// loadDeployTarget reads the app at dir: its name from go.mod, the deploy settings from tifa.yaml, and
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
	if err := retiredDeployKeys(root); err != nil {
		return deployTarget{}, err
	}
	domain, err := deployDomain(settings.Deploy, settings.Site.URL)
	if err != nil {
		return deployTarget{}, err
	}
	t := deployTarget{root: root, app: app, host: settings.Deploy.Host, domain: domain, sshMode: settings.Deploy.SSH, github: settings.Deploy.CI == "github"}
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

// retiredDeployKeys fails when tifa.yaml still has deploy.domain or deploy.github. They are
// ignored otherwise, and setup would then plan to take away what they asked for.
func retiredDeployKeys(root string) error {
	data, err := os.ReadFile(filepath.Join(root, "tifa.yaml"))
	if err != nil {
		return nil // no project file: the defaults apply
	}
	for _, w := range config.ProjectWarnings(data) {
		if strings.Contains(w, ": deploy.") {
			return errors.New(w)
		}
	}
	return nil
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
		return &sshLoginError{user: user, host: t.host, hint: hint, err: err}
	}
	return err
}

// sshLoginError is ssh failing to log in, as opposed to a script that ran and failed.
type sshLoginError struct {
	user, host, hint string
	err              error
}

func (e *sshLoginError) Error() string {
	return fmt.Sprintf("ssh %s@%s failed%s: %v", e.user, e.host, e.hint, e.err)
}
func (e *sshLoginError) Unwrap() error { return e.err }

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
	return s, nil
}

// createApp makes the folder, port and units of the app on a server that is prepared, as the
// deploy user: no root. A server that is not prepared needs deploy setup.
func (t deployTarget) createApp(ctx context.Context, s serverState, stdout, stderr io.Writer) (serverState, error) {
	if !s.Prepared() {
		return s, fmt.Errorf("%s is not prepared for tifa apps: run go tool tifa deploy setup", t.host)
	}
	port, err := pickPort(s.Used)
	if err != nil {
		return s, err
	}
	if err := t.ssh(ctx, deployUser, appScript(t.app, port), nil, io.Discard, stderr); err != nil {
		return s, err
	}
	fmt.Fprintf(stdout, "created %s on %s: %s/%s, port %d, empty shared/.env\n", t.app, t.host, s.Home, t.app, port)
	return t.state(ctx, stderr)
}

func newDeployCmd() *cobra.Command {
	var dir, host string
	var yes, all bool
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Build the app for its server, put it live over ssh and check it",
		Long: `deploy builds the app for the system of the server in deploy.host (tifa.yaml, --host or
DEPLOY_HOST), uploads the program over ssh as the user tifa into a new release, points current at
it and restarts the app through its systemd socket, so no request is refused. On the server it then
asks GET /healthz until the answer is below 500. When that fails, the release before runs again
and deploy exits with 1. With deploy.cdn cloudflare it then clears the CDN cache of the host of site.url.
It never uploads .env. On a server that setup prepared it makes the folder and units of a new
app by itself. Run tifa deploy setup to prepare the server, Cloudflare and GitHub.`,
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
		Short: "Make the server, Cloudflare and GitHub match the deploy settings of tifa.yaml",
		Long: `setup compares the deploy section of tifa.yaml with the server, Cloudflare and GitHub, prints
a plan, asks, and applies it with one line for each step. The plan lists what will add, change
or remove, counts what is already right (--all lists it), and ends with what you must do because
setup cannot. When something is left for you, setup does the rest and then exits with an error.
When all is right it prints one line and asks nothing. It can run again at any time.

  server    the user tifa and its systemd at boot, the first time as root
  app       the folder, port and systemd units of this app, as tifa
  cdn       with deploy.cdn cloudflare: the Cloudflare Tunnel, route, DNS and cache rules of
            the host of site.url (needs cf)
  ci        with deploy.ci github: a key for the repository, secrets, and the workflow (needs
            gh, and with deploy.ssh cloudflare also cf)

It removes only what Tifa made and marked. Without a terminal, pass --yes to apply the plan.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := loadDeployTarget(dir, host)
			if err != nil {
				return err
			}
			return deploySetup(cmd.Context(), t, yes, all, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
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
	setup.Flags().BoolVarP(&yes, "yes", "y", false, "apply the plan without asking")
	setup.Flags().BoolVar(&all, "all", false, "list the things that are already right too")
	cmd.AddCommand(setup, rollback, newDeployEnvCmd(&dir, &host))
	return cmd
}

func deploy(ctx context.Context, t deployTarget, stdout, stderr io.Writer) (err error) {
	started := time.Now()
	sum := deploySummary{App: t.app, Action: "deploy"}
	health := &healthCapture{w: stdout}
	defer func() { finishSummary(&sum, started, health, err, stderr) }()
	s, err := t.state(ctx, stderr)
	if err != nil {
		return err
	}
	if s.Port == 0 {
		if s, err = t.createApp(ctx, s, stdout, stderr); err != nil {
			return err
		}
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
	sum.Release = id
	if err := t.upload(ctx, program, id, stdout, stderr); err != nil {
		return err
	}
	a := activation{App: t.app, ID: id, Commit: gitCommit(t.root), Port: s.Port, Action: "deploy",
		Back: fallback(s), Prune: pruneReleases(s.Releases, s.Current, id, keepReleases), Tries: healthTries}
	sum.Commit = a.Commit
	if err := t.activate(ctx, a, health, stderr); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "deployed %s release %s (commit %s) in %s\n", t.app, id, a.Commit, time.Since(started).Round(100*time.Millisecond))
	sum.Cache = clearCache(ctx, t.domain)
	printCache(stdout, sum.Cache)
	return nil
}

func deployRollback(ctx context.Context, t deployTarget, stdout, stderr io.Writer) (err error) {
	started := time.Now()
	sum := deploySummary{App: t.app, Action: "rollback"}
	health := &healthCapture{w: stdout}
	defer func() { finishSummary(&sum, started, health, err, stderr) }()
	s, err := t.state(ctx, stderr)
	if err != nil {
		return err
	}
	target, err := rollbackTarget(s)
	if err != nil {
		return err
	}
	sum.Release, sum.Commit = target.ID, target.Commit
	fmt.Fprintf(stdout, "rolling %s on %s back from %s to %s (commit %s)\n", t.app, t.host, s.Current, target.ID, target.Commit)
	a := activation{App: t.app, ID: target.ID, Commit: target.Commit, Port: s.Port, Action: "rollback",
		Back: s.Current, Tries: healthTries}
	if err := t.activate(ctx, a, health, stderr); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "rolled back to %s in %s\n", target.ID, time.Since(started).Round(100*time.Millisecond))
	sum.Cache = clearCache(ctx, t.domain)
	printCache(stdout, sum.Cache)
	return nil
}

// finishSummary writes the summary of a run of GitHub Actions, when there is one to write.
func finishSummary(sum *deploySummary, started time.Time, health *healthCapture, err error, stderr io.Writer) {
	sum.Elapsed, sum.Health, sum.Err = time.Since(started), health.line, err
	if werr := writeStepSummary(*sum); werr != nil {
		fmt.Fprintf(stderr, "warning: the summary of the run was not written: %v\n", werr)
	}
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
