package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// untunnelApp is shop on port 9531 with a Tunnel of tifa on the server, with the app moved to
// deploy.front external, and the routes of the tunnel in Cloudflare as given.
func untunnelApp(t *testing.T, routes ...map[string]any) (root string, srv *setupServer, cf *cfEverything) {
	t.Helper()
	root = setupApp(t, "server:\n  addr: \":8080\"\ndeploy:\n  front: external\n", nil)
	fakePorts(t)
	srv = newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts+"lowport 1024\nappdir yes\ncloudflared-tifa yes\nusock 127.0.0.1:9531 tifa-shop.socket tifa-shop.service\nlisten LISTEN 0 4096 127.0.0.1:9531 0.0.0.0:*\n"))
	cf = cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	ingress := []any{}
	for _, r := range routes {
		ingress = append(ingress, r)
	}
	ingress = append(ingress, map[string]any{"service": "http_status:404"})
	cf.configs["tunnel-1"] = map[string]any{"ingress": ingress}
	return root, srv, cf
}

var ownRoute = map[string]any{"hostname": "deploy-a.example.com", "service": "http://127.0.0.1:9531"}

// The last app behind Cloudflare moves to another front: the route goes, and so does the tunnel
// on the server and in the account, in the order remove uses.
func TestSetupMoveOffCloudflareRemovesTheUnusedTunnel(t *testing.T) {
	root, srv, cf := untunnelApp(t, ownRoute)
	out, err := runSetup(t, root, false, "")
	if err == nil {
		t.Fatal("applied without --yes")
	}
	for _, want := range []string{
		"remove  domain: deploy-a.example.com, its route, DNS record and rules, which tifa made",
		"remove  server: stop cloudflared, the service tifa-tunnel, delete its units and the tunnel files in /etc/tifa, and uninstall cloudflared, which setup installed, as root, since no app uses the tunnel any more",
		"remove  cloudflare: the tunnel of the server, tifa-vps-1234, since no app uses the tunnel any more",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(srv.calls(), "untunnel") || len(cf.deleted) != 0 {
		t.Fatalf("removed without consent:\n%s\n%v", srv.calls(), cf.deleted)
	}
	if out, err = runSetup(t, root, false, "", "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(srv.calls(), "untunnel root") {
		t.Errorf("the server kept the tunnel:\n%s", srv.calls())
	}
	if len(cf.tunnels) != 0 || len(cf.deleted) == 0 || cf.deleted[len(cf.deleted)-1] != "tunnels delete tunnel-1 --force" {
		t.Errorf("tunnels %v, deleted %v", cf.tunnels, cf.deleted)
	}
	// Again: nothing of the tunnel is left to see, so nothing is planned.
	setFacts(t, srv, factsOf("shop", 8080, "lowport 1024\nappdir yes\nusock [::]:8080 tifa-shop.socket tifa-shop.service\nlisten LISTEN 0 4096 *:8080 *:*\n"))
	if out, err = runSetup(t, root, false, ""); strings.Contains(out, "tunnel") || strings.Contains(out, "Cloudflare") {
		t.Errorf("second run: %v\n%s", err, out)
	}
}

// A route of another app, or the ssh host name of any app, keeps the tunnel.
func TestSetupMoveOffCloudflareKeepsAUsedTunnel(t *testing.T) {
	for name, route := range map[string]map[string]any{
		"another app": {"hostname": "notes.example.com", "service": "http://127.0.0.1:9600"},
		"ssh host":    {"hostname": "ssh-abc.example.com", "service": sshService},
	} {
		t.Run(name, func(t *testing.T) {
			root, srv, cf := untunnelApp(t, ownRoute, route)
			out, err := runSetup(t, root, false, "", "--yes")
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			if strings.Contains(out, "tifa-tunnel") || strings.Contains(srv.calls(), "untunnel") || cf.tunnels["tunnel-1"] == "" {
				t.Errorf("the tunnel went:\n%s\n%s", out, srv.calls())
			}
			if !strings.Contains(out, "remove  domain: deploy-a.example.com") {
				t.Errorf("the route of the app stayed:\n%s", out)
			}
		})
	}
}

// When the routes cannot be read, or cf is not logged in, the tunnel stays and one note says how
// to remove it.
func TestSetupMoveOffCloudflareKeepsTheTunnelItCannotJudge(t *testing.T) {
	t.Run("routes unreadable", func(t *testing.T) {
		root, srv, cf := untunnelApp(t, ownRoute)
		inner := runCF
		runCF = func(ctx context.Context, args ...string) ([]byte, error) {
			if len(args) > 1 && args[0] == "tunnels" && args[1] == "get" {
				return nil, &cfError{args: args, stderr: "503 unavailable", err: errors.New("exit status 1")}
			}
			return inner(ctx, args...)
		}
		t.Cleanup(func() { runCF = inner })
		out, _ := runSetup(t, root, false, "", "--yes")
		if strings.Contains(srv.calls(), "untunnel") || cf.tunnels["tunnel-1"] == "" {
			t.Errorf("the tunnel went:\n%s\n%s", out, srv.calls())
		}
		if !strings.Contains(out, "the Cloudflare Tunnel, which setup made for deploy.front cloudflare, stays") || !strings.Contains(out, "go tool tifa deploy setup") {
			t.Errorf("no note:\n%s", out)
		}
		t.Logf("out:\n%s", out)
	})
	t.Run("cf not logged in", func(t *testing.T) {
		root, srv, _ := untunnelApp(t, ownRoute)
		have := haveCF
		haveCF = func() bool { return false }
		t.Cleanup(func() { haveCF = have })
		out, _ := runSetup(t, root, false, "", "--yes")
		if strings.Contains(srv.calls(), "untunnel") || !strings.Contains(out, "the Cloudflare Tunnel, which setup made for deploy.front cloudflare, stays") {
			t.Errorf("out:\n%s\n%s", out, srv.calls())
		}
	})
}
