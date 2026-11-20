package main

// tifa deploy setup makes the server, Cloudflare and GitHub match what the deploy section of
// tifa.yaml asks for. It works out a plan first: every item is ok, add, change or remove, or says
// why it cannot be done. Then, once the developer agreed, it applies the plan and prints a line
// for each step. It can run again at any time.
//
// It removes only what Tifa made and marked: the authorized_keys lines with the comment
// "tifa-github <owner>/<repo>", Cloudflare objects whose names start with "tifa", the route and
// DNS record of a domain it added, the repository variable TIFA_DEPLOY and the secrets it set,
// and the workflow file that starts with the line of tifa.

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/term"
)

type planOp string

const (
	opOK     planOp = "ok"
	opAdd    planOp = "add"
	opChange planOp = "change"
	opRemove planOp = "remove"
	opCannot planOp = "cannot"
)

// planStep is one line of the plan.
type planStep struct {
	op   planOp
	text string
	why  string // for opCannot: what is missing
	// do applies the step and says what came of it. Nil for ok and cannot.
	do func(ctx context.Context) (string, error)
	// soft makes a failure a warning: setup goes on with the steps after it.
	soft bool
}

type setupPlan struct {
	steps []planStep
	notes []string
}

func (p *setupPlan) add(op planOp, text string, do func(context.Context) (string, error)) {
	p.steps = append(p.steps, planStep{op: op, text: text, do: do})
}
func (p *setupPlan) ok(text string) { p.steps = append(p.steps, planStep{op: opOK, text: text}) }
func (p *setupPlan) cannot(text, why string) {
	p.steps = append(p.steps, planStep{op: opCannot, text: text, why: why})
}

// todo counts the steps that change something.
func (p setupPlan) todo() (n int) {
	for _, s := range p.steps {
		if s.do != nil {
			n++
		}
	}
	return n
}

func (p setupPlan) print(w io.Writer, app, host string) {
	fmt.Fprintf(w, "Plan for %s on %s\n\n", app, host)
	for _, s := range p.steps {
		if s.op == opCannot {
			fmt.Fprintf(w, "  %-7s %s (%s)\n", s.op, s.text, s.why)
		} else {
			fmt.Fprintf(w, "  %-7s %s\n", s.op, s.text)
		}
	}
	for _, n := range p.notes {
		fmt.Fprintf(w, "\nNote: %s\n", n)
	}
	fmt.Fprintln(w)
}

