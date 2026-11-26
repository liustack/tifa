package main

import (
	"strings"
	"testing"
)

// The server of a deploy is an ssh alias or a host name, or ssh://host[:port], with an admin user
// in front or none. A user that is not a user name, the user tifa, a path, a query and any other
// scheme are refused with a reason.
func TestParseDeployHost(t *testing.T) {
	for in, want := range map[string]deployHost{
		"vps":                    {raw: "vps", name: "vps"},
		"vps.example.com":        {raw: "vps.example.com", name: "vps.example.com"},
		"203.0.113.7":            {raw: "203.0.113.7", name: "203.0.113.7"},
		"my_box":                 {raw: "my_box", name: "my_box"},
		"fe80::1":                {raw: "fe80::1", name: "fe80::1"},
		"ssh://203.0.113.7":      {raw: "ssh://203.0.113.7", name: "203.0.113.7", url: true},
		"ssh://203.0.113.7:2222": {raw: "ssh://203.0.113.7:2222", name: "203.0.113.7", port: 2222, url: true},
		"ssh://vps.example.com/": {raw: "ssh://vps.example.com/", name: "vps.example.com", url: true},
		"ssh://[2001:db8::1]:22": {raw: "ssh://[2001:db8::1]:22", name: "2001:db8::1", port: 22, url: true},
		// The admin user, who runs the steps of root through sudo when root cannot log in.
		"ubuntu@203.0.113.7":              {raw: "ubuntu@203.0.113.7", name: "203.0.113.7", user: "ubuntu"},
		"ec2-user@vps":                    {raw: "ec2-user@vps", name: "vps", user: "ec2-user"},
		"root@vps":                        {raw: "root@vps", name: "vps", user: "root"},
		"a.b_c-1@vps":                     {raw: "a.b_c-1@vps", name: "vps", user: "a.b_c-1"},
		"ssh://ubuntu@203.0.113.7:2222":   {raw: "ssh://ubuntu@203.0.113.7:2222", name: "203.0.113.7", port: 2222, url: true, user: "ubuntu"},
		"ssh://ubuntu@[2001:db8::1]:22":   {raw: "ssh://ubuntu@[2001:db8::1]:22", name: "2001:db8::1", port: 22, url: true, user: "ubuntu"},
		"ssh://ec2-user@vps.example.com/": {raw: "ssh://ec2-user@vps.example.com/", name: "vps.example.com", url: true, user: "ec2-user"},
	} {
		got, err := parseDeployHost(in)
		if err != nil || got != want {
			t.Errorf("%q: %+v, %v, want %+v", in, got, err, want)
		}
	}
	for in, want := range map[string]string{
		"-oProxyCommand=x@vps":    "user",
		"a b@vps":                 "user",
		"u$er@vps":                "user",
		"@vps":                    "user",
		"a@b@vps":                 "user",
		"tifa@vps":                "tifa",
		"ssh://tifa@vps:2222":     "tifa",
		"ssh://@vps":              "user",
		"ssh://u:pw@vps":          "password",
		"ssh://%2Doops@vps":       "user",
		"ssh://a%20b@vps":         "user",
		"ubuntu@-oProxyCommand=x": "-",
		"ssh://vps/path":          "path",
		"ssh://vps?x=1":           "query",
		"ssh://vps#x":             "query",
		"http://vps":              "ssh://",
		"sftp://vps":              "ssh://",
		"ssh://":                  "host name",
		"ssh://vps:0":             "port",
		"ssh://vps:99999":         "port",
		"ssh://vps:abc":           "port",
		"ssh://-oProxyCommand=x":  "-",
		"-oProxyCommand=x":        "-",
		"a b":                     "letters",
		"vps;rm":                  "letters",
	} {
		_, err := parseDeployHost(in)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want an error with %q", in, err, want)
		}
	}
}

// ssh gets the host without the user: the user goes to ssh as -l, which is tifa for the steps of
// the deploy user and the admin user for the steps of root.
func TestDeployHostAddr(t *testing.T) {
	for in, want := range map[string]string{
		"vps":                           "vps",
		"ssh://203.0.113.7:2222":        "ssh://203.0.113.7:2222",
		"ubuntu@vps":                    "vps",
		"root@203.0.113.7":              "203.0.113.7",
		"ubuntu@fe80::1":                "fe80::1",
		"ssh://ubuntu@203.0.113.7:2222": "ssh://203.0.113.7:2222",
		"ssh://ubuntu@203.0.113.7":      "ssh://203.0.113.7",
		"ssh://ubuntu@[2001:db8::1]:22": "ssh://[2001:db8::1]:22",
	} {
		h, err := parseDeployHost(in)
		if err != nil || h.addr() != want {
			t.Errorf("%q: addr %q, %v, want %q", in, h.addr(), err, want)
		}
	}
	h, _ := parseDeployHost("vps")
	if h.admin() != "root" || h.withUser("ssh-x.example.com") != "ssh-x.example.com" {
		t.Errorf("no user: admin %q, withUser %q", h.admin(), h.withUser("ssh-x.example.com"))
	}
	h, _ = parseDeployHost("ssh://ubuntu@203.0.113.7:2222")
	if h.admin() != "ubuntu" || h.withUser("ssh-x.example.com") != "ubuntu@ssh-x.example.com" {
		t.Errorf("ubuntu: admin %q, withUser %q", h.admin(), h.withUser("ssh-x.example.com"))
	}
	// What ssh -G resolved goes into a secret: never with a user.
	if err := checkResolvedHost("ubuntu@203.0.113.7"); err == nil {
		t.Error("a resolved address with a user passed")
	}
}

// Setup passes the port to CI as part of the URL, and the mask in the log of Actions hides the
// host and not the port.
func TestDeployHostURL(t *testing.T) {
	if got := deployHostURL("203.0.113.7", 2222); got != "ssh://203.0.113.7:2222" {
		t.Errorf("got %s", got)
	}
	if got := deployHostURL("2001:db8::1", 22); got != "ssh://[2001:db8::1]:22" {
		t.Errorf("got %s", got)
	}
}
