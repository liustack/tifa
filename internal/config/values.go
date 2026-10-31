package config

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/idna"
)

// value parses one setting from text, whatever layer the text came from.
type value interface {
	Set(string) error
	String() string
	kind() string
}

type stringValue struct{ p *string }

func (v stringValue) Set(s string) error {
	if s == "" {
		return errors.New("must not be empty")
	}
	*v.p = s
	return nil
}
func (v stringValue) String() string { return *v.p }
func (v stringValue) kind() string   { return "string" }

// optionalValue is text that may be empty, which turns its setting off, also when a lower layer set it.
type optionalValue struct{ p *string }

func (v optionalValue) Set(s string) error { *v.p = s; return nil }
func (v optionalValue) String() string     { return *v.p }
func (v optionalValue) kind() string       { return "string" }

// domainsValue is server.tls_domains: comma-separated host names, lowercased, empty for none.
// Let's Encrypt gives certificates for names it can reach over HTTP or TLS, so wildcards (which
// need a DNS challenge), IP addresses, ports, schemes and names without a dot are refused.
type domainsValue struct{ p *[]string }

func (v domainsValue) Set(s string) error {
	if strings.TrimSpace(s) == "" {
		*v.p = nil
		return nil
	}
	var domains []string
	for _, d := range strings.Split(s, ",") {
		d = strings.ToLower(strings.TrimSpace(d))
		// A final dot names the same domain, and browsers do not send it.
		name := strings.TrimSuffix(d, ".")
		switch {
		case d == "":
			return fmt.Errorf("empty domain in %q", s)
		case strings.Contains(d, "*"):
			return fmt.Errorf("%q: wildcard certificates need a DNS challenge, which tifa does not do, list each name", d)
		case strings.Contains(d, "://") || strings.Contains(d, "/"):
			return fmt.Errorf("%q: write the host name without a scheme or path, such as example.com", d)
		case net.ParseIP(strings.Trim(name, "[]")) != nil:
			return fmt.Errorf("%q: an IP address gets no certificate this way, use a domain name", d)
		case strings.Contains(d, ":"):
			return fmt.Errorf("%q: write the host name without a port, such as example.com", d)
		}
		if !strings.Contains(name, ".") {
			return fmt.Errorf("%q: a domain needs a dot, such as example.com", d)
		}
		if slices.Contains(strings.Split(name, "."), "") {
			return fmt.Errorf("%q: a domain has no empty parts between dots", d)
		}
		// autocert compares names in this ASCII form, and drops the ones that do not convert.
		ascii, err := idna.Lookup.ToASCII(name)
		if err != nil {
			return fmt.Errorf("%q is not a valid domain name: %v", d, err)
		}
		domains = append(domains, ascii)
	}
	*v.p = domains
	return nil
}
func (v domainsValue) String() string { return strings.Join(*v.p, ",") }
func (v domainsValue) kind() string   { return "string" }

// splitList splits comma-separated text into trimmed items. Blank text is no items, and an empty
// item between commas is an error.
func splitList(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	items := strings.Split(s, ",")
	for i, item := range items {
		items[i] = strings.TrimSpace(item)
		if items[i] == "" {
			return nil, fmt.Errorf("empty item in %q", s)
		}
	}
	return items, nil
}

// originsValue is server.trusted_origins: comma-separated origins, each a scheme, a host and
// maybe a port. A browser sends an origin in lower case and without the default port of the
// scheme, and the server compares the text, so Set writes each one that way, empty for none.
type originsValue struct{ p *[]string }

func (v originsValue) Set(s string) error {
	items, err := splitList(s)
	if err != nil {
		return err
	}
	for i, item := range items {
		u, err := url.Parse(item)
		switch {
		case err != nil:
			return fmt.Errorf("invalid origin %q: %v", item, err)
		case u.Scheme == "":
			return fmt.Errorf("%q needs a scheme, such as https://admin.example.com", item)
		case u.Scheme != "http" && u.Scheme != "https":
			return fmt.Errorf("%q: the scheme must be http or https", item)
		case u.Host == "":
			return fmt.Errorf("%q needs a host, such as https://admin.example.com", item)
		case u.User != nil:
			return fmt.Errorf("%q: an origin has no user name or password", item)
		case u.Path != "":
			return fmt.Errorf("%q: an origin has no path, not even a final slash", item)
		case u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(item, "?#"):
			return fmt.Errorf("%q: an origin has no query or fragment", item)
		}
		host := strings.ToLower(u.Host)
		if u.Port() == map[string]string{"http": "80", "https": "443"}[u.Scheme] {
			host = strings.ToLower(u.Hostname())
			if strings.Contains(u.Hostname(), ":") {
				host = "[" + host + "]"
			}
		}
		items[i] = u.Scheme + "://" + host
	}
	*v.p = items
	return nil
}
func (v originsValue) String() string { return strings.Join(*v.p, ",") }
func (v originsValue) kind() string   { return "string" }

