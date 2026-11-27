package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// factsOpen is the probe output of a prepared server with the app set up behind deploy.front
// external on port, on every address of the server.
func factsOpen(app string, port int, extra string) string {
	u := unitsOf(app, port, "", true)
	return fmt.Sprintf("os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nport %d\nunit-socket %s\nunit-service %s\nmachine vps-1234\napt yes\n%s%s",
		port, sha(u.socket), sha(u.service), hostKeysFact, extra)
}

// deploy.port sets the port of front external, and only of that front: with the others it would
// mean nothing, and below 80 the user tifa cannot open it.
func TestDeployPortSettings(t *testing.T) {
	for yaml, want := range map[string]string{
		"deploy:\n  port: 8080\n":                     "deploy.port is for deploy.front external",
		"deploy:\n  front: nginx\n  port: 8080\n":     "deploy.port is for deploy.front external",
		"deploy:\n  front: external\n  port: 70\n":    "deploy.port is 70, below 80",
		"deploy:\n  front: external\n  port: 80000\n": "deploy.port",
	} {
		root := setupApp(t, yaml, nil)
		if _, err := loadDeployTargets(root, nil, &strings.Builder{}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", yaml, err, want)
		}
	}
	root := setupApp(t, "deploy:\n  front: external\n  port: 8080\n", nil)
	ts, err := loadDeployTargets(root, nil, &strings.Builder{})
	if err != nil || ts[0].fixedPort != 8080 || !ts[0].open {
		t.Errorf("%+v, %v", ts, err)
	}
}

// With deploy.port the app gets that port on every address, not the first one free from 9527, and
// a port from 1024 needs nothing of root.
func TestSetupDeployPort(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: external\n  port: 8080\n", nil)
	fakePorts(t)
	srv := newSetupServer(t, "", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine vps-1234\nlowport 1024\n"+hostKeysFact)
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "add     app: /srv/tifa/shop with an empty shared/.env, port 8080 on every address of the server, for the load balancer or proxy in front") {
		t.Errorf("output:\n%s", out)
	}
	if strings.Contains(out, "ip_unprivileged_port_start") || strings.Contains(srv.calls(), "ports root") {
		t.Errorf("port 8080 needs no sysctl:\n%s\n%s", out, srv.calls())
	}
	if !strings.Contains(srv.argv(), "ListenStream=8080\n") {
		t.Errorf("the units of the app:\n%s", srv.argv())
	}
}

// Port 80 or 443 behind something of the user's is plain HTTP on that one port: no socket for
// Let's Encrypt, no certificate, no rule in ufw for visitors. The user tifa may open it once
// root ran the same sysctl as for front none.
func TestSetupDeployPortBelow1024(t *testing.T) {
	for _, port := range []int{80, 443} {
		t.Run(fmt.Sprint(port), func(t *testing.T) {
			root := setupApp(t, fmt.Sprintf("site:\n  url: https://shop.example.com\ndeploy:\n  front: external\n  port: %d\n", port), nil)
			fakePorts(t)
			srv := newSetupServer(t, "", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine vps-1234\nlowport 1024\nufw on\n"+hostKeysFact)
			srv.ufw(t, "Status: active\n")
			out, err := runSetup(t, root, false, "", "--yes")
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			for _, want := range []string{
				fmt.Sprintf("change  server: let the systemd of tifa open port %d, with net.ipv4.ip_unprivileged_port_start = 80 (now 1024) in /etc/sysctl.d/60-tifa-ports.conf, as root", port),
				fmt.Sprintf("port %d on every address of the server, for the load balancer or proxy in front", port),
			} {
				if !strings.Contains(out, want) {
					t.Errorf("lacks %q:\n%s", want, out)
				}
			}
			if !strings.Contains(srv.calls(), "ports root") || strings.Contains(srv.calls(), "ufwallow") {
				t.Errorf("calls:\n%s", srv.calls())
			}
			argv := srv.argv()
			if !strings.Contains(argv, fmt.Sprintf("ListenStream=%d\n", port)) {
				t.Errorf("the units of the app:\n%s", argv)
			}
			if !strings.Contains(srv.unquoted(), "http_unit=''") {
				t.Errorf("a socket for port 80 of its own:\n%s", argv)
			}
			for _, bad := range []string{"FileDescriptorName", "SERVER_TLS_DOMAINS", "Visitors need"} {
				if strings.Contains(argv, bad) || strings.Contains(out, bad) {
					t.Errorf("has %q:\n%s\n%s", bad, out, argv)
				}
			}
		})
	}
}

// A port that another program or another app of Tifa holds stops setup before it changes anything,
// and the error says to pick another deploy.port.
func TestSetupDeployPortTaken(t *testing.T) {
	for facts, want := range map[string]string{
		"listen LISTEN 0 511 0.0.0.0:8080 0.0.0.0:* users:((\"nginx\",pid=812,fd=6))\n": "port 8080 of vps is taken by nginx (pid 812)",
		"usock [::]:8080 tifa-blog.socket tifa-blog.service\ntaken 8080\nother blog\n":  "port 8080 of vps is taken by blog, an app Tifa set up there",
	} {
		root := setupApp(t, "deploy:\n  front: external\n  port: 8080\n", nil)
		fakePorts(t)
		srv := newSetupServer(t, "", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine vps-1234\nlowport 1024\n"+hostKeysFact+facts)
		out, err := runSetup(t, root, false, "", "--yes")
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "set another deploy.port in tifa.yaml") {
			t.Errorf("got %v\n%s", err, out)
		}
		if strings.Contains(srv.calls(), "app tifa") {
			t.Errorf("made the app:\n%s", srv.calls())
		}
	}
}

// Setting deploy.port on an app that runs on another port is a move in the plan: new socket, and
// the app restarts on it and passes its check.
func TestSetupDeployPortMoves(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: external\n  port: 8080\n", nil)
	fakePorts(t)
	srv := newSetupServer(t, "", factsOpen("shop", 9527, "lowport 1024\nappdir yes\nusock [::]:9527 tifa-shop.socket tifa-shop.service\nlisten LISTEN 0 4096 *:9527 *:*\n"))
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "change  app: move shop from port 9527 to port 8080 on every address of the server, for the load balancer or proxy in front. New systemd socket, and the app restarts on it") {
		t.Errorf("output:\n%s", out)
	}
	if argv := srv.unquoted(); !strings.Contains(argv, "move='1'") || !strings.Contains(argv, "ListenStream=8080\n") {
		t.Errorf("the app script:\n%s", argv)
	}
	// Once there, setup finds it right, its own socket being no other program.
	setFacts(t, srv, factsOpen("shop", 8080, "lowport 1024\nappdir yes\nusock [::]:8080 tifa-shop.socket tifa-shop.service\nlisten LISTEN 0 4096 *:8080 *:*\n"))
	out, err = runSetup(t, root, false, "")
	if err != nil || !strings.Contains(out, "matches tifa.yaml") {
		t.Errorf("second run: %v\n%s", err, out)
	}
}

