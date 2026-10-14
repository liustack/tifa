package main

import (
	"encoding/json"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
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
	for _, want := range []string{"go mod tidy", "pnpm install", "go tool tifa dev", "go mod edit -replace", "http://127.0.0.1:3000"} {
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
		".gitignore", "README.md",
		"app/about/route.tsx", "app/layout.tsx", "app/route.go", "app/route.tsx", "app/route_test.go", "app/styles.css",
		"app/todos/todo+/route.go", "app/todos/todo+/route.tsx", "app/todos/todo+/route_test.go",
		"db.go", "db_test.go", "go.mod", "hooks.go", "hooks_test.go", "package.json", "schema.sql",
		"todo.go", "todo_test.go", "tsconfig.json",
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
	for _, line := range []string{"module My-App\n", "\tgithub.com/liustack/tifa v0.0.0\n", "\tmodernc.org/sqlite v", "tool github.com/liustack/tifa/cmd/tifa\n"} {
		if !strings.Contains(gomod, line) {
			t.Errorf("go.mod has no line %q:\n%s", line, gomod)
		}
	}
	if !strings.Contains(read("todo.go"), "package myapp\n") || !strings.Contains(read("app/route.go"), `domain "My-App"`) {
		t.Errorf("the root package is not myapp:\n%s\n%s", read("todo.go"), read("app/route.go"))
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
		// the pages import the root package as domain, so it may have any name a package may have,
		// also one a route package, a predeclared name or a name the pages use
		{"example.com/todo", "todo", "todo", "todo"},
		{"example.com/domain", "domain", "domain", "domain"},
		{"example.com/error", "error", "error", "error"},
		{"example.com/C", "c", "C", "c"},
		{"example.com/tifa", "tifa", "tifa", "tifa"},
		{"example.com/err", "err", "err", "err"},
		{"example.com/Context", "context", "Context", "context"},
		{"example.com/" + strings.Repeat("a", 300), strings.Repeat("a", 300), strings.Repeat("a", 300), strings.Repeat("a", 214)},
	}
	for _, tc := range cases {
		app, err := appNames(tc.module)
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
		if _, err := appNames(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// The Go files under app/ import the root package as domain, whatever its name, so domain must
// name nothing else in them.
func TestNewPagesImportTheRootPackageAsDomain(t *testing.T) {
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
			named := spec.Name != nil && spec.Name.Name == "domain"
			if root && !named {
				t.Errorf("%s imports the root package without the name domain", rel)
			}
			if !root && (named || path.Base(strings.Trim(spec.Path.Value, `"`)) == "domain") {
				t.Errorf("%s imports %s as domain", rel, spec.Path.Value)
			}
			if root {
				checked = append(checked, rel)
			}
		}
		// Past the imports, domain may only be the package in domain.X.
		for _, decl := range file.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "domain" {
						return false
					}
				}
				if id, ok := n.(*ast.Ident); ok && id.Name == "domain" {
					t.Errorf("%s uses domain for something else than the root package", rel)
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"app/route.go", "app/route_test.go", "app/todos/todo+/route.go", "app/todos/todo+/route_test.go"}
	if !reflect.DeepEqual(checked, want) {
		t.Fatalf("the root package is imported by %q, want %q", checked, want)
	}
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

// modernc.org/sqlite and the QuickJS of tifa both run on modernc.org/libc, and each is built for
// the exact libc version its go.mod requires. Go picks the highest version any module requires,
// so the app builds with that libc only when the SQLite release go.mod.tmpl pins and the QuickJS
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
	quickjs := strings.TrimSpace(string(goCmd("list", "-m", "-f", "{{.GoMod}}", "github.com/liustack/quickjs")))
	// The SQLite module is not a dependency of tifa, so it may have to be downloaded.
	out, err := exec.Command("go", "mod", "download", "-json", sqlite).Output()
	if err != nil {
		t.Skipf("go mod download %s: %v, the module proxy may be out of reach", sqlite, err)
	}
	var download struct{ GoMod string }
	if err := json.Unmarshal(out, &download); err != nil {
		t.Fatal(err)
	}
	for _, gomod := range []string{quickjs, download.GoMod} {
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
			t.Errorf("%s requires modernc.org/libc %s, tifa builds with %s: pin a modernc.org/sqlite in go.mod.tmpl built for the same libc as the QuickJS of tifa", f.Module.Mod, required, libc)
		}
	}
}
