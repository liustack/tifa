package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeGH stands in for gh: one repository, its secrets and variables.
type fakeGH struct {
	loggedIn bool
	// staleOther is a second account whose token is no longer valid: gh auth status fails unless
	// it checks only the active account. noActive is a gh too old to know --active.
	staleOther, noActive bool
	repo                 string
	branch               string // empty: no default branch yet
	calls                []string
	secrets              map[string]string
	vars                 map[string]string
	// files are the files on the default branch, by path.
	files map[string]string
}

func newFakeGH() *fakeGH {
	return &fakeGH{loggedIn: true, repo: "acme/shop", branch: "main", secrets: map[string]string{}, vars: map[string]string{}}
}

func (g *fakeGH) install(t *testing.T) {
	oldRun, oldHave := runGH, haveGH
	t.Cleanup(func() { runGH, haveGH = oldRun, oldHave })
	haveGH = func() bool { return true }
	runGH = func(_ context.Context, _ string, stdin io.Reader, args ...string) ([]byte, error) {
		fakeMu.Lock()
		defer fakeMu.Unlock()
		g.calls = append(g.calls, strings.Join(args, " "))
		switch {
		case len(args) >= 2 && args[0] == "auth" && args[1] == "status":
			active := slices.Contains(args, "--active")
			switch {
			case active && g.noActive:
				return nil, errors.New("gh auth status: unknown flag: --active")
			case !g.loggedIn:
				return nil, errors.New("gh auth status: You are not logged into any GitHub hosts")
			case g.staleOther && !active:
				return nil, errors.New("gh auth status: The token in keyring for other-account is invalid")
			}
			return nil, nil
		case args[0] == "repo" && args[1] == "view":
			ref := "null"
			if g.branch != "" {
				ref = fmt.Sprintf(`{"name":%q}`, g.branch)
			}
			return []byte(fmt.Sprintf(`{"nameWithOwner":%q,"defaultBranchRef":%s}`, g.repo, ref)), nil
		case args[0] == "secret" && args[1] == "list":
			var out []map[string]string
			for n := range g.secrets {
				out = append(out, map[string]string{"name": n})
			}
			return json.Marshal(out)
		case args[0] == "variable" && args[1] == "list":
			var out []map[string]string
			for n, v := range g.vars {
				out = append(out, map[string]string{"name": n, "value": v})
			}
			return json.Marshal(out)
		case args[0] == "secret" && args[1] == "set", args[0] == "variable" && args[1] == "set":
			if stdin == nil || len(args) != 5 || args[3] != "--repo" || args[4] != g.repo {
				return nil, fmt.Errorf("fake gh: bad set %v", args)
			}
			data, _ := io.ReadAll(stdin)
			if args[0] == "secret" {
				g.secrets[args[2]] = string(data)
			} else {
				g.vars[args[2]] = string(data)
			}
			return nil, nil
		case args[0] == "api":
			p := args[len(args)-1]
			for name, text := range g.files {
				if p == "repos/"+g.repo+"/contents/"+name+"?ref="+g.branch {
					return []byte(text), nil
				}
			}
			return nil, errors.New("gh api: Not Found (HTTP 404)")
		case args[0] == "secret" && args[1] == "delete":
			delete(g.secrets, args[2])
			return nil, nil
		case args[0] == "variable" && args[1] == "delete":
			delete(g.vars, args[2])
			return nil, nil
		}
		return nil, fmt.Errorf("fake gh does not know %v", args)
	}
}

// setupServer is a fake ssh. Facts is what the probe of the deploy user prints. A script that
// changes the server is logged by name with the user, and its input is kept.
type setupServer struct {
	dir string
}

func newSetupServer(t *testing.T, cfg, facts string) *setupServer {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake ssh is a shell script")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "cfg"), cfg)
	writeFile(t, filepath.Join(dir, "facts"), facts)
	prog := filepath.Join(dir, "ssh")
	writeFile(t, prog, `#!/bin/sh
d=`+shellQuote(dir)+`
if [ "$1" = -G ]; then cat "$d/cfg"; exit 0; fi
printf '%s\n' "$*" >> "$d/argv"
if [ -f "$d/sshcfg" ]; then if [ -f "$(cat "$d/sshcfg")" ]; then echo present >> "$d/cfgseen"; else echo missing >> "$d/cfgseen"; fi; fi
user=$2
while [ "$1" != "--" ]; do shift; done
script=$3
case "$script" in
true) echo "check $user" >> "$d/calls"; [ -f "$d/checkclosed" ] && { printf 'EOF\nConnection closed by UNKNOWN port 65535\n' >&2; exit 255; }; [ -f "$d/checkfail" ] && { echo "Permission denied (publickey)." >&2; exit 255; }; true ;;
*ssh_host_*) [ "$user" = tifa ] && [ -f "$d/denied" ] && { echo "tifa@vps: Permission denied (publickey)." >&2; exit 255; }; echo "probe $user" >> "$d/calls"; cat "$d/facts" ;;
*"deploy setup needs root"*) echo "probe $user" >> "$d/calls"; printf 'os Linux\narch x86_64\nmachine vps-1234\n' ;;
*'apt-get purge'*) echo "untunnel $user" >> "$d/calls"; echo "stopped tifa-tunnel" ;;
*'disable --quiet $units'*) [ -f "$d/removefail" ] && { echo "tifa: cannot stop the units" >&2; exit 1; }; echo "removeapp $user" >> "$d/calls" ;;
*useradd*) echo "server $user" >> "$d/calls"; rm -f "$d/denied"; printf 'bus yes\nlinger yes\n' >> "$d/facts" ;;
*'tifa-$app.socket'*) echo "app $user" >> "$d/calls" ;;
*'line=$(cat)'*cloudflare-purge*) echo "purgestore $user" >> "$d/calls"; line=$(cat); [ -f "$d/storefail" ] && { echo "tifa: disk full" >&2; exit 1; }; z=${line%%	*}; touch "$d/purge"; grep -v "^$z	" "$d/purge" > "$d/purge.new" || true; printf '%s\n' "$line" >> "$d/purge.new"; mv "$d/purge.new" "$d/purge"; printf 'purge-file yes\npurge-zone %s\n' "$z" >> "$d/facts" ;;
*'cat "$f"'*) echo "purgeread $user" >> "$d/calls"; cat "$d/purge" 2>/dev/null; true ;;
*'rmdir "$d"'*) echo "purgeforget $user" >> "$d/calls"; echo deleted ;;
*'line=$(cat)'*) echo "authorize $user" >> "$d/calls"; cat >> "$d/stdin" ;;
*authorized_keys.XXXXXX*) echo "revoke $user" >> "$d/calls" ;;
*tunnel*) echo "tunnel $user" >> "$d/calls"; cat > /dev/null; echo "started tifa-tunnel.service" ;;
*) echo "other $user" >> "$d/calls" ;;
esac
`)
	if err := os.Chmod(prog, 0o755); err != nil {
		t.Fatal(err)
	}
	old := sshProgram
	sshProgram = prog
	t.Cleanup(func() { sshProgram = old })
	return &setupServer{dir: dir}
}

func (s *setupServer) deny(t *testing.T) { writeFile(t, filepath.Join(s.dir, "denied"), "") }

// failRemoveApp makes the step that removes the app on the server fail.
func (s *setupServer) failRemoveApp(t *testing.T) {
	writeFile(t, filepath.Join(s.dir, "removefail"), "")
}

// failCheck makes the ssh to the ssh host through the tunnel fail, as a key the server does not know.
func (s *setupServer) failCheck(t *testing.T) { writeFile(t, filepath.Join(s.dir, "checkfail"), "") }

// closeCheck makes the ssh to the ssh host through the tunnel end before it gets to the server, so
// that it prints nothing about keys.
func (s *setupServer) closeCheck(t *testing.T) { writeFile(t, filepath.Join(s.dir, "checkclosed"), "") }

// watchSSHConfig makes the fake ssh note, at each call, whether the file at path was there.
func (s *setupServer) watchSSHConfig(t *testing.T, path string) {
	writeFile(t, filepath.Join(s.dir, "sshcfg"), path)
}

// configSeen says what the file of the ssh config looked like at each ssh call: present or missing.
func (s *setupServer) configSeen() []string {
	data, _ := os.ReadFile(filepath.Join(s.dir, "cfgseen"))
	return strings.Fields(string(data))
}
func (s *setupServer) calls() string {
	data, _ := os.ReadFile(filepath.Join(s.dir, "calls"))
	return string(data)
}

// purge is every line the file of purge tokens on the server got.
func (s *setupServer) purge() string {
	data, _ := os.ReadFile(filepath.Join(s.dir, "purge"))
	return string(data)
}

// havePurge puts the line of example.com with token on the server, as setup writes it.
func (s *setupServer) havePurge(t *testing.T, token string) {
	t.Helper()
	writeFile(t, filepath.Join(s.dir, "purge"), "example.com\tz\t"+token+"\n")
}

// argv is every command line ssh got.
func (s *setupServer) argv() string {
	data, _ := os.ReadFile(filepath.Join(s.dir, "argv"))
	return string(data)
}

func (s *setupServer) stdin() string {
	data, _ := os.ReadFile(filepath.Join(s.dir, "stdin"))
	return string(data)
}

// fakeCloudflared is where the cloudflared of the tests is.
const fakeCloudflared = "/usr/local/bin/cloudflared"

const hostKeysFact = "hostkey ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA\nhostkey ssh-rsa AAAAB3NzaC1yc2E=\n"

// factsOf is the probe output of a prepared server with the app set up on port.
func factsOf(app string, port int, extra string) string {
	return fmt.Sprintf("os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nport %d\nunit-socket %s\nunit-service %s\nmachine vps-1234\napt yes\n%s%s",
		port, sha(socketUnit(app, port)), sha(serviceUnit(app)), hostKeysFact, extra)
}

const freshFacts = "os Linux\narch x86_64\nhome /srv/tifa\nmachine vps-1234\napt yes\n" + hostKeysFact

func setupApp(t *testing.T, tifaYAML string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
	writeFile(t, filepath.Join(root, "tifa.yaml"), tifaYAML)
	for name, content := range files {
		writeFile(t, filepath.Join(root, name), content)
	}
	t.Setenv(deployHostEnv, "vps")
	// No cf unless the test installs one: the real cf is never run. Neither is any program of the
	// machine setup could install or log in with: a test that wants them installs a fakeMachine.
	// cloudflared is there, and the home of the developer is a temporary folder: the real ssh
	// config is never read or written.
	oldHave, oldLook, oldRun, oldOut, oldHome := haveCF, lookProgram, runAttached, programOutput, sshHome
	t.Cleanup(func() {
		haveCF, lookProgram, runAttached, programOutput, sshHome = oldHave, oldLook, oldRun, oldOut, oldHome
	})
	haveCF = func() bool { return false }
	lookProgram = func(name string) (string, error) {
		if name == "cloudflared" {
			return fakeCloudflared, nil
		}
		return "", exec.ErrNotFound
	}
	home := t.TempDir()
	sshHome = func() (string, error) { return home, nil }
	runAttached = func(_ context.Context, _ io.Reader, _, _ io.Writer, name string, args ...string) error {
		t.Errorf("ran %s %v on the terminal", name, args)
		return errors.New("not in a test")
	}
	programOutput = func(_ context.Context, name string, args ...string) ([]byte, error) {
		t.Errorf("ran %s %v", name, args)
		return nil, errors.New("not in a test")
	}
	return root
}

