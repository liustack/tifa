//go:build unix

// The tests look for the end of the server with kill(pid, 0), a Unix call. scripts/smoke-dev.sh
// runs --background, status and stop on Windows.

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// bgApp copies test/apps/api, which has no pages and so needs no Node, and adds a route that
// answers with an environment variable of the process the app runs in.
func bgApp(t *testing.T) string {
	t.Helper()
	dir := copyApp(t, "api")
	bgWrite(t, dir, "app/probe/route.go", "package probe\n\nimport (\n\t\"os\"\n\n\t\"github.com/liustack/tifa\"\n)\n\nfunc GET(c *tifa.Ctx) error { return c.JSON(200, map[string]string{\"probe\": os.Getenv(\"DEV_BG_PROBE\")}) }\n")
	return dir
}

// tifaIn runs the tifa command in cwd with extra environment variables and returns its output and
// exit status.
func tifaIn(t *testing.T, cwd string, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(cli(t), args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		code = exit.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	return out.String(), errOut.String(), code
}

// bgStopAtEnd stops the tifa dev of dir when the test ends, however it ends. The server is
// detached, so nothing else ends it.
func bgStopAtEnd(t *testing.T, dir string) {
	t.Helper()
	t.Cleanup(func() {
		if out, errOut, code := tifaIn(t, dir, nil, "dev", "stop"); code != 0 {
			t.Errorf("dev stop at the end: %d\n%s%s", code, out, errOut)
		}
	})
}

func bgWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

type bgStatus struct {
	Running bool   `json:"running"`
	PID     int    `json:"pid"`
	URL     string `json:"url"`
	Started string `json:"started"`
	Build   struct {
		State string `json:"state"`
		Error string `json:"error"`
	} `json:"build"`
	Log string `json:"log"`
}

func bgStatusOf(t *testing.T, dir string) (bgStatus, int) {
	t.Helper()
	out, errOut, code := tifaIn(t, dir, nil, "dev", "status", "--json")
	var st bgStatus
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("dev status --json: %v\n%s%s", err, out, errOut)
	}
	return st, code
}

// tifa dev --background returns once the app answers, takes its flags and environment along, keeps
// all output in .tifa/dev.log, and dev status, logs and stop work on it.
func TestDevBackground(t *testing.T) {
	skipUnlessEnabled(t)
	app := bgApp(t)
	// Relative --dir, and an address of our own: both must reach the detached server, which does
	// not start in the same place of the shell.
	cwd, name := filepath.Dir(app), filepath.Base(app)
	addr := freeLoopbackAddr(t)
	out, errOut, code := tifaIn(t, cwd, []string{"DEV_BG_PROBE=from-the-shell"}, "dev", "--background", "--dir", name, "--addr", addr)
	if code != 0 {
		t.Fatalf("dev --background: %d\n%s%s", code, out, errOut)
	}
	bgStopAtEnd(t, app)
	url := "http://" + addr
	if !strings.Contains(out, "ready at "+url+" (pid ") || !strings.Contains(out, "dev logs") || !strings.Contains(out, "dev status") || !strings.Contains(out, "dev stop") {
		t.Fatalf("output:\n%s", out)
	}
	// Ready means the page answers: no waiting here.
	expectBody(t, url+"/health", `"status":"ok"`)
	// The environment of the shell reached the app, and --addr the server.
	expectBody(t, url+"/probe", `"probe":"from-the-shell"`)

	st, code := bgStatusOf(t, app)
	if code != 0 || !st.Running || st.URL != url || st.Build.State != "ok" || st.PID == 0 {
		t.Fatalf("status: exit %d, %+v", code, st)
	}
	if _, err := time.Parse(time.RFC3339, st.Started); err != nil {
		t.Fatalf("started %q: %v", st.Started, err)
	}
	text, _, code := tifaIn(t, app, nil, "dev", "status")
	if code != 0 || !strings.Contains(text, "tifa dev is running") || !strings.Contains(text, url) || !strings.Contains(text, "build:   ok") {
		t.Fatalf("status text: %d\n%s", code, text)
	}
	// The log holds what tifa dev prints, and what the app prints.
	log, err := os.ReadFile(filepath.Join(app, ".tifa", "dev.log"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Log != filepath.Join(app, ".tifa", "dev.log") && !strings.HasSuffix(st.Log, "/.tifa/dev.log") {
		t.Errorf("status names the log %q", st.Log)
	}
	for _, want := range []string{"tifa dev: serving " + url, "go build done", "apitest: started", "ready in"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("the log lacks %q:\n%s", want, log)
		}
	}
	logs, _, code := tifaIn(t, app, nil, "dev", "logs", "-n", "2")
	if code != 0 || strings.Count(logs, "\n") != 2 || !strings.Contains(string(log), strings.TrimSuffix(logs, "\n")) {
		t.Fatalf("logs -n 2: %d\n%s", code, logs)
	}

	// A second one, in either mode, fails and says what to do.
	for _, args := range [][]string{{"dev"}, {"dev", "--background"}} {
		_, errOut, code := tifaIn(t, app, nil, args...)
		if code != 1 || !strings.Contains(errOut, "already running") || !strings.Contains(errOut, "dev status") || !strings.Contains(errOut, "dev stop") {
			t.Errorf("%v: %d\n%s", args, code, errOut)
		}
	}

	// Stop returns once the server and the lock are gone.
	out, errOut, code = tifaIn(t, app, nil, "dev", "stop")
	if code != 0 || !strings.Contains(out, "tifa dev stopped (pid "+strconv.Itoa(st.PID)+")") {
		t.Fatalf("dev stop: %d\n%s%s", code, out, errOut)
	}
	// The lock goes as the last thing tifa dev does, so the process may still be ending, and once it
	// has ended it stays until the process that took it in as an orphan reaps it.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		err := syscall.Kill(st.PID, 0)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		if time.Now().After(deadline) {
			t.Errorf("the server (pid %d) runs 5s after stop: %v", st.PID, err)
			break
		}
	}
	if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		conn.Close()
		t.Errorf("%s still answers after stop", addr)
	}
	after, code := bgStatusOf(t, app)
	if code != 1 || after.Running {
		t.Errorf("status after stop: exit %d, %+v", code, after)
	}
	// The lock is free: another one starts at once, and the log is its own.
	out, errOut, code = tifaIn(t, app, nil, "dev", "--background", "--addr", addr)
	if code != 0 {
		t.Fatalf("dev --background again: %d\n%s%s", code, out, errOut)
	}
	again, _ := bgStatusOf(t, app)
	if again.PID == st.PID {
		t.Errorf("the second run has the pid of the first")
	}
	// Without DEV_BG_PROBE in the shell, the app sees none: the environment is the one of the shell.
	expectBody(t, url+"/probe", `"probe":""`)
}

