package bundle

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/liustack/tifa/js"
)

// devApp is an application root for Dev: the generated files written by hand, a page component in
// TSX, the SSR host patches, and a Vite config with the tifa plugin and @vitejs/plugin-react.
func devApp(t *testing.T) string {
	t.Helper()
	modules := testModules(t)
	host, err := js.Runtime.ReadFile("runtime/host.js")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"vite.config.ts": "import react from '@vitejs/plugin-react'\nimport tifa from './.tifa/vite.js'\n" +
			"export default { plugins: [tifa(), react()] }\n",
		"app/page/view.tsx": "export default function View({ name }: { name: string }) {\n" +
			"  return <ul>{['a', 'b'].map((x) => <li key={x}>{name + x}</li>)}</ul>\n}\n",
		".tifa/runtime/host.js":   string(host),
		".tifa/runtime/client.ts": "export const clientOnly = 1\n",
		".tifa/client.ts":         "import View from '../app/page/view'\nconsole.log(View)\n",
		".tifa/server.ts": "import './runtime/host.js'\nimport { renderToString } from 'react-dom/server.edge'\n" +
			"import { createElement } from 'react'\nimport View from '../app/page/view'\n" +
			"(globalThis as any).result = process.env.NODE_ENV + ' ' + renderToString(createElement(View, { name: 'n' }))\n",
		".tifa/vite/plugin.js": js.VitePlugin,
		".tifa/vite.js":        "import { tifa } from './vite/plugin.js'\nexport default () => tifa()\n",
	})
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	return root
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// nodeChildren counts the node processes this test process started that are still running.
func nodeChildren(t *testing.T) int {
	t.Helper()
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep is not installed")
	}
	out, err := exec.Command("pgrep", "-P", strconv.Itoa(os.Getpid()), "-x", "node").Output()
	if err != nil && len(out) > 0 {
		t.Fatal(err)
	}
	return len(strings.Fields(string(out)))
}

// tifa dev runs Vite's dev server and the SSR builds on one Node process. The SSR bundle is still
// a production build: the browser modules use the development build of React, QuickJS the
// production one, and the page's JSX renders with it.
func TestDevRunsOneNodeProcess(t *testing.T) {
	root := devApp(t)
	before := nodeChildren(t)
	d, err := NewDev(root, freePort(t), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if n := nodeChildren(t) - before; n != 1 {
		t.Errorf("tifa dev runs %d Node processes, want 1", n)
	}
	code, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(code), "react-dom-server-legacy.browser.production.js") {
		t.Error("the production string renderer is not bundled")
	}
	if _, err := os.Stat(filepath.Join(root, ".tifa", "dist", "ssr.js.map")); !os.IsNotExist(err) {
		t.Errorf("the source map of the SSR bundle is written: %v", err)
	}
	if got, want := evalResult(t, string(code)), "production <ul><li>na</li><li>nb</li></ul>"; got != want {
		t.Errorf("SSR bundle gives %q, want %q", got, want)
	}
}

// tifa build makes a production build whatever NODE_ENV the shell has, so the JSX of the SSR
// bundle matches the React build in it.
func TestAppIgnoresInheritedNodeEnv(t *testing.T) {
	t.Setenv("NODE_ENV", "development")
	root := devApp(t)
	n := NewNode(root, io.Discard)
	defer n.Close()
	if _, err := App(n, root, filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("App: %v", err)
	}
	code, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := evalResult(t, string(code)), "production <ul><li>na</li><li>nb</li></ul>"; got != want {
		t.Errorf("SSR bundle gives %q, want %q", got, want)
	}
}

