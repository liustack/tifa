package e2e

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// browserSessions holds the names of the sessions this test process opened, so that the daemons
// it left can be told from those of other runs.
var browserSessions sync.Map

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
		cmd := exec.Command("playwright-cli", append([]string{"-s=" + session}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("playwright-cli %v: %v\n%s", args, err, out)
		}
		return string(out)
	}, dir
}

func closeBrowser(t *testing.T, session, dir string) {
	t.Helper()
	cmd := exec.Command("playwright-cli", "-s="+session, "close")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
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
		t.Errorf("playwright-cli daemon %d of session %s is still running after close, ending it", pid, session)
		syscall.Kill(pid, syscall.SIGTERM)
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
			syscall.Kill(pid, syscall.SIGTERM)
		}
	}
}
