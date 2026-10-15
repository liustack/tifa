package bundle

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// buildScript builds entry of root like an SSR bundle with the tifa plugin and fails the test on
// an error.
func buildScript(t *testing.T, root, entry string) (string, []string) {
	t.Helper()
	code, modules, err := script(testdata(t), root, entry, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return code, modules
}

// buildPrinting builds entry of root like buildScript and returns the code and what the build
// printed, its warnings among it.
func buildPrinting(t *testing.T, root, entry string) (string, string) {
	t.Helper()
	var output strings.Builder
	code, _, err := script(testdata(t), root, entry, &output)
	if err != nil {
		t.Fatal(err)
	}
	return code, output.String()
}

func hasModule(modules []string, suffix string) bool {
	return slices.ContainsFunc(modules, func(m string) bool { return strings.HasSuffix(m, suffix) })
}

func TestSSRTrimmedReactRendersOnlyTheStringRenderer(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	code, modules := buildScript(t, root, "render.tsx")
	if hasModule(modules, "react-dom-server.edge.production.js") {
		t.Error("the stream renderer is bundled")
	}
	if !hasModule(modules, "react-dom-server-legacy.browser.production.js") {
		t.Errorf("the string renderer is missing: %v", modules)
	}
	html := evalResult(t, code)
	if !strings.Contains(html, "&lt;script&gt;订单 &amp; 🙂&lt;/script&gt;") {
		t.Errorf("text not escaped: %s", html)
	}
	if !strings.Contains(html, "<!--$-->") {
		t.Errorf("Suspense boundary marker missing: %s", html)
	}
}

// renderToString leaves a Suspense boundary whose content threw to the browser and tells no one
// of the error. The tifa plugin gives it the onError option of React's other renderers, so the
// server can report the error.
func TestSSRReportsTheErrorAComponentThrowsInsideSuspense(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"throws.tsx": "import './host'\n" +
		"import { renderToString } from 'react-dom/server.edge'\nimport { Suspense } from 'react'\n" +
		"const errors: unknown[] = []\n" +
		"function Broken(): never {\n  throw new Error('BROKEN')\n}\n" +
		"const html = renderToString(<Suspense fallback=\"loading\"><Broken /></Suspense>, { onError: (e: unknown) => errors.push(e) } as any)\n" +
		"globalThis.result = html + ' ' + errors.map((e) => (e as Error).message).join()\n",
	})
	code, _ := buildScript(t, root, "throws.tsx")
	if got := evalResult(t, code); !strings.HasPrefix(got, "<!--$!-->") || !strings.HasSuffix(got, " BROKEN") {
		t.Errorf("result = %q, want the boundary left to the browser and the error", got)
	}
}

// A render inside a render reports its errors to its own onError, and to no one without one, as
// React's other renderers do. The outer render gets only its own.
func TestSSRReportsTheErrorsOfEachRenderToItsOwnOnError(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"nested.tsx": "import './host'\n" +
		"import { renderToString, renderToStaticMarkup } from 'react-dom/server.edge'\nimport { Suspense } from 'react'\n" +
		"const outer: Error[] = [], inner: Error[] = []\n" +
		"function Inner(): never { throw new Error('INNER') }\n" +
		"function Outer(): never {\n" +
		"  renderToString(<Suspense fallback=\"a\"><Inner /></Suspense>)\n" +
		"  renderToStaticMarkup(<Suspense fallback=\"b\"><Inner /></Suspense>, { onError: (e: Error) => inner.push(e) } as any)\n" +
		"  throw new Error('OUTER')\n}\n" +
		"renderToString(<Suspense fallback=\"c\"><Outer /></Suspense>, { onError: (e: Error) => outer.push(e) } as any)\n" +
		"renderToString(<Suspense fallback=\"d\"><Inner /></Suspense>)\n" +
		"globalThis.result = [outer, inner].map((l) => l.map((e) => e.message).join()).join(' ')\n",
	})
	code, _ := buildScript(t, root, "nested.tsx")
	if got := evalResult(t, code); got != "OUTER INNER" {
		t.Errorf("result = %q, want OUTER INNER", got)
	}
}

func TestSSRTrimmedReactKeepsEveryPublicExport(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	const want = "renderToReadableStream,renderToStaticMarkup,renderToString,resume,version"
	for _, entry := range []string{"exports-namespace.ts", "exports-default.ts"} {
		code, _ := buildScript(t, root, entry)
		if got := evalResult(t, code); got != want {
			t.Errorf("%s: exports %q, want %q", entry, got, want)
		}
	}
}