// runSetup runs deploy setup with the given terminal state and answers.
func runSetup(t *testing.T, root string, terminal bool, input string, args ...string) (string, error) {
	t.Helper()
	out, _, err := runSetupIO(t, root, terminal, input, args...)
	return out, err
}

// runSetupIO is runSetup that also returns what went to stderr.
func runSetupIO(t *testing.T, root string, terminal bool, input string, args ...string) (string, string, error) {
	t.Helper()
	return runSetupIn(t, root, terminal, strings.NewReader(input), args...)
}

// runSetupIn is runSetupIO with the input as a reader.
func runSetupIn(t *testing.T, root string, terminal bool, in io.Reader, args ...string) (string, string, error) {
	t.Helper()
	old := stdinIsTerminal
	t.Cleanup(func() { stdinIsTerminal = old })
	stdinIsTerminal = func(io.Reader) bool { return terminal }
	var out, errOut bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(in)
	cmd.SetArgs(append([]string{"deploy", "setup", "--dir", root}, args...))
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

// An unprepared server: tifa cannot log in, so the plan adds the server as root and then the app as
// tifa. Without a terminal and without --yes the plan is printed and nothing runs.
func TestSetupFreshServer(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	srv := newSetupServer(t, "", freshFacts)
	srv.deny(t)

	out, err := runSetup(t, root, false, "")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("got %v", err)
	}
	for _, want := range []string{"Plan for shop on vps", "add     server: the user tifa", "add     app: /srv/tifa/shop with an empty shared/.env, port 9527"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(srv.calls(), "server root") || strings.Contains(srv.calls(), "app tifa") {
		t.Errorf("something ran:\n%s", srv.calls())
	}

	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(srv.calls(), "server root\nprobe tifa\napp tifa") {
		t.Errorf("order of calls:\n%s", srv.calls())
	}
	if strings.Count(out, "  done    ") != 2 {
		t.Errorf("one line per step:\n%s", out)
	}
}

// With a terminal setup asks: n changes nothing, y applies.
func TestSetupAsks(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	srv := newSetupServer(t, "", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine m\n"+hostKeysFact)
	out, err := runSetup(t, root, true, "n\n")
	if err == nil || strings.Contains(srv.calls(), "app tifa") || !strings.Contains(out, "Apply this plan? [y/N]") {
		t.Fatalf("n: %v\n%s\n%s", err, out, srv.calls())
	}
	if _, err := runSetup(t, root, true, "y\n"); err != nil || !strings.Contains(srv.calls(), "app tifa") {
		t.Fatalf("y: %v\n%s", err, srv.calls())
	}
}

// A prepared server with the app in place: every line is ok and nothing runs, with no terminal and
// no --yes. The server may be an address: it is not in a committed file, so it needs no note.
func TestSetupNothingToChange(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	t.Setenv(deployHostEnv, "ssh://203.0.113.7:2222")
	srv := newSetupServer(t, "", factsOf("shop", 9531, ""))
	out, err := runSetup(t, root, false, "", "--all")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"ok      server:", "ok      app: /srv/tifa/shop, port 9531"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(srv.calls(), "app ") {
		t.Errorf("calls:\n%s", srv.calls())
	}
	if strings.Contains(out, "alias") || strings.Contains(out, "You need to do") {
		t.Errorf("a note about the address:\n%s", out)
	}
	// Units that differ are written again.
	root = setupApp(t, "deploy:\n", nil)
	newSetupServer(t, "", strings.Replace(factsOf("shop", 9531, ""), "unit-service "+sha(serviceUnit("shop")), "unit-service 00", 1))
	out, _ = runSetup(t, root, false, "")
	if strings.Contains(out, "alias") || !strings.Contains(out, "change  app: write the systemd units of shop again") {
		t.Errorf("out:\n%s", out)
	}
}

// cfEverything adds to fakeAccess what setup needs besides: the deletes.
type cfEverything struct {
	*fakeAccess
	deleted []string
}

func (c *cfEverything) install(t *testing.T) {
	c.fakeAccess.install(t)
	inner := runCF
	runCF = func(ctx context.Context, args ...string) ([]byte, error) {
		if strings.Contains(" "+strings.Join(args[:min(5, len(args))], " ")+" ", " delete ") {
			fakeMu.Lock()
			defer fakeMu.Unlock()
			c.calls = append(c.calls, strings.Join(args, " "))
			c.deleted = append(c.deleted, strings.Join(args, " "))
			c.applyDelete(args)
			return []byte(`{}`), nil
		}
		return inner(ctx, args...)
	}
}

func (c *cfEverything) applyDelete(args []string) {
	switch strings.Join(args[:min(3, len(args))], " ") {
	case "dns records delete":
		z := flag(args, "-z")
		c.records[z] = slices.DeleteFunc(c.records[z], func(r map[string]any) bool { return r["id"] == args[3] })
	case "rulesets rules delete":
		for _, rs := range c.rulesets {
			rs["rules"] = slices.DeleteFunc(rs["rules"].([]any), func(r any) bool { return r.(map[string]any)["id"] == args[3] })
		}
	case "zero-trust access service-tokens":
		c.tokens = slices.DeleteFunc(c.tokens, func(r map[string]any) bool { return r["id"] == args[4] })
	case "zero-trust access policies":
		c.policies = slices.DeleteFunc(c.policies, func(r map[string]any) bool { return r["id"] == args[4] })
	}
	if args[0] == "tunnels" && args[1] == "delete" {
		delete(c.tunnels, args[2])
		delete(c.configs, args[2])
	}
	if len(args) > 4 && args[2] == "applications" {
		c.apps = slices.DeleteFunc(c.apps, func(r map[string]any) bool { return r["id"] == args[4] })
	}
}

func setFacts(t *testing.T, srv *setupServer, facts string) {
	t.Helper()
	writeFile(t, filepath.Join(srv.dir, "facts"), facts)
}

// Everything of GitHub: with github true the plan adds the key, the connection and the workflow,
// secrets reach gh on its input and nowhere else, and a second run has nothing to do. With github
// false the plan removes what Tifa made and only that.
func TestSetupGitHub(t *testing.T) {
	eachBackend(t, func(t *testing.T, rest bool) {
		root := setupApp(t, "deploy:\n  ci: github\n", nil)
		srv := newSetupServer(t, "hostname 203.0.113.7\nport 2222\n", factsOf("shop", 9531, ""))
		gh := newFakeGH()
		gh.installBackend(t, rest)

		out, err := runSetup(t, root, false, "", "--yes")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		for _, want := range []string{
			"add     github: a key for acme/shop that may log in as tifa",
			"add     github: where CI logs in (direct)",
			"add     github: .github/workflows/tifa-deploy.yml",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("lacks %q:\n%s", want, out)
			}
		}
		if !strings.HasPrefix(gh.secrets[secretSSHKey], "-----BEGIN OPENSSH PRIVATE KEY-----") ||
			gh.secrets[secretHost] != "ssh://203.0.113.7:2222" ||
			gh.secrets[secretKnownHosts] != "[203.0.113.7]:2222 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA\n[203.0.113.7]:2222 ssh-rsa AAAAB3NzaC1yc2E=" ||
			gh.vars[varMarker] == "" {
			t.Errorf("secrets %v, vars %v", gh.secrets, gh.vars)
		}
		if !strings.Contains(srv.stdin(), "restrict ssh-ed25519 ") || !strings.Contains(srv.stdin(), " tifa-github acme/shop") {
			t.Errorf("authorized line: %q", srv.stdin())
		}
		for _, v := range gh.secrets {
			if strings.Contains(out, v) || strings.Contains(strings.Join(gh.calls, "\n"), v) {
				t.Errorf("a secret value is in the output or on a command line")
			}
		}
		text, err := os.ReadFile(filepath.Join(root, githubWorkflowPath))
		if err != nil || !strings.HasPrefix(string(text), workflowMarker) || !strings.Contains(string(text), "push:") {
			t.Errorf("workflow %v:\n%s", err, text)
		}
		// Where the server is stays in the secret: the workflow has neither the address nor the port.
		for _, not := range []string{"Port ", "203.0.113.7", "2222"} {
			if strings.Contains(string(text), not) {
				t.Errorf("the workflow shows %q:\n%s", not, text)
			}
		}

		// Again: nothing to do.
		setFacts(t, srv, factsOf("shop", 9531, "ghkey acme/shop\n"))
		gh.calls = nil
		out, err = runSetup(t, root, false, "")
		if err != nil || !strings.Contains(out, "matches tifa.yaml") {
			t.Fatalf("second run: %v\n%s", err, out)
		}
		for _, c := range gh.calls {
			if isWriteCall(c) {
				t.Errorf("a second run set %s", c)
			}
		}
		// The same workflow with CRLF line ends, as git on Windows checks it out without a
		// .gitattributes, is right too.
		writeFile(t, filepath.Join(root, githubWorkflowPath), strings.ReplaceAll(string(text), "\n", "\r\n"))
		if out, err = runSetup(t, root, false, ""); err != nil || !strings.Contains(out, "matches tifa.yaml") {
			t.Fatalf("a run with CRLF line ends: %v\n%s", err, out)
		}

		// A host that moved: the connection changes, the key stays.
		newSetupServer(t, "hostname 198.51.100.9\nport 22\n", factsOf("shop", 9531, "ghkey acme/shop\n"))
		out, _ = runSetup(t, root, false, "")
		if !strings.Contains(out, "change  github: where CI logs in") || !strings.Contains(out, "Already right: server 1, app 1, GitHub 2") {
			t.Errorf("moved host:\n%s", out)
		}

		// github: false takes it away.
		writeFile(t, filepath.Join(root, "tifa.yaml"), "deploy:\n")
		srv = newSetupServer(t, "hostname 203.0.113.7\nport 2222\n", factsOf("shop", 9531, "ghkey acme/shop\nghkey acme/other\n"))
		out, err = runSetup(t, root, false, "", "--yes")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		for _, want := range []string{"remove  github: .github/workflows/tifa-deploy.yml", "remove  server: the key of acme/shop", "remove  github: the secrets"} {
			if !strings.Contains(out, want) {
				t.Errorf("lacks %q:\n%s", want, out)
			}
		}
		if len(gh.secrets) != 0 || len(gh.vars) != 0 || !strings.Contains(srv.calls(), "revoke tifa") {
			t.Errorf("left: %v %v\n%s", gh.secrets, gh.vars, srv.calls())
		}
		if _, err := os.Stat(filepath.Join(root, githubWorkflowPath)); err == nil {
			t.Error("the workflow is still there")
		}
	})
}

// Only what Tifa made and marked goes: a workflow without the line of tifa, and secrets while the
// variable of tifa is missing, stay.
func TestSetupRemovesOnlyMarkedThings(t *testing.T) {
	root := setupApp(t, "deploy:\n", map[string]string{githubWorkflowPath: "name: mine\non: push\n"})
	newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, "ghkey acme/other\n"))
	gh := newFakeGH()
	gh.secrets = map[string]string{secretHost: "mine", secretSSHKey: "mine"}
	gh.install(t)
	out, err := runSetup(t, root, false, "")
	if err != nil || !strings.Contains(out, "matches tifa.yaml") {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, githubWorkflowPath)); err != nil || len(gh.secrets) != 2 {
		t.Errorf("removed something: %v %v", err, gh.secrets)
	}
	// A workflow of the developer's own stays with github: true too.
	writeFile(t, filepath.Join(root, "tifa.yaml"), "deploy:\n  ci: github\n")
	out, _ = runSetup(t, root, false, "", "--all")
	if !strings.Contains(out, "has no Tifa line at the top, so it is yours and stays") {
		t.Errorf("out:\n%s", out)
	}
}

