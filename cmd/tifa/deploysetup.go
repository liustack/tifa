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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
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
	// then says what setup does once the steps ran, when it plans again after them.
	then string
	// firewall says which ports visitors need open, for an app that serves them itself.
	firewall string
	// who is the command that makes the plan, setup when empty.
	who string
	// machine is the ID of the server the plan read, which ~/.tifa knows it by. Remove sets it.
	machine string
	// rest are the other servers of an app on several, which keep it when remove takes it off one.
	rest []groupServer
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
	if p.then != "" {
		fmt.Fprintf(w, "\n%s\n", p.then)
	}
	if p.firewall != "" && p.todo() > 0 {
		fmt.Fprintf(w, "\n%s\n", p.firewall)
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

// stdoutIsTerminal tells whether the output goes to a terminal. Tests replace it.
var stdoutIsTerminal = func(w io.Writer) bool {
	f, ok := w.(*os.File)
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
	if a := strings.ToLower(strings.TrimSpace(readLine(in))); a != "y" && a != "yes" {
		return &exitError{code: 1, err: errors.New("nothing was changed")}
	}
	return nil
}

// readLine reads one line of an answer a byte at a time, so it takes nothing after the line from
// in: setup may ask again.
func readLine(in io.Reader) string {
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := in.Read(b)
		if n == 1 {
			if b[0] == '\n' {
				break
			}
			line = append(line, b[0])
		}
		if err != nil {
			break
		}
	}
	return string(line)
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
	t deployTarget
	// branch is the branch that deploys from GitHub Actions: deploy.auto.branch, else the default
	// branch of the repository. Set once the repository is known.
	branch         string
	stdout, stderr io.Writer
	st             serverState
	haveServer     bool // tifa logs in to the server
	port           int  // the port of the app, picked when it has none yet
	cfChecked, cf  bool
	zone           cfZone // of the domain
	in             io.Reader
	// errOut is the stderr of setup itself, which a program run on the terminal gets.
	errOut   io.Writer
	tunnelID string
	repo     githubRepo
	gh       githubAPI
	ghNames  map[string]bool
	ghVars   map[string]string
	// sshName is the ssh host name of the domain for deploy.connect cloudflare, once chosen.
	sshName string
	// removing says this is tifa deploy remove, not setup. Its steps in Cloudflare that cut the
	// ssh through the tunnel wait in late until the steps on the server ran.
	removing bool
	late     []planStep
	// cloudflaredAt is where setup installed cloudflared on this machine, which the PATH of this
	// process may not find.
	cloudflaredAt string
	// tunnelHost is the ssh host name of the tunnel once the check that ssh reaches the server
	// through it passed: the host ~/.tifa keeps then.
	tunnelHost string
	// hostAdvice is what setup tells the developer to set when TIFA_DEPLOY_HOST comes from a
	// place it does not change, once the ssh host name exists.
	hostAdvice string
	// accessRemovalPlanned says the plan already takes the Access of the ssh host name away.
	accessRemovalPlanned bool
	// afterConnection are steps that wait until CI has what replaces what they take away.
	afterConnection []planStep
	// domainNew says the plan adds the route of the domain: its first setup. purgeOnServer says
	// the server has the purge token of the zone. purgeVerified says that token works: Cloudflare
	// took it for the zone.
	domainNew, purgeOnServer, purgeVerified bool
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

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// buildSetupPlan reads the server, Cloudflare and GitHub, and compares them with tifa.yaml. It
// changes nothing. With tools, a plan that installs or logs in cf or gh has only those steps: the
// rest is read with them.
func buildSetupPlan(ctx context.Context, t deployTarget, f setupFlags, tools bool, in io.Reader, stdout, stderr io.Writer) (setupPlan, *setupRun, error) {
	// The reads run on several goroutines, and each one may write to stderr.
	r := &setupRun{t: t, setupFlags: f, in: in, stdout: stdout, stderr: &lockedWriter{w: stderr}, errOut: stderr}
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
	if tools {
		if r.toolSteps(ctx, &p); p.todo() > 0 {
			return p, r, nil
		}
	}
	// The round of GitHub Actions for an app on several servers, once each server is set up.
	if g := f.group; g != nil && g.github {
		r.progress("the server")
		if err := r.refresh(ctx); err != nil {
			return p, r, err
		}
		return p, r, r.githubSteps(ctx, &p)
	}
	// A server that is named by its ssh host is reached through the tunnel: this machine needs
	// cloudflared, its place in the ssh config and its Access policy first, also once cf and gh
	// were made ready by the round before.
	if !f.skipMachine {
		stop, err := r.machineFirst(ctx, &p)
		if err != nil {
			return p, r, err
		}
		if stop || p.todo() > 0 {
			return p, r, nil
		}
	}
	r.progress("the server")
	if err := r.serverSteps(ctx, &p); err != nil {
		return p, r, err
	}
	// What an earlier nginx front left goes first: nginx may hold the ports the app needs now.
	if t.front != "nginx" {
		if err := r.nginxGoneSteps(ctx, &p); err != nil {
			return p, r, err
		}
	}
	if err := r.appSteps(ctx, &p); err != nil {
		return p, r, err
	}
	if t.front == "nginx" {
		r.progress("nginx")
		if err := r.nginxSteps(ctx, &p); err != nil {
			return p, r, err
		}
	}
	if t.domain != "" {
		r.progress("Cloudflare")
	}
	if err := r.domainSteps(ctx, &p); err != nil {
		return p, r, err
	}
	// With CI, the GitHub part plans it: it reads the secrets the token may move from.
	if t.domain != "" && !t.github {
		r.purgeSteps(ctx, &p)
	}
	if err := r.githubSteps(ctx, &p); err != nil {
		return p, r, err
	}
	// With CI, the GitHub part plans the ssh host name too: it needs the repository for the
	// token. Without CI the ssh host name is for the machines of the people alone.
	switch {
	case t.connectMode == connCloudflare && !t.github:
		if !r.useCF(ctx) {
			p.cannot(sshAccessWhat, cfNeed())
		} else if err := r.accessSteps(ctx, &p); err != nil {
			return p, r, err
		}
	case t.connectMode != connCloudflare && t.domain != "":
		if !r.accessRemovalPlanned && r.useCF(ctx) {
			if err := r.removeAccess(ctx, &p); err != nil {
				return p, r, err
			}
		}
		// What this machine holds for the ssh host name goes with it, unless the developer still
		// reaches the server by that name: removeAccess said what to do then.
		if path, err := sshConfigPath(); err == nil && !looksLikeSSHHost(t.dest.name, t.domain) {
			if hosts, err := sshBlocksFor(path, t.app, t.domain); err != nil {
				p.cannot("this machine: the block of tifa "+t.app+" in "+path, err.Error())
			} else if len(hosts) > 0 {
				p.add(opRemove, "this machine: the block of tifa "+t.app+" in "+path+", which tifa wrote, since deploy.connect is not cloudflare any more", func(context.Context) (string, error) {
					_, err := removeSSHBlocks(path, t.app, t.domain)
					return "", err
				})
			}
		}
	}
	return p, r, nil
}

// sshAccessWhat names the ssh host name and its Access application in the plan, when it cannot do them.
const sshAccessWhat = "cloudflare: the ssh host name and its Access application"

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
	var noSudo *noSudoError
	switch {
	case err == nil:
		// An admin user that cannot run sudo stops setup here, before a step of root fails
		// halfway through the plan. A server without the user tifa finds it in its probe below.
		if r.t.dest.sudo() {
			if serr := r.t.ssh(ctx, "root", sudoCheckBody, nil, io.Discard, r.stderr); serr != nil {
				return serr
			}
		}
	case errors.As(err, &login) && login.user == deployUser:
		var out strings.Builder
		if rerr := r.t.ssh(ctx, "root", setupProbeScript(), nil, &out, r.stderr); rerr != nil {
			if errors.As(rerr, &noSudo) {
				return rerr
			}
			hint := ""
			if r.t.dest.user == "" && errors.As(rerr, new(*sshLoginError)) {
				example := "ubuntu@" + r.t.dest.raw
				if r.t.dest.url {
					example = "ssh://ubuntu@" + strings.TrimPrefix(r.t.dest.raw, "ssh://")
				}
				hint = fmt.Sprintf(". When root cannot log in to this server, put the user you log in with in front of the host, such as %s, a user with passwordless sudo", example)
			}
			return fmt.Errorf("cannot log in to %s as %s (%v: %s) or as %s: %w%s", r.t.host, deployUser, err, login.said, r.t.rootLogin(), rerr, hint)
		}
		s, perr := parseServerState(out.String())
		if perr != nil {
			return perr
		}
		if _, _, terr := serverTarget(s.OS, s.Arch); terr != nil {
			return terr
		}
		if merr := r.t.checkMachine(s.Machine); merr != nil {
			return merr
		}
		// What the app needs to know of a server without the user: who listens on the ports of
		// the visitors, and whether its user may open them.
		r.st.LowPort, r.st.Listen, r.st.Used, r.st.UFW = s.LowPort, s.Listen, s.Used, s.UFW
		p.add(opAdd, fmt.Sprintf("server: the user %s does not exist yet (ssh cannot log in as it), so setup creates it (home %s) with the keys of %s, and starts its systemd at boot", deployUser, deployHome, r.t.dest.admin()), r.serverApply)
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
	if err := r.t.ssh(ctx, "root", serverScript(r.t.dest.admin()), nil, &out, r.stderr); err != nil {
		return "", err
	}
	if err := r.refresh(ctx); err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.ReplaceAll(out.String(), "\n", ", ")), nil
}

