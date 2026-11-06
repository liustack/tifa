package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"golang.org/x/mod/modfile"
)

// forwardedEnv is set on the command that a forward starts, so that it never forwards again.
const forwardedEnv = "TIFA_FORWARDED"

const toolPackage = tifaModule + "/cmd/tifa"

// forwardTarget says whether a tifa started with args in cwd hands the command to the tifa that
// the app pins, and returns the root of the app (the folder of its go.mod) when it does.
//
// The app is the folder of --dir, else cwd, or the nearest folder above it with a go.mod. tifa
// forwards when that module requires github.com/liustack/tifa and has the tool line for the
// command, unless the marker is set, the command is new, or this binary already is the version
// the app pins: info names a release of the module, equal to the required version, and go.mod has
// no replace for the module. A go.work file in use (GOWORK, or one above the app) can change which
// tifa the app gets without touching go.mod, so then it always forwards, and go tool follows the
// workspace.
func forwardTarget(args []string, cwd string, info *debug.BuildInfo, getenv func(string) string) (root string, ok bool) {
	if getenv(forwardedEnv) != "" || len(args) > 0 && args[0] == "new" {
		return "", false
	}
	dir := cwd
	if d, found := dirFlag(args); found {
		dir = d
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}
	}
	root = findModRoot(dir)
	if root == "" {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", false
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return "", false
	}
	required, hasTool, replaced := "", false, false
	for _, r := range f.Require {
		if r.Mod.Path == tifaModule {
			required = r.Mod.Version
		}
	}
	for _, t := range f.Tool {
		hasTool = hasTool || t.Path == toolPackage
	}
	for _, r := range f.Replace {
		replaced = replaced || r.Old.Path == tifaModule
	}
	if required == "" || !hasTool {
		return "", false
	}
	if !replaced && releaseVersion(info) == required && !workspaceInUse(root, getenv) {
		return "", false
	}
	return root, true
}

// runByGoCommand reports whether exe, the path of this program, is in the build cache of the go
// command, where go tool and go run keep the programs they build. Such a tifa is the one the app
// pins already, so it never forwards: without this check, go tool tifa in an app that replaces
// tifa would start a second go tool tifa. A GOCACHE set only with go env -w is not seen, and such
// a tifa forwards once.
func runByGoCommand(exe string, getenv func(string) string) bool {
	cache := getenv("GOCACHE")
	if cache == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return false
		}
		cache = filepath.Join(dir, "go-build")
	}
	return strings.HasPrefix(exe, cache+string(filepath.Separator))
}

// dirFlag finds --dir in args: "--dir x" or "--dir=x", before any "--".
func dirFlag(args []string) (string, bool) {
	for i, a := range args {
		switch {
		case a == "--":
			return "", false
		case a == "--dir" && i+1 < len(args):
			return args[i+1], true
		case strings.HasPrefix(a, "--dir="):
			return strings.TrimPrefix(a, "--dir="), true
		}
	}
	return "", false
}

// findModRoot returns the nearest folder from dir upward that holds a go.mod, or "".
func findModRoot(dir string) string {
	for {
		if st, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !st.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// workspaceInUse reports whether the go command would use a go.work file for the app at root.
func workspaceInUse(root string, getenv func(string) string) bool {
	if w := getenv("GOWORK"); w != "" {
		return w != "off"
	}
	for dir := root; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return true
		}
		if filepath.Dir(dir) == dir {
			return false
		}
	}
}

// forwardFailed is the error when go tool tifa cannot start for the app at root.
func forwardFailed(root string, args []string, err error) error {
	return fmt.Errorf("the app in %s pins its own tifa, but starting go tool tifa failed: %w\nrun it yourself: cd %s && go tool tifa %s",
		root, err, cdOperand(root), strings.Join(args, " "))
}