func TestSSRRejectsNodeAPIsInPageCode(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	for _, entry := range []string{"node-prefixed.ts", "node-bare.ts"} {
		_, _, err := script(testdata(t), root, entry, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "Node APIs are not supported in page code") {
			t.Errorf("%s: err = %v, want Node API rejection", entry, err)
		}
	}
}

// A dependency that still loads a Node module in the bundle gets a warning at the place it does,
// with its package, whether it requires the module or imports it. The build goes on: the module
// may never load on the server. A require that a typeof require test guards gets none: QuickJS
// takes the other branch.
func TestSSRWarnsOfNodeAPIsLeftInDependencies(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"node_modules/uses-fs/package.json":    `{"name":"uses-fs","main":"index.js"}`,
		"node_modules/uses-fs/index.js":        `const fs = require('fs'); module.exports = (p) => fs.readFileSync(p)`,
		"node_modules/esm-fs/package.json":     `{"name":"esm-fs","type":"module","main":"index.js"}`,
		"node_modules/esm-fs/index.js":         "import { readFileSync } from 'node:fs'\nexport const read = (p) => readFileSync(p)\n",
		"esm-fs.ts":                            "import { read } from 'esm-fs'\nglobalThis.result = String(read)\n",
		"node_modules/guarded-fs/package.json": `{"name":"guarded-fs","main":"index.js"}`,
		"node_modules/guarded-fs/index.js": "module.exports = function (name) {\n" +
			"  return typeof require === 'function' ? require('fs').existsSync(name) : 'safe'\n}\n",
		"guarded-fs.ts": "import exists from 'guarded-fs'\nglobalThis.result = exists('x')\n",
	})
	want := filepath.Join("node_modules", "uses-fs", "index.js") + `:1:12 (uses-fs): require("fs") loads a Node module`
	if _, got := buildPrinting(t, root, "uses-fs.ts"); !strings.Contains(got, want) {
		t.Errorf("uses-fs.ts: the build prints:\n%s\nwant %s", got, want)
	}
	if _, got := buildPrinting(t, root, "esm-fs.ts"); !strings.Contains(got, `require("node:fs") loads a Node module imported by esm-fs`) {
		t.Errorf("esm-fs.ts: the build prints:\n%s\nwant the import of node:fs", got)
	}
	code, got := buildPrinting(t, root, "guarded-fs.ts")
	if strings.Contains(got, "guarded-fs") {
		t.Errorf("guarded-fs.ts: the build prints:\n%s", got)
	}
	if got := evalResult(t, code); got != "safe" {
		t.Errorf("guarded require('fs') gives %q", got)
	}
}

// The SSR bundle takes the main files of packages and not their browser files. A browser file
// often takes what the browser has, as abort-controller takes AbortController from self, and
// QuickJS has none of it: the render would fail where the main file works. Conditional exports
// leave the browser condition out too, a package may export there code that renders only in the
// browser.
func TestSSRKeepsTheMainFilesOfPackages(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"node_modules/two-mains/package.json":   `{"name":"two-mains","main":"index.js","browser":"browser.js"}`,
		"node_modules/two-mains/index.js":       "module.exports = class { get name() { return 'main' } }\n",
		"node_modules/two-mains/browser.js":     "const { Thing } = typeof self !== 'undefined' ? self : undefined\nmodule.exports = Thing\n",
		"node_modules/conditional/package.json": `{"name":"conditional","exports":{".":{"browser":"./client.js","default":"./server.js"}}}`,
		"node_modules/conditional/client.js":    "export default 'client'\n",
		"node_modules/conditional/server.js":    "export default 'server'\n",
		"mains.ts": "import Thing from 'two-mains'\nimport which from 'conditional'\n" +
			"globalThis.result = new Thing().name + ',' + which\n",
	})
	code, _ := buildPrinting(t, root, "mains.ts")
	if got := evalResult(t, code); got != "main,server" {
		t.Errorf("result = %q, want main,server", got)
	}
}

func TestSSRDefinesProductionGlobals(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	code, _ := buildScript(t, root, "define.ts")
	if got := evalResult(t, code); got != "production:true" {
		t.Fatalf("result = %q, want production:true", got)
	}
	if !strings.HasPrefix(code, "(function(){") || strings.Contains(code, "\n  ") {
		t.Errorf("SSR bundle is not a minified IIFE: %.40s", code)
	}
}

func TestSSRSelectsCoreJSPropertyState(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	code, modules := buildScript(t, root, "url.ts")
	if hasModule(modules, "core-js/internals/weak-map-basic-detection.js") {
		t.Error("core-js WeakMap detection is still bundled")
	}
	if !slices.Contains(modules, "\x00tifa-core-js-property-state") {
		t.Errorf("core-js property state adapter not applied: %v", modules)
	}
	if got := evalResult(t, code); got != "SH-1042" {
		t.Fatalf("URL polyfill result = %q", got)
	}
}

