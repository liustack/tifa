package main

// With deploy.front none the app serves visitors itself. When ufw is on and blocks the ports the app
// needs, setup allows them in rules whose comment names the app, once the developer agreed, and
// remove, or a setup that moves the app behind Cloudflare, deletes those rules and no other.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ufwVerbose and ufwAdded are what ufw status verbose and ufw show added printed on a Ubuntu 24.04
// server (ufw 0.36.2) with fail2ban, which allows only ssh. Two of its eleven lines of fail2ban are
// kept. The line of 22/tcp ends with spaces, as ufw writes it.
const (
	ufwVerbose = `Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
Anywhere                   REJECT IN   209.99.190.186             # by Fail2Ban after 4 attempts against sshd
Anywhere                   REJECT IN   92.118.39.77               # by Fail2Ban after 2 attempts against sshd
22/tcp                     ALLOW IN    Anywhere
`
	ufwAdded = `Added user rules (see 'ufw status' for running firewall):
ufw reject from 209.99.190.186 comment 'by Fail2Ban after 4 attempts against sshd'
ufw reject from 92.118.39.77 comment 'by Fail2Ban after 2 attempts against sshd'
ufw allow 22/tcp
`
	// ufwInactive is ufw status verbose with ufw off.
	ufwInactive = "Status: inactive\n"
)

// ufwRead is what the root script that reads ufw prints for status and added.
func ufwRead(status, added string) string {
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimSuffix(status, "\n"), "\n") {
		b.WriteString("ufw-status " + l + "\n")
	}
	for _, l := range strings.Split(strings.TrimSuffix(added, "\n"), "\n") {
		b.WriteString("ufw-added " + l + "\n")
	}
	return b.String()
}

// unquoted is what the fake ssh got, with the quotes of the script as they were written.
func (s *setupServer) unquoted() string { return strings.ReplaceAll(s.argv(), `'\''`, `'`) }

func (s *setupServer) ufw(t *testing.T, out string) {
	t.Helper()
	writeFile(t, filepath.Join(s.dir, "ufw"), out)
}

// The real output reads as active, deny by default, and the rules in the short form ufw prints
// when they hold for every address: a rule from one address is no rule for visitors.
func TestParseUFW(t *testing.T) {
	u, err := parseUFW(ufwRead(ufwVerbose, ufwAdded))
	if err != nil {
		t.Fatal(err)
	}
	if !u.active || u.allowIn {
		t.Errorf("active %v, allow in %v", u.active, u.allowIn)
	}
	want := []ufwRule{{action: "allow", ports: "22", proto: "tcp", text: "allow 22/tcp"}}
	if !reflect.DeepEqual(u.rules, want) {
		t.Errorf("rules %+v", u.rules)
	}
	if _, ok := u.rule(80); ok {
		t.Error("port 80 has a rule")
	}

	u, err = parseUFW(ufwRead(ufwInactive, "Added user rules (see 'ufw status' for running firewall):\n(None)\n"))
	if err != nil || u.active || len(u.rules) != 0 {
		t.Errorf("inactive: %+v %v", u, err)
	}
	if _, err := parseUFW("ufw-added ufw allow 22/tcp\n"); err == nil {
		t.Error("no status read as a state")
	}

	// Which rule decides a port: the first that covers it over TCP, as ufw matches them.
	u, err = parseUFW(ufwRead(strings.Replace(ufwVerbose, "deny (incoming)", "allow (incoming)", 1), `Added user rules (see 'ufw status' for running firewall):
ufw allow out 80/tcp
ufw allow in on eth0 to any port 80
ufw allow 'Nginx Full'
ufw allow 80/udp
ufw deny 8080
ufw allow 79:81/tcp comment 'web'
ufw allow 80/tcp
ufw limit 22,443/tcp
ufw allow 443/tcp comment 'tifa shop'
ufw allow 8443/tcp comment 'tifa shop'
ufw allow 80/tcp comment 'tifa shop2'
`))
	if err != nil {
		t.Fatal(err)
	}
	if !u.allowIn {
		t.Error("default allow incoming")
	}
	for port, want := range map[int]string{80: "allow 79:81/tcp comment 'web'", 443: "limit 22,443/tcp", 8080: "deny 8080"} {
		if r, ok := u.rule(port); !ok || r.text != want {
			t.Errorf("port %d: %+v %v", port, r, ok)
		}
	}
	if got := u.tifaPorts("shop"); !reflect.DeepEqual(got, []int{443, 8443}) {
		t.Errorf("the ports of tifa shop: %v", got)
	}

	// A rule in the long form that lets some address in to a port, a load balancer say, is no rule for
	// visitors and still allows the port from somewhere.
	u, err = parseUFW(ufwRead(ufwInactive, `ufw allow from 10.0.0.5 to any port 8080 proto tcp comment 'load balancer'
ufw limit in on eth1 from 10.0.0.0/8 port 1234 to 10.0.0.9 port 9000:9010
ufw allow from 10.0.0.5 to any app OpenSSH
ufw allow from 10.0.0.5 port 53 to any
ufw allow from 10.0.0.5 to any port 5353 proto udp
ufw deny from 10.0.0.5 to any port 5432 proto tcp
`))
	if err != nil {
		t.Fatal(err)
	}
	for port, want := range map[int]bool{8080: true, 9005: true, 1234: false, 22: false, 53: false, 5353: false, 5432: false} {
		if got := u.allowsFromSomewhere(port); got != want {
			t.Errorf("port %d allowed from somewhere: %v", port, got)
		}
	}
	if len(u.rules) != 0 {
		t.Errorf("long rules read as short ones: %+v", u.rules)
	}
}

