package bundle

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
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

func TestSSRRejectsNodeAPIsLeftInDependencies(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"node_modules/uses-fs/package.json": `{"name":"uses-fs","main":"index.js"}`,
		"node_modules/uses-fs/index.js":     `const fs = require('fs'); module.exports = (p) => fs.readFileSync(p)`,
	})
	_, _, err := script(testdata(t), root, "uses-fs.ts", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "SSR bundle has unresolved runtime dependencies") {
		t.Fatalf("err = %v, want unresolved dependency error", err)
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
// require: the build fails and names the place.
func TestSSRRejectsRequireLeftInDependencies(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"node_modules/dynamic-reader/package.json": `{"name":"dynamic-reader","main":"index.cjs"}`,
		"node_modules/dynamic-reader/index.cjs":    "module.exports = function (name) {\n  return require(name).sep\n}\n",
		"dynamic.ts":                               "import read from 'dynamic-reader'\nglobalThis.result = read('node:path')\n",
	})
	_, _, err := script(testdata(t), root, "dynamic.ts", io.Discard)
	if err == nil || !strings.Contains(err.Error(), filepath.Join("dynamic-reader", "index.cjs")+":2:10") ||
		!strings.Contains(err.Error(), "require") {
		t.Fatalf("err = %v, want the place of the require", err)
	}
}

// A require the module declares itself, as a bundle made by browserify does, and a typeof
// require test are no require of the host.
func TestSSRAcceptsRequireTheModuleDeclares(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"node_modules/own-require/package.json": `{"name":"own-require","main":"index.js"}`,
		"node_modules/own-require/index.js": "var load = (function (require) { return function (n) { return require(n) } })(function (n) { return n + '!' })\n" +
			"function local(name) { var require = function (n) { return n + '?' }; return require(name) }\n" +
			"module.exports = load('a') + local('b') + (typeof require === 'function' ? 'node' : 'host')\n",
		"own.ts": "import value from 'own-require'\nglobalThis.result = value\n",
	})
	code, _ := buildScript(t, root, "own.ts")
	if got := evalResult(t, code); got != "a!b?host" {
		t.Fatalf("result = %q", got)
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
