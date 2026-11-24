package main

// tifa deploy remove takes the app down and takes back what tifa made for it: on the server its
// units and its folder, in Cloudflare the route, DNS record and rules of its domain and the Access
// of the repository, in GitHub the key and the secrets. The tunnel and cloudflared serve every app
// of the server, so they go only with the last app, and so does the file of purge tokens on the
// server. Like setup it reads everything first, prints a
// plan and asks, here for the name of the app, and it removes only what Tifa made and marked. The
// workflow file is a file of the repository, so it stays.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// removeAppBody runs as the deploy user. It stops and disables the units of the app, deletes
// them and the folder of the app, and leaves every other app as it is. It refuses while a deploy
// of the app runs.
const removeAppBody = `set -eu
d="$HOME/$app"
unit="tifa-$app"
u="$HOME/.config/systemd/user"
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
if [ -d "$d" ]; then
	exec 9>"$d/.lock"
	if ! flock -n 9; then echo "tifa: a deploy of $app runs on the server" >&2; exit 1; fi
fi
units=""
for n in socket -http.socket service; do
	case "$n" in -*) f="$unit$n" ;; *) f="$unit.$n" ;; esac
	if [ -f "$u/$f" ]; then units="$units $f"; fi
done
if [ -n "$units" ]; then
	# Stop them all before disabling: a service whose socket is active gets a warning.
	systemctl --user stop $units
	systemctl --user disable --quiet $units
	rm -f "$u/$unit.socket" "$u/$unit-http.socket" "$u/$unit.service"
	systemctl --user daemon-reload
fi
rm -rf "$d"
`

// untunnelBody runs as root once no app of tifa is left on the server. It stops cloudflared and
// deletes its units and /etc/tifa, which holds the ID and the token of the tunnel. When setup
// installed cloudflared, which it marks with its apt setting, it uninstalls it and deletes the
// package repository it added. /etc/tifa goes last: a run that stopped halfway finds the tunnel
// again.
const untunnelBody = `set -eu
s=/etc/systemd/system
for u in tifa-tunnel.service tifa-tunnel-upgrade.path; do
	if [ -f "$s/$u" ]; then systemctl disable --now --quiet "$u"; fi
done
rm -f "$s/tifa-tunnel.service" "$s/tifa-tunnel-upgrade.path" "$s/tifa-tunnel-upgrade.service"
systemctl daemon-reload
did="stopped tifa-tunnel"
if [ -f /etc/apt/apt.conf.d/52tifa-cloudflared ]; then
	export DEBIAN_FRONTEND=noninteractive NEEDRESTART_SUSPEND=1
	apt-get purge -y -q cloudflared </dev/null >/dev/null
	rm -f /etc/apt/sources.list.d/cloudflared.list /usr/share/keyrings/cloudflare-main.gpg /etc/apt/apt.conf.d/52tifa-cloudflared
	did="$did, uninstalled cloudflared"
fi
rm -rf /etc/tifa
echo "$did"
`

