package e2e

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// devApp copies test/apps/pages into a temporary directory that tifa dev may edit.
func devApp(t *testing.T) string {
	t.Helper()
	return copyApp(t, "pages")
}

// copyApp copies test/apps/<name> into a temporary directory that tifa dev may edit. go.mod points
// at this repository and node_modules is linked from the original, which must be installed.
func copyApp(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join(repoRoot(t), "test", "apps", name)
	if _, err := os.Stat(filepath.Join(src, "node_modules")); err != nil {
		t.Skipf("run pnpm install --frozen-lockfile in test/apps/%s first", name)
	}
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if slices.Contains([]string{"node_modules", ".tifa", "bin"}, e.Name()) {
			continue
		}
		if out, err := exec.Command("cp", "-R", filepath.Join(src, e.Name()), dst).CombinedOutput(); err != nil {
			t.Fatalf("copy %s: %v\n%s", e.Name(), err, out)
		}
	}
	if err := os.Symlink(filepath.Join(src, "node_modules"), filepath.Join(dst, "node_modules")); err != nil {
		t.Fatal(err)
	}
	gomod := filepath.Join(dst, "go.mod")
	data, err := os.ReadFile(gomod)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "=> ../../..", "=> "+repoRoot(t), 1))
	if err := os.WriteFile(gomod, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

type devRun struct {
	dir      string
	base     string
	log      *lockedBuffer
	statuses chan devStatus
}

type devStatus struct {
	Version string `json:"version"`
	Title   string `json:"title"`
	Error   string `json:"error"`
}

var serving = regexp.MustCompile(`tifa dev: serving (http://\S+)`)

// startDev runs tifa dev on dir with a PATH that has no node, and follows its SSE events.
func startDev(t *testing.T, dir string) *devRun {
	t.Helper()
	return startDevWith(t, dir, withoutNode(t))
}

// startDevWith runs tifa dev on dir with the given PATH, and follows its SSE events.
func startDevWith(t *testing.T, dir, path string) *devRun {
	t.Helper()
	d := &devRun{dir: dir, log: &lockedBuffer{}, statuses: make(chan devStatus, 100)}
	cmd := exec.Command(cli(t), "dev", "--dir", dir, "--addr", "127.0.0.1:0")
	// The app rejects unknown TIFA_* variables as settings, TIFA_E2E among them.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TIFA_") && !strings.HasPrefix(kv, "PATH=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "PATH="+path)
	cmd.Stdout = d.log
	cmd.Stderr = d.log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGINT)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("tifa dev exited with %v\n%s", err, d.log)
			}
		case <-time.After(30 * time.Second):
			cmd.Process.Kill()
			t.Errorf("tifa dev did not stop after SIGINT\n%s", d.log)
		}
	})
	d.waitLog(t, 0, serving)
	d.base = serving.FindStringSubmatch(d.log.String())[1]

	resp, err := http.Get(d.base + "/__tifa/dev/events")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			if data, ok := strings.CutPrefix(scanner.Text(), "data: "); ok {
				var st devStatus
				if json.Unmarshal([]byte(data), &st) == nil {
					d.statuses <- st
				}
			}
		}
	}()
	return d
}

// waitLog waits until the log after offset from matches re and returns the log from there.
func (d *devRun) waitLog(t *testing.T, from int, re *regexp.Regexp) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if out := d.log.String()[from:]; re.MatchString(out) {
			return out
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("log never matched %s:\n%s", re, d.log)
	return ""
}

// waitStatus returns the first status that matches.
func (d *devRun) waitStatus(t *testing.T, match func(devStatus) bool) devStatus {
	t.Helper()
	timeout := time.After(2 * time.Minute)
	for {
		select {
		case st := <-d.statuses:
			if match(st) {
				return st
			}
		case <-timeout:
			t.Fatalf("no matching status\n%s", d.log)
		}
	}
}

// edit replaces old with new in a file of the app and returns the log offset before the edit.
func (d *devRun) edit(t *testing.T, name, old, new string) int {
	t.Helper()
	path := filepath.Join(d.dir, filepath.FromSlash(name))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s lacks %q", name, old)
	}
	mark := len(d.log.String())
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	return mark
}

var (
	ready       = regexp.MustCompile(`ready in \S+`)
	buildFailed = regexp.MustCompile(`build failed: `)
	stepDone    = regexp.MustCompile(`(static analysis|code generation|bundle|go build) done in (\S+)`)
)