// fakeCoreJS installs a core-js of version at dir whose internal state module is state.
func fakeCoreJS(t *testing.T, root, dir, version, state string) {
	t.Helper()
	writeTree(t, root, map[string]string{
		dir + "/package.json":                          `{"name":"core-js","version":"` + version + `"}`,
		dir + "/internals/internal-state.js":           state,
		dir + "/internals/weak-map-basic-detection.js": "module.exports = true",
	})
}

const coreJSState = "module.exports = require('../internals/weak-map-basic-detection') ? 'weak map' : 'properties'"

// The core-js adapter does not depend on the version: as long as the internal state module loads
// the WeakMap detection, it gets the property-backed state.
func TestSSRAdaptsCoreJSOfOtherVersions(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	fakeCoreJS(t, root, "node_modules/core-js", "3.99.0", coreJSState)
	var output strings.Builder
	_, modules, err := script(testdata(t), root, "core-js-state.ts", &output)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(modules, "\x00tifa-core-js-property-state") {
		t.Errorf("core-js property state adapter not applied: %v", modules)
	}
	if strings.Contains(output.String(), "core-js") {
		t.Errorf("warning for an adapted core-js:\n%s", output.String())
	}
}

// A core-js whose internal state no longer loads the WeakMap detection builds as it is, and the
// build says that the adapter did not apply.
func TestSSRWarnsWhenTheCoreJSAdapterDoesNotApply(t *testing.T) {
	for name, dir := range map[string]string{"installed": "node_modules/core-js", "linked": "vendor/core-js"} {
		t.Run(name, func(t *testing.T) {
			root := copyTestdata(t, "ssr", false)
			if name == "linked" {
				linkCoreJS(t, root)
			}
			fakeCoreJS(t, root, dir, "4.0.0", "module.exports = 'new state'")
			var output strings.Builder
			if _, _, err := script(testdata(t), root, "core-js-state.ts", &output); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "core-js 4.0.0") || !strings.Contains(output.String(), "WeakMap") {
				t.Errorf("no warning about the core-js adapter:\n%s", output.String())
			}
		})
	}
}

// fakeReactDOM installs a copy of the react-dom of testdata with version and server.edge.js
// changed, next to the react of testdata.
func fakeReactDOM(t *testing.T, root, version, entry string) {
	t.Helper()
	real, err := filepath.EvalSymlinks(filepath.Join(testModules(t), "react-dom"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "node_modules", "react-dom")
	if err := os.CopyFS(dir, os.DirFS(real)); err != nil {
		t.Fatal(err)
	}
	pkg, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	pkg = regexp.MustCompile(`"version": "[^"]+"`).ReplaceAll(pkg, []byte(`"version": "`+version+`"`))
	writeTree(t, root, map[string]string{"node_modules/react-dom/package.json": string(pkg)})
	if entry != "" {
		writeTree(t, root, map[string]string{"node_modules/react-dom/server.edge.js": entry})
	}
	if err := os.Symlink(filepath.Join(testModules(t), "react"), filepath.Join(root, "node_modules", "react")); err != nil {
		t.Fatal(err)
	}
}

// The renderer trimming reads the form of react-dom/server.edge.js, not its version or bytes.
func TestSSRTrimsReactDOMOfOtherVersions(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(testModules(t), "react-dom", "server.edge.js"))
	if err != nil {
		t.Fatal(err)
	}
	root := copyTestdata(t, "ssr", false)
	fakeReactDOM(t, root, "19.4.0", "// another release\n"+string(source))
	var output strings.Builder
	code, modules, err := script(testdata(t), root, "react-dom-server.ts", &output)
	if err != nil {
		t.Fatal(err)
	}
	if hasModule(modules, "react-dom-server.edge.production.js") || !hasModule(modules, "react-dom-server-legacy.browser.production.js") {
		t.Errorf("not trimmed to the string renderer: %v", modules)
	}
	if got := evalResult(t, code); got != "function" {
		t.Errorf("result = %q", got)
	}
	if strings.Contains(output.String(), "react-dom") {
		t.Errorf("warning for a trimmed react-dom:\n%s", output.String())
	}
}

