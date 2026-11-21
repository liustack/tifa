package main

// tifa deploy setup makes the server, Cloudflare and GitHub match what the deploy section of
// tifa.yaml asks for. It works out a plan first: every item is ok, add, change or remove, or says
// why it cannot be done. Then, once the developer agreed, it applies the plan and prints a line
// for each step. It can run again at any time.
//
// It removes only what Tifa made and marked: the authorized_keys lines with the comment
// "tifa-github <owner>/<repo>", Cloudflare objects whose names start with "tifa", the route and
// DNS record of a domain it added, the repository variable TIFA_DEPLOY_FINGERPRINT and the secrets it set,
// and the workflow file that starts with the line of tifa.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

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
	// done is the text of the step once it ran, when it knows more than the plan did: a name
	// that is made only when the step runs shows as a placeholder in text.
	done func() string
}

// planNote is something the developer may want to do, though setup works without it.
type planNote struct{ what, why string }

type setupPlan struct {
	steps []planStep
	notes []planNote
	// who is the command that makes the plan, setup when empty.
	who string
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

// count counts the steps with the given op.
func (p setupPlan) count(op planOp) (n int) {
	for _, s := range p.steps {
		if s.op == op {
			n++
		}
	}
	return n
}

// areaNames shows the word before the first ":" of a step in the form the developer knows.
var areaNames = map[string]string{"cloudflare": "Cloudflare", "github": "GitHub"}

// alreadyRight counts the ok steps by area, in the order the areas first show.
func (p setupPlan) alreadyRight() string {
	var areas []string
	counts := map[string]int{}
	for _, s := range p.steps {
		if s.op != opOK {
			continue
		}
		area, _, _ := strings.Cut(s.text, ":")
		if name, ok := areaNames[area]; ok {
			area = name
		}
		if counts[area] == 0 {
			areas = append(areas, area)
		}
		counts[area]++
	}
	parts := make([]string, len(areas))
	for i, a := range areas {
		parts[i] = fmt.Sprintf("%s %d", a, counts[a])
	}
	return strings.Join(parts, ", ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// print shows the title, what will change, what is already right and what the developer has to
// do. With all it lists the ok lines instead of counting them.
func (p setupPlan) print(w io.Writer, title string, all bool) {
	fmt.Fprintln(w, title)
	first := true
	for _, s := range p.steps {
		if s.op == opCannot || (s.op == opOK && !all) {
			continue
		}
		if first {
			fmt.Fprintln(w)
			first = false
		}
		fmt.Fprintf(w, "  %-7s %s\n", s.op, s.text)
	}
	if !all && p.count(opOK) > 0 {
		fmt.Fprintf(w, "\nAlready right: %s (--all lists them)\n", p.alreadyRight())
	}
	cannot := p.count(opCannot)
	if n := cannot + len(p.notes); n > 0 {
		who := p.who
		if who == "" {
			who = "setup"
		}
		fmt.Fprintf(w, "\nYou need to do %d %s that %s cannot:\n", n, plural(n, "thing", "things"), who)
		i := 0
		for _, s := range p.steps {
			if s.op == opCannot {
				i++
				printItem(w, i, s.text, s.why)
			}
		}
		for _, n := range p.notes {
			i++
			printItem(w, i, n.what+" (optional)", n.why)
		}
	}
	fmt.Fprintln(w)
}

// printItem writes one numbered item: what on its line, and the reason below it, a sentence to a
// line.
func printItem(w io.Writer, i int, what, why string) {
	fmt.Fprintf(w, "  %d. %s\n", i, what)
	for _, line := range strings.SplitAfter(why, ". ") {
		fmt.Fprintf(w, "     %s\n", strings.TrimSpace(line))
	}
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
	if f, ok := in.(*os.File); ok {
		flushInput(f)
	}
	fmt.Fprint(out, "Apply this plan? [y/N] ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
		return &exitError{code: 1, err: errors.New("nothing was changed")}
	}
	return nil
}

// skipped is what a step returns when the developer chose to leave an optional thing out. It is
// not a failure, so it is no warning either.
type skipped struct{ why string }

func (s skipped) Error() string { return s.why }

// apply runs the steps and says how many soft ones failed.
func (p setupPlan) apply(ctx context.Context, w io.Writer) (warnings int, err error) {
	for _, s := range p.steps {
		if s.do == nil {
			continue
		}
		res, err := s.do(ctx)
		if err == nil && s.done != nil {
			s.text = s.done()
		}
		switch {
		case errors.As(err, new(skipped)):
			fmt.Fprintf(w, "  skipped %s: %v\n", s.text, err)
		case err != nil && s.soft:
			fmt.Fprintf(w, "  warning %s: %v\n", s.text, err)
			warnings++
		case err != nil:
			fmt.Fprintf(w, "  failed  %s\n", s.text)
			return warnings, err
		case res != "":
			fmt.Fprintf(w, "  done    %s: %s\n", s.text, res)
		default:
			fmt.Fprintf(w, "  done    %s\n", s.text)
		}
	}
	return warnings, nil
}

// setupRun holds what the steps of one setup share.
type setupRun struct {
	t              deployTarget
	stdout, stderr io.Writer
	st             serverState
	haveServer     bool // tifa logs in to the server
	port           int  // the port of the app, picked when it has none yet
	cfChecked, cf  bool
	zone           cfZone // of the domain
	in             io.Reader
	tunnelID       string
	repo           githubRepo
	gh             githubAPI
	ghNames        map[string]bool
	ghVars         map[string]string
	// sshName is the ssh host name of the domain for deploy.ssh cloudflare, once chosen.
	sshName string
	// afterConnection are steps that wait until CI has what replaces what they take away.
	afterConnection []planStep
	setupFlags
}

func (r *setupRun) refresh(ctx context.Context) error {
	st, err := r.readState(ctx)
	if err != nil {
		return err
	}
	r.st, r.haveServer = st, true
	return nil
}

// readState asks the server about the app. Inside a plan it asks once.
//
// What ssh says on stderr is kept while the login may fail: a server without the user tifa
// answers Permission denied, which the plan puts in its own words. Any other answer, and the
// words of a login that failed for a reason the plan cannot name, go to stderr after all.
func (r *setupRun) readState(ctx context.Context) (serverState, error) {
	return memoRead(ctx, "ssh-state", func() (serverState, error) {
		var said bytes.Buffer
		st, err := r.t.state(ctx, &said)
		var login *sshLoginError
		if errors.As(err, &login) && login.user == deployUser {
			login.said = strings.TrimSpace(said.String())
		} else {
			r.stderr.Write(said.Bytes())
		}
		return st, err
	})
}

// progress says which part of the plan is being read. Reading takes seconds, and a terminal that
// stays silent looks hung.
func (r *setupRun) progress(what string) { fmt.Fprintf(r.stderr, "Reading %s...\n", what) }

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
func buildSetupPlan(ctx context.Context, t deployTarget, f setupFlags, in io.Reader, stdout, stderr io.Writer) (setupPlan, *setupRun, error) {
	// The reads run on several goroutines, and each one may write to stderr.
	r := &setupRun{t: t, setupFlags: f, in: in, stdout: stdout, stderr: &lockedWriter{w: stderr}}
	// The plan reads each thing once. The steps it returns run later, with the context of the
	// caller, which has no memo, so they see what they changed.
	ctx, cancel := context.WithCancel(withReadMemo(ctx))
	var reads sync.WaitGroup
	defer func() {
		cancel()
		reads.Wait()
	}()
	r.prefetch(ctx, &reads)
	var p setupPlan
	r.progress("the server")
	if err := r.serverSteps(ctx, &p); err != nil {
		return p, r, err
	}
	if err := r.appSteps(&p); err != nil {
		return p, r, err
	}
	if t.domain != "" {
		r.progress("Cloudflare")
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
			return fmt.Errorf("cannot log in to %s as %s (%v: %s) or as root: %w", r.t.host, deployUser, err, login.said, rerr)
		}
		s, perr := parseServerState(out.String())
		if perr != nil {
			return perr
		}
		if _, _, terr := serverTarget(s.OS, s.Arch); terr != nil {
			return terr
		}
		p.add(opAdd, fmt.Sprintf("server: the user %s does not exist yet (ssh cannot log in as it), so setup creates it (home %s) with the keys of root, and starts its systemd at boot", deployUser, deployHome), r.serverApply)
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

// domainSteps puts the domain of site.url behind the Cloudflare Tunnel of the server, and takes away the
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
		// The name has the machine id of the server, which setup reads once the user tifa is there.
		var done func() string
		if r.st.Machine == "" {
			tunnelText = "cloudflare: the tunnel of the server, tifa-<machine id>"
			done = func() string { return "cloudflare: the tunnel of the server, tifa-" + r.st.Machine }
		}
		p.steps = append(p.steps, planStep{op: opAdd, text: tunnelText, done: done, do: func(ctx context.Context) (string, error) {
			id, _, err := ensureTunnel(ctx, r.st.TunnelID, "tifa-"+r.st.Machine, true)
			r.tunnelID = id
			return id, err
		}})
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
	r.hostSteps(ctx, p, zone, t.domain, false, routeText, "DNS "+t.domain+" CNAME the tunnel, proxied", r.service)
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

// hostSteps adds the route and the DNS record of host to the tunnel. fresh says the name was just
// made, so nothing of it can be there yet and there is nothing to read.
func (r *setupRun) hostSteps(ctx context.Context, p *setupPlan, zone cfZone, host string, fresh bool, routeText, dnsText string, service func() string) {
	check := func(ensure func(apply bool) (string, error)) planOp {
		if r.tunnelID == "" || fresh {
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
		p.add(opRemove, domainRemoval(host), func(ctx context.Context) (string, error) {
			return "", removeDomain(ctx, id, host)
		})
	}
	return nil
}

// domainRemoval is the line of the plan that removes what setup made for host.
func domainRemoval(host string) string {
	return fmt.Sprintf("domain: %s, its route, DNS record and rules, which tifa made", host)
}

// ghSecret sets a secret of the repository.
func (r *setupRun) ghSecret(ctx context.Context, name, value string) error {
	return r.gh.SetSecret(ctx, r.repo.Name, name, value)
}

func (r *setupRun) ghLists(ctx context.Context) error {
	names, err := r.gh.SecretNames(ctx, r.repo.Name)
	if err != nil {
		return err
	}
	vars, err := r.gh.Variables(ctx, r.repo.Name)
	if err != nil {
		return err
	}
	r.ghNames, r.ghVars = names, vars
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
	r.progress("GitHub")
	api, repo, repoErr := findGitHub(ctx, t.root)
	if repoErr == nil {
		r.gh, r.repo = api, repo
		if err := r.ghLists(ctx); err != nil {
			return err
		}
	}
	if !t.github {
		return r.githubRemovals(ctx, p, workflow, marked, repoErr)
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
	// connect is the text of the secret TIFA_DEPLOY_HOST: with a direct login the whole target, port
	// included, so the workflow needs no Port line and shows no address. port goes with the host
	// name into the host keys.
	connect, hostName, port := "", "", 22
	if t.sshMode != connCloudflare {
		connect, hostName, port = deployHostURL(resolved.Host, resolved.Port), resolved.Host, resolved.Port
	}
	if t.sshMode == connCloudflare {
		if !r.useCF(ctx) {
			p.cannot("cloudflare: ssh host name and Access for CI", "needs cf logged in: cf auth login")
		} else if err := r.accessSteps(ctx, p); err != nil {
			return err
		}
		if r.sshName != "" {
			connect, hostName = r.sshName, r.sshName
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

	// The connection: where CI logs in, the host keys it trusts, and the zone of the domain.
	// All of it is a secret: a repository that is public must not show where the server is. The
	// zone ID is kept with them and not by itself: the one marker below says they are current.
	if r.haveServer && len(r.st.HostKeys) > 0 || !r.haveServer {
		if t.domain != "" && r.zone.ID == "" && r.useCF(ctx) {
			// A zone that cf could not find is already a plan line of its own.
			if zone, err := findZone(ctx, t.domain); err == nil {
				r.zone = zone
			}
		}
		zoneID := r.zone.ID
		secrets := func(ctx context.Context) ([]secret, error) {
			if len(r.st.HostKeys) == 0 {
				return nil, errors.New("the server has no ssh host keys tifa can read")
			}
			hosts := strings.Join(knownHostsLines(hostName, port, r.st.HostKeys), "\n")
			out := []secret{{secretKnownHosts, hosts}, {secretHost, connect}}
			if zoneID != "" {
				out = append(out, secret{secretZoneID, zoneID})
			}
			return out, nil
		}
		fingerprint := func() string {
			return sha(strings.Join(append([]string{t.sshMode, connect, strconv.Itoa(port), zoneID}, r.st.HostKeys...), "|"))
		}
		names := []string{secretHost, secretKnownHosts}
		if zoneID != "" {
			names = append(names, secretZoneID)
		}
		current := r.ghVars[varMarker] == fingerprint() && r.haveServer
		for _, n := range names {
			current = current && r.ghNames[n]
		}
		what := strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
		text := fmt.Sprintf("github: where CI logs in (%s) and the host keys it trusts, the secrets %s", t.sshMode, what)
		if current {
			p.ok(text)
		} else {
			op := opAdd
			if r.ghNames[secretHost] || r.ghNames[secretKnownHosts] {
				op = opChange
			}
			p.add(op, text, func(ctx context.Context) (string, error) {
				all, err := secrets(ctx)
				if err != nil {
					return "", err
				}
				for _, s := range all {
					if err := r.ghSecret(ctx, s.name, s.value); err != nil {
						return "", err
					}
				}
				return "", r.gh.SetVariable(ctx, repoName, varMarker, fingerprint())
			})
		}
	}
	if t.sshMode == connTailscale {
		r.tailscaleSteps(p)
	}

	// The token that clears the CDN cache, and the workflow.
	purge := false
	if t.domain != "" {
		if err := r.purgeSteps(ctx, p); err != nil {
			return err
		}
		purge = r.zone.ID != ""
	}
	spec, err := workflowSpec(t, repo, t.sshMode, purge)
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
	p.steps = append(p.steps, r.afterConnection...)
	r.removeOldNames(p)
	if t.domain == "" {
		if err := r.removePurge(ctx, p); err != nil {
			return err
		}
		r.removeZoneSecret(p)
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
	// The name is read back from what an earlier run made, and made new only when there is none.
	// It is chosen once and kept, so the steps below and the secrets use the same one.
	found, err := findSSHHosts(ctx, r.tunnelID, t.domain, zone.Name)
	if err != nil {
		return err
	}
	host := found.host
	if host == "" {
		if host, err = newSSHHost(t.domain, zone.Name); err != nil {
			p.cannot("cloudflare: ssh host name and Access for CI", err.Error())
			return nil
		}
	}
	r.sshName = host
	if found.host == "" {
		// The random part is drawn again by every run, so a plan shows that it is new and the line
		// of the step, once it ran, shows the name it made.
		prefix, suffix := sshHostParts(t.domain, zone.Name)
		shown := prefix + "<random>" + suffix
		steps, after := len(p.steps), len(r.afterConnection)
		defer func() {
			for _, later := range [][]planStep{p.steps[steps:], r.afterConnection[after:]} {
				for i := range later {
					text := later[i].text
					later[i].text = strings.ReplaceAll(text, host, shown)
					later[i].done = func() string { return text }
				}
			}
		}()
	}
	if found.legacy {
		legacy := legacySSHHost(t.domain, zone.Name)
		r.afterConnection = append(r.afterConnection, planStep{op: opRemove,
			text: fmt.Sprintf("cloudflare: the ssh host name %s that anyone can guess from the domain, its route, DNS record and Access application, which tifa made. %s replaces it", legacy, host),
			do: func(ctx context.Context) (string, error) {
				return "", removeSSHHost(ctx, r.tunnelID, zone.Name, legacy)
			}})
	}
	r.hostSteps(ctx, p, zone, host, found.host == "", fmt.Sprintf("cloudflare: route %s -> %s", host, sshService),
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

// purgeSteps plans the token CI clears the cache of the domain with. The token is optional, so
// setup asks for it on the first CI setup only, before the marker variable exists, and later
// notes the command that adds it. With --cache-purge-token it asks every time and replaces the secret.
// GitHub does not show a secret, so one that is there is taken as right.
func (r *setupRun) purgeSteps(ctx context.Context, p *setupPlan) error {
	t := r.t
	text := fmt.Sprintf("cloudflare: a token that can only clear the cache of %s, pasted by you and kept as %s", t.domain, secretPurge)
	have := r.ghNames[secretPurge]
	switch {
	case have && !r.purgeToken:
		p.ok("github: the secret " + secretPurge + ". To replace the token: " + purgeTokenCommand)
		return nil
	case !r.purgeToken && (r.ghVars[varMarker] != "" || r.ghVars[oldVarMarker] != "" && !r.hadOldPurgeToken()):
		p.notes = append(p.notes, planNote{what: text, why: purgeTokenLater})
		return nil
	}
	// left says why setup cannot ask now: a note on its own, a thing that needs you when the
	// developer asked with --cache-purge-token.
	left := func(why string) {
		if r.purgeToken {
			p.cannot(text, why)
			return
		}
		p.notes = append(p.notes, planNote{what: text, why: why + ". " + purgeTokenLater})
	}
	if r.zone.ID == "" && r.useCF(ctx) {
		zone, err := findZone(ctx, t.domain)
		if err != nil {
			left(err.Error())
			return nil
		}
		r.zone = zone
	}
	switch {
	case r.zone.ID == "":
		left("Setup needs cf logged in to find the zone: cf auth login")
	case !stdinIsTerminal(r.in):
		left("There is no terminal to paste on")
	default:
		op := opAdd
		if have {
			op = opChange
		}
		p.steps = append(p.steps, planStep{op: op, text: text, soft: true, do: r.pastePurgeToken})
	}
	return nil
}

// purgeTokenCommand makes setup ask for the token that clears the CDN cache.
const purgeTokenCommand = "tifa deploy setup --cache-purge-token"

// purgeTokenLater says what going without the token costs and how to add it.
const purgeTokenLater = "Without it a deploy still works, but " + noTokenCost + ". To add it: " + purgeTokenCommand

// purgeTokenTries is how many tokens setup takes before it gives up.
const purgeTokenTries = 3

// pastePurgeToken shows the link, reads the token hidden, checks it against the zone and sets the
// secret. A token that fails the check is asked for again. An empty line skips.
func (r *setupRun) pastePurgeToken(ctx context.Context) (string, error) {
	t := r.t
	say := func(f string, a ...any) { fmt.Fprintf(r.stdout, "          "+f+"\n", a...) }
	say("Open this link, set Zone Resources to %s only, create the token, and paste it here, or press Enter to skip:", r.zoneText())
	say("%s", purgeTokenLink(r.repo.Name))
	for try := 1; try <= purgeTokenTries; try++ {
		token, err := promptSecret(r.in, r.stdout, secretPurge)
		if err != nil {
			return "", fmt.Errorf("reading the token: %w. To try again: %s", err, purgeTokenCommand)
		}
		token = strings.TrimSpace(token)
		if token == "" {
			return "", skipped{purgeTokenLater}
		}
		err = checkPurgeToken(ctx, token, r.zone.ID, t.domain)
		if err == nil {
			return "", r.ghSecret(ctx, secretPurge, token)
		}
		why := strings.ReplaceAll(err.Error(), "\n", " ")
		if try == purgeTokenTries {
			return "", fmt.Errorf("the token was refused: %s. To try again: %s", why, purgeTokenCommand)
		}
		say("the token was refused: %s. Paste it again, or press Enter to skip.", why)
	}
	return "", nil
}

// The names setup used before every name had the prefix TIFA_DEPLOY_. The old variable marked the
// secrets as tifa's, so a secret under an old name goes only while that variable is there.
const oldVarMarker = "TIFA_DEPLOY"

var oldSecretNames = []string{
	"DEPLOY_SSH_KEY", "DEPLOY_KNOWN_HOSTS", "DEPLOY_HOST", "CF_ZONE_ID",
	"CF_CACHE_PURGE_TOKEN", "CF_PURGE_TOKEN",
	"CF_ACCESS_CLIENT_ID", "CF_ACCESS_CLIENT_SECRET", "TS_OAUTH_CLIENT_ID", "TS_OAUTH_SECRET",
}

// marked says tifa set secrets in the repository, under the new names or the old ones.
func (r *setupRun) marked() bool { return r.ghVars[varMarker] != "" || r.ghVars[oldVarMarker] != "" }

// hadOldPurgeToken says the repository has a purge token under an old name, which cannot be read
// back, so it has to be pasted again.
func (r *setupRun) hadOldPurgeToken() bool {
	return r.ghVars[oldVarMarker] != "" && (r.ghNames["CF_CACHE_PURGE_TOKEN"] || r.ghNames["CF_PURGE_TOKEN"])
}

// removeOldNames plans the removal of the secrets and the variable under the old names, which tifa
// set. The steps that set the new ones come before it in the plan. The Cloudflare token behind an
// old purge secret is the developer's: when CI is switched off, the plan says to delete it.
func (r *setupRun) removeOldNames(p *setupPlan) {
	if r.ghVars[oldVarMarker] == "" {
		return
	}
	var names []string
	for _, n := range oldSecretNames {
		if r.ghNames[n] {
			names = append(names, n)
		}
	}
	text := "github: the variable " + oldVarMarker + ", which tifa set under its old name"
	if len(names) > 0 {
		text = "github: the old secrets " + strings.Join(names, ", ") + " and the variable " + oldVarMarker + ", which tifa set under their old names"
	}
	if r.hadOldPurgeToken() && !r.t.github {
		text += fmt.Sprintf(". Tifa cannot delete the Cloudflare token behind the purge secret: delete the token %q in the Cloudflare dashboard, Manage Account, Account API Tokens", purgeTokenName(r.repo.Name))
	}
	repo := r.repo.Name
	p.add(opRemove, text, func(ctx context.Context) (string, error) {
		for _, n := range names {
			if err := r.gh.DeleteSecret(ctx, repo, n); err != nil {
				return "", err
			}
		}
		return "", r.gh.DeleteVariable(ctx, repo, oldVarMarker)
	})
}

// removePurge takes away the purge secret. The token behind it is the developer's: tifa cannot
// delete it, so the plan says to.
func (r *setupRun) removePurge(ctx context.Context, p *setupPlan) error {
	if r.ghVars[varMarker] == "" || !r.ghNames[secretPurge] {
		return nil
	}
	p.add(opRemove, fmt.Sprintf("github: the secret %s, which tifa set. Tifa cannot delete the Cloudflare token behind it: delete the token %q in the Cloudflare dashboard, Manage Account, Account API Tokens", secretPurge, purgeTokenName(r.repo.Name)), func(ctx context.Context) (string, error) {
		return "", r.gh.DeleteSecret(ctx, r.repo.Name, secretPurge)
	})
	return nil
}

// removeZoneSecret takes away the secret that holds the zone ID, which tifa set. The zone is
// nothing to delete in Cloudflare.
func (r *setupRun) removeZoneSecret(p *setupPlan) {
	if r.ghVars[varMarker] == "" || !r.ghNames[secretZoneID] {
		return
	}
	p.add(opRemove, "github: the secret "+secretZoneID+", which tifa set", func(ctx context.Context) (string, error) {
		return "", r.gh.DeleteSecret(ctx, r.repo.Name, secretZoneID)
	})
}

// removeAccessIfUnwanted takes away the Cloudflare Access of the repository when deploy.ssh is
// not cloudflare any more.
func (r *setupRun) removeAccessIfUnwanted(ctx context.Context, p *setupPlan) error {
	if r.t.sshMode == connCloudflare || !r.marked() {
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
				if err := r.gh.DeleteSecret(ctx, r.repo.Name, n); err != nil {
					return "", err
				}
			}
			return "", nil
		})
	}
	return nil
}

// githubRemovals plans what to take away when deploy.ci is none.
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
	if !r.marked() {
		return nil
	}
	r.removeOldNames(p)
	if err := r.removePurge(ctx, p); err != nil {
		return err
	}
	if err := r.removeAccess(ctx, p); err != nil {
		return err
	}
	if r.ghVars[varMarker] == "" {
		return nil
	}
	var names []string
	for _, n := range []string{secretSSHKey, secretKnownHosts, secretHost, secretZoneID, secretTSID, secretTSSecret} {
		if r.ghNames[n] {
			names = append(names, n)
		}
	}
	p.add(opRemove, "github: the secrets "+strings.Join(names, ", ")+" and the variable "+varMarker+", which tifa set", func(ctx context.Context) (string, error) {
		for _, n := range names {
			if err := r.gh.DeleteSecret(ctx, repoName, n); err != nil {
				return "", err
			}
		}
		return "", r.gh.DeleteVariable(ctx, repoName, varMarker)
	})
	return nil
}

// deploySetup plans, asks and applies. It fails when something needs the developer, after it did
// all it could.
func deploySetup(ctx context.Context, t deployTarget, f setupFlags, in io.Reader, stdout, stderr io.Writer) error {
	switch {
	case f.purgeToken && (t.domain == "" || !t.github):
		return errors.New("--cache-purge-token is for the token CI clears the CDN cache with, which needs deploy.front: cloudflare and deploy.ci: github in tifa.yaml")
	case f.tailscaleOAuth && (t.sshMode != connTailscale || !t.github):
		return errors.New("--tailscale-oauth is for the OAuth client CI joins your tailnet with, which needs deploy.ssh: tailscale and deploy.ci: github in tifa.yaml")
	case (f.purgeToken || f.tailscaleOAuth) && !stdinIsTerminal(in):
		return errors.New("--cache-purge-token and --tailscale-oauth need a terminal to paste on")
	}
	plan, _, err := buildSetupPlan(ctx, t, f, in, stdout, stderr)
	if err != nil {
		return err
	}
	cannot := plan.count(opCannot)
	if plan.todo() == 0 && cannot == 0 && len(plan.notes) == 0 && !f.all {
		fmt.Fprintf(stdout, "%s on %s matches tifa.yaml: %d things checked (--all lists them).\n", t.app, t.host, plan.count(opOK))
		return nil
	}
	plan.print(stdout, fmt.Sprintf("Plan for %s on %s", t.app, t.host), f.all)
	warnings := 0
	if plan.todo() > 0 {
		if err := confirm(in, stdout, f.yes); err != nil {
			return err
		}
		if warnings, err = plan.apply(ctx, stdout); err != nil {
			return err
		}
	}
	if n := cannot + warnings; n > 0 {
		return fmt.Errorf("%d %s, see above", n, plural(n, "thing needs you", "things need you"))
	}
	return nil
}

// zoneText names the zone of the domain for the steps a developer follows: its name once cf found
// it, else what to look for.
func (r *setupRun) zoneText() string {
	if r.zone.Name != "" {
		return "the zone " + r.zone.Name
	}
	return "the zone of " + r.t.domain
}

// setupFlags are the flags of tifa deploy setup.
type setupFlags struct {
	yes, all bool
	// purgeToken (--cache-purge-token) and tailscaleOAuth (--tailscale-oauth) ask for that
	// secret even when GitHub has it, and replace it.
	purgeToken, tailscaleOAuth bool
}
