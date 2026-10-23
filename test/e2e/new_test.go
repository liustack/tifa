package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewApp creates an app with tifa new, follows the steps it prints, and checks that the app
// tests, builds and answers.
func TestNewApp(t *testing.T) {
	skipUnlessEnabled(t)
	modules := filepath.Join(repoRoot(t), "test", "apps", "pages", "node_modules")
	if _, err := os.Stat(modules); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/pages first")
	}
	dir := filepath.Join(t.TempDir(), "hello")
	out, err := exec.Command(cli(t), "new", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("tifa new: %v\n%s", err, out)
	}
	// Follow the printed steps in their order. The CLI is built in this checkout, so the steps
	// point go.mod at it. The install step is replaced by the link below, and tifa dev by the build.
	// tifa new says pnpm install when it finds pnpm on PATH, and npm install otherwise.
	install := "npm install"
	if _, err := exec.LookPath("pnpm"); err == nil {
		install = "pnpm install"
	}
	_, next, found := strings.Cut(string(out), "\nNext:\n")
	if !found {
		t.Fatalf("tifa new printed no next steps:\n%s", out)
	}
	var ran []string
	for _, line := range strings.Split(strings.TrimSpace(next), "\n") {
		step, _, _ := strings.Cut(line, "#")
		step = strings.TrimSpace(step)
		ran = append(ran, step)
		switch {
		case strings.HasPrefix(step, "cd "):
			if step != "cd "+dir {
				t.Fatalf("step %q does not enter %s", step, dir)
			}
		case step == install, step == "go tool tifa dev":
		default:
			step = strings.ReplaceAll(step, "<path to your tifa checkout>", repoRoot(t))
			args := strings.Fields(step)
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v\n%s", step, err, out)
			}
		}
	}
	want := []string{"cd " + dir, "go mod edit -replace github.com/liustack/tifa=<path to your tifa checkout>", "go mod tidy", install, "go tool tifa dev"}
	if strings.Join(ran, "\n") != strings.Join(want, "\n") {
		t.Fatalf("steps %q, want %q", ran, want)
	}
	// npm packages may carry Go files, such as flatted, a dependency of eslint, which has no
	// go.mod of its own. The ignore line of go.mod keeps them out of ./... at any depth.
	broken := filepath.Join(dir, "web", "node_modules", "broken")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "broken.go"), []byte("package broken\n\nthis does not compile\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, step := range [][]string{{"go", "vet", "./..."}, {"go", "test", "./..."}, {"go", "mod", "tidy", "-diff"}} {
		cmd := exec.Command(step[0], step[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(step, " "), err, out)
		}
	}
	// The app installs the packages of test/apps/pages, which TestNewAppUsesTheTestedPackages in
	// cmd/tifa checks.
	if err := os.Symlink(modules, filepath.Join(dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
	// SQLite and the slowjs of tifa both run on modernc.org/libc, and each is built for the exact
	// libc version its go.mod requires. The app must build with that version for both.
	libc := moduleVersion(t, dir, "modernc.org/libc")
	for _, module := range []string{"modernc.org/sqlite", "github.com/liustack/slowjs"} {
		if required := requires(t, dir, module, "modernc.org/libc"); required != libc {
			t.Errorf("%s requires modernc.org/libc %s, the new app builds with %s: pin a modernc.org/sqlite in cmd/tifa/newapp/go.mod.tmpl built for the libc of the slowjs of tifa", module, required, libc)
		}
	}
	exe := buildAt(t, dir)
	// The app opens its database at DB_PATH, by default app.db in the folder it runs in.
	db := filepath.Join(t.TempDir(), "todos.db")
	t.Setenv("DB_PATH", db)
	base, _, output := start(t, exe)
	if status, _, body := get(t, base+"/"); status != 200 || !strings.Contains(body, "<title>hello</title>") || !strings.Contains(body, "Nothing to do") {
		t.Fatalf("GET /: %d\n%s\n%s", status, body, output)
	}
	if _, err := os.Stat(db); err != nil {
		t.Fatalf("the app did not open its database at DB_PATH: %v", err)
	}
	if status, _, body := request(t, "POST", base+"/", `{"title":"  "}`); status != 422 || strings.TrimSpace(body) != `{"error":"Unprocessable Entity","fields":{"title":"Write what needs doing."}}` {
		t.Fatalf("POST / with an empty title: %d %s", status, body)
	}
	if status, _, body := request(t, "POST", base+"/", `{"title":"  Water the plants "}`); status != 200 || strings.TrimSpace(body) != `{"id":1,"title":"Water the plants","done":false}` {
		t.Fatalf("POST /: %d %s", status, body)
	}
	if status, _, body := get(t, base+"/"); status != 200 || !strings.Contains(body, `<a href="/todos/1">Water the plants</a>`) {
		t.Fatalf("GET / after POST: %d\n%s", status, body)
	}
	if status, _, body := get(t, base+"/todos/1"); status != 200 || !strings.Contains(body, "<title>Water the plants</title>") || !strings.Contains(body, "Mark as done") {
		t.Fatalf("GET /todos/1: %d\n%s", status, body)
	}
	if status, _, body := request(t, "POST", base+"/todos/1", `{"done":true}`); status != 200 || strings.TrimSpace(body) != `{"id":1,"title":"Water the plants","done":true}` {
		t.Fatalf("POST /todos/1: %d %s", status, body)
	}
	if status, _, body := get(t, base+"/todos/1"); status != 200 || !strings.Contains(body, "Mark as not done") {
		t.Fatalf("GET /todos/1 after POST: %d\n%s", status, body)
	}
	for _, path := range []string{"/todos/2", "/todos/one"} {
		if status, _, body := get(t, base+path); status != 404 || !strings.Contains(body, "No such todo") {
			t.Fatalf("GET %s: %d\n%s", path, status, body)
		}
	}
	if status, _, body := request(t, "POST", base+"/todos/2", `{"done":true}`); status != 404 {
		t.Fatalf("POST /todos/2: %d %s", status, body)
	}
	if status, _, body := get(t, base+"/nowhere"); status != 404 || !strings.Contains(body, "Page not found") {
		t.Fatalf("GET /nowhere: %d\n%s", status, body)
	}
	if status, _, body := get(t, base+"/about"); status != 200 || !strings.Contains(body, "<h1>About</h1>") {
		t.Fatalf("GET /about: %d\n%s", status, body)
	}
	typecheck(t, dir)
}

// moduleVersion is the version of module the go command selects in the module at dir.
func moduleVersion(t *testing.T, dir, module string) string {
	t.Helper()
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Version}}", module)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -m %s in %s: %v", module, dir, err)
	}
	return strings.TrimSpace(string(out))
}