// A server.edge.js of a form the trimming does not know is bundled as it is, with both renderers,
// and the build says so. The stream renderer needs the host patches to run, which the shop's
// bundle has: TestShop* in internal/ssr renders such a bundle.
func TestSSRKeepsAReactDOMItCannotTrim(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	fakeReactDOM(t, root, "20.0.0", "'use strict';\n"+
		"const l = require('./cjs/react-dom-server-legacy.browser.production.js');\n"+
		"const b = require('./cjs/react-dom-server.edge.production.js');\n"+
		"Object.assign(exports, { renderToString: l.renderToString, renderToReadableStream: b.renderToReadableStream });\n")
	var output strings.Builder
	_, modules, err := script(testdata(t), root, "react-dom-server.ts", &output)
	if err != nil {
		t.Fatal(err)
	}
	if !hasModule(modules, "react-dom-server.edge.production.js") || !hasModule(modules, "react-dom-server-legacy.browser.production.js") {
		t.Errorf("not both renderers: %v", modules)
	}
	if !strings.Contains(output.String(), "react-dom 20.0.0") {
		t.Errorf("no warning about the untrimmed react-dom:\n%s", output.String())
	}
}

// A require whose module name is known only at run time stays in the bundle, and QuickJS has no
// require: the build warns of it at its place, with its package.
func TestSSRWarnsOfRequireLeftInDependencies(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"node_modules/dynamic-reader/package.json": `{"name":"dynamic-reader","main":"index.cjs"}`,
		"node_modules/dynamic-reader/index.cjs":    "module.exports = function (name) {\n  return require(name).sep\n}\n",
		"dynamic.ts":                               "import read from 'dynamic-reader'\nglobalThis.result = read('node:path')\n",
	})
	want := filepath.Join("node_modules", "dynamic-reader", "index.cjs") + ":2:10 (dynamic-reader): require loads a module"
	if _, got := buildPrinting(t, root, "dynamic.ts"); !strings.Contains(got, want) {
		t.Fatalf("the build prints:\n%s\nwant %s", got, want)
	}
}

// A require declared in a block is bound only in that block: a require after it is still the
// host's. The column counts UTF-16 units, as in the source map.
func TestSSRWarnsOfRequireOutsideTheBlockThatDeclaresIt(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"node_modules/dynamic-reader/package.json": `{"name":"dynamic-reader","main":"index.cjs"}`,
		"node_modules/dynamic-reader/index.cjs": "module.exports = function (name) {\n" +
			"  { const require = value => value; globalThis.label = require('本地标签') }\n" +
			"  try { name.x() } catch (require) {}\n" +
			"  return require(name).sep\n}\n",
		"dynamic.ts": "import read from 'dynamic-reader'\nglobalThis.result = read('node:path')\n",
	})
	_, got := buildPrinting(t, root, "dynamic.ts")
	if want := filepath.Join("dynamic-reader", "index.cjs") + ":4:10"; !strings.Contains(got, want) || strings.Count(got, "index.cjs:") != 1 {
		t.Fatalf("the build prints:\n%s\nwant one warning at %s", got, want)
	}
}

// No warning for a require the code never reaches in QuickJS: one in dead code such as the branch
// of an if (false), one that a typeof require test guards, as UMD and isomorphic packages write
// it, and one the module declares itself, as a bundle made by browserify does.
func TestSSRDoesNotWarnOfRequireThatNeverRuns(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"node_modules/guarded/package.json": `{"name":"guarded","main":"index.cjs"}`,
		"node_modules/guarded/index.cjs": "module.exports = function (name) {\n" +
			"  if (false) return require(name).sep\n" +
			"  var a = typeof require === 'function' ? require(name) : 'a'\n" +
			"  var b = 'b'\n  if (typeof require !== 'undefined') b = require(name)\n" +
			"  var c = (typeof require == 'function' && require(name)) || 'c'\n" +
			"  var d = typeof require === 'undefined' || require(name) ? 'd' : 'x'\n" +
			"  var e = !(typeof require === 'function') ? 'e' : require(name)\n" +
			"  var g = 'function' === typeof module && typeof require == 'function' ? require(name) : 'g'\n" +
			"  return a + b + c + d + e + early(name) + g + own\n}\n" +
			"function early(name) {\n  if (typeof require === 'undefined') return 'f'\n  console.log(name)\n  return require(name)\n}\n" +
			"var load = (function (require) { return function (n) { return require(n) } })(function (n) { return n + '!' })\n" +
			"function local(name) { var require = function (n) { return n + '?' }; return require(name) }\n" +
			"var own = load('h') + local('i')\n",
		"guarded.ts": "import read from 'guarded'\nglobalThis.result = read('node:path')\n",
	})
	code, output := buildPrinting(t, root, "guarded.ts")
	if strings.Contains(output, "guarded") {
		t.Errorf("the build prints:\n%s", output)
	}
	if got := evalResult(t, code); got != "abcdefgh!i?" {
		t.Fatalf("result = %q", got)
	}
}

