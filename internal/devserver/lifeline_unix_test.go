//go:build unix

package devserver

import (
	"bufio"
	"os/exec"
	"slices"
	"strconv"
	"testing"
	"time"
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

// The app reads the end of its lifeline when the write end closes, which the system does when
// tifa dev ends. It must not be held by another app that tifa dev starts later, or the first one
// would not stop until that one did.
func TestLifelineEndsWhenOnlyTifaDevHoldsTheWriteEnd(t *testing.T) {
	first, firstEnded := startWaiting(t, "first")
	second, secondEnded := startWaiting(t, "second")

	select {
	case line := <-firstEnded:
		t.Fatalf("the first app ended with its lifeline open: %q", line)
	case line := <-secondEnded:
		t.Fatalf("the second app ended with its lifeline open: %q", line)
	case <-time.After(300 * time.Millisecond):
	}
	first.close()
	select {
	case line := <-firstEnded:
		if line != "first ended\n" {
			t.Fatalf("the first app printed %q", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the first app did not see the end of its lifeline, something else holds the write end")
	}
	select {
	case line := <-secondEnded:
		t.Fatalf("the second app ended when the first lifeline closed: %q", line)
	case <-time.After(300 * time.Millisecond):
	}
	second.close()
	select {
	case <-secondEnded:
	case <-time.After(10 * time.Second):
		t.Fatal("the second app did not see the end of its lifeline")
	}
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
