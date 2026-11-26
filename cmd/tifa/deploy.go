package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/liustack/tifa/internal/codegen"
	"github.com/liustack/tifa/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
)

// deployHostEnv names the server: the environment variable, or the line of the .env at the root
// of the app, that --host overrides. CI sets it from a secret.
const deployHostEnv = "TIFA_DEPLOY_HOST"

// sshProgram is the ssh the deploy commands run: the one of the system, so ~/.ssh/config,
// ProxyCommand, agents and Tailscale names work as they do in a terminal. Tests point it at a fake.
var sshProgram = "ssh"

// deployNow is the clock that names releases. Tests set it.
var deployNow = time.Now

// buildForServer builds the app at root for goos/goarch into out. It runs this tifa again as tifa
// build with GOOS and GOARCH set, as docs/build-and-deploy/build.md says to build for another system, so the
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
	// host is the server as ssh gets it, without the user: see deployHost.addr.
	host string
	// dest is the server as named, checked and taken apart, with the admin user when it names one.
	dest deployHost
	// domain is the host of site.url when deploy.front is cloudflare, else empty.
	domain string
	// front is deploy.front. own is the domain of the app with front none, which it serves HTTPS
	// for itself, and with front nginx, which nginx routes by: the host of site.url when that is a
	// domain, else empty, and visitors get plain HTTP on port 80.
	front, own string
	// connectMode is how ssh reaches the server: direct, cloudflare or tailscale. github says
	// the block deploy.auto is there: the GitHub Actions of the repository deploy the app, as auto says.
	connectMode string
	github      bool
	auto        config.Auto
}

// deployDomain checks the deploy settings against each other and returns the domain of the app:
// the host of site.url when deploy.front is cloudflare, else empty.
func deployDomain(d config.Deploy, siteURL string) (string, error) {
	domain := ""
	if d.Front == "cloudflare" {
		if siteURL == "" {
			return "", errors.New("deploy.front is cloudflare, which needs site.url: set it in tifa.yaml, such as https://shop.example.com")
		}
		u, err := url.Parse(siteURL)
		if err != nil {
			return "", fmt.Errorf("site.url: %w", err)
		}
		if u.Port() != "" {
			return "", fmt.Errorf("site.url has the port %s, and Cloudflare serves the host name only: write https://%s, or set deploy.front to none", u.Port(), u.Hostname())
		}
		domain = u.Hostname()
	}
	switch {
	case d.Connect == "cloudflare" && d.Front != "cloudflare":
		return "", errors.New("deploy.connect is cloudflare, and the ssh host name is made from the domain of site.url: set deploy.front to cloudflare")
	case d.Connect == "tailscale" && !d.Auto.On:
		return "", errors.New("deploy.connect is tailscale, which is how GitHub Actions reaches the server: add the block deploy.auto (auto: {}), or set deploy.connect to direct")
	}
	return domain, nil
}