// tifa dev runs Vite's dev server in development whatever NODE_ENV the shell has, so pages get
// React Fast Refresh, and the SSR bundle is a production build that renders.
func TestDevIgnoresInheritedNodeEnv(t *testing.T) {
	for _, env := range []string{"production", "development"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("NODE_ENV", env)
			root := devApp(t)
			d, err := NewDev(root, freePort(t), io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if _, err := d.Build(filepath.Join(root, "public"), true); err != nil {
				t.Fatalf("Build: %v", err)
			}
			if !strings.Contains(d.head, "@react-refresh") {
				t.Errorf("the dev server runs without React Fast Refresh: %s", d.head)
			}
			code, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "ssr.js"))
			if err != nil {
				t.Fatal(err)
			}
			if got, want := evalResult(t, string(code)), "production <ul><li>na</li><li>nb</li></ul>"; got != want {
				t.Errorf("SSR bundle gives %q, want %q", got, want)
			}
		})
	}
}

// An image a page imports gets the URL of Vite's dev server in the server render too, the one
// the browser modules use. A build inlines a small PNG, the dev server does not.
func TestDevSSRUsesTheDevServerURLsOfAssets(t *testing.T) {
	root := devApp(t)
	small := "\x89PNG small"
	writeTree(t, root, map[string]string{
		"app/page/small.png": small,
		"app/page/large.svg": `<svg xmlns="http://www.w3.org/2000/svg"><!--` + strings.Repeat("x", 8<<10) + `--></svg>`,
		".tifa/server.ts": "import small from '../app/page/small.png'\nimport large from '../app/page/large.svg'\n" +
			"(globalThis as any).result = small + ' ' + large\n",
	})
	port := freePort(t)
	d, err := NewDev(root, port, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	code, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	got := evalResult(t, string(code))
	if want := DevBase + "app/page/small.png " + DevBase + "app/page/large.svg"; got != want {
		t.Fatalf("SSR bundle gives %q, want %q", got, want)
	}
	res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%sapp/page/small.png", port, DevBase))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(body) != small {
		t.Errorf("the dev server answers %d %q", res.StatusCode, body)
	}
}