// buildRemovePlan reads the server, Cloudflare and GitHub, and plans to remove what tifa made for
// the app there. It changes nothing.
func buildRemovePlan(ctx context.Context, t deployTarget, stdout, stderr io.Writer) (setupPlan, error) {
	r := &setupRun{t: t, stdout: stdout, stderr: stderr, removing: true}
	p := setupPlan{who: "remove"}
	// The plan reads each thing once. The steps run later with the context of the caller.
	ctx = withReadMemo(ctx)
	r.progress("the server")
	if err := r.refresh(ctx); err != nil {
		return p, err
	}
	st := r.st
	p.machine = st.Machine
	cf := false
	tunnelName := "tifa-" + st.Machine
	tunnelFound := false
	if t.domain != "" {
		r.progress("Cloudflare")
		if cf = r.useCF(ctx); !cf {
			p.cannot("cloudflare: the route, DNS record and rules of "+t.domain+", the Access of the repository, and the tunnel once no other app uses it",
				cfNeed())
		} else {
			// The tunnel of the server, also when an earlier run took it off the server already.
			id, how, err := ensureTunnel(ctx, st.TunnelID, tunnelName, false)
			if err != nil {
				return p, err
			}
			if how == "already there" {
				r.st.TunnelID, tunnelFound = id, true
			}
			if err := r.removeDomainSteps(ctx, &p); err != nil {
				return p, err
			}
			if err := r.removeOldDomains(ctx, &p, t.domain); err != nil {
				return p, err
			}
		}
	}
	// The ssh host name of the domain, which the machines of the people reach the server by, goes
	// with the app, with or without CI. The steps that cut it wait until the server ran its own.
	viaTunnel := false
	if cf && t.domain != "" {
		if zone, err := findZone(ctx, t.domain); err == nil {
			viaTunnel = t.connectMode == connCloudflare && isSSHHost(t.dest.name, t.domain, zone.Name)
		}
	}
	if t.github {
		r.progress("GitHub")
		api, repo, err := findGitHub(ctx, t.root)
		if err != nil {
			p.cannot("github: the key, secrets and variable of the repository, which tifa set", err.Error())
		} else {
			r.gh, r.repo = api, repo
			if err := r.ghLists(ctx); err != nil {
				return p, err
			}
			// The workflow is a file of the repository: it stays, so it is not passed as marked.
			if err := r.githubRemovals(ctx, &p, "", false, nil); err != nil {
				return p, err
			}
		}
	}
	if cf && t.domain != "" && !r.accessRemovalPlanned {
		if err := r.removeAccess(ctx, &p); err != nil {
			return p, err
		}
	}
	app := t.app
	if st.AppDir || st.SocketSum != "" || st.ServiceSum != "" {
		units := fmt.Sprintf("tifa-%[1]s.socket and tifa-%[1]s.service", app)
		if st.HTTPSum != "" {
			units = fmt.Sprintf("tifa-%[1]s.socket, tifa-%[1]s-http.socket and tifa-%[1]s.service", app)
		}
		text := fmt.Sprintf("server: stop and disable %[3]s, delete their unit files and %[2]s/%[1]s with its releases, shared/.env, the data in shared/ and deploys.log", app, st.Home, units)
		if st.HTTPS != "" {
			text += ", and its certificates in certs/"
		}
		p.add(opRemove, text, func(ctx context.Context) (string, error) {
			return "", t.ssh(ctx, deployUser, script(removeAppBody, "app", app), nil, io.Discard, r.stderr)
		})
	}
	// The app that served visitors itself is the one that needed ports 80 and 443 open to the
	// systemd of the deploy user: what setup set for it goes with it.
	if servesVisitors(st.Port) && st.PortsConf {
		p.add(opRemove, fmt.Sprintf("server: %s, which let the systemd of %s open ports 80 and 443, and the value from before it, as root", portsConf, deployUser), func(ctx context.Context) (string, error) {
			var out strings.Builder
			if err := t.ssh(ctx, "root", unportsBody, nil, &out, r.stderr); err != nil {
				return "", err
			}
			return strings.TrimSpace(out.String()), nil
		})
	}
	// A step that fails does not stop the others: they do not depend on it, and the next run finds
	// what is left. Only the tunnel waits for them all, since what is left may lead to it.
	type step = func(context.Context) (string, error)
	failed := false
	note := func(do step) step {
		return func(ctx context.Context) (string, error) {
			res, err := do(ctx)
			failed = failed || err != nil
			return res, err
		}
	}
	for i, s := range p.steps {
		if s.do != nil {
			p.steps[i].do = note(s.do)
		}
	}
	afterAll := func(do step) step {
		return note(func(ctx context.Context) (string, error) {
			if failed {
				return "", errors.New("kept, since a step above failed: run go tool tifa deploy remove again")
			}
			return do(ctx)
		})
	}
	// Everything that needs ssh comes first, the purge tokens too: when the developer reaches the
	// server through the tunnel, the steps below take that way away.
	// The purge tokens serve every app of the server too, and go with the last one, whatever its
	// front: they are on the server, so no login of cf is needed.
	if len(st.Others) == 0 && st.PurgeFile {
		p.add(opRemove, fmt.Sprintf("server: delete %s, the tokens that clear the CDN cache, which no other app uses", path.Join(st.Home, purgeFile)), afterAll(func(ctx context.Context) (string, error) {
			return "", t.ssh(ctx, deployUser, purgeForgetBody, nil, io.Discard, r.stderr)
		}))
	}
	if cf && len(st.Others) == 0 {
		if st.TunnelID != "" || st.TunnelActive || st.CloudflaredByTifa {
			text := "server: stop cloudflared, the service tifa-tunnel, and delete its units and /etc/tifa, as root"
			if st.CloudflaredByTifa {
				text = "server: stop cloudflared, the service tifa-tunnel, delete its units and /etc/tifa, and uninstall cloudflared, which setup installed, as root"
			}
			p.add(opRemove, text, afterAll(func(ctx context.Context) (string, error) {
				var out strings.Builder
				body := untunnelBody
				if viaTunnel {
					// This ssh goes through the tunnel the script stops: it would cut its own
					// connection. The script runs on its own a few seconds later, after ssh left.
					body = detached(untunnelBody)
				}
				if err := t.ssh(ctx, "root", body, nil, &out, r.stderr); err != nil {
					return "", err
				}
				if viaTunnel {
					time.Sleep(untunnelWait)
				}
				return strings.TrimSpace(out.String()), nil
			}))
		}
	}
	// The ssh host name of the app goes after the last ssh, and only when the steps before it
	// worked: its route is the way in for a run that has to be run again.
	for _, s := range r.late {
		s.do = afterAll(s.do)
		p.steps = append(p.steps, s)
	}
	if cf && len(st.Others) == 0 && tunnelFound {
		id := r.st.TunnelID
		p.add(opRemove, "cloudflare: the tunnel of the server, "+tunnelName+", which no other app uses", afterAll(func(ctx context.Context) (string, error) {
			return "", deleteTunnel(ctx, id)
		}))
	}
	// The block of the ssh config goes last: the steps above may need the tunnel it leads to, and
	// a run that stopped halfway is run again through it. When the developer reaches the server
	// by the ssh host name and cf cannot take that name away, the block stays.
	if path, err := sshConfigPath(); err == nil && t.domain != "" {
		hosts, err := sshBlocksFor(path, app, t.domain)
		switch {
		case err != nil:
			p.cannot("this machine: the block of tifa "+app+" in "+path, err.Error())
		case len(hosts) > 0 && !cf && looksLikeSSHHost(t.dest.name, t.domain):
			p.notes = append(p.notes, planNote{what: "this machine: the block of tifa " + app + " in " + path + " stays",
				why: "You reach the server by " + t.dest.name + ", which setup cannot take away without cf, and ssh needs the block for it. Log in to cf and run remove again, which then removes the block"})
		case len(hosts) > 0:
			p.add(opRemove, "this machine: the block of tifa "+app+" in "+path+", which tifa wrote", afterAll(func(context.Context) (string, error) {
				_, err := removeSSHBlocks(path, app, t.domain)
				return "", err
			}))
		}
	}
	for i := range p.steps {
		p.steps[i].soft = true
	}
	return p, nil
}

