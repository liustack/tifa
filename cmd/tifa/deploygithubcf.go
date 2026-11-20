package main

// The Cloudflare part of tifa deploy github: CI reaches the ssh of the server through the Cloudflare
// Tunnel of the server, and Cloudflare Access lets only the service token of one repository in.
// Like deploycf.go it does everything with the cf of the user, and keeps no Cloudflare token. The
// secret of the service token goes from cf to the caller, which hands it to gh.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// sshService is where the tunnel sends the ssh host name: the sshd of the server.
const sshService = "ssh://localhost:22"

// cloudflareSSHHost is the host name CI uses to reach ssh through the tunnel. Free certificates
// cover one level below a zone, so the name of the app gets -ssh and stays at the level it was:
// deploy-a.example.com becomes deploy-a-ssh.example.com, and the apex example.com becomes
// ssh.example.com.
func cloudflareSSHHost(domain, zone string) string {
	if domain == zone {
		return "ssh." + zone
	}
	first, rest, _ := strings.Cut(domain, ".")
	return first + "-ssh." + rest
}

// cfPages lists what a cf list command prints, page after page.
func cfPages[T any](ctx context.Context, args ...string) ([]T, error) {
	const perPage, maxPages = 50, 100
	var all []T
	for page := 1; page <= maxPages; page++ {
		var got []T
		if err := cfJSON(ctx, &got, append(slices.Clone(args), "--page", strconv.Itoa(page), "--per-page", strconv.Itoa(perPage))...); err != nil {
			return nil, err
		}
		all = append(all, got...)
		if len(got) < perPage {
			return all, nil
		}
	}
	return nil, fmt.Errorf("cf %s lists more than %d pages", strings.Join(args[:min(4, len(args))], " "), maxPages)
}

type sshAccess struct {
	app, domain, repo, tunnelID string
}

// sshAccessResult is what the workflow needs: the host name and the service token.
type sshAccessResult struct {
	host, clientID, clientSecret string
}

// accessName names what one repository owns in Cloudflare.
func accessName(repo string) string { return "tifa-github-" + strings.ReplaceAll(repo, "/", "-") }

// sshAccessSetup puts the ssh of the server on the tunnel under its own host name and lets the
// service token of the repository in, printing one line for each step. Running it again makes a
// new token for the repository, and keeps the rest.
func sshAccessSetup(ctx context.Context, s sshAccess, stdout io.Writer) (sshAccessResult, error) {
	say := func(format string, args ...any) { fmt.Fprintf(stdout, "cloudflare: "+format+"\n", args...) }
	zone, err := findZone(ctx, s.domain)
	if err != nil {
		return sshAccessResult{}, err
	}
	host := cloudflareSSHHost(s.domain, zone)
	how, err := ensureRoute(ctx, s.tunnelID, host, sshService)
	if err != nil {
		return sshAccessResult{}, err
	}
	say("route %s -> %s, %s", host, sshService, how)
	if how, err = ensureDNS(ctx, zone, host, s.tunnelID+".cfargotunnel.com", s.app); err != nil {
		return sshAccessResult{}, err
	}
	say("DNS %s CNAME %s.cfargotunnel.com, proxied, %s", host, s.tunnelID, how)

	name := accessName(s.repo)
	clientID, clientSecret, tokenID, err := renewServiceToken(ctx, name)
	if err != nil {
		return sshAccessResult{}, err
	}
	say("service token %s, new", name)
	policyID, how, err := ensureTokenPolicy(ctx, name, tokenID)
	if err != nil {
		return sshAccessResult{}, err
	}
	say("policy %s, only that token, %s", name, how)
	if how, err = ensureSSHApp(ctx, host, policyID); err != nil {
		return sshAccessResult{}, err
	}
	say("Access application %s, %s", host, how)
	return sshAccessResult{host: host, clientID: clientID, clientSecret: clientSecret}, nil
}

