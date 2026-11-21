//go:build windows

package devserver

import (
	"bufio"
	"os"
	"os/exec"
	"slices"
	"testing"
)

// startWaiting runs the test binary as a program that waits for the end of its lifeline, as
// server.DevMain does, and prints that it ended. It returns the lifeline and a channel for the
// program's line.
func startWaiting(t *testing.T, name string) (*lifeline, <-chan string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "TIFA_FAKE_WAIT="+name)
	life, err := newLifeline(cmd)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	life.started()
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		life.close()
	})
	lines := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(out).ReadString('\n')
		lines <- line
	}()
	return life, lines
}

// The app is told where its lifeline is by the flag that only server.DevMain has: its standard
// input.
func TestLifelineTellsTheAppWhereItIs(t *testing.T) {
	cmd := exec.Command("app", "serve", "--dist", "x")
	life, err := newLifeline(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer life.close()
	want := []string{"app", "serve", "--dist", "x", "--lifeline-fd", "0"}
	if !slices.Equal(cmd.Args, want) {
		t.Fatalf("arguments %q, want %q", cmd.Args, want)
	}
	if cmd.Stdin != life.r || len(cmd.ExtraFiles) != 0 {
		t.Fatalf("stdin %v and %d extra files, want the read end as stdin alone", cmd.Stdin, len(cmd.ExtraFiles))
	}
}