// stdinIsTerminal tells whether the developer can answer a question. Tests replace it.
var stdinIsTerminal = func(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// confirm asks whether to apply the plan. Without --yes it needs a terminal.
func confirm(in io.Reader, out io.Writer, yes bool) error {
	if yes {
		return nil
	}
	if !stdinIsTerminal(in) {
		return errors.New("there is no terminal to ask on: read the plan above and run the command again with --yes to apply it")
	}
	fmt.Fprint(out, "Apply this plan? [y/N] ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
		return &exitError{code: 1, err: errors.New("nothing was changed")}
	}
	return nil
}

func (p setupPlan) apply(ctx context.Context, w io.Writer) error {
	for _, s := range p.steps {
		if s.do == nil {
			continue
		}
		res, err := s.do(ctx)
		switch {
		case err != nil && s.soft:
			fmt.Fprintf(w, "  warning %s: %v\n", s.text, err)
		case err != nil:
			fmt.Fprintf(w, "  failed  %s\n", s.text)
			return err
		case res != "":
			fmt.Fprintf(w, "  done    %s: %s\n", s.text, res)
		default:
			fmt.Fprintf(w, "  done    %s\n", s.text)
		}
	}
	return nil
}

// setupRun holds what the steps of one setup share.
type setupRun struct {
	t              deployTarget
	stdout, stderr io.Writer
	st             serverState
	haveServer     bool // tifa logs in to the server
	port           int  // the port of the app, picked when it has none yet
	cfChecked, cf  bool
	zone           cfZone // of deploy.domain
	tunnelID       string
	repo           githubRepo
	ghNames        map[string]bool
	ghVars         map[string]string
}

func (r *setupRun) refresh(ctx context.Context) error {
	st, err := r.t.state(ctx, r.stderr)
	if err != nil {
		return err
	}
	r.st, r.haveServer = st, true
	return nil
}

func (r *setupRun) appPort() (int, error) {
	if r.st.Port != 0 {
		return r.st.Port, nil
	}
	return pickPort(r.st.Used)
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// buildSetupPlan reads the server, Cloudflare and GitHub, and compares them with tifa.yaml. It
// changes nothing.
func buildSetupPlan(ctx context.Context, t deployTarget, stdout, stderr io.Writer) (setupPlan, *setupRun, error) {
	r := &setupRun{t: t, stdout: stdout, stderr: stderr}
	var p setupPlan
	if net.ParseIP(t.host) != nil {
		p.notes = append(p.notes, fmt.Sprintf("deploy.host is the address %s. tifa.yaml is committed, and a public repository then shows where your server is. Write an ssh alias instead, a Host of ~/.ssh/config, so the address stays on each machine.", t.host))
	}
	if err := r.serverSteps(ctx, &p); err != nil {
		return p, r, err
	}
	if err := r.appSteps(&p); err != nil {
		return p, r, err
	}
	if err := r.domainSteps(ctx, &p); err != nil {
		return p, r, err
	}
	if err := r.githubSteps(ctx, &p); err != nil {
		return p, r, err
	}
	return p, r, nil
}

// useCF says whether cf is installed and logged in. It asks once, and only when a step needs it.
func (r *setupRun) useCF(ctx context.Context) bool {
	if !r.cfChecked {
		r.cf, r.cfChecked = cfLoggedIn(ctx), true
	}
	return r.cf
}

func (r *setupRun) serverSteps(ctx context.Context, p *setupPlan) error {
	err := r.refresh(ctx)
	var login *sshLoginError
	switch {
	case err == nil:
	case errors.As(err, &login) && login.user == deployUser:
		var out strings.Builder
		if rerr := r.t.ssh(ctx, "root", setupProbeScript(), nil, &out, r.stderr); rerr != nil {
			return fmt.Errorf("cannot log in to %s as %s (%v) or as root: %w", r.t.host, deployUser, err, rerr)
		}
		s, perr := parseServerState(out.String())
		if perr != nil {
			return perr
		}
		if _, _, terr := serverTarget(s.OS, s.Arch); terr != nil {
			return terr
		}
		p.add(opAdd, fmt.Sprintf("server: the user %s (home %s) that logs in with the keys of root, with its systemd running at boot", deployUser, deployHome), r.serverApply)
		return nil
	default:
		return err
	}
	if _, _, terr := serverTarget(r.st.OS, r.st.Arch); terr != nil {
		return terr
	}
	if !r.st.Prepared() {
		p.add(opChange, fmt.Sprintf("server: start the systemd of %s at boot (linger), as root", deployUser), r.serverApply)
		return nil
	}
	p.ok(fmt.Sprintf("server: %s logs in and its systemd runs at boot", deployUser))
	return nil
}

func (r *setupRun) serverApply(ctx context.Context) (string, error) {
	var out strings.Builder
	if err := r.t.ssh(ctx, "root", serverScript(), nil, &out, r.stderr); err != nil {
		return "", err
	}
	if err := r.refresh(ctx); err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.ReplaceAll(out.String(), "\n", ", ")), nil
}

func (r *setupRun) appSteps(p *setupPlan) error {
	port, err := r.appPort()
	if err != nil {
		return err
	}
	r.port = port
	app := r.t.app
	desc := fmt.Sprintf("app: %s/%s with an empty shared/.env, port %d, the systemd socket and service tifa-%s", deployHome, app, port, app)
	apply := func(ctx context.Context) (string, error) {
		if !r.haveServer {
			if err := r.refresh(ctx); err != nil {
				return "", err
			}
		}
		if !r.st.Prepared() {
			return "", fmt.Errorf("the systemd of %s does not run on %s", deployUser, r.t.host)
		}
		port, err := r.appPort()
		if err != nil {
			return "", err
		}
		r.port = port
		if err := r.t.ssh(ctx, deployUser, appScript(app, port), nil, io.Discard, r.stderr); err != nil {
			return "", err
		}
		return "", r.refresh(ctx)
	}
	switch {
	case r.st.Port == 0:
		p.add(opAdd, desc, apply)
	case r.st.SocketSum != sha(socketUnit(app, port)) || r.st.ServiceSum != sha(serviceUnit(app)):
		p.add(opChange, fmt.Sprintf("app: write the systemd units of %s again", app), apply)
	default:
		p.ok(fmt.Sprintf("app: %s/%s, port %d, systemd units", deployHome, app, port))
	}
	return nil
}

func (r *setupRun) service() string { return "http://127.0.0.1:" + strconv.Itoa(r.port) }

func opOf(how string) planOp {
	switch how {
	case "already there":
		return opOK
	case "added", "created":
		return opAdd
	}
	return opChange
}

// domainSteps puts deploy.domain behind the Cloudflare Tunnel of the server, and takes away the
// domains an earlier setup put there that tifa.yaml no longer names.
func (r *setupRun) domainSteps(ctx context.Context, p *setupPlan) error {
	t := r.t
	if t.domain == "" {
		return r.removeOldDomains(ctx, p, "")
	}
	what := "domain: " + t.domain + " through the Cloudflare Tunnel of the server (route, DNS, cache rules)"
	if !r.useCF(ctx) {
		p.cannot(what, "needs cf logged in: cf auth login")
		return nil
	}
	if r.haveServer && r.st.Cloudflared == "" && !r.st.Apt && r.st.TunnelID == "" {
		p.cannot(what, "the server has neither cloudflared nor apt: install cloudflared from the packages of the system, https://pkg.cloudflare.com/")
		return nil
	}
	zone, err := findZone(ctx, t.domain)
	if err != nil {
		p.cannot(what, err.Error())
		return nil
	}
	r.zone = zone
	name := "tifa-" + r.st.Machine
	known := r.st.TunnelID
	id, how, err := ensureTunnel(ctx, known, name, false)
	if err != nil {
		return err
	}
	r.tunnelID = id
	tunnelText := "cloudflare: the tunnel of the server, " + name
	if how == "already there" {
		p.ok(tunnelText)
	} else {
		p.add(opAdd, tunnelText, func(ctx context.Context) (string, error) {
			id, _, err := ensureTunnel(ctx, r.st.TunnelID, "tifa-"+r.st.Machine, true)
			r.tunnelID = id
			return id, err
		})
	}
	serverOp := opAdd
	if r.st.Cloudflared != "" {
		serverOp = opChange
	}
	if id != "" && r.st.TunnelID == id && r.st.TunnelActive && r.st.Cloudflared != "" {
		p.ok("server: cloudflared runs the tunnel as the service tifa-tunnel")
	} else {
		p.add(serverOp, "server: install cloudflared when missing and run the tunnel as the service tifa-tunnel, as root", func(ctx context.Context) (string, error) {
			token, err := tunnelToken(ctx, r.tunnelID)
			if err != nil {
				return "", err
			}
			var out strings.Builder
			if err := r.t.ssh(ctx, "root", tunnelScript(r.tunnelID), strings.NewReader(token), &out, r.stderr); err != nil {
				return "", fmt.Errorf("running the tunnel on the server: %w", err)
			}
			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			return lines[len(lines)-1], nil
		})
	}
	routeText := fmt.Sprintf("cloudflare: route %s -> %s", t.domain, r.service())
	r.hostSteps(ctx, p, zone, t.domain, "route", routeText, "DNS "+t.domain+" CNAME the tunnel, proxied", r.service)
	for _, rs := range hostRules(t.domain) {
		how, err := ensureRules(ctx, zone.Name, rs, false)
		if err != nil {
			return err
		}
		text := fmt.Sprintf("cloudflare: %s for %s", rs.what, t.domain)
		if op := opOf(how); op == opOK {
			p.ok(text)
		} else {
			p.add(op, text, func(ctx context.Context) (string, error) {
				_, err := ensureRules(ctx, zone.Name, rs, true)
				return "", err
			})
		}
	}
	return r.removeOldDomains(ctx, p, t.domain)
}

// hostSteps adds the route and the DNS record of host to the tunnel.
func (r *setupRun) hostSteps(ctx context.Context, p *setupPlan, zone cfZone, host, _, routeText, dnsText string, service func() string) {
	check := func(ensure func(apply bool) (string, error)) planOp {
		if r.tunnelID == "" {
			return opAdd
		}
		how, err := ensure(false)
		if err != nil {
			return opChange
		}
		return opOf(how)
	}
	routeOp := check(func(apply bool) (string, error) { return ensureRoute(ctx, r.tunnelID, host, service(), apply) })
	dnsOp := check(func(apply bool) (string, error) {
		return ensureDNS(ctx, zone.Name, host, r.tunnelID+".cfargotunnel.com", r.t.app, apply)
	})
	if routeOp == opOK {
		p.ok(routeText)
	} else {
		p.add(routeOp, routeText, func(ctx context.Context) (string, error) {
			_, err := ensureRoute(ctx, r.tunnelID, host, service(), true)
			return "", err
		})
	}
	if dnsOp == opOK {
		p.ok("cloudflare: " + dnsText)
	} else {
		p.add(dnsOp, "cloudflare: "+dnsText, func(ctx context.Context) (string, error) {
			_, err := ensureDNS(ctx, zone.Name, host, r.tunnelID+".cfargotunnel.com", r.t.app, true)
			return "", err
		})
	}
}

// removeOldDomains finds the routes of the tunnel that lead to this app and are not keep, and
// plans their removal with their DNS record and rules.
func (r *setupRun) removeOldDomains(ctx context.Context, p *setupPlan, keep string) error {
	id := r.st.TunnelID
	if !r.haveServer || id == "" || r.st.Port == 0 || !r.useCF(ctx) {
		return nil
	}
	_, ingress, err := tunnelIngress(ctx, id)
	if err != nil {
		return err
	}
	for _, rule := range ingress {
		host, _ := rule["hostname"].(string)
		if host == "" || host == keep || rule["service"] != "http://127.0.0.1:"+strconv.Itoa(r.st.Port) {
			continue
		}
		p.add(opRemove, fmt.Sprintf("domain: %s, its route, DNS record and rules, which tifa made", host), func(ctx context.Context) (string, error) {
			zone, err := findZone(ctx, host)
			if err != nil {
				return "", err
			}
			if _, err := removeRules(ctx, zone.Name, host, true); err != nil {
				return "", err
			}
			if err := removeDNS(ctx, zone.Name, host, id+".cfargotunnel.com"); err != nil {
				return "", err
			}
			return "", removeRoute(ctx, id, host)
		})
	}
	return nil
}

// ghSecret sets a secret of the repository. The value goes to gh on its input.
func (r *setupRun) ghSecret(ctx context.Context, name, value string) error {
	_, err := runGH(ctx, r.t.root, strings.NewReader(value), "secret", "set", name, "--repo", r.repo.Name)
	return err
}

func (r *setupRun) ghLists(ctx context.Context) error {
	out, err := runGH(ctx, r.t.root, nil, "secret", "list", "--repo", r.repo.Name, "--json", "name")
	if err != nil {
		return err
	}
	var names []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &names); err != nil {
		return fmt.Errorf("gh secret list printed what tifa cannot read: %w", err)
	}
	r.ghNames = map[string]bool{}
	for _, n := range names {
		r.ghNames[n.Name] = true
	}
	out, err = runGH(ctx, r.t.root, nil, "variable", "list", "--repo", r.repo.Name, "--json", "name,value")
	if err != nil {
		return err
	}
	var vars []struct{ Name, Value string }
	if err := json.Unmarshal(out, &vars); err != nil {
		return fmt.Errorf("gh variable list printed what tifa cannot read: %w", err)
	}
	r.ghVars = map[string]string{}
	for _, v := range vars {
		r.ghVars[v.Name] = v.Value
	}
	return nil
}

