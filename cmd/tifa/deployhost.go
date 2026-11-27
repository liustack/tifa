package main

// Where tifa deploy finds the server. tifa.yaml is committed, so the server is not written in it:
// it comes from --host, the environment variable TIFA_DEPLOY_HOST, or TIFA_DEPLOY_HOST in the .env of the
// app, which git ignores, else from the servers setup and deploy remembered in ~/.tifa. The value is what ssh takes as a destination: a host
// name or a Host of ~/.ssh/config, or ssh://host[:port], with or without a user in front. Tifa picks
// the user it logs in as: tifa for the apps, and for the steps of root the user of the value
// through sudo, or root when the value names none.

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// deployHost is a checked value for the server of a deploy.
type deployHost struct {
	raw  string // as given, and as remembered in ~/.tifa
	name string // the host name or address, without the user and the port
	port int    // 0 when the value has none
	url  bool   // ssh://host[:port]
	// user is the admin user the value names, empty when it names none. The steps of root log in
	// as it and run through sudo, unless it is root.
	user string
	// from says where the value was read, as readDeployInput words it: the flag, the environment
	// variable or the line of .env. Empty for a value built in code.
	from string
	// fromHome says the value came from the servers of ~/.tifa.
	fromHome bool
	// knownID is the ID of the server ~/.tifa remembers at this host: what its machine must say.
	knownID string
}

// dotenvHostFrom is what from holds for a host read from the .env file at the root of the app.
const dotenvHostFrom = ".env " + deployHostEnv

// addr is the value as ssh gets it: without the user, which goes to ssh as -l.
func (h deployHost) addr() string {
	switch {
	case h.user == "":
		return h.raw
	case !h.url:
		return h.name
	}
	host := h.name
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if h.port != 0 {
		host += ":" + strconv.Itoa(h.port)
	}
	return "ssh://" + host
}

// admin is the user the steps of root log in as: the user of the value, else root.
func (h deployHost) admin() string {
	if h.user == "" {
		return "root"
	}
	return h.user
}

// sudo says the steps of root run through sudo: the value names a user other than root.
func (h deployHost) sudo() bool { return h.user != "" && h.user != "root" }

// withUser is host, another name of the same server, with the user of the value in front, so the
// admin user stays when setup moves TIFA_DEPLOY_HOST or ~/.tifa to it.
func (h deployHost) withUser(host string) string {
	if h.user == "" {
		return host
	}
	return h.user + "@" + host
}

// fromText says where the host came from, for a sentence.
func (h deployHost) fromText() string {
	switch h.from {
	case "":
		return "place it came from"
	case "--host":
		return "flag --host"
	}
	return h.from
}

// fromHomeNote is the words that follow the host in a title when it came from ~/.tifa, else none.
func (h deployHost) fromHomeNote() string {
	if h.fromHome {
		return " (from " + h.from + ")"
	}
	return ""
}

// parseDeployHost checks a value for the server. A user in front is the admin user, who runs the
// steps of root through sudo: a user name, and not tifa, which is the user of the apps. Text that
// ssh could read as an option or as a second argument is refused.
func parseDeployHost(s string) (deployHost, error) {
	if strings.Contains(s, "://") {
		return parseDeployURL(s)
	}
	user, host, hasUser := strings.Cut(s, "@")
	if !hasUser {
		user, host = "", s
	} else if err := checkAdminUser(user); err != nil {
		return deployHost{}, fmt.Errorf("%q: %w", s, err)
	}
	switch {
	case strings.HasPrefix(host, "-"):
		return deployHost{}, fmt.Errorf("%q: a host does not start with -", s)
	case !hostChars(host):
		return deployHost{}, fmt.Errorf("%q: a host is letters, digits, dots, hyphens, underscores and colons, such as vps or 203.0.113.7, or an ssh:// URL such as ssh://203.0.113.7:2222, with an admin user in front or none, such as ubuntu@203.0.113.7", s)
	}
	return deployHost{raw: s, name: host, user: user}, nil
}

// checkAdminUser checks the user in front of a host: a user name as POSIX has them, letters,
// digits, dots, underscores and hyphens, not first, and not the user of the apps.
func checkAdminUser(user string) error {
	switch {
	case user == "" || strings.HasPrefix(user, "-") || strings.ContainsFunc(user, func(r rune) bool {
		return !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || strings.ContainsRune("._-", r))
	}):
		return errors.New("the user in front of the host is letters, digits, dots, underscores and hyphens, and does not start with -, such as ubuntu@203.0.113.7")
	case user == deployUser:
		return fmt.Errorf("the user in front of the host is the admin user, who runs the steps of root with sudo, and %s is the user Tifa makes for the apps: name the user you log in to the server with, such as ubuntu, or none to log in as root", deployUser)
	}
	return nil
}

