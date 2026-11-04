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

// A file the build read without making it a module is an input too, such as a stylesheet that
// another one imports, which Vite inlines.
func TestInputsListTheFilesThatStylesheetsImport(t *testing.T) {
	root := fakeApp(t, `["es2022"]`)
	writeTree(t, root, map[string]string{
		"app/page/page.css":  "@import './theme.css';\n.page { color: red }\n",
		"app/page/theme.css": ".theme { color: blue }\n",
	})
	buildApp(t, root)
	var in inputs
	readJSON(t, filepath.Join(root, ".tifa", "dist", InputsFile), &in)
	if _, ok := in.Files["app/page/theme.css"]; !ok {
		t.Errorf("inputs.json has no app/page/theme.css: %v", in.Files)
	}
}

// Every file a plugin of the Vite config adds with this.addWatchFile is an input, however the
// plugin is written and however it names the file. The driver reads them from the watchFiles of
// Rolldown's bundles, which Rolldown marks experimental: when a Rolldown drops it, or stops
// putting these files in it, or Vite builds without closing a bundle of the rolldown it imports,
// this test fails, by the error of the driver or by the missing file.
func TestInputsListTheWatchFilesOfEveryPlugin(t *testing.T) {
	const file = "path.join(process.cwd(), 'extra/watched.txt')"
	for name, plugin := range map[string]string{
		// A hook that a getter returns.
		"getter": "{ name: 'getter', get buildStart() { return function () { this.addWatchFile(" + file + ") } } }",
		// A plugin that applyToEnvironment returns in place of its own.
		"environment": "{ name: 'factory', applyToEnvironment: () => ({ name: 'made', buildStart() { this.addWatchFile(" + file + ") } }) }",
		// A path relative to the working directory, which Rolldown resolves against it.
		"relative": "{ name: 'relative', buildStart() { this.addWatchFile('extra/watched.txt') } }",
		// A helper that the hook puts on its context and calls as a method.
		"helper": "{ name: 'helper', buildStart() { this.watch = function (id) { this.addWatchFile(id) }; this.watch(" + file + ") } }",
	} {
		t.Run(name, func(t *testing.T) {
			root := fakeApp(t, `["es2022"]`)
			writeTree(t, root, map[string]string{
				"extra/watched.txt": "read by a plugin\n",
				"vite.config.ts": "import path from 'node:path'\nimport tifa from './.tifa/vite.js'\n" +
					"export default { plugins: [tifa(), " + plugin + "] }\n",
			})
			buildApp(t, root)
			var in inputs
			readJSON(t, filepath.Join(root, ".tifa", "dist", InputsFile), &in)
			if _, ok := in.Files["extra/watched.txt"]; !ok {
				t.Errorf("inputs.json has no extra/watched.txt: %v", in.Files)
			}
		})
	}
}

// A path the driver lists that is not absolute fails the build, rather than leaving the file out.
func TestInputsRefuseAModulePathThatIsNotAbsolute(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	writeTree(t, root, map[string]string{"app/page.tsx": "export default 1\n", "dist/build.json": "{}\n"})
	err := writeInputs(root, dist, filepath.Join(root, "public"), []string{"app/page.tsx"})
	if err == nil {
		t.Fatal("writeInputs took the relative path app/page.tsx")
	}
}

// A file under the root by the path the root was given, which may be a link, such as a path a
// plugin builds from Vite's root, is an input like one under the real root.
func TestInputsTakeAFileUnderTheLinkToTheRoot(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	writeTree(t, real, map[string]string{"extra/watched.txt": "read by a plugin\n", "dist/build.json": "{}\n"})
	root := filepath.Join(dir, "link")
	if err := os.Symlink(real, root); err != nil {
		t.Fatal(err)
	}
	dist := filepath.Join(root, "dist")
	if err := writeInputs(root, dist, filepath.Join(root, "public"), []string{filepath.Join(root, "extra", "watched.txt")}); err != nil {
		t.Fatal(err)
	}
	var in inputs
	readJSON(t, filepath.Join(dist, InputsFile), &in)
	if _, ok := in.Files["extra/watched.txt"]; !ok {
		t.Errorf("inputs.json has no extra/watched.txt: %v", in.Files)
	}
}