// Without gh logged in the GitHub items cannot be done, and the plan says what is missing.
func TestSetupGitHubCannot(t *testing.T) {
	// gh is not logged in, or not installed, and there is no token: both ways are named.
	for name, why := range map[string]string{"not logged in": "gh is not logged in: run gh auth login", "not installed": "gh, the GitHub CLI, is not installed"} {
		t.Run(name, func(t *testing.T) {
			root := setupApp(t, "deploy:\n  ci: github\n", nil)
			newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, ""))
			gh := newFakeGH()
			gh.loggedIn = false
			gh.install(t)
			if name == "not installed" {
				haveGH = func() bool { return false }
			}
			t.Setenv(githubTokenEnv, "")
			out, err := runSetup(t, root, false, "")
			if err == nil || !strings.Contains(out, "1. github: deploys from GitHub Actions") || !strings.Contains(out, why) || !strings.Contains(out, githubTokenEnv+"=<token>") || !strings.Contains(err.Error(), "1 thing needs you") {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}

func cfSetup(t *testing.T, zones ...string) *cfEverything {
	c := &cfEverything{fakeAccess: &fakeAccess{fakeCF: newFakeCF(zones...)}}
	c.install(t)
	return c
}

const tunnelFacts = "tunnel tunnel-1\ncloudflared /usr/bin/cloudflared\ntunnel-active yes\n"

// deploy.domain: the plan adds the tunnel, cloudflared, the route, the record and the rules, the
// token goes to the server and to nobody else, a second run has nothing to do, and removing the
// domain removes what was added for it.
func TestSetupDomain(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n", nil)
	srv := newSetupServer(t, "", factsOf("shop", 9531, ""))
	cf := cfSetup(t, "example.com")

	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"add     cloudflare: the tunnel of the server, tifa-vps-1234",
		"add     server: install cloudflared when missing and run the tunnel as the service tifa-tunnel, as root",
		"add     cloudflare: route shop.example.com -> http://127.0.0.1:9531",
		"add     cloudflare: DNS shop.example.com CNAME the tunnel, proxied",
		"add     cloudflare: cache rules for shop.example.com",
		"add     cloudflare: HTML rewriting off for shop.example.com",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret-of") || !strings.Contains(srv.calls(), "tunnel root") {
		t.Errorf("token in the output, or no tunnel script:\n%s\n%s", out, srv.calls())
	}
	wantWrites := []string{
		"tunnels create --name tifa-vps-1234 --config-source cloudflare",
		"tunnels config update tunnel-1",
		"dns records create -z example.com",
		"rulesets entrypoint update http_request_cache_settings -z example.com",
		"rulesets entrypoint update http_config_settings -z example.com",
	}
	if got := cf.writes(); strings.Join(got, "\n") != strings.Join(wantWrites, "\n") {
		t.Errorf("writes:\n%s", strings.Join(got, "\n"))
	}

	setFacts(t, srv, factsOf("shop", 9531, tunnelFacts))
	cf.calls = nil
	out, err = runSetup(t, root, false, "")
	if err != nil || !strings.Contains(out, "matches tifa.yaml") || len(cf.writes()) != 0 {
		t.Fatalf("second run: %v\n%s\n%v", err, out, cf.writes())
	}

	// A rule changed by hand is put back, and only it.
	cache := cf.rulesets["example.com http_request_cache_settings"]["rules"].([]any)[0].(map[string]any)
	cache["action_parameters"] = map[string]any{"cache": true}
	out, _ = runSetup(t, root, false, "")
	if !strings.Contains(out, "change  cloudflare: cache rules for shop.example.com") {
		t.Errorf("out:\n%s", out)
	}

	// Another domain for the app: the old one is removed, the other app's route stays.
	cf.configs["tunnel-1"]["ingress"] = []any{
		map[string]any{"hostname": "shop.example.com", "service": "http://127.0.0.1:9531"},
		map[string]any{"hostname": "notes.example.com", "service": "http://127.0.0.1:9600"},
		map[string]any{"service": "http_status:404"},
	}
	writeFile(t, filepath.Join(root, "tifa.yaml"), "deploy:\n")
	cf.calls = nil
	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "remove  domain: shop.example.com, its route, DNS record and rules, which tifa made") {
		t.Fatalf("%v\n%s", err, out)
	}
	ingress, _ := json.Marshal(cf.configs["tunnel-1"]["ingress"])
	if strings.Contains(string(ingress), "shop.example.com") || !strings.Contains(string(ingress), "notes.example.com") || len(cf.records["example.com"]) != 0 {
		t.Errorf("ingress %s, records %v", ingress, cf.records)
	}
	for _, rs := range cf.rulesets {
		if n := len(rs["rules"].([]any)); n != 0 {
			t.Errorf("%d rules left", n)
		}
	}
}

// A record of the domain that tifa did not make is not removed, and nothing is removed for a
// route of another app.
func TestSetupLeavesForeignDNS(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, tunnelFacts))
	cf := cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	cf.configs["tunnel-1"] = map[string]any{"ingress": []any{
		map[string]any{"hostname": "shop.example.com", "service": "http://127.0.0.1:9531"},
		map[string]any{"service": "http_status:404"},
	}}
	cf.records["example.com"] = []map[string]any{{"id": "r1", "name": "shop.example.com", "type": "CNAME", "content": "tunnel-1.cfargotunnel.com"}}
	if _, err := runSetup(t, root, false, "", "--yes"); err != nil {
		t.Fatal(err)
	}
	if len(cf.records["example.com"]) != 1 {
		t.Error("a record without the mark of tifa was deleted")
	}
}

// Without cf the domain cannot be done, and the plan says so.
func TestSetupDomainNeedsCF(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, ""))
	oldHave := haveCF
	t.Cleanup(func() { haveCF = oldHave })
	haveCF = func() bool { return false }
	out, err := runSetup(t, root, false, "")
	if err == nil || !strings.Contains(out, "1. domain: shop.example.com through the Cloudflare Tunnel") || !strings.Contains(out, "cf is not installed: pnpm add --global cf, then cf auth login") {
		t.Fatalf("%v\n%s", err, out)
	}
}

// With cf installed but not logged in, the plan asks for the login only.
func TestSetupDomainNeedsCFLogin(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, ""))
	oldRun, oldHave := runCF, haveCF
	t.Cleanup(func() { runCF, haveCF = oldRun, oldHave })
	haveCF = func() bool { return true }
	runCF = func(_ context.Context, args ...string) ([]byte, error) {
		return nil, &cfError{args: args, stderr: "not logged in", err: errors.New("exit status 1")}
	}
	out, err := runSetup(t, root, false, "")
	if err == nil || !strings.Contains(out, "needs cf logged in: cf auth login") || strings.Contains(out, "not installed") {
		t.Fatalf("%v\n%s", err, out)
	}
}

// purgeSetup is a setup with front cloudflare and ci github on a prepared server. Tokens are what
// the hidden prompt returns one after the other, and tried is what it was asked. The Cloudflare
// API accepts the token tok-good and answers 403 to any other.
type purgeSetup struct {
	root   string
	srv    *setupServer
	cf     *cfEverything
	gh     *fakeGH
	prompt []string
	asked  int
	checks []string // Bearer headers and bodies the API got
}

func newPurgeSetup(t *testing.T, tokens ...string) *purgeSetup {
	s := &purgeSetup{prompt: tokens}
	s.root = setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n  ci: github\n", nil)
	s.srv = newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, tunnelFacts))
	s.cf = cfSetup(t, "example.com")
	s.cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	s.gh = newFakeGH()
	s.gh.install(t)
	oldPrompt := promptSecret
	t.Cleanup(func() { promptSecret = oldPrompt })
	promptSecret = func(io.Reader, io.Writer, string) (string, error) {
		if s.asked >= len(s.prompt) {
			t.Fatalf("asked for a token %d times, the test has %d", s.asked+1, len(s.prompt))
		}
		s.asked++
		return s.prompt[s.asked-1], nil
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		s.checks = append(s.checks, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization")+" "+string(data))
		if r.Header.Get("Authorization") != "Bearer tok-good" {
			w.WriteHeader(403)
			io.WriteString(w, `{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`)
			return
		}
		io.WriteString(w, `{"success":true,"errors":[]}`)
	}))
	oldAPI := cloudflareAPI
	t.Cleanup(func() { api.Close(); cloudflareAPI = oldAPI })
	cloudflareAPI = api.URL
	return s
}

const purgeCheckBody = `{"files":["https://shop.example.com/.well-known/tifa-purge-check"]}`

// The line of the plan that keeps the token on the server, and the line once it is there.
const (
	purgeItem = "server: a token that can only clear the cache of example.com, pasted by you and kept in /srv/tifa/.config/tifa/cloudflare-purge, which only tifa can read"
	purgeOK   = "server: the token that clears the cache of example.com, in /srv/tifa/.config/tifa/cloudflare-purge"
)

// noSecretIn fails when a token is in the output, in an argument of gh or ssh, or in GitHub.
func (s *purgeSetup) noSecretIn(t *testing.T, out string) {
	t.Helper()
	for what, text := range map[string]string{"output": out, "gh calls": strings.Join(s.gh.calls, "\n"), "ssh command lines": s.srv.argv()} {
		if strings.Contains(text, "tok-") {
			t.Errorf("a token is in the %s:\n%s", what, text)
		}
	}
	for n, v := range s.gh.secrets {
		if strings.HasPrefix(v, "tok-") {
			t.Errorf("a token went to GitHub as %s", n)
		}
	}
}