func hasMarker(path string) (text string, marked bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(data), strings.HasPrefix(string(data), workflowMarker), nil
}

func (r *setupRun) githubSteps(ctx context.Context, p *setupPlan) error {
	t := r.t
	workflow := filepath.Join(t.root, filepath.FromSlash(githubWorkflowPath))
	have, marked, err := hasMarker(workflow)
	if err != nil {
		return err
	}
	// gh is asked only when there may be something of GitHub: it is wanted, Tifa wrote the
	// workflow, or a key of a repository is on the server.
	if !t.github && !marked && len(r.st.GitHubKeys) == 0 {
		return nil
	}
	repo, repoErr := findGitHubRepo(ctx, t.root)
	if repoErr == nil {
		r.repo = repo
		if err := r.ghLists(ctx); err != nil {
			return err
		}
	}
	if !t.github {
		return r.githubRemovals(ctx, p, workflow, marked, repoErr)
	}
	if t.sshMode == connCloudflare && t.domain == "" {
		p.cannot("github: deploys from GitHub Actions", "deploy.ssh is cloudflare, and the ssh host name is made from deploy.domain: set it")
		return nil
	}
	if repoErr != nil {
		p.cannot("github: deploys from GitHub Actions", repoErr.Error())
		return nil
	}
	if r.haveServer && len(r.st.HostKeys) == 0 {
		p.cannot("github: deploys from GitHub Actions", "the server has no /etc/ssh/ssh_host_*_key.pub file, so tifa cannot tell CI which server to trust")
		return nil
	}
	var resolved sshResolved
	if t.sshMode != connCloudflare {
		if resolved, err = resolveSSH(ctx, t.host); err != nil {
			p.cannot("github: deploys from GitHub Actions", err.Error())
			return nil
		}
	}
	connect, port := resolved.Host, resolved.Port
	if t.sshMode == connCloudflare {
		connect, port = "", 22
	}
	if t.sshMode == connCloudflare {
		if !r.useCF(ctx) {
			p.cannot("cloudflare: ssh host name and Access for CI", "needs cf logged in: cf auth login")
		} else if err := r.accessSteps(ctx, p); err != nil {
			return err
		}
		if r.zone.ID != "" {
			connect = cloudflareSSHHost(t.domain, r.zone.Name)
		}
	}
	repoName := repo.Name

	// The key of the repository.
	keyOnServer := slices.Contains(r.st.GitHubKeys, repoName)
	switch {
	case keyOnServer && r.ghNames[secretSSHKey]:
		p.ok(fmt.Sprintf("github: the key of %s may log in as %s", repoName, deployUser))
	default:
		op := opAdd
		if keyOnServer || r.ghNames[secretSSHKey] {
			op = opChange
		}
		p.add(op, fmt.Sprintf("github: a key for %s that may log in as %s, kept as the secret %s", repoName, deployUser, secretSSHKey), func(ctx context.Context) (string, error) {
			key, err := newDeployKey(repoName)
			if err != nil {
				return "", err
			}
			if err := t.ssh(ctx, deployUser, authorizeScript(repoName), strings.NewReader(key.authorizedLine(repoName)+"\n"), io.Discard, r.stderr); err != nil {
				return "", err
			}
			return "", r.ghSecret(ctx, secretSSHKey, key.Private)
		})
	}

	// The connection: where CI logs in, and the host keys it trusts.
	if r.haveServer && len(r.st.HostKeys) > 0 || !r.haveServer {
		hostSecret := func(ctx context.Context) (string, string, error) {
			if len(r.st.HostKeys) == 0 {
				return "", "", errors.New("the server has no ssh host keys tifa can read")
			}
			c := connect
			if t.sshMode == connCloudflare {
				c = cloudflareSSHHost(t.domain, r.zone.Name)
			}
			hosts := strings.Join(knownHostsLines(c, port, r.st.HostKeys), "\n")
			return c, hosts, nil
		}
		fp := sha(strings.Join(append([]string{t.sshMode, connect, strconv.Itoa(port)}, r.st.HostKeys...), "|"))
		current := r.ghNames[secretKnownHosts] && r.ghNames[secretHost] && r.ghVars[varMarker] == fp && r.haveServer
		text := fmt.Sprintf("github: where CI logs in (%s) and the host keys it trusts, the secrets %s and %s", t.sshMode, secretHost, secretKnownHosts)
		if current {
			p.ok(text)
		} else {
			op := opAdd
			if r.ghNames[secretHost] || r.ghNames[secretKnownHosts] {
				op = opChange
			}
			p.add(op, text, func(ctx context.Context) (string, error) {
				c, hosts, err := hostSecret(ctx)
				if err != nil {
					return "", err
				}
				for _, s := range []secret{{secretKnownHosts, hosts}, {secretHost, c}} {
					if err := r.ghSecret(ctx, s.name, s.value); err != nil {
						return "", err
					}
				}
				fp := sha(strings.Join(append([]string{t.sshMode, c, strconv.Itoa(port)}, r.st.HostKeys...), "|"))
				_, err = runGH(ctx, t.root, strings.NewReader(fp), "variable", "set", varMarker, "--repo", repoName)
				return "", err
			})
		}
	}
	if t.sshMode == connTailscale {
		if r.ghNames[secretTSID] && r.ghNames[secretTSSecret] {
			p.ok("github: the Tailscale secrets " + secretTSID + " and " + secretTSSecret)
		} else {
			p.cannot("github: the Tailscale secrets "+secretTSID+" and "+secretTSSecret,
				"Tifa cannot make an OAuth client: in Tailscale, Settings, OAuth clients, make one with the auth_keys scope for the tag tag:ci, which your tailnet policy must own and let reach port 22 of the server, then run gh secret set for both")
		}
	}

	// The token that clears the CDN cache, and the workflow.
	zoneID := ""
	if t.domain != "" {
		if err := r.purgeSteps(ctx, p); err != nil {
			return err
		}
		zoneID = r.zone.ID
	}
	spec, err := workflowSpec(t, repo, t.sshMode, resolved, zoneID)
	if err != nil {
		return err
	}
	text := githubWorkflow(spec)
	switch {
	case have == "":
		p.add(opAdd, "github: "+githubWorkflowPath+", which you commit and push", r.writeWorkflow(workflow, text))
	case !marked:
		p.ok("github: " + githubWorkflowPath + " has no Tifa line at the top, so it is yours and stays")
	case have == text:
		p.ok("github: " + githubWorkflowPath)
	default:
		p.add(opChange, "github: "+githubWorkflowPath+", which you commit and push", r.writeWorkflow(workflow, text))
	}
	if t.domain == "" && (r.ghNames[secretPurge]) {
		return r.removePurge(ctx, p)
	}
	return r.removeAccessIfUnwanted(ctx, p)
}