// A require next to a typeof test that does not guard it still gets a warning.
func TestSSRWarnsOfRequireTheTypeofTestDoesNotGuard(t *testing.T) {
	for name, body := range map[string]string{
		"other variable": "  return typeof define === 'function' ? require(name) : 'x'\n",
		"after the test": "  if (typeof require === 'function') console.log(name)\n  return require(name)\n",
		"wrong branch":   "  return typeof require === 'undefined' ? require(name) : 'x'\n",
		"wrong operator": "  return typeof require == 'function' || require(name)\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := copyTestdata(t, "ssr", false)
			writeTree(t, root, map[string]string{
				"node_modules/unguarded/package.json": `{"name":"unguarded","main":"index.cjs"}`,
				"node_modules/unguarded/index.cjs":    "module.exports = function (name) {\n" + body + "}\n",
				"unguarded.ts":                        "import read from 'unguarded'\nglobalThis.result = read('node:path')\n",
			})
			if _, got := buildPrinting(t, root, "unguarded.ts"); !strings.Contains(got, filepath.Join("unguarded", "index.cjs")+":") {
				t.Fatalf("the build prints:\n%s\nwant the place of the require", got)
			}
		})
	}
}

func TestSSRStubsClientOnlyModules(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	code, modules := buildScript(t, root, "client-only.tsx")
	if hasModule(modules, "widget.client.tsx") {
		t.Fatal("client-only source bundled into SSR")
	}
	if got := evalResult(t, code); got != "Client-only module invoked during SSR" {
		t.Fatalf("result = %q", got)
	}
}

func TestSSRReportsMissingEntry(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	_, _, err := script(testdata(t), root, "missing.tsx", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "missing.tsx") {
		t.Fatalf("err = %v, want error naming the entry", err)
	}
}

// linkCoreJS installs core-js the way a vendored or linked package would be:
// node_modules/core-js is a symlink to a directory outside node_modules.
func linkCoreJS(t *testing.T, root string) string {
	t.Helper()
	vendor := filepath.Join(root, "vendor", "core-js")
	if err := os.MkdirAll(filepath.Join(root, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(vendor, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(vendor, filepath.Join(root, "node_modules", "core-js")); err != nil {
		t.Fatal(err)
	}
	return vendor
}

func TestSSRAdaptsSymlinkedCoreJS(t *testing.T) {
	real, err := filepath.EvalSymlinks(filepath.Join(testModules(t), "core-js"))
	if err != nil {
		t.Fatal(err)
	}
	root := copyTestdata(t, "ssr", false)
	vendor := linkCoreJS(t, root)
	if err := os.CopyFS(filepath.Join(vendor, "internals"), os.DirFS(filepath.Join(real, "internals"))); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(real, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{"vendor/core-js/package.json": string(data)})
	_, modules := buildScript(t, root, "core-js-state.ts")
	if !slices.Contains(modules, "\x00tifa-core-js-property-state") {
		t.Fatal("core-js property state adapter not applied to a symlinked core-js")
	}
	if hasModule(modules, "core-js/internals/weak-map-basic-detection.js") {
		t.Fatal("core-js WeakMap detection is still bundled")
	}
}

// manyRequires builds a bundle of a package with n requires and returns what the build printed
// and how long it took.
func manyRequires(t *testing.T, n int) (string, time.Duration) {
	t.Helper()
	root := copyTestdata(t, "ssr", false)
	var source strings.Builder
	for i := range n {
		fmt.Fprintf(&source, "exports.f%d = () => require(globalThis.n%d)\n", i, i)
	}
	writeTree(t, root, map[string]string{
		"node_modules/many/package.json": `{"name":"many","main":"index.js"}`,
		"node_modules/many/index.js":     source.String(),
		"many.ts":                        "import * as many from 'many'\nglobalThis.result = Object.keys(many).length\n",
	})
	start := time.Now()
	_, output := buildPrinting(t, root, "many.ts")
	return output, time.Since(start)
}

// Node may exit before it wrote all it printed to a pipe, and a long warning lost its end.
func TestSSRPrintsEveryWarning(t *testing.T) {
	const n = 20000
	output, _ := manyRequires(t, n)
	if got := strings.Count(output, " (many): require loads a module"); got != n {
		t.Errorf("%d warnings, want %d", got, n)
	}
	if !strings.Contains(output, fmt.Sprintf("node_modules/many/index.js:%d:", n)) {
		t.Errorf("the last require is not placed in its line:\n%.2000s", output)
	}
}
