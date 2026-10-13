package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
	"modernc.org/quickjs"
)

// shopModules is the prototype's installed npm dependencies. Tests that bundle
// React or core-js resolve them from there.
func shopModules(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "examples", "shop", "node_modules")
	if _, err := os.Stat(filepath.Join(dir, "react-dom", "package.json")); err != nil {
		t.Skipf("needs npm dependencies: run pnpm install --frozen-lockfile in examples/shop (%v)", err)
	}
	return dir
}

// app copies testdata/<name> into a fresh project root. With modules it links
// the prototype's node_modules into that root, as an application would have.
func app(t *testing.T, name string, modules bool) string {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join("testdata", name)
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if modules {
		if err := os.Symlink(shopModules(t), filepath.Join(root, "node_modules")); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, contents := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func ssrOptions(root, entry string) SSROptions {
	return SSROptions{
		Root:     root,
		Entry:    entry,
		Outfile:  "out/server.js",
		Tsconfig: filepath.Join(root, "tsconfig.json"),
	}
}

func buildSSRCode(t *testing.T, o SSROptions, trimReact bool) (string, metafile) {
	t.Helper()
	result, err := buildSSR(o, trimReact)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.OutputFiles) != 1 {
		t.Fatalf("SSR bundle has %d output files, want 1", len(result.OutputFiles))
	}
	var meta metafile
	if err := json.Unmarshal([]byte(result.Metafile), &meta); err != nil {
		t.Fatal(err)
	}
	return string(result.OutputFiles[0].Contents), meta
}

// evalResult runs a bundle in a fresh QuickJS VM, drains microtasks and
// returns globalThis.result.
func evalResult(t *testing.T, code string) string {
	t.Helper()
	vm, err := quickjs.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	if _, err := vm.Eval(code, quickjs.EvalGlobal); err != nil {
		t.Fatalf("evaluate bundle: %v", err)
	}
	if _, err := vm.ExecutePendingJobs(); err != nil {
		t.Fatal(err)
	}
	value, err := vm.Eval("String(globalThis.result)", quickjs.EvalGlobal)
	if err != nil {
		t.Fatal(err)
	}
	return value.(string)
}

func hasInput(meta metafile, suffix string) bool {
	for _, output := range meta.Outputs {
		for name := range output.Inputs {
			if strings.HasSuffix(name, suffix) {
				return true
			}
		}
	}
	return false
}

func TestSSRTrimmedReactRendersLikeFullReact(t *testing.T) {
	root := app(t, "ssr", true)
	var html []string
	for _, trimReact := range []bool{false, true} {
		code, meta := buildSSRCode(t, ssrOptions(root, "render.tsx"), trimReact)
		html = append(html, evalResult(t, code))
		if stream := hasInput(meta, "react-dom-server.edge.production.js"); stream == trimReact {
			t.Errorf("trimReact=%v: stream renderer bundled=%v", trimReact, stream)
		}
	}
	if html[0] != html[1] {
		t.Fatalf("trimmed React renders differently:\nfull:    %s\ntrimmed: %s", html[0], html[1])
	}
	if !strings.Contains(html[1], "&lt;script&gt;订单 &amp; 🙂&lt;/script&gt;") {
		t.Errorf("text not escaped: %s", html[1])
	}
	if !strings.Contains(html[1], "<!--$-->") {
		t.Errorf("Suspense boundary marker missing: %s", html[1])
	}
}

func TestSSRTrimmedReactKeepsEveryPublicExport(t *testing.T) {
	root := app(t, "ssr", true)
	const want = "renderToReadableStream,renderToStaticMarkup,renderToString,resume,version"
	for _, trimReact := range []bool{false, true} {
		code, _ := buildSSRCode(t, ssrOptions(root, "exports-namespace.ts"), trimReact)
		if got := evalResult(t, code); got != want {
			t.Errorf("trimReact=%v: namespace exports %q, want %q", trimReact, got, want)
		}
	}
	code, _ := buildSSRCode(t, ssrOptions(root, "exports-default.ts"), true)
	if got := evalResult(t, code); got != want {
		t.Errorf("default export keys %q, want %q", got, want)
	}
}

func TestSSRRejectsNodeAPIsInPageCode(t *testing.T) {
	root := app(t, "ssr", false)
	for _, entry := range []string{"node-prefixed.ts", "node-bare.ts"} {
		_, err := SSR(ssrOptions(root, entry))
		if err == nil || !strings.Contains(err.Error(), "Node APIs are not supported in page code") {
			t.Errorf("%s: err = %v, want Node API rejection", entry, err)
		}
	}
}

func TestSSRRejectsNodeAPIsLeftInDependencies(t *testing.T) {
	root := app(t, "ssr", false)
	writeFiles(t, root, map[string]string{
		"node_modules/uses-fs/package.json": `{"name":"uses-fs","main":"index.js"}`,
		"node_modules/uses-fs/index.js":     `const fs = require('fs'); module.exports = (p) => fs.readFileSync(p)`,
	})
	_, err := SSR(ssrOptions(root, "uses-fs.ts"))
	if err == nil || !strings.Contains(err.Error(), "SSR bundle has unresolved runtime dependencies") {
		t.Fatalf("err = %v, want unresolved dependency error", err)
	}
}

func TestSSRProfileCodeOnlyWhenRequested(t *testing.T) {
	root := app(t, "ssr", false)
	for _, profile := range []bool{false, true} {
		o := ssrOptions(root, "profile.ts")
		o.Profile = profile
		code, _ := buildSSRCode(t, o, true)
		for _, name := range []string{"__ssrNow", "renderProfile"} {
			if strings.Contains(code, name) != profile {
				t.Errorf("profile=%v: bundle contains %s = %v", profile, name, !profile)
			}
		}
	}
}

func TestSSRDefinesProductionGlobals(t *testing.T) {
	root := app(t, "ssr", false)
	code, _ := buildSSRCode(t, ssrOptions(root, "define.ts"), true)
	if got := evalResult(t, code); got != "production:true" {
		t.Fatalf("result = %q, want production:true", got)
	}
	if !strings.Contains(code, "(()=>{") || strings.Contains(code, "\n  ") {
		t.Errorf("SSR bundle is not a minified IIFE: %.40s", code)
	}
}

func TestSSRSelectsCoreJSPropertyState(t *testing.T) {
	root := app(t, "ssr", true)
	code, meta := buildSSRCode(t, ssrOptions(root, "url.ts"), true)
	if hasInput(meta, "core-js/internals/weak-map-basic-detection.js") {
		t.Error("core-js WeakMap detection is still bundled")
	}
	if !hasInput(meta, "quickjs-core-js:property-state") {
		t.Error("core-js property state adapter not applied")
	}
	if got := evalResult(t, code); got != "SH-1042" {
		t.Fatalf("URL polyfill result = %q", got)
	}
}

func TestSSRRejectsChangedCoreJS(t *testing.T) {
	real := filepath.Join(shopModules(t), "core-js", "internals", "internal-state.js")
	source, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct{ version, state string }{
		"changed source":  {"3.50.0", "require('../internals/weak-map-basic-detection'); module.exports = {}"},
		"changed version": {"3.51.0", string(source)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			root := app(t, "ssr", false)
			writeFiles(t, root, map[string]string{
				"node_modules/core-js/package.json":                          `{"name":"core-js","version":"` + c.version + `"}`,
				"node_modules/core-js/internals/internal-state.js":           c.state,
				"node_modules/core-js/internals/weak-map-basic-detection.js": "module.exports = true",
			})
			_, err := SSR(ssrOptions(root, "core-js-state.ts"))
			if err == nil || !strings.Contains(err.Error(), "Review the QuickJS core-js compatibility adapter") {
				t.Fatalf("err = %v, want core-js review error", err)
			}
		})
	}
}

