package compare

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// holdWith runs the command a machine has for keeping itself quiet for a timing round, such as a
// script that queues the round with the other work of the machine and takes its locks. The command
// is given a holder to run: holdWith returns once the holder runs, which is when the machine is
// quiet, and the function it returns ends the holder, and so the command. The holder ends by
// itself when this process does, so a run that dies holds nothing. With no command it does nothing.
func holdWith(ctx context.Context, command []string) (release func(), err error) {
	if len(command) == 0 {
		return func() {}, nil
	}
	// The holder says that it runs, then waits for the end of its input.
	args := append(append([]string{}, command[1:]...), "/bin/sh", "-c", "echo held; cat >/dev/null")
	cmd := exec.Command(command[0], args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s: %w", command[0], err)
	}
	exited := make(chan error, 1)
	held := make(chan bool, 1)
	go func() {
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		held <- strings.TrimSpace(line) == "held"
		io.Copy(io.Discard, stdout)
		exited <- cmd.Wait()
	}()
	end := func() {
		stdin.Close()
		select {
		case <-exited:
		case <-time.After(30 * time.Second):
		}
	}
	select {
	case ok := <-held:
		if !ok {
			stdin.Close()
			err := <-exited
			return nil, fmt.Errorf("%s ended before the machine was quiet: %v %s", command[0], err, strings.TrimSpace(stderr.String()))
		}
		return end, nil
	case <-ctx.Done():
		// The holder, once it runs, finds its input closed and ends. The command is told to end too.
		stdin.Close()
		cmd.Process.Signal(syscall.SIGTERM)
		return nil, fmt.Errorf("%s: %w", command[0], ctx.Err())
	}
}
