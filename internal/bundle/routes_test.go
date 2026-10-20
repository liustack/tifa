package bundle

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// routeApp is fakeApp with a route tree in the form tifa generates it: routes.tsx imports the
// root Layout and a page that exports head statically, and another page with import(). Its
// node_modules adds a stand-in for TanStack Router's lazyRouteComponent to the npm packages of
// testdata. Each component returns a text of its own, so the tests can tell which file holds it.
func routeApp(t *testing.T) string {
	t.Helper()
	root := fakeApp(t, `["es2022"]`)
	if err := os.Remove(filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	modules := testModules(t)
	entries, err := os.ReadDir(modules)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.MkdirAll(filepath.Join(root, "node_modules"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(modules, e.Name()), filepath.Join(root, "node_modules", e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	writeTree(t, root, map[string]string{
		"node_modules/@tanstack/react-router/package.json": `{"name": "@tanstack/react-router", "type": "module", "main": "index.js"}`,
		"node_modules/@tanstack/react-router/index.js": "export function lazyRouteComponent(load, name = 'default') {\n" +
			"  let component\n" +
			"  const lazy = () => component()\n" +
			"  lazy.preload = () => load().then((m) => { component = m[name] })\n" +
			"  return lazy\n}\n",
		"app/layout.tsx": "export default function Layout() { return 'LAYOUT-COMPONENT' }\n",
		"app/page/route.tsx": "import './page.css'\n" +
			"(globalThis as any).pageRan = true\n" +
			"const site = 'PAGE-SITE'\n" +
			"export default function Page() { return 'PAGE-COMPONENT ' + site }\n" +
			"export const head = () => ({ meta: [{ title: 'PAGE-HEAD ' + site }] })\n" +
			"export function Error() { return 'PAGE-ERROR' }\n" +
			"export function NotFound() { return 'PAGE-NOT-FOUND' }\n",
		"app/other/route.tsx": "export default function Other() { return 'OTHER-COMPONENT' }\n",
		".tifa/routes.tsx": "import { lazyRouteComponent } from '@tanstack/react-router'\n" +
			"import * as m1 from '../app/layout'\n" +
			"import * as m2 from '../app/page/route'\n" +
			"const m3 = () => import('../app/other/route')\n" +
			"export const parts = { layout: m1.default, page: m2.default, head: m2.head, error: m2.Error, " +
			"notFound: m2.NotFound, other: lazyRouteComponent(m3) }\n",
		".tifa/client.ts": "import { parts } from './routes'\n(globalThis as any).parts = parts\n",
		".tifa/server.ts": "import { parts } from './routes'\n" +
			"(globalThis as any).result = JSON.stringify([parts.layout(), parts.page(), parts.head(), parts.error(), parts.notFound()])\n",
	})
	return root
}

// headFiles returns what the files the head of build.json loads hold, the entry script and the
// chunks it imports, and what the other JavaScript files of the browser bundle hold.
func headFiles(t *testing.T, root string) (head, rest string) {
	t.Helper()
	dist := filepath.Join(root, ".tifa", "dist")
	loaded := map[string]bool{}
	for _, m := range headURL.FindAllStringSubmatch(readBuild(t, root).Head, -1) {
		loaded[filepath.Join(dist, filepath.FromSlash(m[1]))] = true
	}
	files, err := filepath.Glob(filepath.Join(dist, "assets", "*.js"))
	if err != nil {
		t.Fatal(err)
	}
	var h, r strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if loaded[f] {
			h.Write(data)
		} else {
			r.Write(data)
		}
	}
	return h.String(), r.String()
}

// In the browser, a route module that routes.tsx imports statically, because it exports head,
// is loaded only when its route needs it, as the modules routes.tsx loads with import() are. The
// entry script keeps the root Layout. The SSR bundle keeps every module in its one file and reads
// head from the module itself.
func TestBrowserLoadsRouteModulesWhenTheirRoutesNeedThem(t *testing.T) {
	root := routeApp(t)
	buildApp(t, root)
	head, rest := headFiles(t, root)
	if !strings.Contains(head, "LAYOUT-COMPONENT") {
		t.Error("the entry script lacks the root Layout")
	}
	for _, text := range []string{"PAGE-COMPONENT", "PAGE-HEAD", "PAGE-ERROR", "PAGE-NOT-FOUND", "PAGE-SITE", "pageRan", "OTHER-COMPONENT"} {
		if strings.Contains(head, text) {
			t.Errorf("the entry script holds %s", text)
		}
		if !strings.Contains(rest, text) {
			t.Errorf("no chunk holds %s", text)
		}
	}
	// build.json lists the files each route module needs beyond the head, its own chunk first.
	dist := filepath.Join(root, ".tifa", "dist")
	b := readBuild(t, root)
	if _, ok := b.Preload["app/layout"]; ok || len(b.Preload) != 2 {
		t.Errorf("preloads %v, want the two pages", b.Preload)
	}
	for id, text := range map[string]string{"app/page/route": "PAGE-COMPONENT", "app/other/route": "OTHER-COMPONENT"} {
		files := b.Preload[id]
		if len(files) == 0 {
			t.Errorf("%s preloads nothing", id)
			continue
		}
		for _, file := range files {
			if strings.Contains(b.Head, file) {
				t.Errorf("%s preloads %s, which the head loads", id, file)
			}
		}
		data, err := os.ReadFile(filepath.Join(dist, filepath.FromSlash(strings.TrimPrefix(files[0], "/"))))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), text) {
			t.Errorf("%s preloads %s first, which lacks %s", id, files[0], text)
		}
	}
	// The stylesheet of a lazily loaded module is still in the one stylesheet of the head.
	var css strings.Builder
	for _, m := range headURL.FindAllStringSubmatch(readBuild(t, root).Head, -1) {
		if strings.HasSuffix(m[1], ".css") {
			data, err := os.ReadFile(filepath.Join(dist, filepath.FromSlash(m[1])))
			if err != nil {
				t.Fatal(err)
			}
			css.Write(data)
		}
	}
	if !strings.Contains(css.String(), ".page") {
		t.Errorf("the stylesheet lacks the CSS of the page: %s", css.String())
	}

	ssr, err := os.ReadFile(filepath.Join(dist, "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	want := `["LAYOUT-COMPONENT","PAGE-COMPONENT PAGE-SITE",{"meta":[{"title":"PAGE-HEAD PAGE-SITE"}]},"PAGE-ERROR","PAGE-NOT-FOUND"]`
	if got := evalResult(t, string(ssr)); got != want {
		t.Errorf("SSR bundle gives %s, want %s", got, want)
	}

	// Run the browser bundle in Node: the page module runs only once something of it is needed,
	// head then gives the head of the module, and the components are those of the module.
	var entry string
	for _, m := range headURL.FindAllStringSubmatch(readBuild(t, root).Head, -1) {
		if strings.Contains(m[1], "/app-") {
			entry = filepath.Join(dist, filepath.FromSlash(m[1]))
		}
	}
	run := "const { pathToFileURL } = await import('node:url')\n" +
		"await import(pathToFileURL(process.argv[1]).href)\n" +
		"const p = globalThis.parts\n" +
		"const before = globalThis.pageRan === true\n" +
		"const head = await p.head()\n" +
		"await Promise.all([p.page.preload(), p.error.preload(), p.notFound.preload()])\n" +
		"console.log(JSON.stringify([before, p.layout(), head, p.page(), p.error(), p.notFound()]))\n"
	out, err := exec.Command("node", "--input-type=module", "-e", run, entry).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want = `[false,"LAYOUT-COMPONENT",{"meta":[{"title":"PAGE-HEAD PAGE-SITE"}]},"PAGE-COMPONENT PAGE-SITE","PAGE-ERROR","PAGE-NOT-FOUND"]`
	if got := strings.TrimSpace(string(out)); got != want {
		t.Errorf("browser bundle gives %s, want %s", got, want)
	}
}

// A route module that a module of the entry script imports stays part of the entry script, also
// one that exports head. The bundler says so, naming the importer.
func TestAppWarnsOfRouteModulesInTheEntryScript(t *testing.T) {
	root := routeApp(t)
	writeTree(t, root, map[string]string{
		"app/layout.tsx": "import Page from './page/route'\n" +
			"export default function Layout() { return 'LAYOUT-COMPONENT ' + Page() }\n",
	})
	var output strings.Builder
	n := NewNode(root, &output)
	if _, err := App(n, root, filepath.Join(root, "public"), true); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	head, _ := headFiles(t, root)
	if !strings.Contains(head, "PAGE-COMPONENT") {
		t.Fatal("the entry script lacks the module the root Layout imports")
	}
	if !strings.Contains(output.String(), "app/page/route.tsx is dynamically imported by") ||
		!strings.Contains(output.String(), "statically imported by app/layout.tsx") {
		t.Errorf("the build gives no warning that names the module and its importer:\n%s", output.String())
	}
}
