package main

// Where tifa deploy finds the server. tifa.yaml is committed, so the server is not written in it:
// it comes from --host, the environment variable TIFA_DEPLOY_HOST, or TIFA_DEPLOY_HOST in the .env of the
// app, which git ignores, else from the servers setup remembered in ~/.tifa. The value is what ssh takes as a destination, without a user: a host
// name or a Host of ~/.ssh/config, or ssh://host[:port].

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// deployHost is a checked value for the server of a deploy.
type deployHost struct {
	raw  string // as given, and as passed to ssh
	name string // the host name or address, without the port
	port int    // 0 when the value has none
	url  bool   // ssh://host[:port]
	// from says where the value was read, as readDeployInput words it: the flag, the environment
	// variable or the line of .env. Empty for a value built in code.
	from string
	// fromHome says the value came from the servers of ~/.tifa.
	fromHome bool
}

// dotenvHostFrom is what from holds for a host read from the .env file at the root of the app.
const dotenvHostFrom = ".env " + deployHostEnv

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

// parseDeployHost checks a value for the server. The user is Tifa's to choose (root for setup,
// tifa after), so a value with one is refused. Text that ssh could read as an option or as a
// second argument is refused too.
func parseDeployHost(s string) (deployHost, error) {
	if strings.Contains(s, "://") {
		return parseDeployURL(s)
	}
	switch {
	case strings.Contains(s, "@"):
		return deployHost{}, fmt.Errorf("%q: write the host without a user, tifa deploy logs in as root for setup and as tifa after", s)
	case strings.HasPrefix(s, "-"):
		return deployHost{}, fmt.Errorf("%q: a host does not start with -", s)
	case !hostChars(s):
		return deployHost{}, fmt.Errorf("%q: a host is letters, digits, dots, hyphens, underscores and colons, such as vps or 203.0.113.7, or an ssh:// URL such as ssh://203.0.113.7:2222", s)
	}
	return deployHost{raw: s, name: s}, nil
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
		return deployHost{}, fmt.Errorf("%q: write the host without a user, tifa deploy logs in as root for setup and as tifa after", s)
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

// readDeployHost finds the server of the app: the flag, else TIFA_DEPLOY_HOST in the environment,
// else TIFA_DEPLOY_HOST in the .env file at the root of the app, else the servers tifa deploy
// setup remembered in ~/.tifa (see deployservers.go). It says where the value came from in an
// error. A server taken from ~/.tifa is announced with one line on notice.
func readDeployHost(root, app, flagValue string, notice io.Writer) (deployHost, error) {
	value, from, err := readDeployInput(root, "--host", flagValue, deployHostEnv)
	if err != nil {
		return deployHost{}, err
	}
	if value == "" {
		return homeDeployHost(app, notice)
	}
	h, err := parseDeployHost(value)
	if err != nil {
		return deployHost{}, fmt.Errorf("%s: %w", from, err)
	}
	h.from = from
	return h, nil
}

// noServerError is what the commands say when nothing names the server. It says where to name
// one, and that setup remembers it.
func noServerError() error {
	return fmt.Errorf("no server to deploy to: say where it is in one of three ways, the first one found is used:\n"+
		"  1. the flag --host\n"+
		"  2. the environment variable %[1]s\n"+
		"  3. a line in the file .env at the root of the app (git ignores it, so the address stays out of the repository): %[1]s=ssh://203.0.113.7:22\n"+
		"The value is an ssh alias (a Host of ~/.ssh/config) or ssh://host[:port], without a user.\n"+
		"go tool tifa deploy setup remembers the server in %[2]s, and the next app of this machine needs no line", deployHostEnv, shownServersPath())
}

// homeDeployHost takes the server from the file of ~/.tifa when nothing else names one: the server
// that lists the app, else the only one. With several and no way to tell, it stops and lists them.
func homeDeployHost(app string, notice io.Writer) (deployHost, error) {
	servers, err := readServers()
	if err != nil {
		return deployHost{}, fmt.Errorf("no server is named for %s, and ~/.tifa could not be read: %w\nName one with the flag --host or a line %s=<host> in the .env at the root of the app", app, err, deployHostEnv)
	}
	where := shownServersPath()
	switch s, ok := pickServer(servers, app); {
	case ok:
		h, err := parseDeployHost(s.Host)
		if err != nil {
			return deployHost{}, fmt.Errorf("%s: the host of %s: %w", where, s.ID, err)
		}
		h.from, h.fromHome = where, true
		if slices.Contains(s.Apps, app) {
			fmt.Fprintf(notice, "%s is set up on %s (%s), from %s. To use another server for it, put %s=<host> in the .env at the root of the app, or pass --host.\n",
				app, s.name(), s.Host, where, deployHostEnv)
		} else {
			fmt.Fprintf(notice, "No server is named for %s, so tifa uses %s (%s) from %s. To name one yourself, put %s=%s (or another host) in the .env at the root of the app, or pass --host.\n",
				app, s.name(), s.Host, where, deployHostEnv, s.Host)
		}
		return h, nil
	case len(servers) == 0:
		return deployHost{}, noServerError()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "no server is named for %s, and %s lists several:\n", app, where)
	for _, s := range servers {
		apps := "none"
		if len(s.Apps) > 0 {
			apps = strings.Join(s.Apps, ", ")
		}
		fmt.Fprintf(&b, "  %s  %s  apps: %s\n", s.name(), s.Host, apps)
	}
	fmt.Fprintf(&b, "Pick one: put a line such as %s=%s in the .env at the root of the app (git ignores it), or pass --host", deployHostEnv, servers[0].Host)
	return deployHost{}, errors.New(b.String())
}

// checkResolvedHost checks the address ssh -G printed before it goes into a secret and a file.
func checkResolvedHost(s string) error {
	if strings.Contains(s, "://") {
		return fmt.Errorf("%q is not a host name or an address", s)
	}
	_, err := parseDeployHost(s)
	return err
}
