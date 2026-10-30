//go:build unix

// Package orphantest checks that a program a test starts does not outlive the test process. A
// package that starts programs in the background has a helper test, which starts a fake program the
// way the package's tests start real ones. The checks run that helper test in a process of its own,
// end the process in the ways a test process can end without running t.Cleanup, and look at the
// fake program afterwards.
package orphantest

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

//go:embed testdata/fakeserver/main.go
var fakeServerSource []byte

// FakeServer builds the stand-in for a program that the tests start, and returns its path. It
// writes a report about itself (see Report) to the file named by FAKE_PIDFILE, listens on the
// address after --server-addr, and answers every request with 200.
func FakeServer(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, fakeServerSource, 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "fakeserver")
	cmd := exec.Command("go", "build", "-o", exe, src)
	cmd.Dir = dir // outside any module: the program uses only the standard library
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return exe
}

// Report is what the fake program wrote about itself: its process ID, the ID of its parent, and
// the device and inode of each file it holds open from descriptor 3 on, as "dev:ino".
type Report struct {
	Pid, Ppid int
	Files     []string
}

// ReadReport waits for the report that the fake program writes before it says it listens.
func ReadReport(t testing.TB, path string) Report {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		data, err := os.ReadFile(path)
		lines := strings.Fields(string(data))
		if err != nil || len(lines) < 2 {
			continue
		}
		pid, err1 := strconv.Atoi(lines[0])
		ppid, err2 := strconv.Atoi(lines[1])
		if err1 != nil || err2 != nil {
			t.Fatalf("bad report:\n%s", data)
		}
		return Report{pid, ppid, lines[2:]}
	}
	t.Fatalf("the fake program wrote no report to %s", path)
	return Report{}
}

// Gone waits for the process pid to end.
func Gone(pid int) bool {
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
	}
	return false
}

const (
	envExe  = "ORPHANTEST_EXE"
	envMode = "ORPHANTEST_MODE"
)

// Helper is the body of the helper test of a package. It runs only in the process that Ends and
// NoInheritedFiles start. It calls start with the fake program, which starts it the way the
// package starts the programs of its tests, and then either dies from a panic in a goroutine of its
// own, which runs no t.Cleanup, or waits to be killed. A test skips when run on its own.
func Helper(t *testing.T, start func(t *testing.T, exe string)) {
	exe := os.Getenv(envExe)
	if exe == "" {
		t.Skip("runs only as the helper of the checks of orphantest")
	}
	start(t, exe)
	// A start that returns before the program is up must not let the panic come first: the program
	// would end before it wrote its report.
	ReadReport(t, os.Getenv("FAKE_PIDFILE"))
	if os.Getenv(envMode) == "panic" {
		go panic("a panic outside the test goroutine")
	}
	time.Sleep(time.Minute)
}

// options say how startHelper runs the helper. lock is a file the helper holds open from
// descriptor 3 on, without close-on-exec, as a shell holds the lock of flock. nohup runs it with
// SIGHUP ignored, which programs started from it inherit.
type options struct {
	mode  string
	lock  *os.File
	nohup bool
}

// startHelper runs the test helper in a process of its own, as test processes are, and returns it
// once the fake program it started is up.
func startHelper(t *testing.T, helper string, opt options) (*exec.Cmd, Report) {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "report")
	args := []string{os.Args[0], "-test.run=^" + helper + "$"}
	if opt.nohup {
		nohup, err := exec.LookPath("nohup")
		if err != nil {
			t.Skip(err)
		}
		args = append([]string{nohup}, args...)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = append(os.Environ(), envExe+"="+FakeServer(t), "FAKE_PIDFILE="+pidFile, envMode+"="+opt.mode)
	if opt.lock != nil {
		cmd.ExtraFiles = []*os.File{opt.lock}
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Whatever the result, do not leave the helper or the fake program running.
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	var report Report
	t.Cleanup(func() {
		if report.Pid != 0 {
			syscall.Kill(report.Pid, syscall.SIGKILL)
		}
		cmd.Process.Kill()
		<-exited
	})
	report = ReadReport(t, pidFile)
	return cmd, report
}

// Ends checks that the program the helper test starts ends when the test process ends, however it
// ends: a panic in another goroutine and a timeout end a process at once and run no cleanup, and
// the kill of a script is a SIGKILL. It also checks the test process killed while it ignores
// SIGHUP, as it does under nohup.
func Ends(t *testing.T, helper string) {
	for _, tc := range []struct {
		name string
		opt  options
		kill func(cmd *exec.Cmd)
	}{
		{"test process panics", options{mode: "panic"}, func(*exec.Cmd) {}},
		{"test process killed", options{mode: "wait"}, func(cmd *exec.Cmd) { cmd.Process.Kill() }},
		{"test process killed, ignoring SIGHUP", options{mode: "wait", nohup: true}, func(cmd *exec.Cmd) { cmd.Process.Kill() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd, report := startHelper(t, helper, tc.opt)
			tc.kill(cmd)
			if !Gone(report.Pid) {
				t.Fatalf("the program (pid %d) still runs after %s", report.Pid, tc.name)
			}
		})
	}
}

// NoInheritedFiles checks that the program the helper test starts does not hold the files its test
// process inherited, such as the lock of a script that runs the tests under flock: it would keep
// the lock for as long as it lives.
func NoInheritedFiles(t *testing.T, helper string) {
	lock, err := os.Create(filepath.Join(t.TempDir(), "lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	var st syscall.Stat_t
	if err := syscall.Fstat(int(lock.Fd()), &st); err != nil {
		t.Fatal(err)
	}
	_, report := startHelper(t, helper, options{mode: "wait", lock: lock})
	if got := fmt.Sprintf("%d:%d", uint64(st.Dev), uint64(st.Ino)); slices.Contains(report.Files, got) {
		t.Fatalf("the program (pid %d) holds the lock file %s, among its files %v", report.Pid, got, report.Files)
	}
}
