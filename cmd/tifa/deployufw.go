package main

// ufw is the firewall of Ubuntu, and of Debian when installed. An app with deploy.front none serves
// visitors itself, so when ufw is on and does not let them reach the ports of the app, setup plans
// to allow them, in rules whose comment marks them as Tifa's and names the app: tifa <app>. The
// developer agrees to the plan, or passes --yes, before anything runs. Remove deletes those rules,
// and so does a setup of an app that no longer serves visitors on a port, and no other rule. Only
// root reads and changes ufw. Other firewalls, and those of the provider, Tifa leaves alone.

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ufwConf and ufwProgram are the settings file of ufw, which every user may read, and its program.
// Tests point them at files of their own.
var ufwConf, ufwProgram = "/etc/ufw/ufw.conf", "/usr/sbin/ufw"

// ufwProbe prints "ufw on" when ufw is installed and on at boot, "ufw off" when it is installed and
// off, and nothing without it. It needs $ufw_conf and $ufw_program, and runs as any user.
const ufwProbe = `if [ -r "$ufw_conf" ] && { [ -x "$ufw_program" ] || command -v ufw >/dev/null 2>&1; }; then
	if grep -q '^ENABLED=yes' "$ufw_conf"; then echo "ufw on"; else echo "ufw off"; fi
fi
`

// ufwReadBody runs as root and prints ufw status verbose and ufw show added, each line after a word
// that says which. The C locale keeps the words of ufw in English.
const ufwReadBody = `set -eu
# tifa: read ufw
export LC_ALL=C PATH="/usr/sbin:/usr/bin:/sbin:/bin:$PATH"
s=$(ufw status verbose)
a=$(ufw show added)
printf '%s\n' "$s" | sed 's/^/ufw-status /'
printf '%s\n' "$a" | sed 's/^/ufw-added /'
`

// ufwRule is a rule of ufw show added in the short form, which ufw prints for an incoming rule
// from any address to any address of the server on any interface: such a rule is about visitors.
type ufwRule struct {
	// action is allow, deny, reject or limit. ports are as ufw writes them: 80, 80,443 or
	// 8000:9000. proto is tcp, udp or any.
	action, ports, proto string
	comment              string
	// text is the rule as ufw show added prints it, without "ufw ".
	text string
}

// ufwState is what root read of ufw.
type ufwState struct {
	// active says ufw filters now. allowIn says it lets in what no rule decides.
	active, allowIn bool
	// rules are the rules in the short form, in the order ufw matches them.
	rules []ufwRule
}

// parseUFW reads the output of ufwReadBody.
func parseUFW(out string) (ufwState, error) {
	var u ufwState
	status := false
	for _, line := range strings.Split(out, "\n") {
		key, value, _ := strings.Cut(line, " ")
		value = strings.TrimSpace(value)
		switch key {
		case "ufw-status":
			switch {
			case strings.HasPrefix(value, "Status: "):
				status = true
				u.active = value == "Status: active"
			case strings.HasPrefix(value, "Default: "):
				u.allowIn = strings.Contains(value, "allow (incoming)")
			}
		case "ufw-added":
			if r, ok := parseUFWRule(value); ok {
				u.rules = append(u.rules, r)
			}
		}
	}
	if !status {
		return u, fmt.Errorf("ufw status printed no status: %q", out)
	}
	return u, nil
}

// parseUFWRule reads a line of ufw show added. It returns false for any other line, a rule in the
// long form (from an address, on an interface, to an address), an outgoing or routed rule, and a
// rule of an application profile, whose ports ufw does not print there.
func parseUFWRule(line string) (ufwRule, bool) {
	text, ok := strings.CutPrefix(line, "ufw ")
	if !ok {
		return ufwRule{}, false
	}
	r := ufwRule{text: text}
	rest := text
	if i := strings.Index(rest, " comment '"); i >= 0 {
		r.comment = strings.TrimSuffix(rest[i+len(" comment '"):], "'")
		rest = rest[:i]
	}
	f := strings.Fields(rest)
	if len(f) > 1 && (f[1] == "log" || f[1] == "log-all") {
		f = slices.Delete(f, 1, 2)
	}
	if len(f) != 2 || !slices.Contains([]string{"allow", "deny", "reject", "limit"}, f[0]) {
		return ufwRule{}, false
	}
	r.action = f[0]
	r.ports, r.proto, _ = strings.Cut(f[1], "/")
	if r.proto == "" {
		r.proto = "any"
	}
	if r.ports == "" || strings.Trim(r.ports, "0123456789,:") != "" {
		return ufwRule{}, false
	}
	return r, true
}