// untunnelWait is how long remove waits after it scheduled the stop of the tunnel through the
// tunnel, so the connections are gone when it asks Cloudflare to delete it. Tests set it.
var untunnelWait = 8 * time.Second

// detached wraps a script so that it runs on the server on its own, after ssh has left.
func detached(body string) string {
	return "f=$(mktemp)\ncat > \"$f\" <<'TIFA_DETACHED_EOF'\n" + body + "TIFA_DETACHED_EOF\n" +
		"( sleep 3; sh \"$f\"; rm -f \"$f\" ) </dev/null >/dev/null 2>&1 &\n" +
		"echo \"scheduled: the tunnel stops in a few seconds\"\n"
}

// removeDomainSteps plans the removal of what setup made for the domain of the app. It finds them
// by the host name, so also once the app and its port are gone from the server.
func (r *setupRun) removeDomainSteps(ctx context.Context, p *setupPlan) error {
	host, id := r.t.domain, r.st.TunnelID
	zone, err := findZone(ctx, host)
	if err != nil {
		p.cannot(domainRemoval(host), err.Error())
		return nil
	}
	rules, err := removeRules(ctx, zone.Name, host, false)
	if err != nil {
		return err
	}
	records, route := 0, false
	if id != "" {
		if records, err = removeDNS(ctx, zone.Name, host, id+".cfargotunnel.com", false); err != nil {
			return err
		}
		_, ingress, err := tunnelIngress(ctx, id)
		if err != nil {
			return err
		}
		route = slices.ContainsFunc(ingress, func(rule map[string]any) bool {
			return rule["hostname"] == host && rule["path"] == nil && r.ownRoute(rule["service"])
		})
	}
	if rules+records > 0 || route {
		p.add(opRemove, domainRemoval(host), func(ctx context.Context) (string, error) {
			return "", removeDomain(ctx, id, host)
		})
	}
	return nil
}