// With a terminal, setup prints the link of the Cloudflare form, reads the token hidden, checks it
// against the zone, and writes it with the zone to the server, on the input of ssh as tifa. A token
// that fails the check is asked again. GitHub gets neither, and the workflow hands neither to Tifa.
// A second run finds the zone on the server: no item, no question.
func TestSetupPurgeTokenTerminal(t *testing.T) {
	s := newPurgeSetup(t, "tok-bad", "tok-good")
	out, err := runSetup(t, s.root, true, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"add     " + purgeItem,
		"https://dash.cloudflare.com/?to=/:account/api-tokens&permissionGroupKeys=%5B%7B%22key%22%3A%22cache%22%2C%22type%22%3A%22purge%22%7D%5D&name=tifa%20cache%20purge%20example.com",
		"Zone Resources",
		"Tifa keeps this token on your server vps, readable only by the user tifa, and every deploy to the zone example.com uses it.",
		"the token was refused",
		"done    " + purgeItem,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if got := s.srv.purge(); got != "example.com\tz\ttok-good\n" {
		t.Errorf("the server got %q", got)
	}
	if !strings.Contains(s.srv.calls(), "purgestore tifa") {
		t.Errorf("calls:\n%s", s.srv.calls())
	}
	want := []string{"POST /zones/z/purge_cache Bearer tok-bad " + purgeCheckBody, "POST /zones/z/purge_cache Bearer tok-good " + purgeCheckBody}
	if !slices.Equal(s.checks, want) {
		t.Errorf("checks %q", s.checks)
	}
	s.noSecretIn(t, out)
	if _, ok := s.gh.secrets[secretPurge]; ok {
		t.Error("the token went to GitHub")
	}
	if _, ok := s.gh.secrets[secretZoneID]; ok {
		t.Error("the zone ID went to GitHub")
	}
	text, _ := os.ReadFile(filepath.Join(s.root, githubWorkflowPath))
	if strings.Contains(string(text), envPurgeToken) || strings.Contains(string(text), envZoneID) {
		t.Errorf("the workflow hands the token or the zone:\n%s", text)
	}
	// The zone is on the server now, so a second run only says ok and asks nothing.
	s.asked = len(s.prompt) // one more ask would fail the test
	out, err = runSetup(t, s.root, true, "", "--yes", "--all")
	if err != nil || !strings.Contains(out, "ok      "+purgeOK) || strings.Contains(out, "pasted by you") {
		t.Errorf("second run: %v\n%s", err, out)
	}
	setFacts(t, s.srv, factsOf("shop", 9531, tunnelFacts+"ghkey acme/shop\npurge-file yes\npurge-zone example.com\n"))
	if out, err = runSetup(t, s.root, true, ""); err != nil || !strings.Contains(out, "matches tifa.yaml") {
		t.Errorf("third run: %v\n%s", err, out)
	}
}

// A second app on the same server and zone finds the token there: no item, no question, and its
// repository gets no purge secret either.
func TestSetupPurgeTokenSecondApp(t *testing.T) {
	s := newPurgeSetup(t)
	setFacts(t, s.srv, factsOf("shop", 9531, tunnelFacts+"other notes\ntaken 9600\npurge-file yes\npurge-zone example.com\n"))
	s.srv.havePurge(t, "tok-good")
	out, err := runSetup(t, s.root, true, "", "--yes", "--all")
	if err != nil || !strings.Contains(out, "ok      "+purgeOK) || strings.Contains(out, "pasted by you") || s.asked != 0 {
		t.Fatalf("%v, asked %d\n%s", err, s.asked, out)
	}
	if _, ok := s.gh.secrets[secretPurge]; ok || strings.Contains(s.srv.calls(), "purgestore") {
		t.Errorf("secrets %v, calls\n%s", s.gh.secrets, s.srv.calls())
	}
}

// An empty line skips the token: the token is optional, so the run still succeeds and says what
// skipping costs. Three refused tokens give up with a warning, since the developer wanted one.
// Either way the way by hand is printed, nothing is stored, and the steps after it still run.
func TestSetupPurgeTokenSkippedOrRefused(t *testing.T) {
	for name, tokens := range map[string][]string{"empty": {""}, "refused three times": {"tok-1", "tok-2", "tok-3"}} {
		s := newPurgeSetup(t, tokens...)
		out, err := runSetup(t, s.root, true, "", "--yes")
		if name == "empty" {
			if err != nil || !strings.Contains(out, "skipped "+purgeItem) || !strings.Contains(out, "Cache time") {
				t.Fatalf("%s: %v\n%s", name, err, out)
			}
		} else if err == nil || !strings.Contains(err.Error(), "1 thing needs you") || !strings.Contains(out, "warning "+purgeItem) {
			t.Fatalf("%s: a refused token must fail the run: %v\n%s", name, err, out)
		}
		if !strings.Contains(out, "tifa deploy setup --cache-purge-token") {
			t.Errorf("%s:\n%s", name, out)
		}
		if s.srv.purge() != "" || s.asked != len(tokens) {
			t.Errorf("%s: stored %q, asked %d", name, s.srv.purge(), s.asked)
		}
		if s.gh.secrets[secretSSHKey] == "" {
			t.Errorf("%s: the steps after the token did not run", name)
		}
		if name == "empty" && len(s.checks) != 0 {
			t.Errorf("an empty line was checked: %q", s.checks)
		}
		s.noSecretIn(t, out)
		// Setup asks on the first setup only. Run again, it notes the command that adds the
		// token, asks nothing and succeeds.
		out, err = runSetup(t, s.root, true, "", "--yes")
		if err != nil || s.asked != len(tokens) || !strings.Contains(out, "(optional)") || !strings.Contains(out, "tifa deploy setup --cache-purge-token") {
			t.Errorf("%s: second run: %v, asked %d\n%s", name, err, s.asked, out)
		}
		if strings.Contains(out, "gh secret") {
			t.Errorf("%s: setup tells to run gh:\n%s", name, out)
		}
	}
}

// --cache-purge-token asks for the token even when the server has it, and replaces it, on a
// terminal or, without one, on the page. It needs deploy.front cloudflare, and works without
// deploy.ci.
func TestSetupCachePurgeTokenFlag(t *testing.T) {
	s := newPurgeSetup(t, "tok-good", "tok-good")
	setFacts(t, s.srv, factsOf("shop", 9531, tunnelFacts+"purge-file yes\npurge-zone example.com\n"))
	s.srv.havePurge(t, "tok-old")
	out, err := runSetup(t, s.root, true, "", "--yes", "--cache-purge-token")
	if err != nil || !strings.Contains(out, "change  "+purgeItem) || s.srv.purge() != "example.com\tz\ttok-good\n" {
		t.Fatalf("%v, stored %q\n%s", err, s.srv.purge(), out)
	}
	s.noSecretIn(t, out)
	// Without a terminal the flag asks on the page, with --yes too.
	s.srv.havePurge(t, "tok-old")
	submitValues(t, "tok-good")
	if out, err := runSetup(t, s.root, false, "", "--yes", "--cache-purge-token"); err != nil || s.srv.purge() != "example.com\tz\ttok-good\n" {
		t.Errorf("no terminal: %v, stored %q\n%s", err, s.srv.purge(), out)
	}
	writeFile(t, filepath.Join(s.root, "tifa.yaml"), "site:\n  url: https://shop.example.com\ndeploy:\n  ci: github\n")
	if _, err := runSetup(t, s.root, true, "", "--yes", "--cache-purge-token"); err == nil || !strings.Contains(err.Error(), "deploy.front") {
		t.Errorf("front none: %v", err)
	}
	// Without CI: a deploy from a machine without a cf login uses it too.
	writeFile(t, filepath.Join(s.root, "tifa.yaml"), "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n")
	out, err = runSetup(t, s.root, true, "", "--yes", "--cache-purge-token")
	if err != nil || !strings.Contains(out, "change  "+purgeItem) || s.asked != 2 {
		t.Errorf("ci none: %v, asked %d\n%s", err, s.asked, out)
	}
}

// Without CI, the first setup of the domain asks too, and later runs ask nothing and note nothing:
// a deploy from this machine clears the cache with cf.
func TestSetupPurgeTokenWithoutCI(t *testing.T) {
	s := newPurgeSetup(t, "tok-good")
	writeFile(t, filepath.Join(s.root, "tifa.yaml"), "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n")
	out, err := runSetup(t, s.root, true, "", "--yes")
	if err != nil || !strings.Contains(out, "add     "+purgeItem) || s.srv.purge() != "example.com\tz\ttok-good\n" {
		t.Fatalf("%v, stored %q\n%s", err, s.srv.purge(), out)
	}
	s.noSecretIn(t, out)
	// Skipped on the first run: later runs leave it alone.
	s = newPurgeSetup(t, "")
	writeFile(t, filepath.Join(s.root, "tifa.yaml"), "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n")
	if _, err := runSetup(t, s.root, true, "", "--yes"); err != nil {
		t.Fatal(err)
	}
	out, err = runSetup(t, s.root, true, "", "--yes")
	if err != nil || !strings.Contains(out, "matches tifa.yaml") || s.asked != 1 {
		t.Errorf("second run: %v, asked %d\n%s", err, s.asked, out)
	}
}

// Without a terminal there is nobody to paste a token: the plan notes, as optional, how to make it
// and set it and what going without costs. The run succeeds.
func TestSetupPurgeTokenNoTerminal(t *testing.T) {
	s := newPurgeSetup(t)
	out, err := runSetup(t, s.root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "gh secret") {
		t.Errorf("setup tells to run gh:\n%s", out)
	}
	for _, want := range []string{"1. " + purgeItem, "(optional)", "Cache time", "tifa deploy setup --cache-purge-token", "go tool tifa deploy setup --yes --cache-purge-token", "opens a page in your browser"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if s.asked != 0 || len(s.checks) != 0 || s.srv.purge() != "" {
		t.Errorf("asked %d, checks %q, stored %q", s.asked, s.checks, s.srv.purge())
	}
}

// Without a terminal, --cache-purge-token asks on the page, with --yes: it shows the link of the
// Cloudflare form, what to set there and that the token stays on the server. A refused token opens
// a new page that says why. No output shows a token.
func TestSetupPurgeTokenPage(t *testing.T) {
	s := newPurgeSetup(t)
	pages := submitPages(t, []string{"tok-bad"}, []string{"tok-good"})
	out, errOut, err := runSetupIO(t, s.root, false, "", "--yes", "--cache-purge-token")
	if err != nil || s.srv.purge() != "example.com\tz\ttok-good\n" || s.asked != 0 {
		t.Fatalf("%v, stored %q, asked %d\n%s", err, s.srv.purge(), s.asked, out)
	}
	forms := pages.forms()
	for _, want := range []string{"Zone Resources", "on your server vps, readable only by the user tifa, and every deploy to the zone example.com uses it", "The token was refused", "api-tokens&amp;permissionGroupKeys="} {
		if !strings.Contains(forms, want) {
			t.Errorf("the pages lack %q:\n%s", want, forms)
		}
	}
	if strings.Contains(forms, "GitHub secret") {
		t.Errorf("the page says the token is a GitHub secret:\n%s", forms)
	}
	if !strings.Contains(errOut, "http://127.0.0.1:") || strings.Contains(forms, "tok-") {
		t.Errorf("out:\n%s\n%s", out, errOut)
	}
	s.noSecretIn(t, out+errOut)
}

// v018Repo makes the repository one that setup of v0.1.18 made: the purge token and the zone ID
// as secrets, with the marker, and the workflow that hands them to Tifa.
func (s *purgeSetup) v018Repo(t *testing.T) {
	t.Helper()
	if out, err := runSetup(t, s.root, false, "", "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	s.gh.secrets[secretPurge] = "set by v0.1.18"
	s.gh.secrets[secretZoneID] = "z"
	spec, err := workflowSpec(deployTarget{root: s.root}, githubRepo{Name: "acme/shop", Branch: "main"}, connDirect, true)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(s.root, githubWorkflowPath), githubWorkflow(spec))
	s.gh.files = map[string]string{githubWorkflowPath: githubWorkflow(spec)}
	setFacts(t, s.srv, factsOf("shop", 9531, tunnelFacts+"ghkey acme/shop\n"))
}

// push puts the workflow of the checkout on the default branch, as a commit and push does.
func (s *purgeSetup) push(t *testing.T) {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(s.root, githubWorkflowPath))
	if err != nil {
		t.Fatal(err)
	}
	s.gh.files[githubWorkflowPath] = string(text)
}

// handsSecrets says whether the workflow of the checkout hands Tifa the two old secrets.
func (s *purgeSetup) handsSecrets(t *testing.T) bool {
	t.Helper()
	text, _ := os.ReadFile(filepath.Join(s.root, githubWorkflowPath))
	return strings.Contains(string(text), envPurgeToken+": ${{ secrets.") && strings.Contains(string(text), envZoneID+": ${{ secrets.")
}

const secretsStay = "github: the secrets TIFA_DEPLOY_CF_PURGE_TOKEN and TIFA_DEPLOY_CF_ZONE_ID stay while the workflow on main hands them to Tifa"

// A repository set up by v0.1.18 keeps the purge token as a secret. Setup asks for it once to keep
// it on the server and writes the workflow without the secrets. The secrets stay while the
// workflow on the default branch still hands them over, so CI that runs it keeps clearing the
// cache. Once the new workflow is pushed, the next run checks the token on the server and removes
// them.
func TestSetupMovesThePurgeTokenToTheServer(t *testing.T) {
	s := newPurgeSetup(t, "tok-good")
	s.v018Repo(t)
	out, err := runSetup(t, s.root, true, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"add     " + purgeItem, "change  github: .github/workflows/tifa-deploy.yml", secretsStay, "commit and push"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "remove  github: the secrets") || s.gh.secrets[secretPurge] == "" || s.gh.secrets[secretZoneID] == "" {
		t.Errorf("the secrets went while main still uses them: %v\n%s", s.gh.secrets, out)
	}
	if s.srv.purge() != "example.com\tz\ttok-good\n" || s.handsSecrets(t) {
		t.Errorf("stored %q, workflow hands the secrets %v", s.srv.purge(), s.handsSecrets(t))
	}
	s.noSecretIn(t, out)

	s.push(t)
	checks := len(s.checks)
	out, err = runSetup(t, s.root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "remove  github: the secrets TIFA_DEPLOY_CF_PURGE_TOKEN and TIFA_DEPLOY_CF_ZONE_ID, which tifa set and the workflow no longer uses") || !strings.Contains(out, `delete the token "tifa cache purge acme/shop" in the Cloudflare dashboard`) {
		t.Fatalf("after the push: %v\n%s", err, out)
	}
	if len(s.gh.secrets[secretPurge]+s.gh.secrets[secretZoneID]) != 0 || len(s.checks) != checks+1 {
		t.Errorf("secrets %v, the token on the server was checked %d times", s.gh.secrets, len(s.checks)-checks)
	}
	if out, err = runSetup(t, s.root, true, ""); err != nil || !strings.Contains(out, "matches tifa.yaml") {
		t.Errorf("third run: %v\n%s", err, out)
	}
}

// Until a token is on the server, the workflow setup writes keeps handing Tifa the two old
// secrets, whatever the plan expected: without a terminal, after a skipped paste, and after a
// write to the server that failed. Then the token moves, and the secrets wait for the push.
func TestSetupKeepsThePurgeSecretsUntilTheTokenMoves(t *testing.T) {
	s := newPurgeSetup(t, "", "tok-good", "tok-good")
	s.v018Repo(t)
	old, _ := os.ReadFile(filepath.Join(s.root, githubWorkflowPath))
	out, err := runSetup(t, s.root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "1. "+purgeItem) || !strings.Contains(out, "tifa deploy setup --cache-purge-token") || strings.Contains(out, "remove  github: the secrets") {
		t.Errorf("no terminal:\n%s", out)
	}
	if text, _ := os.ReadFile(filepath.Join(s.root, githubWorkflowPath)); string(text) != string(old) || s.gh.secrets[secretPurge] == "" || s.gh.secrets[secretZoneID] == "" {
		t.Errorf("secrets %v, workflow:\n%s", s.gh.secrets, text)
	}

	out, err = runSetup(t, s.root, true, "", "--yes")
	if err != nil || !strings.Contains(out, "skipped "+purgeItem) || !s.handsSecrets(t) || s.gh.secrets[secretPurge] == "" {
		t.Fatalf("skipped: %v, hands the secrets %v, secrets %v\n%s", err, s.handsSecrets(t), s.gh.secrets, out)
	}

	writeFile(t, filepath.Join(s.srv.dir, "storefail"), "")
	out, err = runSetup(t, s.root, true, "", "--yes")
	if err == nil || !strings.Contains(out, "warning "+purgeItem) || !s.handsSecrets(t) || s.gh.secrets[secretPurge] == "" {
		t.Fatalf("a failed write: %v, hands the secrets %v, secrets %v\n%s", err, s.handsSecrets(t), s.gh.secrets, out)
	}

	os.Remove(filepath.Join(s.srv.dir, "storefail"))
	out, err = runSetup(t, s.root, true, "", "--yes")
	if err != nil || s.srv.purge() != "example.com\tz\ttok-good\n" || s.handsSecrets(t) || s.gh.secrets[secretPurge] == "" || !strings.Contains(out, secretsStay) {
		t.Errorf("moved: %v, stored %q, secrets %v\n%s", err, s.srv.purge(), s.gh.secrets, out)
	}
}

// Setup checks the token the server has for the zone before it trusts it, as it checks a pasted
// one. One that Cloudflare refuses is asked for again, and the old secrets stay until a token on
// the server works.
func TestSetupChecksTheTokenOnTheServer(t *testing.T) {
	s := newPurgeSetup(t, "tok-good")
	s.v018Repo(t)
	s.gh.files[githubWorkflowPath] = githubWorkflow(githubWorkflowSpec{Branch: "main", Mode: connDirect})
	s.srv.havePurge(t, "tok-dead")
	setFacts(t, s.srv, factsOf("shop", 9531, tunnelFacts+"ghkey acme/shop\npurge-file yes\npurge-zone example.com\n"))
	out, err := runSetup(t, s.root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "1. "+purgeItem) || !strings.Contains(out, "the token the server has for example.com does not work") || strings.Contains(out, "Authentication error") == false {
		t.Fatalf("no terminal: %v\n%s", err, out)
	}
	if strings.Contains(out, "remove  github: the secrets") || s.gh.secrets[secretPurge] == "" || !s.handsSecrets(t) {
		t.Errorf("trusted a token that does not work: secrets %v\n%s", s.gh.secrets, out)
	}
	if !slices.Contains(s.checks, "POST /zones/z/purge_cache Bearer tok-dead "+purgeCheckBody) {
		t.Errorf("checks %q", s.checks)
	}
	out, err = runSetup(t, s.root, true, "", "--yes")
	if err != nil || !strings.Contains(out, "change  "+purgeItem) || s.srv.purge() != "example.com\tz\ttok-good\n" {
		t.Fatalf("terminal: %v, stored %q\n%s", err, s.srv.purge(), out)
	}
	if !strings.Contains(out, "done    github: the secrets") || len(s.gh.secrets[secretPurge]+s.gh.secrets[secretZoneID]) != 0 || s.handsSecrets(t) {
		t.Errorf("secrets %v\n%s", s.gh.secrets, out)
	}
	s.noSecretIn(t, out)
}

// A secret under the old name CF_PURGE_TOKEN is removed with the old variable.
func TestSetupPurgeTokenOldName(t *testing.T) {
	s := newPurgeSetup(t, "tok-good")
	if _, err := runSetup(t, s.root, true, "", "--yes"); err != nil {
		t.Fatal(err)
	}
	s.gh.secrets["CF_PURGE_TOKEN"] = "old"
	s.gh.vars["TIFA_DEPLOY"] = "old fingerprint"
	out, err := runSetup(t, s.root, true, "", "--yes")
	if err != nil || !strings.Contains(out, "remove  github: the old secrets CF_PURGE_TOKEN and the variable TIFA_DEPLOY, which tifa set under their old names") {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, ok := s.gh.secrets["CF_PURGE_TOKEN"]; ok {
		t.Error("the old secret is still there")
	}
}

// Without front, or without ci, the secrets an older setup set go, and the plan says to delete the
// token in Cloudflare, which tifa cannot do.
func TestSetupPurgeTokenRemoved(t *testing.T) {
	for name, yaml := range map[string]string{
		"front none": "site:\n  url: https://shop.example.com\ndeploy:\n  ci: github\n",
		"ci none":    "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n",
	} {
		s := newPurgeSetup(t, "tok-good")
		s.v018Repo(t)
		writeFile(t, filepath.Join(s.root, "tifa.yaml"), yaml)
		s.asked = len(s.prompt)
		out, err := runSetup(t, s.root, false, "", "--yes")
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		if !strings.Contains(out, "remove  github: the secret TIFA_DEPLOY_CF_PURGE_TOKEN, which tifa set") || !strings.Contains(out, `delete the token "tifa cache purge acme/shop" in the Cloudflare dashboard`) {
			t.Errorf("%s:\n%s", name, out)
		}
		if _, ok := s.gh.secrets[secretPurge]; ok {
			t.Errorf("%s: the secret is still there", name)
		}
		if _, ok := s.gh.secrets[secretZoneID]; ok {
			t.Errorf("%s: the zone secret is still there", name)
		}
	}
}

var sshHostName = regexp.MustCompile(`^deploy-a-ssh-[a-z2-7]{12}\.example\.com$`)

// deploy.ssh cloudflare: the ssh host name goes on the tunnel with an Access application that
// admits the token of the repository. Back to direct, the Access objects go.
func TestSetupCloudflareSSH(t *testing.T) {
	homeOf(t)
	root := setupApp(t, "site:\n  url: https://deploy-a.example.com\ndeploy:\n  front: cloudflare\n  ssh: cloudflare\n  ci: github\n", nil)
	srv := newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts))
	cf := cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	gh := newFakeGH()
	gh.install(t)
	// The cache token needs a terminal to paste on. It is optional, so setup only notes it.
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "(optional)") {
		t.Fatalf("%v\n%s", err, out)
	}
	host := gh.secrets[secretHost]
	if !sshHostName.MatchString(host) || gh.secrets[secretCFID] == "" || gh.secrets[secretCFSecret] == "" {
		t.Errorf("secrets %v", gh.secrets)
	}
	// ~/.tifa keeps the host that works from here: the tunnel, once the check passed.
	if known, err := readServers(); err != nil || len(known) != 1 || known[0].ID != "vps-1234" || known[0].Host != host {
		t.Errorf("recorded %+v, %v, want the ssh host %s", known, err, host)
	}
	if !strings.Contains(gh.secrets[secretKnownHosts], host+" ssh-ed25519") {
		t.Errorf("known hosts %q", gh.secrets[secretKnownHosts])
	}
	if len(cf.apps) != 1 || cf.apps[0]["domain"] != host {
		t.Errorf("apps %v", cf.apps)
	}
	// The Access application admits the token first and the person of the cf login after it.
	if got := policyIDs(cf.apps[0]); len(got) != 2 || got[1] != policyNamed(t, cf.fakeAccess, peopleName(host)) {
		t.Errorf("policies of the application: %v", cf.apps[0]["policies"])
	}
	text, _ := os.ReadFile(filepath.Join(root, githubWorkflowPath))
	if !strings.Contains(string(text), "ProxyCommand cloudflared access ssh") || strings.Contains(string(text), host) {
		t.Errorf("workflow:\n%s", text)
	}

	// A second run: nothing, no new token, and the same name.
	setFacts(t, srv, factsOf("shop", 9531, tunnelFacts+"ghkey acme/shop\n"))
	cf.calls = nil
	secret := gh.secrets[secretCFSecret]
	out, _ = runSetup(t, root, false, "", "--yes")
	if gh.secrets[secretCFSecret] != secret || gh.secrets[secretHost] != host || !strings.Contains(out, "Already right: server 2, app 1, Cloudflare 9, this machine 2, GitHub 3") {
		t.Errorf("a second run changed the token or the name:\n%s", out)
	}

	// The marker and the secret of the host are gone, as when a run stopped halfway: setup reads
	// the name back from the tunnel and the Access application, and does not make another.
	delete(gh.vars, varMarker)
	delete(gh.secrets, secretHost)
	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil || gh.secrets[secretHost] != host || len(cf.apps) != 1 || strings.Contains(out, "remove ") {
		t.Errorf("the name changed to %q from %q: %v\n%s", gh.secrets[secretHost], host, err, out)
	}
	// Only the Access application is left of it: the name is read from there.
	cf.configs["tunnel-1"]["ingress"] = []any{map[string]any{"service": "http_status:404"}}
	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil || gh.secrets[secretHost] != host || len(cf.apps) != 1 || !strings.Contains(out, "add     cloudflare: route "+host) {
		t.Errorf("the name was not read from the application: %v\n%s", err, out)
	}

	// Back to direct.
	writeFile(t, filepath.Join(root, "tifa.yaml"), "site:\n  url: https://deploy-a.example.com\ndeploy:\n  front: cloudflare\n  ci: github\n")
	newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, tunnelFacts+"ghkey acme/shop\n"))
	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "remove  cloudflare: the Access service token, policy and application of acme/shop") {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(cf.apps) != 0 || len(cf.tokens) != 0 || len(cf.policies) != 0 || gh.secrets[secretCFID] != "" {
		t.Errorf("left: apps %v tokens %v policies %v secrets %v", cf.apps, cf.tokens, cf.policies, gh.secrets)
	}
	// Back to the address that works without the tunnel.
	if known, _ := readServers(); len(known) != 1 || known[0].Host != "vps" {
		t.Errorf("recorded %+v, want the direct host vps", known)
	}
	ingress, _ := json.Marshal(cf.configs["tunnel-1"]["ingress"])
	if strings.Contains(string(ingress), "-ssh-") {
		t.Errorf("the ssh route is still there: %s", ingress)
	}
}

