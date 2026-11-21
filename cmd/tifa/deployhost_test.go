package main

import (
	"strings"
	"testing"
)

// The server of a deploy is an ssh alias or a host name, or ssh://host[:port]. Tifa picks the
// user, so a user, a path, a query and any other scheme are refused with a reason.
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
	} {
		got, err := parseDeployHost(in)
		if err != nil || got != want {
			t.Errorf("%q: %+v, %v, want %+v", in, got, err, want)
		}
	}
	for in, want := range map[string]string{
		"root@vps":               "user",
		"ssh://root@vps":         "user",
		"ssh://tifa@vps:2222":    "user",
		"ssh://vps/path":         "path",
		"ssh://vps?x=1":          "query",
		"ssh://vps#x":            "query",
		"http://vps":             "ssh://",
		"sftp://vps":             "ssh://",
		"ssh://":                 "host name",
		"ssh://vps:0":            "port",
		"ssh://vps:99999":        "port",
		"ssh://vps:abc":          "port",
		"ssh://-oProxyCommand=x": "-",
		"-oProxyCommand=x":       "-",
		"a b":                    "letters",
		"vps;rm":                 "letters",
	} {
		_, err := parseDeployHost(in)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want an error with %q", in, err, want)
		}
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
