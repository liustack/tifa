// Package compare builds the workbench pages of examples/shop four ways, as tifa, as a Next.js app,
// as a TanStack Start app and as a React Router app, checks that the four show the same text, and
// measures how many whole page requests each answers. Nothing here runs in CI. `go test ./test/compare` (with
// TIFA_COMPARE=1) checks the pages, and `go run ./test/compare/cmd/compare` measures, reports and diffs. docs/testing.md
// has the steps.
//
// The three Node apps hold only what is specific to their framework: the layouts and pages that load
// data, a few shims, and a package.json with a lockfile. The components, the stylesheet and the
// photos are copied from examples/shop when the apps are built (build.go), so a change to a
// component of the shop reaches all three at once. The data of the pages is computed in Node on
// every request by domain/, a port of the Go code of the shop that TestDomainMatchesGo keeps equal
// to the Go code.
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
	"slices"
	"strings"
)

// Env is where the pieces are.
type Env struct {
	Root  string // the repository
	Shop  string // examples/shop
	Next  string // test/compare/nextjs
	Start string // test/compare/start
	React string // test/compare/reactrouter
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
		React: filepath.Join(dir, "reactrouter"),
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
	shellRoute    = "app/layout"
	listRoute     = "app/projects/project+/route"
	orderRoute    = "app/projects/project+/orders/order+/route"
	statsRoute    = "app/projects/project+/stats/route"
	productsRoute = "app/projects/project+/products/route"
	productRoute  = "app/projects/project+/products/product+/route"
	projLayout    = "app/projects/project+/layout"
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

// OrderIDs are the ids of every order of the shop's data, by project: SH-1001 to SH-1042 in studio
// and WK-0190 to WK-0201 in weekend. TestDomainMatchesGo checks them against the data of the shop.
var OrderIDs = map[string][2]int{"studio": {1001, 1042}, "weekend": {190, 201}}

var orderPrefix = map[string]string{"studio": "SH-", "weekend": "WK-"}

// ProductPages are the product pages of the back office: the list of the products of each shop
// (studio sells the catalogue of the storefront) and the edit page of one of each. The checks read
// them with the other pages, the measuring does not.
var ProductPages = []Page{
	{"products studio", "/projects/studio/products", chain(productsRoute)},
	{"product oak-tray", "/projects/studio/products/oak-tray", chain(productRoute)},
	{"products weekend", "/projects/weekend/products", chain(productsRoute)},
	{"product postcards", "/projects/weekend/products/postcards", chain(productRoute)},
}

// CheckedPages are the pages the checks read: AllPages and then ProductPages.
func CheckedPages() []Page { return append(AllPages(), ProductPages...) }

// AllPages are the workbench pages the apps serve: the six of Pages first, in their order, then
// the order page of every other order of the shop (54 order pages in all).
func AllPages() []Page {
	out := slices.Clone(Pages)
	for _, project := range []string{"studio", "weekend"} {
		r := OrderIDs[project]
		for n := r[1]; n >= r[0]; n-- {
			id := fmt.Sprintf("%s%04d", orderPrefix[project], n)
			if slices.ContainsFunc(out, func(p Page) bool { return p.Name == "order "+id }) {
				continue
			}
			out = append(out, Page{"order " + id, "/projects/" + project + "/orders/" + id, chain(orderRoute)})
		}
	}
	return out
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