// ufw on and blocking: the plan allows ports 80 and 443 for the app with a domain, and nothing
// changes without --yes. With it, root runs ufw allow for each port, with the comment of the app.
func TestSetupOpensUFW(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n", nil)
	srv := newSetupServer(t, "", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine vps-1234\nlowport 80\nufw on\n"+hostKeysFact)
	srv.ufw(t, ufwRead(ufwVerbose, ufwAdded))
	line := "change  firewall: allow ports 80 and 443 (tcp) in ufw, so visitors reach shop, in rules with the comment 'tifa shop', as root"
	out, err := runSetup(t, root, false, "")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("got %v\n%s", err, out)
	}
	if !strings.Contains(out, line) {
		t.Errorf("lacks %q:\n%s", line, out)
	}
	if !strings.Contains(out, "Visitors need ports 80 and 443 of the server open. Tifa does not change your provider's firewall or security groups: if they block them, open them there.") {
		t.Errorf("what is left to the developer:\n%s", out)
	}
	if strings.Contains(srv.calls(), "ufwallow") || strings.Contains(srv.calls(), "app tifa") {
		t.Fatalf("changed something without consent:\n%s", srv.calls())
	}

	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(srv.calls(), "ufwread root") || !strings.Contains(srv.calls(), "ufwallow root") {
		t.Errorf("calls:\n%s", srv.calls())
	}
	// ufw opens before the app moves: the app restarted on 443 has no certificate until Let's
	// Encrypt reaches it, so its health check would fail behind a closed firewall.
	if allow, move := strings.Index(srv.calls(), "ufwallow root"), strings.Index(srv.calls(), "app tifa"); move < 0 || allow > move {
		t.Errorf("ufw opens after the app moves:\n%s", srv.calls())
	}
	for _, want := range []string{"ufw allow 80/tcp comment 'tifa shop'\n", "ufw allow 443/tcp comment 'tifa shop'\n"} {
		if !strings.Contains(srv.unquoted(), want) {
			t.Errorf("the script lacks %q:\n%s", want, srv.unquoted())
		}
	}
	if !strings.Contains(out, "done    firewall: allow ports 80 and 443 (tcp) in ufw") {
		t.Errorf("output:\n%s", out)
	}
	if strings.Contains(srv.unquoted(), "22/tcp") || strings.Contains(srv.unquoted(), "ufw delete") {
		t.Errorf("touched another rule:\n%s", srv.unquoted())
	}
}

