package main

// tifa deploy setup puts the app behind a Cloudflare Tunnel with the user's own Cloudflare CLI,
// cf, and its login. Tifa never holds a Cloudflare token: cf does every call, and the token of the
// tunnel goes from cf straight to the server over ssh. One tunnel serves every app of a server.
// Each app adds its route to it, a proxied DNS record, and rules scoped to its host name: the
// cache follows the Cache-Control of the app, /__tifa/ and the navigation data skip the cache, and
// the features that write into the HTML are off. Nothing that holds for the whole zone changes.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
)

// runCF runs the user's cf with args and returns what it printed. The error holds what cf printed
// on stderr, never its output, which can be a secret. Tests replace it.
var runCF = func(ctx context.Context, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, "cf", args...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, &cfError{args: args, stderr: strings.TrimSpace(stderr.String()), err: err}
	}
	return out, nil
}

// haveCF reports whether cf is on the PATH. Tests replace it.
var haveCF = func() bool {
	_, err := exec.LookPath("cf")
	return err == nil
}

// cfError is a failed cf call.
type cfError struct {
	args   []string
	stderr string
	err    error
}

func (e *cfError) Error() string {
	// The command without its flags: a --body can be long.
	var words []string
	for _, a := range e.args {
		if strings.HasPrefix(a, "-") {
			break
		}
		words = append(words, a)
	}
	msg := e.stderr
	if msg == "" {
		msg = e.err.Error()
	}
	return fmt.Sprintf("cf %s: %s", strings.Join(words, " "), msg)
}

func (e *cfError) Unwrap() error { return e.err }

// cfNotFound reports whether err is cf saying that what it was asked for does not exist.
//
// cf gives no way to tell this apart other than its text: it exits 1 for every error, a missing
// tunnel as much as a bad flag, and prints the error as a box of text on stderr, not as JSON.
// This matches the status line cf 1.0.0-beta.18 prints, "404 Not Found · HTTP <path>", and may
// need to change with cf. TestCFError holds a sample of it.
func cfNotFound(err error) bool {
	var e *cfError
	return errors.As(err, &e) && strings.Contains(e.stderr, "404 Not Found")
}

// cfJSON runs cf and decodes its output into v.
func cfJSON(ctx context.Context, v any, args ...string) error {
	out, err := runCF(ctx, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("cf %s printed what tifa cannot read: %w", strings.Join(args[:min(3, len(args))], " "), err)
	}
	return nil
}

// cfLoggedIn reports whether cf is installed and logged in.
func cfLoggedIn(ctx context.Context) bool {
	if !haveCF() {
		return false
	}
	var who struct {
		Authenticated bool `json:"authenticated"`
	}
	return cfJSON(ctx, &who, "auth", "whoami") == nil && who.Authenticated
}

// tunnelSetup is what the Cloudflare part of setup needs and gives back.
type tunnelSetup struct {
	app, domain string
	port        int
	// tunnelID is the tunnel the server runs already, from /etc/tifa/tunnel, or empty.
	tunnelID string
	// tunnelName names a tunnel setup creates: tifa-<host name>-<start of the machine id>.
	tunnelName string
	// server installs cloudflared on the server and runs it with the token, see tunnelScript.
	server func(ctx context.Context, tunnelID, token string) (string, error)
}

