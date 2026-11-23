package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/config"
)

// The app is named after its module, as tifa build names the program, so one server tells the
// apps apart without a setting. A name that is unsafe in a path or a unit name is refused.
func TestDeployAppName(t *testing.T) {
	for module, want := range map[string]string{
		"github.com/liustack/tifa/examples/shop": "shop",
		"example.com/team/notes/v2":              "notes",
		"my_app":                                 "my_app",
		"example.com/Blog.v1":                    "Blog.v1",
	} {
		if got, err := deployAppName(module); err != nil || got != want {
			t.Errorf("%s: %q, %v, want %q", module, got, err, want)
		}
	}
	for _, module := range []string{"example.com/.hidden", "example.com/-x", "example.com/a+b", "example.com/a b"} {
		if got, err := deployAppName(module); err == nil {
			t.Errorf("%s: %q, want an error", module, got)
		}
	}
}

// uname names the system and the machine. Only Linux runs the systemd units.
func TestDeployServerTarget(t *testing.T) {
	for in, want := range map[[2]string][2]string{
		{"Linux", "x86_64"}:  {"linux", "amd64"},
		{"Linux", "aarch64"}: {"linux", "arm64"},
		{"Linux", "arm64"}:   {"linux", "arm64"},
	} {
		goos, goarch, err := serverTarget(in[0], in[1])
		if err != nil || [2]string{goos, goarch} != want {
			t.Errorf("%v: %s/%s, %v", in, goos, goarch, err)
		}
	}
	for _, in := range [][2]string{{"Linux", "armv7l"}, {"Darwin", "arm64"}, {"FreeBSD", "amd64"}, {"", ""}} {
		if _, _, err := serverTarget(in[0], in[1]); err == nil {
			t.Errorf("%v: no error", in)
		}
	}
}

// Setup takes the lowest port from 9527 that no app of the deploy user has and nothing listens on.
func TestDeployPickPort(t *testing.T) {
	ss := "LISTEN 0      4096      127.0.0.54:53 0.0.0.0:*\nLISTEN 0      4096   127.0.0.53%lo:53 0.0.0.0:*\nLISTEN 0      128          0.0.0.0:22 0.0.0.0:*\nLISTEN 0 4096 [::]:9528 [::]:*\nLISTEN 0 4096 *:9530 *:*\n"
	used := listeningPorts(ss)
	if want := []int{53, 53, 22, 9528, 9530}; !reflect.DeepEqual(used, want) {
		t.Fatalf("listening %v, want %v", used, want)
	}
	if got, err := pickPort(append(used, 9527, 9529)); err != nil || got != 9531 {
		t.Errorf("got %d, %v", got, err)
	}
	if got, err := pickPort(nil); err != nil || got != 9527 {
		t.Errorf("empty server: %d, %v", got, err)
	}
	all := []int{}
	for p := firstPort; p <= lastPort; p++ {
		all = append(all, p)
	}
	if _, err := pickPort(all); err == nil {
		t.Error("no free port, no error")
	}
}