// ownDomain is the domain an app with deploy.front none serves HTTPS for itself, and the host
// nginx routes to an app with deploy.front nginx: the host of site.url, unless that is missing, an IP
// address or localhost, and then visitors get plain HTTP. Visitors reach ports 80 and 443, so a
// site.url with another port cannot be its address.
func ownDomain(d config.Deploy, siteURL string) (string, error) {
	if (d.Front != "none" && d.Front != "nginx") || siteURL == "" {
		return "", nil
	}
	u, err := url.Parse(siteURL)
	if err != nil {
		return "", fmt.Errorf("site.url: %w", err)
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || net.ParseIP(host) != nil {
		return "", nil
	}
	if p := u.Port(); p != "" && p != "443" && p != "80" {
		if d.Front == "nginx" {
			return "", fmt.Errorf("site.url has the port %s, and with deploy.front nginx visitors reach ports 80 and 443: write https://%s, or set deploy.front to cloudflare", p, host)
		}
		return "", fmt.Errorf("site.url has the port %s, and with deploy.front none the app serves visitors on ports 80 and 443 itself: write https://%s, or set deploy.front to cloudflare", p, host)
	}
	return host, nil
}

// checkDeploySettings runs the checks of the deploy settings that need no network, which tifa
// check and build run so a setting that cannot work is found before a deploy, and tifa dev shows as
// warnings: the settings against each other, and with the block deploy.auto the workflow in needs.
func checkDeploySettings(root string) error {
	settings, err := codegen.ProjectSettings(root)
	if err != nil {
		return err
	}
	d := settings.Deploy
	if _, err := deployDomain(d, settings.Site.URL); err != nil {
		return err
	}
	if d.Auto.On {
		// The branch is checked when tifa.yaml names it. Without it the default branch is on GitHub.
		if _, err := resolveNeeds(root, d.Auto.Needs, d.Auto.Branch); err != nil {
			return err
		}
	}
	return nil
}

// loadDeployTarget reads the app at dir: its name from go.mod, the deploy settings from tifa.yaml, and
// the server from the flag, else TIFA_DEPLOY_HOST in the environment, else TIFA_DEPLOY_HOST in .env,
// else the servers of ~/.tifa. A server taken from there is announced on notice.
func loadDeployTarget(dir, hostFlag string, notice io.Writer) (deployTarget, error) {
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
	domain, err := deployDomain(settings.Deploy, settings.Site.URL)
	if err != nil {
		return deployTarget{}, err
	}
	own, err := ownDomain(settings.Deploy, settings.Site.URL)
	if err != nil {
		return deployTarget{}, err
	}
	if err := checkUnknownInputNames(root); err != nil {
		return deployTarget{}, err
	}
	h, err := readDeployHost(root, app, hostFlag, notice)
	if err != nil {
		return deployTarget{}, err
	}
	return deployTarget{root: root, app: app, host: h.addr(), dest: h, domain: domain, front: settings.Deploy.Front, own: own,
		connectMode: settings.Deploy.Connect, github: settings.Deploy.Auto.On, auto: settings.Deploy.Auto}, nil
}

// sshWaitDelay is how long ssh may leave its output open once it has ended.
const sshWaitDelay = 2 * time.Second

// ssh runs script with sh on the server, logged in as user, with stdin as its input. The user root
// stands for the admin user of the host: with one other than root, ssh logs in as it and the script
// runs through sudo -n, which fails with a noSudoError before the script starts when the user
// has no passwordless sudo.
func (t deployTarget) ssh(ctx context.Context, user, script string, stdin io.Reader, stdout, stderr io.Writer) error {
	viaSudo := user == "root" && t.dest.sudo()
	if viaSudo {
		user, script = t.dest.user, sudoScript(script)
	}
	args := append([]string{"-l", user}, sshOptions(ctx, t.host)...)
	c := exec.CommandContext(ctx, sshProgram, append(args, "--", t.host, "sh -c "+shellQuote(script))...)
	said := &tailWriter{}
	c.Stdin, c.Stdout, c.Stderr = stdin, stdout, io.MultiWriter(stderr, said)
	// Killing ssh does not close its output when another process holds it too, such as the
	// master of an ssh ControlMaster, so Wait would wait for that process. Once ssh has ended,
	// give the output this long, then close it.
	c.WaitDelay = sshWaitDelay
	err := c.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 255 {
		hint := ""
		switch {
		case strings.Contains(said.String(), "REMOTE HOST IDENTIFICATION HAS CHANGED"):
			hint = fmt.Sprintf(". The host key of %[1]s changed since this machine last logged in. If the server was rebuilt or reinstalled, delete the old key with ssh-keygen -R %[2]s and run again. If not, someone may be between you and the server: do not go on", t.host, t.knownHostsArg())
		case strings.Contains(said.String(), "Host key verification failed"):
			hint = fmt.Sprintf(". The host key of %[1]s is not in your known_hosts yet, and ssh could not ask whether to trust it. Log in once with ssh %[1]s in a terminal and check the key it shows, or accept it with ssh -o StrictHostKeyChecking=accept-new -l %[2]s %[1]s true, then run again", t.host, user)
		case strings.Contains(said.String(), "Could not resolve hostname") || strings.Contains(said.String(), "timed out") ||
			strings.Contains(said.String(), "Connection refused") || strings.Contains(said.String(), "No route to host"):
			hint = fmt.Sprintf(". ssh could not reach %s: check that the server is on, that the address is right and that ssh is open on its firewall", t.host)
		case user == deployUser:
			hint = fmt.Sprintf(". If the server has no user %s yet, run go tool tifa deploy setup first", deployUser)
		}
		return &sshLoginError{user: user, host: t.host, hint: hint, err: err}
	}
	if viaSudo && errors.As(err, &exit) && exit.ExitCode() == noSudoExit {
		return &noSudoError{user: user, host: t.host}
	}
	return err
}

// sshConnectTimeout is how long ssh may take to reach a server it reaches directly and to start
// talking to it, so that a server that is off fails in seconds rather than at the timeout of TCP.
const sshConnectTimeout = "15"

// sshOptions are the options of ssh for host. Without a terminal ssh cannot ask anything, so
// BatchMode makes it fail at once instead. A host that ssh reaches directly, with no ConnectTimeout
// in the ssh configuration, gets sshConnectTimeout. A ProxyCommand or ProxyJump gets none, because
// it may wait for the developer, as the login of Cloudflare Access does in the browser, and the
// timeout of ssh counts that wait.
func sshOptions(ctx context.Context, host string) []string {
	var opts []string
	if !stdinIsTerminal(os.Stdin) {
		opts = append(opts, "-o", "BatchMode=yes")
	}
	out, err := exec.CommandContext(ctx, sshProgram, "-G", "--", host).Output()
	if err != nil {
		// ssh itself reports a host it cannot read when it logs in: leave the timeout to it.
		return opts
	}
	direct, ownTimeout := true, true
	for _, line := range strings.Split(string(out), "\n") {
		word, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch strings.ToLower(word) {
		case "proxycommand", "proxyjump":
			if value != "" && value != "none" {
				direct = false
			}
		case "connecttimeout":
			ownTimeout = value != "none"
		}
	}
	if direct && !ownTimeout {
		opts = append(opts, "-o", "ConnectTimeout="+sshConnectTimeout)
	}
	return opts
}

// knownHostsName is the name known_hosts keeps the key of the server under: the host, with the
// port in [host]:port when it is not 22.
func (t deployTarget) knownHostsName() string {
	name, port := t.dest.name, t.dest.port
	if name == "" {
		name = t.host
	}
	if port != 0 && port != 22 {
		return fmt.Sprintf("[%s]:%d", name, port)
	}
	return name
}

// knownHostsArg is knownHostsName as a shell word: [host]:port is quoted, since a shell may read
// the brackets as a pattern.
func (t deployTarget) knownHostsArg() string {
	name := t.knownHostsName()
	if strings.HasPrefix(name, "[") {
		return shellQuote(name)
	}
	return name
}

// tailWriter keeps the last tailWriterSize bytes written to it.
type tailWriter struct{ b []byte }

const tailWriterSize = 4096

func (w *tailWriter) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	if len(w.b) > tailWriterSize {
		w.b = w.b[len(w.b)-tailWriterSize:]
	}
	return len(p), nil
}

