package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// fakeApp is an application root with generated entries that need no npm packages.
func fakeApp(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"app/page/route.tsx":       "import { route } from './+route'\nimport './page.css'\nexport default route\n",
		"app/page/page.css":        ".page { color: red }\n",
		".tifa/app/page/+route.ts": "export const route = 'page-route'\n",
		".tifa/runtime/client.ts":  "export const clientOnly = 1\n",
		".tifa/tsconfig.json":      "{}\n",
		".tifa/client.ts":          "import('../app/page/route').then((m) => console.log(m.default))\n",
		".tifa/server.ts": "declare const __TIFA_ASSETS__: unknown\n" +
			"import '../app/page/route'\n(globalThis as any).startRender = () => JSON.stringify(__TIFA_ASSETS__)\n",
		"public/robots.txt":   "User-agent: *\n",
		".tifa/dist/stale.js": "old\n",
	})
	return root
}

func readBuild(t *testing.T, root string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "build.json"))
	if err != nil {
		t.Fatal(err)
	}
	var b map[string]string
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAppWritesDist(t *testing.T) {
	root := fakeApp(t)
	id, err := App(root, filepath.Join(root, "public"), "es2022", true)
	if err != nil {
		t.Fatalf("App: %v", err)
	}
	dist := filepath.Join(root, ".tifa", "dist")
	b := readBuild(t, root)
	if b["buildId"] != id || len(id) != 16 {
		t.Errorf("build.json %v, id %s", b, id)
	}
	if !strings.HasPrefix(b["script"], "/assets/app-") || !strings.HasPrefix(b["style"], "/assets/") {
		t.Errorf("assets %v", b)
	}
	for _, file := range []string{b["script"], b["style"], "/public/robots.txt"} {
		if _, err := os.Stat(filepath.Join(dist, filepath.FromSlash(file))); err != nil {
			t.Errorf("%s: %v", file, err)
		}
	}
	ssr, err := os.ReadFile(filepath.Join(dist, "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ssr), b["script"]) {
		t.Error("the SSR bundle does not know the browser script")
	}
	for _, gone := range []string{"stale.js", "ssr.css"} {
		if _, err := os.Stat(filepath.Join(dist, gone)); !os.IsNotExist(err) {
			t.Errorf("%s exists: %v", gone, err)
		}
	}
	// Same input, same bytes, same ID.
	again, err := App(root, filepath.Join(root, "public"), "es2022", true)
	if err != nil || again != id {
		t.Errorf("second build: %s %v, first %s", again, err, id)
	}
	// Any byte of the output changes the ID.
	writeTree(t, root, map[string]string{"public/robots.txt": "User-agent: tifa\n"})
	if changed, err := App(root, filepath.Join(root, "public"), "es2022", true); err != nil || changed == id {
		t.Errorf("changed public file kept the ID %s (%v)", changed, err)
	}
}

func TestAppWithoutPagesOrPublic(t *testing.T) {
	root := t.TempDir()
	id, err := App(root, filepath.Join(root, "public"), "es2022", false)
	if err != nil {
		t.Fatalf("App: %v", err)
	}
	if b := readBuild(t, root); b["buildId"] != id || b["script"] != "" {
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
		for name, content := range files {
			path := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		id, err := distID(dir)
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
}

// build.target lowers the browser bundle. The SSR bundle keeps the syntax QuickJS runs.
func TestAppHonoursTheTarget(t *testing.T) {
	assignment := regexp.MustCompile(`\|\|=`)
	browser := func(root string) string {
		var all strings.Builder
		matches, err := filepath.Glob(filepath.Join(root, ".tifa", "dist", "assets", "*.js"))
		if err != nil || len(matches) == 0 {
			t.Fatalf("no browser files: %v", err)
		}
		for _, m := range matches {
			data, err := os.ReadFile(m)
			if err != nil {
				t.Fatal(err)
			}
			all.Write(data)
		}
		return all.String()
	}
	build := func(target string) (string, string, error) {
		root := fakeApp(t)
		writeTree(t, root, map[string]string{
			"app/page/route.tsx": "import { route } from './+route'\nlet n = (globalThis as any).n\nn ||= route\nexport default n\n",
		})
		if _, err := App(root, filepath.Join(root, "public"), target, true); err != nil {
			return "", "", err
		}
		ssr, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "ssr.js"))
		if err != nil {
			t.Fatal(err)
		}
		return browser(root), string(ssr), nil
	}
	old, ssr, err := build("es2020")
	if err != nil {
		t.Fatal(err)
	}
	if assignment.MatchString(old) || !assignment.MatchString(ssr) {
		t.Errorf("es2020: browser keeps ||= %t, SSR keeps ||= %t", assignment.MatchString(old), assignment.MatchString(ssr))
	}
	if modern, _, err := build("es2022"); err != nil || !assignment.MatchString(modern) {
		t.Errorf("es2022: %v, browser keeps ||= %t", err, assignment.MatchString(modern))
	}
	if engines, _, err := build("es2019,chrome90,safari14"); err != nil || assignment.MatchString(engines) {
		t.Errorf("engines: %v", err)
	}
	if _, _, err := build("es1999"); err == nil || !strings.Contains(err.Error(), "es1999") {
		t.Errorf("unknown target: %v", err)
	}
}