// cloudflareSetup puts the app behind the tunnel of its server, creating the tunnel when the
// server has none, and prints one line for each step.
func cloudflareSetup(ctx context.Context, s tunnelSetup, stdout io.Writer) error {
	say := func(format string, args ...any) { fmt.Fprintf(stdout, "cloudflare: "+format+"\n", args...) }
	zone, err := findZone(ctx, s.domain)
	if err != nil {
		return err
	}
	say("zone %s", zone)
	id, how, err := ensureTunnel(ctx, s.tunnelID, s.tunnelName)
	if err != nil {
		return err
	}
	say("tunnel %s (%s), %s", s.tunnelName, id, how)
	token, err := tunnelToken(ctx, id)
	if err != nil {
		return err
	}
	how, err = s.server(ctx, id, token)
	if err != nil {
		return err
	}
	say("cloudflared on the server, %s", how)
	service := "http://127.0.0.1:" + strconv.Itoa(s.port)
	if how, err = ensureRoute(ctx, id, s.domain, service); err != nil {
		return err
	}
	say("route %s -> %s, %s", s.domain, service, how)
	if how, err = ensureDNS(ctx, zone, s.domain, id+".cfargotunnel.com", s.app); err != nil {
		return err
	}
	say("DNS %s CNAME %s.cfargotunnel.com, proxied, %s", s.domain, id, how)
	for _, rs := range hostRules(s.domain) {
		if how, err = ensureRules(ctx, zone, rs); err != nil {
			return err
		}
		say("%s for %s, %s", rs.what, s.domain, how)
	}
	fmt.Fprintf(stdout, "the app answers at https://%s once it is deployed\n", s.domain)
	return nil
}

// zoneCandidates lists the names the zone of domain can have, longest first, down to two labels.
func zoneCandidates(domain string) []string {
	var out []string
	labels := strings.Split(domain, ".")
	for i := 0; i+2 <= len(labels); i++ {
		out = append(out, strings.Join(labels[i:], "."))
	}
	return out
}

// findZone returns the zone of the account that holds domain.
func findZone(ctx context.Context, domain string) (string, error) {
	for _, name := range zoneCandidates(domain) {
		var zones []struct {
			Name string `json:"name"`
		}
		if err := cfJSON(ctx, &zones, "zones", "list", "--name", name); err != nil {
			return "", err
		}
		if len(zones) == 1 && zones[0].Name == name {
			return name, nil
		}
	}
	return "", fmt.Errorf("no zone of your Cloudflare account holds %s: add the domain to Cloudflare first, or log cf in to the account that has it", domain)
}

type cfTunnel struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	DeletedAt *string `json:"deleted_at"`
}

// ensureTunnel returns the tunnel the server runs, the tunnel called name when the server has
// none (a setup that stopped before the server knew it), or a new remotely managed one.
func ensureTunnel(ctx context.Context, known, name string) (id, how string, err error) {
	if known != "" {
		var t cfTunnel
		err := cfJSON(ctx, &t, "tunnels", "get", known)
		switch {
		case err == nil && t.DeletedAt == nil:
			return known, "already there", nil
		case err != nil && !cfNotFound(err):
			return "", "", err
		}
	}
	var list []cfTunnel
	if err := cfJSON(ctx, &list, "tunnels", "list", "--name", name, "--is-deleted", "false"); err != nil {
		return "", "", err
	}
	if len(list) == 1 {
		return list[0].ID, "already there", nil
	}
	if len(list) > 1 {
		return "", "", fmt.Errorf("your Cloudflare account has %d tunnels called %s: delete the ones no server runs", len(list), name)
	}
	var t cfTunnel
	if err := cfJSON(ctx, &t, "tunnels", "create", "--name", name, "--config-source", "cloudflare"); err != nil {
		return "", "", err
	}
	if t.ID == "" {
		return "", "", errors.New("cf tunnels create gave no tunnel ID")
	}
	return t.ID, "created", nil
}

// tunnelToken returns the token cloudflared runs the tunnel with. It is a secret: it goes only
// to the server.
func tunnelToken(ctx context.Context, id string) (string, error) {
	out, err := runCF(ctx, "tunnels", "token", "get", id)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(out))
	var s string
	if json.Unmarshal(out, &s) == nil {
		token = s
	}
	if token == "" || strings.ContainsAny(token, " \n\"{") {
		return "", errors.New("cf tunnels token get gave no token tifa can read")
	}
	return token, nil
}

