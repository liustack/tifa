package tifatest

import (
	"bytes"
	"sync"
	"testing"

	"github.com/liustack/tifa/server"
)

// Command runs the command of the app that args name in the test process, as the program runs
// it: OnCommand adds the commands, the command takes the flags of the settings, the settings are
// loaded before it runs, and cmd.Context() is the context of t. OnStart and OnShutdown do not run,
// as in the program, so the test opens what the command needs, such as the database, the way the
// command does. app is tifaapp.App, as for Start.
//
// It returns what the command wrote to cmd.OutOrStdout() and cmd.ErrOrStderr(), in the order it
// wrote it, and the error the command returned, which the program would print before it exits
// with 1. What the command prints with fmt.Println goes to the output of the test instead.
//
// The settings come from args and tifa.yaml alone, not from the environment or a .env file. The
// options of the app keep their defaults in a test binary, so set the variables of options.go in the
// test: a flag of an option of the app fails the test. Command also fails the test when args name
// no command of the app, and when .tifa/tifaapp was generated from other routes than the app has
// now.
func Command(t testing.TB, app server.App, args ...string) (output string, err error) {
	t.Helper()
	if !checkTable(t, "Command", app) {
		return "", nil
	}
	var out lockedBuffer
	run, err := server.CommandTest(t.Context(), app, args, &out)
	if err != nil {
		t.Fatalf("tifatest.Command: %v", err)
		return "", nil
	}
	err = run()
	return out.String(), err
}

// lockedBuffer is a buffer a command may write to from several goroutines.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