// While it plans, setup says which part it is reading, on stderr, one line each.
func TestSetupProgressOnStderr(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://deploy-a.example.com\ndeploy:\n  front: cloudflare\n  ssh: cloudflare\n  ci: github\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts))
	cfSetup(t, "example.com")
	newFakeGH().install(t)
	out, errOut, err := runSetupIO(t, root, false, "")
	if err == nil {
		t.Fatalf("no --yes and no terminal, but no error:\n%s", out)
	}
	want := "Reading the server...\nReading Cloudflare...\nReading GitHub...\n"
	if errOut != want {
		t.Errorf("stderr:\n%q\nwant\n%q", errOut, want)
	}
	if strings.Contains(out, "Reading") {
		t.Errorf("progress went to stdout:\n%s", out)
	}

	// Nothing of Cloudflare or GitHub is asked for here, so nothing is said about them.
	root = setupApp(t, "deploy:\n", nil)
	_, errOut, _ = runSetupIO(t, root, false, "")
	if errOut != "Reading the server...\n" {
		t.Errorf("stderr: %q", errOut)
	}
}

// One plan reads each thing once, however many steps look at it.
func TestSetupPlanReadsOnce(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://deploy-a.example.com\ndeploy:\n  front: cloudflare\n  ssh: cloudflare\n  ci: github\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts))
	cf := cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	gh := newFakeGH()
	gh.install(t)
	if _, err := runSetup(t, root, false, ""); err == nil {
		t.Fatal("no --yes and no terminal, but no error")
	}
	seen := map[string]int{}
	for _, c := range append(slices.Clone(cf.calls), gh.calls...) {
		seen[c]++
	}
	if len(seen) < 10 {
		t.Fatalf("too few calls, did the plan run?\n%v", seen)
	}
	for c, n := range seen {
		if n != 1 {
			t.Errorf("%q ran %d times in one plan", c, n)
		}
	}
}

