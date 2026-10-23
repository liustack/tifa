package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"golang.org/x/mod/modfile"

	"github.com/liustack/tifa/internal/bundle"
	"github.com/liustack/tifa/internal/config"
)

// withPnpm makes tifa new find pnpm on PATH, or not.
func withPnpm(t *testing.T, found bool) {
	old := lookPath
	lookPath = func(file string) (string, error) {
		if file == "pnpm" && found {
			return "/usr/local/bin/pnpm", nil
		}
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { lookPath = old })
}

func TestNewUsesPnpmOrNpm(t *testing.T) {
	for _, tc := range []struct {
		pnpm          bool
		install, run  string
		other, others string
	}{
		{true, "\n  pnpm install\n", "pnpm typecheck", "\n  npm install\n", "npm run"},
		{false, "\n  npm install\n", "npm run typecheck", "\n  pnpm install\n", "pnpm"},
	} {
		withPnpm(t, tc.pnpm)
		dir := filepath.Join(t.TempDir(), "app")
		out, _, err := run(t, "new", dir)
		if err != nil {
			t.Fatal(err)
		}
		readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, tc.install) || strings.Contains(out, tc.other) {
			t.Errorf("pnpm found %v: the steps do not say %q:\n%s", tc.pnpm, strings.TrimSpace(tc.install), out)
		}
		if !strings.Contains(string(readme), tc.run) || strings.Contains(string(readme), tc.others) {
			t.Errorf("pnpm found %v: README.md does not say %q:\n%s", tc.pnpm, tc.run, readme)
		}
	}
}

func TestNewWritesAnApp(t *testing.T) {
	withPnpm(t, true)
	dir := filepath.Join(t.TempDir(), "My-App")
	out, _, err := run(t, "new", dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	for _, want := range []string{"go mod tidy", "pnpm install", "go tool tifa dev", "go mod edit -replace", "http://127.0.0.1:9527"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output does not mention %q:\n%s", want, out)
		}
	}
	var files []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return err
	})
	want := []string{
		".gitignore", "AGENTS.md", "CLAUDE.md", "README.md",
		"app/about/route.tsx", "app/layout.tsx", "app/route.go", "app/route.tsx", "app/route_test.go", "app/styles.css",
		"app/todos/todo+/route.go", "app/todos/todo+/route.tsx", "app/todos/todo+/route_test.go",
		"app/todos/todo.go", "app/todos/todo_test.go",
		"db.go", "db_test.go", "go.mod", "hooks.go", "hooks_test.go", "package.json", "schema.sql",
		"tifa.yaml", "tsconfig.json", "vite.config.ts",
	}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("files %q, want %q", files, want)
	}
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	gomod := read("go.mod")
	for _, line := range []string{"module My-App\n", "\tgithub.com/liustack/tifa v0.0.0\n", "\tmodernc.org/sqlite v", "tool github.com/liustack/tifa/cmd/tifa\n", "\nignore node_modules\n"} {
		if !strings.Contains(gomod, line) {
			t.Errorf("go.mod has no line %q:\n%s", line, gomod)
		}
	}
	if !strings.Contains(read("db.go"), "package myapp\n") || !strings.Contains(read("app/todos/todo.go"), "\t\"My-App\"\n") ||
		!strings.Contains(read("app/todos/todo.go"), "myapp.DB.") {
		t.Errorf("the root package is not myapp:\n%s\n%s", read("db.go"), read("app/todos/todo.go"))
	}
	// The todos domain lives next to its pages, and the pages import it.
	if !strings.Contains(read("app/todos/todo.go"), "package todos\n") || !strings.Contains(read("app/route.go"), `"My-App/app/todos"`) {
		t.Errorf("the pages do not import the todos domain in app/todos:\n%s\n%s", read("app/todos/todo.go"), read("app/route.go"))
	}
	for _, name := range files {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		src := read(name)
		if formatted, err := format.Source([]byte(src)); err != nil || string(formatted) != src {
			t.Errorf("%s is not gofmt'ed Go: %v", name, err)
		}
	}
	if !strings.Contains(read("app/layout.tsx"), "{ title: 'My-App' }") || !strings.Contains(read("package.json"), `"name": "myapp"`) {
		t.Errorf("the page does not show the app name:\n%s", read("app/layout.tsx"))
	}
	// The database the app opens by default stays out of version control.
	for _, pattern := range []string{"*.db\n", "*.db-wal\n", "*.db-shm\n"} {
		if !strings.Contains(read(".gitignore"), pattern) {
			t.Errorf(".gitignore does not ignore %q:\n%s", pattern, read(".gitignore"))
		}
	}
}

