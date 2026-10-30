//go:build unix

package ssr_test

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/liustack/tifa/internal/testproc/orphantest"
)

// TestStartProgramHelper is the test process of the tests below: it starts the fake program.
func TestStartProgramHelper(t *testing.T) {
	orphantest.Helper(t, func(t *testing.T, exe string) { startProgram(t, exe) })
}

// A program that startProgram runs must not outlive the test process, whichever way it dies: a
// panic in another goroutine and a timeout end a process at once and run no cleanup, and the kill
// of a script is a SIGKILL.
func TestStartProgramEndsWhenItsParentDies(t *testing.T) {
	orphantest.Ends(t, "TestStartProgramHelper")
}

// The program must not hold files its test process inherited, such as the lock of a script that
// runs the tests under flock: it would keep the lock for as long as it lives.
func TestStartProgramDoesNotInheritFiles(t *testing.T) {
	orphantest.NoInheritedFiles(t, "TestStartProgramHelper")
}

// When the test ends normally, its cleanup stops the program before the test returns.
func TestStartProgramStopsAtCleanup(t *testing.T) {
	exe := orphantest.FakeServer(t)
	pidFile := filepath.Join(t.TempDir(), "report")
	t.Setenv("FAKE_PIDFILE", pidFile)
	t.Run("start", func(t *testing.T) { startProgram(t, exe) })
	report := orphantest.ReadReport(t, pidFile)
	if syscall.Kill(report.Pid, 0) == nil {
		syscall.Kill(report.Pid, syscall.SIGKILL)
		t.Fatalf("the program (pid %d) still runs after its test ended", report.Pid)
	}
}
