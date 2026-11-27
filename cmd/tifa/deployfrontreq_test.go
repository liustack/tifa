package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// frontChoices is what the error of a missing deploy.front lists: each value with when to use it.
var frontChoices = []string{"cloudflare", "none", "external", "nginx"}

// Without deploy.front every deploy command stops and lists the four values. An app that never
// wrote it is told that leaving it out used to mean none.
func TestDeployNeedsFront(t *testing.T) {
	root := setupApp(t, frontLeftOut+"site:\n  url: https://shop.example.com\n", nil)
	_, err := loadDeployTargets(root, nil, &strings.Builder{})
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range append([]string{"deploy.front is not set", "front: none", "used to mean none"}, frontChoices...) {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("lacks %q:\n%v", want, err)
		}
	}
	for _, args := range [][]string{{"deploy", "status"}, {"deploy", "setup"}, {"deploy"}, {"deploy", "logs"}, {"deploy", "rollback"}, {"deploy", "remove"}} {
		_, _, err := run(t, append(args, "--dir", root)...)
		if err == nil || !strings.Contains(err.Error(), "deploy.front is not set") {
			t.Errorf("%v: got %v", args, err)
		}
	}
}

// tifa build and tifa check do not need deploy.front, and a new app's tifa.yaml, which has
// front: cloudflare and no site.url yet, passes them.
func TestCheckDeploySettingsWithoutFront(t *testing.T) {
	root := setupApp(t, frontLeftOut+"", nil)
	if err := checkDeploySettings(root); err != nil {
		t.Errorf("front left out: %v", err)
	}
	root = setupApp(t, "deploy:\n  front: cloudflare\n", nil)
	if err := checkDeploySettings(root); err != nil {
		t.Errorf("front cloudflare without site.url: %v", err)
	}
}

// front: cloudflare needs a domain. A missing site.url, an IP address and localhost stop with
// what to write instead.
func TestCloudflareNeedsADomain(t *testing.T) {
	for _, site := range []string{"", "site:\n  url: http://203.0.113.7\n", "site:\n  url: http://localhost:9527\n"} {
		root := setupApp(t, site+"deploy:\n  front: cloudflare\n", nil)
		_, err := loadDeployTargets(root, nil, &strings.Builder{})
		if err == nil {
			t.Fatalf("%q: no error", site)
		}
		for _, want := range []string{"needs a domain in site.url", "front: none", "front: external"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%q lacks %q:\n%v", site, want, err)
			}
		}
	}
}

// The setup of front: cloudflare never falls back to another front. Without cf, or without a
// login, the plan names the step it cannot do, says how to get cf, and says that not wanting
// Tifa to manage Cloudflare is a choice of front, not a reason to leave Cloudflare.
func TestSetupCloudflareWithoutCFSaysTheWays(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, ""))
	oldRun, oldHave := runCF, haveCF
	t.Cleanup(func() { runCF, haveCF = oldRun, oldHave })
	haveCF = func() bool { return false }
	out, err := runSetup(t, root, false, "")
	if err == nil {
		t.Fatalf("no error:\n%s", out)
	}
	for _, want := range []string{
		"cf is not installed: pnpm add --global cf, then cf auth login",
		"If you would rather not have Tifa manage Cloudflare through cf, for example because you manage DNS and the proxy yourself in the Cloudflare dashboard, write deploy.front: none or deploy.front: external in tifa.yaml",
		"says which one fits",
		"Setup does not change tifa.yaml for you",
		"docs/build-and-deploy/deploy.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "front: none in place") || strings.Contains(out, "falling back") {
		t.Errorf("setup fell back:\n%s", out)
	}
}

// A domain that no zone of the account cf is logged in to holds: add it to that account, or log
// cf in to the one that has it, or leave the management of Cloudflare out.
func TestSetupCloudflareDomainNotInAccount(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, ""))
	cfSetup(t, "other.org")
	out, err := runSetup(t, root, false, "")
	if err == nil {
		t.Fatalf("no error:\n%s", out)
	}
	for _, want := range []string{
		"no zone of the Cloudflare account cf is logged in to holds shop.example.com",
		"add the domain to that account",
		"https://dash.cloudflare.com/",
		"log cf in to the account that has it",
		"If you would rather not have Tifa manage Cloudflare through cf, for example because you manage DNS and the proxy yourself in the Cloudflare dashboard, write deploy.front: none or deploy.front: external in tifa.yaml",
		"front: none",
		"front: external",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
}

