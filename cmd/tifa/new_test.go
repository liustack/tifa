package main

import (
	"encoding/json"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

func TestNewWritesAnApp(t *testing.T) {
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
	want := []string{".gitignore", "README.md", "app/layout.tsx", "app/route.go", "app/route.tsx", "app/route_test.go", "app/styles.css", "counter.go", "go.mod", "package.json", "tsconfig.json"}
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
	for _, line := range []string{"module My-App\n", "require github.com/liustack/tifa v0.0.0\n", "tool github.com/liustack/tifa/cmd/tifa\n"} {
		if !strings.Contains(gomod, line) {
			t.Errorf("go.mod has no line %q:\n%s", line, gomod)
		}
	}
	if !strings.Contains(read("counter.go"), "package myapp\n") || !strings.Contains(read("app/route.go"), `myapp "My-App"`) {
		t.Errorf("the root package is not myapp:\n%s\n%s", read("counter.go"), read("app/route.go"))
	}
	for _, name := range []string{"counter.go", "app/route.go", "app/route_test.go"} {
		src := read(name)
		if formatted, err := format.Source([]byte(src)); err != nil || string(formatted) != src {
			t.Errorf("%s is not gofmt'ed Go: %v", name, err)
		}
	}
	if !strings.Contains(read("app/route.tsx"), "<h1>My-App</h1>") || !strings.Contains(read("package.json"), `"name": "myapp"`) {
		t.Errorf("the page does not show the app name:\n%s", read("app/route.tsx"))
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
		// names only the test file uses, which the import of the root package in route.go cannot hide
		{"example.com/err", "err", "err", "err"},
		{"example.com/testing", "testing", "testing", "testing"},
		{"example.com/" + strings.Repeat("a", 300), strings.Repeat("a", 300), strings.Repeat("a", 300), strings.Repeat("a", 214)},
	}
	for _, tc := range cases {
		app, err := appNames(tc.module)
		if err != nil || app.Package != tc.pkg || app.Title != tc.title || app.NPM != tc.npm {
			t.Errorf("%s: %+v, %v, want package %s, title %s and npm name %s", tc.module, app, err, tc.pkg, tc.title, tc.npm)
		}
	}
	for _, bad := range []string{
		"example.com/123", "example.com/main", "example.com/tifa", "example.com/type", "example.com/a b", "",
		// predeclared names and names of the generated code, which the import of the root package would hide
		"example.com/error", "example.com/int64", "example.com/nil", "example.com/c", "example.com/C",
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

// app/route.go imports the root package by its name, so that name must not be one route.go uses
// for something else.
func TestNewRootPackageHidesNoName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	if _, _, err := run(t, "new", dir, "--module", "example.com/myapp"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"app/route.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				ast.Inspect(sel.X, func(n ast.Node) bool { return checkIdent(t, name, n) })
				return false
			}
			return checkIdent(t, name, n)
		})
	}
}

func checkIdent(t *testing.T, file string, n ast.Node) bool {
	id, ok := n.(*ast.Ident)
	// A package name is lowercase, so only a lowercase name can be the same.
	if !ok || id.Name == "myapp" || id.Name == "app" || id.Name == "_" || id.Name != strings.ToLower(id.Name) {
		return true
	}
	if _, err := appNames("example.com/" + id.Name); err == nil {
		t.Errorf("%s uses %s, which tifa new accepts as the name of the root package", file, id.Name)
	}
	return true
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