// A deploy never moves the app: when deploy.port names another port than the app has, it says to
// run setup.
func TestDeployPortChangedNeedsSetup(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n\ngo 1.27.0\n")
	writeFile(t, filepath.Join(root, "tifa.yaml"), "deploy:\n  front: external\n  port: 8080\n")
	t.Setenv(deployHostEnv, "vps")
	homeOf(t)
	home := t.TempDir()
	prepareFakeServer(t, home)
	fakeServer(t, home, nil)
	writeFile(t, filepath.Join(home, "shop", "port"), "9527\n")
	_, _, err := run(t, "deploy", "--dir", root)
	if err == nil || !strings.Contains(err.Error(), "shop has port 9527 on vps, and deploy.port is 8080: run go tool tifa deploy setup, which moves it") {
		t.Fatalf("got %v", err)
	}
}

// The sysctl that lets the user tifa open ports below 1024 goes with the last app that needs it,
// whatever its front.
func TestRemoveKeepsLowPortsForOtherApps(t *testing.T) {
	cases := []struct {
		yaml, facts string
		unports     bool
	}{
		{"deploy:\n  front: external\n  port: 80\n", factsOpen("shop", 80, "appdir yes\nports-conf yes\n"), true},
		{"deploy:\n  front: external\n  port: 80\n", factsOpen("shop", 80, "appdir yes\nports-conf yes\ntaken 443\nother blog\n"), false},
		{"site:\n  url: https://shop.example.com\ndeploy:\n", factsHTTPS("shop", "shop.example.com", "appdir yes\nports-conf yes\ntaken 2052\nother blog\n"), true},
		{"site:\n  url: https://shop.example.com\ndeploy:\n", factsHTTPS("shop", "shop.example.com", "appdir yes\nports-conf yes\ntaken 993\nother blog\n"), false},
		{"deploy:\n  front: external\n  port: 8080\n", factsOpen("shop", 8080, "appdir yes\nports-conf yes\n"), false},
	}
	for _, c := range cases {
		root := setupApp(t, c.yaml, nil)
		srv := newSetupServer(t, "", c.facts)
		out, _, err := runRemove(t, root, false, "", "--yes")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if got := strings.Contains(srv.calls(), "unports root"); got != c.unports {
			t.Errorf("%s: unports %v, want %v:\n%s", c.facts, got, c.unports, out)
		}
		if c.unports && !strings.Contains(out, "remove  server: /etc/sysctl.d/60-tifa-ports.conf, which let the systemd of tifa open ports below 1024, and the value from before it, as root") {
			t.Errorf("output:\n%s", out)
		}
		// Behind something of the user's, setup added no rule in ufw, so remove reads none.
		if strings.Contains(c.yaml, "external") && strings.Contains(srv.calls(), "ufwread") {
			t.Errorf("read ufw:\n%s", srv.calls())
		}
	}
}

// Several servers with deploy.port get that port on each, and setup does not look for another.
func TestSetupSeveralServersDeployPort(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: external\n  port: 8080\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	homeOf(t)
	fakePorts(t)
	srv := newSetupServer(t, "", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine vps-1234\nlowport 1024\napt yes\n"+hostKeysFact)
	two := srv.addHost(t, "vps2", "hostname 203.0.113.8\n", box2Facts+"lowport 1024\n")
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if n := strings.Count(srv.unquoted()+two.unquoted(), "ListenStream=8080\n"); n < 2 {
		t.Errorf("the units of port 8080 went to %d servers:\n%s", n, out)
	}
	if !strings.Contains(out, "203.0.113.8:8080") {
		t.Errorf("output:\n%s", out)
	}
}

// Setup of an app on several servers a second time keeps the port it gave: the app's own socket
// on it is no other program.
func TestSetupSeveralServersKeepsThePort(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: external\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	homeOf(t)
	fakePorts(t)
	own := "lowport 1024\nappdir yes\nusock [::]:9527 tifa-shop.socket tifa-shop.service\nlisten LISTEN 0 4096 *:9527 *:*\n"
	srv := newSetupServer(t, "", factsOpen("shop", 9527, own))
	srv.addHost(t, "vps2", "hostname 203.0.113.8\n", strings.Replace(factsOpen("shop", 9527, own), "machine vps-1234", "machine box2-5678", 1))
	out, err := runSetup(t, root, false, "")
	if err != nil || strings.Contains(out, "move") || !strings.Contains(out, "matches tifa.yaml") {
		t.Errorf("%v\n%s", err, out)
	}
}