// server.addr of front external is where the systemd socket listens. ip is empty for every
// address of the server.
func TestExternalListen(t *testing.T) {
	ok := []struct {
		addr, ip string
		port     int
	}{
		{":9527", "", 9527},
		{"0.0.0.0:8080", "", 8080},
		{"[::]:8080", "", 8080},
		{"10.0.0.5:9527", "10.0.0.5", 9527},
		{"[fd00::5]:9527", "fd00::5", 9527},
		{":80", "", 80},
	}
	for _, c := range ok {
		ip, port, err := externalListen(c.addr, false)
		if err != nil || ip != c.ip || port != c.port {
			t.Errorf("%s: %q %d, %v", c.addr, ip, port, err)
		}
	}
	bad := []struct {
		addr    string
		several bool
		want    string
	}{
		{"127.0.0.1:9527", false, "the default, which only this machine reaches"},
		{"127.0.0.1:8080", false, "only this machine reaches"},
		{"[::1]:8080", false, "only this machine reaches"},
		{"localhost:8080", false, "only this machine reaches"},
		{"10.0.0.5:9527", true, "runs on several servers"},
		{"shop.internal:9527", false, "an IP address"},
		{":70", false, "below 80"},
		{":0", false, "port"},
		{"9527", false, "host:port"},
	}
	for _, c := range bad {
		_, _, err := externalListen(c.addr, c.several)
		if err == nil || !strings.Contains(err.Error(), "server.addr") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.addr, err, c.want)
		}
	}
	if _, _, err := externalListen(":9527", true); err != nil {
		t.Errorf("a port alone on several servers: %v", err)
	}
}

// The target of an app with front external takes its port and address from server.addr, and the
// other fronts read neither.
func TestTargetListenFromServerAddr(t *testing.T) {
	root := setupApp(t, "server:\n  addr: 10.0.0.5:8080\ndeploy:\n  front: external\n", nil)
	ts, err := loadDeployTargets(root, nil, &strings.Builder{})
	if err != nil || ts[0].fixedPort != 8080 || ts[0].listenIP != "10.0.0.5" || !ts[0].open {
		t.Errorf("%+v, %v", ts, err)
	}
	root = setupApp(t, frontLeftOut+"deploy:\n  front: external\n", nil)
	if _, err := loadDeployTargets(root, nil, &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "the default, which only this machine reaches") {
		t.Errorf("default addr: %v", err)
	}
	root = setupApp(t, "server:\n  addr: 10.0.0.5:8080\ndeploy:\n  front: none\n", nil)
	ts, err = loadDeployTargets(root, nil, &strings.Builder{})
	if err != nil || ts[0].fixedPort != 0 || ts[0].listenIP != "" || ts[0].open {
		t.Errorf("none: %+v, %v", ts, err)
	}
}

