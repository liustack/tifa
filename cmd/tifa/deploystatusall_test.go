package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// statusAllWorld is a fake server with two apps, shop that runs and blog whose service failed
// (blog is not in the file of the servers, as if it was set up from another machine), and a
// ~/.tifa in a temporary folder. It returns the machine ID of the fake server. The host down
// makes ssh fail to log in, and any other host is the fake server.
func statusAllWorld(t *testing.T) (tifaDir, machine string) {
	t.Helper()
	tifaDir = homeOf(t)
	home := t.TempDir()
	prepareFakeServer(t, home)
	for app, port := range map[string]string{"shop": "9531", "blog": "9532"} {
		id := "20261120T120000Z"
		writeFile(t, filepath.Join(home, app, "port"), port+"\n")
		writeFile(t, filepath.Join(home, app, "releases", id, app), "x")
		if err := os.Symlink("releases/"+id, filepath.Join(home, app, "current")); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(home, app, "deploys.log"), "2026-11-20T12:00:05Z\t"+id+"\tccc333\tdeploy\tok\n")
	}
	writeFile(t, filepath.Join(home, ".run", "show-tifa-shop"),
		fmt.Sprintf("ActiveState=active\nActiveEnterTimestamp=Fri 2026-11-20 12:00:05 UTC\nMainPID=%d\nNRestarts=2\n", os.Getpid()))
	writeFile(t, filepath.Join(home, ".run", "show-tifa-blog"), "ActiveState=failed\nActiveEnterTimestamp=\nMainPID=0\nNRestarts=5\n")
	fakeServer(t, home, map[string]string{"20261120T120000Z": "200"})
	// The fake ssh refuses the host down, as a server that is off does.
	inner := sshProgram
	wrapper := filepath.Join(t.TempDir(), "ssh")
	writeFile(t, wrapper, "#!/bin/sh\nfor a in \"$@\"; do if [ \"$prev\" = -- ]; then host=$a; fi; prev=$a; done\nif [ \"$host\" = down ]; then echo 'ssh: connect to host down port 22: Connection refused' >&2; exit 255; fi\nexec "+shellQuote(inner)+" \"$@\"\n")
	if err := os.Chmod(wrapper, 0o755); err != nil {
		t.Fatal(err)
	}
	sshProgram = wrapper
	t.Cleanup(func() { sshProgram = inner })
	// The files of the facts.
	files := map[string]string{
		"os-release": "NAME=\"Ubuntu\"\nPRETTY_NAME=\"Ubuntu 24.04.1 LTS\"\n",
		"meminfo":    "MemTotal:        2048000 kB\nMemFree: 1 kB\nMemAvailable:    1024000 kB\n",
		"uptime":     "1058400.55 2000.00\n",
		"loadavg":    "0.10 0.20 0.30 1/200 999\n",
	}
	oldOS, oldMem, oldUp, oldLoad := factsOSRelease, factsMeminfo, factsUptime, factsLoadavg
	t.Cleanup(func() { factsOSRelease, factsMeminfo, factsUptime, factsLoadavg = oldOS, oldMem, oldUp, oldLoad })
	path := func(name string) string {
		writeFile(t, filepath.Join(home, ".run", name), files[name])
		return filepath.Join(home, ".run", name)
	}
	factsOSRelease, factsMeminfo, factsUptime, factsLoadavg = path("os-release"), path("meminfo"), path("uptime"), path("loadavg")
	oldNow := deployNow
	t.Cleanup(func() { deployNow = oldNow })
	deployNow = func() time.Time { return time.Date(2026, 11, 20, 14, 3, 30, 0, time.UTC) }
	out, err := exec.Command("sh", "-c", `printf '%s-%s' "$(hostname -s)" "$(head -c 8 /etc/machine-id 2>/dev/null)"`).Output()
	if err != nil {
		t.Fatal(err)
	}
	return tifaDir, string(out)
}

func writeServers(t *testing.T, tifaDir string, entries ...string) {
	t.Helper()
	writeFile(t, filepath.Join(tifaDir, "config.yaml"), "servers:\n"+strings.Join(entries, ""))
}

func serverEntry(id, host, app string) string {
	return fmt.Sprintf("  - id: %s\n    host: %s\n    apps:\n      - {name: %s, path: /work/%s, host: %s}\n", id, host, app, app, host)
}