func (w *tailWriter) String() string { return string(w.b) }

// noSudoExit is the status sudoScript ends with when sudo -n cannot run a command: the user has no
// passwordless sudo, or the server has no sudo. No script of tifa deploy exits with it.
const noSudoExit = 86

// sudoScript runs script as root through sudo, with root's HOME and the input of ssh. It first
// asks sudo for nothing, without the input, so a user who needs a password stops it before the
// script starts.
func sudoScript(script string) string {
	return fmt.Sprintf("sudo -n true </dev/null >/dev/null 2>&1 || exit %d\nexec sudo -n -H sh -c %s", noSudoExit, shellQuote(script))
}

// noSudoError is an admin user that logged in and cannot run sudo without a password.
type noSudoError struct{ user, host string }

func (e *noSudoError) Error() string {
	return fmt.Sprintf("ssh %[1]s@%[2]s worked, but %[1]s cannot run sudo without a password, and the steps that change the server run as root through it. "+
		"Give %[1]s passwordless sudo on the server, with the line %[1]s ALL=(ALL) NOPASSWD:ALL in a file of /etc/sudoers.d (and no requiretty for it, which makes sudo want a terminal), or name the host without a user so tifa deploy logs in as root. Nothing was changed by this step",
		e.user, e.host)
}

// rootLogin names who runs the steps of root, for a sentence: root, or the admin user with sudo.
func (t deployTarget) rootLogin() string {
	if t.dest.sudo() {
		return t.dest.user + " with sudo"
	}
	return "root"
}

// sshLoginError is ssh failing to log in, as opposed to a script that ran and failed.
type sshLoginError struct {
	user, host, hint string
	err              error
	// said is what ssh printed, when the caller kept it.
	said string
}

func (e *sshLoginError) Error() string {
	return fmt.Sprintf("ssh %s@%s failed%s: %v", e.user, e.host, e.hint, e.err)
}
func (e *sshLoginError) Unwrap() error { return e.err }

