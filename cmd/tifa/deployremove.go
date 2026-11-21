package main

// tifa deploy remove takes the app down and takes back what tifa made for it: on the server its
// units and its folder, in Cloudflare the route, DNS record and rules of its domain and the Access
// of the repository, in GitHub the key and the secrets. The tunnel and cloudflared serve every app
// of the server, so they go only with the last app. Like setup it reads everything first, prints a
// plan and asks, here for the name of the app, and it removes only what Tifa made and marked. The
// workflow file is a file of the repository, so it stays.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

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
for n in socket service; do
	if [ -f "$u/$unit.$n" ]; then units="$units $unit.$n"; fi
done
if [ -n "$units" ]; then
	# Stop both before disabling: a service whose socket is active gets a warning.
	systemctl --user stop $units
	systemctl --user disable --quiet $units
	rm -f "$u/$unit.socket" "$u/$unit.service"
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
	r := &setupRun{t: t, stdout: stdout, stderr: stderr}
	p := setupPlan{who: "remove"}
	// The plan reads each thing once. The steps run later with the context of the caller.
	ctx = withReadMemo(ctx)
	r.progress("the server")
	if err := r.refresh(ctx); err != nil {
		return p, err
	}
	st := r.st
	cf := false
	tunnelName := "tifa-" + st.Machine
	tunnelFound := false
	if t.domain != "" {
		r.progress("Cloudflare")
		if cf = r.useCF(ctx); !cf {
			p.cannot("cloudflare: the route, DNS record and rules of "+t.domain+", the Access of the repository, and the tunnel once no other app uses it",
				"needs cf logged in: cf auth login")
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
	app := t.app
	if st.AppDir || st.SocketSum != "" || st.ServiceSum != "" {
		text := fmt.Sprintf("server: stop and disable tifa-%[1]s.socket and tifa-%[1]s.service, delete their unit files and %[2]s/%[1]s with its releases, shared/.env, the data in shared/ and deploys.log", app, st.Home)
		p.add(opRemove, text, func(ctx context.Context) (string, error) {
			return "", t.ssh(ctx, deployUser, script(removeAppBody, "app", app), nil, io.Discard, r.stderr)
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
	if cf && len(st.Others) == 0 {
		if st.TunnelID != "" || st.TunnelActive || st.CloudflaredByTifa {
			text := "server: stop cloudflared, the service tifa-tunnel, and delete its units and /etc/tifa, as root"
			if st.CloudflaredByTifa {
				text = "server: stop cloudflared, the service tifa-tunnel, delete its units and /etc/tifa, and uninstall cloudflared, which setup installed, as root"
			}
			p.add(opRemove, text, afterAll(func(ctx context.Context) (string, error) {
				var out strings.Builder
				if err := t.ssh(ctx, "root", untunnelBody, nil, &out, r.stderr); err != nil {
					return "", err
				}
				return strings.TrimSpace(out.String()), nil
			}))
		}
		if tunnelFound {
			id := r.st.TunnelID
			p.add(opRemove, "cloudflare: the tunnel of the server, "+tunnelName+", which no other app uses", afterAll(func(ctx context.Context) (string, error) {
				return "", deleteTunnel(ctx, id)
			}))
		}
	}
	for i := range p.steps {
		p.steps[i].soft = true
	}
	return p, nil
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
		return nil
	}
	plan.print(stdout, fmt.Sprintf("Plan to remove %s from %s", t.app, t.host), false)
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
	return nil
}

func newDeployRemoveCmd(dir, host *string) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Take the app off the server, and take back what tifa made for it",
		Long: `remove stops the app and deletes its units and its folder on the server, with its releases,
its .env and its data. With deploy.front cloudflare it deletes the route, the DNS record and the
rules of its domain and the Access of the repository, and the tunnel and cloudflared once no other
app of the server uses them. With deploy.ci github it deletes the key of the repository on the
server and the secrets and the variable of the repository. It removes only what Tifa made and
marked, and keeps the workflow file, which is a file of your repository.

Like setup it prints a plan first. On a terminal it then asks you to type the name of the app.
Without a terminal, pass --yes. What it cannot remove, such as without a login of cf or gh, is
listed at the end, the rest is removed, and it exits with an error. Running it again is safe.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := loadDeployTarget(*dir, *host)
			if err != nil {
				return err
			}
			return deployRemove(cmd.Context(), t, yes, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "remove without a terminal to ask on (on a terminal the name of the app is always asked)")
	return cmd
}