func (r *setupRun) writeWorkflow(path, text string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		return "", os.WriteFile(path, []byte(text), 0o644)
	}
}

// accessSteps lets the repository reach the ssh of the server through the tunnel: a route and a
// DNS record for the ssh host name, and an Access application that admits the service token.
func (r *setupRun) accessSteps(ctx context.Context, p *setupPlan) error {
	t := r.t
	zone, err := findZone(ctx, t.domain)
	if err != nil {
		p.cannot("cloudflare: ssh host name and Access for CI", err.Error())
		return nil
	}
	r.zone = zone
	host := cloudflareSSHHost(t.domain, zone.Name)
	r.hostSteps(ctx, p, zone, host, "ssh", fmt.Sprintf("cloudflare: route %s -> %s", host, sshService),
		"DNS "+host+" CNAME the tunnel, proxied", func() string { return sshService })
	name := accessName(r.repo.Name)
	ids, err := serviceTokenIDs(ctx, name)
	if err != nil {
		return err
	}
	text := fmt.Sprintf("cloudflare: the Access application of %s admits the service token %s, kept as %s and %s", host, name, secretCFID, secretCFSecret)
	if len(ids) == 1 && r.ghNames[secretCFID] && r.ghNames[secretCFSecret] {
		if _, how, err := ensureTokenPolicy(ctx, name, ids[0], false); err == nil && how == "already there" {
			var policyID string
			if pol, _ := policyByName(ctx, name); pol != nil {
				policyID = pol.ID
			}
			if how, err := ensureSSHApp(ctx, host, policyID, false); err == nil && how == "already there" {
				p.ok(text)
				return nil
			}
		}
	}
	op := opAdd
	if len(ids) > 0 {
		op = opChange
	}
	p.add(op, text, func(ctx context.Context) (string, error) {
		clientID, clientSecret, tokenID, how, err := renewServiceToken(ctx, name)
		if err != nil {
			return "", err
		}
		policyID, _, err := ensureTokenPolicy(ctx, name, tokenID, true)
		if err != nil {
			return "", err
		}
		if _, err := ensureSSHApp(ctx, host, policyID, true); err != nil {
			return "", err
		}
		if err := r.ghSecret(ctx, secretCFID, clientID); err != nil {
			return "", err
		}
		return how, r.ghSecret(ctx, secretCFSecret, clientSecret)
	})
	return nil
}

