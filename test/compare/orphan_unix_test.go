//go:build unix

package compare_test

import (
	"context"
	"os"
	"testing"

	"github.com/liustack/tifa/internal/testproc/orphantest"
	"github.com/liustack/tifa/test/compare"
)

// TestStartHelper is the test process of the tests below: it starts a fake program with
// Target.Start, as the comparison starts the servers it measures.
func TestStartHelper(t *testing.T) {
	orphantest.Helper(t, func(t *testing.T, exe string) {
		target := compare.Target{Name: "fake", Dir: os.TempDir(), Prog: exe, Args: []string{"--server-addr", "127.0.0.1:%PORT%"}}
		server, err := target.Start(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(server.Stop)
	})
}

// A server that Target.Start runs must not outlive the test process, whichever way it dies.
func TestStartEndsWhenTheTestProcessDies(t *testing.T) {
	orphantest.Ends(t, "TestStartHelper")
}

// The server must not hold files its test process inherited, such as the lock of a script that
// runs the tests under flock.
func TestStartKeepsInheritedFilesOut(t *testing.T) {
	orphantest.NoInheritedFiles(t, "TestStartHelper")
}
