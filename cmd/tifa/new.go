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

	"github.com/liustack/tifa/internal/bundle"
	"github.com/liustack/tifa/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/mod/module"
)

// newapp holds the files of a new app. Each ends in .tmpl, so the go command neither builds the
// Go files nor takes go.mod for a module. gitignore.tmpl becomes .gitignore,
// vscode-settings.json.tmpl becomes .vscode/settings.json and github-ci.yml.tmpl becomes
// .github/workflows/ci.yml.
//
//go:embed newapp
var newapp embed.FS

const tifaModule = "github.com/liustack/tifa"

type newApp struct {
	Module  string // the module path in go.mod
	Package string // the name of the root package, which the Go files under app/ import
	Title   string // the last element of the module path, the page title
	NPM     string // the name in package.json
	Go      string // the go line of go.mod
	Version string // the tifa version in go.mod
	Install string // the command that installs the npm packages: pnpm install or npm install
	Run     string // the command that runs a package.json script: pnpm or npm run
	// ReactRouter is the version of @tanstack/react-router in package.json, the one tifa is
	// tested with.
	ReactRouter string
	// Written says which tifa wrote the notes and the skill: Tifa v0.0.5, or a development build of Tifa.
	Written string
	// Empty is set by --empty: the skeleton without the todo list.
	Empty bool
	// Node is set when the app has a package.json, which the CI workflow installs from.
	Node bool
	// PnpmVersion is the major version pnpm/action-setup gets when package.json has no
	// packageManager that names pnpm. Empty when it does.
	PnpmVersion string
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
	var empty bool
	cmd := &cobra.Command{
		Use:   "new <dir>",
		Short: "Create a new application in an empty directory",
		Long: `new writes a small working app into dir: a todo list stored in SQLite, with the hooks and
the database in the root package, the todos domain next to its pages under app/, tests,
tifa.yaml, notes for coding agents (AGENTS.md) and the Tifa skill in
.claude/skills/tifa, the same files tifa agents writes. It runs nothing and needs no
network. The next steps it prints download the Go and npm dependencies, with pnpm when it is on
PATH and npm otherwise.

With --empty the app is a skeleton with no todo list: the same root files and the sessions
table, a root Layout and a home page of one line.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			fromFolder := modulePath == ""
			if fromFolder {
				abs, err := filepath.Abs(dir)
				if err != nil {
					return err
				}
				modulePath = filepath.Base(abs)
			}
			app, err := appNames(modulePath, fromFolder)
			if err != nil {
				return err
			}
			app.Empty = empty
			app.Node, app.PnpmVersion = true, defaultPnpmVersion // package.json of a new app has no packageManager
			app.Install, app.Run = packageManager()
			if app.Go, err = goDirective(runtime.Version()); err != nil {
				return err
			}
			info, _ := debug.ReadBuildInfo()
			if app.Version = releaseVersion(info); app.Version == "" {
				app.Version = "v0.0.0"
			}
			app.Written = writtenBy(info)
			if err := writeApp(dir, app); err != nil {
				return err
			}
			if _, err := syncAgents(dir, app); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "created %s in %s\n", app.Module, dir)
			fmt.Fprintf(out, "wrote AGENTS.md and the Tifa skill in %s for coding agents\n", defaultSkillsDir)
			if app.Version == "v0.0.0" {
				fmt.Fprintf(out, "\nThis tifa is not a released version, so go.mod requires %s v0.0.0: the replace step below points it at your tifa checkout.\n", tifaModule)
			}
			fmt.Fprintf(out, "\nNext:\n  cd %s\n", cdOperand(dir))
			if app.Version == "v0.0.0" {
				fmt.Fprintf(out, "  go mod edit -replace %s=<path to your tifa checkout>\n", tifaModule)
			}
			fmt.Fprintf(out, "  go mod tidy\n  %s\n  go tool tifa dev     # http://%s\n", app.Install, config.DefaultAddr)
			return nil
		},
	}
	cmd.Flags().StringVar(&modulePath, "module", "", "module path of the app (default: the name of dir)")
	cmd.Flags().BoolVar(&empty, "empty", false, "write a skeleton without the todo list")
	return cmd
}

// appNames derives the names of the app from its module path. The root package is the last
// element without the major version suffix, lowercased, with its letters and digits only, so
// example.com/Order-Desk/v2 is the package orderdesk. That is a valid npm name as well.
// fromFolder tells whether the module path is the name of the folder, with no --module, so an
// error asks for what the user can change.
func appNames(modulePath string, fromFolder bool) (newApp, error) {
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
	title, pkg := deriveNames(prefix)
	// The Go files under app/ import the root package by its name, and Go does not take init as
	// the name of an imported package.
	if !token.IsIdentifier(pkg) || token.IsKeyword(pkg) || pkg == "main" || pkg == "init" {
		return newApp{}, fmt.Errorf("the module path %s ends in %q, which makes the package name %q: a package name must start with a letter and may not be a keyword, main or init, so pass --module with another last element", modulePath, title, pkg)
	}
	if clash, ok := rootNameClashes[pkg]; ok {
		fix := "change the last element of --module"
		if fromFolder {
			fix = "name the folder differently or pass --module with another last element"
		}
		return newApp{}, fmt.Errorf("the module path %s makes the root package %s, and the Go files under app/ that import it already use %s for %s: %s", modulePath, pkg, pkg, clash, fix)
	}
	npm := pkg
	if len(npm) > 214 { // the longest npm name
		npm = npm[:214]
	}
	return newApp{Module: modulePath, Package: pkg, Title: title, NPM: npm, ReactRouter: bundle.TestedReactRouter}, nil
}

// deriveNames returns the title (the last element of the module path without its major version
// suffix) and the root package name made from it: lowercased, with its letters and digits only.
func deriveNames(prefix string) (title, pkg string) {
	title = path.Base(prefix)
	pkg = strings.Map(func(r rune) rune {
		if 'a' <= r && r <= 'z' || '0' <= r && r <= '9' {
			return r
		}
		return -1
	}, strings.ToLower(title))
	return title, pkg
}

// rootNameClashes lists the names a root package may not have, each with what already uses it.
// The Go files under app/ that import the root package refer to it by its name, so that name
// must not mean anything else where they use it: a package they import, a predeclared name they
// use, or a parameter or variable in whose scope they use the root package.
// TestRootNameClashesMatchTheTemplate checks that every name here breaks the build of a new app,
// and that the other names in those files do not.
var rootNameClashes = map[string]string{
	// imported packages
	"todos":    "the package app/todos",
	"tifa":     "the package github.com/liustack/tifa",
	"tifatest": "the package github.com/liustack/tifa/tifatest",
	"context":  "the package context",
	"sql":      "the package database/sql",
	"errors":   "the package errors",
	"strings":  "the package strings",
	"filepath": "the package path/filepath",
	"httptest": "the package net/http/httptest",
	"reflect":  "the package reflect",
	"testing":  "the package testing",
	// predeclared names
	"append": "the predeclared name append",
	"bool":   "the predeclared name bool",
	"error":  "the predeclared name error",
	"int64":  "the predeclared name int64",
	"len":    "the predeclared name len",
	"nil":    "the predeclared name nil",
	"string": "the predeclared name string",
	"true":   "the predeclared name true",
	// parameters and variables in whose scope the root package is used
	"ctx":   "a parameter of the functions in app/todos/todo.go",
	"id":    "a parameter of the functions in app/todos/todo.go",
	"title": "a parameter of the functions in app/todos/todo.go",
	"done":  "a parameter of the functions in app/todos/todo.go",
	"t":     "a parameter of useTestDB in app/route_test.go and the tests under app/todos",
	"db":    "a variable of useTestDB in app/route_test.go and the tests under app/todos",
	"err":   "a variable of useTestDB in app/route_test.go and the tests under app/todos",
}

// importProblem tells why the go command would not import the package at the root of a module
// with this path and what to pass instead, or returns "". The generated main imports it once the
// app has hooks.go, and the todos domain imports it for the database from the start.
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
		out, err := renderFile(newapp, name, app)
		if err != nil {
			return err
		}
		rel := strings.TrimSuffix(strings.TrimPrefix(name, "newapp/"), ".tmpl")
		if app.Empty && todoFile(rel) {
			return nil
		}
		// A folder or file whose name starts with a dot is left out of an embedded folder.
		switch rel {
		case "gitignore":
			rel = ".gitignore"
		case "vscode-settings.json":
			rel = ".vscode/settings.json"
		case "github-ci.yml":
			rel = ciPath
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, out, 0o644)
	})
}

// todoFile reports whether rel, a path of newapp without .tmpl, belongs to the todo list that an
// app made with --empty leaves out.
func todoFile(rel string) bool {
	switch rel {
	case "app/about/page.tsx", "app/route.go", "app/route_test.go":
		return true
	}
	return strings.HasPrefix(rel, "app/todos/")
}

// renderFile runs the template name of files with app.
func renderFile(files fs.FS, name string, app newApp) ([]byte, error) {
	text, err := fs.ReadFile(files, name)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New(name).Option("missingkey=error").Parse(string(text))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, app); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// goDirective gives the version for the go line of go.mod of the Go toolchain that built tifa,
// such as 1.27.0 for go1.27.2. A toolchain built from the Go tip reports devel go1.27-<commit>
// <date> or go1.27-devel_<commit> <date>, which stands for the language version of the release in
// the making, 1.27. It has no patch version yet, so the line is 1.27.
func goDirective(runtimeVersion string) (string, error) {
	fields := strings.Fields(runtimeVersion)
	devel := len(fields) > 1 && fields[0] == "devel"
	if devel {
		fields = fields[1:]
	}
	if len(fields) > 0 {
		name, suffix, _ := strings.Cut(fields[0], "-")
		devel = devel || strings.HasPrefix(suffix, "devel")
		if lang := version.Lang(name); lang != "" {
			if devel {
				return strings.TrimPrefix(lang, "go"), nil
			}
			return strings.TrimPrefix(lang, "go") + ".0", nil
		}
	}
	return "", fmt.Errorf("tifa new cannot tell the Go language version of %q, the Go this tifa was built with", runtimeVersion)
}
