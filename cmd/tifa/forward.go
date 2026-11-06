package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"

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

// forward runs go tool tifa with args in root, wired to the given streams, and returns its exit
// status. The terminal sends Ctrl-C to the whole process group, so the child gets it without
// help: this process ignores SIGINT, to stay alive until the child has stopped and to report
// its status. SIGTERM, which reaches only this process, is passed on.
func forward(root string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	cmd := exec.Command("go", append([]string{"tool", "tifa"}, args...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), forwardedEnv+"=1")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("the app in %s pins its own tifa, but starting go tool tifa failed: %w\nrun it yourself: cd %s && go tool tifa %s",
			root, err, cdOperand(root), strings.Join(args, " "))
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-sigs:
				if s == syscall.SIGTERM {
					_ = cmd.Process.Signal(s)
				}
			case <-done:
				return
			}
		}
	}()
	err := cmd.Wait()
	close(done)
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		return exit.ExitCode(), nil
	case errors.As(err, &exit):
		return 1, nil
	}
	return 1, err
}
