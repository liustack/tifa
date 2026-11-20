package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fakeMu guards the fakes of cf and gh: setup reads from several goroutines while it plans.
var fakeMu sync.Mutex

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
		fakeMu.Lock()
		defer fakeMu.Unlock()
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
	if got := err.Error(); got != "cf tunnels config update id: [1000] bad, 400 Bad Request" {
		t.Errorf("got %q", got)
	}
	// The box cf draws around an API error becomes one line, to fit in a line of the setup plan.
	boxed := &cfError{args: []string{"user", "tokens", "list"}, stderr: "┌ APIError\n│ [9109] Unauthorized to access requested resource\n│ 403 Forbidden · HTTP /user/tokens\n└\n\n→ Check your API token permissions"}
	if got := boxed.Error(); got != "cf user tokens list: [9109] Unauthorized to access requested resource, 403 Forbidden · HTTP /user/tokens, → Check your API token permissions" {
		t.Errorf("got %q", got)
	}
	if cfNotFound(err) {
		t.Error("a 400 counts as not found")
	}
	// What cf 1.0.0-beta.18 prints for a tunnel that does not exist.
	missing := "┌ APIError\n│ [1002] Tunnel not found\n│ 404 Not Found · HTTP /accounts/a1b2/cfd_tunnel/00000000-0000-0000-0000-000000000000\n└"
	if !cfNotFound(&cfError{stderr: missing}) {
		t.Error("a 404 does not count as not found")
	}
	// A 404 in the path of another error is not a 404.
	if cfNotFound(&cfError{stderr: "│ [1000] down\n│ 500 Internal Server Error · HTTP /accounts/f404e1/cfd_tunnel/x\n"}) {
		t.Error("a 500 counts as not found")
	}
	if cfNotFound(errors.New("404 Not Found")) {
		t.Error("an error that is not from cf counts as not found")
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
	st, err := parseServerState("tunnel tunnel-1\nmachine vps-1234abcd\ncloudflared /usr/bin/cloudflared\napt yes\n")
	if err != nil || st.TunnelID != "tunnel-1" || st.Machine != "vps-1234abcd" || st.Cloudflared != "/usr/bin/cloudflared" || !st.Apt {
		t.Errorf("setup state %+v, %v", st, err)
	}
}

// cloudflared comes from the signed package repository of Cloudflare through apt, which checks
// its signature and lets unattended-upgrades keep it current. Nothing is downloaded by hand.
func TestTunnelInstall(t *testing.T) {
	s := tunnelScript("tunnel-1")
	for _, want := range []string{
		"https://pkg.cloudflare.com/cloudflare-main.gpg",
		"deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared any main",
		"apt-get install",
		`Unattended-Upgrade::Origins-Pattern { "site=pkg.cloudflare.com,origin=cloudflared"; };`,
		"PathChanged=",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q", want)
		}
	}
	for _, bad := range []string{"github.com", "/usr/local/bin"} {
		if strings.Contains(s, bad) {
			t.Errorf("script has %q", bad)
		}
	}
	path, service := tunnelUpgradeUnits("/usr/bin/cloudflared")
	if !strings.Contains(path, "PathChanged=/usr/bin/cloudflared\n") || !strings.Contains(service, "ExecStart=systemctl try-restart tifa-tunnel.service\n") {
		t.Errorf("upgrade units:\n%s\n%s", path, service)
	}
}
