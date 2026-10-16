package bundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// The versions of TanStack Router tifa is tested with. This is the one place that names them:
// tifa new writes TestedReactRouter into package.json, and a test checks that the apps of this
// repository install both. To move to another version, change them here, install it in those
// apps and run the router tests in internal/codegen, which check the option below.
//
// js/runtime/router.tsx sets router.options.serializationAdapters, which these versions read when
// they write the page data on the server and read it back in the browser, but which TanStack does
// not document outside TanStack Start. TestedRouterCore is the router-core that TestedReactRouter
// depends on.
const (
	TestedReactRouter = "1.170.42"
	TestedRouterCore  = "1.171.35"
)

// CheckTanStack returns an error when the app at root installs another @tanstack/react-router
// than TestedReactRouter, or when that react-router resolves another @tanstack/router-core than
// TestedRouterCore. It looks the packages up the way Node and Vite do, from the real directory of
// the app.
func CheckTanStack(root string) error {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	routerDir, router, err := findPackage(root, "@tanstack/react-router")
	if err != nil {
		return err
	}
	_, core, err := findPackage(routerDir, "@tanstack/router-core")
	if err != nil {
		return err
	}
	if router == TestedReactRouter && core == TestedRouterCore {
		return nil
	}
	return fmt.Errorf(`the app installs @tanstack/react-router %s with @tanstack/router-core %s, and tifa is tested with @tanstack/react-router %s and @tanstack/router-core %s.
tifa hands page data to the browser through an option of TanStack Router that is not documented (router.options.serializationAdapters). Another version may stop reading it, and then every page with object data stays blank in the browser.
Write "@tanstack/react-router": "%s" in package.json and install again, or to build with this version anyway, set build.skip_tanstack_check: true in tifa.yaml`,
		router, core, TestedReactRouter, TestedRouterCore, TestedReactRouter)
}

// findPackage finds the package name the way Node resolves it from dir: in the node_modules of
// dir and of each folder above it. It returns the real directory of the package, with symbolic
// links resolved as pnpm makes them, and its version.
func findPackage(dir, name string) (string, string, error) {
	for {
		pkg := filepath.Join(dir, "node_modules", filepath.FromSlash(name))
		data, err := os.ReadFile(filepath.Join(pkg, "package.json"))
		if err == nil {
			var meta struct {
				Version string `json:"version"`
			}
			if err := json.Unmarshal(data, &meta); err != nil {
				return "", "", fmt.Errorf("%s: %w", filepath.Join(pkg, "package.json"), err)
			}
			real, err := filepath.EvalSymlinks(pkg)
			return real, meta.Version, err
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", fmt.Errorf("%s is not installed, run the install of your package manager in the app root", name)
		}
		dir = parent
	}
}
