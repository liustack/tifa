package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a buffer that a command writes to while the test reads it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// logs prints the journal of the user units of the app, as the user tifa, the last 100 lines
// unless -n says otherwise.
func TestDeployLogs(t *testing.T) {
	root, _, calls := envApp(t, "200")
	out, _, err := run(t, "deploy", "logs", "--dir", root)
	if err != nil || !strings.Contains(out, "the app said goodbye") {
		t.Fatalf("%v\n%s", err, out)
	}
	want := "journalctl --user -u tifa-shop.service -u tifa-shop.socket -n 100 --no-pager\n"
	if !strings.Contains(calls(), "ssh tifa vps\n"+want) {
		t.Errorf("calls:\n%s", calls())
	}
	if _, _, err := run(t, "deploy", "logs", "-n", "5", "--dir", root); err != nil || !strings.Contains(calls(), " -n 5 --no-pager\n") {
		t.Errorf("-n 5: %v\n%s", err, calls())
	}
	if _, _, err := run(t, "deploy", "logs", "-n", "-1", "--dir", root); err == nil {
		t.Error("-n -1: no error")
	}
}

// statusApp is an app on the fake server with three releases, the newest current, its records,
// and a service that systemd says is active since show.
func statusApp(t *testing.T, show string) (root, home string) {
	t.Helper()
	root = t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n\ngo 1.27.0\n")
	t.Setenv(deployHostEnv, "vps")
	home = t.TempDir()
	prepareFakeServer(t, home)
	writeFile(t, filepath.Join(home, "shop", "port"), "9531\n")
	ids := []string{"20261120T100000Z", "20261120T110000Z", "20261120T120000Z"}
	for _, id := range ids {
		writeFile(t, filepath.Join(home, "shop", "releases", id, "shop"), "x")
	}
	if err := os.Symlink("releases/"+ids[2], filepath.Join(home, "shop", "current")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, "shop", "deploys.log"), strings.Join([]string{
		"2026-11-20T10:00:05Z\t" + ids[0] + "\taaa111\tdeploy\tok",
		"2026-11-20T11:00:05Z\t" + ids[1] + "\tbbb222-dirty\tdeploy\tfailed",
		"2026-11-20T12:00:05Z\t" + ids[2] + "\tccc333\tdeploy\tok",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(home, ".run", "show"), show)
	fakeServer(t, home, map[string]string{ids[2]: "200"})
	oldNow := deployNow
	t.Cleanup(func() { deployNow = oldNow })
	deployNow = func() time.Time { return time.Date(2026, 11, 20, 14, 3, 30, 0, time.UTC) }
	return root, home
}

// status says which release runs and from which commit, which releases are kept and whether
// each passed its check, since when the service runs, what the health check answers now, and the
// memory of the program, as the user tifa and in a few lines.
func TestDeployStatus(t *testing.T) {
	show := fmt.Sprintf("ActiveState=active\nActiveEnterTimestamp=Fri 2026-11-20 12:00:05 UTC\nMainPID=%d\n", os.Getpid())
	root, _ := statusApp(t, show)
	out, _, err := run(t, "deploy", "status", "--dir", root)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	lines := strings.Split(out, "\n")
	want := []string{
		"shop on vps, port 9531",
		"  release  20261120T120000Z  commit ccc333        passed its health check",
		"  kept     20261120T110000Z  commit bbb222-dirty  failed its health check",
		"           20261120T100000Z  commit aaa111        passed its health check",
		"  service  active since 2026-11-20 12:00:05 UTC, 2h3m ago",
		"  health   GET /healthz answered 200",
	}
	if len(lines) != len(want)+2 || strings.Join(lines[:len(want)], "\n") != strings.Join(want, "\n") ||
		!strings.HasPrefix(lines[len(want)], "  memory   ") || !strings.HasSuffix(lines[len(want)], " MB") {
		t.Errorf("got:\n%s\nwant:\n%s\n  memory   ... MB", out, strings.Join(want, "\n"))
	}
}

// A service that does not run is not asked for its health: the socket would start it.
func TestDeployStatusStopped(t *testing.T) {
	root, _ := statusApp(t, "ActiveState=failed\nActiveEnterTimestamp=\nMainPID=0\n")
	out, _, err := run(t, "deploy", "status", "--dir", root)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"  service  failed\n", "  health   not asked, the service does not run\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "memory") {
		t.Errorf("memory of a program that does not run:\n%s", out)
	}
}

// An app that is not on the server has no status.
func TestDeployStatusNeedsTheApp(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n\ngo 1.27.0\n")
	t.Setenv(deployHostEnv, "vps")
	fakeServer(t, t.TempDir(), nil)
	if _, _, err := run(t, "deploy", "status", "--dir", root); err == nil || !strings.Contains(err.Error(), "tifa deploy first") {
		t.Errorf("got %v", err)
	}
}

// With -f the journal follows until Ctrl-C, which ends the command cleanly.
func TestDeployLogsFollowEndsOnCtrlC(t *testing.T) {
	root, _, calls := envApp(t, "200")
	var out syncBuffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"deploy", "logs", "-f", "--dir", root})
	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	for deadline := time.Now().Add(10 * time.Second); !strings.Contains(out.String(), "following"); {
		if time.Now().After(deadline) {
			t.Fatalf("journalctl -f did not start:\n%s\n%s", out.String(), calls())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(calls(), " --no-pager -f\n") {
		t.Errorf("calls:\n%s", calls())
	}
	self, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := self.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Ctrl-C: %v\n%s", err, out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("logs -f did not end on Ctrl-C")
	}
}

// The health line judges the answer the way a deploy does: below 500 passes, and a 404 is an app
// without a /healthz route.
func TestHealthLine(t *testing.T) {
	for code, want := range map[string]string{
		"":    "not asked, the service does not run",
		"000": "GET /healthz got no answer within 5 seconds",
		"200": "GET /healthz answered 200",
		"404": "GET /healthz answered 404, which passes: the app has no /healthz route",
		"503": "GET /healthz answered 503, which fails",
	} {
		if got := healthLine(code); got != want {
			t.Errorf("%q: got %q, want %q", code, got, want)
		}
	}
}