// state asks the server, as the deploy user, about the app.
func (t deployTarget) state(ctx context.Context, stderr io.Writer) (serverState, error) {
	return t.probe(ctx, probeScript(t.app), stderr)
}

// probe runs a script of probeBody as the deploy user and reads what it printed.
func (t deployTarget) probe(ctx context.Context, script string, stderr io.Writer) (serverState, error) {
	var out bytes.Buffer
	if err := t.ssh(ctx, deployUser, script, nil, &out, stderr); err != nil {
		return serverState{}, err
	}
	st, err := parseServerState(out.String())
	if err != nil {
		return st, err
	}
	return st, t.checkMachine(st.Machine)
}

// checkMachine stops a command whose server came from ~/.tifa when the machine at that host is not
// the one setup recorded: an ssh alias or an address can come to point at another server.
func (t deployTarget) checkMachine(got string) error {
	if t.dest.knownID == "" || got == t.dest.knownID {
		return nil
	}
	return fmt.Errorf("%s is not the server %s remembers: %s says it is %q, and the entry is %q. The host now points at another machine. Name the right server with --host or %s in .env, or delete the entry from that file",
		t.host, t.dest.from, t.host, got, t.dest.knownID, deployHostEnv)
}

// visitorPort is the port the app on s listens on: with front none 80, or 443 with a domain, and
// else its loopback port behind the tunnel or nginx, which it keeps, or the first one free.
func (t deployTarget) visitorPort(s serverState) (int, error) {
	switch {
	case t.front == "none" && t.own != "":
		return httpsPort, nil
	case t.front == "none":
		return httpPort, nil
	case s.Port >= firstPort && s.Port <= lastPort:
		return s.Port, nil
	}
	return pickPort(s.Used)
}

// portsFree checks that nothing but the app listens on the ports an app that serves visitors
// itself needs, and that the deploy user may open them. It names what is in the way and says what to
// do. programs names the programs of a port when s could not, as only root can: nil when it cannot.
// The nginx of Tifa is in the way too, unless gone says setup removes it before the app moves, which
// it does when this app is the only one the nginx has.
func (t deployTarget) portsFree(s serverState, port int, programs func() []listener, gone bool) error {
	if !servesVisitors(port) {
		return nil
	}
	ports := []int{httpPort}
	if port == httpsPort {
		ports = append(ports, httpsPort)
	}
	instead := fmt.Sprintf("or put %s behind Cloudflare with deploy.front: cloudflare in tifa.yaml", t.app)
	for _, p := range ports {
		other, names, taken := s.holder(t.app, p)
		switch {
		case !taken:
			continue
		case other != "":
			return fmt.Errorf("port %d of %s is taken by %s, an app Tifa set up there. Only one app on a server can serve visitors on ports 80 and 443 itself: remove %s from the server (go tool tifa deploy remove in its folder), %s. Nothing was changed", p, t.host, other, other, instead)
		}
		if len(names) == 0 && programs != nil {
			for _, l := range programs() {
				if l.Port == p {
					names = append(names, l.Programs...)
				}
			}
		}
		if s.NginxTifa && (len(names) == 0 || slices.ContainsFunc(names, func(n string) bool { return strings.HasPrefix(n, "nginx ") })) {
			if apps := slices.DeleteFunc(slices.Clone(s.NginxSites), func(a string) bool { return a == t.app }); len(apps) > 0 || !gone {
				return fmt.Errorf("port %d of %s is held by the nginx of Tifa, which serves %s. Only one program can listen on a port, so %s cannot serve visitors itself while it runs: set deploy.front: nginx (or cloudflare) in tifa.yaml, or remove its apps from the server first (go tool tifa deploy remove in their folders). Nothing was changed",
					p, t.host, pick(len(s.NginxSites) == 0, "no app", strings.Join(s.NginxSites, ", ")), t.app)
			}
			continue
		}
		if len(names) == 0 {
			return fmt.Errorf("port %d of %s is taken by a program that is not a Tifa app, and Tifa cannot see which without root: run sudo ss -ltnp on the server. Only one program can listen on a port: stop that one and keep it from starting at boot, %s. Nothing was changed", p, t.host, instead)
		}
		return fmt.Errorf("port %d of %s is taken by %s. Only one program can listen on a port: stop it on the server and keep it from starting at boot, %s. Nothing was changed", p, t.host, strings.Join(names, ", "), instead)
	}
	return nil
}