// The reads that do not depend on each other run together: with every call slow, the plan takes
// far less than all its calls one after the other.
func TestSetupPlanReadsTogether(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://deploy-a.example.com\ndeploy:\n  front: cloudflare\n  ssh: cloudflare\n  ci: github\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts))
	cf := cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	gh := newFakeGH()
	gh.install(t)
	const delay = 100 * time.Millisecond
	innerCF, innerGH := runCF, runGH
	runCF = func(ctx context.Context, args ...string) ([]byte, error) {
		time.Sleep(delay)
		return innerCF(ctx, args...)
	}
	runGH = func(ctx context.Context, dir string, in io.Reader, args ...string) ([]byte, error) {
		time.Sleep(delay)
		return innerGH(ctx, dir, in, args...)
	}
	start := time.Now()
	if _, err := runSetup(t, root, false, ""); err == nil {
		t.Fatal("no --yes and no terminal, but no error")
	}
	calls := len(cf.calls) + len(gh.calls)
	took := time.Since(start)
	t.Logf("%d calls, %v", calls, took)
	if took > time.Duration(calls)*delay/2 {
		t.Errorf("%d calls of %v took %v: they ran one after the other", calls, delay, took)
	}
}

// Hidden ok lines: only the changes show, and one line counts what is right, by area.
func TestSetupHidesOKLines(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	newSetupServer(t, "", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine m\n"+hostKeysFact)
	out, err := runSetup(t, root, false, "")
	if err == nil {
		t.Fatalf("no --yes and no terminal, but no error:\n%s", out)
	}
	for _, want := range []string{"Plan for shop on vps", "add     app:", "Already right: server 1 (--all lists them)"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ok      ") {
		t.Errorf("an ok line shows:\n%s", out)
	}
	if strings.Index(out, "add     app:") > strings.Index(out, "Already right") {
		t.Errorf("order:\n%s", out)
	}
}

// --all lists the ok lines and drops the summary.
func TestSetupAllListsOKLines(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	newSetupServer(t, "", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine m\n"+hostKeysFact)
	out, _ := runSetup(t, root, false, "", "--all")
	if !strings.Contains(out, "ok      server:") || strings.Contains(out, "Already right") {
		t.Errorf("out:\n%s", out)
	}
}

// Everything is right and nobody has to do anything: one line, no plan, no question.
func TestSetupAllInOrderOneLine(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	homeOf(t)
	if _, err := rememberServer("vps-1234", "vps", at(root, "shop")); err != nil {
		t.Fatal(err)
	}
	newSetupServer(t, "", factsOf("shop", 9531, ""))
	out, err := runSetup(t, root, true, "")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out != "shop on vps matches tifa.yaml: 2 things checked (--all lists them).\n" {
		t.Errorf("out: %q", out)
	}
}

// What setup cannot do comes last, numbered, with the reason below each item. It makes the exit
// non-zero, after everything else was done.
func TestSetupCannotListedLast(t *testing.T) {
	root := setupApp(t, "deploy:\n  ci: github\n", nil)
	homeOf(t)
	if _, err := rememberServer("vps-1234", "vps", at(root, "shop")); err != nil {
		t.Fatal(err)
	}
	newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, ""))
	gh := newFakeGH()
	gh.loggedIn = false
	gh.install(t)
	out, err := runSetup(t, root, false, "")
	if err == nil || !strings.Contains(err.Error(), "1 thing needs you, see above") {
		t.Fatalf("err %v\n%s", err, out)
	}
	want := "Plan for shop on vps\n\nAlready right: server 1, app 1 (--all lists them)\n\nYou need to do 1 thing that setup cannot:\n  1. github: deploys from GitHub Actions\n     gh is not logged in: run gh auth login (gh auth status: You are not logged into any GitHub hosts).\n     Or without gh: make a fine-grained personal access token.\n     Open https://github.com/settings/personal-access-tokens/new?"
	if !strings.HasPrefix(out, want) || !strings.HasSuffix(out, "in the .env at the root of the app (git ignores it, and tifa only reads it)\n\n") || strings.Contains(out, "cannot  ") || strings.Contains(out, "Apply this plan") {
		t.Errorf("out:\n%s", out)
	}
}

