package devserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

// appWaitDelay bounds how long Wait keeps reading the app's output after the app exited.
// A child that left the process group can hold the output pipe open forever.
const appWaitDelay = time.Second

// app is one run of the application executable.
type app struct {
	cmd  *exec.Cmd
	done chan struct{} // closed once the process exited
	err  error         // result of cmd.Wait, read after done is closed
	gen  int           // counts the apps one Run started, the newest is current
	// stderr keeps the end of what the app wrote to stderr, where server.Main writes why it
	// exits and the runtime writes a panic.
	stderr tail
}

// startApp runs command in dir with TIFA_SERVER_ADDR set to addr, which the application's
// configuration reads as server.addr. It also gives the app a lifeline (see lifeline), so that the
// app stops when this process ends, however it ends.
func startApp(command []string, dir, addr string, stdout, stderr io.Writer) (*app, error) {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TIFA_SERVER_ADDR="+addr)
	a := &app{cmd: cmd, done: make(chan struct{})}
	// Wait returns once the output is copied, so the tail is complete when done is closed.
	cmd.Stdout, cmd.Stderr = stdout, io.MultiWriter(stderr, &a.stderr)
	cmd.WaitDelay = appWaitDelay
	ownGroup(cmd)
	life, err := newLifeline(cmd)
	if err != nil {
		return nil, fmt.Errorf("starting the app: %w", err)
	}
	if err := cmd.Start(); err != nil {
		life.close()
		return nil, fmt.Errorf("starting the app: %w", err)
	}
	life.started()
	go func() {
		a.err = cmd.Wait()
		// Whatever the app left running in its group goes with it.
		_ = killGroup(cmd.Process)
		life.close()
		close(a.done)
	}()
	return a, nil
}

// exitError describes how the process ended. Call it after done is closed.
func (a *app) exitError() error {
	err := errors.New("the app exited")
	if a.err != nil {
		err = fmt.Errorf("the app exited: %w", a.err)
	}
	return &appExit{err: err, output: a.stderr.String()}
}

// appExit is why an app did not start or stopped, with the end of its stderr. Error is the
// short message for the log of tifa dev, which shows the output of the app already. The browser
// gets the output too, see explain.
type appExit struct {
	err    error
	output string
}

func (e *appExit) Error() string { return e.err.Error() }
func (e *appExit) Unwrap() error { return e.err }

// explain is err as the browser shows it: with the last output of the app when err is about
// the app, as the browser has no terminal to look at.
func explain(err error) string {
	var exit *appExit
	if errors.As(err, &exit) && exit.output != "" {
		return err.Error() + "\n\nThe app's last output:\n" + exit.output
	}
	return err.Error()
}

// waitReady waits until addr accepts connections. It fails when the process exits first,
// or when timeout passes.
func (a *app) waitReady(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return &appExit{err: fmt.Errorf("the app did not listen on %s within %s", addr, timeout), output: a.stderr.String()}
		}
		select {
		case <-a.done:
			return a.exitError()
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// stop interrupts the app's process group so the app runs OnShutdown, and kills the group when
// the app has not exited after timeout. The interrupt is not available on Windows, Run refuses
// to start there.
func (a *app) stop(timeout time.Duration) error {
	if err := interruptGroup(a.cmd.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("interrupting the app: %w", err)
	}
	select {
	case <-a.done:
		return nil
	case <-time.After(timeout):
	}
	if err := killGroup(a.cmd.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("killing the app: %w", err)
	}
	<-a.done // Wait gives up on the output appWaitDelay after the exit
	return fmt.Errorf("the app was still running %s after an interrupt, killed it", timeout)
}

// freeAddr returns a loopback address with a port that is free right now.
func freeAddr() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer ln.Close()
	return ln.Addr().String(), nil
}

// The limits of what tail keeps.
const (
	tailLines = 40
	tailBytes = 16 << 10
)

// tail keeps the last lines written to it, at most tailLines lines and tailBytes bytes. The line
// of a panic or a fatal error comes first in a Go crash, and a long stack trace pushes it out,
// so tail keeps that line apart and puts it in front. It counts only while the output ends with
// the report it starts: a crash report is the last thing a process writes, and a line after it
// that no report has, such as a log line, means the process went on.
type tail struct {
	mu      sync.Mutex
	buf     []byte
	dropped int    // the bytes cut from the front of buf
	line    []byte // the start of the line being written, at most maxLine bytes
	last    byte   // the last byte of the line being written
	lineAt  int    // where the line being written starts, counting dropped bytes
	crash   []byte // the line that starts the report the output ends with, nil without one
	crashAt int    // where crash starts, counting dropped bytes
}

// maxLine is how much of a line tail reads to sort it, and keeps of a crash line.
const maxLine = 1024

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	at := t.dropped + len(t.buf)
	t.buf = append(t.buf, p...)
	for rest := p; len(rest) > 0; {
		part, after, ended := bytes.Cut(rest, []byte("\n"))
		t.line = append(t.line, part[:min(len(part), maxLine-len(t.line))]...)
		if len(part) > 0 {
			t.last = part[len(part)-1]
		}
		at += len(part)
		rest = after
		if ended {
			at++
			t.endLine()
			t.lineAt = at
		}
	}
	// Trimming copies, so it waits until buf holds twice what String may show.
	if len(t.buf) > 2*tailBytes {
		cut := len(t.buf) - tailBytes
		t.buf = append([]byte(nil), t.buf[cut:]...)
		t.dropped += cut
	}
	return len(p), nil
}

// endLine sorts the line just written. t.mu is held.
func (t *tail) endLine() {
	switch {
	case crashStart(t.line):
		t.crash, t.crashAt = append([]byte(nil), t.line...), t.lineAt
	case t.crash != nil && !inReport(t.line, t.last):
		t.crash = nil
	}
	t.line, t.last = t.line[:0], 0
}

// String returns the last lines, after the crash line when they leave it out.
func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	start := max(0, len(t.buf)-tailBytes)
	lines := 0
	for i := len(bytes.TrimSuffix(t.buf, []byte("\n"))) - 1; i >= start; i-- {
		if t.buf[i] == '\n' {
			if lines++; lines == tailLines {
				start = i + 1
				break
			}
		}
	}
	crash := t.crash
	if len(t.line) > 0 && !inReport(t.line, t.last) || t.crashAt >= t.dropped+start {
		crash = nil // the output went on after it, or it is shown anyway
	}
	if crash == nil {
		return string(t.buf[start:])
	}
	return string(crash) + "\n...\n" + string(t.buf[start:])
}

// crashStart reports whether line starts the report of a Go panic or fatal error.
func crashStart(line []byte) bool {
	return bytes.HasPrefix(line, []byte("panic: ")) || bytes.HasPrefix(line, []byte("fatal error: "))
}

// inReport reports whether a line can be part of the report of a Go crash: a blank line, a
// goroutine header, a frame (a function call, or its file indented by a tab) and the other
// lines the runtime writes. last is the line's last byte.
func inReport(line []byte, last byte) bool {
	if len(line) == 0 || line[0] == '\t' || last == ')' || last == ':' {
		return true
	}
	for _, prefix := range []string{"goroutine ", "created by ", "[", "...", "panic: ", "fatal error: ", "runtime: ", "runtime stack:"} {
		if bytes.HasPrefix(line, []byte(prefix)) {
			return true
		}
	}
	return false
}
