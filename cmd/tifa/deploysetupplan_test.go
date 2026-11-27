package main

import (
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"
)

// planExit is the exit status tifa ends with for the error a command returned.
func planExit(err error) int {
	var exit *exitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.code
	}
	return 1
}

type planJSON struct {
	App     string `json:"app"`
	Matches bool   `json:"matches"`
	Servers []struct {
		Host  string `json:"host"`
		Steps []struct {
			Op   string `json:"op"`
			Text string `json:"text"`
			Why  string `json:"why"`
		} `json:"steps"`
		Notes []struct {
			What string `json:"what"`
			Why  string `json:"why"`
		} `json:"notes"`
		Matches bool `json:"matches"`
	} `json:"servers"`
}

func readPlanJSON(t *testing.T, out string) planJSON {
	t.Helper()
	var p planJSON
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("not one JSON object: %v\n%s", err, out)
	}
	return p
}

// A server that matches tifa.yaml: exit 0, and no script that changes anything runs.
func TestSetupPlanMatches(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	srv := newSetupServer(t, "", factsOf("shop", 80, ""))
	out, _, err := runSetupIO(t, root, false, "", "--plan")
	if planExit(err) != 0 || !strings.Contains(out, "shop on vps matches tifa.yaml") {
		t.Fatalf("exit %d: %v\n%s", planExit(err), err, out)
	}
	if strings.Contains(srv.calls(), "app ") || strings.Contains(srv.calls(), "server ") {
		t.Errorf("a change script ran:\n%s", srv.calls())
	}
	out, _, err = runSetupIO(t, root, false, "", "--plan", "--json")
	p := readPlanJSON(t, out)
	if planExit(err) != 0 || !p.Matches || len(p.Servers) != 1 || !p.Servers[0].Matches || len(p.Servers[0].Steps) != 0 || p.App != "shop" || p.Servers[0].Host != "vps" {
		t.Errorf("exit %d: %+v\n%s", planExit(err), p, out)
	}
	out, _, _ = runSetupIO(t, root, false, "", "--plan", "--json", "--all")
	if p = readPlanJSON(t, out); len(p.Servers[0].Steps) == 0 || p.Servers[0].Steps[0].Op != "ok" || !p.Matches {
		t.Errorf("--all lists the ok steps:\n%s", out)
	}
}

// A pending change: exit 2, the step is in the output, and nothing is applied, also on a terminal.
func TestSetupPlanPending(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	srv := newSetupServer(t, "", freshFacts)
	srv.deny(t)
	for _, terminal := range []bool{false, true} {
		out, _, err := runSetupIO(t, root, terminal, "", "--plan")
		if planExit(err) != 2 || !strings.Contains(out, "add     server: the user tifa") || strings.Contains(out, "Apply this plan?") {
			t.Fatalf("terminal %v, exit %d: %v\n%s", terminal, planExit(err), err, out)
		}
	}
	if strings.Contains(srv.calls(), "server root") || strings.Contains(srv.calls(), "app tifa") {
		t.Errorf("something ran:\n%s", srv.calls())
	}
	out, _, err := runSetupIO(t, root, false, "", "--plan", "--json")
	p := readPlanJSON(t, out)
	if planExit(err) != 2 || p.Matches || p.Servers[0].Matches || p.Servers[0].Steps[0].Op != "add" || !strings.HasPrefix(p.Servers[0].Steps[0].Text, "server: the user tifa") {
		t.Errorf("exit %d: %+v\n%s", planExit(err), p, out)
	}
}

// Without cf the Cloudflare part is a step setup cannot do: exit 2, and cf is not installed, also
// on a terminal where setup itself would offer to.
func TestSetupPlanMissingCF(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, ""))
	oldLook := lookProgram
	t.Cleanup(func() { lookProgram = oldLook })
	lookProgram = func(name string) (string, error) {
		if name == "pnpm" {
			return "/usr/bin/pnpm", nil
		}
		return oldLook(name)
	}
	// setupApp makes any program run on the terminal fail the test.
	for _, terminal := range []bool{false, true} {
		out, _, err := runSetupIO(t, root, terminal, "", "--plan")
		if planExit(err) != 2 || !strings.Contains(out, "1. domain: shop.example.com through the Cloudflare Tunnel") || strings.Contains(out, "install cf") {
			t.Fatalf("terminal %v, exit %d: %v\n%s", terminal, planExit(err), err, out)
		}
	}
	out, _, err := runSetupIO(t, root, false, "", "--plan", "--json")
	p := readPlanJSON(t, out)
	var cannot string
	for _, s := range p.Servers[0].Steps {
		if s.Op == "cannot" {
			cannot = s.Why
		}
	}
	if planExit(err) != 2 || !strings.Contains(cannot, "cf is not installed") {
		t.Errorf("exit %d:\n%s", planExit(err), out)
	}
}