func TestDeployParseState(t *testing.T) {
	out := strings.Join([]string{
		"os Linux",
		"arch x86_64",
		"port 9527",
		"current 20261120T100000Z",
		"release 20261120T090000Z",
		"release 20261120T100000Z",
		"record 2026-11-20T09:00:05Z\t20261120T090000Z\tabc123\tdeploy\tok",
		"record 2026-11-20T10:00:05Z\t20261120T100000Z\tdef456-dirty\tdeploy\tok",
		"",
	}, "\n")
	s, err := parseServerState(out)
	if err != nil {
		t.Fatal(err)
	}
	want := serverState{
		OS: "Linux", Arch: "x86_64", Port: 9527, Current: "20261120T100000Z",
		Releases: []string{"20261120T090000Z", "20261120T100000Z"},
		Records: []deployRecord{
			{Time: "2026-11-20T09:00:05Z", ID: "20261120T090000Z", Commit: "abc123", Action: "deploy", Result: "ok"},
			{Time: "2026-11-20T10:00:05Z", ID: "20261120T100000Z", Commit: "def456-dirty", Action: "deploy", Result: "ok"},
		},
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("got %+v\nwant %+v", s, want)
	}
	for _, bad := range []string{"port x\n", "record a\tb\n", "what 1\n"} {
		if _, err := parseServerState(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func state(current string, releases []string, records ...string) serverState {
	s := serverState{Current: current, Releases: releases}
	for _, r := range records {
		f := strings.Fields(r) // id result
		s.Records = append(s.Records, deployRecord{ID: f[0], Commit: "c" + f[0], Action: "deploy", Result: f[1]})
	}
	return s
}

// A rollback goes to the newest release before the current one that passed its health check
// and is still on the server.
func TestDeployRollbackTarget(t *testing.T) {
	s := state("5", []string{"1", "2", "3", "4", "5"}, "1 ok", "2 ok", "3 ok", "4 failed", "5 ok")
	if got, err := rollbackTarget(s); err != nil || got.ID != "3" || got.Commit != "c3" {
		t.Errorf("got %+v, %v", got, err)
	}
	// After a rollback to 3, the next one goes to 2.
	s.Current = "3"
	if got, err := rollbackTarget(s); err != nil || got.ID != "2" {
		t.Errorf("from 3: %+v, %v", got, err)
	}
	// A release that was pruned is gone.
	s = state("5", []string{"4", "5"}, "3 ok", "4 failed", "5 ok")
	if _, err := rollbackTarget(s); err == nil || !strings.Contains(err.Error(), "no earlier release") {
		t.Errorf("none left: %v", err)
	}
	if _, err := rollbackTarget(state("", nil)); err == nil || !strings.Contains(err.Error(), "nothing is deployed") {
		t.Errorf("nothing deployed: %v", err)
	}
}

// When a new release fails its check, the app goes back to the one that ran before, if that one
// ever passed. A first deploy that fails has nothing to go back to.
func TestDeployFallback(t *testing.T) {
	if got := fallback(state("2", []string{"1", "2"}, "1 ok", "2 ok")); got != "2" {
		t.Errorf("got %q", got)
	}
	if got := fallback(state("2", []string{"1", "2"}, "1 ok", "2 failed")); got != "" {
		t.Errorf("current failed: %q", got)
	}
	if got := fallback(state("", nil)); got != "" {
		t.Errorf("first deploy: %q", got)
	}
}

// The last releases stay, the current one and the new one always.
func TestDeployPrune(t *testing.T) {
	got := pruneReleases([]string{"1", "2", "3", "4", "5", "6", "7"}, "2", "8", 5)
	if want := []string{"1", "3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := pruneReleases([]string{"1", "2"}, "2", "3", 5); len(got) != 0 {
		t.Errorf("few: %v", got)
	}
}

// The units listen on the loopback port of the app and run the program of the current release
// in the shared folder, which holds .env and the data.
func TestDeployUnits(t *testing.T) {
	socket, service := socketUnit("shop", 9531), serviceUnit("shop")
	for _, want := range []string{"[Socket]\nListenStream=127.0.0.1:9531\n", "WantedBy=sockets.target"} {
		if !strings.Contains(socket, want) {
			t.Errorf("socket unit lacks %q:\n%s", want, socket)
		}
	}
	for _, want := range []string{
		"Requires=tifa-shop.socket\nAfter=tifa-shop.socket\n",
		"WorkingDirectory=%h/shop/shared\n",
		"ExecStart=%h/shop/current/shop\n",
		"Restart=on-failure\n",
		"WantedBy=default.target",
	} {
		if !strings.Contains(service, want) {
			t.Errorf("service unit lacks %q:\n%s", want, service)
		}
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{"": "''", "a b": "'a b'", "it's": `'it'\''s'`} {
		if got := shellQuote(in); got != want {
			t.Errorf("%q: %s, want %s", in, got, want)
		}
	}
}

// The server comes from --host, else TIFA_DEPLOY_HOST of the environment, else TIFA_DEPLOY_HOST in the .env
// at the root of the app. tifa.yaml has no host: the file is committed.
func TestDeployHost(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
	writeFile(t, filepath.Join(root, "tifa.yaml"), "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n  auto: {}\n")
	t.Setenv(deployHostEnv, "")
	writeFile(t, filepath.Join(root, ".env"), "# the server\nOTHER=1\nTIFA_DEPLOY_HOST=ssh://203.0.113.7:2222\n")
	if d, err := loadTarget(root, ""); err != nil || d.host != "ssh://203.0.113.7:2222" || d.dest.name != "203.0.113.7" || d.dest.port != 2222 || d.app != "shop" || d.domain != "shop.example.com" || !d.github {
		t.Errorf(".env: %+v, %v", d, err)
	}
	t.Setenv(deployHostEnv, "from-env")
	if d, err := loadTarget(root, ""); err != nil || d.host != "from-env" {
		t.Errorf("env beats .env: %+v, %v", d, err)
	}
	if d, err := loadTarget(root, "from-flag"); err != nil || d.host != "from-flag" {
		t.Errorf("flag beats env: %+v, %v", d, err)
	}
	if _, err := loadTarget(root, "root@vps"); err == nil || !strings.Contains(err.Error(), "--host") {
		t.Errorf("bad flag: %v", err)
	}
	t.Setenv(deployHostEnv, "-oProxyCommand=x")
	if _, err := loadTarget(root, ""); err == nil || !strings.Contains(err.Error(), deployHostEnv) {
		t.Errorf("bad env: %v", err)
	}
	t.Setenv(deployHostEnv, "")
	writeFile(t, filepath.Join(root, ".env"), "TIFA_DEPLOY_HOST=tifa@vps\n")
	if _, err := loadTarget(root, ""); err == nil || !strings.Contains(err.Error(), ".env") || !strings.Contains(err.Error(), "user") {
		t.Errorf("bad .env: %v", err)
	}
	if err := os.Remove(filepath.Join(root, ".env")); err != nil {
		t.Fatal(err)
	}
	_, err := loadTarget(root, "")
	if err == nil {
		t.Fatal("no server named, and no error")
	}
	for _, want := range []string{"--host", "environment variable TIFA_DEPLOY_HOST", ".env", "TIFA_DEPLOY_HOST=ssh://"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("no host: %q lacks %q", err, want)
		}
	}
}

// A tifa.yaml that still has deploy.host stops every deploy command, and says where the server goes.
func TestDeployHostRetired(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
	writeFile(t, filepath.Join(root, "tifa.yaml"), "deploy:\n  host: vps\n")
	t.Setenv(deployHostEnv, "vps")
	_, err := loadTarget(root, "vps")
	if err == nil {
		t.Fatal("deploy.host was accepted")
	}
	for _, want := range []string{"deploy.host", "line 2", "TIFA_DEPLOY_HOST=", ".env", "--host"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q lacks %q", err, want)
		}
	}
}

// The deploy settings are checked against each other where the target is loaded, so every deploy
// command refuses a mix that cannot work, and says what to change.
func TestDeploySettingsChecks(t *testing.T) {
	site := "site:\n  url: https://shop.example.com\n"
	for name, tc := range map[string]struct {
		yaml, want string // want is empty when the settings are fine
	}{
		"none":                       {"deploy:\n", ""},
		"front needs a site url":     {"deploy:\n  front: cloudflare\n", "site.url"},
		"front with a port":          {"site:\n  url: https://shop.example.com:8443\ndeploy:\n  front: cloudflare\n", "port"},
		"port without front":         {"site:\n  url: http://localhost:8080", ""},
		"ssh cloudflare needs front": {site + "deploy:\n  connect: cloudflare\n  auto: {}\n", "deploy.front"},
		"ssh tailscale needs ci":     {site + "deploy:\n  connect: tailscale\n", "deploy.auto"},
		"ssh cloudflare without ci":  {site + "deploy:\n  front: cloudflare\n  connect: cloudflare\n", ""},
		"all set":                    {site + "deploy:\n  front: cloudflare\n  connect: cloudflare\n  auto: {}\n", ""},
		"old domain":                 {"deploy:\n  domain: shop.example.com\n", "deploy.front"},
		"old cdn":                    {"deploy:\n  cdn: cloudflare\n", "it is now deploy.front"},
		"old github":                 {"deploy:\n  github: true\n", "deploy.auto"},
	} {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
		writeFile(t, filepath.Join(root, "tifa.yaml"), tc.yaml)
		t.Setenv(deployHostEnv, "vps")
		d, err := loadTarget(root, "")
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: %v", name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: got %v, want %q", name, err, tc.want)
		}
		if tc.want == "" && d.domain != map[bool]string{true: "shop.example.com"}[strings.Contains(tc.yaml, "front: cloudflare")] {
			t.Errorf("%s: domain %q", name, d.domain)
		}
	}
}

