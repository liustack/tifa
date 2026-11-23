package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// browserSessions holds the names of the sessions this test process opened, so that the daemons
// it left can be told from those of other runs.
var browserSessions sync.Map

// timedOutSessions holds the sessions that had a command killed for running out of time. Such a
// command can leave the daemon without a socket, so close says "is not open" and the daemon
// stays. That is expected there and is ended without failing the test, which already failed.
var timedOutSessions sync.Map

// sessionDaemons gives the PIDs of the playwright-cli daemons that run the session. The daemon is
// a detached node process, so it outlives a test that does not close it.
func sessionDaemons(session string) []int {
	return daemonsWhere(func(name string) bool { return name == session })
}

// leftDaemons gives the PIDs of the daemons of any session this test process opened. TestMain
// fails the run when there are any, which catches a path that skipped the close of openBrowser.
func leftDaemons() []int {
	return daemonsWhere(func(name string) bool { _, ok := browserSessions.Load(name); return ok })
}

func daemonsWhere(match func(session string) bool) []int {
	out, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !match(fields[len(fields)-1]) || !strings.HasSuffix(fields[len(fields)-2], "cliDaemon.js") {
			continue
		}
		if pid, err := strconv.Atoi(fields[0]); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

// maxSessionName is the longest session name the daemon can listen under. It listens on a unix
// socket at <tmp>/pw-xxxxxxxx/cli/<16 characters>-<name>.sock, and a path over 104 characters
// makes it fail with EINVAL. The daemon then stays running without a socket, `close` answers
// "is not open" for it, and only a kill ends it.
func maxSessionName() int {
	return 104 - len(strings.TrimRight(os.TempDir(), "/")) - len("/pw-xxxxxxxx/cli/") - 16 - 1 - len(".sock")
}

// openBrowser returns a function that runs playwright-cli commands in the session called name.
// Every command runs in one folder of the test, which is also returned. The first command is
// normally "open". The close is registered here, before anything can open the browser, so it also
// runs when open fails half way or the test stops with t.Fatal. After the close it checks that no
// daemon of the session is left, and ends one that is.
func openBrowser(t *testing.T, name string) (pw func(args ...string) string, dir string) {
	t.Helper()
	session := name
	if max := maxSessionName(); len(session) > max {
		t.Fatalf("session name %q has %d characters, the socket path of playwright-cli allows %d under %s", session, len(session), max, os.TempDir())
	}
	browserSessions.Store(session, true)
	dir = t.TempDir() // registered first, so removed after the close below
	t.Cleanup(func() { closeBrowser(t, session, dir) })
	return func(args ...string) string {
		t.Helper()
		out, err := runPlaywright(session, dir, pwTimeout, args...)
		if err != nil {
			t.Fatalf("playwright-cli %v: %v\n%s", args, err, out)
		}
		return out
	}, dir
}

func closeBrowser(t *testing.T, session, dir string) {
	t.Helper()
	text, err := runPlaywright(session, dir, closeTimeout, "close")
	out := []byte(text)
	switch {
	case err != nil:
		t.Errorf("playwright-cli close: %v\n%s", err, out)
	case strings.Contains(string(out), "is not open"):
		// The command exits 0 here. Either open never started a browser, or it already quit.
		t.Logf("playwright-cli close: %s", strings.TrimSpace(string(out)))
	case !strings.Contains(string(out), "closed"):
		t.Errorf("playwright-cli close did not say the browser closed:\n%s", out)
	}
	// The daemon quits a moment after close answers.
	for deadline := time.Now().Add(5 * time.Second); len(sessionDaemons(session)) > 0 && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
	}
	for _, pid := range sessionDaemons(session) {
		if _, ok := timedOutSessions.Load(session); ok {
			t.Logf("daemon %d of session %s is left by a command that timed out, ending it", pid, session)
		} else {
			t.Errorf("playwright-cli daemon %d of session %s is still running after close, ending it", pid, session)
		}
		endProcessTree(pid)
	}
}