func hostChars(s string) bool {
	return s != "" && !strings.ContainsFunc(s, func(r rune) bool {
		return !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || strings.ContainsRune(".-_:", r))
	})
}

func parseDeployURL(s string) (deployHost, error) {
	u, err := url.Parse(s)
	switch {
	case err != nil:
		return deployHost{}, fmt.Errorf("%q: not a valid ssh:// URL: %v", s, err)
	case u.Scheme != "ssh":
		return deployHost{}, fmt.Errorf("%q: the only URL is ssh://host[:port], such as ssh://203.0.113.7:2222", s)
	case u.User != nil:
		if _, has := u.User.Password(); has {
			return deployHost{}, fmt.Errorf("%q: an ssh:// URL here has no password: ssh logs in with your keys", s)
		}
		if err := checkAdminUser(u.User.Username()); err != nil {
			return deployHost{}, fmt.Errorf("%q: %w", s, err)
		}
	case u.Path != "" && u.Path != "/", u.Opaque != "":
		return deployHost{}, fmt.Errorf("%q: an ssh:// URL here is the host and the port only, with no path", s)
	case u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(s, "?#"):
		return deployHost{}, fmt.Errorf("%q: an ssh:// URL here is the host and the port only, with no query or fragment", s)
	}
	name := u.Hostname()
	switch {
	case name == "":
		return deployHost{}, fmt.Errorf("%q: the URL has no host name, such as ssh://203.0.113.7:2222", s)
	case strings.HasPrefix(name, "-"):
		return deployHost{}, fmt.Errorf("%q: a host does not start with -", s)
	case net.ParseIP(name) == nil && !hostChars(name):
		return deployHost{}, fmt.Errorf("%q: a host is letters, digits, dots, hyphens, underscores and colons", s)
	}
	h := deployHost{raw: s, name: name, url: true}
	if u.User != nil {
		h.user = u.User.Username()
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return deployHost{}, fmt.Errorf("%q: the port is a number from 1 to 65535", s)
		}
		h.port = n
	}
	return h, nil
}

// deployHostURL is the target CI connects to: ssh://host:port, always with the port, so the
// workflow needs no Port line of its own.
func deployHostURL(host string, port int) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "ssh://" + host + ":" + strconv.Itoa(port)
}

// parseDeployHosts checks the values that name the servers of an app: each value is one host or
// several separated by commas, and the flag may be given more than once. The first host is the
// first server, which runs deploy.before_start and, with deploy.front nginx, the nginx visitors
// come through. An empty entry, and a server named twice (with or without a user), are refused.
func parseDeployHosts(values []string) ([]deployHost, error) {
	var hosts []deployHost
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				return nil, fmt.Errorf("%q has an empty entry: separate the hosts with one comma, such as vps1,vps2", v)
			}
			h, err := parseDeployHost(part)
			if err != nil {
				return nil, err
			}
			for _, o := range hosts {
				if o.addr() == h.addr() || (o.name == h.name && o.port == h.port) {
					return nil, fmt.Errorf("%q names the server %s twice: name each server once", v, h.name)
				}
			}
			hosts = append(hosts, h)
		}
	}
	return hosts, nil
}

// readDeployHosts finds the servers of the app as readDeployHost finds one: the flags, else
// TIFA_DEPLOY_HOST in the environment, else in .env, else ~/.tifa. Each of the first three may
// name several servers separated by commas.
func readDeployHosts(root, app string, flagValues []string, notice io.Writer) ([]deployHost, error) {
	value, from, err := readDeployInput(root, "--host", strings.Join(flagValues, ","), deployHostEnv)
	if err != nil {
		return nil, err
	}
	if value == "" {
		if os.Getenv(envGitHubActions) == "true" {
			return nil, fmt.Errorf("%s is empty: the secret %[1]s is missing in the repository. Run go tool tifa deploy setup on your machine to set it", deployHostEnv)
		}
		return homeDeployHosts(root, app, notice)
	}
	hosts, err := parseDeployHosts([]string{value})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", from, err)
	}
	for i := range hosts {
		hosts[i].from = from
	}
	return hosts, nil
}