// With several servers only a port is accepted: an address that fits one server is not the
// address of the others.
func TestSeveralServersTakeOnlyAPort(t *testing.T) {
	root := setupApp(t, "server:\n  addr: 10.0.0.5:9527\ndeploy:\n  front: external\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	homeOf(t)
	_, err := loadDeployTargets(root, nil, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "server.addr is 10.0.0.5:9527") || !strings.Contains(err.Error(), `":9527"`) {
		t.Errorf("got %v", err)
	}
	root = setupApp(t, "server:\n  addr: \":9527\"\ndeploy:\n  front: external\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	ts, err := loadDeployTargets(root, nil, &strings.Builder{})
	if err != nil || len(ts) != 2 || ts[1].fixedPort != 9527 {
		t.Errorf("%+v, %v", ts, err)
	}
}

// The socket of an address listens on that address only, with FreeBind so it starts at boot
// before the address is up. The mark for harden stays, and a port alone is every address.
func TestUnitsOfListenAddress(t *testing.T) {
	every := unitsAt("shop", 9527, "", true, false, "").socket
	if !strings.Contains(every, "ListenStream=9527\n") || strings.Contains(every, "FreeBind") || !strings.Contains(every, externalMark+"\n") {
		t.Errorf("every address:\n%s", every)
	}
	one := unitsAt("shop", 9527, "", true, false, "10.0.0.5").socket
	if !strings.Contains(one, "ListenStream=10.0.0.5:9527\n") || !strings.Contains(one, "FreeBind=true\n") || !strings.Contains(one, externalMark+"\n") {
		t.Errorf("one address:\n%s", one)
	}
	six := unitsAt("shop", 9527, "", true, false, "fd00::5").socket
	if !strings.Contains(six, "ListenStream=[fd00::5]:9527\n") {
		t.Errorf("IPv6:\n%s", six)
	}
}

// Setup opens the socket on the address of server.addr, and a second run finds it right.
func TestSetupExternalAddress(t *testing.T) {
	root := setupApp(t, "server:\n  addr: 10.0.0.5:8080\ndeploy:\n  front: external\n", nil)
	fakePorts(t)
	srv := newSetupServer(t, "", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine vps-1234\nlowport 1024\n"+hostKeysFact)
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "port 8080 on 10.0.0.5, for the load balancer or proxy in front") {
		t.Errorf("output:\n%s", out)
	}
	if !strings.Contains(srv.argv(), "ListenStream=10.0.0.5:8080\n") {
		t.Errorf("the units of the app:\n%s", srv.argv())
	}
	setFacts(t, srv, factsOpenAt("shop", 8080, "10.0.0.5", "lowport 1024\nappdir yes\nusock 10.0.0.5:8080 tifa-shop.socket tifa-shop.service\nlisten LISTEN 0 4096 10.0.0.5:8080 *:*\n"))
	out, err = runSetup(t, root, false, "")
	if err != nil || !strings.Contains(out, "matches tifa.yaml") {
		t.Errorf("second run: %v\n%s", err, out)
	}
}

// Two apps with front external on one port: the second stops and names the first.
func TestSetupExternalPortTakenByAnotherApp(t *testing.T) {
	root := setupApp(t, "server:\n  addr: \":8080\"\ndeploy:\n  front: external\n", nil)
	fakePorts(t)
	srv := newSetupServer(t, "", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine vps-1234\nlowport 1024\n"+hostKeysFact+"usock [::]:8080 tifa-blog.socket tifa-blog.service\ntaken 8080\nother blog\n")
	out, err := runSetup(t, root, false, "", "--yes")
	if err == nil || !strings.Contains(err.Error(), "port 8080 of vps is taken by blog, an app Tifa set up there") || !strings.Contains(err.Error(), "server.addr") || strings.Contains(err.Error(), "deploy.port") {
		t.Errorf("got %v\n%s", err, out)
	}
	if strings.Contains(srv.calls(), "app tifa") {
		t.Errorf("made the app:\n%s", srv.calls())
	}
}

// With cloudflare, nginx and none the port is the business of setup. A server.addr that is not
// the default gets one line saying the server does not use it.
func TestSetupSaysServerAddrIsNotUsed(t *testing.T) {
	for _, front := range []string{"none", "nginx", "cloudflare"} {
		root := setupApp(t, "site:\n  url: https://shop.example.com\nserver:\n  addr: \":8080\"\ndeploy:\n  front: "+front+"\n", nil)
		newSetupServer(t, "", factsOf("shop", 9531, ""))
		out, _ := runSetup(t, root, false, "")
		want := "server.addr is :8080 in tifa.yaml, and with deploy.front " + front + " the server is not told to use it"
		if !strings.Contains(out, want) {
			t.Errorf("%s lacks %q:\n%s", front, want, out)
		}
	}
	root := setupApp(t, "deploy:\n  front: none\n", nil)
	newSetupServer(t, "", factsOf("shop", 80, ""))
	if out, _ := runSetup(t, root, false, ""); strings.Contains(out, "server.addr") {
		t.Errorf("the default address got a line:\n%s", out)
	}
}

// A SERVER_ADDR in the .env of the server does not move the app: the socket systemd passes wins.
func TestSetupSaysEnvServerAddrIsNotUsed(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: none\n", nil)
	newSetupServer(t, "", factsOf("shop", 80, "env-server-addr yes\n"))
	out, _ := runSetup(t, root, false, "")
	if !strings.Contains(out, "SERVER_ADDR is set in /srv/tifa/shop/shared/.env on vps") || !strings.Contains(out, "The systemd socket of the app wins and the value is not used") {
		t.Errorf("output:\n%s", out)
	}
	root = setupApp(t, "deploy:\n  front: none\n", nil)
	newSetupServer(t, "", factsOf("shop", 80, ""))
	if out, _ := runSetup(t, root, false, ""); strings.Contains(out, "SERVER_ADDR") {
		t.Errorf("no SERVER_ADDR, got a line:\n%s", out)
	}
}

// The probe reads whether the .env of the app sets SERVER_ADDR.
func TestProbeReadsEnvServerAddr(t *testing.T) {
	s, err := parseServerState("env-server-addr yes\n")
	if err != nil || !s.EnvServerAddr {
		t.Errorf("%+v, %v", s, err)
	}
	if !strings.Contains(probeBody, "SERVER_ADDR") {
		t.Error("the probe does not ask")
	}
}

// harden reads the address of a socket that listens on one address as it reads one on all.
func TestHardenReadsExternalOnOneAddress(t *testing.T) {
	for addr, want := range map[string]int{"10.0.0.5:8080": 8080, "[::]:8080": 8080, "*:8080": 8080, "[fd00::5]:8080": 8080} {
		if got, ok := hardenAddress(addr); !ok || got != want {
			t.Errorf("%s: %d, %v", addr, got, ok)
		}
	}
}

var _ = errors.New
var _ = context.Background
var _ = fmt.Sprint

// An app that listens on one address of the server is asked there, not on the loopback: the file
// ip next to port names it, and a deploy's health check, which every restart shares, curls it.
func TestHealthCheckAsksTheListenAddress(t *testing.T) {
	for ip, want := range map[string]string{"10.0.0.5": "http://10.0.0.5:9531/healthz", "fd00::5": "http://[fd00::5]:9531/healthz", "": "http://127.0.0.1:9531/healthz"} {
		root := t.TempDir()
		writeShopApp(t, root)
		writeFile(t, filepath.Join(root, "tifa.yaml"), withFront("server:\n  addr: \":9531\"\ndeploy:\n  front: external\n"))
		t.Setenv(deployHostEnv, "vps")
		home := t.TempDir()
		for _, dir := range []string{"shop/releases", "shop/shared"} {
			if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		writeFile(t, filepath.Join(home, "shop", "port"), "9531\n")
		if ip != "" {
			writeFile(t, filepath.Join(home, "shop", "ip"), ip+"\n")
		}
		calls := fakeServer(t, home, map[string]string{})
		oldBuild := buildForServer
		t.Cleanup(func() { buildForServer = oldBuild })
		buildForServer = func(_ context.Context, _, _, _, out string, _, _ io.Writer) error {
			return os.WriteFile(out, []byte("#!/bin/sh\n"), 0o755)
		}
		if out, stderr, err := run(t, "deploy", "--dir", root); err != nil {
			t.Fatalf("%q: %v\n%s%s", ip, err, out, stderr)
		}
		if !strings.Contains(calls(), "--max-time 5 "+want) {
			t.Errorf("ip %q: want a curl of %s:\n%s", ip, want, calls())
		}
	}
}

// Setup writes the address next to the port, and takes the file away when server.addr names none.
func TestAppScriptWritesTheListenAddress(t *testing.T) {
	with := appScript("shop", 9531, "", "10.0.0.5", true, false, false, false, 30)
	if !strings.Contains(with, "ip='10.0.0.5'") {
		t.Errorf("no ip:\n%s", with)
	}
	if without := appScript("shop", 9531, "", "", true, false, false, false, 30); !strings.Contains(without, "ip=''") {
		t.Errorf("no empty ip:\n%s", without)
	}
	if !strings.Contains(appBody, `"$d/ip"`) {
		t.Error("appBody does not write the file ip")
	}
}

// When the Cloudflare part of front: cloudflare cannot be done, setup prints the plan and stops
// before it changes anything, with --yes too: going on would leave the app with no way in.
func TestSetupCloudflareThatCannotGoOnChangesNothing(t *testing.T) {
	facts := "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine vps-1234\nlowport 1024\n" + hostKeysFact
	for name, prepare := range map[string]func(t *testing.T){
		"cf missing": func(t *testing.T) {
			oldHave := haveCF
			t.Cleanup(func() { haveCF = oldHave })
			haveCF = func() bool { return false }
		},
		"zone missing": func(t *testing.T) { cfSetup(t, "other.org") },
	} {
		t.Run(name, func(t *testing.T) {
			root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n", nil)
			fakePorts(t)
			srv := newSetupServer(t, "", facts)
			prepare(t)
			out, err := runSetup(t, root, false, "", "--yes")
			if err == nil || !strings.Contains(err.Error(), "stopped before changing anything") {
				t.Fatalf("got %v\n%s", err, out)
			}
			// The plan shows what setup would do and what is missing.
			for _, want := range []string{"add     app: /srv/tifa/shop", "domain: shop.example.com through the Cloudflare Tunnel"} {
				if !strings.Contains(out, want) {
					t.Errorf("plan lacks %q:\n%s", want, out)
				}
			}
			if strings.Contains(srv.calls(), "app tifa") || strings.Contains(srv.calls(), "root") {
				t.Errorf("setup changed the server:\n%s", srv.calls())
			}
		})
	}
}
