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
// child. On the pages app and nested the application throws, from a component React renders as the
// page and as the child of an element.
const placesEntry = "import './runtime/host.js'\n" +
	"import { renderToString } from 'react-dom/server.edge'\n" +
	"import { createElement as h, use } from 'react'\n" +
	"import * as React from 'react'\n" +
	"function Use() {\n  return use(42 as any)\n}\n" +
	"function App() {\n  React.useState(0)\n  throw new Error('APP')\n}\n" +
	"const pages: Record<string, () => any> = {\n" +
	"  style: () => h('div', { style: 'color: red' }),\n" +
	"  use: () => h(Use),\n" +
	"  object: () => h('div', null, {} as any),\n" +
	"  app: () => h(App),\n" +
	"  nested: () => h('main', null, h(App)),\n" +
	"}\n" +
	";(globalThis as any).startRender = (json: string) => {\n" +
	"  try {\n" +
	"    ;(globalThis as any).renderResult = '<' + renderToString(pages[JSON.parse(json).url]())\n" +
	"  } catch (error: any) {\n" +
	"    ;(globalThis as any).renderResult = JSON.stringify({ error: `${error.name}: ${error.message}\\n${error.stack}` })\n" +
	"  }\n" +
	"}\n"

// The places in the sources of the application that the stack traces of render errors give stay
// right though the tifa plugin changes the bundle after rolldown wrote it.
func TestSSRGivesThePlacesOfTheApplicationInRenderErrors(t *testing.T) {
	root := devApp(t)
	writeTree(t, root, map[string]string{filepath.Join(".tifa", "server.ts"): placesEntry})
	buildApp(t, root)
	_, err := appPool(t, root).Render(context.Background(), ssr.Input{URL: "app"})
	// Line 10, column 13 is the Error of throw new Error('APP'). The name of App is minified.
	if err == nil || !regexp.MustCompile(`Error: APP\n    at \S+ \(\.tifa/server\.ts:10:13\)\n`).MatchString(err.Error()) {
		t.Errorf("the error does not name the place of the throw in the application:\n%v", err)
	}
}

// rendererPlace finds a place in React's string renderer in an explained stack trace, with its
// line and column.
var rendererPlace = regexp.MustCompile(`react-dom-server-legacy\.browser\.production\.js:(\d+):(\d+)`)

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

// call is what a place of a frame of a stack trace starts with in React DOM: a call, which may be
// that of a return statement.
var call = regexp.MustCompile(`^(return )?[\w$.]+\(`)

// Each frame in React DOM of the stack trace of an error the application throws gives the place of
// a call in its file, those of the code the tifa plugin changes too: the plugin keeps each call on
// the line it was on.
func TestSSRGivesThePlacesOfCallsInReactDOM(t *testing.T) {
	root := devApp(t)
	writeTree(t, root, map[string]string{filepath.Join(".tifa", "server.ts"): placesEntry})
	buildApp(t, root)
	pool := appPool(t, root)
	renderer, err := os.ReadFile(filepath.Join(testModules(t), "react-dom", "cjs", "react-dom-server-legacy.browser.production.js"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(renderer), "\n")
	_, err = pool.Render(context.Background(), ssr.Input{URL: "nested"})
	if err == nil {
		t.Fatal("the render does not fail")
	}
	places := rendererPlace.FindAllStringSubmatch(err.Error(), -1)
	if len(places) < 5 {
		t.Fatalf("the error names %d places in React DOM, want the frames of the components and elements around the throw:\n%v", len(places), err)
	}
	for _, m := range places {
		line, _ := strconv.Atoi(m[1])
		column, _ := strconv.Atoi(m[2])
		if line > len(lines) {
			continue // the code the tifa plugin adds after React DOM
		}
		if column < 1 || column > len(lines[line-1]) || !call.MatchString(lines[line-1][column-1:]) {
			t.Errorf("the error names %s:%s of React DOM, which is no call: %q\n%v", m[1], m[2], lines[line-1], err)
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
	pool, err := ssr.NewPool(string(code), sourceMap, config.SSR{VMs: 1, VMMemory: 64 << 20, VMStackDepth: config.Default().SSR.VMStackDepth, Timeout: 5 * time.Second, QueueLimit: 32, WaitTimeout: time.Second}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}
