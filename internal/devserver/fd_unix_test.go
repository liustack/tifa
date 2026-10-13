//go:build unix

package devserver

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// watchWithFileLimit runs in a test process started by runWatchWithFileLimit.
func watchWithFileLimit() {
	n, err := strconv.ParseUint(os.Getenv("TIFA_TEST_FILE_LIMIT"), 10, 64)
	if err == nil {
		err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &syscall.Rlimit{Cur: n, Max: n})
	}
	if err == nil {
		var ignore []string
		if d := os.Getenv("TIFA_TEST_IGNORE"); d != "" {
			ignore = []string{d}
		}
		var w *watcher
		w, err = newWatcher(os.Getenv("TIFA_TEST_DIR"), ignore, func(string) {})
		if err == nil {
			w.fs.Close()
		}
	}
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println("watching")
}

func runWatchWithFileLimit(t *testing.T, dir, ignore string, limit int) (string, error) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "TIFA_TEST_FILE_LIMIT="+strconv.Itoa(limit), "TIFA_TEST_DIR="+dir, "TIFA_TEST_IGNORE="+ignore)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func TestWatcherExplainsRunningOutOfFiles(t *testing.T) {
	dir := newApp(t)
	for i := range 300 {
		writeFile(t, dir, fmt.Sprintf("assets/%03d.png", i), "png")
	}
	out, err := runWatchWithFileLimit(t, dir, "", 128)
	if err == nil {
		t.Skipf("watching %s needs no descriptor per file on this system", dir)
	}
	for _, want := range []string{"ulimit -n", "Ignore"} {
		if !strings.Contains(out, want) {
			t.Fatalf("error %q does not mention %q", out, want)
		}
	}
	if out, err := runWatchWithFileLimit(t, dir, "assets", 128); err != nil {
		t.Fatalf("with assets ignored: %s", out)
	}
}