// purgeSteps makes the token CI clears the cache of deploy.domain with.
func (r *setupRun) purgeSteps(ctx context.Context, p *setupPlan) error {
	t := r.t
	text := fmt.Sprintf("cloudflare: a token that can only clear the cache of the zone of %s, kept as %s", t.domain, secretPurge)
	if !r.useCF(ctx) {
		if r.ghNames[secretPurge] {
			p.ok("github: the secret " + secretPurge)
			return nil
		}
		p.cannot(text, "needs cf logged in: cf auth login. By hand: "+purgeTokenManual("of "+t.domain, t.domain))
		return nil
	}
	if r.zone.ID == "" {
		zone, err := findZone(ctx, t.domain)
		if err != nil {
			p.cannot(text, err.Error())
			return nil
		}
		r.zone = zone
	}
	want := purgeTokenName(r.repo.Name, r.zone.Name)
	tokens, err := purgeTokens(ctx, r.repo.Name)
	if err != nil {
		p.cannot(text, err.Error()+". By hand: "+purgeTokenManual(r.zone.Name, t.domain))
		return nil
	}
	found := false
	for _, tok := range tokens {
		if tok.Name == want {
			found = true
		}
	}
	if found && r.ghNames[secretPurge] {
		p.ok(text)
	} else {
		op := opAdd
		if found || r.ghNames[secretPurge] {
			op = opChange
		}
		p.steps = append(p.steps, planStep{op: op, text: text, soft: true, do: func(ctx context.Context) (string, error) {
			value, err := createPurgeToken(ctx, want, r.zone)
			if err != nil {
				return "", fmt.Errorf("%w. Do it by hand: %s", err, purgeTokenManual(r.zone.Name, t.domain))
			}
			return "", r.ghSecret(ctx, secretPurge, value)
		}})
	}
	for _, tok := range tokens {
		if tok.Name != want {
			p.add(opRemove, "cloudflare: the token "+tok.Name+", for another zone", func(ctx context.Context) (string, error) {
				_, err := runCF(ctx, "user", "tokens", "delete", tok.ID, "--force")
				return "", err
			})
		}
	}
	return nil
}