// fakeServerArgv returns every command line the ssh of the last fakeServer got.
var fakeServerArgv func() string

// fakeServer makes a folder of programs that stand in for ssh and for the tools the scripts call
// on the server, so the scripts run on this machine with home as the home of the deploy user.
// health holds what GET /healthz answers for a release, 404 when it is not listed. calls returns
// the ssh and systemctl calls so far.
func fakeServer(t *testing.T, home string, health map[string]string) (calls func() string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the server scripts run under sh")
	}
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "calls")
	tool := func(name, body string) {
		writeFile(t, filepath.Join(bin, name), "#!/bin/sh\n"+body+"\n")
		if err := os.Chmod(filepath.Join(bin, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	healthDir := t.TempDir()
	for id, code := range health {
		writeFile(t, filepath.Join(healthDir, id), code)
	}
	argv := log + ".argv"
	fakeServerArgv = func() string {
		data, _ := os.ReadFile(argv)
		return string(data)
	}
	// Every argument of ssh is logged whole, so a test can tell no secret is on a command line.
	tool("ssh", `printf '%s\n' "$*" >> `+shellQuote(argv)+`
while [ "$1" != "--" ]; do if [ "$1" = -l ]; then user=$2; fi; shift; done
echo "ssh $user $2" >> `+shellQuote(log)+`
HOME=`+shellQuote(home)+` XDG_RUNTIME_DIR=`+shellQuote(filepath.Join(home, ".run"))+` PATH=`+shellQuote(bin)+`:"$PATH" exec sh -c "$3"`)
	// show answers with the file .run/show of the home, which a test writes.
	tool("systemctl", `echo "systemctl $*" >> `+shellQuote(log)+`
case " $* " in *" show "*) cat "$HOME/.run/show" 2>/dev/null ;; esac`)
	tool("curl", `rel=$(basename "$(readlink "$HOME/shop/current")")
cat `+shellQuote(healthDir)+`/"$rel" 2>/dev/null || printf 404`)
	tool("loginctl", `cat `+shellQuote(filepath.Join(home, ".run", "linger"))+` 2>/dev/null || echo no`)
	tool("flock", `echo "flock $*" >> `+shellQuote(log))
	tool("sleep", "exit 0")
	// With -f it says so and waits as the one process of the ssh, so killing the ssh ends it.
	tool("journalctl", `echo "journalctl $*" >> `+shellQuote(log)+`
case " $* " in *" -f "*) echo following; exec /bin/sleep 30 ;; esac
echo the app said goodbye`)
	tool("uname", `case "$1" in -s) echo Linux ;; -m) echo x86_64 ;; esac`)
	if _, err := exec.LookPath("sha256sum"); err != nil {
		tool("sha256sum", `shasum -a 256 "$@"`)
	}
	old := sshProgram
	sshProgram = filepath.Join(bin, "ssh")
	t.Cleanup(func() { sshProgram = old })
	return func() string {
		data, _ := os.ReadFile(log)
		return string(data)
	}
}

// tifa deploy builds for the server, uploads, switches and checks. A release that fails its
// check is recorded and the one before runs again, and tifa deploy rollback goes back past it.
func TestDeployFlow(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
	writeFile(t, filepath.Join(root, "tifa.yaml"), "deploy:\n")
	t.Setenv(deployHostEnv, "vps")
	home := t.TempDir()
	for _, dir := range []string{"shop/releases", "shop/shared"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(home, "shop", "port"), "9531\n")
	ids := []string{"20261120T100000Z", "20261120T110000Z", "20261120T120000Z"}
	calls := fakeServer(t, home, map[string]string{ids[1]: "503"})

	clock := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	oldNow, oldBuild := deployNow, buildForServer
	t.Cleanup(func() { deployNow, buildForServer = oldNow, oldBuild })
	deployNow = func() time.Time { t := clock; clock = clock.Add(time.Hour); return t }
	var targets []string
	buildForServer = func(_ context.Context, _, goos, goarch, out string, _, _ io.Writer) error {
		targets = append(targets, goos+"/"+goarch)
		return os.WriteFile(out, []byte("#!/bin/sh\necho "+goos+"\n"), 0o755)
	}
	current := func() string {
		c, _ := os.Readlink(filepath.Join(home, "shop", "current"))
		return c
	}
	records := func() string {
		data, _ := os.ReadFile(filepath.Join(home, "shop", "deploys.log"))
		var out []string
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			f := strings.Split(line, "\t")
			out = append(out, strings.Join(f[1:], " "))
		}
		return strings.Join(out, "\n")
	}

	out, stderr, err := run(t, "deploy", "--dir", root)
	if err != nil {
		t.Fatalf("first deploy: %v\n%s%s", err, out, stderr)
	}
	if current() != "releases/"+ids[0] || !reflect.DeepEqual(targets, []string{"linux/amd64"}) {
		t.Fatalf("current %s, built for %v\n%s", current(), targets, out)
	}
	if !strings.Contains(calls(), "systemctl --user restart tifa-shop.service") || !strings.Contains(calls(), "ssh tifa vps") {
		t.Errorf("calls:\n%s", calls())
	}
	if !strings.Contains(out, "health check: GET /healthz answered 404") {
		t.Errorf("output:\n%s", out)
	}

	out, _, err = run(t, "deploy", "--dir", root)
	if err == nil || !strings.Contains(out, "going back to "+ids[0]) || !strings.Contains(out, "the app said goodbye") {
		t.Fatalf("failing deploy: %v\n%s", err, out)
	}
	if current() != "releases/"+ids[0] {
		t.Errorf("after the failed deploy current is %s", current())
	}

	if out, _, err := run(t, "deploy", "--dir", root); err != nil {
		t.Fatalf("third deploy: %v\n%s", err, out)
	}
	out, _, err = run(t, "deploy", "rollback", "--dir", root)
	if err != nil || current() != "releases/"+ids[0] {
		t.Fatalf("rollback: %v, current %s\n%s", err, current(), out)
	}
	want := strings.Join([]string{
		ids[0] + " none deploy ok",
		ids[1] + " none deploy failed",
		ids[2] + " none deploy ok",
		ids[0] + " none rollback ok",
	}, "\n")
	if got := records(); got != want {
		t.Errorf("deploys.log:\n%s\nwant\n%s", got, want)
	}
	for _, id := range ids {
		data, err := os.ReadFile(filepath.Join(home, "shop", "releases", id, "shop"))
		if err != nil || string(data) != "#!/bin/sh\necho linux\n" {
			t.Errorf("release %s: %q, %v", id, data, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "shop", "shared", ".env")); err == nil {
		t.Error("a deploy wrote .env")
	}
}

// Before setup the server has no port for the app, and tifa deploy says to run setup.
func TestDeployNeedsSetup(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
	t.Setenv(deployHostEnv, "vps")
	fakeServer(t, t.TempDir(), nil)
	if _, _, err := run(t, "deploy", "--dir", root); err == nil || !strings.Contains(err.Error(), "tifa deploy setup") {
		t.Errorf("got %v", err)
	}
}

// A first deploy that fails its check has nothing to go back to, so the app is stopped.
func TestDeployFirstReleaseFails(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
	t.Setenv(deployHostEnv, "vps")
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "shop", "port"), "9531\n")
	if err := os.MkdirAll(filepath.Join(home, "shop", "releases"), 0o755); err != nil {
		t.Fatal(err)
	}
	calls := fakeServer(t, home, map[string]string{"20261120T100000Z": "500"})
	oldNow, oldBuild := deployNow, buildForServer
	t.Cleanup(func() { deployNow, buildForServer = oldNow, oldBuild })
	deployNow = func() time.Time { return time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC) }
	buildForServer = func(_ context.Context, _, _, _, out string, _, _ io.Writer) error {
		return os.WriteFile(out, []byte("program"), 0o755)
	}
	out, _, err := run(t, "deploy", "--dir", root)
	if err == nil || !strings.Contains(err.Error(), "no release runs") || !strings.Contains(out, "answered 500") {
		t.Fatalf("got %v\n%s", err, out)
	}
	if !strings.Contains(calls(), "systemctl --user stop tifa-shop.service") {
		t.Errorf("the app was not stopped:\n%s", calls())
	}
}

