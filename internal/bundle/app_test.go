package bundle

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/liustack/tifa/js"
)

// fakeApp is an application root with the files tifa build generates written by hand, its Vite
// config and the npm packages of testdata. target is the browser target of the tifa plugin.
func fakeApp(t *testing.T, target string) string {
	t.Helper()
	modules := testModules(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"vite.config.ts":           "import tifa from './.tifa/vite.js'\nexport default { plugins: [tifa()] }\n",
		"app/page/route.tsx":       "import { route } from './+route'\nimport './page.css'\nexport default route\n",
		"app/page/page.css":        ".page { color: red }\n",
		".tifa/app/page/+route.ts": "export const route = 'page-route'\n",
		".tifa/runtime/client.ts":  "export const clientOnly = 1\n",
		".tifa/client.ts":          "import('../app/page/route').then((m) => console.log(m.default))\n",
		".tifa/server.ts": "declare const __TIFA_ASSETS__: string\n" +
			"import route from '../app/page/route'\n(globalThis as any).result = route + ' ' + __TIFA_ASSETS__\n",
		".tifa/vite/plugin.js": js.VitePlugin,
		".tifa/vite.js":        "import { tifa } from './vite/plugin.js'\nexport default () => tifa({ target: " + target + " })\n",
		"public/robots.txt":    "User-agent: *\n",
		".tifa/dist/stale.js":  "old\n",
	})
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	return root
}

func readBuild(t *testing.T, root string) Build {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "build.json"))
	if err != nil {
		t.Fatal(err)
	}
	var b Build
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func buildApp(t *testing.T, root string) string {
	t.Helper()
	n := NewNode(root, io.Discard)
	defer n.Close()
	id, err := App(n, root, filepath.Join(root, "public"), true)
	if err != nil {
		t.Fatalf("App: %v", err)
	}
	return id
}

var headURL = regexp.MustCompile(`(?:href|src)="(/assets/[^"]+)"`)

func TestAppWritesDist(t *testing.T) {
	root := fakeApp(t, `["es2022"]`)
	id := buildApp(t, root)
	dist := filepath.Join(root, ".tifa", "dist")
	b := readBuild(t, root)
	if b.ID != id || len(id) != 16 {
		t.Errorf("build.json %v, id %s", b, id)
	}
	urls := headURL.FindAllStringSubmatch(b.Head, -1)
	if !strings.Contains(b.Head, `<link rel="stylesheet" href="/assets/`) ||
		!strings.Contains(b.Head, `<script type="module" src="/assets/app-`) || len(urls) < 2 {
		t.Errorf("head %s", b.Head)
	}
	for _, m := range urls {
		if _, err := os.Stat(filepath.Join(dist, filepath.FromSlash(m[1]))); err != nil {
			t.Errorf("%s: %v", m[1], err)
		}
	}
	for _, gone := range []string{"stale.js", ".vite"} {
		if _, err := os.Stat(filepath.Join(dist, gone)); !os.IsNotExist(err) {
			t.Errorf("%s is left in dist: %v", gone, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dist, "public", "robots.txt")); err != nil {
		t.Error(err)
	}
	ssr, err := os.ReadFile(filepath.Join(dist, "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	if got := evalResult(t, string(ssr)); got != "page-route "+b.Head {
		t.Errorf("SSR bundle gives %q, want the route and the head %q", got, b.Head)
	}
	if again := buildApp(t, root); again != id {
		t.Errorf("the same input gives build %s, then %s", id, again)
	}
}

func TestAppWithoutPagesOrPublic(t *testing.T) {
	root := t.TempDir()
	// An application without pages starts no Node: a missing node_modules is no error.
	n := NewNode(root, io.Discard)
	defer n.Close()
	id, err := App(n, root, filepath.Join(root, "public"), false)
	if err != nil {
		t.Fatalf("App: %v", err)
	}
	if b := readBuild(t, root); b.ID != id || b.Head != "" {
		t.Errorf("build.json %v", b)
	}
	if _, err := os.Stat(filepath.Join(root, ".tifa", "dist", "ssr.js")); !os.IsNotExist(err) {
		t.Errorf("ssr.js without pages: %v", err)
	}
}

// Two different file trees never share an ID, even when one file holds what the separators of
// the other would look like.
func TestDistIDTellsFileTreesApart(t *testing.T) {
	write := func(files map[string]string) string {
		dir := t.TempDir()
		writeTree(t, dir, files)
		id, err := distID(dir, "")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	one := write(map[string]string{"public/a": "first\x00public/b\x00second"})
	two := write(map[string]string{"public/a": "first", "public/b": "second"})
	if one == two {
		t.Errorf("both trees get ID %s", one)
	}
	if skipped := write(map[string]string{"public/a": "first", "ssr.js": "x"}); skipped == write(map[string]string{"public/a": "first"}) {
		t.Error("ssr.js counts")
	}
}

// The target of the tifa plugin lowers the browser bundle. The SSR bundle keeps the syntax
// QuickJS runs.
func TestAppHonoursTheTarget(t *testing.T) {
	assignment := regexp.MustCompile(`\|\|=`)
	build := func(target string) (string, string) {
		root := fakeApp(t, target)
		writeTree(t, root, map[string]string{
			"app/page/route.tsx": "import { route } from './+route'\nlet n = (globalThis as any).n\nn ||= route\nexport default n\n",
		})
		buildApp(t, root)
		var browser strings.Builder
		matches, err := filepath.Glob(filepath.Join(root, ".tifa", "dist", "assets", "*.js"))
		if err != nil || len(matches) == 0 {
			t.Fatalf("no browser files: %v", err)
		}
		for _, m := range matches {
			data, err := os.ReadFile(m)
			if err != nil {
				t.Fatal(err)
			}
			browser.Write(data)
		}
		ssr, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "ssr.js"))
		if err != nil {
			t.Fatal(err)
		}
		return browser.String(), string(ssr)
	}
	old, ssr := build(`["es2020"]`)
	if assignment.MatchString(old) || !assignment.MatchString(ssr) {
		t.Errorf("es2020: browser keeps ||= %t, SSR keeps ||= %t", assignment.MatchString(old), assignment.MatchString(ssr))
	}
	if modern, _ := build(`["es2022"]`); !assignment.MatchString(modern) {
		t.Error("es2022: browser lowers ||=")
	}
	if engines, _ := build(`["es2019", "chrome90", "safari14"]`); assignment.MatchString(engines) {
		t.Error("es2019 with browsers: browser keeps ||=")
	}
}

// Fonts and images a stylesheet references with url() go into the browser assets with a content
// hash, or into the stylesheet when they are small, whatever the case of their extension, and a
// URL may encode the file name.
func TestAppCopiesFilesThatStylesheetsReference(t *testing.T) {
	root := fakeApp(t, "undefined")
	writeTree(t, root, map[string]string{
		"app/page/page.css": ".page { background: url(\"./img/bg.png\") }\n" +
			".icon { background: url(\"./img/icon.SVG\") }\n" +
			".space { background: url(\"./img/with%20space.svg\") }\n" +
			"@font-face { font-family: Brand; src: url(\"./brand.woff2?v=2\") format(\"woff2\") }\n",
		"app/page/img/bg.png":          strings.Repeat("png bytes ", 500),
		"app/page/img/icon.SVG":        "<svg xmlns=\"http://www.w3.org/2000/svg\"><rect width=\"1\" height=\"1\"/></svg>",
		"app/page/img/with space.svg":  "<svg xmlns=\"http://www.w3.org/2000/svg\"><circle r=\"1\"/></svg>",
		"app/page/brand.woff2":         strings.Repeat("font bytes ", 500),
		"app/page/img/unused-file.png": "never referenced",
	})
	buildApp(t, root)
	style := regexp.MustCompile(`href="(/assets/[^"]+\.css)"`).FindStringSubmatch(readBuild(t, root).Head)
	if style == nil {
		t.Fatal("no stylesheet in the head")
	}
	css, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", filepath.FromSlash(style[1])))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bg", "brand"} {
		m := regexp.MustCompile(`url\("?/assets/(` + name + `-[\w-]+\.\w+)(\?v=2)?"?\)`).FindStringSubmatch(string(css))
		if m == nil {
			t.Errorf("style has no hashed URL for %s:\n%s", name, css)
			continue
		}
		if _, err := os.Stat(filepath.Join(root, ".tifa", "dist", "assets", m[1])); err != nil {
			t.Error(err)
		}
	}
	// Small SVG files are inlined as data URLs, which also proves Vite read them.
	if n := strings.Count(string(css), "data:image/svg+xml"); n != 2 {
		t.Errorf("want the two SVG files inlined, found %d:\n%s", n, css)
	}
}