// With changes and a cannot item, setup asks, applies, and then fails.
func TestSetupCannotAfterApply(t *testing.T) {
	root := setupApp(t, "deploy:\n  ci: github\n", nil)
	srv := newSetupServer(t, "hostname 203.0.113.7\n", "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine m\n"+hostKeysFact)
	gh := newFakeGH()
	gh.loggedIn = false
	gh.install(t)
	out, err := runSetup(t, root, false, "", "--yes")
	if err == nil || !strings.Contains(err.Error(), "needs you") || !strings.Contains(srv.calls(), "app tifa") {
		t.Fatalf("err %v\n%s\n%s", err, out, srv.calls())
	}
	if strings.Index(out, "  done    app:") < strings.Index(out, "You need to do") || strings.Contains(out, "could not be done") {
		t.Errorf("out:\n%s", out)
	}
}

// TIFA_DEPLOY_HOST holds the whole target, so the port is written even when it is 22. The zone ID
// is no secret of the connection any more: one an older setup set goes, and the connection stays.
func TestSetupConnectionSecrets(t *testing.T) {
	s := newPurgeSetup(t, "tok-good")
	s.srv = newSetupServer(t, "hostname 203.0.113.7\nport 22\n", factsOf("shop", 9531, tunnelFacts))
	if out, err := runSetup(t, s.root, true, "", "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := s.gh.secrets[secretHost]; got != "ssh://203.0.113.7:22" {
		t.Errorf("TIFA_DEPLOY_HOST %q", got)
	}
	if got := s.gh.secrets[secretKnownHosts]; !strings.HasPrefix(got, "203.0.113.7 ssh-ed25519 ") {
		t.Errorf("known hosts %q", got)
	}
	setFacts(t, s.srv, factsOf("shop", 9531, tunnelFacts+"ghkey acme/shop\npurge-file yes\npurge-zone example.com\n"))
	if out, err := runSetup(t, s.root, true, "", "--yes"); err != nil || !strings.Contains(out, "matches tifa.yaml") {
		t.Fatalf("second run: %v\n%s", err, out)
	}
	s.gh.secrets[secretZoneID] = "an old zone"
	out, err := runSetup(t, s.root, true, "", "--yes")
	if _, ok := s.gh.secrets[secretZoneID]; err != nil || ok || strings.Contains(out, "github: where CI logs in") || !strings.Contains(out, "remove  github: the secret TIFA_DEPLOY_CF_ZONE_ID, which tifa set and the workflow no longer uses") {
		t.Errorf("%v, secrets %v\n%s", err, s.gh.secrets, out)
	}
	delete(s.gh.vars, varMarker)
	if out, err = runSetup(t, s.root, true, "", "--yes"); err != nil || !strings.Contains(out, "change  github: where CI logs in (direct)") {
		t.Errorf("no marker: %v\n%s", err, out)
	}
}

// An earlier setup made the ssh host name from the domain alone, which anyone can guess. Setup
// replaces it: the new name gets its route, record and Access application, the old ones go, and
// the repository gets the new name. Remove finds the new name through the Access application.
func TestSetupReplacesTheOldSSHHost(t *testing.T) {
	const old = "deploy-a-ssh.example.com"
	root := setupApp(t, "site:\n  url: https://deploy-a.example.com\ndeploy:\n  front: cloudflare\n  ssh: cloudflare\n  ci: github\n", nil)
	srv := newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts+"ghkey acme/shop\n"))
	cf := cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	cf.configs["tunnel-1"] = map[string]any{"ingress": []any{
		map[string]any{"hostname": old, "service": sshService},
		map[string]any{"service": "http_status:404"},
	}}
	cf.records["example.com"] = []map[string]any{{"id": "record-old", "type": "CNAME", "name": old, "content": "tunnel-1.cfargotunnel.com", "proxied": true, "comment": dnsComment + "shop"}}
	cf.apps = []map[string]any{{"id": "app-old", "name": accessAppPrefix + old, "type": "self_hosted", "domain": old, "policies": []any{}}}
	gh := newFakeGH()
	gh.secrets[secretHost] = old
	gh.install(t)

	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	host := gh.secrets[secretHost]
	if !sshHostName.MatchString(host) {
		t.Fatalf("TIFA_DEPLOY_HOST %q\n%s", host, out)
	}
	for _, want := range []string{"add     cloudflare: route deploy-a-ssh-<random>.example.com -> ssh://localhost:22", "done    cloudflare: route " + host + " -> ssh://localhost:22", "add     cloudflare: DNS deploy-a-ssh-<random>.example.com", "remove  cloudflare: the ssh host name " + old} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	ingress, _ := json.Marshal(cf.configs["tunnel-1"]["ingress"])
	if strings.Contains(string(ingress), old) || !strings.Contains(string(ingress), host) {
		t.Errorf("ingress %s", ingress)
	}
	if len(cf.apps) != 1 || cf.apps[0]["domain"] != host {
		t.Errorf("apps %v", cf.apps)
	}
	for _, r := range cf.records["example.com"] {
		if r["name"] == old {
			t.Errorf("the old record is still there: %v", r)
		}
	}
	if !strings.Contains(gh.secrets[secretKnownHosts], host+" ssh-ed25519") {
		t.Errorf("known hosts %q", gh.secrets[secretKnownHosts])
	}
	// Run again: nothing to do, and the same name.
	setFacts(t, srv, factsOf("shop", 9531, tunnelFacts+"ghkey acme/shop\n"))
	out, err = runSetup(t, root, false, "", "--yes")
	if plain := withoutTunnelCheck(out); err != nil || strings.Contains(plain, "  add     ") || strings.Contains(plain, "  change  ") || strings.Contains(plain, "  remove  ") || gh.secrets[secretHost] != host {
		t.Fatalf("second run: %v\n%s", err, out)
	}
	// ci none: remove finds the name through the application and takes the route and record too.
	writeFile(t, filepath.Join(root, "tifa.yaml"), "site:\n  url: https://deploy-a.example.com\ndeploy:\n  front: cloudflare\n")
	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	ingress, _ = json.Marshal(cf.configs["tunnel-1"]["ingress"])
	if len(cf.apps) != 0 || strings.Contains(string(ingress), "-ssh") {
		t.Errorf("left: apps %v, ingress %s", cf.apps, ingress)
	}
}

// oldNameSecrets are the secrets and the variable earlier versions of setup set.
var oldNameSecrets = []string{"DEPLOY_SSH_KEY", "DEPLOY_KNOWN_HOSTS", "DEPLOY_HOST", "CF_ZONE_ID", "CF_CACHE_PURGE_TOKEN", "CF_PURGE_TOKEN", "CF_ACCESS_CLIENT_ID", "CF_ACCESS_CLIENT_SECRET", "TS_OAUTH_CLIENT_ID", "TS_OAUTH_SECRET"}

// Secrets under the old names, vouched for by the old variable TIFA_DEPLOY, are replaced: the new
// ones are set first, then the plan removes the old secrets and the old variable. A secret of
// the developer's own stays.
func TestSetupMigratesOldNames(t *testing.T) {
	eachBackend(t, func(t *testing.T, rest bool) {
		root := setupApp(t, "deploy:\n  ci: github\n", nil)
		srv := newSetupServer(t, "hostname 203.0.113.7\nport 2222\n", factsOf("shop", 9531, "ghkey acme/shop\n"))
		gh := newFakeGH()
		for _, n := range oldNameSecrets {
			gh.secrets[n] = "old " + n
		}
		gh.secrets["MY_OWN"] = "mine"
		gh.vars["TIFA_DEPLOY"] = "old fingerprint"
		gh.installBackend(t, rest)

		out, err := runSetup(t, root, false, "", "--yes")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		for _, n := range oldNameSecrets {
			if _, ok := gh.secrets[n]; ok {
				t.Errorf("the old secret %s is still there", n)
			}
		}
		if _, ok := gh.vars["TIFA_DEPLOY"]; ok {
			t.Error("the old variable is still there")
		}
		for _, n := range []string{secretSSHKey, secretKnownHosts, secretHost} {
			if gh.secrets[n] == "" || strings.HasPrefix(gh.secrets[n], "old ") {
				t.Errorf("the new secret %s was not set: %q", n, gh.secrets[n])
			}
		}
		if gh.vars[varMarker] == "" || gh.secrets["MY_OWN"] != "mine" {
			t.Errorf("vars %v, secrets %v", gh.vars, gh.secrets)
		}
		i, j := strings.Index(out, "add     github: where CI logs in"), strings.Index(out, "remove  github: the old secrets")
		if i < 0 || j < 0 || i > j {
			t.Errorf("the old names must go after the new ones are set:\n%s", out)
		}
		if !strings.Contains(out, "DEPLOY_HOST") || !strings.Contains(out, "the variable TIFA_DEPLOY,") || strings.Contains(out, "MY_OWN") {
			t.Errorf("plan:\n%s", out)
		}

		// Nothing is left to do.
		setFacts(t, srv, factsOf("shop", 9531, "ghkey acme/shop\n"))
		if out, err = runSetup(t, root, false, ""); err != nil || !strings.Contains(out, "matches tifa.yaml") {
			t.Errorf("second run: %v\n%s", err, out)
		}
	})
}

// Old names without the old variable are not Tifa's to remove.
func TestSetupKeepsOldNamesWithoutTheOldMarker(t *testing.T) {
	root := setupApp(t, "deploy:\n  ci: github\n", nil)
	newSetupServer(t, "hostname 203.0.113.7\nport 2222\n", factsOf("shop", 9531, ""))
	gh := newFakeGH()
	gh.secrets["DEPLOY_HOST"] = "mine"
	gh.secrets["CF_ZONE_ID"] = "mine"
	gh.install(t)
	if out, err := runSetup(t, root, false, "", "--yes"); err != nil || strings.Contains(out, "old secrets") {
		t.Fatalf("%v\n%s", err, out)
	}
	if gh.secrets["DEPLOY_HOST"] != "mine" || gh.secrets["CF_ZONE_ID"] != "mine" {
		t.Errorf("secrets %v", gh.secrets)
	}
}

// With ci none, setup takes away the old secrets and the old variable, and so does remove for an
// app with ci github.
func TestSetupAndRemoveDropOldNames(t *testing.T) {
	eachBackend(t, func(t *testing.T, rest bool) {
		for _, command := range []string{"setup", "remove"} {
			yaml := "deploy:\n"
			if command == "remove" {
				yaml = "deploy:\n  ci: github\n"
			}
			root := setupApp(t, yaml, nil)
			newSetupServer(t, "hostname 203.0.113.7\nport 2222\n", factsOf("shop", 9531, "ghkey acme/shop\n"))
			gh := newFakeGH()
			for _, n := range oldNameSecrets {
				gh.secrets[n] = "old"
			}
			gh.secrets["MY_OWN"] = "mine"
			gh.vars["TIFA_DEPLOY"] = "old fingerprint"
			gh.installBackend(t, rest)
			var out string
			var err error
			if command == "setup" {
				out, err = runSetup(t, root, false, "", "--yes")
			} else {
				out, _, err = runRemove(t, root, false, "", "--yes")
			}
			if err != nil {
				t.Fatalf("%s: %v\n%s", command, err, out)
			}
			if len(gh.secrets) != 1 || gh.secrets["MY_OWN"] != "mine" || len(gh.vars) != 0 {
				t.Errorf("%s: left %v %v\n%s", command, gh.secrets, gh.vars, out)
			}
		}
	})
}