// A first build that fails ends the command with an error that says why, and leaves the server,
// which builds again once the file is fixed.
func TestDevBackgroundFirstBuildFails(t *testing.T) {
	skipUnlessEnabled(t)
	app := bgApp(t)
	bgWrite(t, app, "app/broken/route.go", "package broken\n\nfunc GET() { undefinedName() }\n")
	out, errOut, code := tifaIn(t, app, nil, "dev", "--background", "--addr", freeLoopbackAddr(t))
	bgStopAtEnd(t, app)
	if code != 1 || !strings.Contains(errOut, "the first build failed") || !strings.Contains(errOut, "undefined") {
		t.Fatalf("dev --background: %d\n%s%s", code, out, errOut)
	}
	st, code := bgStatusOf(t, app)
	if code != 0 || !st.Running || st.Build.State != "failed" || st.Build.Error == "" || strings.Contains(st.Build.Error, "\n") {
		t.Fatalf("status: exit %d, %+v", code, st)
	}
	if err := os.Remove(filepath.Join(app, "app", "broken", "route.go")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		if st, _ := bgStatusOf(t, app); st.Build.State == "ok" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the build does not recover: %+v", st)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// A server that cannot start ends the command with the end of its log, and holds nothing.
func TestDevBackgroundStartFails(t *testing.T) {
	skipUnlessEnabled(t)
	app := bgApp(t)
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	out, errOut, code := tifaIn(t, app, nil, "dev", "--background", "--addr", taken.Addr().String())
	if code != 1 || !strings.Contains(errOut, "stopped before it was ready") || !strings.Contains(errOut, "already in use") {
		t.Fatalf("dev --background: %d\n%s%s", code, out, errOut)
	}
	if st, code := bgStatusOf(t, app); code != 1 || st.Running {
		t.Fatalf("status: exit %d, %+v", code, st)
	}
	// Stop with nothing to stop is not an error.
	if out, _, code := tifaIn(t, app, nil, "dev", "stop"); code != 0 || !strings.Contains(out, "not running") {
		t.Fatalf("dev stop: %d, %s", code, out)
	}
}