// prepareFakeServer makes the fake server one that setup prepared: its systemd runs at boot.
func prepareFakeServer(t *testing.T, home string) {
	t.Helper()
	writeFile(t, filepath.Join(home, ".run", "linger"), "yes\n")
	writeFile(t, filepath.Join(home, ".run", "bus"), "")
}

// On a prepared server that has no folder for the app yet, tifa deploy makes it as tifa, with no
// root: the folder, an empty .env, a port that is free and the units. Its units name the port.
func TestDeployCreatesTheApp(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
	t.Setenv(deployHostEnv, "vps")
	home := t.TempDir()
	prepareFakeServer(t, home)
	writeFile(t, filepath.Join(home, "other", "port"), "9527\n")
	calls := fakeServer(t, home, nil)
	oldNow, oldBuild := deployNow, buildForServer
	t.Cleanup(func() { deployNow, buildForServer = oldNow, oldBuild })
	deployNow = func() time.Time { return time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC) }
	buildForServer = func(_ context.Context, _, _, _, out string, _, _ io.Writer) error {
		return os.WriteFile(out, []byte("program"), 0o755)
	}
	out, _, err := run(t, "deploy", "--dir", root)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if port, _ := os.ReadFile(filepath.Join(home, "shop", "port")); string(port) != "9528\n" {
		t.Errorf("port %q", port)
	}
	if st, err := os.Stat(filepath.Join(home, "shop", "shared", ".env")); err != nil || st.Size() != 0 {
		t.Errorf(".env: %v", err)
	}
	socket, _ := os.ReadFile(filepath.Join(home, ".config", "systemd", "user", "tifa-shop.socket"))
	if string(socket) != socketUnit("shop", 9528) {
		t.Errorf("socket unit %q", socket)
	}
	for _, want := range []string{"ssh tifa vps", "systemctl --user enable --now tifa-shop.socket"} {
		if !strings.Contains(calls(), want) {
			t.Errorf("calls lack %q:\n%s", want, calls())
		}
	}
	if strings.Contains(calls(), "ssh root") || !strings.Contains(out, "created shop on vps") {
		t.Errorf("root was used, or no word about it:\n%s\n%s", calls(), out)
	}
}

