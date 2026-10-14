package bundle

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// buildScript builds entry of root like an SSR bundle with the tifa plugin and fails the test on
// an error.
func buildScript(t *testing.T, root, entry string) (string, []string) {
	t.Helper()
	code, modules, err := script(testdata(t), root, entry)
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
		_, _, err := script(testdata(t), root, entry)
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
	_, _, err := script(testdata(t), root, "uses-fs.ts")
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

func TestSSRRejectsChangedCoreJS(t *testing.T) {
	real := filepath.Join(testModules(t), "core-js", "internals", "internal-state.js")
	source, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct{ version, state string }{
		"changed source":  {"3.50.0", "require('../internals/weak-map-basic-detection'); module.exports = {}"},
		"changed version": {"3.51.0", string(source)},
		// A file without imports must not slip past the check.
		"changed source without imports": {"3.50.0", "module.exports = {}"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			root := copyTestdata(t, "ssr", false)
			writeTree(t, root, map[string]string{
				"node_modules/core-js/package.json":                          `{"name":"core-js","version":"` + c.version + `"}`,
				"node_modules/core-js/internals/internal-state.js":           c.state,
				"node_modules/core-js/internals/weak-map-basic-detection.js": "module.exports = true",
			})
			_, _, err := script(testdata(t), root, "core-js-state.ts")
			if err == nil || !strings.Contains(err.Error(), "Review the QuickJS core-js compatibility adapter") {
				t.Fatalf("err = %v, want core-js review error", err)
			}
		})
	}
}

func TestSSRRejectsChangedReactDOM(t *testing.T) {
	real := filepath.Join(testModules(t), "react-dom", "server.edge.js")
	source, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct{ version, entry string }{
		"changed source":  {"19.3.0", string(source) + "\n"},
		"changed version": {"19.4.0", string(source)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			root := copyTestdata(t, "ssr", false)
			writeTree(t, root, map[string]string{
				"node_modules/react-dom/package.json":   `{"name":"react-dom","version":"` + c.version + `","exports":{"./server.edge":"./server.edge.js"}}`,
				"node_modules/react-dom/server.edge.js": c.entry,
			})
			_, _, err := script(testdata(t), root, "react-dom-server.ts")
			if err == nil || !strings.Contains(err.Error(), "Review the React SSR build adapter") {
				t.Fatalf("err = %v, want React review error", err)
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
	_, _, err := script(testdata(t), root, "missing.tsx")
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

func TestSSRChecksSymlinkedCoreJS(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	linkCoreJS(t, root)
	writeTree(t, root, map[string]string{
		"vendor/core-js/package.json":                          `{"name":"core-js","version":"3.50.0"}`,
		"vendor/core-js/internals/internal-state.js":           "require('../internals/weak-map-basic-detection'); module.exports = {}",
		"vendor/core-js/internals/weak-map-basic-detection.js": "module.exports = true",
	})
	_, _, err := script(testdata(t), root, "core-js-state.ts")
	if err == nil || !strings.Contains(err.Error(), "Review the QuickJS core-js compatibility adapter") {
		t.Fatalf("err = %v, want core-js review error", err)
	}
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