// --all prints every server of the file once, in the order of the file, with the facts of the
// server and its apps by name, the ones set up from another machine too. A server that does not
// answer shows its error and the others still print. The exit is an error since blog failed and
// a server is down.
func TestDeployStatusAll(t *testing.T) {
	tifaDir, machine := statusAllWorld(t)
	writeServers(t, tifaDir, serverEntry(machine, "vps", "shop"), serverEntry("gone-1a2b3c4d", "down", "api"))
	out, _, err := run(t, "deploy", "status", "--all")
	if err == nil || err.Error() != "1 server could not be checked and 1 app is not well" {
		t.Fatalf("error %v\n%s", err, out)
	}
	name := knownServer{ID: machine}.name()
	for _, want := range []string{
		name + " (vps)\n",
		"  system    Ubuntu 24.04.1 LTS, x86_64",
		"  memory    2.1 GB total, 1.0 GB available\n",
		"  uptime    up 12d 6h, load 0.10 0.20 0.30\n",
		"\n  blog\n    release   20261120T120000Z  commit ccc333  passed its health check\n    service   failed, 5 restarts\n    health    not asked, the service does not run\n\n  shop\n    release   20261120T120000Z  commit ccc333  passed its health check\n    service   active since 2026-11-20 12:00:05 UTC, 2h3m ago, 2 restarts\n    health    GET /healthz answered 200\n    memory    ",
		"gone (down)\n  problem   ssh tifa@down failed (exit status 255): ssh: connect to host down port 22: Connection refused\n",
		"Only the servers this machine has set up or deployed to, the ones in " + filepath.Join(tifaDir, "config.yaml") + ", are shown.\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, name+", reached") > strings.Index(out, "gone, reached") {
		t.Errorf("the servers are not in the order of the file:\n%s", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("colors in the output:\n%s", out)
	}
}

// When every server answers and every deployed app is well, the exit is 0. An app that is not
// deployed yet says so and is not a problem.
func TestDeployStatusAllWell(t *testing.T) {
	tifaDir, machine := statusAllWorld(t)
	runDir := filepath.Dir(factsMeminfo)
	writeFile(t, filepath.Join(runDir, "show-tifa-blog"), fmt.Sprintf("ActiveState=active\nActiveEnterTimestamp=Fri 2026-11-20 12:00:05 UTC\nMainPID=%d\nNRestarts=0\n", os.Getpid()))
	// blog has no release yet, and new is in the file but not on the server.
	if err := os.Remove(filepath.Join(runDir, "..", "blog", "current")); err != nil {
		t.Fatal(err)
	}
	writeServers(t, tifaDir, serverEntry(machine, "vps", "shop")+"      - {name: new, path: /work/new, host: vps}\n")
	out, _, err := run(t, "deploy", "status", "--all")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "  new\n    release   not deployed yet: run go tool tifa deploy\n") ||
		!strings.Contains(out, "  blog\n    release   not deployed yet") {
		t.Errorf("output:\n%s", out)
	}
}

// A host that now answers as another machine is reported as that, and its apps are not.
func TestDeployStatusAllOtherMachine(t *testing.T) {
	tifaDir, machine := statusAllWorld(t)
	writeServers(t, tifaDir, serverEntry("old-0badc0de", "vps2", "shop"), serverEntry(machine, "vps", "shop"))
	out, _, err := run(t, "deploy", "status", "--all")
	if err == nil || err.Error() != "1 server could not be checked and 1 app is not well" {
		t.Fatalf("error %v\n%s", err, out)
	}
	if !strings.Contains(out, "old (vps2)\n  problem   the host now points at another machine: vps2 says it is \""+machine+"\", and the entry is \"old-0badc0de\"\n\n") {
		t.Errorf("output:\n%s", out)
	}
	if block, _, _ := strings.Cut(out, "\n\n"); strings.Contains(block, "shop") || strings.Count(out, "  shop\n") != 1 {
		t.Errorf("apps of the other machine:\n%s", out)
	}
}