// appSteps plans the folder, port and units of the app. With deploy.front none the app serves
// visitors itself on port 80, and with a domain on 443 too: setup stops before it changes anything
// when something else listens there, and lets the systemd of the deploy user open them, as root.
// It plans ufw for those ports before the app moves, see firewallSteps.
func (r *setupRun) appSteps(ctx context.Context, p *setupPlan) error {
	t := r.t
	port, err := t.visitorPort(r.st)
	if err != nil {
		return err
	}
	r.port = port
	if err := t.portsFree(r.st, port, func() []listener { return r.rootListeners(ctx) }, true); err != nil {
		return err
	}
	app, domain := t.app, t.ownFor(port)
	serves := portsText(port)
	switch {
	case t.open:
		serves = fmt.Sprintf("port %d on every address of the server, for the load balancer or proxy in front", port)
		p.firewall = fmt.Sprintf("The load balancer or proxy in front needs port %d of the server. Let only it reach port %[1]d, in the firewall or security groups of your provider (or ufw): a port open to everyone lets visitors past it. Tifa opens no port for it.", port)
		if r.st.UFW == "on" {
			p.notes = append(p.notes, planNote{
				what: fmt.Sprintf("firewall: let the load balancer or proxy reach port %d through ufw", port),
				why:  fmt.Sprintf("ufw is on, and blocks port %d unless a rule allows it. Run sudo ufw allow from <addresses of the load balancer or proxy> to any port %[1]d proto tcp on the server", port),
			})
		}
	case port == httpsPort && r.cert != nil:
		serves = fmt.Sprintf("serving visitors itself on ports 80 and 443, with HTTPS for %s and your certificate in %s/%s/%s", domain, deployHome, app, tlsDir)
	case port == httpsPort:
		serves = fmt.Sprintf("serving visitors itself on ports 80 and 443, with HTTPS for %s and its certificate from Let's Encrypt kept in %s/%s/certs", domain, deployHome, app)
	case port == httpPort:
		serves = "serving visitors itself on port 80, over plain HTTP since site.url names no domain"
	}
	if !t.open && servesVisitors(port) {
		p.firewall = fmt.Sprintf("Visitors need %s of the server open. Tifa does not change your provider's firewall or security groups: if they block %s, open %[2]s there.",
			portsText(port), pick(port == httpsPort, "them", "it"))
	}
	if low := t.lowPort(port); low != 0 && r.st.LowPort > low {
		opens := "ports 80 and 443"
		if t.open {
			opens = fmt.Sprintf("port %d", port)
		}
		p.add(opChange, fmt.Sprintf("server: let the systemd of %s open %s, with net.ipv4.ip_unprivileged_port_start = 80 (now %d) in %s, as root. Ports from 80 to 1023 are then open to every user of the server, not only root", deployUser, opens, r.st.LowPort, portsConf), func(ctx context.Context) (string, error) {
			var out strings.Builder
			if err := t.ssh(ctx, "root", script(portsBody, "lowport_file", lowPortFile), nil, &out, r.stderr); err != nil {
				return "", err
			}
			return strings.TrimSpace(out.String()), nil
		})
	}
	u := unitsOf(app, port, domain, t.open, r.cert != nil)
	httpSum := ""
	if u.http != "" {
		httpSum = sha(u.http)
	}
	had := r.st.SocketSum != ""
	move := had && (r.st.SocketSum != sha(u.socket) || r.st.HTTPSum != httpSum)
	restart := had && (move || r.st.ServiceSum != sha(u.service))
	apply := func(ctx context.Context) (string, error) {
		if !r.haveServer {
			if err := r.refresh(ctx); err != nil {
				return "", err
			}
		}
		if !r.st.Prepared() {
			return "", fmt.Errorf("the systemd of %s does not run on %s", deployUser, t.host)
		}
		port, err := t.visitorPort(r.st)
		if err != nil {
			return "", err
		}
		r.port = port
		var out strings.Builder
		if err := t.ssh(ctx, deployUser, appScript(app, port, t.ownFor(port), t.open, r.cert != nil, move, restart, t.healthWait()), nil, &out, r.stderr); err != nil {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(out.String()))
		}
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		return lines[len(lines)-1], r.refresh(ctx)
	}
	after := r.firewallSteps(ctx, p, port, t.open)
	if port == httpsPort && !t.open {
		r.ownCertSteps(ctx, p)
	}
	switch {
	case r.st.Port == 0:
		p.add(opAdd, fmt.Sprintf("app: %s/%s with an empty shared/.env, %s, the systemd %s and service tifa-%s", deployHome, app, serves, pick(u.http != "", "sockets", "socket"), app), apply)
	case port != r.st.Port:
		was := fmt.Sprintf("port %d", r.st.Port)
		switch {
		case r.st.HTTPSum != "":
			was = portsText(r.st.Port)
		case r.st.SocketSum != sha(unitsOf(app, r.st.Port, "", true, false).socket) && !servesVisitors(r.st.Port):
			was = fmt.Sprintf("127.0.0.1:%d, which only the server reaches,", r.st.Port)
		}
		p.add(opChange, fmt.Sprintf("app: move %s from %s to %s. New systemd %s, and the app restarts on %s", app, was, serves, pick(u.http != "", "sockets", "socket"), pick(u.http != "", "them", "it")), apply)
	case restart && port == httpsPort && r.cert != nil && r.st.TLSCertSum == "":
		p.add(opChange, fmt.Sprintf("app: serve HTTPS for %s with your certificate in %s/%s/%s, in place of one from Let's Encrypt. New service unit, and the app restarts", domain, deployHome, app, tlsDir), apply)
	case restart && port == httpsPort && r.cert == nil && r.st.TLSCertSum != "":
		p.add(opChange, fmt.Sprintf("app: serve HTTPS for %s with a certificate from Let's Encrypt kept in %s/%s/certs, in place of yours. New service unit, and the app restarts", domain, deployHome, app), apply)
	case restart || !had:
		p.add(opChange, fmt.Sprintf("app: write the systemd units of %s again", app), apply)
	default:
		p.ok(fmt.Sprintf("app: %s/%s, %s, systemd units", deployHome, app, t.portsText(port)))
	}
	if after != nil {
		after()
	}
	r.ownCertRemoval(p)
	return nil
}