// createApp makes the folder, port and units of the app on a server that is prepared, as the
// deploy user: no root. A server that is not prepared needs deploy setup, and so does an app
// that serves visitors on ports 80 and 443 when the deploy user may not open them yet.
func (t deployTarget) createApp(ctx context.Context, s serverState, stdout, stderr io.Writer) (serverState, error) {
	if !s.Prepared() {
		return s, fmt.Errorf("%s is not prepared for tifa apps: run go tool tifa deploy setup", t.host)
	}
	port, err := t.visitorPort(s)
	if err != nil {
		return s, err
	}
	if err := t.portsFree(s, port, nil, false); err != nil {
		return s, err
	}
	if servesVisitors(port) && s.LowPort > httpPort {
		return s, fmt.Errorf("%s serves visitors on %s itself, which only root may open on %s yet: run go tool tifa deploy setup", t.app, portsText(port), t.host)
	}
	if err := t.ssh(ctx, deployUser, appScript(t.app, port, t.ownFor(port), false, false), nil, io.Discard, stderr); err != nil {
		return s, err
	}
	fmt.Fprintf(stdout, "created %s on %s: %s/%s, %s, empty shared/.env\n", t.app, t.host, s.Home, t.app, portsText(port))
	return t.state(ctx, stderr)
}

// ownFor is the domain of HTTPS on port: own on 443, else none.
func (t deployTarget) ownFor(port int) string {
	if port == httpsPort {
		return t.own
	}
	return ""
}

