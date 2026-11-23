package main

import (
	"strings"
	"testing"
)

// Every name tifa deploy reads or sets starts with TIFA_DEPLOY_, except the ones other software
// fixes: the variables cloudflared reads, and the inputs of the Tailscale action.
func TestDeployNamesShareOnePrefix(t *testing.T) {
	for _, c := range [][2]string{
		{deployHostEnv, "TIFA_DEPLOY_HOST"},
		{envPurgeToken, "TIFA_DEPLOY_CF_PURGE_TOKEN"},
		{envZoneID, "TIFA_DEPLOY_CF_ZONE_ID"},
		{secretSSHKey, "TIFA_DEPLOY_SSH_KEY"},
		{secretKnownHosts, "TIFA_DEPLOY_KNOWN_HOSTS"},
		{secretHost, "TIFA_DEPLOY_HOST"},
		{secretCFID, "TIFA_DEPLOY_CF_ACCESS_ID"},
		{secretCFSecret, "TIFA_DEPLOY_CF_ACCESS_SECRET"},
		{varTSClient, "TIFA_DEPLOY_TAILSCALE_ID"},
		{varTSAudience, "TIFA_DEPLOY_TAILSCALE_AUDIENCE"},
		{varMarker, "TIFA_DEPLOY_FINGERPRINT"},
	} {
		if name, want := c[0], c[1]; name != want {
			t.Errorf("got %q, want %q", name, want)
		}
	}
}

// The workflow reads the secret names and hands Tifa the names it reads as inputs.
func TestWorkflowUsesNewNames(t *testing.T) {
	text := githubWorkflow(githubWorkflowSpec{Branch: "main", Mode: connCloudflare})
	for _, want := range []string{
		"TIFA_DEPLOY_SSH_KEY: ${{ secrets.TIFA_DEPLOY_SSH_KEY }}",
		"TIFA_DEPLOY_KNOWN_HOSTS: ${{ secrets.TIFA_DEPLOY_KNOWN_HOSTS }}",
		"TIFA_DEPLOY_HOST: ${{ secrets.TIFA_DEPLOY_HOST }}",
		"TUNNEL_SERVICE_TOKEN_ID: ${{ secrets.TIFA_DEPLOY_CF_ACCESS_ID }}",
		"TUNNEL_SERVICE_TOKEN_SECRET: ${{ secrets.TIFA_DEPLOY_CF_ACCESS_SECRET }}",
		`go tool tifa deploy --host "$TIFA_DEPLOY_HOST"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("workflow lacks %q:\n%s", want, text)
		}
	}
	ts := githubWorkflow(githubWorkflowSpec{Branch: "main", Mode: connTailscale})
	for _, want := range []string{"oauth-client-id: ${{ vars.TIFA_DEPLOY_TAILSCALE_ID }}", "audience: ${{ vars.TIFA_DEPLOY_TAILSCALE_AUDIENCE }}"} {
		if !strings.Contains(ts, want) {
			t.Errorf("workflow lacks %q", want)
		}
	}
}