// TestBrowserSessionLeavesNoDaemon runs a test that opens a browser and then stops with a failed
// step, and checks that no daemon of its session is left when it is over.
func TestBrowserSessionLeavesNoDaemon(t *testing.T) {
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	var session string
	t.Run("opens and stops", func(t *testing.T) {
		pw, _ := openBrowser(t, "e2e-leak")
		session = "e2e-leak"
		pw("open", "about:blank")
		if len(sessionDaemons(session)) == 0 {
			t.Fatal("no daemon after open")
		}
		t.SkipNow() // stops the test the way t.Fatal does, the cleanups still run
	})
	if pids := sessionDaemons(session); len(pids) > 0 {
		t.Errorf("daemons left after the test: %v", pids)
		for _, pid := range pids {
			endProcessTree(pid)
		}
	}
}

// TestBrowserCommandTimeoutLeavesNoProcess lets a command run out of time while the browser is
// still starting, as a launch that hangs would. The test must get an error back and not wait, and
// the cleanup must end the daemon and the Chrome it started, whatever state they are in.
func TestBrowserCommandTimeoutLeavesNoProcess(t *testing.T) {
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	t.Run("times out", func(t *testing.T) {
		_, dir := openBrowser(t, "e2e-timeout")
		start := time.Now()
		if _, err := runPlaywright("e2e-timeout", dir, 50*time.Millisecond, "open", "about:blank"); err == nil {
			t.Fatal("open finished within 50ms, the deadline was not applied")
		}
		if d := time.Since(start); d > 20*time.Second {
			t.Fatalf("the command took %v to give up", d)
		}
	})
	if pids := sessionDaemons("e2e-timeout"); len(pids) > 0 {
		t.Errorf("daemons left after the cleanup: %v", pids)
		for _, pid := range pids {
			endProcessTree(pid)
		}
	}
	if out, _ := exec.Command("pgrep", "-f", "e2e-timeout").Output(); len(out) > 0 {
		t.Errorf("processes of the session are left: %s", out)
	}
}

// TestRunPlaywrightEndsPipeHolders checks the deadline against a command that leaves a child
// holding its output, which makes a plain CombinedOutput wait for as long as the child lives.
func TestRunPlaywrightEndsPipeHolders(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake command is a shell script")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\nsleep 300 &\necho $! > \"$PWD/child.pid\"\nsleep 300\n"
	if err := os.WriteFile(bin+"/playwright-cli", []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	dir := t.TempDir()
	start := time.Now()
	// The deadline leaves the script time to start its child even on a loaded machine: with 500 ms
	// the full suite once killed it before it wrote child.pid.
	if _, err := runPlaywright("e2e-fake", dir, 5*time.Second, "open"); err == nil {
		t.Fatal("no error from a command that does not end")
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("the command took %v to give up", d)
	}
	raw, err := os.ReadFile(dir + "/child.pid")
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if !processAlive(pid) {
			return
		}
	}
	endProcessTree(pid)
	t.Fatalf("the child %d of the command is still running", pid)
}

// pwTimeout is how long one playwright-cli command may run, closeTimeout the same for the close
// of the cleanup. Both are far below the timeout of go test, so that a hung browser fails one
// test with its output and the cleanup still runs. A test that panics from that timeout never
// runs its cleanups, and leaves the daemon and its Chrome behind.
var (
	pwTimeout    = 3 * time.Minute
	closeTimeout = 30 * time.Second
)

// runPlaywright runs one playwright-cli command of the session in dir and gives its output. The
// command gets its own process group and is killed with the group when the time is up, and the
// pipe is cut a few seconds after it ends, since a child that kept the output open would
// otherwise keep CombinedOutput waiting with no end.
func runPlaywright(session, dir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "playwright-cli", append([]string{"-s=" + session}, args...)...)
	cmd.Dir = dir
	killWithGroup(cmd)
	cmd.WaitDelay = 3 * time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		timedOutSessions.Store(session, true)
		// The killed command may already have started the daemon. The cleanup of openBrowser
		// closes it, and ends what close cannot.
		return string(out), fmt.Errorf("no answer after %v: %w", timeout, ctx.Err())
	}
	return string(out), err
}
