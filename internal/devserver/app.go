package devserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"time"
)

// app is one run of the application executable.
type app struct {
	cmd  *exec.Cmd
	done chan struct{} // closed once the process exited
	err  error         // result of cmd.Wait, read after done is closed
}

// startApp runs command with TIFA_SERVER_ADDR set to addr, which the application's
// configuration reads as server.addr.
func startApp(command []string, addr string, stdout, stderr io.Writer) (*app, error) {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env = append(os.Environ(), "TIFA_SERVER_ADDR="+addr)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting the app: %w", err)
	}
	a := &app{cmd: cmd, done: make(chan struct{})}
	go func() {
		a.err = cmd.Wait()
		close(a.done)
	}()
	return a, nil
}

// exitError describes how the process ended. Call it after done is closed.
func (a *app) exitError() error {
	if a.err == nil {
		return errors.New("the app exited")
	}
	return fmt.Errorf("the app exited: %w", a.err)
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
			return fmt.Errorf("the app did not listen on %s within %s", addr, timeout)
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

// stop sends an interrupt so the app runs OnShutdown, and kills it when it has not exited
// after timeout. The interrupt is not available on Windows, Run refuses to start there.
func (a *app) stop(timeout time.Duration) error {
	if err := a.cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("interrupting the app: %w", err)
	}
	select {
	case <-a.done:
		return nil
	case <-time.After(timeout):
	}
	if err := a.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("killing the app: %w", err)
	}
	<-a.done
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