func TestSSRRejectsChangedReactDOM(t *testing.T) {
	real := filepath.Join(shopModules(t), "react-dom", "server.edge.js")
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
			root := app(t, "ssr", false)
			writeFiles(t, root, map[string]string{
				"node_modules/react-dom/package.json":   `{"name":"react-dom","version":"` + c.version + `","exports":{"./server.edge":"./server.edge.js"}}`,
				"node_modules/react-dom/server.edge.js": c.entry,
			})
			_, err := SSR(ssrOptions(root, "react-dom-server.ts"))
			if err == nil || !strings.Contains(err.Error(), "Review the React SSR build adapter") {
				t.Fatalf("err = %v, want React review error", err)
			}
		})
	}
}

func TestSSRStubsClientOnlyModules(t *testing.T) {
	root := app(t, "ssr", false)
	code, meta := buildSSRCode(t, ssrOptions(root, "client-only.tsx"), true)
	if hasInput(meta, "widget.client.tsx") {
		t.Fatal("client-only source bundled into SSR")
	}
	if got := evalResult(t, code); got != "Client-only module invoked during SSR" {
		t.Fatalf("result = %q", got)
	}
}

func TestSSRRunsCallerPluginsFirst(t *testing.T) {
	root := app(t, "ssr", false)
	o := ssrOptions(root, "virtual.ts")
	o.Plugins = []api.Plugin{{
		Name: "virtual",
		Setup: func(b api.PluginBuild) {
			b.OnResolve(api.OnResolveOptions{Filter: `^virtual:answer$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: "answer", Namespace: "virtual"}, nil
			})
			b.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "virtual"}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
				contents := "export default 42"
				return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS}, nil
			})
		},
	}}
	code, _ := buildSSRCode(t, o, true)
	if got := evalResult(t, code); got != "42" {
		t.Fatalf("result = %q, want 42", got)
	}
}

func TestSSRWritesOutfile(t *testing.T) {
	root := app(t, "ssr", false)
	o := ssrOptions(root, "define.ts")
	o.Write = true
	if _, err := SSR(o); err != nil {
		t.Fatal(err)
	}
	code, err := os.ReadFile(filepath.Join(root, "out", "server.js"))
	if err != nil {
		t.Fatal(err)
	}
	if got := evalResult(t, string(code)); got != "production:true" {
		t.Fatalf("result = %q", got)
	}
}

func TestSSRReportsMissingEntry(t *testing.T) {
	root := app(t, "ssr", false)
	_, err := SSR(ssrOptions(root, "missing.tsx"))
	if err == nil || !strings.Contains(err.Error(), "missing.tsx") {
		t.Fatalf("err = %v, want error naming the entry", err)
	}
}