// noServerError is what the commands say when nothing names the server. It says where to name
// one, and that setup remembers it.
func noServerError() error {
	return fmt.Errorf("no server to deploy to: say where it is in one of three ways, the first one found is used:\n"+
		"  1. the flag --host\n"+
		"  2. the environment variable %[1]s\n"+
		"  3. a line in the file .env at the root of the app (git ignores it, so the address stays out of the repository): %[1]s=ssh://203.0.113.7:22\n"+
		"The value is an ssh alias (a Host of ~/.ssh/config) or ssh://host[:port]. When root cannot log in, put the user you log in with in front, such as ubuntu@203.0.113.7: it needs passwordless sudo.\n"+
		"go tool tifa deploy setup and each deploy that works remember the server in %[2]s, and the next app of this machine needs no line", deployHostEnv, shownServersPath())
}

// homeDeployHosts takes the servers from the file of ~/.tifa when nothing else names one: the
// servers that list the app with the same list of hosts, in that order, else what homeDeployHost
// finds.
func homeDeployHosts(root, app string, notice io.Writer) ([]deployHost, error) {
	servers, err := readServers()
	if err == nil {
		if group, ok := groupOf(servers, root); ok {
			var hosts []deployHost
			for _, g := range group {
				h, err := parseDeployHost(g.host)
				if err != nil {
					return nil, fmt.Errorf("%s: the host of %s: %w", shownServersPath(), g.id, err)
				}
				h.from, h.fromHome, h.knownID = shownServersPath(), true, g.id
				hosts = append(hosts, h)
			}
			names := make([]string, len(group))
			for i, g := range group {
				names[i] = g.host
			}
			fmt.Fprintf(notice, "%s is set up on %s, from %s. To use other servers for it, put %s=<host>,<host> in the .env at the root of the app, or pass --host.\n",
				app, strings.Join(names, ", "), shownServersPath(), deployHostEnv)
			return hosts, nil
		}
	}
	h, err := homeDeployHost(root, app, notice)
	if err != nil {
		return nil, err
	}
	return []deployHost{h}, nil
}

// homeDeployHost takes the server from the file of ~/.tifa when nothing else names one: the server
// that lists the app, else the only one. With several and no way to tell, it stops and lists them.
func homeDeployHost(root, app string, notice io.Writer) (deployHost, error) {
	servers, err := readServers()
	if err != nil {
		return deployHost{}, fmt.Errorf("no server is named for %s, and ~/.tifa could not be read: %w\nName one with the flag --host or a line %s=<host> in the .env at the root of the app", app, err, deployHostEnv)
	}
	where := shownServersPath()
	switch s, host, ok := pickServer(servers, root); {
	case ok:
		h, err := parseDeployHost(host)
		if err != nil {
			return deployHost{}, fmt.Errorf("%s: the host of %s: %w", where, s.ID, err)
		}
		h.from, h.fromHome, h.knownID = where, true, s.ID
		if _, listed := s.appAt(root); listed {
			fmt.Fprintf(notice, "%s is set up on %s (%s), from %s. To use another server for it, put %s=<host> in the .env at the root of the app, or pass --host.\n",
				app, s.name(), host, where, deployHostEnv)
		} else {
			fmt.Fprintf(notice, "No server is named for %s, so tifa uses %s (%s) from %s. To name one yourself, put %s=%s (or another host) in the .env at the root of the app, or pass --host.\n",
				app, s.name(), host, where, deployHostEnv, host)
		}
		return h, nil
	case len(servers) == 0:
		return deployHost{}, noServerError()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "no server is named for %s, and %s lists several:\n", app, where)
	for _, s := range servers {
		fmt.Fprintf(&b, "  %s  %s  apps: %s\n", s.name(), s.Host, s.appNames())
	}
	fmt.Fprintf(&b, "Pick one: put the line %s=<host> in the .env at the root of the app (git ignores it), with the host of one of them, or pass --host", deployHostEnv)
	return deployHost{}, errors.New(b.String())
}

// checkResolvedHost checks the address ssh -G printed before it goes into a secret and a file.
func checkResolvedHost(s string) error {
	if strings.Contains(s, "://") || strings.Contains(s, "@") {
		return fmt.Errorf("%q is not a host name or an address", s)
	}
	_, err := parseDeployHost(s)
	return err
}