// covers says the rule decides port over TCP.
func (r ufwRule) covers(port int) bool {
	if r.proto != "tcp" && r.proto != "any" {
		return false
	}
	for _, p := range strings.Split(r.ports, ",") {
		lo, hi, isRange := strings.Cut(p, ":")
		if !isRange {
			hi = lo
		}
		a, err1 := strconv.Atoi(lo)
		b, err2 := strconv.Atoi(hi)
		if err1 == nil && err2 == nil && a <= port && port <= b {
			return true
		}
	}
	return false
}

// rule is the first rule that decides port over TCP, as ufw matches them.
func (u ufwState) rule(port int) (ufwRule, bool) {
	for _, r := range u.rules {
		if r.covers(port) {
			return r, true
		}
	}
	return ufwRule{}, false
}

// ufwComment marks the rules setup adds for app.
func ufwComment(app string) string { return "tifa " + app }

// tifaPorts are the ports of the rules setup added for app: allow <port>/tcp with its comment.
func (u ufwState) tifaPorts(app string) []int {
	var ports []int
	for _, r := range u.rules {
		p, err := strconv.Atoi(r.ports)
		if err == nil && r.action == "allow" && r.proto == "tcp" && r.comment == ufwComment(app) && !slices.Contains(ports, p) {
			ports = append(ports, p)
		}
	}
	return ports
}

// ufwScript is a root script that runs ufw with verb (allow, or delete allow) for each port, in the
// rule with the comment of app. ufw deletes a rule with a comment only when the comment is the
// same, so delete leaves a rule of the developer for the same port.
func ufwScript(marker, verb, app string, ports []int) string {
	return "set -eu\n# tifa: " + marker + "\nexport LC_ALL=C PATH=\"/usr/sbin:/usr/bin:/sbin:/bin:$PATH\"\n" +
		strings.Join(ufwCommands(verb, app, ports), "\n") + "\n"
}

// visitorPorts are the ports visitors reach an app on port: 80, 80 and 443, or none.
func visitorPorts(port int) []int {
	switch port {
	case httpPort:
		return []int{httpPort}
	case httpsPort:
		return []int{httpPort, httpsPort}
	}
	return nil
}

// portList names ports: port 80, or ports 80 and 443.
func portList(ports []int) string {
	words := make([]string, len(ports))
	for i, p := range ports {
		words[i] = strconv.Itoa(p)
	}
	return pick(len(ports) > 1, "ports ", "port ") + strings.Join(words, " and ")
}

// readUFW asks the server as root for the state of ufw.
func (r *setupRun) readUFW(ctx context.Context) (ufwState, error) {
	var out strings.Builder
	if err := r.t.ssh(ctx, "root", ufwReadBody, nil, &out, r.stderr); err != nil {
		return ufwState{}, err
	}
	return parseUFW(out.String())
}

// ufwRun runs the script as root and says what ufw said, a line to a phrase.
func (r *setupRun) ufwRun(body string) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		var out strings.Builder
		err := r.t.ssh(ctx, "root", body, nil, &out, r.stderr)
		said := strings.TrimSpace(out.String())
		if err != nil && said != "" {
			return "", fmt.Errorf("%w: %s", err, said)
		}
		return strings.ReplaceAll(said, "\n", ", "), err
	}
}

