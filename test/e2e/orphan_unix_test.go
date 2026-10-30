//go:build unix

package e2e

import (
	"testing"

	"github.com/liustack/tifa/internal/testproc/orphantest"
)

// TestStartHelper is the test process of the tests below: it starts a fake program with start.
func TestStartHelper(t *testing.T) {
	orphantest.Helper(t, func(t *testing.T, exe string) { start(t, exe) })
}

// A program that start runs must not outlive the test process, whichever way it dies.
func TestStartEndsWhenTheTestProcessDies(t *testing.T) {
	orphantest.Ends(t, "TestStartHelper")
}

// The program must not hold files its test process inherited, such as the lock of a script that
// runs the tests under flock.
func TestStartKeepsInheritedFilesOut(t *testing.T) {
	orphantest.NoInheritedFiles(t, "TestStartHelper")
}