// patternsValue is server.cross_site_paths: comma-separated http.ServeMux patterns, empty for
// none. A pattern that ServeMux would panic on, or that overlaps another one of the list in a
// way it rejects, fails here instead.
type patternsValue struct{ p *[]string }

func (v patternsValue) Set(s string) error {
	items, err := splitList(s)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	for _, item := range items {
		if err := tryPattern(mux, item); err != nil {
			return fmt.Errorf("invalid path pattern %q: %v", item, err)
		}
	}
	*v.p = items
	return nil
}
func (v patternsValue) String() string { return strings.Join(*v.p, ",") }
func (v patternsValue) kind() string   { return "string" }

// tryPattern registers pattern in mux and turns the panic of a bad one into an error.
func tryPattern(mux *http.ServeMux, pattern string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	mux.Handle(pattern, http.NotFoundHandler())
	return nil
}

// caValue is server.tls_ca, the https URL of an ACME directory.
type caValue struct{ p *string }

func (v caValue) Set(s string) error {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("invalid ACME directory %q, use an https URL such as %s", s, LetsEncrypt)
	}
	*v.p = s
	return nil
}
func (v caValue) String() string { return *v.p }
func (v caValue) kind() string   { return "string" }

// httpAddrValue is server.http_addr: an address with a port, or off. Words that read as a
// switch fail with what to write.
type httpAddrValue struct{ p *string }

func (v httpAddrValue) Set(s string) error {
	switch strings.ToLower(s) {
	case "off":
		if s != "off" {
			return fmt.Errorf("%q is not a value, to turn it off, write off", s)
		}
		*v.p = s
		return nil
	case "false", "no", "none", "disable", "disabled", "0":
		return fmt.Errorf("%q is not a value, to turn it off, write off", s)
	}
	if _, _, err := net.SplitHostPort(s); err != nil {
		return fmt.Errorf("invalid address %q, use a host and port such as :80 or 0.0.0.0:80, or off", s)
	}
	*v.p = s
	return nil
}
func (v httpAddrValue) String() string { return *v.p }
func (v httpAddrValue) kind() string   { return "string" }

// intValue is a whole number of at least min and, when max is not 0, at most max. why says what
// goes wrong above max.
type intValue struct {
	p        *int
	min, max int
	why      string
}

func (v intValue) Set(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("invalid integer %q", s)
	}
	if n < v.min {
		return fmt.Errorf("%d is below the minimum %d", n, v.min)
	}
	if v.max != 0 && n > v.max {
		return fmt.Errorf("%d is above the maximum %d: %s", n, v.max, v.why)
	}
	*v.p = n
	return nil
}
func (v intValue) String() string { return strconv.Itoa(*v.p) }
func (v intValue) kind() string   { return "int" }

type boolValue struct{ p *bool }

func (v boolValue) Set(s string) error {
	b, err := strconv.ParseBool(s) // also True and TRUE, which YAML reads as bools
	if err != nil {
		return fmt.Errorf("invalid bool %q, use true or false", s)
	}
	*v.p = b
	return nil
}
func (v boolValue) String() string { return strconv.FormatBool(*v.p) }
func (v boolValue) kind() string   { return "bool" }

type durationValue struct{ p *time.Duration }

func (v durationValue) Set(s string) error {
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return fmt.Errorf("invalid duration %q, use a positive value such as 3s or 500ms", s)
	}
	*v.p = d
	return nil
}
func (v durationValue) String() string { return v.p.String() }
func (v durationValue) kind() string   { return "duration" }

var sizeUnits = []struct {
	name string
	n    int64
}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"B", 1}}

// sizeValue is a byte count written as a whole number with an optional B, KiB, MiB or GiB unit.
type sizeValue struct {
	p   *int64
	min int64
}

func (v sizeValue) Set(s string) error {
	digits, unit := s, int64(1)
	for _, u := range sizeUnits {
		if rest, ok := strings.CutSuffix(s, u.name); ok {
			digits, unit = rest, u.n
			break
		}
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n < 0 || n > (1<<62)/unit {
		return fmt.Errorf("invalid size %q, use a whole number with B, KiB, MiB or GiB", s)
	}
	if n*unit < v.min {
		return fmt.Errorf("size %q is below the minimum %d bytes", s, v.min)
	}
	*v.p = n * unit
	return nil
}

func (v sizeValue) String() string {
	for _, u := range sizeUnits {
		if *v.p != 0 && *v.p%u.n == 0 {
			return strconv.FormatInt(*v.p/u.n, 10) + u.name
		}
	}
	return "0"
}
func (v sizeValue) kind() string { return "size" }

// rawValue keeps the flag text so that parsing errors carry the layer and key like every other layer.
type rawValue struct {
	s   string
	typ string
}

func (v *rawValue) Set(s string) error { v.s = s; return nil }
func (v *rawValue) String() string     { return v.s }
func (v *rawValue) Type() string       { return v.typ }
