package tifatest_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/liustack/tifa/tifatest"
)

// A Start that fails closes the listener it opened.
func TestFailedStartClosesItsListener(t *testing.T) {
	if _, err := os.ReadDir("/dev/fd"); err != nil {
		t.Skip("no /dev/fd to count the open files with")
	}
	open := func() int {
		entries, err := os.ReadDir("/dev/fd")
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}
	app := signInApp()
	app.Hooks.OnStart = func(context.Context) error { return errors.New("no database") }
	before := open()
	for range 20 {
		f := &fatal{TB: t}
		if srv := tifatest.Start(f, app); srv != nil || f.msg == "" {
			t.Fatalf("Start with a failing OnStart: %v, %q", srv, f.msg)
		}
	}
	if after := open(); after >= before+20 {
		t.Errorf("20 failed starts left %d more files open", after-before)
	}
}