// renewServiceToken deletes the tokens called name and makes a new one that does not expire, so
// CI does not stop working a year later: run github again to replace it. It returns the client ID
// and secret CI presents, and the ID of the token that policies name.
func renewServiceToken(ctx context.Context, name string) (clientID, clientSecret, tokenID string, err error) {
	type token struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	old, err := cfPages[token](ctx, "zero-trust", "access", "service-tokens", "list", "--name", name)
	if err != nil {
		return "", "", "", err
	}
	for _, t := range old {
		if t.Name == name {
			if _, err := runCF(ctx, "zero-trust", "access", "service-tokens", "delete", t.ID); err != nil {
				return "", "", "", err
			}
		}
	}
	body, _ := json.Marshal(map[string]any{"name": name, "duration": "forever"})
	var made token
	if err := cfJSON(ctx, &made, "zero-trust", "access", "service-tokens", "create", "--body", string(body)); err != nil {
		return "", "", "", err
	}
	if made.ID == "" || made.ClientID == "" || made.ClientSecret == "" {
		return "", "", "", errors.New("cf made the service token but gave no ID or secret tifa can read")
	}
	return made.ClientID, made.ClientSecret, made.ID, nil
}

// ensureTokenPolicy makes the reusable policy called name, which lets in the service token and
// nothing else without a login (decision non_identity), or points the policy that is there at the
// new token.
func ensureTokenPolicy(ctx context.Context, name, tokenID string) (id, how string, err error) {
	body, _ := json.Marshal(map[string]any{
		"name":     name,
		"decision": "non_identity",
		"include":  []map[string]any{{"service_token": map[string]any{"token_id": tokenID}}},
	})
	policies, err := cfPages[struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}](ctx, "zero-trust", "access", "policies", "list")
	if err != nil {
		return "", "", err
	}
	for _, p := range policies {
		if p.Name == name {
			if _, err := runCF(ctx, "zero-trust", "access", "policies", "update", p.ID, "--body", string(body)); err != nil {
				return "", "", err
			}
			return p.ID, "points at the new token", nil
		}
	}
	var made struct {
		ID string `json:"id"`
	}
	if err := cfJSON(ctx, &made, "zero-trust", "access", "policies", "create", "--body", string(body)); err != nil {
		return "", "", err
	}
	if made.ID == "" {
		return "", "", errors.New("cf zero-trust access policies create gave no policy ID")
	}
	return made.ID, "created", nil
}

// ensureSSHApp makes the self-hosted Access application of the ssh host name and attaches the
// policy to it, keeping the policies of other repositories that are attached.
func ensureSSHApp(ctx context.Context, host, policyID string) (string, error) {
	apps, err := cfPages[map[string]any](ctx, "zero-trust", "access", "applications", "list", "--domain", host)
	if err != nil {
		return "", err
	}
	var app map[string]any
	for _, a := range apps {
		if a["domain"] == host {
			app = a
		}
	}
	if app == nil {
		body, _ := json.Marshal(map[string]any{
			"name":                 "tifa deploy ssh " + host,
			"type":                 "self_hosted",
			"domain":               host,
			"session_duration":     "24h",
			"app_launcher_visible": false,
			"policies":             []map[string]any{{"id": policyID, "precedence": 1}},
		})
		if _, err := runCF(ctx, "zero-trust", "access", "applications", "create", "--body", string(body)); err != nil {
			return "", err
		}
		return "created", nil
	}
	policies, _ := app["policies"].([]any)
	for _, p := range policies {
		if m, ok := p.(map[string]any); ok && m["id"] == policyID {
			return "already there", nil
		}
	}
	id, _ := app["id"].(string)
	if id == "" {
		return "", fmt.Errorf("cf gave the Access application of %s without an ID", host)
	}
	// An update replaces the whole application, so it sends back what cf printed, with the policy
	// added and the fields cf sets itself left out.
	for _, k := range []string{"id", "aud", "created_at", "updated_at"} {
		delete(app, k)
	}
	var attached []map[string]any
	for _, p := range policies {
		if m, ok := p.(map[string]any); ok {
			attached = append(attached, map[string]any{"id": m["id"], "precedence": len(attached) + 1})
		}
	}
	app["policies"] = append(attached, map[string]any{"id": policyID, "precedence": len(attached) + 1})
	body, err := json.Marshal(app)
	if err != nil {
		return "", err
	}
	if _, err := runCF(ctx, "zero-trust", "access", "applications", "update", id, "--body", string(body)); err != nil {
		return "", err
	}
	return "policy added", nil
}
