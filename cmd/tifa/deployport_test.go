package main

import (
	"fmt"
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