func newDeployCmd() *cobra.Command {
	var dir, host string
	var sf setupFlags
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Build the app for its server, put it live over ssh and check it",
		Long: `deploy builds the app for the system of the server (--host, or TIFA_DEPLOY_HOST in the environment or
in the .env of the app, else the only server of ~/.tifa/config.yaml), uploads the program over ssh as the user tifa into a new release, points current at
it and restarts the app through its systemd socket, so no request is refused. On the server it then
asks GET /healthz until the answer is below 500. When that fails, the release before runs again
and deploy exits with 1. With deploy.front cloudflare it then clears the CDN cache of the host of site.url,
with the token setup keeps on the server.
It never uploads .env. On a server that setup prepared it makes the folder and units of a new
app by itself. Run tifa deploy setup to prepare the server, Cloudflare and GitHub.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := loadDeployTarget(dir, host, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			return deploy(cmd.Context(), t, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", ".", "application root, the directory holding go.mod and app/")
	cmd.PersistentFlags().StringVar(&host, "host", "", "the server: an ssh alias or ssh://host[:port], with user@ in front when root cannot log in (default "+deployHostEnv+" of the environment or of .env, else the server in ~/.tifa)")

	setup := &cobra.Command{
		Use:   "setup",
		Short: "Make the server, Cloudflare and GitHub match the deploy settings of tifa.yaml",
		Long: `setup compares the deploy section of tifa.yaml with the server, Cloudflare and GitHub, prints
a plan, asks, and applies it with one line for each step. The plan lists what will add, change
or remove, counts what is already right (--all lists it), and ends with what you must do because
setup cannot. When something is left for you, setup does the rest and then exits with an error.
When all is right it prints one line and asks nothing. It can run again at any time.

  server    the user tifa and its systemd at boot, the first time as root, or as the user
            in front of the host (ubuntu@203.0.113.7) with passwordless sudo
  app       the folder, port and systemd units of this app, as tifa. With deploy.front none
            the app serves visitors itself on port 80, and with a domain in site.url on 443
            with HTTPS: setup stops when another program has them, and lets the systemd of
            tifa open them, as root once. When ufw is on and blocks them, setup allows them
            there, in rules with the comment tifa <app>. Tifa does not change the firewall or
            security groups of your provider
  front     with deploy.front cloudflare: the Cloudflare Tunnel, route, DNS and cache rules of
            the host of site.url (needs cf), and on the server a token that can only clear
            its cache, which every deploy reads. With deploy.front nginx: an nginx that setup
            installs with apt and owns, a site for the app, and with a domain certbot and its
            certificate. Setup stops when the server has an nginx it did not install, or
            another program on ports 80 and 443
  auto      with deploy.auto: a key for the repository, secrets, and the workflow (needs
            gh logged in, or a fine-grained token in TIFA_DEPLOY_GITHUB_TOKEN, and with
            deploy.connect cloudflare also cf)
  ssh       with deploy.connect cloudflare: an ssh host name on the tunnel and its Access
            application, which admits the email of your cf login. On this machine: cloudflared
            (installed with brew or winget when missing), a block in ~/.ssh/config, a check
            that ssh gets through (the first time it opens the Access login in your browser),
            and then TIFA_DEPLOY_HOST in .env is the ssh host

On a terminal, a missing cf or gh comes first: a plan that installs it with pnpm, brew or
winget and logs it in, then the plan of the rest. It removes only what Tifa made and marked. Without a terminal, pass --yes to apply the plan.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := loadDeployTarget(dir, host, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			return deploySetup(cmd.Context(), t, sf, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	rollback := &cobra.Command{
		Use:   "rollback [release]",
		Short: "Run the release before the current one again, or the one you name",
		Long: `rollback points current at the newest earlier release that passed its health check and
restarts the app through its socket. Run it again to go further back.

With a release, as tifa deploy status lists it (such as 20261120T110000Z), rollback points
current at that release instead, older or newer than the current one. The release must still be
on the server and have passed its health check when it was deployed. Otherwise rollback changes
nothing and lists the releases it can go to. If that release is the current one, rollback says so
and changes nothing. The restart and the health check are the same as without a release.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			release := ""
			if len(args) == 1 {
				release = args[0]
				if !releaseIDShape.MatchString(release) {
					return fmt.Errorf("%q is not a release ID: use one as tifa deploy status lists it, such as 20261120T110000Z", release)
				}
			}
			t, err := loadDeployTarget(dir, host, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			return deployRollback(cmd.Context(), t, release, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	setup.Flags().BoolVarP(&sf.yes, "yes", "y", false, "apply the plan without asking")
	setup.Flags().BoolVar(&sf.all, "all", false, "list the things that are already right too")
	setup.Flags().BoolVar(&sf.purgeToken, "cache-purge-token", false, "ask for the token that clears the CDN cache at each deploy, and replace the one the server has")
	setup.Flags().StringVar(&sf.tailscaleClientID, "tailscale-client-id", "", "the client ID of the Tailscale identity CI joins your tailnet with (not secret)")
	cmd.AddCommand(setup, rollback, newDeployEnvCmd(&dir, &host), newDeployLogsCmd(&dir, &host), newDeployStatusCmd(&dir, &host), newDeployRemoveCmd(&dir, &host))
	return cmd
}

// maskHost asks the log of GitHub Actions to hide the host part of the target, and not the port.
// Actions hides the exact text of the secret TIFA_DEPLOY_HOST, but ssh prints the address it resolved
// in its errors, which is not that text.
func maskHost(w io.Writer, t deployTarget) {
	if os.Getenv(envGitHubActions) == "true" {
		fmt.Fprintf(w, "::add-mask::%s\n", t.dest.name)
	}
}

func deploy(ctx context.Context, t deployTarget, stdout, stderr io.Writer) (err error) {
	maskHost(stdout, t)
	started := time.Now()
	sum := deploySummary{App: t.app, Action: "deploy"}
	health := &healthCapture{w: stdout}
	defer func() { finishSummary(&sum, started, health, err, stderr) }()
	s, err := t.state(ctx, stderr)
	if err != nil {
		return err
	}
	machine := s.Machine
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
	// The release is live and passed its check, so the server goes into ~/.tifa as setup puts it
	// there: a server set up from another machine, or before the file was there, gets its entry.
	// CI never reads the file.
	if os.Getenv(envGitHubActions) != "true" && machine != "" {
		rememberAndSay(machine, t.dest.raw, knownApp{Name: t.app, Path: t.root}, stdout, stderr)
	}
	sum.Cache = clearCache(ctx, t, stderr)
	printCache(stdout, sum.Cache)
	t.printVisitors(ctx, stdout, s)
	return nil
}

// portAnswers says whether the app answers this machine on port of host within a few seconds:
// GET healthPath over HTTP on 80 and over HTTPS on 443, with a status below 500 and no redirect
// followed. A connection alone proves nothing: a proxy on this machine, such as one that takes
// all traffic, accepts it and then fails. The certificate is not checked, since the question is
// whether the port is open. Tests replace it.
var portAnswers = func(ctx context.Context, host string, port int) bool {
	return askPort(ctx, pick(port == httpsPort, "https", "http"), host, port)
}

// askPort is portAnswers with the scheme given.
func askPort(ctx context.Context, scheme, host string, port int) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+net.JoinHostPort(host, strconv.Itoa(port))+healthPath, nil)
	if err != nil {
		return false
	}
	client := &http.Client{
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, ServerName: host}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

// serverAddress is the name or address this machine reaches the server at, as ssh resolves its
// host, for a URL of the app: an alias of ~/.ssh/config is no address. The host as given when ssh
// does not say.
func (t deployTarget) serverAddress(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, sshProgram, "-G", "--", t.host).Output()
	if err != nil {
		return t.dest.name
	}
	for _, line := range strings.Split(string(out), "\n") {
		if word, value, _ := strings.Cut(strings.TrimSpace(line), " "); strings.EqualFold(word, "hostname") {
			return value
		}
	}
	return t.dest.name
}

// printVisitors says, after the health check passed on the server, where visitors reach the app of
// s. An app that serves them itself, or that nginx serves, is also asked for from this machine, on
// each of its ports: one that does not answer is closed somewhere between, which may be the provider.
func (t deployTarget) printVisitors(ctx context.Context, w io.Writer, s serverState) {
	var site string
	var ports []int
	switch {
	case t.front == "cloudflare" && t.domain != "":
		fmt.Fprintf(w, "visitors: https://%s, through the Cloudflare Tunnel\n", t.domain)
		return
	case t.front == "nginx" && !slices.Contains(s.NginxSites, t.app):
		fmt.Fprintf(w, "visitors: none yet. %s listens on 127.0.0.1:%d, which only the server reaches: run go tool tifa deploy setup, and nginx serves it\n", t.app, s.Port)
		return
	case t.front == "nginx":
		ports = []int{httpPort}
		if t.own != "" {
			ports = nginxPorts
		}
	case !servesVisitors(s.Port):
		fmt.Fprintf(w, "visitors: none yet. %s listens on 127.0.0.1:%d, which only the server reaches: run go tool tifa deploy setup, and it serves visitors on port 80 itself\n", t.app, s.Port)
		return
	default:
		ports = []int{httpPort}
		if s.Port == httpsPort {
			ports = nginxPorts
		}
	}
	host := t.serverAddress(ctx)
	site = "http://" + host
	if strings.Contains(host, ":") {
		site = "http://[" + host + "]"
	}
	if s.HTTPS != "" {
		site, host = "https://"+s.HTTPS, s.HTTPS
	}
	if t.front == "nginx" && t.own != "" {
		site, host = "https://"+t.own, t.own
	}
	var closed []int
	for _, p := range ports {
		if !portAnswers(ctx, host, p) {
			closed = append(closed, p)
		}
	}
	many := len(ports) > 1
	if len(closed) == 0 {
		fmt.Fprintf(w, "visitors: %s (%s %s from this machine)\n", site, portList(ports), pick(many, "answer", "answers"))
		return
	}
	shut := portList(closed)
	line := fmt.Sprintf("visitors: %s, once %s %s open to them. %s did not answer from this machine within 3 seconds: the firewall or a security group of your provider may block %s. Open %s (tcp) there, which Tifa does not change",
		site, portList(ports), pick(many, "are", "is"), strings.ToUpper(shut[:1])+shut[1:], pick(len(closed) > 1, "them", "it"), shut)
	if s.UFW == "on" {
		// ufw may block them too, when it was turned on after setup or its plan was not applied.
		line += ". ufw is on too: go tool tifa deploy setup lets visitors through it"
	}
	fmt.Fprintln(w, line)
}

// pick is a when cond holds, else b.
func pick(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// deployRollback runs the release named by release, or the newest earlier good one when it is empty.
func deployRollback(ctx context.Context, t deployTarget, release string, stdout, stderr io.Writer) (err error) {
	maskHost(stdout, t)
	started := time.Now()
	sum := deploySummary{App: t.app, Action: "rollback"}
	health := &healthCapture{w: stdout}
	defer func() { finishSummary(&sum, started, health, err, stderr) }()
	s, err := t.state(ctx, stderr)
	if err != nil {
		return err
	}
	var target deployRecord
	if release == "" {
		target, err = rollbackTarget(s)
	} else {
		var current bool
		if target, current, err = rollbackTo(s, release); err == nil && current {
			sum.Release = release
			fmt.Fprintf(stdout, "%s on %s already runs release %s: nothing to change\n", t.app, t.host, release)
			return nil
		}
	}
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
	sum.Cache = clearCache(ctx, t, stderr)
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