// rootListeners asks the server as root which programs listen on which ports, which the deploy
// user cannot see. Nil when root cannot log in: the caller says so.
func (r *setupRun) rootListeners(ctx context.Context) []listener {
	var out strings.Builder
	if err := r.t.ssh(ctx, "root", listenersBody, nil, &out, io.Discard); err != nil {
		return nil
	}
	st, err := parseServerState(out.String())
	if err != nil {
		return nil
	}
	return st.Listen
}

// printVisitors ends a setup of an app that serves visitors itself: which ports they need, and
// that Tifa does not open them at the provider. Once a release runs, this machine asks each port.
func (r *setupRun) printVisitors(ctx context.Context, w io.Writer) {
	if r.haveServer && r.st.SSHPassword {
		fmt.Fprintln(w, "The server accepts ssh passwords. go tool tifa deploy harden turns that off.")
	}
	switch {
	case r.group != nil:
		// deploySetupAll says it once for every server.
		return
	case r.t.front == "nginx":
		r.printNginxVisitors(ctx, w)
		return
	case r.t.front == "external":
		if r.haveServer && r.st.Port != 0 {
			printBalancerTargets(ctx, []deployTarget{r.t}, []serverState{r.st}, w)
		}
		return
	}
	if !r.haveServer || !servesVisitors(r.port) {
		return
	}
	line := fmt.Sprintf("%s serves visitors itself on %s of the server. If the firewall or a security group of your provider blocks %s, open %[3]s there: Tifa does not change those.",
		r.t.app, portsText(r.port), pick(r.port == httpsPort, "them", "it"))
	if r.st.Current != "" {
		host := r.t.serverAddress(ctx)
		if r.st.HTTPS != "" {
			host = r.st.HTTPS
		}
		var closed []string
		for _, p := range []int{httpPort, httpsPort} {
			if (p == httpPort || r.port == httpsPort) && !portAnswers(ctx, pick(p == httpsPort, "https", "http"), host, p) {
				closed = append(closed, strconv.Itoa(p))
			}
		}
		if len(closed) == 0 {
			line += " " + pick(r.port == httpsPort, "Both answer", "It answers") + " from this machine."
		} else {
			line += fmt.Sprintf(" %s %s did not answer from this machine within 3 seconds.", pick(len(closed) > 1, "Ports", "Port"), strings.Join(closed, " and "))
		}
	}
	fmt.Fprintln(w, line)
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
		p.cannot(what, cfNeed())
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
	r.domainNew = r.hostSteps(ctx, p, zone, t.domain, false, routeText, "DNS "+t.domain+" CNAME the tunnel, proxied", r.service) != opOK
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
// made, so nothing of it can be there yet and there is nothing to read. It returns what the plan
// does with the route.
func (r *setupRun) hostSteps(ctx context.Context, p *setupPlan, zone cfZone, host string, fresh bool, routeText, dnsText string, service func() string) planOp {
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
	return routeOp
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
	// With several servers GitHub Actions gets every one at once, in a round of its own.
	if t.github && r.group != nil && !r.group.github {
		return nil
	}
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
	if t.domain != "" {
		r.purgeSteps(ctx, p)
	}
	if repoErr != nil {
		p.cannot("github: deploys from GitHub Actions", repoErr.Error())
		return nil
	}
	// The branch that deploys, and the workflow it waits for, which must exist and run on a push to it.
	r.branch = t.auto.Branch
	if r.branch == "" {
		r.branch = repo.Branch
	}
	ciName, err := resolveNeeds(t.root, t.auto.Needs, r.branch)
	if err != nil {
		p.cannot("github: deploys from GitHub Actions on "+r.branch+" after "+t.auto.Needs+" passed", err.Error())
		return nil
	}
	// The servers CI deploys to: this one, or each of several, with what setup read of them.
	servers := []ciServer{{t: t}}
	if r.group != nil {
		servers = make([]ciServer, len(r.group.ts))
		for i, gt := range r.group.ts {
			servers[i] = ciServer{t: gt, st: r.group.states[i]}
		}
	}
	servers[0].st = r.st
	for _, s := range servers {
		if r.haveServer && len(s.st.HostKeys) == 0 {
			p.cannot("github: deploys from GitHub Actions", pick(r.group != nil, s.t.host, "the server")+" has no /etc/ssh/ssh_host_*_key.pub file, so tifa cannot tell CI which server to trust")
			return nil
		}
	}
	// connect is the text of the secret TIFA_DEPLOY_HOST: with a direct login the whole target, port
	// included, so the workflow needs no Port line and shows no address. port goes with the host
	// name into the host keys. With tailscale the host is the one the runner reaches on the tailnet.
	if t.connectMode != connCloudflare {
		for i := range servers {
			resolved, err := resolveSSH(ctx, servers[i].t.host)
			if err != nil {
				p.cannot("github: deploys from GitHub Actions", err.Error())
				return nil
			}
			servers[i].connect, servers[i].hostName, servers[i].port = deployHostURL(resolved.Host, resolved.Port), resolved.Host, resolved.Port
		}
	} else {
		servers[0].port = 22
		if !r.useCF(ctx) {
			p.cannot(sshAccessWhat, cfNeed())
		} else if err := r.accessSteps(ctx, p); err != nil {
			return err
		}
		if r.sshName != "" {
			servers[0].connect, servers[0].hostName = r.sshName, r.sshName
		}
	}
	var connects []string
	for _, s := range servers {
		connects = append(connects, s.connect)
	}
	connect := strings.Join(connects, ",")
	repoName := repo.Name

	// The key of the repository, on every server.
	keyOnServer := true
	for i := range servers {
		keyOnServer = keyOnServer && slices.Contains(servers[i].st.GitHubKeys, repoName)
	}
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
			for _, s := range servers {
				if err := s.t.ssh(ctx, deployUser, authorizeScript(repoName), strings.NewReader(key.authorizedLine(repoName)+"\n"), io.Discard, r.stderr); err != nil {
					return "", err
				}
			}
			return "", r.ghSecret(ctx, secretSSHKey, key.Private)
		})
	}

	// The connection: where CI logs in and the host keys it trusts. Both are secrets: a repository
	// that is public must not show where the server is. The marker below says they are current.
	if r.haveServer && len(r.st.HostKeys) > 0 || !r.haveServer {
		servers[0].st = r.st
		secrets := func(ctx context.Context) ([]secret, error) {
			// The first server is read again once the steps before ran.
			servers[0].st = r.st
			var lines []string
			for _, s := range servers {
				if len(s.st.HostKeys) == 0 {
					return nil, errors.New(pick(r.group != nil, s.t.host, "the server") + " has no ssh host keys tifa can read")
				}
				lines = append(lines, knownHostsLines(s.hostName, s.port, s.st.HostKeys)...)
			}
			hosts := strings.Join(lines, "\n")
			return []secret{{secretKnownHosts, hosts}, {secretHost, connect}}, nil
		}
		fingerprint := func() string {
			servers[0].st = r.st
			parts := []string{t.connectMode}
			for _, s := range servers {
				parts = append(append(parts, s.connect, strconv.Itoa(s.port)), s.st.HostKeys...)
			}
			return sha(strings.Join(parts, "|"))
		}
		names := []string{secretHost, secretKnownHosts}
		current := r.ghVars[varMarker] == fingerprint() && r.haveServer
		for _, n := range names {
			current = current && r.ghNames[n]
		}
		what := strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
		text := fmt.Sprintf("github: where CI logs in (%s) and the host keys it trusts, the secrets %s", t.connectMode, what)
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
	if t.connectMode == connTailscale {
		r.tailscaleIdentitySteps(p)
	}

	// The workflow.
	spec, err := workflowSpec(t, r.branch, ciName, t.connectMode)
	if err != nil {
		return err
	}
	text := githubWorkflow(spec)
	write := r.writeWorkflow(workflow, func() string { return text })
	same := strings.ReplaceAll(have, "\r\n", "\n") == text // git on Windows may check it out with CRLF
	commit := ", which you commit and push"
	if ciName != "" && r.branch != repo.Branch {
		// GitHub starts a workflow_run workflow only from the file on the default branch.
		commit = ", which you commit and push to " + repo.Branch + ", the default branch: GitHub runs a workflow that waits for another only from the file there, whatever branch deploys"
	}
	switch {
	case have == "":
		p.add(opAdd, "github: "+githubWorkflowPath+commit, write)
	case !marked:
		p.ok("github: " + githubWorkflowPath + " has no Tifa line at the top, so it is yours and stays")
	case same:
		p.ok("github: " + githubWorkflowPath)
	default:
		p.add(opChange, "github: "+githubWorkflowPath+commit, write)
	}
	p.steps = append(p.steps, r.afterConnection...)
	r.tailscaleVariableSteps(p)
	return r.removeAccessIfUnwanted(ctx, p)
}