func TestAppReportsAConfigWithoutTheTifaPlugin(t *testing.T) {
	root := fakeApp(t, "undefined")
	writeTree(t, root, map[string]string{"vite.config.ts": "export default {}\n"})
	n := NewNode(root, io.Discard)
	defer n.Close()
	_, err := App(n, root, filepath.Join(root, "public"), true)
	if err == nil || !strings.Contains(err.Error(), "does not use the tifa plugin") {
		t.Fatalf("err = %v, want the missing plugin", err)
	}
}

func TestAppReportsAMissingConfig(t *testing.T) {
	root := fakeApp(t, "undefined")
	if err := os.Remove(filepath.Join(root, "vite.config.ts")); err != nil {
		t.Fatal(err)
	}
	n := NewNode(root, io.Discard)
	defer n.Close()
	_, err := App(n, root, filepath.Join(root, "public"), true)
	if err == nil || !strings.Contains(err.Error(), "no vite.config.ts, create it") {
		t.Fatalf("err = %v, want the missing config", err)
	}
}

func TestHeadTagsFollowStaticImports(t *testing.T) {
	head, err := headTags(map[string]manifestEntry{
		".tifa/client.ts": {File: "assets/app-1.js", IsEntry: true, Imports: []string{"_a.js", "_b.js"}, CSS: []string{"assets/entry.css"}},
		"_a.js":           {File: "assets/chunk-a.js", Imports: []string{"_b.js"}},
		"_b.js":           {File: "assets/chunk-b.js", Imports: []string{"_a.js"}},
		"app/x/route.tsx": {File: "assets/chunk-x.js", Imports: []string{"_a.js"}},
		"style.css":       {File: "assets/style-1.css"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `<link rel="stylesheet" href="/assets/entry.css"><link rel="stylesheet" href="/assets/style-1.css">` +
		`<link rel="modulepreload" href="/assets/chunk-a.js"><link rel="modulepreload" href="/assets/chunk-b.js">` +
		`<script type="module" src="/assets/app-1.js"></script>`
	if head != want {
		t.Errorf("head\n%s\nwant\n%s", head, want)
	}
	if _, err := headTags(map[string]manifestEntry{"other.ts": {File: "x.js", IsEntry: true}}); err == nil {
		t.Error("a manifest without the tifa entry gives no error")
	}
}