// removePurge takes away the purge secret and the tokens of the repository.
func (r *setupRun) removePurge(ctx context.Context, p *setupPlan) error {
	if r.ghVars[varMarker] == "" {
		return nil
	}
	var tokens []cfToken
	if r.useCF(ctx) {
		var err error
		if tokens, err = purgeTokens(ctx, r.repo.Name); err != nil {
			// The same login could not have made one, but another could have: say what to look for.
			p.cannot("cloudflare: the tokens tifa made to clear the cache for "+r.repo.Name,
				err.Error()+". By hand: in the Cloudflare dashboard, My Profile, API Tokens, delete the tokens whose name starts with "+purgeTokenPrefix(r.repo.Name))
		}
	}
	if r.ghNames[secretPurge] {
		p.add(opRemove, "github: the secret "+secretPurge+", which tifa set", func(ctx context.Context) (string, error) {
			_, err := runGH(ctx, r.t.root, nil, "secret", "delete", secretPurge, "--repo", r.repo.Name)
			return "", err
		})
	}
	for _, tok := range tokens {
		p.add(opRemove, "cloudflare: the token "+tok.Name, func(ctx context.Context) (string, error) {
			_, err := runCF(ctx, "user", "tokens", "delete", tok.ID, "--force")
			return "", err
		})
	}
	return nil
}