// withRoute returns the ingress rules of a tunnel with hostname going to service: the rule of
// hostname changed or added before the last rule, which matches everything else. ok is false
// when the rules are already so.
func withRoute(ingress []map[string]any, hostname, service string) (out []map[string]any, changed bool) {
	var last map[string]any
	for _, r := range ingress {
		if r["hostname"] == nil && r["path"] == nil {
			last = r // the catch-all
			continue
		}
		if r["hostname"] == hostname && r["path"] == nil {
			if r["service"] != service {
				r = cloneMap(r)
				r["service"] = service
				changed = true
			}
			hostname = "" // found
		}
		out = append(out, r)
	}
	if hostname != "" {
		out = append(out, map[string]any{"hostname": hostname, "service": service})
		changed = true
	}
	if last == nil {
		last = map[string]any{"service": "http_status:404"}
		changed = true
	}
	return append(out, last), changed
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ensureRoute adds the route of hostname to the configuration of the tunnel, keeping its other
// routes and settings.
func ensureRoute(ctx context.Context, id, hostname, service string) (string, error) {
	var got struct {
		Config map[string]any `json:"config"`
	}
	if err := cfJSON(ctx, &got, "tunnels", "config", "get", id); err != nil && !cfNotFound(err) {
		return "", err
	}
	config := got.Config
	if config == nil {
		config = map[string]any{}
	}
	var ingress []map[string]any
	if raw, ok := config["ingress"]; ok && raw != nil {
		data, _ := json.Marshal(raw)
		if err := json.Unmarshal(data, &ingress); err != nil {
			return "", fmt.Errorf("the tunnel has ingress rules tifa cannot read: %w", err)
		}
	}
	ingress, changed := withRoute(ingress, hostname, service)
	if !changed {
		return "already there", nil
	}
	config["ingress"] = ingress
	body, err := json.Marshal(map[string]any{"config": config})
	if err != nil {
		return "", err
	}
	if _, err := runCF(ctx, "tunnels", "config", "update", id, "--body", string(body)); err != nil {
		return "", err
	}
	return "added", nil
}

// ensureDNS makes hostname a proxied CNAME to target. A record of hostname that points elsewhere
// is left alone, and setup stops.
func ensureDNS(ctx context.Context, zone, hostname, target, app string) (string, error) {
	var records []struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Content string `json:"content"`
		Proxied bool   `json:"proxied"`
	}
	if err := cfJSON(ctx, &records, "dns", "records", "list", "-z", zone, "--name", hostname); err != nil {
		return "", err
	}
	for _, r := range records {
		if r.Type == "CNAME" && strings.EqualFold(r.Content, target) {
			if !r.Proxied {
				return "", fmt.Errorf("the DNS record of %s points at the tunnel but is not proxied: turn its proxy on in Cloudflare", hostname)
			}
			return "already there", nil
		}
	}
	if len(records) > 0 {
		return "", fmt.Errorf("%s already has a %s record in Cloudflare, which tifa leaves alone: delete it, or set deploy.domain to another name", hostname, records[0].Type)
	}
	body, _ := json.Marshal(map[string]any{"type": "CNAME", "name": hostname, "content": target, "proxied": true, "ttl": 1,
		"comment": "tifa deploy setup: " + app})
	if _, err := runCF(ctx, "dns", "records", "create", "-z", zone, "--body", string(body)); err != nil {
		return "", err
	}
	return "added", nil
}

// cfRule is a rule of a ruleset as setup writes it, and as cf reads it back.
type cfRule struct {
	ID               string         `json:"id,omitempty"`
	Description      string         `json:"description"`
	Expression       string         `json:"expression"`
	Action           string         `json:"action"`
	ActionParameters map[string]any `json:"action_parameters"`
	Enabled          bool           `json:"enabled"`
}

