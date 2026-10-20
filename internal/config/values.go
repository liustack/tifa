package config

import (
	"errors"
	"fmt"
	"net"
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
// switch fail with what to write, as for build.pgo.
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

// pgoValue is build.pgo: auto, off or a file. Words that read as a switch, which would otherwise
// name a file, fail with what to write. Files may also give it as a bool or a number for that reason.
type pgoValue struct{ p *string }

func (v pgoValue) Set(s string) error {
	switch strings.ToLower(s) {
	case "":
		return errors.New("must not be empty")
	case "off", "false", "no", "none", "disable", "disabled", "0":
		if s != "off" {
			return fmt.Errorf("%q is not a value, to turn it off, write off", s)
		}
	case "auto", "true", "yes", "on", "1":
		if s != "auto" {
			return fmt.Errorf("%q is not a value, write auto, off or a file", s)
		}
	}
	*v.p = s
	return nil
}
func (v pgoValue) String() string { return *v.p }
func (v pgoValue) kind() string   { return "pgo" }

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