// The head of a dev page links the stylesheets the server render imports, from Vite's dev
// server, so the page is styled before its scripts run, as a built page is. Vite's client knows
// the links by data-vite-dev-id and updates them in place of adding a second copy.
func TestDevHeadLinksTheStylesheetsOfTheServerRender(t *testing.T) {
	root := devApp(t)
	writeTree(t, root, map[string]string{
		"app/page/page.css": ".page { color: red }\n",
		"app/page/view.tsx": "import './page.css'\nexport default function View() { return <p className=\"page\">p</p> }\n",
		".tifa/server.ts": "declare const __TIFA_ASSETS__: string\nimport View from '../app/page/view'\n" +
			"(globalThis as any).result = View + __TIFA_ASSETS__\n",
	})
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	d, err := NewDev(root, port, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	link := `<link rel="stylesheet" href="` + DevBase + `app/page/page.css" data-vite-dev-id="` +
		filepath.Join(real, "app", "page", "page.css") + `">`
	if head := readBuild(t, root).Head; !strings.HasPrefix(head, link) {
		t.Errorf("build.json head %s, want it to start with %s", head, link)
	}
	code, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	if got := evalResult(t, string(code)); !strings.Contains(got, link) {
		t.Errorf("the SSR bundle has the head %s, want %s in it", got, link)
	}
	// A browser asks for a stylesheet with this Accept header, which tells Vite to send CSS.
	req, err := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d%sapp/page/page.css", port, DevBase), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/css,*/*;q=0.1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(body), "color: red") ||
		!strings.HasPrefix(res.Header.Get("Content-Type"), "text/css") {
		t.Errorf("the dev server answers %d %s %q", res.StatusCode, res.Header.Get("Content-Type"), body)
	}
}

// aliasApp is devApp with the tsconfig.json of a tifa application, whose paths send @/components
// somewhere other than @/ does: the SSR bundle reports which message module it got. A shadcn
// component outside app/ also imports through @/.
func aliasApp(t *testing.T) string {
	t.Helper()
	root := devApp(t)
	writeTree(t, root, map[string]string{
		"tsconfig.json": `{"extends": "./.tifa/tsconfig.json", "compilerOptions": {"paths": {` +
			`"@/*": ["./*"], "@/components/*": ["./custom/*"], "tifa/client": ["./.tifa/runtime/client.ts"]}},` +
			` "include": ["**/*", ".tifa/**/*"]}`,
		".tifa/tsconfig.json":   `{"compilerOptions": {"module": "ESNext", "moduleResolution": "bundler", "jsx": "react-jsx"}}`,
		"custom/message.ts":     "export default 'tsconfig alias'\n",
		"components/message.ts": "export default 'root alias'\n",
		"lib/utils.ts":          "export const join = (a: string, b: string) => a + ' ' + b\n",
		"shadcn/ui/badge.ts":    "import { join } from '@/lib/utils'\nexport default join('badge', 'ok')\n",
		"app/page/view.tsx":     "import message from '@/components/message'\nimport badge from '@/shadcn/ui/badge'\nimport { clientOnly } from 'tifa/client'\nexport default message + ', ' + badge + ', ' + clientOnly\n",
		".tifa/client.ts":       "import view from '../app/page/view'\nconsole.log(view)\n",
		".tifa/server.ts":       "import view from '../app/page/view'\n(globalThis as any).result = view\n",
	})
	return root
}

const aliasResult = "tsconfig alias, badge ok, 1"

// The paths of the application's tsconfig.json decide what @/ and tifa/client import, in the
// browser bundle and the SSR bundle of tifa build, as they do for TypeScript.
func TestAppResolvesImportsByTheTsconfigPaths(t *testing.T) {
	root := aliasApp(t)
	n := NewNode(root, io.Discard)
	defer n.Close()
	if _, err := App(n, root, filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("App: %v", err)
	}
	code, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	if got := evalResult(t, string(code)); got != aliasResult {
		t.Errorf("SSR bundle gives %q, want %q", got, aliasResult)
	}
	browser, err := filepath.Glob(filepath.Join(root, ".tifa", "dist", "assets", "*.js"))
	if err != nil || len(browser) == 0 {
		t.Fatalf("no browser bundle: %v", err)
	}
	var all string
	for _, f := range browser {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		all += string(data)
	}
	if !strings.Contains(all, "tsconfig alias") || strings.Contains(all, "root alias") {
		t.Errorf("the browser bundle does not import custom/message.ts")
	}
}

// Under tifa dev the SSR bundle and the modules of Vite's dev server follow the tsconfig paths.
func TestDevResolvesImportsByTheTsconfigPaths(t *testing.T) {
	root := aliasApp(t)
	port := freePort(t)
	d, err := NewDev(root, port, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	code, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	if got := evalResult(t, string(code)); got != aliasResult {
		t.Errorf("SSR bundle gives %q, want %q", got, aliasResult)
	}
	res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%sapp/page/view.tsx", port, DevBase))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(body), DevBase+"custom/message.ts") {
		t.Errorf("the dev server answers %d:\n%s", res.StatusCode, body)
	}
}

// tifa dev checks the unminified bundle the same way: a require in code that never runs is
// dropped, one that stays fails the build at its place in the source.
func TestDevChecksTheRequiresLeftInTheBundle(t *testing.T) {
	root := devApp(t)
	writeTree(t, root, map[string]string{
		"app/page/reader.cjs": "module.exports = function (name) {\n  if (false) return require(name).sep\n  return '/'\n}\n",
		".tifa/server.ts": "import './runtime/host.js'\nimport read from '../app/page/reader.cjs'\n" +
			"(globalThis as any).result = read('node:path')\n",
	})
	d, err := NewDev(root, freePort(t), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	code, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	if got := evalResult(t, string(code)); got != "/" {
		t.Fatalf("SSR bundle gives %q", got)
	}

	writeTree(t, root, map[string]string{
		"app/page/reader.cjs": "module.exports = function (name) {\n  if (name) return require(name).sep\n  return '/'\n}\n",
	})
	_, err = d.Build(filepath.Join(root, "public"), true)
	if err == nil || !strings.Contains(err.Error(), filepath.Join("app", "page", "reader.cjs")+":2:20") {
		t.Fatalf("Build: %v, want the place of the require", err)
	}
}