// steps lists the steps logged in out, and their durations.
func steps(out string) (names []string, took []string) {
	for _, m := range stepDone.FindAllStringSubmatch(out, -1) {
		names = append(names, m[1])
		took = append(took, m[2])
	}
	return names, took
}

func expectSteps(t *testing.T, change, out string, want ...string) {
	t.Helper()
	names, took := steps(out)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("%s ran %v, want %v:\n%s", change, names, want, out)
	}
	t.Logf("%s: %v %v, %s", change, names, took, ready.FindString(out))
}

func expectBody(t *testing.T, url, want string) {
	t.Helper()
	status, _, body := get(t, url)
	if status != 200 || !strings.Contains(body, want) {
		t.Fatalf("GET %s: %d, want 200 with %q:\n%.400s", url, status, want, body)
	}
}

func TestDev(t *testing.T) {
	skipUnlessEnabled(t)
	if runtime.GOOS == "windows" {
		t.Skip("tifa dev needs interrupts, which Windows lacks")
	}
	d := startDev(t, devApp(t))

	// The first start runs every step.
	out := d.waitLog(t, 0, ready)
	expectSteps(t, "first build", out, "static analysis", "code generation", "bundle", "go build")
	v1 := d.waitStatus(t, func(st devStatus) bool { return st.Version != "" && st.Error == "" }).Version
	expectBody(t, d.base+"/", "North shop")

	// TSX only: a new bundle and a restart, no go build.
	mark := d.edit(t, "app/route.tsx", "North shop", "North store")
	expectSteps(t, "TSX edit", d.waitLog(t, mark, ready), "bundle")
	v2 := d.waitStatus(t, func(st devStatus) bool { return st.Version != v1 && st.Error == "" }).Version
	expectBody(t, d.base+"/", "North store")

	// A Go function body: analysis finds the same routes, go build without esbuild.
	mark = d.edit(t, "app/shops/shop+/layout.go", `"Shop " + id`, `"Store " + id`)
	out = d.waitLog(t, mark, ready)
	expectSteps(t, "Go body edit", out, "static analysis", "go build")
	if !strings.Contains(out, "route manifest unchanged") {
		t.Errorf("Go body edit changed the manifest:\n%s", out)
	}
	v3 := d.waitStatus(t, func(st devStatus) bool { return st.Version != v2 && st.Error == "" }).Version
	expectBody(t, d.base+"/shops/north", "Store north")

	// A changed SSR constant changes the route manifest: everything runs.
	mark = d.edit(t, "app/account/login/route.go", "const SSR = false", "const SSR = true")
	out = d.waitLog(t, mark, ready)
	expectSteps(t, "SSR edit", out, "static analysis", "code generation", "bundle", "go build")
	if !strings.Contains(out, "route manifest changed") {
		t.Errorf("SSR edit kept the manifest:\n%s", out)
	}
	v4 := d.waitStatus(t, func(st devStatus) bool { return st.Version != v3 && st.Error == "" }).Version
	if _, header, _ := get(t, d.base+"/account/login"); header.Get("X-Tifa-Render") != "vm" {
		t.Errorf("SSR edit: rendered by %q", header.Get("X-Tifa-Render"))
	}

	// A broken build keeps the running app and reports the error over SSE.
	mark = d.edit(t, "app/shops/shop+/layout.go", `return Shop{ID: id, Name: "Store " + id}, nil`, `return Shop{ID: id, Name: "Store " + id}, nil +`)
	d.waitLog(t, mark, buildFailed)
	failed := d.waitStatus(t, func(st devStatus) bool { return st.Error != "" })
	if failed.Version != v4 || failed.Title != "Build failed" || !strings.Contains(failed.Error, "layout.go") {
		t.Fatalf("status %+v", failed)
	}
	expectBody(t, d.base+"/shops/north", "Store north")

	// The fix clears the error and brings a new version.
	mark = d.edit(t, "app/shops/shop+/layout.go", `return Shop{ID: id, Name: "Store " + id}, nil +`, `return Shop{ID: id, Name: "Shop " + id + "!"}, nil`)
	d.waitLog(t, mark, ready)
	d.waitStatus(t, func(st devStatus) bool { return st.Version != v4 && st.Error == "" })
	expectBody(t, d.base+"/shops/north", "Shop north!")
}