// requires is the version of dep that the selected version of module requires in its go.mod, as
// the module graph of the module at dir shows it.
func requires(t *testing.T, dir, module, dep string) string {
	t.Helper()
	from := module + "@" + moduleVersion(t, dir, module)
	cmd := exec.Command("go", "mod", "graph")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go mod graph in %s: %v", dir, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if edge := strings.Fields(line); len(edge) == 2 && edge[0] == from && strings.HasPrefix(edge[1], dep+"@") {
			return strings.TrimPrefix(edge[1], dep+"@")
		}
	}
	return "none"
}

// The Go files under app/ import the root package by its own name. tifa new refuses a name that
// clashes there, and the names it takes compile: also the name of a package under app/, which a
// file may import from a package of the same name, and the name of a package of the standard
// library that those files do not import.
func TestNewAppRootPackageNames(t *testing.T) {
	skipUnlessEnabled(t)
	for _, module := range []string{"example.com/app", "example.com/todo", "example.com/fmt"} {
		t.Run(module, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(t.TempDir(), "app")
			for _, step := range [][]string{
				{cli(t), "new", dir, "--module", module},
				{"go", "mod", "edit", "-replace", "github.com/liustack/tifa=" + repoRoot(t)},
				{"go", "mod", "tidy"},
				{"go", "vet", "./..."},
			} {
				cmd := exec.Command(step[0], step[1:]...)
				if step[0] == "go" {
					cmd.Dir = dir
				}
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%s: %v\n%s", strings.Join(step, " "), err, out)
				}
			}
		})
	}
}
