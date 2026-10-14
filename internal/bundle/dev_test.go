package bundle

import (
	"io"
	"net"
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
