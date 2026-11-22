//go:build unix

// The lifeline of the app is file descriptor 3, passed with ExtraFiles, which Windows does not have.

package devserver

import (
	"bufio"
	"os/exec"
	"slices"
	"strconv"
	"testing"
)

// startWaiting runs a shell that waits for the end of its lifeline, as server.DevMain does, and
// prints that it ended. It returns the lifeline and a channel for the shell's line.
func startWaiting(t *testing.T, name string) (*lifeline, <-chan string) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", `read line <&3; echo "$0 ended"`, name)
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

// The app is told where its lifeline is by the flag that only server.DevMain has.
func TestLifelineTellsTheAppWhereItIs(t *testing.T) {
	cmd := exec.Command("app", "serve", "--dist", "x")
	life, err := newLifeline(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer life.close()
	want := []string{"app", "serve", "--dist", "x", "--lifeline-fd", strconv.Itoa(3)}
	if !slices.Equal(cmd.Args, want) {
		t.Fatalf("arguments %q, want %q", cmd.Args, want)
	}
	if len(cmd.ExtraFiles) != 1 {
		t.Fatalf("%d extra files, want the read end alone as file descriptor 3", len(cmd.ExtraFiles))
	}
}