// removeAccessIfUnwanted takes away the Cloudflare Access of the repository when deploy.ssh is
// not cloudflare any more.
func (r *setupRun) removeAccessIfUnwanted(ctx context.Context, p *setupPlan) error {
	if r.t.sshMode == connCloudflare || r.ghVars[varMarker] == "" {
		return nil
	}
	return r.removeAccess(ctx, p)
}

func (r *setupRun) removeAccess(ctx context.Context, p *setupPlan) error {
	if r.useCF(ctx) {
		found, err := removeSSHAccess(ctx, r.repo.Name, r.st.TunnelID, false, nil)
		if err != nil {
			return err
		}
		if found {
			p.add(opRemove, "cloudflare: the Access service token, policy and application of "+r.repo.Name+", and the route and DNS record of the ssh host name once no repository uses it, which tifa made", func(ctx context.Context) (string, error) {
				_, err := removeSSHAccess(ctx, r.repo.Name, r.st.TunnelID, true, func(f string, a ...any) { fmt.Fprintf(r.stdout, "          "+f+"\n", a...) })
				return "", err
			})
		}
	}
	var names []string
	for _, n := range []string{secretCFID, secretCFSecret} {
		if r.ghNames[n] {
			names = append(names, n)
		}
	}
	if len(names) > 0 {
		p.add(opRemove, "github: the secrets "+strings.Join(names, ", ")+", which tifa set", func(ctx context.Context) (string, error) {
			for _, n := range names {
				if _, err := runGH(ctx, r.t.root, nil, "secret", "delete", n, "--repo", r.repo.Name); err != nil {
					return "", err
				}
			}
			return "", nil
		})
	}
	return nil
}