// ciServer is a server GitHub Actions deploys to: connect is its part of the secret
// TIFA_DEPLOY_HOST, and hostName and port what its host keys go with.
type ciServer struct {
	t                 deployTarget
	st                serverState
	connect, hostName string
	port              int
}

// writeWorkflow writes the text the workflow has once the steps before it ran.
func (r *setupRun) writeWorkflow(path string, text func() string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		return "", os.WriteFile(path, []byte(text()), 0o644)
	}
}

// accessSteps lets the repository reach the ssh of the server through the tunnel: a route and a
// DNS record for the ssh host name, and an Access application that admits the service token.
func (r *setupRun) accessSteps(ctx context.Context, p *setupPlan) error {
	t := r.t
	zone, err := findZone(ctx, t.domain)
	if err != nil {
		p.cannot(sshAccessWhat, err.Error())
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
			p.cannot(sshAccessWhat, err.Error())
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
	// Access comes before the route: nothing is published until the application and a policy
	// that lets someone in are made, so the ssh host name is never open to everyone. The steps
	// run in the order of the plan, and a failed one stops the rest.
	admits := false
	if t.github && r.repo.Name != "" {
		ok, err := r.tokenSteps(ctx, p, host)
		if err != nil {
			return err
		}
		admits = ok
	}
	people, err := r.peopleSteps(ctx, p, host)
	if err != nil {
		return err
	}
	if !admits && !people {
		p.cannot("cloudflare: the route and DNS record of "+host+", which the ssh of the server goes through",
			"Nothing admits anyone to the Access application yet, so setup does not publish them: a route without Access would be open to everyone. Fix what the item above says, and run setup again")
		return nil
	}
	r.hostSteps(ctx, p, zone, host, found.host == "", fmt.Sprintf("cloudflare: route %s -> %s", host, sshService),
		"DNS "+host+" CNAME the tunnel, proxied", func() string { return sshService })
	// The ssh of this machine goes through the tunnel too.
	r.localSSHSteps(p, host, true)
	return nil
}

// tokenSteps lets the repository in: a service token that does not expire, kept as secrets of the
// repository, and its policy on the Access application of the ssh host name.
func (r *setupRun) tokenSteps(ctx context.Context, p *setupPlan, host string) (bool, error) {
	name := accessName(r.repo.Name)
	ids, err := serviceTokenIDs(ctx, name)
	if err != nil {
		return false, err
	}
	text := fmt.Sprintf("cloudflare: the Access application of %s admits the service token %s, kept as %s and %s", host, name, secretCFID, secretCFSecret)
	if len(ids) == 1 && r.ghNames[secretCFID] && r.ghNames[secretCFSecret] {
		if _, how, err := ensureTokenPolicy(ctx, name, ids[0], false); err == nil && how == "already there" {
			var policyID string
			if pol, _ := policyByName(ctx, name); pol != nil {
				policyID = pol.ID
			}
			if how, err := ensureSSHApp(ctx, host, policyID, false, false); err == nil && how == "already there" {
				p.ok(text)
				return true, nil
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
		if _, err := ensureSSHApp(ctx, host, policyID, false, true); err != nil {
			return "", err
		}
		if err := r.ghSecret(ctx, secretCFID, clientID); err != nil {
			return "", err
		}
		return how, r.ghSecret(ctx, secretCFSecret, clientSecret)
	})
	return true, nil
}

// peopleSteps lets the person of the cf login in: their email goes into the policy of the people
// of the ssh host name, on the Access application tifa made for it. The emails that are there stay,
// so a second person who runs setup with their own login is added next to the first. Nothing
// outside that application and its policy changes.
func (r *setupRun) peopleSteps(ctx context.Context, p *setupPlan, host string) (bool, error) {
	email, err := cfEmail(ctx)
	if err != nil {
		return false, err
	}
	if email == "" {
		const why = "cf does not tell your email, so setup cannot let you in: it is logged in with an API token. Log in with cf auth login"
		what := "cloudflare: the Access application of " + host + " admits you"
		if r.t.github {
			// CI has its token: only this machine goes without.
			p.notes = append(p.notes, planNote{what: what, why: why + ", then run setup again to reach the server from this machine"})
		} else {
			p.cannot(what, why)
		}
		return false, nil
	}
	name := peopleName(host)
	id, emails, found, err := peopleOf(ctx, name)
	if err != nil {
		return false, err
	}
	attached := false
	if found {
		how, err := ensureSSHApp(ctx, host, id, true, false)
		attached = err == nil && how == "already there"
	}
	text := fmt.Sprintf("cloudflare: the Access application of %s admits %s", host, email)
	if found && hasEmail(emails, email) && attached {
		p.ok(text)
		return true, nil
	}
	op := opAdd
	if found {
		op = opChange
		if kept := slices.DeleteFunc(slices.Clone(emails), func(e string) bool { return strings.EqualFold(e, email) }); len(kept) > 0 {
			text += ", and keeps " + strings.Join(kept, ", ")
		}
	}
	p.add(op, text, func(ctx context.Context) (string, error) {
		id, how, err := ensurePeoplePolicy(ctx, name, email, true)
		if err != nil {
			return "", err
		}
		if _, err := ensureSSHApp(ctx, host, id, true, true); err != nil {
			return "", err
		}
		return how, nil
	})
	return true, nil
}

// purgeSteps plans the token that clears the CDN cache of the domain at each deploy. It lives on
// the server, one for each zone, so every app of the server on that zone uses it, and a zone the
// server has needs nothing. The token is optional, so setup asks for it on the first setup of the
// domain or of CI, and later notes the command
// that adds it when CI needs it. With --cache-purge-token it asks every time and replaces it.
func (r *setupRun) purgeSteps(ctx context.Context, p *setupPlan) {
	t := r.t
	var zoneErr error
	if r.zone.ID == "" && r.useCF(ctx) {
		r.zone, zoneErr = findZone(ctx, t.domain)
	}
	zone := r.zone.Name
	if zone == "" {
		zone = "the zone of " + t.domain
	}
	where := path.Join(r.serverHome(), purgeFile)
	text := fmt.Sprintf("server: a token that can only clear the cache of %s, pasted by you and kept in %s, which only %s can read", zone, where, deployUser)
	r.purgeOnServer = r.zone.Name != "" && slices.ContainsFunc(r.st.PurgeZones, func(z string) bool { return strings.EqualFold(z, r.zone.Name) })
	// broken says why the token the server has cannot be trusted. Setup checks it the way it
	// checks a pasted one.
	broken := ""
	if r.purgeOnServer && !r.purgeToken {
		if broken = r.checkServerPurge(ctx); broken == "" {
			r.purgeVerified = true
		}
	}
	firstCI := t.github && r.ghVars[varMarker] == ""
	switch {
	case r.purgeVerified:
		p.ok(fmt.Sprintf("server: the token that clears the cache of %s, in %s", zone, where))
		return
	case !r.purgeToken && broken == "" && !r.domainNew && !firstCI:
		// A deploy from a machine with cf clears the cache without it: only CI needs the note.
		if t.github {
			p.notes = append(p.notes, planNote{what: text, why: purgeTokenLater})
		}
		return
	}
	// left says why setup cannot ask now: a note on its own, a thing that needs you when the
	// developer asked with --cache-purge-token.
	left := func(why string) {
		if broken != "" {
			why = broken + ". " + why
		}
		if r.purgeToken {
			p.cannot(text, why)
			return
		}
		p.notes = append(p.notes, planNote{what: text, why: why + ". " + purgeTokenLater})
	}
	switch {
	case zoneErr != nil:
		left(zoneErr.Error())
	case r.zone.ID == "":
		left("Setup cannot find the zone: " + cfNeed())
	case !stdinIsTerminal(r.in) && !r.purgeToken:
		// Only the flag waits for a person: a run that does not pass it never waits.
		left("There is no terminal to paste on. " + purgeTokenPageCommand + " opens a page in your browser for it")
	default:
		op := opAdd
		if r.purgeOnServer {
			op = opChange
		}
		p.steps = append(p.steps, planStep{op: op, text: text, soft: true, do: r.pastePurgeToken})
	}
}

// checkServerPurge reads the token the server has for the zone and checks it against the zone, as
// a pasted one is checked. It says what is wrong, or nothing when the token works.
func (r *setupRun) checkServerPurge(ctx context.Context) string {
	t := r.t
	t.domain = r.zone.Name
	c, ok, err := t.serverPurge(ctx, r.stderr)
	switch {
	case err != nil:
		return strings.ReplaceAll(err.Error(), "\n", " ")
	case !ok:
		return "the server has no token for " + r.zone.Name + " that tifa can read"
	case c.ZoneID != r.zone.ID:
		return "the token the server has for " + r.zone.Name + " is for another zone ID"
	}
	if err := checkPurgeToken(ctx, c.Token, c.ZoneID, r.t.domain); err != nil {
		return fmt.Sprintf("the token the server has for %s does not work: %s", r.zone.Name, strings.ReplaceAll(err.Error(), "\n", " "))
	}
	return ""
}

// serverHome is the home of the deploy user, as the server said, or the one setup gives it.
func (r *setupRun) serverHome() string {
	if r.st.Home != "" {
		return r.st.Home
	}
	return deployHome
}

// keepPurge puts the token of the zone on the server.
func (r *setupRun) keepPurge(ctx context.Context, token string) error {
	return r.t.storePurge(ctx, purgeCred{Zone: r.zone.Name, ZoneID: r.zone.ID, Token: token}, r.stderr)
}

// purgeTokenCommand makes setup ask for the token that clears the CDN cache.
const purgeTokenCommand = "tifa deploy setup --cache-purge-token"

// purgeTokenPageCommand asks for it without a terminal, on a page in the browser.
const purgeTokenPageCommand = "go tool tifa deploy setup --yes --cache-purge-token"

// purgeTokenLater says what going without the token costs and how to add it.
const purgeTokenLater = "Without it a deploy still works, but " + noTokenCost + ". To add it: " + purgeTokenCommand + " on a terminal, or " + purgeTokenPageCommand + ", which opens a page in your browser"

// purgeTokenTries is how many tokens setup takes before it gives up.
const purgeTokenTries = 3

// pastePurgeToken shows the link, reads the token hidden or on the page, checks it against the zone
// and keeps it on the server. Without a terminal setup runs it only for --cache-purge-token. A
// token that fails the check is asked for again. An empty one skips.
func (r *setupRun) pastePurgeToken(ctx context.Context) (string, error) {
	t := r.t
	say := func(f string, a ...any) { fmt.Fprintf(r.stdout, "          "+f+"\n", a...) }
	kept := fmt.Sprintf("Tifa keeps this token on your server %s, readable only by the user tifa, and every deploy to %s uses it", t.host, r.zoneText())
	who := t.app + " on " + t.host
	if r.repo.Name != "" {
		who = t.app + " · " + r.repo.Name
	}
	ask := secretAsk{
		Title: "Cloudflare purge token",
		For:   who,
		Why: fmt.Sprintf("Tifa clears the Cloudflare cache of %s after each deploy. %s. "+
			"In the Cloudflare form, which has the permission Cache Purge filled in, set Zone Resources to %s only, then create the token and paste it below.",
			t.domain, kept, r.zoneText()),
		Link:     purgeTokenLink(r.zone.Name),
		LinkText: "Create the token in Cloudflare",
		Fields:   []string{"Cloudflare token"},
		Skip:     "Leave it empty to skip: deploys still work, and a page with Cache can stay old until its Cache time runs out.",
	}
	terminal := stdinIsTerminal(r.in)
	if terminal {
		say("%s.", kept)
		say("Open this link, set Zone Resources to %s only, create the token, and paste it here, or press Enter to skip:", r.zoneText())
		say("%s", ask.Link)
	}
	for try := 1; try <= purgeTokenTries; try++ {
		values, err := askSecrets(ctx, r.in, r.errOut, ask)
		if err != nil {
			return "", fmt.Errorf("reading the token: %w. To try again: %s", err, purgeTokenCommand)
		}
		token := strings.TrimSpace(values[0])
		if token == "" {
			return "", skipped{purgeTokenLater}
		}
		err = checkPurgeToken(ctx, token, r.zone.ID, t.domain)
		if err == nil {
			return "", r.keepPurge(ctx, token)
		}
		why := strings.ReplaceAll(err.Error(), "\n", " ")
		if try == purgeTokenTries {
			return "", fmt.Errorf("the token was refused: %s. To try again: %s", why, purgeTokenCommand)
		}
		ask.Problem = fmt.Sprintf("The token was refused: %s. Paste it again, or leave it empty to skip.", why)
		if terminal {
			say("the token was refused: %s. Paste it again, or press Enter to skip.", why)
		}
	}
	return "", nil
}

// removeAccessIfUnwanted takes away the Cloudflare Access of the repository when deploy.connect is
// not cloudflare any more.
func (r *setupRun) removeAccessIfUnwanted(ctx context.Context, p *setupPlan) error {
	if r.t.connectMode == connCloudflare || r.ghVars[varMarker] == "" {
		return nil
	}
	return r.removeAccess(ctx, p)
}

func (r *setupRun) removeAccess(ctx context.Context, p *setupPlan) error {
	r.accessRemovalPlanned = true
	repo := r.repo.Name
	// With ssh still cloudflare, only the token of the repository goes: the people who deploy from
	// their machines keep the application. Remove takes it all.
	keepApp := r.t.connectMode == connCloudflare && !r.removing
	if !r.removing && r.t.connectMode != connCloudflare && looksLikeSSHHost(r.t.dest.name, r.t.domain) {
		// The developer reaches the server by the ssh host name that this would take away.
		p.cannot("cloudflare: the ssh host name "+r.t.dest.name+", its route, DNS record and Access application, which tifa made and deploy.connect no longer asks for",
			"TIFA_DEPLOY_HOST is that host name, and removing it would cut the way setup reaches the server. Set TIFA_DEPLOY_HOST to the address of the server (its ssh port must be open to you), and run setup again: it then removes them, and the block of your ssh config")
	} else if r.useCF(ctx) {
		found, err := removeSSHAccess(ctx, repo, r.t.domain, r.st.TunnelID, keepApp, false, nil)
		if err != nil {
			return err
		}
		if found {
			text := "cloudflare: the Access application of the ssh host name of " + r.t.domain + ", its policies, route and DNS record, which tifa made"
			switch {
			case repo != "" && keepApp:
				text = "cloudflare: the Access service token and policy of " + repo + ", which tifa made. The Access application stays for the people who deploy from their machines"
			case repo != "":
				text = "cloudflare: the Access service token, policy and application of " + repo + ", and the route and DNS record of the ssh host name once no repository uses it, which tifa made"
			}
			step := planStep{op: opRemove, text: text, do: func(ctx context.Context) (string, error) {
				_, err := removeSSHAccess(ctx, repo, r.t.domain, r.st.TunnelID, keepApp, true, func(f string, a ...any) { fmt.Fprintf(r.stdout, "          "+f+"\n", a...) })
				return "", err
			}}
			if r.removing {
				r.late = append(r.late, step)
			} else {
				p.steps = append(p.steps, step)
			}
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

// githubRemovals plans what to take away when the block deploy.auto is not there.
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
	if err := r.removeAccess(ctx, p); err != nil {
		return err
	}
	var names []string
	for _, n := range []string{secretSSHKey, secretKnownHosts, secretHost} {
		if r.ghNames[n] {
			names = append(names, n)
		}
	}
	// The variables of the Tailscale identity go before the marker, which says they are tifa's.
	var vars []string
	for _, n := range []string{varTSClient, varTSAudience} {
		if r.ghVars[n] != "" {
			vars = append(vars, n)
		}
	}
	what := "the secrets " + strings.Join(names, ", ")
	if len(vars) > 0 {
		what += ", the variables " + strings.Join(vars, ", ")
	}
	p.add(opRemove, "github: "+what+" and the variable "+varMarker+", which tifa set", func(ctx context.Context) (string, error) {
		for _, n := range names {
			if err := r.gh.DeleteSecret(ctx, repoName, n); err != nil {
				return "", err
			}
		}
		for _, n := range vars {
			if err := r.gh.DeleteVariable(ctx, repoName, n); err != nil {
				return "", err
			}
		}
		return "", r.gh.DeleteVariable(ctx, repoName, varMarker)
	})
	return nil
}

// deploySetupAll sets up every server of ts. With one it is deploySetup. With several, behind a load
// balancer, it gives the app one port free on all of them, sets up each in turn, the first first,
// and stops at the first that fails or needs the developer: setup again goes on from there, as
// each step checks what is there. GitHub Actions comes last, with every server.
func deploySetupAll(ctx context.Context, ts []deployTarget, f setupFlags, in io.Reader, stdout, stderr io.Writer) error {
	if len(ts) == 1 {
		return deploySetup(ctx, ts[0], f, in, stdout, stderr)
	}
	states := make([]serverState, len(ts))
	for i, t := range ts {
		states[i] = readGroupState(ctx, t)
	}
	port := ts[0].fixedPort
	if port == 0 {
		var err error
		if port, err = groupPort(ts[0].app, states); err != nil {
			return err
		}
	}
	g := &setupGroup{ts: ts, states: make([]serverState, len(ts))}
	f.group = g
	for i := range ts {
		ts[i].port = port
	}
	for i, t := range ts {
		run, err := deploySetupOne(ctx, t, f, in, stdout, stderr)
		if err != nil {
			if i == 0 {
				return err
			}
			return fmt.Errorf("%w. Setup stopped at %s: %s %s set up, run go tool tifa deploy setup again once that is fixed", err, t.host, hostNames(ts[:i]), pick(i > 1, "are", "is"))
		}
		g.states[i] = run.st
		fmt.Fprintln(stdout)
	}
	if ts[0].github {
		g.github = true
		run, err := deploySetupOne(ctx, ts[0], f, in, stdout, stderr)
		if err != nil {
			return err
		}
		g.states[0] = run.st
	}
	group := make([]groupServer, len(ts))
	for i, t := range ts {
		group[i] = groupServer{id: g.states[i].Machine, host: t.dest.raw}
		if group[i].id == "" {
			return nil
		}
	}
	if ts[0].front == "external" {
		// Each server has the port of the group now.
		states := slices.Clone(g.states)
		for i := range states {
			states[i].Port = port
		}
		printBalancerTargets(ctx, ts, states, stdout)
	}
	rememberGroupAndSay(group, knownApp{Name: ts[0].app, Path: ts[0].root}, stdout, stderr)
	return nil
}

// readGroupState reads what deploySetupAll needs of a server before setup changes it: the ports
// taken and the port of the app. A server without the user tifa is read as root. What cannot be
// read stays empty: the setup of that server says why.
func readGroupState(ctx context.Context, t deployTarget) serverState {
	st, err := t.state(ctx, io.Discard)
	var login *sshLoginError
	if errors.As(err, &login) && login.user == deployUser {
		var out strings.Builder
		if t.ssh(ctx, "root", setupProbeScript(), nil, &out, io.Discard) == nil {
			st, _ = parseServerState(out.String())
		}
	}
	return st
}

// groupPort is the port of app on several servers: the first port one of them gives it that is
// free for it on all, else the first port free for it on all. Its own socket takes no port.
func groupPort(app string, states []serverState) (int, error) {
	free := func(p int) bool {
		for _, st := range states {
			if !st.freeFor(app, p) {
				return false
			}
		}
		return true
	}
	for _, st := range states {
		if st.Port >= firstPort && st.Port <= lastPort && free(st.Port) {
			return st.Port, nil
		}
	}
	for p := firstPort; p <= lastPort; p++ {
		if free(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("every port from %d to %d is taken on one of the servers", firstPort, lastPort)
}

// deploySetup plans, asks and applies. It fails when something needs the developer, after it did
// all it could.
func deploySetup(ctx context.Context, t deployTarget, f setupFlags, in io.Reader, stdout, stderr io.Writer) error {
	_, err := deploySetupOne(ctx, t, f, in, stdout, stderr)
	return err
}

// deploySetupOne is deploySetup, and returns the run of its last plan.
func deploySetupOne(ctx context.Context, t deployTarget, f setupFlags, in io.Reader, stdout, stderr io.Writer) (*setupRun, error) {
	switch {
	case f.purgeToken && t.domain == "":
		return nil, errors.New("--cache-purge-token is for the token that clears the CDN cache at each deploy, which needs deploy.front: cloudflare in tifa.yaml")
	case f.tailscaleClientID != "" && (t.connectMode != connTailscale || !t.github):
		return nil, errors.New("--tailscale-client-id is for the identity CI joins your tailnet with, which needs deploy.connect: tailscale and the block deploy.auto in tifa.yaml")
	case f.tailscaleClientID != "":
		if err := checkTailscaleClientID(strings.TrimSpace(f.tailscaleClientID)); err != nil {
			return nil, fmt.Errorf("--tailscale-client-id: %w", err)
		}
	}
	// The certificate of the app is checked on this machine before anything is read or changed.
	cert, err := loadOwnCert(t)
	if err != nil {
		return nil, err
	}
	if f.cert = cert; cert != nil {
		sayOwnCertWins(t, stdout)
	}
	plan, run, err := buildSetupPlan(ctx, t, f, true, in, stdout, stderr)
	if err != nil {
		return run, err
	}
	where := t.host + t.serverNote()
	title := fmt.Sprintf("Plan for %s on %s%s", t.app, where, t.dest.fromHomeNote())
	if g := f.group; g != nil && g.github {
		where = "GitHub Actions"
		title = fmt.Sprintf("Plan for %s on GitHub Actions, which deploys it to %s", t.app, hostNames(g.ts))
	}
	// toolWarnings counts the steps on this machine that failed: they need the developer too.
	toolWarnings := 0
	for round := 0; plan.then != ""; round++ {
		// cf and gh come first: the rest of the plan reads Cloudflare and GitHub with them. Then
		// this machine, when the server is named by its ssh host and it cannot reach it yet. Their
		// steps may fail without stopping setup, and what failed shows again in the next plan.
		plan.print(stdout, title, f.all)
		if err := confirm(in, stdout, f.yes); err != nil {
			return run, err
		}
		warnings, err := plan.apply(ctx, stdout)
		if err != nil {
			return run, err
		}
		toolWarnings += warnings
		fmt.Fprintln(stdout)
		next := f
		next.skipMachine = round >= 1
		if plan, run, err = buildSetupPlan(ctx, t, next, false, in, stdout, stderr); err != nil {
			return run, err
		}
	}
	cannot := plan.count(opCannot)
	if plan.todo() == 0 && cannot == 0 && len(plan.notes) == 0 && !f.all {
		fmt.Fprintf(stdout, "%s on %s matches tifa.yaml: %d things checked (--all lists them).\n", t.app, where, plan.count(opOK))
		run.printVisitors(ctx, stdout)
		run.remember(stdout, stderr)
		return run, needsYou(toolWarnings)
	}
	plan.print(stdout, title, f.all)
	warnings := 0
	if plan.todo() > 0 {
		if err := confirm(in, stdout, f.yes); err != nil {
			return run, err
		}
		if warnings, err = plan.apply(ctx, stdout); err != nil {
			return run, err
		}
		if run.hostAdvice != "" {
			fmt.Fprintln(stdout, run.hostAdvice)
		}
	}
	run.printVisitors(ctx, stdout)
	run.remember(stdout, stderr)
	return run, needsYou(cannot + warnings + toolWarnings)
}

// remember records the server in ~/.tifa after a setup that reached it and prepared it: the host
// that worked, which with ssh through the tunnel is the ssh host name once the check passed, and
// the app. Either keeps the admin user the host names. It says so when that changed the file. A file it cannot write is a warning: the server
// is set up all the same, and the next deploy only needs the host named.
func (r *setupRun) remember(stdout, stderr io.Writer) {
	if r.group != nil {
		// deploySetupAll remembers every server at once.
		return
	}
	if !r.haveServer || !r.st.Prepared() || r.st.Machine == "" {
		return
	}
	host := r.t.dest.raw
	if r.tunnelHost != "" {
		host = r.t.dest.withUser(r.tunnelHost)
	}
	rememberAndSay(r.st.Machine, host, knownApp{Name: r.t.app, Path: r.t.root}, stdout, stderr)
}

// needsYou is the error of a setup that left n things to the developer, nil for none.
func needsYou(n int) error {
	if n == 0 {
		return nil
	}
	return fmt.Errorf("%d %s, see above", n, plural(n, "thing needs you", "things need you"))
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
	// purgeToken (--cache-purge-token) asks for that secret even when the server has it, and
	// replaces it.
	purgeToken bool
	// tailscaleClientID (--tailscale-client-id) is the client ID of the Tailscale identity CI joins
	// the tailnet with. It is not secret.
	tailscaleClientID string
	// skipMachine leaves out the first plan for this machine: setup already made it twice.
	skipMachine bool
	// group is what the setups of an app on several servers share, nil for one server.
	group *setupGroup
	// cert is the certificate of the app that setup uploads, read from the files that
	// TIFA_DEPLOY_TLS_CERT and TIFA_DEPLOY_TLS_KEY name, nil without them.
	cert *ownCert
}

// setupGroup is what the setups of an app on several servers share. Each server is set up in turn,
// the first first, and then GitHub Actions for all of them in a round of its own.
type setupGroup struct {
	ts []deployTarget
	// states are what each server's setup left, read once its steps ran.
	states []serverState
	// github says this is the round of GitHub Actions.
	github bool
}