// The new app installs the npm packages the end-to-end tests build with.
func TestNewAppUsesTheTestedPackages(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	if _, _, err := run(t, "new", dir); err != nil {
		t.Fatal(err)
	}
	type pkg struct {
		Name            string
		Dependencies    map[string]string
		DevDependencies map[string]string
	}
	read := func(name string) pkg {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var p pkg
		if err := json.Unmarshal(data, &p); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return p
	}
	got, tested := read(filepath.Join(dir, "package.json")), read(filepath.Join("..", "..", "test", "apps", "pages", "package.json"))
	if got.Name != "app" || !reflect.DeepEqual(got.Dependencies, tested.Dependencies) || !reflect.DeepEqual(got.DevDependencies, tested.DevDependencies) {
		t.Fatalf("package.json %+v, want the packages of test/apps/pages %+v", got, tested)
	}
}

func TestNewNamesTheRootPackage(t *testing.T) {
	cases := []struct{ module, pkg, title, npm string }{
		{"example.com/shop", "shop", "shop", "shop"},
		{"example.com/shop/v2", "shop", "shop", "shop"},
		{"example.com/Order_Desk", "orderdesk", "Order_Desk", "orderdesk"},
		{"example.com/_desk", "desk", "_desk", "desk"},
		{"gopkg.in/yaml.v3", "yaml", "yaml", "yaml"},
		{"example.com/app", "app", "app", "app"},
		{"example.com/atomic", "atomic", "atomic", "atomic"},
		{"desk", "desk", "desk", "desk"},
		{"example.com/fmt", "fmt", "fmt", "fmt"},
		{"example.com/std", "std", "std", "std"},
		// the name of a route package is fine: a file may import a package named like its own
		{"example.com/todo", "todo", "todo", "todo"},
		{"example.com/domain", "domain", "domain", "domain"},
		{"example.com/" + strings.Repeat("a", 300), strings.Repeat("a", 300), strings.Repeat("a", 300), strings.Repeat("a", 214)},
	}
	for _, tc := range cases {
		app, err := appNames(tc.module, false)
		if err != nil || app.Package != tc.pkg || app.Title != tc.title || app.NPM != tc.npm {
			t.Errorf("%s: %+v, %v, want package %s, title %s and npm name %s", tc.module, app, err, tc.pkg, tc.title, tc.npm)
		}
	}
	for _, bad := range []string{
		"example.com/123", "example.com/main", "example.com/Main", "example.com/type", "example.com/a b", "example.com/-", "",
		// invalid major version suffixes
		"example.com/shop/v1", "example.com/shop/v0", "example.com/shop/v02", "gopkg.in/desk",
		// paths the standard library or the go command keeps, in any case
		"fmt", "net/http", "uuid", "time/clock", "Fmt", "Net/http", "UUID", "std", "all", "tool", "work", "cmd",
		// a vendor element makes the go command refuse to import the path
		"example.com/vendor", "example.com/vendor/desk", "vendor/desk",
	} {
		if _, err := appNames(bad, false); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// The Go files under app/ import the root package by its own name, as Go code usually does.
func TestNewAppImportsTheRootPackageByItsName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	if _, _, err := run(t, "new", dir, "--module", "example.com/myapp"); err != nil {
		t.Fatal(err)
	}
	var checked []string
	err := filepath.WalkDir(filepath.Join(dir, "app"), func(name string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(name, ".go") {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, name)
		rel = filepath.ToSlash(rel)
		for _, spec := range file.Imports {
			root := spec.Path.Value == `"example.com/myapp"`
			if root && spec.Name != nil {
				t.Errorf("%s imports the root package as %s", rel, spec.Name.Name)
			}
			if root {
				checked = append(checked, rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Only the todos domain needs DB. The pages import the domain, and tests open a database.
	want := []string{"app/route_test.go", "app/todos/todo+/route_test.go", "app/todos/todo.go", "app/todos/todo_test.go"}
	if !reflect.DeepEqual(checked, want) {
		t.Fatalf("the root package is imported by %q, want %q", checked, want)
	}
}

// A root package named like something the Go files under app/ use next to it would not compile,
// so tifa new refuses it and says what it clashes with.
func TestNewRefusesARootPackageThatClashes(t *testing.T) {
	for module, clash := range map[string]string{
		"example.com/todos":   "the package app/todos",
		"example.com/Todos":   "the package app/todos",
		"example.com/tifa":    "the package github.com/liustack/tifa",
		"example.com/Context": "the package context",
		"example.com/error":   "the predeclared name error",
		"example.com/db":      "app/route_test.go",
	} {
		_, err := appNames(module, false)
		if err == nil || !strings.Contains(err.Error(), clash) || !strings.Contains(err.Error(), "--module") {
			t.Errorf("%s: %v, want an error naming %q and --module", module, err, clash)
		}
	}
	// The name comes from the folder unless --module gives it, and the error says which to change.
	dir := filepath.Join(t.TempDir(), "todos")
	if _, _, err := run(t, "new", dir); err == nil || !strings.Contains(err.Error(), "app/todos") || !strings.Contains(err.Error(), "name the folder differently") {
		t.Errorf("tifa new todos: %v, want an error about app/todos that asks for another folder name", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("tifa new todos wrote %s: %v", dir, err)
	}
	dir = filepath.Join(t.TempDir(), "desk")
	_, _, err := run(t, "new", dir, "--module", "example.com/todos")
	if err == nil || !strings.Contains(err.Error(), "change the last element of --module") || strings.Contains(err.Error(), "folder") {
		t.Errorf("tifa new --module example.com/todos: %v, want an error that asks for another last element of --module only", err)
	}
}

// A package named init cannot be imported by its name, since init may only be a function.
func TestNewRefusesInit(t *testing.T) {
	for _, module := range []string{"init", "example.com/init", "example.com/Init"} {
		if _, err := appNames(module, false); err == nil || !strings.Contains(err.Error(), "init") {
			t.Errorf("%s: %v, want an error about init", module, err)
		}
	}
}

// TestRootNameClashesMatchTheTemplate generates apps with root packages of many names and builds
// them: each name of rootNameClashes, and init, must break the build, and every other name the
// Go files that import the root package use, plus some common ones, must build, vet and be taken
// by tifa new. It runs the go command for each name, so it runs with the end-to-end tests.
func TestRootNameClashesMatchTheTemplate(t *testing.T) {
	if os.Getenv("TIFA_E2E") == "" {
		t.Skip("set TIFA_E2E=1 to build an app per root package name")
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goCmd := func(t *testing.T, dir string, args ...string) error {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return nil
	}
	app := func(module, pkg string) newApp {
		return newApp{Module: module, Package: pkg, Title: pkg, NPM: pkg, Go: "1.27.0", Version: "v0.0.0", Install: "npm install", Run: "npm run", ReactRouter: bundle.TestedReactRouter}
	}
	// Tidy one app, and give each app below its go.mod and go.sum, which do not depend on the name.
	base := filepath.Join(t.TempDir(), "base")
	if err := writeApp(base, app("example.com/base", "base")); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"mod", "edit", "-replace", tifaModule + "=" + repo}, {"mod", "tidy"}} {
		if err := goCmd(t, base, args...); err != nil {
			t.Fatal(err)
		}
	}
	gomod, err := os.ReadFile(filepath.Join(base, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	gosum, err := os.ReadFile(filepath.Join(base, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	build := func(t *testing.T, a newApp) error {
		dir := filepath.Join(t.TempDir(), "app")
		if err := writeApp(dir, a); err != nil {
			t.Fatal(err)
		}
		mod := strings.Replace(string(gomod), "module example.com/base\n", "module "+a.Module+"\n", 1)
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.sum"), gosum, 0o644); err != nil {
			t.Fatal(err)
		}
		// vet type checks the tests too, build checks the rest as the app is shipped.
		if err := goCmd(t, dir, "vet", "./..."); err != nil {
			return err
		}
		return goCmd(t, dir, "build", "./...")
	}
	for _, name := range candidateRootNames(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			module := "example.com/" + name
			_, clashes := rootNameClashes[name]
			if clashes || name == "init" {
				if _, err := appNames(module, false); err == nil {
					t.Errorf("tifa new takes %s", module)
				}
				if err := build(t, app(module, name)); err == nil {
					t.Errorf("a root package named %s builds, so it does not clash", name)
				}
				return
			}
			a, err := appNames(module, false)
			if err != nil {
				t.Fatalf("tifa new refuses %s: %v", module, err)
			}
			if err := build(t, a); err != nil {
				t.Errorf("a root package named %s does not build, add it to rootNameClashes: %v", name, err)
			}
		})
	}
}

// candidateRootNames returns the names a root package could clash with: every name a Go file of
// the template that imports the root package uses, with the names of its imports, and common
// names besides. Only names tifa new can derive from a module path are kept.
func candidateRootNames(t *testing.T) []string {
	const module = "example.com/tifanewroot"
	names := map[string]bool{"init": true}
	for _, name := range strings.Fields("c r s app todo shop desk order orders store money domain root false iota unsafe sqlite atomic") {
		names[name] = true
	}
	for name := range rootNameClashes {
		names[name] = true
	}
	err := fs.WalkDir(newapp, "newapp", func(name string, d fs.DirEntry, err error) error {
		if err != nil || !strings.HasSuffix(name, ".go.tmpl") {
			return err
		}
		src, err := renderFile(newapp, name, newApp{Module: module, Package: "tifanewroot"})
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(file.Imports, func(spec *ast.ImportSpec) bool { return spec.Path.Value == strconv.Quote(module) }) {
			return nil
		}
		for _, spec := range file.Imports {
			names[path.Base(strings.Trim(spec.Path.Value, `"`))] = true
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				names[id.Name] = true
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var list []string
	for name := range names {
		if regexp.MustCompile(`^[a-z][a-z0-9]*$`).MatchString(name) && !token.IsKeyword(name) && name != "main" && name != "tifanewroot" {
			list = append(list, name)
		}
	}
	slices.Sort(list)
	return list
}

func TestNewQuotesTheFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my app")
	out, _, err := run(t, "new", dir, "--module", "example.com/desk")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "cd '"+dir+"'\n") {
		t.Fatalf("the cd step does not quote %q:\n%s", dir, out)
	}
	if got := cdOperand("it's"); got != `'it'\''s'` {
		t.Fatalf("cdOperand: %s", got)
	}
	for dir, want := range map[string]string{"plain/dir-1.x": "plain/dir-1.x", "-P": "./-P", "-": "./-", "-a b": "'./-a b'", "/abs/-P": "/abs/-P"} {
		if got := cdOperand(dir); got != want {
			t.Errorf("cdOperand(%q) = %s, want %s", dir, got, want)
		}
	}
}

func TestNewTellsADevelopmentBuild(t *testing.T) {
	versioned := &debug.BuildInfo{Main: debug.Module{Path: tifaModule, Version: "v0.3.1", Sum: "h1:abc="}}
	cases := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{"go run @version", versioned, "v0.3.1"},
		{"go build in a checkout", &debug.BuildInfo{Main: debug.Module{Path: tifaModule, Version: "v0.0.0-20261014151129-dc5832c595aa"}}, ""},
		{"go build with changes", &debug.BuildInfo{Main: debug.Module{Path: tifaModule, Version: "v0.0.0-20261014151129-dc5832c595aa+dirty"}}, ""},
		{"go run in a checkout", &debug.BuildInfo{Main: debug.Module{Path: tifaModule, Version: "(devel)"}}, ""},
		{"replaced tool", &debug.BuildInfo{Main: debug.Module{Path: tifaModule, Version: "(devel)", Replace: &debug.Module{Path: "/src/tifa", Version: "(devel)"}}}, ""},
		{"replaced by another version", &debug.BuildInfo{Main: debug.Module{Path: tifaModule, Version: "v0.3.1", Sum: "h1:abc=", Replace: &debug.Module{Path: "example.com/fork", Version: "v0.3.2", Sum: "h1:def="}}}, ""},
		{"test binary", &debug.BuildInfo{Main: debug.Module{Path: "github.com/liustack/tifa/cmd/tifa.test"}}, ""},
	}
	for _, tc := range cases {
		if got := releaseVersion(tc.info); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	dir := filepath.Join(t.TempDir(), "app")
	out, _, err := run(t, "new", dir)
	if err != nil {
		t.Fatal(err)
	}
	// The replace step edits the go.mod of the new app, so it comes after cd.
	cd, replace, tidy := strings.Index(out, "\n  cd "), strings.Index(out, "\n  go mod edit -replace "+tifaModule+"="), strings.Index(out, "\n  go mod tidy")
	if cd < 0 || replace < cd || tidy < replace {
		t.Fatalf("the steps are not cd, replace, tidy:\n%s", out)
	}
}

func TestNewRefusesAFolderWithFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, "new", dir, "--module", "example.com/notes"); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("new into a folder with files: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("new wrote into a folder with files: %v", entries)
	}
	empty := t.TempDir()
	if _, _, err := run(t, "new", empty, "--module", "example.com/empty"); err != nil {
		t.Fatalf("new into an empty folder: %v", err)
	}
}

func TestStdRootsListTheStandardLibrary(t *testing.T) {
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(strings.TrimSpace(string(out)), "src"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != "testdata" && !slices.Contains(stdRoots, e.Name()) {
			t.Errorf("stdRoots lacks %s of the standard library", e.Name())
		}
	}
}

// modernc.org/sqlite and the slowjs of tifa both run on modernc.org/libc, and each is built for
// the exact libc version its go.mod requires. Go picks the highest version any module requires,
// so the app builds with that libc only when the SQLite release go.mod.tmpl pins and the slowjs
// tifa requires both require the libc tifa builds with.
func TestNewPinsASQLiteBuiltForTheLibcOfTifa(t *testing.T) {
	tmpl, err := os.ReadFile(filepath.Join("newapp", "go.mod.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^\s*modernc\.org/sqlite (v\S+)$`).FindSubmatch(tmpl)
	if m == nil {
		t.Fatalf("go.mod.tmpl requires no modernc.org/sqlite:\n%s", tmpl)
	}
	sqlite := "modernc.org/sqlite@" + string(m[1])
	goCmd := func(args ...string) []byte {
		t.Helper()
		out, err := exec.Command("go", args...).Output()
		if err != nil {
			t.Fatalf("go %s: %v", strings.Join(args, " "), err)
		}
		return out
	}
	libc := strings.TrimSpace(string(goCmd("list", "-m", "-f", "{{.Version}}", "modernc.org/libc")))
	engine := strings.TrimSpace(string(goCmd("list", "-m", "-f", "{{.GoMod}}", "github.com/liustack/slowjs")))
	// The SQLite module is not a dependency of tifa, so it may have to be downloaded.
	out, err := exec.Command("go", "mod", "download", "-json", sqlite).Output()
	if err != nil {
		t.Skipf("go mod download %s: %v, the module proxy may be out of reach", sqlite, err)
	}
	var download struct{ GoMod string }
	if err := json.Unmarshal(out, &download); err != nil {
		t.Fatal(err)
	}
	for _, gomod := range []string{engine, download.GoMod} {
		data, err := os.ReadFile(gomod)
		if err != nil {
			t.Fatal(err)
		}
		f, err := modfile.ParseLax(gomod, data, nil)
		if err != nil {
			t.Fatal(err)
		}
		required := "none"
		for _, r := range f.Require {
			if r.Mod.Path == "modernc.org/libc" {
				required = r.Mod.Version
			}
		}
		if required != libc {
			t.Errorf("%s requires modernc.org/libc %s, tifa builds with %s: pin a modernc.org/sqlite in go.mod.tmpl built for the same libc as the slowjs of tifa", f.Module.Mod, required, libc)
		}
	}
}

// tifa.yaml lists every setting commented out, so as written it loads the defaults. Turned on, it
// loads them too, except ssr.vms, whose default depends on the machine and which shows an example.
func TestNewTifaYAMLListsEverySetting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	if _, _, err := run(t, "new", dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "tifa.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c, err := config.Load(pflag.NewFlagSet("serve", pflag.ContinueOnError), nil, "tifa.yaml", data); err != nil || !reflect.DeepEqual(c, config.Default()) {
		t.Fatalf("tifa.yaml as written: %+v, %v, want the defaults", c, err)
	}
	setting := regexp.MustCompile(`^# ( *)([a-z0-9_]+):`)
	var lines, keys []string
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		if m := setting.FindStringSubmatch(line); m != nil {
			line = strings.TrimPrefix(line, "# ")
			if m[1] == "" {
				section = m[2]
			} else {
				keys = append(keys, section+"."+m[2])
			}
		}
		lines = append(lines, line)
	}
	if !reflect.DeepEqual(keys, config.Keys()) {
		t.Fatalf("tifa.yaml lists %q, want %q", keys, config.Keys())
	}
	c, err := config.Load(pflag.NewFlagSet("serve", pflag.ContinueOnError), nil, "tifa.yaml", []byte(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatalf("tifa.yaml with the settings turned on: %v", err)
	}
	// ssr.vms defaults to GOMAXPROCS, so the file shows an example.
	want := config.Default()
	want.SSR.VMs = c.SSR.VMs
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("tifa.yaml with the settings turned on: %+v, want the defaults %+v", c, want)
	}
}

// AGENTS.md guides coding agents and links to the docs of tifa, and CLAUDE.md points at it.
func TestNewWritesAgentNotes(t *testing.T) {
	withPnpm(t, false)
	dir := filepath.Join(t.TempDir(), "app")
	if _, _, err := run(t, "new", dir); err != nil {
		t.Fatal(err)
	}
	claude, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil || string(claude) != "@AGENTS.md\n" {
		t.Fatalf("CLAUDE.md: %q, %v", claude, err)
	}
	agents, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agents), "npm run typecheck") {
		t.Errorf("AGENTS.md does not use the commands of npm:\n%s", agents)
	}
	links := regexp.MustCompile(`https://github\.com/liustack/tifa/blob/main/([^)#\s]+)`).FindAllStringSubmatch(string(agents), -1)
	if len(links) == 0 {
		t.Fatal("AGENTS.md links to no docs")
	}
	for _, link := range links {
		if _, err := os.Stat(filepath.Join("..", "..", filepath.FromSlash(link[1]))); err != nil {
			t.Errorf("AGENTS.md links to %s: %v", link[0], err)
		}
	}
}