// Without the user tifa, ssh says Permission denied and the plan says why in its own words: the
// user does not exist yet and setup creates it. The raw line of ssh stays out of the output.
func TestSetupNoDeployUserYet(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	srv := newSetupServer(t, "", freshFacts)
	srv.deny(t)
	out, errOut, _ := runSetupIO(t, root, false, "")
	if strings.Contains(out+errOut, "Permission denied") {
		t.Errorf("the plan shows what ssh said:\n%s\n%s", out, errOut)
	}
	if want := "add     server: the user tifa does not exist yet (ssh cannot log in as it), so setup creates it"; !strings.Contains(out, want) {
		t.Errorf("plan lacks %q:\n%s", want, out)
	}
}

// The tunnel is named after the machine id of the server, which a server without the user tifa
// cannot tell yet. The plan then shows a placeholder and not a cut name.
func TestSetupTunnelNameBeforeTheServerIsRead(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n", nil)
	srv := newSetupServer(t, "", freshFacts)
	srv.deny(t)
	cfSetup(t, "example.com")
	out, _ := runSetup(t, root, false, "")
	if want := "add     cloudflare: the tunnel of the server, tifa-<machine id>"; !strings.Contains(out, want) {
		t.Errorf("plan lacks %q:\n%s", want, out)
	}
	if strings.Contains(out, "server, tifa-\n") {
		t.Errorf("a cut name:\n%s", out)
	}
}

// The ssh host name is drawn when setup applies, so the plan of a run that applies nothing says
// that the random part is new, and the run that applies shows the name it made.
func TestSetupPlanSaysTheSSHNameIsNew(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://deploy-a.example.com\ndeploy:\n  front: cloudflare\n  ssh: cloudflare\n  ci: github\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts))
	cf := cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	gh := newFakeGH()
	gh.install(t)
	random := regexp.MustCompile(`deploy-a-ssh-[a-z2-7]{12}`)

	out, _ := runSetup(t, root, false, "")
	for _, want := range []string{"add     cloudflare: route deploy-a-ssh-<random>.example.com -> ssh://localhost:22", "add     cloudflare: DNS deploy-a-ssh-<random>.example.com", "the Access application of deploy-a-ssh-<random>.example.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
	if random.MatchString(out) {
		t.Errorf("the plan shows a name that the next run does not make:\n%s", out)
	}

	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	host := gh.secrets[secretHost]
	if !strings.Contains(out, "done    cloudflare: route "+host+" -> ssh://localhost:22") {
		t.Errorf("the done line lacks the name %s:\n%s", host, out)
	}
}

// policyIDs lists the IDs of the policies an Access application has, in order.
func policyIDs(app map[string]any) []string {
	var ids []string
	for _, p := range app["policies"].([]any) {
		ids = append(ids, p.(map[string]any)["id"].(string))
	}
	return ids
}

// policyNamed returns the ID of the policy with the name, or fails the test.
func policyNamed(t *testing.T, cf *fakeAccess, name string) string {
	t.Helper()
	for _, p := range cf.policies {
		if p["name"] == name {
			return p["id"].(string)
		}
	}
	t.Fatalf("no policy %s in %v", name, cf.policies)
	return ""
}

// emailsOf lists the emails of a policy.
func emailsOf(p map[string]any) []string {
	var include []map[string]any
	for _, in := range p["include"].([]any) {
		include = append(include, in.(map[string]any))
	}
	return includeEmails(include)
}

func policyByNameIn(cf *fakeAccess, name string) map[string]any {
	for _, p := range cf.policies {
		if p["name"] == name {
			return p
		}
	}
	return nil
}

const sshCloudflareNoCI = "site:\n  url: https://deploy-a.example.com\ndeploy:\n  front: cloudflare\n  ssh: cloudflare\n"

// deploy.ssh cloudflare needs no CI: the ssh host name, its route and record and its Access
// application are made for the machines of the people, and the application admits the email of
// the cf login. A second run changes nothing, a second person's email joins the first in the same
// policy, and the plan says so.
func TestSetupCloudflareSSHWithoutCI(t *testing.T) {
	root := setupApp(t, sshCloudflareNoCI, nil)
	srv := newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts))
	cf := cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "(optional)") {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"add     cloudflare: route deploy-a-ssh-<random>.example.com -> ssh://localhost:22",
		"add     cloudflare: DNS deploy-a-ssh-<random>.example.com CNAME the tunnel, proxied",
		"add     cloudflare: the Access application of deploy-a-ssh-<random>.example.com admits dev@example.com",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if len(cf.apps) != 1 || !sshHostName.MatchString(cf.apps[0]["domain"].(string)) {
		t.Fatalf("apps %v", cf.apps)
	}
	host := cf.apps[0]["domain"].(string)
	if len(cf.tokens) != 0 || len(cf.secrets) != 0 {
		t.Errorf("a service token was made without CI: %v", cf.tokens)
	}
	people := policyByNameIn(cf.fakeAccess, peopleName(host))
	if people == nil || people["decision"] != "allow" || !reflect.DeepEqual(emailsOf(people), []string{"dev@example.com"}) {
		t.Fatalf("people policy %v", people)
	}
	if got := policyIDs(cf.apps[0]); len(got) != 1 || got[0] != people["id"] {
		t.Errorf("the application has %v", cf.apps[0]["policies"])
	}
	ingress, _ := json.Marshal(cf.configs["tunnel-1"]["ingress"])
	if !strings.Contains(string(ingress), host) {
		t.Errorf("ingress %s", ingress)
	}

	// A second run with the same login changes nothing in Cloudflare.
	cf.calls = nil
	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatal(err)
	}
	if plain := withoutTunnelCheck(out); len(cf.writes()) != 0 || strings.Contains(plain, "  add     ") || strings.Contains(plain, "  change  ") {
		t.Errorf("a second run wrote %v:\n%s", cf.writes(), out)
	}

	// Another person with another login is added to the same policy, and the first stays.
	cf.email = "ana@example.com"
	out, _ = runSetup(t, root, false, "", "--yes", "--all")
	_ = out
	cf.calls = nil
	cf.email = "bo@example.com"
	out, _ = runSetup(t, root, false, "", "--yes")
	want := "change  cloudflare: the Access application of " + host + " admits bo@example.com, and keeps dev@example.com, ana@example.com"
	if !strings.Contains(out, want) {
		t.Errorf("lacks %q:\n%s", want, out)
	}
	people = policyByNameIn(cf.fakeAccess, peopleName(host))
	if got := emailsOf(people); !reflect.DeepEqual(got, []string{"dev@example.com", "ana@example.com", "bo@example.com"}) {
		t.Errorf("emails %v", got)
	}
	if len(cf.policies) != 1 || len(cf.apps) != 1 {
		t.Errorf("policies %d, apps %d", len(cf.policies), len(cf.apps))
	}
	_ = srv

	// Back to direct: the Access application, its policy, its route and its record go.
	writeFile(t, filepath.Join(root, "tifa.yaml"), "site:\n  url: https://deploy-a.example.com\ndeploy:\n  front: cloudflare\n")
	newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, tunnelFacts))
	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "remove  cloudflare: the Access application of the ssh host name of deploy-a.example.com") {
		t.Errorf("no removal in:\n%s", out)
	}
	ingress, _ = json.Marshal(cf.configs["tunnel-1"]["ingress"])
	if len(cf.apps) != 0 || len(cf.policies) != 0 || strings.Contains(string(ingress), "-ssh-") {
		t.Errorf("left: apps %v policies %v ingress %s", cf.apps, cf.policies, ingress)
	}
}

// An app set up by v0.2.1 with CI has the application, the token and its policy. Running setup
// again adds the people policy and nothing else: no new token, and the application stays.
func TestSetupCloudflareSSHKeepsAV021App(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://deploy-a.example.com\ndeploy:\n  front: cloudflare\n  ssh: cloudflare\n  ci: github\n", nil)
	srv := newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts))
	cf := cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	gh := newFakeGH()
	gh.install(t)
	if _, err := runSetup(t, root, false, "", "--yes"); err != nil && !strings.Contains(err.Error(), "needs you") {
		t.Fatal(err)
	}
	host := gh.secrets[secretHost]
	// Take away what v0.2.1 did not make: the people policy and its link.
	for i, p := range cf.policies {
		if p["name"] == peopleName(host) {
			cf.policies = append(cf.policies[:i], cf.policies[i+1:]...)
			break
		}
	}
	cf.apps[0]["policies"] = cf.apps[0]["policies"].([]any)[:1]
	secret := gh.secrets[secretCFSecret]
	appID := cf.apps[0]["id"]
	setFacts(t, srv, factsOf("shop", 9531, tunnelFacts+"ghkey acme/shop\n"))
	cf.calls = nil
	out, _ := runSetup(t, root, false, "", "--yes")
	if !strings.Contains(out, "add     cloudflare: the Access application of "+host+" admits dev@example.com") {
		t.Errorf("no people line:\n%s", out)
	}
	if gh.secrets[secretCFSecret] != secret || cf.apps[0]["id"] != appID || len(cf.apps) != 1 || len(cf.tokens) != 1 {
		t.Errorf("the token or the application was replaced: %v %v", cf.tokens, cf.apps)
	}
	for _, w := range cf.writes() {
		if strings.Contains(w, "service-tokens") || strings.Contains(w, "applications delete") || strings.Contains(w, "applications create") || strings.Contains(w, "rotate") {
			t.Errorf("wrote %s", w)
		}
	}
	if got := policyIDs(cf.apps[0]); len(got) != 2 {
		t.Errorf("policies %v", cf.apps[0]["policies"])
	}
}

// cf logged in with an API token has no email: without CI that is something for the person to do,
// with CI it is only a note, since CI has its token.
func TestSetupCloudflareSSHNoEmail(t *testing.T) {
	root := setupApp(t, sshCloudflareNoCI, nil)
	newSetupServer(t, "", factsOf("shop", 9531, tunnelFacts))
	cf := cfSetup(t, "example.com")
	cf.tunnels["tunnel-1"] = "tifa-vps-1234"
	cf.email = ""
	out, err := runSetup(t, root, false, "", "--yes")
	if err == nil || !strings.Contains(out, "does not tell your email") || len(cf.policies) != 0 {
		t.Errorf("%v\n%s", err, out)
	}
}

// withoutTunnelCheck drops the lines of the check of the tunnel, which a setup whose
// TIFA_DEPLOY_HOST comes from the environment runs each time.
func withoutTunnelCheck(out string) string {
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		if !strings.Contains(l, "this machine: check that ssh reaches") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}
