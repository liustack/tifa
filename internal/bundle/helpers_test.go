package bundle

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/liustack/quickjs"
)

// testModules is the npm packages these tests use, Vite among them, installed from
// testdata/package.json. Tests that need them skip without them or without node.
func testModules(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "testdata", "node_modules")
	if _, err := os.Stat(filepath.Join(dir, "vite", "package.json")); err != nil {
		t.Skipf("needs npm packages: run pnpm install --frozen-lockfile in internal/bundle/testdata (%v)", err)
	}
	return dir
}

// testdata is the directory that holds testModules.
func testdata(t *testing.T) string {
	t.Helper()
	return filepath.Dir(testModules(t))
}

// copyTestdata copies testdata/<name> into a fresh directory. With modules it links testModules
// into that directory, as an application would have them.
func copyTestdata(t *testing.T, name string, modules bool) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("testdata", name))); err != nil {
		t.Fatal(err)
	}
	if modules {
		if err := os.Symlink(testModules(t), filepath.Join(root, "node_modules")); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// evalResult runs a bundle in a fresh QuickJS VM, drains microtasks and returns
// globalThis.result.
func evalResult(t *testing.T, code string) string {
	t.Helper()
	vm, err := quickjs.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	if _, err := vm.Eval(code, quickjs.EvalGlobal); err != nil {
		t.Fatalf("evaluate bundle: %v", err)
	}
	if _, err := vm.ExecutePendingJobs(); err != nil {
		t.Fatal(err)
	}
	value, err := vm.Eval("String(globalThis.result)", quickjs.EvalGlobal)
	if err != nil {
		t.Fatal(err)
	}
	return value.(string)
}
