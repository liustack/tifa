package main

// The Cloudflare part of deploy.ssh cloudflare in tifa deploy setup: CI reaches the ssh of the server through the Cloudflare
// Tunnel of the server, and Cloudflare Access lets only the service token of one repository in.
// Like deploycf.go it does everything with the cf of the user, and keeps no Cloudflare token. The
// secret of the service token goes from cf to the caller, which hands it to gh.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// sshService is where the tunnel sends the ssh host name: the sshd of the server.
const sshService = "ssh://localhost:22"

// The ssh host name CI uses to reach ssh through the tunnel is made of the name of the domain and
// a random part, so a public repository, or anyone who knows the domain, cannot guess it. Free
// certificates cover one level below a zone, so the name stays at the level the domain is:
// deploy-a.example.com becomes deploy-a-ssh-<random>.example.com, and the apex example.com becomes
// ssh-<random>.example.com. Before, the name had no random part: deploy-a-ssh.example.com.

// sshHostRandom is how many characters the random part has: 12 of 32 symbols, 60 bits.
const sshHostRandom = 12

// sshHostAlphabet is lowercase base32, the characters a host name keeps in lower case.
const sshHostAlphabet = "abcdefghijklmnopqrstuvwxyz234567"

// sshHostParts is what stands before and after the random part.
func sshHostParts(domain, zone string) (prefix, suffix string) {
	if domain == zone {
		return "ssh-", "." + zone
	}
	first, rest, _ := strings.Cut(domain, ".")
	return first + "-ssh-", "." + rest
}

// newSSHHost makes a new ssh host name for the domain.
func newSSHHost(domain, zone string) (string, error) {
	prefix, suffix := sshHostParts(domain, zone)
	if n := len(prefix) + sshHostRandom; n > 63 {
		return "", fmt.Errorf("the first part of %s is too long for the ssh host name: a name of the DNS has at most 63 characters before the first dot, and this one would have %d", domain, n)
	}
	random := make([]byte, sshHostRandom)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for i, b := range random {
		random[i] = sshHostAlphabet[b%32] // 256 is a multiple of 32, so no symbol is likelier
	}
	return prefix + string(random) + suffix, nil
}

// isSSHHost says whether host is a name newSSHHost can make for the domain.
func isSSHHost(host, domain, zone string) bool {
	prefix, suffix := sshHostParts(domain, zone)
	random, ok := strings.CutPrefix(host, prefix)
	if !ok {
		return false
	}
	random, ok = strings.CutSuffix(random, suffix)
	return ok && len(random) == sshHostRandom && !strings.ContainsFunc(random, func(r rune) bool { return !strings.ContainsRune(sshHostAlphabet, r) })
}

// legacySSHHost is the name earlier versions made from the domain alone. Setup replaces it.
func legacySSHHost(domain, zone string) string {
	if domain == zone {
		return "ssh." + zone
	}
	first, rest, _ := strings.Cut(domain, ".")
	return first + "-ssh." + rest
}

// foundSSHHosts is what the tunnel and Cloudflare Access hold of the ssh host name of a domain.
type foundSSHHosts struct {
	// host is the name made by this version, empty when there is none.
	host string
	// legacy says that the name of earlier versions is still in use.
	legacy bool
}

// findSSHHosts reads the ssh host name back from what setup made: the routes of the tunnel to
// ssh://localhost:22 and the Access applications that tifa named. Both are looked at, so a name
// stays when one of them is gone. A name found in both comes first, then the smallest.
func findSSHHosts(ctx context.Context, tunnelID, domain, zone string) (foundSSHHosts, error) {
	var found foundSSHHosts
	legacy := legacySSHHost(domain, zone)
	inRoute, inApp := map[string]bool{}, map[string]bool{}
	note := func(set map[string]bool, host string) {
		switch {
		case host == legacy:
			found.legacy = true
		case isSSHHost(host, domain, zone):
			set[host] = true
		}
	}
	if tunnelID != "" {
		_, ingress, err := tunnelIngress(ctx, tunnelID)
		if err != nil {
			return found, err
		}
		for _, rule := range ingress {
			if host, _ := rule["hostname"].(string); host != "" && rule["service"] == sshService && rule["path"] == nil {
				note(inRoute, host)
			}
		}
	}
	apps, err := cfPages[map[string]any](ctx, "zero-trust", "access", "applications", "list")
	if err != nil {
		return found, err
	}
	for _, app := range apps {
		name, _ := app["name"].(string)
		if host, _ := app["domain"].(string); strings.HasPrefix(name, accessAppPrefix) && name == accessAppPrefix+host {
			note(inApp, host)
		}
	}
	// A name in both places is surer than one in a single place.
	var both, one []string
	for h := range inRoute {
		if inApp[h] {
			both = append(both, h)
		} else {
			one = append(one, h)
		}
	}
	for h := range inApp {
		if !inRoute[h] {
			one = append(one, h)
		}
	}
	slices.Sort(both)
	slices.Sort(one)
	if hosts := append(both, one...); len(hosts) > 0 {
		found.host = hosts[0]
	}
	return found, nil
}