// firewallSteps plans ufw for the app on port: allow the ports it serves visitors on when ufw is
// active and no rule allows them yet, and delete the rules setup added for it on ports it no
// longer serves. It reads ufw, as root, only when ufw is on and the app serves visitors, or when
// ufw is installed and the app gives up a port it served on.
//
// The caller plans it before the app moves, since an app restarted on 443 gets no certificate
// until Let's Encrypt reaches it, and its health check fails until then. The rules of the ports
// the app gives up go after the move, in the step that the returned function adds, nil when none.
func (r *setupRun) firewallSteps(ctx context.Context, p *setupPlan, port int) (after func()) {
	app := r.t.app
	need := visitorPorts(port)
	var gone []int
	for _, q := range visitorPorts(r.st.Port) {
		if !slices.Contains(need, q) {
			gone = append(gone, q)
		}
	}
	if !(len(need) > 0 && r.st.UFW == "on") && !(len(gone) > 0 && r.st.UFW != "") {
		return
	}
	u, err := r.readUFW(ctx)
	if err != nil {
		if len(need) > 0 {
			p.notes = append(p.notes, planNote{
				what: fmt.Sprintf("firewall: check that ufw lets visitors reach %s on %s", app, portList(need)),
				why: fmt.Sprintf("ufw is on, and only root can read its rules, but setup cannot log in to %s as root: %v. If ufw blocks them, run sudo %s on the server",
					r.t.host, err, strings.Join(ufwCommands("allow", app, need), " and sudo ")),
			})
		} else {
			p.cannot(fmt.Sprintf("firewall: the rules of ufw with the comment '%s'", ufwComment(app)),
				fmt.Sprintf("%s no longer serves visitors itself, so the rules setup added for it in ufw go, and only root can read and delete them, but setup cannot log in to %s as root: %v. Run sudo ufw status numbered on the server and delete the rules with that comment", app, r.t.host, err))
		}
		return
	}
	// A rule of the developer that denies a port stays: ufw would let a rule of Tifa for the same
	// port replace it.
	var add, open, blocked []int
	var denying []string
	for _, q := range need {
		rule, found := u.rule(q)
		switch {
		case !u.active:
		case found && (rule.action == "allow" || rule.action == "limit"):
			open = append(open, q)
		case found:
			blocked = append(blocked, q)
			if !slices.Contains(denying, rule.text) {
				denying = append(denying, rule.text)
			}
		case u.allowIn:
			open = append(open, q)
		default:
			add = append(add, q)
		}
	}
	if len(add) > 0 {
		p.add(opChange, fmt.Sprintf("firewall: allow %s (tcp) in ufw, so visitors reach %s, in rules with the comment '%s', as root", portList(add), app, ufwComment(app)),
			r.ufwRun(ufwScript("allow in ufw", "allow", app, add)))
	}
	if len(need) > 0 && len(open) == len(need) {
		p.ok(fmt.Sprintf("firewall: ufw lets visitors reach %s on %s", app, portList(need)))
	}
	if len(blocked) > 0 {
		deletes := make([]string, len(denying))
		for i, d := range denying {
			deletes[i] = "sudo ufw delete " + d
		}
		p.cannot(fmt.Sprintf("firewall: let visitors reach %s on %s in ufw", app, portList(blocked)),
			fmt.Sprintf("ufw denies %s by the %s ufw %s, which Tifa did not add and leaves as it is. If visitors should reach %s, delete %s on the server with %s, then run setup again",
				portList(blocked), pick(len(denying) > 1, "rules", "rule"), strings.Join(denying, " and ufw "), app, pick(len(denying) > 1, "them", "it"), strings.Join(deletes, " and ")))
	}
	var drop []int
	for _, q := range u.tifaPorts(app) {
		if !slices.Contains(need, q) {
			drop = append(drop, q)
		}
	}
	if len(drop) == 0 {
		return nil
	}
	return func() {
		p.add(opRemove, fmt.Sprintf("firewall: the rules of ufw for %s (tcp) with the comment '%s', which %s no longer needs, as root", portList(drop), ufwComment(app), app),
			r.ufwRun(ufwScript("delete from ufw", "delete allow", app, drop)))
	}
}

// ufwCommands are the commands of ufw that verb the ports of app, as setup runs them.
func ufwCommands(verb, app string, ports []int) []string {
	cmds := make([]string, len(ports))
	for i, q := range ports {
		cmds[i] = fmt.Sprintf("ufw %s %d/tcp comment %s", verb, q, shellQuote(ufwComment(app)))
	}
	return cmds
}

// removeFirewall plans, for remove, the deletion of the rules setup added in ufw for an app that
// served visitors itself, once root read them.
func (r *setupRun) removeFirewall(ctx context.Context, p *setupPlan) {
	app := r.t.app
	if !servesVisitors(r.st.Port) || r.st.UFW == "" {
		return
	}
	u, err := r.readUFW(ctx)
	if err != nil {
		p.cannot(fmt.Sprintf("firewall: the rules of ufw with the comment '%s'", ufwComment(app)),
			fmt.Sprintf("only root can read and delete them, but remove cannot log in to %s as root: %v. Run sudo ufw status numbered on the server and delete the rules with that comment", r.t.host, err))
		return
	}
	if ports := u.tifaPorts(app); len(ports) > 0 {
		p.add(opRemove, fmt.Sprintf("firewall: the rules of ufw for %s (tcp) with the comment '%s', which setup added, as root", portList(ports), ufwComment(app)),
			r.ufwRun(ufwScript("delete from ufw", "delete allow", app, ports)))
	}
}