// ownRoute says whether a route of the tunnel leads to the app: to its port, or, once the app is
// gone from the server, to a loopback port that no other app has and nothing listens on.
func (r *setupRun) ownRoute(service any) bool {
	s, _ := service.(string)
	p, ok := strings.CutPrefix(s, "http://127.0.0.1:")
	port, err := strconv.Atoi(p)
	if !ok || err != nil {
		return false
	}
	if r.st.Port != 0 {
		return port == r.st.Port
	}
	return !slices.Contains(r.st.Used, port)
}

// confirmName asks for the name of the app on a terminal. Without one, --yes is the answer.
func confirmName(in io.Reader, out io.Writer, app string, yes bool) error {
	if !stdinIsTerminal(in) {
		if yes {
			return nil
		}
		return errors.New("there is no terminal to ask on: read the plan above and run the command again with --yes to remove it")
	}
	if f, ok := in.(*os.File); ok {
		flushInput(f)
	}
	fmt.Fprintf(out, "This cannot be undone. To remove it, type the name of the app (%s): ", app)
	line, _ := bufio.NewReader(in).ReadString('\n')
	if strings.TrimSpace(line) != app {
		return &exitError{code: 1, err: errors.New("nothing was removed")}
	}
	return nil
}

// deployRemove plans, asks and removes. It fails when something is left for the developer, after
// it removed all it could.
func deployRemove(ctx context.Context, t deployTarget, yes bool, in io.Reader, stdout, stderr io.Writer) error {
	plan, err := buildRemovePlan(ctx, t, stdout, stderr)
	if err != nil {
		return err
	}
	cannot := plan.count(opCannot)
	if plan.todo() == 0 && cannot == 0 {
		places := []string{"on " + t.host}
		if t.domain != "" {
			places = append(places, "in Cloudflare")
		}
		if t.github {
			places = append(places, "in GitHub")
		}
		fmt.Fprintf(stdout, "Nothing of %s is left to remove %s.\n", t.app, strings.Join(places, ", "))
		forgetApp(plan.machine, t.root, stderr)
		return nil
	}
	plan.print(stdout, fmt.Sprintf("Plan to remove %s from %s%s", t.app, t.host, t.dest.fromHomeNote()), false)
	warnings := 0
	if plan.todo() > 0 {
		if err := confirmName(in, stdout, t.app, yes); err != nil {
			return err
		}
		if warnings, err = plan.apply(ctx, stdout); err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(t.root, filepath.FromSlash(githubWorkflowPath))); err == nil {
			fmt.Fprintf(stdout, "Kept %s, a file of your repository: the next push fails to deploy until you delete it.\n", githubWorkflowPath)
		}
	}
	if n := cannot + warnings; n > 0 {
		return fmt.Errorf("%d %s, see above", n, plural(n, "thing needs you", "things need you"))
	}
	// Only a clean finish forgets the app: after a warning something of it may still be on the
	// server, and a retry without --host must find the same server.
	forgetApp(plan.machine, t.root, stderr)
	return nil
}

// forgetApp takes the app off the entry of the server in ~/.tifa, once the server no longer has
// it. The entry goes with its last app. A file that cannot be written is a warning: remove did
// what it was asked to.
func forgetApp(machine, path string, stderr io.Writer) {
	if machine == "" {
		return
	}
	if err := forgetServerApp(machine, path); err != nil {
		fmt.Fprintf(stderr, "Could not update %s: %v\n", shownServersPath(), err)
	}
}

func newDeployRemoveCmd(dir, host *string) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Take the app off the server, and take back what tifa made for it",
		Long: `remove stops the app and deletes its units and its folder on the server, with its releases,
its .env and its data. With deploy.front cloudflare it deletes the route, the DNS record and the
rules of its domain and the Access of the repository, and the tunnel, cloudflared and the cache
tokens on the server once no other app of the server uses them. With deploy.auto it deletes the key of the repository on the
server and the secrets and the variable of the repository. It removes only what Tifa made and
marked, and keeps the workflow file, which is a file of your repository.

Like setup it prints a plan first. On a terminal it then asks you to type the name of the app.
Without a terminal, pass --yes. What it cannot remove, such as without a login of cf or gh, is
listed at the end, the rest is removed, and it exits with an error. Running it again is safe.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := loadDeployTarget(*dir, *host, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			return deployRemove(cmd.Context(), t, yes, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "remove without a terminal to ask on (on a terminal the name of the app is always asked)")
	return cmd
}
