//go:build unix

package ssr_test

import (
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

// fakeServer builds the stand-in for a program that tifa build makes, and returns its path.
func fakeServer(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "fakeserver")
	if out, err := exec.Command("go", "build", "-o", exe, "./testdata/fakeserver").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return exe
}

// fakeReport is what the fake program wrote about itself: its process ID, the ID of its parent
// (the reaper), and the device and inode of each file it holds open from descriptor 3 on.
type fakeReport struct {
	pid, ppid int
	files     []string
}

// readFakeReport waits for the report that the fake program writes before it says it listens.
func readFakeReport(t *testing.T, path string) fakeReport {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
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
		return fakeReport{pid, ppid, lines[2:]}
	}
	t.Fatalf("the fake program wrote no report to %s", path)
	return fakeReport{}
}

// gone waits for the process pid to end.
func gone(pid int) bool {
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
	}
	return false
}

// TestStartProgramHelper is the test process of the tests below: it starts the fake program, and
// then either dies from a panic in a goroutine of its own, which runs no t.Cleanup, or waits to be
// killed.
func TestStartProgramHelper(t *testing.T) {
	exe := os.Getenv("FAKE_SERVER")
	if exe == "" {
		t.Skip("runs only as the helper of the tests of startProgram")
	}
	startProgram(t, exe)
	if os.Getenv("FAKE_HELPER") == "panic" {
		go panic("a panic outside the test goroutine")
	}
	time.Sleep(time.Minute)
}

// helperOptions say how startHelper runs the helper. lock is a file the helper holds open from
// descriptor 3 on, without close-on-exec, as a shell holds the lock of flock. nohup runs it with
// SIGHUP ignored, which programs started from it inherit.
type helperOptions struct {
	mode  string
	lock  *os.File
	nohup bool
}

// startHelper runs TestStartProgramHelper in a process of its own, as test processes do, and
// returns it once the fake program it started is up.
func startHelper(t *testing.T, opt helperOptions) (*exec.Cmd, fakeReport) {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "report")
	args := []string{os.Args[0], "-test.run=^TestStartProgramHelper$"}
	if opt.nohup {
		nohup, err := exec.LookPath("nohup")
		if err != nil {
			t.Skip(err)
		}
		args = append([]string{nohup}, args...)
	}
	helper := exec.Command(args[0], args[1:]...)
	helper.Env = append(os.Environ(), "FAKE_SERVER="+fakeServer(t), "FAKE_PIDFILE="+pidFile, "FAKE_HELPER="+opt.mode)
	if opt.lock != nil {
		helper.ExtraFiles = []*os.File{opt.lock}
	}
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	report := readFakeReport(t, pidFile)
	// Whatever the result, do not leave the helper or the fake program running.
	t.Cleanup(func() {
		syscall.Kill(report.pid, syscall.SIGKILL)
		syscall.Kill(report.ppid, syscall.SIGKILL)
		helper.Process.Kill()
		helper.Wait()
	})
	return helper, report
}

// A program that startProgram runs must not outlive the test process, nor the reaper that startProgram
// puts between them, whichever way they die: a panic in another goroutine and a timeout end a process
// at once and run no cleanup, and the kill of a script is a SIGKILL.
func TestStartProgramEndsWhenItsParentsDie(t *testing.T) {
	for _, tc := range []struct {
		name string
		opt  helperOptions
		kill func(helper *exec.Cmd, report fakeReport)
	}{
		{"test process panics", helperOptions{mode: "panic"}, func(*exec.Cmd, fakeReport) {}},
		{"test process killed", helperOptions{mode: "wait"}, func(helper *exec.Cmd, _ fakeReport) { helper.Process.Kill() }},
		{"reaper killed", helperOptions{mode: "wait"}, func(_ *exec.Cmd, report fakeReport) { syscall.Kill(report.ppid, syscall.SIGKILL) }},
		{"reaper killed, test process ignores SIGHUP", helperOptions{mode: "wait", nohup: true}, func(_ *exec.Cmd, report fakeReport) { syscall.Kill(report.ppid, syscall.SIGKILL) }},
		{"reaper and test process killed", helperOptions{mode: "wait"}, func(helper *exec.Cmd, report fakeReport) {
			syscall.Kill(report.ppid, syscall.SIGKILL)
			helper.Process.Kill()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			helper, report := startHelper(t, tc.opt)
			tc.kill(helper, report)
			if !gone(report.pid) {
				t.Fatalf("the program (pid %d) still runs after %s", report.pid, tc.name)
			}
		})
	}
}

// The program must not hold files its test process inherited, such as the lock of a script that
// runs the tests under flock: it would keep the lock for as long as it lives.
func TestStartProgramDoesNotInheritFiles(t *testing.T) {
	lock, err := os.Create(filepath.Join(t.TempDir(), "lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	var st syscall.Stat_t
	if err := syscall.Fstat(int(lock.Fd()), &st); err != nil {
		t.Fatal(err)
	}
	_, report := startHelper(t, helperOptions{mode: "wait", lock: lock})
	if got := fmt.Sprintf("%d:%d", uint64(st.Dev), uint64(st.Ino)); slices.Contains(report.files, got) {
		t.Fatalf("the program (pid %d) holds the lock file %s, among its files %v", report.pid, got, report.files)
	}
}

// When the test ends normally, its cleanup stops the program before the test returns.
func TestStartProgramStopsAtCleanup(t *testing.T) {
	exe := fakeServer(t)
	pidFile := filepath.Join(t.TempDir(), "report")
	t.Setenv("FAKE_PIDFILE", pidFile)
	t.Run("start", func(t *testing.T) { startProgram(t, exe) })
	report := readFakeReport(t, pidFile)
	if syscall.Kill(report.pid, 0) == nil {
		syscall.Kill(report.pid, syscall.SIGKILL)
		t.Fatalf("the program (pid %d) still runs after its test ended", report.pid)
	}
}