// The probe tells what the server and the app are like, and tolerates a server without ss or
// loginctl.
func TestDeployParseProbeFacts(t *testing.T) {
	out := strings.Join([]string{
		"os Linux", "arch aarch64", "home /srv/tifa", "bus yes", "linger yes", "port 9528",
		"taken 9527", "listen LISTEN 0 4096 127.0.0.1:9600 0.0.0.0:*",
		"unit-socket aa", "unit-service bb", "tunnel t-1", "machine vps-1234", "cloudflared /usr/bin/cloudflared",
		"tunnel-active yes", "apt yes", "hostkey ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA", "ghkey acme/shop",
		"appdir yes", "other notes", "cloudflared-tifa yes",
		"unit-active active", "unit-since Fri 2026-11-20 12:00:05 UTC", "unit-rss 38000", "health 200",
		"purge-file yes", "purge-zone example.com", "purge-zone example.org", "",
	}, "\n")
	s, err := parseServerState(out)
	if err != nil {
		t.Fatal(err)
	}
	want := serverState{OS: "Linux", Arch: "aarch64", Home: "/srv/tifa", Bus: true, Linger: true, Port: 9528,
		Used: []int{9527, 9600}, SocketSum: "aa", ServiceSum: "bb", TunnelID: "t-1", Machine: "vps-1234",
		Cloudflared: "/usr/bin/cloudflared", TunnelActive: true, Apt: true,
		HostKeys: []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA"}, GitHubKeys: []string{"acme/shop"},
		AppDir: true, Others: []string{"notes"}, CloudflaredByTifa: true,
		Active: "active", ActiveSince: "Fri 2026-11-20 12:00:05 UTC", RSS: 38000, Health: "200",
		PurgeFile: true, PurgeZones: []string{"example.com", "example.org"}}
	if !reflect.DeepEqual(s, want) || !s.Prepared() {
		t.Errorf("got %+v", s)
	}
	for _, bad := range []string{"hostkey ssh-ed25519 !!!\n", "unit-rss many\n", "health x\n"} {
		if _, err := parseServerState(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// In GitHub Actions the log hides the secret TIFA_DEPLOY_HOST, but ssh prints the address it resolved,
// which is not that exact text. Deploy and rollback first ask the log to hide the host part of
// the target as well, and never the port.
func TestDeployMasksTheHostInActions(t *testing.T) {
	mask := func(host string, actions string) string {
		t.Helper()
		t.Setenv(envGitHubActions, actions)
		h, err := parseDeployHost(host)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		maskHost(&b, deployTarget{host: h.raw, dest: h})
		return b.String()
	}
	for host, want := range map[string]string{
		"ssh://203.0.113.7:2222":     "::add-mask::203.0.113.7\n",
		"ssh://[2001:db8::1]:22":     "::add-mask::2001:db8::1\n",
		"ssh://vps.example.com:22":   "::add-mask::vps.example.com\n",
		"deploy-a-ssh-x.example.com": "::add-mask::deploy-a-ssh-x.example.com\n",
	} {
		if got := mask(host, "true"); got != want {
			t.Errorf("%s: %q, want %q", host, got, want)
		}
	}
	if got := mask("ssh://203.0.113.7:2222", ""); got != "" {
		t.Errorf("outside Actions: %q", got)
	}

	// Before anything reaches the server.
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
	t.Setenv(deployHostEnv, "ssh://203.0.113.7:2222")
	t.Setenv(envGitHubActions, "true")
	fakeServer(t, t.TempDir(), nil)
	for _, args := range [][]string{{"deploy", "--dir", root}, {"deploy", "rollback", "--dir", root}} {
		out, _, _ := run(t, args...)
		if !strings.HasPrefix(out, "::add-mask::203.0.113.7\n") || strings.Contains(out, "::add-mask::203.0.113.7:2222") {
			t.Errorf("%v: output %q", args, out)
		}
	}
}

// deploy.ssh and deploy.ci are gone from tifa.yaml. check, build and every deploy command stop at
// the line and say what to write instead.
func TestDeployOldNamesStopEveryCommand(t *testing.T) {
	for name, tc := range map[string]struct{ yaml, want string }{
		"ssh": {"deploy:\n  front: none\n  ssh: cloudflare\n", "tifa.yaml line 3: deploy.ssh is now deploy.connect: write connect: cloudflare"},
		"ci":  {"deploy:\n  ci: github\n", "tifa.yaml line 2: deploy.ci is now the block deploy.auto: replace ci: github with auto: {}"},
	} {
		dir := apiApp(t)
		writeFile(t, filepath.Join(dir, "tifa.yaml"), tc.yaml)
		t.Setenv(deployHostEnv, "vps")
		for _, args := range [][]string{{"check"}, {"build", "-o", filepath.Join(t.TempDir(), "app")}, {"deploy", "status"}, {"deploy", "setup", "--yes"}, {"deploy", "remove", "--yes"}} {
			out, stderr, err := run(t, append(args, "--dir", dir)...)
			if got := out + stderr; err == nil || !strings.Contains(got+err.Error(), tc.want) {
				t.Errorf("%s: tifa %s: err %v, output %q, want %q", name, strings.Join(args, " "), err, got, tc.want)
			}
		}
	}
}

// check, build and dev stop on deploy settings that cannot work together, with the messages the
// deploy commands give, and on a needs file that is missing, has no name or does not run on a push
// to the branch tifa.yaml names. None of it needs the network.
func TestCheckRunsTheDeployChecks(t *testing.T) {
	for name, tc := range map[string]struct {
		yaml  string
		files map[string]string
		want  string
	}{
		"tailscale without auto":        {"deploy:\n  connect: tailscale\n", nil, "deploy.connect is tailscale, which is how GitHub Actions reaches the server"},
		"cloudflare without front":      {"deploy:\n  connect: cloudflare\n", nil, "deploy.connect is cloudflare, and the ssh host name is made from the domain of site.url"},
		"front without site url":        {"deploy:\n  front: cloudflare\n", nil, "deploy.front is cloudflare, which needs site.url"},
		"needs missing":                 {"deploy:\n  auto: {}\n", nil, ".github/workflows/ci.yml does not exist"},
		"needs without name":            {"deploy:\n  auto: {}\n", map[string]string{testCIPath: "on: push\n"}, "has no name: line"},
		"needs not on push":             {"deploy:\n  auto:\n    branch: main\n", map[string]string{testCIPath: "name: CI\non: pull_request\n"}, "does not run on a push to main"},
		"needs on no push at all":       {"deploy:\n  auto: {}\n", map[string]string{testCIPath: "name: CI\non: pull_request\n"}, "does not run on a push (its on: has no push)"},
		"needs on tag pushes only":      {"deploy:\n  auto: {}\n", map[string]string{testCIPath: "name: CI\non:\n  push:\n    tags: ['v*']\n"}, "does not run on a push"},
		"needs not on the named branch": {"deploy:\n  auto:\n    branch: release\n", map[string]string{testCIPath: testCI}, "does not run on a push to release"},
	} {
		dir := apiApp(t)
		writeFile(t, filepath.Join(dir, "tifa.yaml"), tc.yaml)
		for f, text := range tc.files {
			writeFile(t, filepath.Join(dir, f), text)
		}
		out, stderr, err := run(t, "check", "--dir", dir)
		if got := out + stderr; err == nil || !strings.Contains(got+err.Error(), tc.want) {
			t.Errorf("%s: err %v, output %q, want %q", name, err, got, tc.want)
		}
	}
	// With the default branch the branch is not matched, since it is on GitHub: some push is enough.
	dir := apiApp(t)
	writeFile(t, filepath.Join(dir, "tifa.yaml"), "deploy:\n  auto: {}\n")
	writeFile(t, filepath.Join(dir, testCIPath), "name: CI\non:\n  push:\n    branches: [develop]\n")
	if out, stderr, err := run(t, "check", "--dir", dir); err != nil {
		t.Errorf("default branch: %v\n%s%s", err, out, stderr)
	}
}

// An empty value of a key of deploy.auto says what to write.
func TestEmptyAutoKeysSayWhatToWrite(t *testing.T) {
	for yaml, want := range map[string]string{
		"deploy:\n  auto:\n    needs:\n":  "deploy.auto.needs: write a workflow file such as ci.yml, or none",
		"deploy:\n  auto:\n    branch:\n": "deploy.auto.branch: write a branch name, or leave branch out for the default branch",
	} {
		_, _, err := config.Project([]byte(yaml))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", yaml, err)
		}
	}
}

// tifa routes and tifa gen read the app, not its deploy settings: they run with settings the deploy
// checks would stop.
func TestRoutesIgnoresDeploySettings(t *testing.T) {
	dir := apiApp(t)
	writeFile(t, filepath.Join(dir, "tifa.yaml"), "deploy:\n  connect: tailscale\n")
	if out, stderr, err := run(t, "routes", "--dir", dir); err != nil {
		t.Fatalf("%v\n%s%s", err, out, stderr)
	}
}

// tifa dev shows the deploy problems as warnings, once, and keeps running: they do not touch the
// running app. They come again when the settings or the needs file change to another problem, and
// go when fixed. tifa.yaml values that do not parse are still errors, and stop dev before.
func TestDevWarnsOfDeploySettings(t *testing.T) {
	dir := apiApp(t)
	var out strings.Builder
	p := &devPipeline{root: dir, warn: &out}
	tifaYAML := filepath.Join(dir, "tifa.yaml")
	writeFile(t, tifaYAML, "deploy:\n  connect: tailscale\n")
	for range 2 {
		if err := p.checkDeploy(); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Count(out.String(), "deploy.connect is tailscale"); got != 1 || !strings.Contains(out.String(), "(warning)") {
		t.Fatalf("printed %d times:\n%s", got, out.String())
	}
	if w := strings.Join(p.Warnings(), "\n"); !strings.Contains(w, "deploy.connect is tailscale") {
		t.Errorf("Warnings: %q", w)
	}
	writeFile(t, tifaYAML, "deploy:\n  auto: {}\n")
	if err := p.checkDeploy(); err != nil || !strings.Contains(out.String(), ".github/workflows/ci.yml does not exist") {
		t.Errorf("needs missing: %v\n%s", err, out.String())
	}
	writeFile(t, filepath.Join(dir, testCIPath), testCI)
	if err := p.checkDeploy(); err != nil || len(p.Warnings()) != 0 {
		t.Errorf("fixed: %v, %q", err, p.Warnings())
	}
	writeFile(t, filepath.Join(dir, testCIPath), "on: push\n")
	before := out.Len()
	if err := p.checkDeploy(); err != nil || !strings.Contains(out.String()[before:], "has no name: line") {
		t.Errorf("again: %v\n%s", err, out.String()[before:])
	}
}
