// Package compare builds the workbench pages of examples/shop three ways, as tifa, as a Next.js
// app and as a TanStack Start app, checks that the three show the same text, and measures how many
// whole page requests each answers. Nothing here runs in CI. `go test ./test/compare` (with
// TIFA_COMPARE=1) checks the pages, and `go run ./test/compare/cmd/compare` measures. docs/testing.md
// has the steps.
//
// The two Node apps hold only what is specific to their framework: the layouts and pages that load
// data, a few shims, and a package.json with a lockfile. Everything else is copied from
// examples/shop when the apps are built (sync.go), so a change to a component of the shop reaches
// all three at once. The data of the pages is what the shop's own program loads for them
// (snapshot.go), so it cannot drift either.
package compare

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Env is where the pieces are.
type Env struct {
	Root  string // the repository
	Shop  string // examples/shop
	Next  string // test/compare/nextjs
	Start string // test/compare/start
}

// NewEnv finds the repository from the location of this source file.
func NewEnv() (Env, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return Env{}, errors.New("cannot find the source file of package compare")
	}
	dir := filepath.Dir(file)
	root := filepath.Join(dir, "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return Env{}, fmt.Errorf("the repository is not at %s: %w", root, err)
	}
	root = filepath.Clean(root)
	return Env{
		Root:  root,
		Shop:  filepath.Join(root, "examples", "shop"),
		Next:  filepath.Join(dir, "nextjs"),
		Start: filepath.Join(dir, "start"),
	}, nil
}

// Page is one of the six workbench pages. Routes are the IDs of its Layouts and page, which the
// shop answers with their data when asked as the browser router does.
type Page struct {
	Name   string
	URL    string
	Routes string
}

const (
	shellRoute = "app/layout"
	listRoute  = "app/projects/project+/route"
	orderRoute = "app/projects/project+/orders/order+/route"
	statsRoute = "app/projects/project+/stats/route"
	projLayout = "app/projects/project+/layout"
)

func chain(page string) string { return shellRoute + "," + projLayout + "," + page }

// Pages are the six pages every comparison renders: the order list, an order and the stats of
// each of the two shops.
var Pages = []Page{
	{"list studio", "/projects/studio", chain(listRoute)},
	{"list weekend", "/projects/weekend", chain(listRoute)},
	{"order SH-1042", "/projects/studio/orders/SH-1042", chain(orderRoute)},
	{"order WK-0201", "/projects/weekend/orders/WK-0201", chain(orderRoute)},
	{"stats studio", "/projects/studio/stats", chain(statsRoute)},
	{"stats weekend", "/projects/weekend/stats", chain(statsRoute)},
}

// MissingTools returns a reason when something the build needs is not installed, and "" when all
// of go, node and pnpm are on the PATH. extra names more programs, such as oha.
func MissingTools(extra ...string) string {
	var missing []string
	for _, name := range append([]string{"go", "node", "pnpm"}, extra...) {
		if _, err := exec.LookPath(name); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return "not installed: " + strings.Join(missing, ", ")
	}
	return ""
}

// run runs a command in dir, writing its output to log, and returns an error that holds the end of
// the output when it fails.
func run(ctx context.Context, log io.Writer, dir string, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	fmt.Fprintf(log, "$ (cd %s && %s %s)\n", dir, name, strings.Join(args, " "))
	err := cmd.Run()
	if err != nil {
		tail := out.Bytes()
		if len(tail) > 4000 {
			tail = tail[len(tail)-4000:]
		}
		return fmt.Errorf("%s %s in %s: %w\n%s", name, strings.Join(args, " "), dir, err, tail)
	}
	return nil
}