// sameRule reports whether a rule read back from cf does what want does.
func sameRule(have, want cfRule) bool {
	norm := func(m map[string]any) any {
		data, _ := json.Marshal(m)
		var v any
		_ = json.Unmarshal(data, &v)
		return v
	}
	return have.Expression == want.Expression && have.Action == want.Action && have.Enabled == want.Enabled &&
		reflect.DeepEqual(norm(have.ActionParameters), norm(want.ActionParameters))
}

// ruleSet is the rules setup keeps in one phase of the zone for a host name.
type ruleSet struct {
	what  string // what the rules do, for the output
	phase string
	rules []cfRule
}

// hostRules are the rules for hostname, from docs/deploy.md: cache what the Cache-Control of the
// app allows and nothing without one, never cache /__tifa/ or the navigation data (?_tifa=), and
// turn off the features that write into the HTML. Each rule names the host, so the other sites of
// the zone keep their settings.
func hostRules(hostname string) []ruleSet {
	host := strconv.Quote(hostname)
	skip := `(starts_with(http.request.uri.path, "/__tifa/") or http.request.uri.query contains "_tifa=")`
	prefix := "tifa deploy: " + hostname + ": "
	return []ruleSet{
		{what: "cache rules", phase: "http_request_cache_settings", rules: []cfRule{
			{
				Description: prefix + "cache what Cache-Control allows",
				Expression:  "(http.host eq " + host + " and not " + skip + ")",
				Action:      "set_cache_settings",
				ActionParameters: map[string]any{
					"cache":    true,
					"edge_ttl": map[string]any{"mode": "bypass_by_default"},
				},
				Enabled: true,
			},
			{
				Description:      prefix + "never cache /__tifa/ and navigation data",
				Expression:       "(http.host eq " + host + " and " + skip + ")",
				Action:           "set_cache_settings",
				ActionParameters: map[string]any{"cache": false},
				Enabled:          true,
			},
		}},
		{what: "HTML rewriting off", phase: "http_config_settings", rules: []cfRule{
			{
				Description: prefix + "leave the HTML alone",
				Expression:  "(http.host eq " + host + ")",
				Action:      "set_config",
				ActionParameters: map[string]any{
					"rocket_loader":     false,
					"email_obfuscation": false,
					"fonts":             false,
					"disable_zaraz":     true,
					"disable_rum":       true,
				},
				Enabled: true,
			},
		}},
	}
}

// ensureRules puts the rules of rs into the entry point ruleset of its phase, matched by their
// descriptions: missing ones are added at the end, changed ones are updated, and the other rules
// of the ruleset stay as they are.
func ensureRules(ctx context.Context, zone string, rs ruleSet) (string, error) {
	var entry struct {
		ID    string   `json:"id"`
		Rules []cfRule `json:"rules"`
	}
	err := cfJSON(ctx, &entry, "rulesets", "entrypoint", "get", rs.phase, "-z", zone)
	if cfNotFound(err) {
		body, _ := json.Marshal(map[string]any{"rules": rs.rules})
		if _, err := runCF(ctx, "rulesets", "entrypoint", "update", rs.phase, "-z", zone, "--body", string(body)); err != nil {
			return "", err
		}
		return "added", nil
	}
	if err != nil {
		return "", err
	}
	how := "already there"
	for _, want := range rs.rules {
		body, _ := json.Marshal(want)
		i := -1
		for j, have := range entry.Rules {
			if have.Description == want.Description {
				i = j
			}
		}
		switch {
		case i < 0:
			if _, err := runCF(ctx, "rulesets", "rules", "create", entry.ID, "-z", zone, "--body", string(body)); err != nil {
				return "", err
			}
			how = "added"
		case !sameRule(entry.Rules[i], want):
			if _, err := runCF(ctx, "rulesets", "rules", "update", entry.Rules[i].ID, "--ruleset-id", entry.ID, "-z", zone, "--body", string(body)); err != nil {
				return "", err
			}
			if how == "already there" {
				how = "updated"
			}
		}
	}
	return how, nil
}
