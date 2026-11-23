package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tsApp is an app with ssh tailscale and ci github. Its server is reached by its name on the
// tailnet, port 2222, which is what CI logs in to.
type tsApp struct {
	root string
	srv  *setupServer
	gh   *fakeGH
}

func newTSApp(t *testing.T, ci bool) *tsApp {
	t.Helper()
	a := &tsApp{root: setupApp(t, "deploy:\n  connect: tailscale\n  auto: {}\n", nil)}
	a.srv = newSetupServer(t, "hostname vps.tail1234.ts.net\nport 2222\n", factsOf("shop", 9531, "ghkey acme/shop\n"))
	a.gh = newFakeGH()
	a.gh.install(t)
	return a
}

func wantAll(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("lacks %q:\n%s", w, got)
		}
	}
}

// The client ID is not secret. Setup says how to make the identity, with the subject of this
// repository, and CI is not ready until it has the client ID.
func TestSetupTailscaleCINeedsIdentity(t *testing.T) {
	a := newTSApp(t, true)
	out, err := runSetup(t, a.root, false, "", "--yes")
	if err == nil || !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("%v\n%s", err, out)
	}
	wantAll(t, out,
		"github: the Tailscale identity CI joins your tailnet with, the variables TIFA_DEPLOY_TAILSCALE_ID and TIFA_DEPLOY_TAILSCALE_AUDIENCE",
		"https://console.tailscale.com/admin/settings/trust-credentials", "https://token.actions.githubusercontent.com",
		"repo:acme/shop:ref:refs/heads/main", "auth_keys", "tag:ci", "tagOwners",
		"go tool tifa deploy setup --tailscale-client-id <client ID>")
	if len(a.gh.vars) > 1 && a.gh.vars[varTSClient] != "" {
		t.Errorf("variables %v", a.gh.vars)
	}
}

// With the client ID, setup sets two variables and no secret of Tailscale, and writes the workflow
// that joins with them. CI logs in to the name of the server on the tailnet.
func TestSetupTailscaleCI(t *testing.T) {
	a := newTSApp(t, true)
	out, err := runSetup(t, a.root, false, "", "--yes", "--tailscale-client-id", "k1b2CNTRL")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	wantAll(t, out, "add     github: the Tailscale identity CI joins your tailnet with")
	if a.gh.vars[varTSClient] != "k1b2CNTRL" || a.gh.vars[varTSAudience] != "api.tailscale.com/k1b2CNTRL" {
		t.Errorf("variables %v", a.gh.vars)
	}
	if a.gh.secrets[secretTSID] != "" || a.gh.secrets[secretTSSecret] != "" {
		t.Errorf("a Tailscale secret was set: %v", a.gh.secrets)
	}
	if a.gh.secrets[secretHost] != "ssh://vps.tail1234.ts.net:2222" || !strings.HasPrefix(a.gh.secrets[secretKnownHosts], "[vps.tail1234.ts.net]:2222 ssh-ed25519 ") {
		t.Errorf("connection: %q, %q", a.gh.secrets[secretHost], a.gh.secrets[secretKnownHosts])
	}
	data, err := os.ReadFile(filepath.Join(a.root, githubWorkflowPath))
	if err != nil {
		t.Fatal(err)
	}
	wantAll(t, string(data), "  id-token: write\n", "oauth-client-id: ${{ vars.TIFA_DEPLOY_TAILSCALE_ID }}", "audience: ${{ vars.TIFA_DEPLOY_TAILSCALE_AUDIENCE }}", "tags: tag:ci")
	if strings.Contains(string(data), "secrets.TIFA_DEPLOY_TAILSCALE") {
		t.Errorf("workflow:\n%s", data)
	}

	// A second run: nothing to do, and the same ID again changes nothing.
	out, err = runSetup(t, a.root, false, "", "--yes", "--all", "--tailscale-client-id", "k1b2CNTRL")
	if err != nil {
		t.Fatalf("second run: %v\n%s", err, out)
	}
	wantAll(t, out, "ok      github: the Tailscale identity, the variables")
	if strings.Contains(out, "  add     ") || strings.Contains(out, "  change  ") {
		t.Errorf("a second run did something:\n%s", out)
	}
	// Another client ID replaces it.
	out, err = runSetup(t, a.root, false, "", "--yes", "--tailscale-client-id", "k9z8CNTRL")
	if err != nil || a.gh.vars[varTSClient] != "k9z8CNTRL" || a.gh.vars[varTSAudience] != "api.tailscale.com/k9z8CNTRL" {
		t.Errorf("replace: %v %v\n%s", err, a.gh.vars, out)
	}
}

// On a terminal setup asks for the client ID, and an empty answer skips it.
func TestSetupTailscaleCIAsksOnTerminal(t *testing.T) {
	a := newTSApp(t, true)
	out, err := runSetup(t, a.root, true, "\n", "--yes")
	if err == nil || a.gh.vars[varTSClient] != "" {
		t.Fatalf("skipped: %v, %v\n%s", err, a.gh.vars, out)
	}
	wantAll(t, out, "Paste its client ID, or press Enter to skip", "skipped, and CI cannot join the tailnet without it")
	out, err = runSetup(t, a.root, true, "k1b2CNTRL\n", "--yes")
	if err != nil || a.gh.vars[varTSClient] != "k1b2CNTRL" {
		t.Fatalf("answered: %v, %v\n%s", err, a.gh.vars, out)
	}
}

