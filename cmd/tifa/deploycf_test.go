package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// fakeCF stands in for the cf of the user: an account with zones, tunnels, DNS records and entry
// point rulesets, which answers the calls setup makes as cf does and records them.
type fakeCF struct {
	zones    []string
	tunnels  map[string]string         // id to name
	configs  map[string]map[string]any // tunnel id to its config
	records  map[string][]map[string]any
	rulesets map[string]map[string]any // zone and phase to the entry point ruleset
	calls    []string
	next     int
}

func newFakeCF(zones ...string) *fakeCF {
	return &fakeCF{zones: zones, tunnels: map[string]string{}, configs: map[string]map[string]any{},
		records: map[string][]map[string]any{}, rulesets: map[string]map[string]any{}}
}

func (f *fakeCF) id(kind string) string { f.next++; return fmt.Sprintf("%s-%d", kind, f.next) }

// install makes runCF and haveCF use f for the test.
func (f *fakeCF) install(t *testing.T) {
	oldRun, oldHave := runCF, haveCF
	t.Cleanup(func() { runCF, haveCF = oldRun, oldHave })
	haveCF = func() bool { return true }
	runCF = func(_ context.Context, args ...string) ([]byte, error) {
		f.calls = append(f.calls, strings.Join(args, " "))
		v, err := f.answer(args)
		if err != nil {
			return nil, &cfError{args: args, stderr: err.Error(), err: err}
		}
		if s, ok := v.(string); ok {
			return []byte(s), nil
		}
		return json.Marshal(v)
	}
}

func flag(args []string, name string) string {
	if i := slices.Index(args, name); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

func body(args []string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(flag(args, "--body")), &m); err != nil {
		panic(err)
	}
	return m
}

func (f *fakeCF) answer(args []string) (any, error) {
	cmd := strings.Join(args[:min(3, len(args))], " ")
	notFound := fmt.Errorf("[10000] not found\n404 Not Found")
	switch {
	case cmd == "auth whoami":
		return map[string]any{"authenticated": true}, nil
	case cmd == "zones list --name":
		out := []map[string]any{}
		if slices.Contains(f.zones, args[3]) {
			out = append(out, map[string]any{"id": "z", "name": args[3]})
		}
		return out, nil
	case cmd == "tunnels get "+args[2]:
		if _, ok := f.tunnels[args[2]]; !ok {
			return nil, notFound
		}
		return map[string]any{"id": args[2], "name": f.tunnels[args[2]], "deleted_at": nil}, nil
	case cmd == "tunnels list --name":
		out := []map[string]any{}
		for id, name := range f.tunnels {
			if name == args[3] {
				out = append(out, map[string]any{"id": id, "name": name})
			}
		}
		return out, nil
	case cmd == "tunnels create --name":
		id := f.id("tunnel")
		f.tunnels[id] = args[3]
		return map[string]any{"id": id, "name": args[3]}, nil
	case cmd == "tunnels token get":
		return fmt.Sprintf("%q", "secret-of-"+args[3]), nil
	case cmd == "tunnels config get":
		c, ok := f.configs[args[3]]
		if !ok {
			return map[string]any{"config": nil}, nil
		}
		return map[string]any{"config": c}, nil
	case cmd == "tunnels config update":
		f.configs[args[3]] = body(args)["config"].(map[string]any)
		return map[string]any{}, nil
	case cmd == "dns records list":
		out := []map[string]any{}
		for _, r := range f.records[flag(args, "-z")] {
			if r["name"] == flag(args, "--name") {
				out = append(out, r)
			}
		}
		return out, nil
	case cmd == "dns records create":
		r := body(args)
		r["id"] = f.id("record")
		f.records[flag(args, "-z")] = append(f.records[flag(args, "-z")], r)
		return r, nil
	case cmd == "rulesets entrypoint get":
		rs, ok := f.rulesets[flag(args, "-z")+" "+args[3]]
		if !ok {
			return nil, notFound
		}
		return rs, nil
	case cmd == "rulesets entrypoint update":
		rs := body(args)
		rs["id"] = f.id("ruleset")
		for _, r := range rs["rules"].([]any) {
			r.(map[string]any)["id"] = f.id("rule")
		}
		f.rulesets[flag(args, "-z")+" "+args[3]] = rs
		return rs, nil
	case cmd == "rulesets rules create":
		for _, rs := range f.rulesets {
			if rs["id"] == args[3] {
				r := body(args)
				r["id"] = f.id("rule")
				rs["rules"] = append(rs["rules"].([]any), r)
				return rs, nil
			}
		}
		return nil, notFound
	case cmd == "rulesets rules update":
		for _, rs := range f.rulesets {
			for i, r := range rs["rules"].([]any) {
				if r.(map[string]any)["id"] == args[3] {
					nr := body(args)
					nr["id"] = args[3]
					rs["rules"].([]any)[i] = nr
					return rs, nil
				}
			}
		}
		return nil, notFound
	}
	return nil, fmt.Errorf("the fake cf does not know %q", strings.Join(args, " "))
}

