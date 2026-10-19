package bundle

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
)

// placesEntry is an SSR entry whose startRender renders the page its URL names and reports the
// error the render throws, as the runtime of tifa does. Each page makes React DOM throw at
// another place of its string renderer: near its start, in its hooks and where it renders a
// child.
const placesEntry = "import './runtime/host.js'\n" +
	"import { renderToString } from 'react-dom/server.edge'\n" +
	"import { createElement as h, use } from 'react'\n" +
	"function Use() {\n  return use(42 as any)\n}\n" +
	"const pages: Record<string, () => any> = {\n" +
	"  style: () => h('div', { style: 'color: red' }),\n" +
	"  use: () => h(Use),\n" +
	"  object: () => h('div', null, {} as any),\n" +
	"}\n" +
	";(globalThis as any).startRender = (json: string) => {\n" +
	"  try {\n" +
	"    ;(globalThis as any).renderResult = '<' + renderToString(pages[JSON.parse(json).url]())\n" +
	"  } catch (error: any) {\n" +
	"    ;(globalThis as any).renderResult = JSON.stringify({ error: `${error.name}: ${error.message}\\n${error.stack}` })\n" +
	"  }\n" +
	"}\n"

// rendererPlace finds a place in React's string renderer in an explained stack trace.
var rendererPlace = regexp.MustCompile(`react-dom-server-legacy\.browser\.production\.js:(\d+):\d+`)

// The places in React DOM that the stack traces of render errors give are those of its file:
// the tifa plugin changes the string renderer, but leaves each of its lines where it was.
func TestSSRGivesThePlacesOfReactDOMInRenderErrors(t *testing.T) {
	root := devApp(t)
	writeTree(t, root, map[string]string{filepath.Join(".tifa", "server.ts"): placesEntry})
	buildApp(t, root)
	pool := appPool(t, root)
	renderer, err := os.ReadFile(filepath.Join(testModules(t), "react-dom", "cjs", "react-dom-server-legacy.browser.production.js"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(renderer), "\n")
	for page, want := range map[string]string{
		"style":  "throw Error(formatProdErrorMessage(62));",
		"use":    "throw Error(formatProdErrorMessage(438, String(usable)));",
		"object": "throw Error(",
	} {
		_, err := pool.Render(context.Background(), ssr.Input{URL: page})
		if err == nil {
			t.Errorf("%s: the render does not fail", page)
			continue
		}
		m := rendererPlace.FindStringSubmatch(err.Error())
		if m == nil {
			t.Errorf("%s: the error names no place in React DOM:\n%v", page, err)
			continue
		}
		line, _ := strconv.Atoi(m[1])
		if line < 1 || line > len(lines) || !strings.Contains(lines[line-1], want) {
			t.Errorf("%s: the error names line %d of React DOM, which does not have %q:\n%v", page, line, want, err)
		}
	}
}

// appPool loads the SSR bundle that buildApp built in root into a pool of one VM, with its source
// map.
func appPool(t *testing.T, root string) *ssr.Pool {
	t.Helper()
	dist := filepath.Join(root, ".tifa", "dist")
	code, err := os.ReadFile(filepath.Join(dist, "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	sourceMap, err := os.ReadFile(filepath.Join(dist, "ssr.js.map"))
	if err != nil {
		t.Fatal(err)
	}
	pool, err := ssr.NewPool(string(code), sourceMap, config.SSR{VMs: 1, VMMemory: 64 << 20, VMStackDepth: config.Default().SSR.VMStackDepth, Timeout: 5 * time.Second}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}
