//go:build unix

package devserver

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// watchWithFileLimit runs in a test process started by runWatchWithFileLimit. It lowers the limit
// on open files, watches TIFA_TEST_DIR, edits TIFA_TEST_EDIT and prints whether the edit was
// reported and how many directories are polled.
func watchWithFileLimit() {
	if err := watchWithFileLimitErr(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func watchWithFileLimitErr() error {
	var lim syscall.Rlimit // the fields are uint64 or int64 depending on the system
	if _, err := fmt.Sscan(os.Getenv("TIFA_TEST_FILE_LIMIT"), &lim.Cur); err != nil {
		return err
	}
	lim.Max = lim.Cur
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return err
	}
	w, err := prepareWatcher(os.Getenv("TIFA_TEST_DIR"), nil, func(msg string) { fmt.Println("warning:", msg) })
	if err != nil {
		return err
	}
	if os.Getenv("TIFA_TEST_NO_BUDGET") != "" {
		w.budget = math.MaxInt // run into the system's own limit
	}
	w.minPoll = 10 * time.Millisecond
	if err := w.start(); err != nil {
		return err
	}
	polled := len(w.polled)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.run(ctx) }()
	edit := os.Getenv("TIFA_TEST_EDIT")
	want := map[string]bool{}
	if stage := os.Getenv("TIFA_TEST_STAGE"); stage != "" {
		// Move every file of stage into the watched directory edit while the watcher runs.
		entries, err := os.ReadDir(stage)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := os.Rename(filepath.Join(stage, e.Name()), filepath.Join(w.root, edit, e.Name())); err != nil {
				return err
			}
			want[path.Join(edit, e.Name())] = true
		}
	} else {
		if err := os.WriteFile(filepath.Join(w.root, edit), []byte(fmt.Sprintf("export const edited = %d", time.Now().UnixNano())), 0o644); err != nil {
			return err
		}
		want[edit] = true
	}
	timeout := time.After(10 * time.Second)
	for {
		select {
		case err := <-done:
			return fmt.Errorf("watcher stopped: %v", err)
		case <-w.changes.ready:
			for _, c := range w.changes.take() {
				delete(want, c.path)
			}
			if len(want) == 0 {
				fmt.Printf("changed %s, polled %d\n", edit, polled)
				return nil
			}
		case <-timeout:
			return fmt.Errorf("%s: %d changes not reported within 10s", edit, len(want))
		}
	}
}

func runWatchWithFileLimit(t *testing.T, dir, edit string, limit int, noBudget bool, env ...string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "TIFA_TEST_FILE_LIMIT="+strconv.Itoa(limit), "TIFA_TEST_DIR="+dir, "TIFA_TEST_EDIT="+edit)
	if noBudget {
		cmd.Env = append(cmd.Env, "TIFA_TEST_NO_BUDGET=1")
	}
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// With few file descriptors, the directories that do not fit are polled and edits in them are
// still reported, whether the watcher stops at its budget or the system says it ran out.
func TestWatcherWorksWithFewFiles(t *testing.T) {
	dir := newApp(t)
	for i := range 300 {
		writeFile(t, dir, fmt.Sprintf("app/m%02d/f%02d.ts", i/10, i%10), "export {}")
	}
	edit := "app/m29/f09.ts" // in the last directory watched
	for _, noBudget := range []bool{false, true} {
		t.Run(fmt.Sprintf("budget=%v", !noBudget), func(t *testing.T) {
			out := runWatchWithFileLimit(t, dir, edit, 128, noBudget)
			t.Log(out)
			if !strings.Contains(out, "changed "+edit) {
				t.Fatalf("the edit was not reported: %s", out)
			}
			if strings.Contains(out, "polled 0") {
				t.Logf("watching %s needs no descriptor per file on this system, nothing was polled", dir)
			}
		})
	}
}

// A watched directory that grows while tifa dev runs takes more file descriptors on kqueue, one
// per new file. Past the budget, or once the system runs out, the watcher polls instead of
// stopping, and every file that arrived is reported.
func TestWatcherWorksWhenADirectoryGrows(t *testing.T) {
	for _, noBudget := range []bool{false, true} {
		t.Run(fmt.Sprintf("budget=%v", !noBudget), func(t *testing.T) {
			dir := newApp(t)
			stage := t.TempDir()
			for i := range 180 {
				writeFile(t, stage, fmt.Sprintf("f%03d.ts", i), "export {}")
			}
			out := runWatchWithFileLimit(t, dir, "app", 128, noBudget, "TIFA_TEST_STAGE="+stage)
			t.Log(out)
			if !strings.Contains(out, "changed app") {
				t.Fatalf("the new files were not all reported: %s", out)
			}
		})
	}
}