// ufw off, inactive or not there: nothing to change, and no line in the plan. Off and not there
// are read from the probe of tifa, with no root.
func TestSetupLeavesUFWThatDoesNotBlock(t *testing.T) {
	prepared := "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine vps-1234\nlowport 80\n" + hostKeysFact
	for name, tc := range map[string]struct {
		facts, read string
		root        bool // root reads ufw
	}{
		"off at boot":       {facts: "ufw off\n"},
		"not installed":     {},
		"on but inactive":   {facts: "ufw on\n", read: ufwRead(ufwInactive, "Added user rules (see 'ufw status' for running firewall):\n(None)\n"), root: true},
		"allowed by people": {facts: "ufw on\n", read: ufwRead(ufwVerbose, ufwAdded+"ufw allow 80,443/tcp\n"), root: true},
		"allow by default":  {facts: "ufw on\n", read: ufwRead(strings.Replace(ufwVerbose, "deny (incoming)", "allow (incoming)", 1), ufwAdded), root: true},
	} {
		t.Run(name, func(t *testing.T) {
			root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n", nil)
			srv := newSetupServer(t, "", prepared+tc.facts)
			srv.ufw(t, tc.read)
			out, err := runSetup(t, root, false, "", "--yes", "--all")
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			if strings.Contains(out, "change  firewall") || strings.Contains(srv.calls(), "ufwallow") {
				t.Errorf("changed ufw:\n%s\n%s", out, srv.calls())
			}
			if got := strings.Contains(srv.calls(), "ufwread root"); got != tc.root {
				t.Errorf("root read ufw: %v\n%s", got, srv.calls())
			}
			lets := strings.Contains(out, "ok      firewall: ufw lets visitors reach shop on ports 80 and 443")
			if want := strings.HasPrefix(name, "allow"); lets != want {
				t.Errorf("ok line %v:\n%s", lets, out)
			}
		})
	}
}

// Only the ports a rule of the developer does not allow yet are added. A rule that denies a port
// is theirs: setup leaves it and says what to do.
func TestSetupUFWPartlyOpen(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n", nil)
	srv := newSetupServer(t, "", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine vps-1234\nlowport 80\nufw on\n"+hostKeysFact)
	srv.ufw(t, ufwRead(ufwVerbose, ufwAdded+"ufw allow 80/tcp\n"))
	out, _ := runSetup(t, root, false, "")
	if !strings.Contains(out, "change  firewall: allow port 443 (tcp) in ufw, so visitors reach shop, in rules with the comment 'tifa shop', as root") {
		t.Errorf("plan:\n%s", out)
	}

	srv.ufw(t, ufwRead(ufwVerbose, ufwAdded+"ufw deny 80/tcp\n"))
	out, _ = runSetup(t, root, false, "")
	for _, want := range []string{
		"change  firewall: allow port 443 (tcp) in ufw",
		"firewall: let visitors reach shop on port 80 in ufw",
		"ufw denies port 80 by the rule ufw deny 80/tcp, which Tifa did not add and leaves as it is.",
		"sudo ufw delete deny 80/tcp",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
}

// When root cannot log in, setup cannot read ufw: a note says what to check, and nothing stops.
func TestSetupUFWWithoutRoot(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	srv := newSetupServer(t, "", factsOf("shop", 80, "lowport 80\nufw on\n"))
	writeFile(t, filepath.Join(srv.dir, "rootdenied"), "")
	out, _ := runSetup(t, root, false, "")
	for _, want := range []string{
		"firewall: check that ufw lets visitors reach shop on port 80 (optional)",
		"ufw is on, and only root can read its rules, but setup cannot log in to vps as root",
		"sudo ufw allow 80/tcp comment 'tifa shop'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
}

// remove deletes the rules of ufw with the comment of the app, after the plan listed them, and no
// other rule: not those of the developer, of fail2ban or of another app.
func TestRemoveDeletesTheUFWRulesOfTheApp(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n", nil)
	srv := newSetupServer(t, "", factsHTTPS("shop", "shop.example.com", "appdir yes\nports-conf yes\nufw on\n"))
	srv.ufw(t, ufwRead(ufwVerbose, ufwAdded+"ufw allow 80/tcp comment 'tifa shop'\nufw allow 443/tcp comment 'tifa shop'\nufw allow 8443/tcp comment 'tifa notes'\nufw allow 9000/tcp comment 'tifa shop extra'\n"))
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "remove  firewall: the rules of ufw for ports 80 and 443 (tcp) with the comment 'tifa shop', which setup added, as root") {
		t.Errorf("plan:\n%s", out)
	}
	if !strings.Contains(srv.calls(), "ufwdelete root") {
		t.Fatalf("calls:\n%s", srv.calls())
	}
	got := srv.unquoted()
	for _, want := range []string{"ufw delete allow 80/tcp comment 'tifa shop'\n", "ufw delete allow 443/tcp comment 'tifa shop'\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("the script lacks %q", want)
		}
	}
	for _, other := range []string{"22/tcp", "8443", "9000", "Fail2Ban", "ufw allow"} {
		if strings.Contains(got, other) {
			t.Errorf("touched %q:\n%s", other, got)
		}
	}
}

// remove of an app with no rule of its own in ufw plans nothing for ufw.
func TestRemoveWithoutUFWRules(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	srv := newSetupServer(t, "", factsOf("shop", 80, "appdir yes\nufw on\n"))
	srv.ufw(t, ufwRead(ufwVerbose, ufwAdded+"ufw allow 80/tcp\n"))
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "firewall") || strings.Contains(srv.calls(), "ufwdelete") {
		t.Errorf("planned ufw:\n%s\n%s", out, srv.calls())
	}
}

