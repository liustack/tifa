package ssr_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/liustack/tifa/internal/testproc"
)

func TestMain(m *testing.M) {
	// A script that runs the tests may leave files open that this process inherits, such as the
	// lock of flock. No program the tests start may hold them: it would keep a lock for as long as
	// it lives.
	if err := testproc.CloseInheritedOnExec(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
