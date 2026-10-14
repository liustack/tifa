package main

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"go/token"
	"go/version"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	"golang.org/x/mod/module"
)

// newapp holds the files of a new app. Each ends in .tmpl, so the go command neither builds the
// Go files nor takes go.mod for a module, and gitignore.tmpl becomes .gitignore.
//
//go:embed newapp
var newapp embed.FS

const tifaModule = "github.com/liustack/tifa"

type newApp struct {
	Module  string // the module path in go.mod
	Package string // the name of the root package. The pages import it as domain
	Title   string // the last element of the module path, the page title
	NPM     string // the name in package.json
	Go      string // the go line of go.mod
	Version string // the tifa version in go.mod
	Install string // the command that installs the npm packages: pnpm install or npm install
	Run     string // the command that runs a package.json script: pnpm or npm run
}

// lookPath finds a program on PATH. Tests replace it.
var lookPath = exec.LookPath

// packageManager returns the commands of pnpm when it is on PATH, and of npm otherwise.
func packageManager() (install, run string) {
	if _, err := lookPath("pnpm"); err == nil {
		return "pnpm install", "pnpm"
	}
	return "npm install", "npm run"
}

func newNewCmd() *cobra.Command {
	var modulePath string
	cmd := &cobra.Command{
		Use:   "new <dir>",
		Short: "Create a new application in an empty directory",
		Long: `new writes a small working app into dir: a todo list stored in SQLite, with a root package,
pages under app/, tests, tifa.yaml and notes for coding agents. It runs nothing and needs no
network. The next steps it prints download the Go and npm dependencies, with pnpm when it is on
PATH and npm otherwise.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			if modulePath == "" {
				abs, err := filepath.Abs(dir)
				if err != nil {
					return err
				}
				modulePath = filepath.Base(abs)
			}
			app, err := appNames(modulePath)
			if err != nil {
				return err
			}
			app.Install, app.Run = packageManager()
			app.Go = strings.TrimPrefix(version.Lang(runtime.Version()), "go") + ".0"
			info, _ := debug.ReadBuildInfo()
			if app.Version = releaseVersion(info); app.Version == "" {
				app.Version = "v0.0.0"
			}
			if err := writeApp(dir, app); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "created %s in %s\n", app.Module, dir)
			if app.Version == "v0.0.0" {
				fmt.Fprintf(out, "\nThis tifa is not a released version, so go.mod requires %s v0.0.0: the replace step below points it at your tifa checkout.\n", tifaModule)
			}
			fmt.Fprintf(out, "\nNext:\n  cd %s\n", cdOperand(dir))
			if app.Version == "v0.0.0" {
				fmt.Fprintf(out, "  go mod edit -replace %s=<path to your tifa checkout>\n", tifaModule)
			}
			fmt.Fprintf(out, "  go mod tidy\n  %s\n  go tool tifa dev     # http://127.0.0.1:3000\n", app.Install)
			return nil
		},
	}
	cmd.Flags().StringVar(&modulePath, "module", "", "module path of the app (default: the name of dir)")
	return cmd
}

// appNames derives the names of the app from its module path. The root package is the last
// element without the major version suffix, lowercased, with its letters and digits only, so
// example.com/Order-Desk/v2 is the package orderdesk. That is a valid npm name as well.
func appNames(modulePath string) (newApp, error) {
	if err := module.CheckImportPath(modulePath); err != nil {
		return newApp{}, fmt.Errorf("%q is not a module path, pass one with --module: %w", modulePath, err)
	}
	prefix, _, ok := module.SplitPathVersion(modulePath)
	if !ok {
		return newApp{}, fmt.Errorf("%s ends in an invalid major version suffix: a module path ends in /v2 or a later major version, or in .vN for gopkg.in", modulePath)
	}
	if problem := importProblem(modulePath); problem != "" {
		return newApp{}, fmt.Errorf("tifa new does not take the module path %s: %s", modulePath, problem)
	}
	title := path.Base(prefix)
	pkg := strings.Map(func(r rune) rune {
		if 'a' <= r && r <= 'z' || '0' <= r && r <= '9' {
			return r
		}
		return -1
	}, strings.ToLower(title))
	// The pages import the root package as domain, so any name a package may have works.
	if !token.IsIdentifier(pkg) || token.IsKeyword(pkg) || pkg == "main" {
		return newApp{}, fmt.Errorf("the module path %s ends in %q, which makes the package name %q: a package name must start with a letter and may not be a keyword or main, so pass --module with another last element", modulePath, title, pkg)
	}
	npm := pkg
	if len(npm) > 214 { // the longest npm name
		npm = npm[:214]
	}
	return newApp{Module: modulePath, Package: pkg, Title: title, NPM: npm}, nil
}

// importProblem tells why the go command would not import the package at the root of a module
// with this path and what to pass instead, or returns "". The generated main imports it once the app has hooks.go, and
// the home page imports it from the start.
func importProblem(modulePath string) string {
	elems := strings.Split(modulePath, "/")
	if slices.ContainsFunc(elems, func(e string) bool { return strings.EqualFold(e, "vendor") }) {
		return "the go command refuses import paths with a vendor element, so pass --module with a path without one"
	}
	if first := elems[0]; !strings.Contains(first, ".") && slices.ContainsFunc(stdRoots, func(r string) bool { return strings.EqualFold(r, first) }) {
		return fmt.Sprintf("a path that starts with %s, in any case, may name a package of the standard library or a package pattern of the go command, so pass --module with a path that starts with a domain, such as example.com/%s", first, modulePath)
	}
	return ""
}

// stdRoots are the first elements of the standard library import paths, from the src folder of
// Go 1.27, and the package patterns of the go command. The list is fixed because a tifa built with
// -trimpath does not know where the standard library is.
var stdRoots = strings.Fields(`all std tool work
	archive arena bufio builtin bytes cmd cmp compress container context crypto database debug
	embed encoding errors expvar flag fmt go hash html image index internal io iter log maps math
	mime net os path plugin reflect regexp runtime simd slices sort strconv strings structs sync
	syscall testing text time unicode unique unsafe uuid vendor weak`)

// releaseVersion returns the version of the tifa module this command was built from when the
// go command downloaded it, as with go run or go install of github.com/liustack/tifa/cmd/tifa@v,
// or the tool of an app that requires that version. A command built in a checkout, or from a
// replaced module, gets "": its version names no module the new app can download.
func releaseVersion(info *debug.BuildInfo) string {
	if info == nil || info.Main.Path != tifaModule || info.Main.Replace != nil || info.Main.Sum == "" {
		return ""
	}
	return info.Main.Version
}

// cdOperand writes dir as the operand of cd in a POSIX shell: quoted when it holds more than
// letters, digits and / . _ -, and after ./ when it starts with -, which cd would take for an
// option.
func cdOperand(dir string) string {
	if strings.HasPrefix(dir, "-") {
		dir = "./" + dir
	}
	if strings.IndexFunc(dir, func(r rune) bool {
		return !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || strings.ContainsRune("/._-", r))
	}) < 0 {
		return dir
	}
	return "'" + strings.ReplaceAll(dir, "'", `'\''`) + "'"
}

// writeApp writes the files of app into dir, which must be missing or empty.
func writeApp(dir string, app newApp) error {
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case len(entries) > 0:
		return fmt.Errorf("%s is not empty: tifa new only writes into a new or empty directory", dir)
	}
	return fs.WalkDir(newapp, "newapp", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		text, err := fs.ReadFile(newapp, name)
		if err != nil {
			return err
		}
		tmpl, err := template.New(name).Option("missingkey=error").Parse(string(text))
		if err != nil {
			return err
		}
		var out bytes.Buffer
		if err := tmpl.Execute(&out, app); err != nil {
			return err
		}
		rel := strings.TrimSuffix(strings.TrimPrefix(name, "newapp/"), ".tmpl")
		if rel == "gitignore" {
			rel = ".gitignore"
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, out.Bytes(), 0o644)
	})
}
