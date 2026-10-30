//go:build unix

package testproc

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/testproc/orphantest"
)

// TestStartHelper is the test process of the tests below: it starts the fake program.
func TestStartHelper(t *testing.T) {
	orphantest.Helper(t, func(t *testing.T, exe string) {
		cmd := exec.Command(exe)
		if err := Start(cmd); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	})
}

func TestStartEndsWhenTheTestProcessDies(t *testing.T) {
	orphantest.Ends(t, "TestStartHelper")
}

func TestStartKeepsInheritedFilesOut(t *testing.T) {
	orphantest.NoInheritedFiles(t, "TestStartHelper")
}

// Linux ends a program when the thread that started it ends. A goroutine that locks its thread and
// returns ends the thread, and the program it started must live on. The goroutines go one after the
// other, since Go never ends the main thread, and a program started from it would live on anyway.
func TestStartSurvivesTheThreadOfItsCaller(t *testing.T) {
	exe := orphantest.FakeServer(t)
	for i := range 3 {
		cmd := exec.Command(exe)
		pidFile := filepath.Join(t.TempDir(), "report")
		cmd.Env = append(os.Environ(), "FAKE_PIDFILE="+pidFile)
		done := make(chan error)
		go func() {
			runtime.LockOSThread() // never unlocked: the thread ends with the goroutine
			done <- Start(cmd)
		}()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		exited := make(chan struct{})
		var waitErr error
		go func() { waitErr = cmd.Wait(); close(exited) }()
		t.Cleanup(func() { cmd.Process.Kill(); <-exited })
		orphantest.ReadReport(t, pidFile)
		// A program ended by the signal stays a zombie until it is waited for, so Wait tells.
		select {
		case <-exited:
			t.Fatalf("program %d ended with the thread of the goroutine that started it: %v", i, waitErr)
		case <-time.After(300 * time.Millisecond):
		}
	}
}