// removeSSHHost takes away the route, the DNS record and the Access application of an ssh host
// name that tifa made. The route goes last, so a run that stopped halfway finds the name again.
func removeSSHHost(ctx context.Context, tunnelID, zone, host string) error {
	apps, err := cfPages[map[string]any](ctx, "zero-trust", "access", "applications", "list", "--domain", host)
	if err != nil {
		return err
	}
	for _, app := range apps {
		if name, _ := app["name"].(string); app["domain"] == host && name == accessAppPrefix+host {
			id, _ := app["id"].(string)
			if _, err := runCF(ctx, "zero-trust", "access", "applications", "delete", id, "--force"); err != nil {
				return err
			}
		}
	}
	if tunnelID == "" {
		return nil
	}
	if _, err := removeDNS(ctx, zone, host, tunnelID+".cfargotunnel.com", true); err != nil {
		return err
	}
	return removeRoute(ctx, tunnelID, host)
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

// accessName names what one repository owns in Cloudflare.
func accessName(repo string) string { return "tifa-github-" + strings.ReplaceAll(repo, "/", "-") }

// accessAppPrefix starts the name of the Access applications tifa makes for ssh host names.
const accessAppPrefix = "tifa deploy ssh "

// serviceTokenIDs lists the IDs of the service tokens called name.
func serviceTokenIDs(ctx context.Context, name string) ([]string, error) {
	type token struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	listed, err := cfPages[token](ctx, "zero-trust", "access", "service-tokens", "list", "--name", name)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, t := range listed {
		if t.Name == name {
			ids = append(ids, t.ID)
		}
	}
	return ids, nil
}

type cfPolicy struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Include []map[string]any `json:"include"`
}

// policyByName finds the reusable policy called name.
func policyByName(ctx context.Context, name string) (*cfPolicy, error) {
	policies, err := cfPages[cfPolicy](ctx, "zero-trust", "access", "policies", "list")
	if err != nil {
		return nil, err
	}
	for _, p := range policies {
		if p.Name == name {
			return &p, nil
		}
	}
	return nil, nil
}

// removeSSHAccess takes away what one repository owns in Cloudflare: its policy leaves the Access
// applications, and an application that has no policy left goes with its route in the tunnel and
// its DNS record. Then the policy and the service token go. It reports whether there was anything
// of the repository, and with apply false it changes nothing.
func removeSSHAccess(ctx context.Context, repo, tunnelID string, apply bool, say func(string, ...any)) (bool, error) {
	name := accessName(repo)
	ids, err := serviceTokenIDs(ctx, name)
	if err != nil {
		return false, err
	}
	policy, err := policyByName(ctx, name)
	if err != nil {
		return false, err
	}
	if len(ids) == 0 && policy == nil {
		return false, nil
	}
	if !apply {
		return true, nil
	}
	if policy != nil {
		apps, err := cfPages[map[string]any](ctx, "zero-trust", "access", "applications", "list")
		if err != nil {
			return true, err
		}
		for _, app := range apps {
			appName, _ := app["name"].(string)
			policies, _ := app["policies"].([]any)
			if !strings.HasPrefix(appName, accessAppPrefix) || !attachedPolicy(policies, policy.ID) {
				continue
			}
			id, _ := app["id"].(string)
			host, _ := app["domain"].(string)
			var rest []map[string]any
			for _, p := range policies {
				if m, ok := p.(map[string]any); ok && m["id"] != policy.ID {
					rest = append(rest, map[string]any{"id": m["id"], "precedence": len(rest) + 1})
				}
			}
			if len(rest) > 0 {
				for _, k := range []string{"id", "aud", "created_at", "updated_at"} {
					delete(app, k)
				}
				app["policies"] = rest
				b, _ := json.Marshal(app)
				if _, err := runCF(ctx, "zero-trust", "access", "applications", "update", id, "--body", string(b)); err != nil {
					return true, err
				}
				say("Access application %s no longer admits the token of %s", host, repo)
				continue
			}
			if _, err := runCF(ctx, "zero-trust", "access", "applications", "delete", id, "--force"); err != nil {
				return true, err
			}
			say("Access application %s removed", host)
			if tunnelID != "" {
				if err := removeRoute(ctx, tunnelID, host); err != nil {
					return true, err
				}
				zone, err := findZone(ctx, host)
				if err != nil {
					return true, err
				}
				if _, err := removeDNS(ctx, zone.Name, host, tunnelID+".cfargotunnel.com", true); err != nil {
					return true, err
				}
				say("route and DNS of %s removed", host)
			}
		}
		if _, err := runCF(ctx, "zero-trust", "access", "policies", "delete", policy.ID, "--force"); err != nil {
			return true, err
		}
		say("policy %s removed", name)
	}
	for _, id := range ids {
		if _, err := runCF(ctx, "zero-trust", "access", "service-tokens", "delete", id, "--force"); err != nil {
			return true, err
		}
		say("service token %s removed", name)
	}
	return true, nil
}

