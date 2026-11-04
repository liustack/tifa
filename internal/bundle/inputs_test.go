package bundle

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// inputs.json records the modules of the bundle inside the root, the project files, the public
// files and the content, and ChangedInputs names those that changed or are gone since.
func TestInputsNameTheChangedFiles(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"app/page.tsx":        "page",
		"components/nav.tsx":  "nav",
		"package.json":        "{}",
		"tifa.yaml":           "",
		"public/logo.svg":     "<svg/>",
		"content/blog/a.md":   "# A",
		"node_modules/x/x.js": "x",
		"README.md":           "not read by the build",
	}
	for name, data := range files {
		writeFile(t, filepath.Join(root, name), data)
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.ts")
	writeFile(t, outside, "o")
	modules := []string{filepath.Join(real, "app/page.tsx"), filepath.Join(real, "components/nav.tsx"), outside, filepath.Join(real, "virtual.ts")}
	dist := filepath.Join(root, ".tifa", "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeInputs(root, dist, filepath.Join(root, "public"), modules); err != nil {
		t.Fatal(err)
	}
	if changed, err := ChangedInputs(root, dist); err != nil || len(changed) != 0 {
		t.Fatalf("right after the build: %v, %v", changed, err)
	}
	writeFile(t, filepath.Join(root, "components/nav.tsx"), "nav 2")
	writeFile(t, filepath.Join(root, "content/blog/a.md"), "# A 2")
	writeFile(t, filepath.Join(root, "README.md"), "changed, not an input")
	writeFile(t, filepath.Join(root, "node_modules/x/x.js"), "changed, not an input")
	writeFile(t, outside, "changed, not an input")
	if err := os.Remove(filepath.Join(root, "public/logo.svg")); err != nil {
		t.Fatal(err)
	}
	changed, err := ChangedInputs(root, dist)
	if want := []string{"components/nav.tsx", "content/blog/a.md", "public/logo.svg"}; err != nil || !slices.Equal(changed, want) {
		t.Fatalf("changed %v, %v, want %v", changed, err, want)
	}
	if _, err := ChangedInputs(root, t.TempDir()); !os.IsNotExist(err) {
		t.Errorf("a dist without inputs.json: %v", err)
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A file that appears after the build is an input too when the build would have read it: a public
// file, a file of content/, or a project file such as tsconfig.json.
func TestInputsNameTheFilesAddedSince(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), "{}")
	writeFile(t, filepath.Join(root, "static/logo.svg"), "<svg/>")
	dist := filepath.Join(root, ".tifa", "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeInputs(root, dist, filepath.Join(root, "static"), nil); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "static/icons/new.svg"), "<svg/>")
	writeFile(t, filepath.Join(root, "content/blog/a.md"), "# A")
	writeFile(t, filepath.Join(root, "tsconfig.json"), "{}")
	writeFile(t, filepath.Join(root, "notes.txt"), "not an input")
	changed, err := ChangedInputs(root, dist)
	if want := []string{"content/blog/a.md", "static/icons/new.svg", "tsconfig.json"}; err != nil || !slices.Equal(changed, want) {
		t.Fatalf("changed %v, %v, want %v", changed, err, want)
	}
}

// inputs.json lists the modules of both bundles: a module only the browser loads, such as a
// component that renders only there, is an input of the build too.
func TestInputsListTheModulesOfTheBrowserBundle(t *testing.T) {
	root := fakeApp(t, `["es2022"]`)
	writeTree(t, root, map[string]string{
		"app/page/browser.ts": "export const browser = 'only in the browser'\n",
		".tifa/client.ts": "import { browser } from '../app/page/browser'\n" +
			"import('../app/page/page').then((m) => console.log(m.default, browser))\n",
	})
	buildApp(t, root)
	var in inputs
	readJSON(t, filepath.Join(root, ".tifa", "dist", InputsFile), &in)
	for _, want := range []string{"app/page/browser.ts", "app/page/page.tsx", ".tifa/client.ts", ".tifa/server.ts"} {
		if _, ok := in.Files[want]; !ok {
			t.Errorf("inputs.json has no %s: %v", want, in.Files)
		}
	}
}