// An app that moves behind Cloudflare no longer serves visitors itself: its rules in ufw go, also
// when ufw is off at boot, since they are still in its files.
func TestSetupMoveToCloudflareDeletesUFWRules(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n", nil)
	srv := newSetupServer(t, "", factsHTTPS("shop", "shop.example.com", "appdir yes\nufw off\n"))
	srv.ufw(t, ufwRead(ufwInactive, ufwAdded+"ufw allow 80/tcp comment 'tifa shop'\nufw allow 443/tcp comment 'tifa shop'\n"))
	cfSetup(t, "example.com")
	out, _ := runSetup(t, root, false, "")
	if !strings.Contains(out, "remove  firewall: the rules of ufw for ports 80 and 443 (tcp) with the comment 'tifa shop', which shop no longer needs, as root") {
		t.Errorf("plan:\n%s", out)
	}
	if strings.Contains(srv.calls(), "ufwdelete") {
		t.Fatalf("deleted without consent:\n%s", srv.calls())
	}
	runSetup(t, root, false, "", "--yes")
	if !strings.Contains(srv.calls(), "ufwdelete root") || !strings.Contains(srv.unquoted(), "ufw delete allow 443/tcp comment 'tifa shop'\n") {
		t.Errorf("calls:\n%s", srv.calls())
	}
}

// An app that loses its domain keeps port 80 and drops the rule of 443.
func TestSetupDropsTheUFWRuleOf443(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	srv := newSetupServer(t, "", factsHTTPS("shop", "shop.example.com", "appdir yes\nlowport 80\nufw on\n"))
	srv.ufw(t, ufwRead(ufwVerbose, ufwAdded+"ufw allow 80/tcp comment 'tifa shop'\nufw allow 443/tcp comment 'tifa shop'\n"))
	out, _ := runSetup(t, root, false, "")
	if !strings.Contains(out, "remove  firewall: the rules of ufw for port 443 (tcp) with the comment 'tifa shop', which shop no longer needs, as root") || strings.Contains(out, "change  firewall") {
		t.Errorf("plan:\n%s", out)
	}
}

// After a deploy a port that does not answer may be closed at the provider, and with ufw on in
// ufw too, which setup opens.
func TestDeployNamesWhatMayBlockThePort(t *testing.T) {
	root, home, _ := serveApp(t, "80", "", map[string]string{"20261120T100000Z": "200", "20261120T110000Z": "200"})
	out, _, err := run(t, "deploy", "--dir", root)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := "visitors: http://203.0.113.7, once port 80 is open to them. Port 80 did not answer from this machine within 3 seconds: the firewall or a security group of your provider may block it. Open port 80 (tcp) there, which Tifa does not change\n"
	if !strings.HasSuffix(out, want) {
		t.Errorf("output does not end with %q:\n%s", want, out)
	}
	// fakeServer points the probe at these two files.
	writeFile(t, filepath.Join(home, ".run", "ufw.conf"), "# /etc/ufw/ufw.conf\nENABLED=yes\nLOGLEVEL=low\n")
	writeFile(t, filepath.Join(home, ".run", "ufw"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(home, ".run", "ufw"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, _, err = run(t, "deploy", "--dir", root)
	if err != nil || !strings.HasSuffix(out, "which Tifa does not change. ufw is on too: go tool tifa deploy setup lets visitors through it\n") {
		t.Errorf("%v\n%s", err, out)
	}
}