// githubRemovals plans what to take away when deploy.github is false.
func (r *setupRun) githubRemovals(ctx context.Context, p *setupPlan, workflow string, marked bool, repoErr error) error {
	if marked {
		p.add(opRemove, "github: "+githubWorkflowPath+", which tifa wrote", func(context.Context) (string, error) {
			return "", os.Remove(workflow)
		})
	}
	if repoErr != nil {
		if marked {
			p.cannot("github: the key and secrets of the repository", repoErr.Error())
		}
		return nil
	}
	repoName := r.repo.Name
	if slices.Contains(r.st.GitHubKeys, repoName) {
		p.add(opRemove, fmt.Sprintf("server: the key of %s in authorized_keys of %s", repoName, deployUser), func(ctx context.Context) (string, error) {
			return "", r.t.ssh(ctx, deployUser, revokeScript(repoName), nil, io.Discard, r.stderr)
		})
	}
	if r.ghVars[varMarker] == "" {
		return nil
	}
	if err := r.removePurge(ctx, p); err != nil {
		return err
	}
	if err := r.removeAccess(ctx, p); err != nil {
		return err
	}
	var names []string
	for _, n := range []string{secretSSHKey, secretKnownHosts, secretHost} {
		if r.ghNames[n] {
			names = append(names, n)
		}
	}
	p.add(opRemove, "github: the secrets "+strings.Join(names, ", ")+" and the variable "+varMarker+", which tifa set", func(ctx context.Context) (string, error) {
		for _, n := range names {
			if _, err := runGH(ctx, r.t.root, nil, "secret", "delete", n, "--repo", repoName); err != nil {
				return "", err
			}
		}
		_, err := runGH(ctx, r.t.root, nil, "variable", "delete", varMarker, "--repo", repoName)
		return "", err
	})
	return nil
}

// deploySetup plans, asks and applies.
func deploySetup(ctx context.Context, t deployTarget, yes bool, in io.Reader, stdout, stderr io.Writer) error {
	plan, _, err := buildSetupPlan(ctx, t, stdout, stderr)
	if err != nil {
		return err
	}
	plan.print(stdout, t.app, t.host)
	cannot := 0
	for _, s := range plan.steps {
		if s.op == opCannot {
			cannot++
		}
	}
	if plan.todo() == 0 {
		fmt.Fprintln(stdout, "Nothing to change.")
	} else {
		if err := confirm(in, stdout, yes); err != nil {
			return err
		}
		if err := plan.apply(ctx, stdout); err != nil {
			return err
		}
	}
	if cannot > 0 {
		fmt.Fprintf(stdout, "\n%d item(s) could not be done, see above. Fix what they need and run setup again.\n", cannot)
	}
	return nil
}