// writes lists the calls that change the account.
func (f *fakeCF) writes() []string {
	var out []string
	for _, c := range f.calls {
		for _, w := range []string{"create", "update", "delete"} {
			if strings.Contains(c, " "+w+" ") || strings.HasSuffix(c, " "+w) {
				w, _, _ := strings.Cut(c, " --body")
				out = append(out, w)
				break
			}
		}
	}
	return out
}

// fakeServerTunnel records what setup hands the server.
type fakeServerTunnel struct {
	id, token string
}

func (s *fakeServerTunnel) run(_ context.Context, id, token string) (string, error) {
	how := "already running"
	if s.id != id || s.token != token {
		how = "started tifa-tunnel.service"
	}
	s.id, s.token = id, token
	return how, nil
}

// A first setup creates the tunnel, hands its token to the server only, routes the domain, adds
// the proxied record and the rules of the host. Running it again changes nothing. A second app
// adds its route, record and rules to the same tunnel and rulesets.
func TestCloudflareSetup(t *testing.T) {
	f := newFakeCF("example.com")
	f.install(t)
	server := &fakeServerTunnel{}
	setup := func(app, domain string, port int) string {
		t.Helper()
		var out bytes.Buffer
		err := cloudflareSetup(context.Background(), tunnelSetup{app: app, domain: domain, port: port,
			tunnelID: server.id, tunnelName: "tifa-vps-1234abcd", server: server.run}, &out)
		if err != nil {
			t.Fatalf("%s: %v\n%s", app, err, out.String())
		}
		if strings.Contains(out.String(), "secret-of") {
			t.Errorf("the output holds the token:\n%s", out.String())
		}
		return out.String()
	}

	out := setup("shop", "shop.example.com", 9527)
	wantWrites := []string{
		"tunnels create --name tifa-vps-1234abcd --config-source cloudflare",
		"tunnels config update tunnel-1",
		"dns records create -z example.com",
		"rulesets entrypoint update http_request_cache_settings -z example.com",
		"rulesets entrypoint update http_config_settings -z example.com",
	}
	if got := f.writes(); !reflect.DeepEqual(got, wantWrites) {
		t.Errorf("writes:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(wantWrites, "\n"))
	}
	if server.id != "tunnel-1" || server.token != "secret-of-tunnel-1" {
		t.Errorf("the server got %+v", server)
	}
	for _, want := range []string{"zone example.com", "tunnel tifa-vps-1234abcd (tunnel-1), created", "route shop.example.com -> http://127.0.0.1:9527, added", "DNS shop.example.com CNAME tunnel-1.cfargotunnel.com, proxied, added"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	ingress, _ := json.Marshal(f.configs["tunnel-1"]["ingress"])
	if string(ingress) != `[{"hostname":"shop.example.com","service":"http://127.0.0.1:9527"},{"service":"http_status:404"}]` {
		t.Errorf("ingress %s", ingress)
	}
	record := f.records["example.com"][0]
	if record["type"] != "CNAME" || record["content"] != "tunnel-1.cfargotunnel.com" || record["proxied"] != true {
		t.Errorf("record %v", record)
	}

	f.calls = nil
	out = setup("shop", "shop.example.com", 9527)
	if got := f.writes(); len(got) != 0 {
		t.Errorf("a second setup wrote %v", got)
	}
	if strings.Count(out, "already there")+strings.Count(out, "already running") != 6 {
		t.Errorf("a second setup:\n%s", out)
	}

	f.calls = nil
	setup("notes", "notes.example.com", 9528)
	wantWrites = []string{
		"tunnels config update tunnel-1",
		"dns records create -z example.com",
		"rulesets rules create ruleset-3 -z example.com",
		"rulesets rules create ruleset-3 -z example.com",
		"rulesets rules create ruleset-6 -z example.com",
	}
	if got := f.writes(); !reflect.DeepEqual(got, wantWrites) {
		t.Errorf("second app writes:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(wantWrites, "\n"))
	}
	ingress, _ = json.Marshal(f.configs["tunnel-1"]["ingress"])
	if string(ingress) != `[{"hostname":"shop.example.com","service":"http://127.0.0.1:9527"},{"hostname":"notes.example.com","service":"http://127.0.0.1:9528"},{"service":"http_status:404"}]` {
		t.Errorf("ingress %s", ingress)
	}

	// A rule changed by hand is put back, and only that one.
	cache := f.rulesets["example.com http_request_cache_settings"]["rules"].([]any)[0].(map[string]any)
	cache["action_parameters"] = map[string]any{"cache": true, "edge_ttl": map[string]any{"mode": "override_origin", "default": 60}}
	f.calls = nil
	out = setup("shop", "shop.example.com", 9527)
	if got := f.writes(); !reflect.DeepEqual(got, []string{"rulesets rules update rule-4 --ruleset-id ruleset-3 -z example.com"}) {
		t.Errorf("writes after a change by hand: %v", got)
	}
	if !strings.Contains(out, "cache rules for shop.example.com, updated") {
		t.Errorf("output:\n%s", out)
	}
}

// A server whose tunnel ID got lost finds its tunnel by name, and one whose tunnel was deleted
// gets a new one.
func TestCloudflareSetupFindsTheTunnel(t *testing.T) {
	f := newFakeCF("example.com")
	f.install(t)
	f.tunnels["tunnel-9"] = "tifa-vps-1234abcd"
	id, how, err := ensureTunnel(context.Background(), "", "tifa-vps-1234abcd")
	if err != nil || id != "tunnel-9" || how != "already there" {
		t.Errorf("by name: %s %s %v", id, how, err)
	}
	id, how, err = ensureTunnel(context.Background(), "gone", "tifa-other-00000000")
	if err != nil || how != "created" || f.tunnels[id] != "tifa-other-00000000" {
		t.Errorf("deleted: %s %s %v", id, how, err)
	}
}

// Setup leaves records and zones of others alone.
func TestCloudflareSetupLeavesOtherRecords(t *testing.T) {
	f := newFakeCF("example.com")
	f.install(t)
	f.records["example.com"] = []map[string]any{{"id": "r", "name": "shop.example.com", "type": "A", "content": "203.0.113.7", "proxied": true}}
	err := cloudflareSetup(context.Background(), tunnelSetup{app: "shop", domain: "shop.example.com", port: 9527,
		tunnelName: "tifa-vps", server: (&fakeServerTunnel{}).run}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "already has a A record") {
		t.Errorf("got %v", err)
	}
	for _, w := range f.writes() {
		if strings.HasPrefix(w, "dns") {
			t.Errorf("wrote %s", w)
		}
	}
	err = cloudflareSetup(context.Background(), tunnelSetup{app: "shop", domain: "shop.example.org", port: 9527,
		tunnelName: "tifa-vps", server: (&fakeServerTunnel{}).run}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no zone") {
		t.Errorf("other zone: %v", err)
	}
}

func TestZoneCandidates(t *testing.T) {
	if got := zoneCandidates("a.b.example.co.uk"); !reflect.DeepEqual(got, []string{"a.b.example.co.uk", "b.example.co.uk", "example.co.uk", "co.uk"}) {
		t.Errorf("got %v", got)
	}
}

// The ingress keeps the other routes and their settings, and ends with the rule for the rest.
func TestWithRoute(t *testing.T) {
	in := []map[string]any{
		{"hostname": "a.example.com", "service": "http://127.0.0.1:9527", "originRequest": map[string]any{"noTLSVerify": true}},
		{"hostname": "b.example.com", "path": "/api", "service": "http://127.0.0.1:1"},
		{"service": "http_status:503"},
	}
	out, changed := withRoute(in, "a.example.com", "http://127.0.0.1:9527")
	if changed || !reflect.DeepEqual(out, in) {
		t.Errorf("same route: %v %v", out, changed)
	}
	out, changed = withRoute(in, "a.example.com", "http://127.0.0.1:9530")
	if !changed || out[0]["service"] != "http://127.0.0.1:9530" || out[0]["originRequest"] == nil || in[0]["service"] != "http://127.0.0.1:9527" {
		t.Errorf("new port: %v", out)
	}
	out, changed = withRoute(in, "c.example.com", "http://127.0.0.1:9531")
	if !changed || len(out) != 4 || out[2]["hostname"] != "c.example.com" || out[3]["service"] != "http_status:503" {
		t.Errorf("new host: %v", out)
	}
}

// Every rule names the host, so no other site of the zone changes, and the cache rules follow
// the CDN settings of docs/deploy.md.
func TestHostRules(t *testing.T) {
	sets := hostRules("shop.example.com")
	for _, rs := range sets {
		for _, r := range rs.rules {
			if !strings.HasPrefix(r.Expression, `(http.host eq "shop.example.com"`) || !strings.HasPrefix(r.Description, "tifa deploy: shop.example.com: ") {
				t.Errorf("%s: %s", r.Description, r.Expression)
			}
		}
	}
	cache := sets[0].rules
	if cache[0].ActionParameters["edge_ttl"].(map[string]any)["mode"] != "bypass_by_default" || cache[1].ActionParameters["cache"] != false {
		t.Errorf("cache rules %+v", cache)
	}
	for _, want := range []string{`"/__tifa/"`, `"_tifa="`} {
		if !strings.Contains(cache[1].Expression, want) || !strings.Contains(cache[0].Expression, "not ") {
			t.Errorf("the rule that skips the cache lacks %s: %s", want, cache[1].Expression)
		}
	}
	html := sets[1].rules[0].ActionParameters
	if html["rocket_loader"] != false || html["email_obfuscation"] != false {
		t.Errorf("config rule %v", html)
	}
}

// Without cf, setup prints the steps to do by hand.
func TestCloudflareWithoutCF(t *testing.T) {
	oldHave := haveCF
	t.Cleanup(func() { haveCF = oldHave })
	haveCF = func() bool { return false }
	if cfLoggedIn(context.Background()) {
		t.Error("logged in without cf")
	}
	var out bytes.Buffer
	printTunnelSteps(&out, "shop.example.com", 9527, "")
	for _, want := range []string{"shop.example.com -> http://127.0.0.1:9527", "cloudflared on the server", "CDN settings"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("lacks %q:\n%s", want, out.String())
		}
	}
}

// A cf that is not logged in counts as missing.
func TestCloudflareNotLoggedIn(t *testing.T) {
	oldRun, oldHave := runCF, haveCF
	t.Cleanup(func() { runCF, haveCF = oldRun, oldHave })
	haveCF = func() bool { return true }
	runCF = func(context.Context, ...string) ([]byte, error) { return []byte(`{"authenticated":false}`), nil }
	if cfLoggedIn(context.Background()) {
		t.Error("logged in")
	}
}

// The error of a cf call holds what cf printed on stderr and the command, never its output.
func TestCFError(t *testing.T) {
	err := &cfError{args: []string{"tunnels", "config", "update", "id", "--body", "{}"}, stderr: "[1000] bad\n400 Bad Request"}
	if got := err.Error(); got != "cf tunnels config update id: [1000] bad\n400 Bad Request" {
		t.Errorf("got %q", got)
	}
	if cfNotFound(err) || !cfNotFound(&cfError{stderr: "404 Not Found"}) {
		t.Error("cfNotFound")
	}
}

// cloudflared gets the token as a systemd credential, never in its command line, and runs as a
// user of its own. The script that installs it takes the token on its input.
func TestTunnelUnit(t *testing.T) {
	unit := tunnelUnit("/usr/local/bin/cloudflared")
	for _, want := range []string{
		"ExecStart=/usr/local/bin/cloudflared --no-autoupdate tunnel run --token-file %d/token\n",
		"LoadCredential=token:/etc/tifa/tunnel-token\n",
		"DynamicUser=yes\n",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit lacks %q:\n%s", want, unit)
		}
	}
	s := tunnelScript("tunnel-1")
	if !strings.Contains(s, "tunnel='tunnel-1'\n") || !strings.Contains(s, "token=$(cat)") || strings.Contains(s, "secret") {
		t.Errorf("script:\n%s", s)
	}
	st, err := parseSetupState("tunnel tunnel-1\nmachine vps-1234abcd\n")
	if err != nil || st.TunnelID != "tunnel-1" || st.Machine != "vps-1234abcd" {
		t.Errorf("setup state %+v, %v", st, err)
	}
}