// --json is the same facts as one document, in lower camelCase.
func TestDeployStatusAllJSON(t *testing.T) {
	tifaDir, machine := statusAllWorld(t)
	writeServers(t, tifaDir, serverEntry(machine, "vps", "shop"), serverEntry("gone-1a2b3c4d", "down", "api"))
	out, _, err := run(t, "deploy", "status", "--all", "--json")
	if err == nil {
		t.Error("no error although a server is down")
	}
	var got []map[string]any
	if jerr := json.Unmarshal([]byte(out), &got); jerr != nil {
		t.Fatalf("%v\n%s", jerr, out)
	}
	if len(got) != 2 || got[0]["reached"] != true || got[1]["reached"] != false || got[1]["host"] != "down" ||
		!strings.Contains(got[1]["error"].(string), "Connection refused") {
		t.Fatalf("servers: %s", out)
	}
	facts := got[0]["facts"].(map[string]any)
	if facts["os"] != "Ubuntu 24.04.1 LTS" || facts["memoryTotalKiB"] != 2048000.0 || facts["memoryAvailableKiB"] != 1024000.0 ||
		facts["uptimeSeconds"] != 1058400.0 || facts["load5"] != 0.2 || facts["arch"] != "x86_64" {
		t.Errorf("facts: %v", facts)
	}
	apps := got[0]["apps"].([]any)
	blog, shop := apps[0].(map[string]any), apps[1].(map[string]any)
	if blog["name"] != "blog" || blog["service"] != "failed" || blog["restarts"] != 5.0 || blog["ok"] != false ||
		shop["name"] != "shop" || shop["service"] != "active" || shop["health"] != "200" || shop["ok"] != true ||
		shop["release"] != "20261120T120000Z" || shop["commit"] != "ccc333" || shop["checkPassed"] != true || shop["deployed"] != true {
		t.Errorf("apps: %v", apps)
	}
	if apps := got[1]["apps"].([]any); len(apps) != 0 {
		t.Errorf("apps of the server that is down: %v", apps)
	}
}

// With no server in the file the command says how to set one up, and exits 0.
func TestDeployStatusAllNoServers(t *testing.T) {
	homeOf(t)
	out, _, err := run(t, "deploy", "status", "--all")
	if err != nil || !strings.Contains(out, "No server was set up from this machine") || !strings.Contains(out, "config.yaml") || !strings.Contains(out, "go tool tifa deploy setup") {
		t.Errorf("%v\n%s", err, out)
	}
}

// --all looks at the servers of the file, so --host and --dir mean nothing with it.
func TestDeployStatusAllRefusesHostAndDir(t *testing.T) {
	homeOf(t)
	for _, args := range [][]string{{"--host", "vps"}, {"--dir", "."}} {
		_, _, err := run(t, append([]string{"deploy", "status", "--all"}, args...)...)
		if err == nil || !strings.Contains(err.Error(), "takes no --host or --dir") {
			t.Errorf("%v: %v", args, err)
		}
	}
	if _, _, err := run(t, "deploy", "status", "--json"); err == nil || !strings.Contains(err.Error(), "--all") {
		t.Errorf("--json alone: %v", err)
	}
}

func TestParseServerFacts(t *testing.T) {
	out := "os=Ubuntu 24.04.1 LTS\narch=aarch64\ncpus=4\nmem_total_kb=2048000\nmem_available_kb=1024000\n" +
		"disk_total_kb=25000000\ndisk_used_kb=12000000\nuptime_s=1058400\nload=0.10 0.20 0.30\nmachine=vps-1a2b3c4d\napp=blog\napp=shop\n"
	got, err := parseServerFacts(out)
	want := serverFacts{OS: "Ubuntu 24.04.1 LTS", Arch: "aarch64", CPUs: 4, MemoryTotalKiB: 2048000, MemoryAvailKiB: 1024000,
		DiskTotalKiB: 25000000, DiskUsedKiB: 12000000, UptimeSeconds: 1058400, Load1: 0.1, Load5: 0.2, Load15: 0.3,
		Machine: "vps-1a2b3c4d", Apps: []string{"blog", "shop"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, %v\nwant %+v", got, err, want)
	}
	// A server that lacks a file leaves the line out.
	if got, err := parseServerFacts("arch=x86_64\nmachine=vps-1\n"); err != nil || got.CPUs != 0 || got.OS != "" || got.Arch != "x86_64" {
		t.Errorf("partial: %+v, %v", got, err)
	}
	for _, bad := range []string{"cpus=many\n", "load=1 2\n", "mem_total_kb=-1\n", "surprise=1\n", "no equals sign\n"} {
		if _, err := parseServerFacts(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// The facts script runs as the deploy user and prints what the parser reads.
func TestFactsScriptOutput(t *testing.T) {
	tifaDir, machine := statusAllWorld(t)
	_ = tifaDir
	t.Setenv(deployHostEnv, "vps")
	d := deployTarget{host: "vps"}
	var out strings.Builder
	if err := d.ssh(t.Context(), deployUser, factsScript(), nil, &out, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	f, err := parseServerFacts(out.String())
	if err != nil || f.Machine != machine || !reflect.DeepEqual(f.Apps, []string{"blog", "shop"}) || f.OS != "Ubuntu 24.04.1 LTS" || f.DiskTotalKiB == 0 {
		t.Errorf("%+v, %v\n%s", f, err, out.String())
	}
}
