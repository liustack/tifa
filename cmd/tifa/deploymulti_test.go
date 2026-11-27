package main

// tifa deploy to several servers: the list of hosts, the steps that run once per deploy and those
// that run on each server, a failure on a later server that moves the earlier ones back, and the
// commands that look at or act on every server.

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The host is one server, or several separated by commas, each written as one host is. --host
// may be given more than once, and each value may hold several too. An empty entry and a server
// named twice are refused.
func TestParseDeployHosts(t *testing.T) {
	for in, want := range map[string][]string{
		"vps":                            {"vps"},
		"vps,vps2":                       {"vps", "vps2"},
		" vps , ubuntu@vps2 ":            {"vps", "ubuntu@vps2"},
		"ssh://203.0.113.7:2222,vps2":    {"ssh://203.0.113.7:2222", "vps2"},
		"ubuntu@203.0.113.7,203.0.113.8": {"ubuntu@203.0.113.7", "203.0.113.8"},
	} {
		hs, err := parseDeployHosts([]string{in})
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		var got []string
		for _, h := range hs {
			got = append(got, h.raw)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	if hs, err := parseDeployHosts([]string{"vps", "vps2,vps3"}); err != nil || len(hs) != 3 || hs[2].raw != "vps3" {
		t.Errorf("repeated: %+v, %v", hs, err)
	}
	for in, want := range map[string]string{
		"vps,":                      "empty",
		",vps":                      "empty",
		"vps,,vps2":                 "empty",
		"vps,vps":                   "twice",
		"vps,ubuntu@vps":            "twice",
		"ssh://vps:22,ssh://vps:22": "twice",
		"vps,tifa@vps2":             "tifa",
	} {
		if _, err := parseDeployHosts([]string{in}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", in, err, want)
		}
	}
}

// twoServers makes an app at root with tifa.yaml and two fake servers that setup prepared, vps at
// 203.0.113.7 and vps2 at 203.0.113.8, with the app on port 9531 on each. The program the build
// makes writes where it ran into the file ran of the home, with its arguments.
func twoServers(t *testing.T, tifaYAML string, health map[string]string) (root, home1, home2 string, calls func() string) {
	t.Helper()
	root = t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n\ngo 1.27.0\n")
	writeFile(t, filepath.Join(root, "tifa.yaml"), tifaYAML)
	homeOf(t)
	home1, home2 = t.TempDir(), t.TempDir()
	for _, home := range []string{home1, home2} {
		for _, dir := range []string{"shop/releases", "shop/shared"} {
			if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		writeFile(t, filepath.Join(home, "shop", "port"), "9531\n")
		prepareFakeServer(t, home)
	}
	calls = fakeServer(t, home1, health)
	addFakeHost(t, "vps", home1, "203.0.113.7", "box1")
	addFakeHost(t, "vps2", home2, "203.0.113.8", "box2")
	clock := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	oldNow, oldBuild := deployNow, buildForServer
	t.Cleanup(func() { deployNow, buildForServer = oldNow, oldBuild })
	deployNow = func() time.Time { c := clock; clock = clock.Add(time.Hour); return c }
	builds := 0
	buildForServer = func(_ context.Context, _, _, _, out string, _, _ io.Writer) error {
		builds++
		writeFile(t, filepath.Join(root, ".builds"), strings.Repeat("x", builds))
		return os.WriteFile(out, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$HOME/ran\"\necho \"ran on $(cat \"$HOME/.run/hostname\")\"\n"), 0o755)
	}
	return root, home1, home2, calls
}

func currentOf(home string) string {
	c, _ := os.Readlink(filepath.Join(home, "shop", "current"))
	return strings.TrimPrefix(c, "releases/")
}

func recordsOf(home string) string {
	data, _ := os.ReadFile(filepath.Join(home, "shop", "deploys.log"))
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if f := strings.Split(line, "\t"); len(f) == 5 {
			out = append(out, f[1]+" "+f[3]+" "+f[4])
		}
	}
	return strings.Join(out, "\n")
}

// A deploy to two servers builds once, uploads to both, runs the command before the start once,
// on the first server, and switches the servers one at a time. Both end on the new release.
func TestDeploySeveralServers(t *testing.T) {
	root, home1, home2, _ := twoServers(t, "deploy:\n  front: external\n  release_command: migrate --all\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	out, stderr, err := run(t, "deploy", "--dir", root)
	if err != nil {
		t.Fatalf("%v\n%s%s", err, out, stderr)
	}
	id := "20261120T100000Z"
	if currentOf(home1) != id || currentOf(home2) != id {
		t.Fatalf("current: %s and %s\n%s", currentOf(home1), currentOf(home2), out)
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".builds")); string(b) != "x" {
		t.Errorf("built %d times", len(b))
	}
	// The command before the start ran once, on the first server.
	if ran, _ := os.ReadFile(filepath.Join(home1, "ran")); string(ran) != "migrate --all\n" {
		t.Errorf("the first server ran %q", ran)
	}
	if ran, err := os.ReadFile(filepath.Join(home2, "ran")); err == nil {
		t.Errorf("the second server ran %q", ran)
	}
	if n := strings.Count(out, "deploy.release_command:"); n != 1 {
		t.Errorf("the command ran %d times:\n%s", n, out)
	}
	for _, want := range []string{"deploying shop to vps, vps2", "switching vps (1 of 2)", "switching vps2 (2 of 2)", "deployed shop release " + id} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// The switch of the second comes after the first passed its check.
	if i, j := strings.Index(out, "switching vps (1 of 2)"), strings.Index(out, "switching vps2 (2 of 2)"); i < 0 || j < i ||
		!strings.Contains(out[i:j], "health check") {
		t.Errorf("order:\n%s", out)
	}
	for _, home := range []string{home1, home2} {
		if got := recordsOf(home); got != id+" deploy ok" {
			t.Errorf("deploys.log: %q", got)
		}
	}
}

// When the second server fails its check, it goes back by itself, and the first one, which
// passed, is moved back to the release it ran before, so both run one release again.
func TestDeploySeveralServersMovesBack(t *testing.T) {
	root, home1, home2, _ := twoServers(t, "deploy:\n  front: external\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	first, second := "20261120T100000Z", "20261120T110000Z"
	if out, _, err := run(t, "deploy", "--dir", root); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	writeFile(t, filepath.Join(home2, ".run", "health-"+second), "503")
	out, _, err := run(t, "deploy", "--dir", root)
	if err == nil {
		t.Fatalf("the deploy passed:\n%s", out)
	}
	if currentOf(home1) != first || currentOf(home2) != first {
		t.Fatalf("current: %s and %s\n%s", currentOf(home1), currentOf(home2), out)
	}
	for _, want := range []string{"going back to " + first, "moving vps back to " + first} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, want := range []string{"vps2", "failed its health check", "vps", first} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	// The release failed on vps2, so it is failed on vps too, which ran it and went back.
	if got, want := recordsOf(home1), first+" deploy ok\n"+second+" deploy ok\n"+first+" rollback ok\n"+second+" deploy failed"; got != want {
		t.Errorf("deploys.log of vps:\n%s\nwant\n%s", got, want)
	}
	if got, want := recordsOf(home2), first+" deploy ok\n"+second+" deploy failed"; got != want {
		t.Errorf("deploys.log of vps2:\n%s\nwant\n%s", got, want)
	}
	// Neither status nor rollback offers it.
	out, _, err = run(t, "deploy", "status", "--dir", root)
	if err != nil || strings.Contains(out, second+"  commit none  passed") || strings.Count(out, second+"  commit none  failed its health check") != 2 {
		t.Errorf("status: %v\n%s", err, out)
	}
	if out, _, err := run(t, "deploy", "rollback", second, "--dir", root); err == nil || currentOf(home1) != first {
		t.Errorf("rolled back to the failed release: %v\n%s", err, out)
	}
}

// When the first server fails, the servers after it never switched, but they have the release:
// it is failed there too, so no rollback can pick it.
func TestDeploySeveralServersFailedOnTheFirst(t *testing.T) {
	root, home1, home2, _ := twoServers(t, "deploy:\n  front: external\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	first, second := "20261120T100000Z", "20261120T110000Z"
	if out, _, err := run(t, "deploy", "--dir", root); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	writeFile(t, filepath.Join(home1, ".run", "health-"+second), "503")
	if out, _, err := run(t, "deploy", "--dir", root); err == nil {
		t.Fatalf("the deploy passed:\n%s", out)
	}
	if currentOf(home1) != first || currentOf(home2) != first {
		t.Fatalf("current: %s and %s", currentOf(home1), currentOf(home2))
	}
	if got, want := recordsOf(home2), first+" deploy ok\n"+second+" deploy failed"; got != want {
		t.Errorf("deploys.log of vps2:\n%s\nwant\n%s", got, want)
	}
}

// A first deploy that fails on the second server leaves nothing to go back to: the app stops on
// every server that started the release, and the error says so.
func TestDeploySeveralServersFirstDeployFails(t *testing.T) {
	root, home1, home2, calls := twoServers(t, "deploy:\n  front: external\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	id := "20261120T100000Z"
	writeFile(t, filepath.Join(home2, ".run", "health-"+id), "503")
	out, _, err := run(t, "deploy", "--dir", root)
	if err == nil {
		t.Fatalf("the deploy passed:\n%s", out)
	}
	if !strings.Contains(err.Error(), "shop is stopped on every server that started it") {
		t.Errorf("error: %v\n%s", err, out)
	}
	if n := strings.Count(calls(), "systemctl --user stop tifa-shop.service"); n != 2 {
		t.Errorf("stopped %d times:\n%s", n, calls())
	}
	for _, home := range []string{home1, home2} {
		if got := recordsOf(home); got != id+" deploy failed" && got != id+" deploy ok\n"+id+" deploy failed" {
			t.Errorf("deploys.log: %q", got)
		}
	}
}

// When deploy.release_command fails, the release is deleted from every server it went to, and the
// first one records why.
func TestDeploySeveralServersReleaseCommandFails(t *testing.T) {
	root, home1, home2, _ := twoServers(t, "deploy:\n  front: external\n  release_command: migrate\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	first, second := "20261120T100000Z", "20261120T110000Z"
	if out, _, err := run(t, "deploy", "--dir", root); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// The program of the next build fails every command.
	buildForServer = func(_ context.Context, _, _, _, out string, _, _ io.Writer) error {
		return os.WriteFile(out, []byte("#!/bin/sh\nexit 7\n"), 0o755)
	}
	out, _, err := run(t, "deploy", "--dir", root)
	if err == nil {
		t.Fatalf("the deploy passed:\n%s", out)
	}
	for _, home := range []string{home1, home2} {
		if _, err := os.Stat(filepath.Join(home, "shop", "releases", second)); err == nil {
			t.Errorf("%s keeps the release", home)
		}
		if currentOf(home) != first {
			t.Errorf("current of %s: %s", home, currentOf(home))
		}
	}
	if got := recordsOf(home1); got != first+" deploy ok\n"+second+" deploy "+releaseCommandFailed {
		t.Errorf("deploys.log of vps: %q", got)
	}
}

// A deploy that names fewer servers than ~/.tifa keeps for the app is refused: the server left out
// would keep running an older release behind the balancer.
func TestDeploySeveralServersFewerThanRemembered(t *testing.T) {
	root, home1, _, _ := twoServers(t, "deploy:\n  front: external\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	if out, _, err := run(t, "deploy", "--dir", root); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Setenv(deployHostEnv, "vps")
	out, _, err := run(t, "deploy", "--dir", root)
	want := "go tool tifa deploy remove --host vps2"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("got %v, want %q\n%s", err, want, out)
	}
	if strings.Count(currentOf(home1), "Z") != 1 || currentOf(home1) != "20261120T100000Z" {
		t.Errorf("vps changed: %s", currentOf(home1))
	}
}

// When a server that is moved back fails its check with the old release too, it stops: it never
// runs the release that failed on another server again. The deploy says which server runs what.
func TestDeploySeveralServersMoveBackFails(t *testing.T) {
	root, home1, home2, _ := twoServers(t, "deploy:\n  front: external\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	first, second := "20261120T100000Z", "20261120T110000Z"
	if out, _, err := run(t, "deploy", "--dir", root); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	writeFile(t, filepath.Join(home2, ".run", "health-"+second), "503")
	// The first release fails on vps from now on.
	writeFile(t, filepath.Join(home1, ".run", "health-"+first), "500")
	out, _, err := run(t, "deploy", "--dir", root)
	if err == nil {
		t.Fatalf("the deploy passed:\n%s", out)
	}
	if currentOf(home2) != first {
		t.Fatalf("current of vps2: %s\n%s", currentOf(home2), out)
	}
	for _, want := range []string{"vps runs no release", "vps2 runs " + first} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

// Several servers share the load behind a load balancer of the user, and no other front: none
// serves HTTPS on each machine itself, nginx serves the apps of one server, and a tunnel on several
// sends each request to the closest one. They are refused when the target loads, for every command,
// and the error names external.
func TestDeploySeveralServersRefused(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n\ngo 1.27.0\n")
	t.Setenv(deployHostEnv, "vps,vps2")
	for yaml, want := range map[string]string{
		"deploy:\n":                 "2 servers, and deploy.front is none, where the server serves visitors itself, with HTTPS of its own: set deploy.front to external in tifa.yaml",
		"deploy:\n  front: nginx\n": "deploy.front is nginx, which serves the apps of one server: set deploy.front to external",
		"site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n": "deploy.front is cloudflare, and a tunnel on several servers sends each request to the one closest to the visitor, which does not spread the load: set deploy.front to external",
	} {
		writeFile(t, filepath.Join(root, "tifa.yaml"), yaml)
		for _, args := range [][]string{{"deploy"}, {"deploy", "status"}, {"deploy", "rollback"}, {"deploy", "setup"}, {"deploy", "remove"}, {"deploy", "env", "list"}} {
			_, _, err := run(t, append(args, "--dir", root)...)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%q %v: got %v, want %q", yaml, args, err, want)
			}
		}
	}
}

// One host behaves as before: the messages name no server number.
func TestDeployOneHostSaysNoNumbers(t *testing.T) {
	root, _, _, _ := twoServers(t, "deploy:\n  front: external\n", nil)
	t.Setenv(deployHostEnv, "vps")
	out, _, err := run(t, "deploy", "--dir", root)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "(1 of") || strings.Contains(out, "switching") || !strings.Contains(out, "deploying shop to vps (linux/amd64, port 9531)") {
		t.Errorf("output:\n%s", out)
	}
}

// rollback goes back on every server, one at a time, and status looks at each.
func TestDeploySeveralServersRollbackAndStatus(t *testing.T) {
	root, home1, home2, _ := twoServers(t, "deploy:\n  front: external\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	first, second := "20261120T100000Z", "20261120T110000Z"
	for range 2 {
		if out, _, err := run(t, "deploy", "--dir", root); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	out, _, err := run(t, "deploy", "status", "--dir", root)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"shop on vps, port 9531", "shop on vps2, port 9531", "release  " + second} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
	out, _, err = run(t, "deploy", "status", "--json", "--dir", root)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var list []appStatus
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list) != 2 || list[0].Server.Host != "vps" || list[1].Server.Host != "vps2" || list[1].Release != second {
		t.Errorf("status --json: %v\n%s", err, out)
	}

	out, _, err = run(t, "deploy", "rollback", "--dir", root)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if currentOf(home1) != first || currentOf(home2) != first {
		t.Errorf("after rollback: %s and %s\n%s", currentOf(home1), currentOf(home2), out)
	}
	if !strings.Contains(out, "rolling shop on vps (1 of 2) back") || !strings.Contains(out, "rolling shop on vps2 (2 of 2) back") {
		t.Errorf("output:\n%s", out)
	}
	if _, _, err := run(t, "deploy", "rollback", second, "--dir", root); err != nil || currentOf(home1) != second || currentOf(home2) != second {
		t.Errorf("rollback to %s: %v", second, err)
	}
}

// run runs the command on the first server, or on the one --host names.
func TestDeploySeveralServersRun(t *testing.T) {
	root, home1, home2, _ := twoServers(t, "deploy:\n  front: external\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	if out, _, err := run(t, "deploy", "--dir", root); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	out, _, err := run(t, "deploy", "run", "--dir", root, "seed", "--count", "5")
	if err != nil || !strings.Contains(out, "ran on box1") {
		t.Fatalf("%v\n%s", err, out)
	}
	if ran, _ := os.ReadFile(filepath.Join(home2, "ran")); len(ran) > 0 {
		t.Errorf("vps2 ran %q", ran)
	}
	out, _, err = run(t, "deploy", "run", "--dir", root, "--host", "vps2", "seed")
	if err != nil || !strings.Contains(out, "ran on box2") {
		t.Fatalf("%v\n%s", err, out)
	}
	if ran, _ := os.ReadFile(filepath.Join(home1, "ran")); string(ran) != "seed --count 5\n" {
		t.Errorf("vps ran %q", ran)
	}
}

// With deploy.front external the app listens on its port on every address, so the balancer of
// the user reaches it, and the deploy prints what to enter in the balancer. One host works too.
func TestDeployLoadBalancer(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n\ngo 1.27.0\n")
	writeFile(t, filepath.Join(root, "tifa.yaml"), "site:\n  url: https://shop.example.com\ndeploy:\n  front: external\n")
	t.Setenv(deployHostEnv, "vps")
	homeOf(t)
	home := t.TempDir()
	prepareFakeServer(t, home)
	calls := fakeServer(t, home, nil)
	oldNow, oldBuild := deployNow, buildForServer
	t.Cleanup(func() { deployNow, buildForServer = oldNow, oldBuild })
	deployNow = func() time.Time { return time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC) }
	buildForServer = func(_ context.Context, _, _, _, out string, _, _ io.Writer) error {
		return os.WriteFile(out, []byte("program"), 0o755)
	}
	writeFile(t, filepath.Join(home, ".run", "open-9527"), "")
	out, _, err := run(t, "deploy", "--dir", root)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	socket, _ := os.ReadFile(filepath.Join(home, ".config", "systemd", "user", "tifa-shop.socket"))
	if !strings.Contains(string(socket), "ListenStream=9527\n") || string(socket) != unitsOf("shop", 9527, "", true).socket {
		t.Errorf("socket unit %q", socket)
	}
	for _, want := range []string{
		"load balancer or proxy: send plain HTTP to this target, and check it with GET /healthz, which a status from 200 to 499 passes",
		"  203.0.113.7:9527",
		"203.0.113.7:9527 answers this machine, so it may be open to anyone",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "visitors: none yet") {
		t.Errorf("output:\n%s", out)
	}
	// The health check on the server still asks the loopback.
	if !strings.Contains(calls(), "http://127.0.0.1:9527/healthz") {
		t.Errorf("calls:\n%s", calls())
	}
	// Behind Cloudflare or nginx the socket stays on the loopback, byte for byte as before.
	if u := unitsOf("shop", 9527, "", false).socket; !strings.Contains(u, "ListenStream=127.0.0.1:9527\n") {
		t.Errorf("loopback unit %q", u)
	}
}

// env set and unset change the .env of every server, one at a time, each restarted and checked.
// list prints the names once, and says when the servers differ.
func TestDeploySeveralServersEnv(t *testing.T) {
	root, home1, home2, _ := twoServers(t, "deploy:\n  front: external\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	if out, _, err := run(t, "deploy", "--dir", root); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	out, err := runEnv(t, "s3cr3t\n", false, "deploy", "env", "set", "API_KEY", "--dir", root)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, home := range []string{home1, home2} {
		if data, _ := os.ReadFile(filepath.Join(home, "shop", "shared", ".env")); string(data) != "API_KEY=s3cr3t\n" {
			t.Errorf(".env of %s: %q", home, data)
		}
	}
	if !strings.Contains(out, "set API_KEY on vps, shop restarted") || !strings.Contains(out, "set API_KEY on vps2, shop restarted") || strings.Contains(out, "s3cr3t") {
		t.Errorf("output:\n%s", out)
	}
	if out, err := runEnv(t, "", false, "deploy", "env", "list", "--dir", root); err != nil || out != "API_KEY\n" {
		t.Errorf("list: %v %q", err, out)
	}
	writeFile(t, filepath.Join(home2, "shop", "shared", ".env"), "API_KEY=x\nOTHER=1\n")
	if out, err := runEnv(t, "", false, "deploy", "env", "list", "--dir", root); err == nil || !strings.Contains(out, "vps2: API_KEY OTHER") {
		t.Errorf("list of servers that differ: %v %q", err, out)
	}
	if out, err := runEnv(t, "", false, "deploy", "env", "unset", "API_KEY", "--dir", root); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if data, _ := os.ReadFile(filepath.Join(home2, "shop", "shared", ".env")); string(data) != "OTHER=1\n" {
		t.Errorf(".env of vps2 after unset: %q", data)
	}
}

// fakePorts makes this machine reach no port of a server, for the setups below.
func fakePorts(t *testing.T) {
	old := portAnswers
	t.Cleanup(func() { portAnswers = old })
	portAnswers = func(context.Context, string, int) bool { return false }
}

// box2Facts is the probe of a second server that setup prepared, without the app.
const box2Facts = "os Linux\narch x86_64\nhome /srv/tifa\nbus yes\nlinger yes\nmachine box2-5678\napt yes\n" + hostKeysFact

// Setup of an app on two servers behind a load balancer sets up each server in turn, gives the app
// one port on both, free on both, on every address, and ends with what to enter in the balancer.
// ~/.tifa keeps both servers, in order.
func TestSetupSeveralServersLoadBalancer(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: external\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	homeOf(t)
	fakePorts(t)
	srv := newSetupServer(t, "", factsOf("shop", 9527, "appdir yes\n"))
	// blog has port 9527 on the second server, so the app moves to 9528 on both.
	two := srv.addHost(t, "vps2", "hostname 203.0.113.8\n", box2Facts+"taken 9527\nother blog\n")
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"Plan for shop on vps (1 of 2)",
		"Plan for shop on vps2 (2 of 2)",
		"app: move shop from 127.0.0.1:9527, which only the server reaches, to port 9528 on every address of the server, for the load balancer or proxy in front",
		"app: /srv/tifa/shop with an empty shared/.env, port 9528 on every address of the server, for the load balancer or proxy in front",
		"load balancer or proxy: send plain HTTP to these targets",
		"vps:9528", "203.0.113.8:9528",
		"Let only it reach port 9528",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if n := strings.Count(srv.unquoted(), "ListenStream=9528\n"); n < 2 {
		t.Errorf("the units of port 9528 went to %d servers:\n%s", n, srv.unquoted())
	}
	if !strings.Contains(srv.calls(), "app tifa") || !strings.Contains(two.calls(), "app tifa") {
		t.Errorf("calls:\n%s\n%s", srv.calls(), two.calls())
	}
	servers, err := readServers()
	if err != nil || len(servers) != 2 {
		t.Fatalf("remembered %+v, %v", servers, err)
	}
	for _, s := range servers {
		if len(s.Apps) != 1 || !reflect.DeepEqual(s.Apps[0].Hosts, []string{"vps", "vps2"}) {
			t.Errorf("%s: %+v", s.ID, s.Apps)
		}
	}
	// The next command that names no server gets both, in order.
	t.Setenv(deployHostEnv, "")
	var said strings.Builder
	ts, err := loadDeployTargets(root, nil, &said)
	if err != nil || len(ts) != 2 || ts[0].host != "vps" || ts[1].host != "vps2" || ts[1].dest.knownID != "box2-5678" {
		t.Errorf("targets from ~/.tifa: %+v, %v", ts, err)
	}
}

// GitHub Actions of an app on several servers gets them all in one round after the servers: the
// secret TIFA_DEPLOY_HOST lists each, the host keys of each are trusted, and one key of the
// repository logs in to each.
func TestSetupSeveralServersGitHub(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: external\n  auto: {}\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	homeOf(t)
	fakePorts(t)
	srv := newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9527, "appdir yes\n"))
	two := srv.addHost(t, "vps2", "hostname 203.0.113.8\n", box2Facts)
	gh := newFakeGH()
	gh.install(t)
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "Plan for shop on GitHub Actions, which deploys it to vps, vps2") {
		t.Errorf("output:\n%s", out)
	}
	if got := gh.secrets[secretHost]; got != "ssh://203.0.113.7:22,ssh://203.0.113.8:22" {
		t.Errorf("%s is %q", secretHost, got)
	}
	if hosts := gh.secrets[secretKnownHosts]; !strings.Contains(hosts, "203.0.113.7 ") || !strings.Contains(hosts, "203.0.113.8 ") {
		t.Errorf("%s is %q", secretKnownHosts, hosts)
	}
	for _, s := range []*setupServer{srv, two} {
		if strings.Count(s.calls(), "authorize tifa") != 1 {
			t.Errorf("calls:\n%s", s.calls())
		}
	}
	// A second run has nothing to do on GitHub, once the servers list the key.
	for _, s := range []*setupServer{srv, two} {
		f, err := os.OpenFile(filepath.Join(s.dir, "facts"), os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString("ghkey acme/shop\n")
		f.Close()
	}
	out, err = runSetup(t, root, false, "", "--yes")
	if err != nil || !strings.Contains(out, "shop on GitHub Actions matches tifa.yaml") {
		t.Errorf("%v\n%s", err, out)
	}
}

// A deploy to several servers sets up no app: a server without it, or with it on another port than
// the first server, needs setup, which gives the app one port on all of them. Nothing changes.
func TestDeploySeveralServersNeedSetup(t *testing.T) {
	for name, tc := range map[string]struct {
		port string
		want string
	}{
		"missing":    {"", "shop is not set up on vps2: run go tool tifa deploy setup, which sets up every server of shop"},
		"other port": {"9533\n", "shop has port 9531 on vps and 9533 on vps2: run go tool tifa deploy setup, which gives it one port on every server"},
	} {
		t.Run(name, func(t *testing.T) {
			root, home1, home2, _ := twoServers(t, "deploy:\n  front: external\n", nil)
			if tc.port == "" {
				os.RemoveAll(filepath.Join(home2, "shop"))
			} else {
				writeFile(t, filepath.Join(home2, "shop", "port"), tc.port)
			}
			t.Setenv(deployHostEnv, "vps,vps2")
			out, _, err := run(t, "deploy", "--dir", root)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q\n%s", err, tc.want, out)
			}
			if _, err := os.Stat(filepath.Join(home2, "shop", "port")); tc.port == "" && err == nil {
				t.Error("deploy made the app on vps2")
			}
			if currentOf(home1) != "" {
				t.Error("vps got a release")
			}
		})
	}
}

// Remove of an app on several servers asks once and takes it off every server, the others first
// and the first last. Only the plan of the first takes back what is in GitHub, and each server
// loses the key of the repository. ~/.tifa forgets the app on every server.
func TestRemoveSeveralServers(t *testing.T) {
	root := setupApp(t, "deploy:\n  front: external\n  auto: {}\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	homeOf(t)
	fakePorts(t)
	gh := newFakeGH()
	gh.install(t)
	srv := newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, "appdir yes\n"))
	two := srv.addHost(t, "vps2", "hostname 203.0.113.8\n", box2Facts)
	if out, err := runSetup(t, root, false, "", "--yes"); err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	setFacts(t, srv, factsOf("shop", 9531, "appdir yes\nghkey acme/shop\n"))
	setFacts(t, two, box2Facts+"port 9531\nappdir yes\nghkey acme/shop\n")
	t.Setenv(deployHostEnv, "")
	out, _, err := runRemove(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	i, j := strings.Index(out, "Plan to remove shop from vps2 (2 of 2)"), strings.Index(out, "Plan to remove shop from vps (1 of 2)")
	if i < 0 || j < i {
		t.Fatalf("order of the plans:\n%s", out)
	}
	if plan2 := out[i:j]; strings.Contains(plan2, "github:") {
		t.Errorf("the plan of vps2 changes GitHub:\n%s", plan2)
	}
	for _, want := range []string{"revoke tifa", "removeapp tifa"} {
		if !strings.Contains(two.calls(), want) || !strings.Contains(srv.calls(), want) {
			t.Errorf("calls lack %q:\n%s\n%s", want, srv.calls(), two.calls())
		}
	}
	if len(gh.secrets) != 0 {
		t.Errorf("left in GitHub: %v", gh.secrets)
	}
	if servers, err := readServers(); err != nil || len(servers) != 0 {
		t.Errorf("~/.tifa keeps %+v, %v", servers, err)
	}
}

// remove --host with one server of an app on several takes the app off that server only: the others
// keep it, ~/.tifa keeps them as the servers of the app, and GitHub is left alone.
func TestRemoveOneOfSeveralServers(t *testing.T) {
	root, home1, home2, _ := twoServers(t, "deploy:\n  front: external\n", nil)
	t.Setenv(deployHostEnv, "vps,vps2")
	if out, _, err := run(t, "deploy", "--dir", root); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Setenv(deployHostEnv, "")
	out, _, err := runRemove(t, root, false, "", "--host", "vps2", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home2, "shop")); err == nil {
		t.Error("vps2 keeps the app")
	}
	if currentOf(home1) == "" {
		t.Error("vps lost the app")
	}
	servers, err := readServers()
	if err != nil || len(servers) != 1 || servers[0].Host != "vps" || len(servers[0].Apps) != 1 || len(servers[0].Apps[0].Hosts) != 0 {
		t.Fatalf("~/.tifa: %+v, %v", servers, err)
	}
	// The next deploy names the server that is left, and goes on.
	if out, _, err := run(t, "deploy", "--dir", root); err != nil || !strings.Contains(out, "deploying shop to vps (") {
		t.Errorf("%v\n%s", err, out)
	}
}