func attachedPolicy(policies []any, id string) bool {
	for _, p := range policies {
		if m, ok := p.(map[string]any); ok && m["id"] == id {
			return true
		}
	}
	return false
}

// renewServiceToken makes the token called name, one that does not expire so CI does not stop
// working a year later, or gives the token that is there a new secret, which ends the old one: run
// github again to replace a secret. The token keeps its ID, because Cloudflare does not delete a
// token a policy names. It returns the client ID and secret CI presents, and the ID of the token
// that policies name, and how it got them.
func renewServiceToken(ctx context.Context, name string) (clientID, clientSecret, tokenID, how string, err error) {
	type token struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	ids, err := serviceTokenIDs(ctx, name)
	if err != nil {
		return "", "", "", "", err
	}
	var made token
	switch len(ids) {
	case 0:
		how = "created"
		body, _ := json.Marshal(map[string]any{"name": name, "duration": "forever"})
		err = cfJSON(ctx, &made, "zero-trust", "access", "service-tokens", "create", "--body", string(body))
	case 1:
		how = "new secret, the old one stops working"
		err = cfJSON(ctx, &made, "zero-trust", "access", "service-tokens", "rotate", ids[0])
	default:
		err = fmt.Errorf("Cloudflare has %d service tokens called %s (%s): delete all but one in Zero Trust, Access, Service credentials", len(ids), name, strings.Join(ids, ", "))
	}
	if err != nil {
		return "", "", "", "", err
	}
	if made.ID == "" || made.ClientID == "" || made.ClientSecret == "" {
		return "", "", "", "", errors.New("cf gave no ID or secret of the service token that tifa can read")
	}
	return made.ClientID, made.ClientSecret, made.ID, how, nil
}

// ensureTokenPolicy makes the reusable policy called name, which lets in the service token and
// nothing else without a login (decision non_identity), or points the policy that is there at the
// token. tokenID is empty when the token is yet to be made.
func ensureTokenPolicy(ctx context.Context, name, tokenID string, apply bool) (id, how string, err error) {
	body, _ := json.Marshal(map[string]any{
		"name":     name,
		"decision": "non_identity",
		"include":  []map[string]any{{"service_token": map[string]any{"token_id": tokenID}}},
	})
	p, err := policyByName(ctx, name)
	if err != nil {
		return "", "", err
	}
	if p != nil {
		have, _ := json.Marshal(p.Include)
		want, _ := json.Marshal([]map[string]any{{"service_token": map[string]any{"token_id": tokenID}}})
		if tokenID != "" && string(have) == string(want) {
			return p.ID, "already there", nil
		}
		if err := cfWrite(ctx, apply, "zero-trust", "access", "policies", "update", p.ID, "--body", string(body)); err != nil {
			return "", "", err
		}
		return p.ID, "set again", nil
	}
	if !apply {
		return "", "created", nil
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
func ensureSSHApp(ctx context.Context, host, policyID string, apply bool) (string, error) {
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
			"name":                 accessAppPrefix + host,
			"type":                 "self_hosted",
			"domain":               host,
			"session_duration":     "24h",
			"app_launcher_visible": false,
			"policies":             []map[string]any{{"id": policyID, "precedence": 1}},
		})
		if err := cfWrite(ctx, apply, "zero-trust", "access", "applications", "create", "--body", string(body)); err != nil {
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
	if err := cfWrite(ctx, apply, "zero-trust", "access", "applications", "update", id, "--body", string(body)); err != nil {
		return "", err
	}
	return "policy added", nil
}