// Failing to build the plan is exit 1, not 2: no front, a server that cannot be reached.
func TestSetupPlanFailures(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	srv := newSetupServer(t, "", freshFacts)
	srv.deny(t)
	writeFile(t, srv.dir+"/rootdenied", "")
	out, _, err := runSetupIO(t, root, false, "", "--plan", "--json")
	if planExit(err) != 1 || err == nil || strings.TrimSpace(out) != "" {
		t.Errorf("unreachable: exit %d, %v\n%s", planExit(err), err, out)
	}
	root = setupApp(t, frontLeftOut+"deploy:\n", nil)
	if _, _, err = runSetupIO(t, root, false, "", "--plan"); planExit(err) != 1 || err == nil {
		t.Errorf("no front: exit %d, %v", planExit(err), err)
	}
}

// An advice alone is a note, and does not make the plan differ from tifa.yaml.
func TestSetupPlanNotesAlone(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	newSetupServer(t, "", factsOf("shop", 80, "ssh-password yes\n"))
	out, _, err := runSetupIO(t, root, false, "", "--plan", "--json")
	p := readPlanJSON(t, out)
	if planExit(err) != 0 || !p.Matches || len(p.Servers[0].Notes) != 1 || !strings.Contains(p.Servers[0].Notes[0].What, "accepts passwords") {
		t.Errorf("exit %d:\n%s", planExit(err), out)
	}
}

// Several servers: one entry for each, the worst exit status wins, a server that fails fails the plan.
func TestSetupPlanSeveralServers(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: external\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	homeOf(t)
	fakePorts(t)
	srv := newSetupServer(t, "", factsOf("shop", 9527, "appdir yes\n"))
	srv.addHost(t, "vps2", "hostname 203.0.113.8\n", box2Facts)
	out, _, err := runSetupIO(t, root, false, "", "--plan", "--json")
	p := readPlanJSON(t, out)
	if planExit(err) != 2 || len(p.Servers) != 2 || p.Servers[0].Host != "vps" || p.Servers[1].Host != "vps2" || p.Matches || p.Servers[1].Matches || len(p.Servers[1].Steps) == 0 {
		t.Fatalf("exit %d: %+v\n%s", planExit(err), p, out)
	}
	for _, s := range p.Servers {
		if s.Steps == nil || s.Notes == nil {
			t.Errorf("steps and notes are arrays, never null:\n%s", out)
		}
	}
	text, _, err := runSetupIO(t, root, false, "", "--plan")
	if planExit(err) != 2 || !strings.Contains(text, "Plan for shop on vps (1 of 2)") || !strings.Contains(text, "Plan for shop on vps2 (2 of 2)") {
		t.Errorf("exit %d:\n%s", planExit(err), text)
	}
	if strings.Contains(srv.calls(), "app tifa") {
		t.Errorf("something ran:\n%s", srv.calls())
	}
}

// --plan only looks, so --yes and --cache-purge-token contradict it, and --json is only for --plan.
func TestSetupPlanFlagsRefused(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	srv := newSetupServer(t, "", freshFacts)
	if _, _, err := runSetupIO(t, root, false, "", "--plan", "--yes"); err == nil || !strings.Contains(err.Error(), "--plan") || !strings.Contains(err.Error(), "--yes") || planExit(err) != 1 {
		t.Errorf("--plan --yes: %v", err)
	}
	if _, _, err := runSetupIO(t, root, false, "", "--json"); err == nil || !strings.Contains(err.Error(), "add --plan") || planExit(err) != 1 {
		t.Errorf("--json: %v", err)
	}
	// --cache-purge-token asks for the token, on a terminal or on a page in the browser.
	cfRoot := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n", nil)
	if _, _, err := runSetupIO(t, cfRoot, false, "", "--plan", "--cache-purge-token"); err == nil || !strings.Contains(err.Error(), "--plan") || !strings.Contains(err.Error(), "--cache-purge-token") || planExit(err) != 1 {
		t.Errorf("--plan --cache-purge-token: %v", err)
	}
	if srv.calls() != "" {
		t.Errorf("the server was asked:\n%s", srv.calls())
	}
}

// Without a terminal and without --yes the plan is printed and setup stops with an error that
// points to --plan for a run that only looks.
func TestSetupNoTerminalPointsToPlan(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	srv := newSetupServer(t, "", freshFacts)
	srv.deny(t)
	_, err := runSetup(t, root, false, "")
	if err == nil || !strings.Contains(err.Error(), "--yes") || !strings.Contains(err.Error(), "--plan") {
		t.Errorf("got %v", err)
	}
}
