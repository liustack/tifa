package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// notRecognized starts every build warning that says a rewrite of the tifa Vite plugin did not
// recognize the code it was to change (js/vite/plugin.js).
const notRecognized = "tifa rewrite not recognized: "

// The SSR bundle of each example app that tests render pages from is built with every rewrite of
// the Vite plugin. A rewrite that does not recognize the library it changes turns itself off, the
// pages render the same and slower, and the build prints one warning. So a dependency that moves
// on is found here, where the build prints the text that starts such a warning. The scheduled CI
// job runs this test against the latest React.
func TestBuildsRecognizeEveryRewrite(t *testing.T) {
	skipUnlessEnabled(t)
	for _, app := range []struct{ name, dir string }{
		{"shop", filepath.Join("examples", "shop")},
		{"mui", filepath.Join("test", "apps", "mui")},
		{"bench", filepath.Join("test", "apps", "bench")},
	} {
		t.Run(app.name, func(t *testing.T) {
			_, output := buildWithOutput(t, filepath.Join(repoRoot(t), app.dir), os.Getenv("PATH"))
			for _, line := range strings.Split(output, "\n") {
				if strings.Contains(line, notRecognized) {
					t.Errorf("the build of %s prints a rewrite that did not recognize its code:\n%s", app.dir, line)
				}
			}
		})
	}
}