// A Tailscale key is a secret and is never taken as the client ID, and never printed.
func TestSetupTailscaleClientIDChecks(t *testing.T) {
	a := newTSApp(t, true)
	out, err := runSetup(t, a.root, false, "", "--yes", "--tailscale-client-id", "tskey-client-abc123-SECRETSECRET")
	if err == nil || !strings.Contains(err.Error(), "secret") || strings.Contains(err.Error()+out, "SECRETSECRET") {
		t.Errorf("a key: %v\n%s", err, out)
	}
	if _, err := runSetup(t, a.root, false, "", "--yes", "--tailscale-client-id", "no spaces"); err == nil || !strings.Contains(err.Error(), "not a client ID") {
		t.Errorf("spaces: %v", err)
	}
	writeFile(t, filepath.Join(a.root, "tifa.yaml"), "deploy:\n  auto: {}\n")
	if _, err := runSetup(t, a.root, false, "", "--yes", "--tailscale-client-id", "k1"); err == nil || !strings.Contains(err.Error(), "deploy.connect") {
		t.Errorf("ssh direct: %v", err)
	}
	if len(a.gh.vars) != 0 {
		t.Errorf("variables %v", a.gh.vars)
	}
}

// An app set up by v0.2.x joins with the OAuth secrets. Setup leaves that working until the
// identity is there, then writes the workflow for it, and takes the secrets away only once the
// workflow on the default branch no longer uses them.
func TestSetupTailscaleFromOAuthSecrets(t *testing.T) {
	a := newTSApp(t, true)
	workflow := filepath.Join(a.root, githubWorkflowPath)
	oldFlow := githubWorkflow(githubWorkflowSpec{Branch: "main", Mode: connTailscale, TailscaleOAuth: true, CIName: testCIName})
	writeFile(t, workflow, oldFlow)
	a.gh.files = map[string]string{githubWorkflowPath: oldFlow}
	for _, n := range []string{secretSSHKey, secretKnownHosts, secretHost, secretTSID, secretTSSecret} {
		a.gh.secrets[n] = "set by v0.2"
	}
	a.gh.vars[varMarker] = "old"

	// Without the client ID the old workflow stays, and the secrets with it.
	out, err := runSetup(t, a.root, false, "", "--yes")
	if err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("%v\n%s", err, out)
	}
	wantAll(t, out, "the Tailscale identity CI joins your tailnet with", "CI still joins with the OAuth secrets TIFA_DEPLOY_TAILSCALE_CLIENT_ID and TIFA_DEPLOY_TAILSCALE_SECRET")
	if data, _ := os.ReadFile(workflow); string(data) != oldFlow || a.gh.secrets[secretTSID] == "" {
		t.Errorf("setup changed what CI joins with:\n%s\n%v", data, a.gh.secrets)
	}

	// With it, the workflow is written for the identity. The secrets stay while main still has the old one.
	out, err = runSetup(t, a.root, false, "", "--yes", "--tailscale-client-id", "k1b2CNTRL")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	newFlow, _ := os.ReadFile(workflow)
	wantAll(t, string(newFlow), "vars.TIFA_DEPLOY_TAILSCALE_ID", "id-token: write")
	wantAll(t, out, "github: the secrets TIFA_DEPLOY_TAILSCALE_CLIENT_ID and TIFA_DEPLOY_TAILSCALE_SECRET stay while the workflow on main hands them to the Tailscale action")
	if a.gh.secrets[secretTSID] == "" || a.gh.secrets[secretTSSecret] == "" {
		t.Errorf("the secrets went too early: %v", a.gh.secrets)
	}

	// Once the new workflow is on main, the next run removes them.
	a.gh.files[githubWorkflowPath] = string(newFlow)
	out, err = runSetup(t, a.root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	wantAll(t, out, "remove  github: the secrets TIFA_DEPLOY_TAILSCALE_CLIENT_ID and TIFA_DEPLOY_TAILSCALE_SECRET, the OAuth client of an older setup, which the workflow no longer uses")
	if _, ok := a.gh.secrets[secretTSID]; ok {
		t.Errorf("secrets %v", a.gh.secrets)
	}
	if _, ok := a.gh.secrets[secretTSSecret]; ok {
		t.Errorf("secrets %v", a.gh.secrets)
	}
}

// Remove takes the identity variables away with the secrets setup set.
func TestRemoveTailscaleIdentity(t *testing.T) {
	root := setupApp(t, "deploy:\n  connect: tailscale\n  auto: {}\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, "ghkey acme/shop\nappdir yes\n"))
	gh := newFakeGH()
	for _, n := range []string{secretSSHKey, secretKnownHosts, secretHost} {
		gh.secrets[n] = "set by setup"
	}
	gh.vars[varMarker], gh.vars[varTSClient], gh.vars[varTSAudience] = "fingerprint", "k1", "api.tailscale.com/k1"
	gh.install(t)
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err != nil || len(gh.secrets) != 0 || len(gh.vars) != 0 {
		t.Errorf("%v, left %v %v\n%s", err, gh.secrets, gh.vars, out)
	}
}
