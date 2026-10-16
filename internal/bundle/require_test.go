package bundle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
)

// requireCase is one module of testdata/require-cases.json: a dependency that exports a function
// of a module name. Runs tells whether calling it in QuickJS, which has no require, runs, or
// fails because require is missing. A case may fail for another reason and still run in this
// sense. Post, when set, is code run on the result value.
type requireCase struct {
	Name string
	Code string
	Post string
	Runs bool
}

// requireLimits are the cases where the build warns otherwise than QuickJS runs, and why. The
// warning is a hint: most of these get one although they run, the rest none although they fail.
var requireLimits = map[string]string{
	// A require the code catches the error of: the check does not follow try.
	"try-catch-optional-fallback": "warns, a missing require only throws inside try",
	// The code creates require itself. The check takes typeof require to be "undefined", which
	// holds only while no code writes require, so a write counts as a use.
	"assign-free-read":       "warns, the code assigns require before it reads it",
	"assign-only":            "warns, the code assigns require",
	"assign-destructure":     "warns, the code assigns require",
	"for-in-assignment-free": "warns, the code assigns require",
	"for-of-assignment-free": "warns, the code assigns require",
	"global-assigned-read":   "warns, the code sets globalThis.require before it reads it",
	// The minifier keeps the names in code with a direct eval or with, so a require declared
	// there counts as the host's.
	"eval-bound":  "warns, eval declares require",
	"eval-shadow": "warns, a direct eval keeps the name of the local require",
	"with-local":  "warns, the require a with object gives is not declared",
	// The check takes typeof require to be "undefined" and does not follow code that sets
	// globalThis.require and removes it again.
	"guard-delete":                    "no warning, the code sets globalThis.require and deletes it",
	"guard-await-mutation":            "no warning, the code sets globalThis.require and deletes it",
	"guard-callback-mutation":         "no warning, the code sets globalThis.require and deletes it",
	"guard-mutation-conditional-test": "no warning, the code sets globalThis.require and deletes it",
	// Code in a string is not checked.
	"eval-free":  "no warning, the require is in a string that eval runs",
	"eval-alias": "no warning, the require is in a string that eval runs",
}

// unresolvedCase requires a package that is not installed. The bundler fails to resolve it, so it
// is built on its own.
const unresolvedCase = "guard-static-nonnode"

func loadRequireCases(t *testing.T) []requireCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "require-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []requireCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	return slices.DeleteFunc(cases, func(c requireCase) bool { return c.Name == unresolvedCase })
}

// writeRequireCases writes the cases as modules under root/vendor/node_modules/cases and an SSR
// entry, .tifa/server.ts, whose startRender calls the case its URL names, waits for a promise it
// returns, and renders the result, or reports the error as the runtime of tifa does. Each call
// ends by removing a require the case set on globalThis.
func writeRequireCases(t *testing.T, root string, cases []requireCase) {
	t.Helper()
	files := map[string]string{}
	var entry strings.Builder
	entry.WriteString("import './runtime/host.js'\n")
	for i, c := range cases {
		files[filepath.Join("vendor", "node_modules", "cases", c.Name+".cjs")] = c.Code
		fmt.Fprintf(&entry, "import c%d from '../vendor/node_modules/cases/%s.cjs'\n", i, c.Name)
	}
	entry.WriteString("const cases: Record<string, [(name: string) => any, (value: any) => any]> = {\n")
	for i, c := range cases {
		fmt.Fprintf(&entry, "  %q: [c%d, (value: any) => { %s; return value }],\n", c.Name, i, c.Post)
	}
	entry.WriteString("}\n" +
		";(globalThis as any).startRender = (json: string) => {\n" +
		"  const [run, post] = cases[JSON.parse(json).url]\n" +
		"  void (async () => {\n" +
		"    try {\n      ;(globalThis as any).renderResult = '<' + String(await post(run('node:path')))\n" +
		"    } catch (error: any) {\n" +
		"      ;(globalThis as any).renderResult = JSON.stringify({ error: `${error.name}: ${error.message}\\n${error.stack}` })\n" +
		"    } finally {\n      delete (globalThis as any).require\n    }\n  })()\n}\n")
	files[filepath.Join(".tifa", "server.ts")] = entry.String()
	writeTree(t, root, files)
}

// checkRequireCases checks, for each case, the warning of the build of root and the render of the
// case in QuickJS. A case that fails there because require is missing gets a warning at its
// place, and the render error says which package called require and what to do. One that runs
// gets no warning. requireLimits get the warning they say.
func checkRequireCases(t *testing.T, root string, cases []requireCase, output string) {
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
	pool, err := ssr.NewPool(string(code), sourceMap, config.SSR{VMs: 1, VMMemory: 64 << 20, Timeout: 5 * time.Second}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, c := range cases {
		file := "vendor/node_modules/cases/" + c.Name + ".cjs"
		_, err := pool.Render(context.Background(), ssr.Input{URL: c.Name})
		failed := err != nil && strings.Contains(err.Error(), "'require' is not defined")
		if failed == c.Runs {
			t.Errorf("%s renders with %v in QuickJS, want runs = %v", c.Name, err, c.Runs)
		} else if failed && !regexp.MustCompile(`The package cases calls require at `+regexp.QuoteMeta(file)+`:\d+:\d+\. SSR runs in QuickJS`).MatchString(err.Error()) {
			t.Errorf("%s: the render error does not name the place of the require:\n%v", c.Name, err)
		}
		_, limit := requireLimits[c.Name]
		// Rolldown warns of eval at the same place, in another form.
		place := regexp.MustCompile(regexp.QuoteMeta(file) + `:\d+:\d+ \(cases\): `)
		if warned := place.MatchString(output); warned != (c.Runs == limit) {
			t.Errorf("%s: warned = %v, want %v (runs = %v, limit %q)", c.Name, warned, !warned, c.Runs, requireLimits[c.Name])
		}
	}
}

// The build warns of each case of testdata/require-cases.json that fails in QuickJS because
// require is missing, and of none that runs, except requireLimits, and the render of a case that
// fails says why. Minified as tifa build makes it. The warning does not fail the build.
func TestSSRRequireCases(t *testing.T) {
	cases := loadRequireCases(t)
	root := devApp(t)
	writeRequireCases(t, root, cases)
	var output strings.Builder
	n := NewNode(root, &output)
	if _, err := App(n, root, filepath.Join(root, "public"), true); err != nil {
		t.Fatal(err)
	}
	// What Node printed is complete once it exited.
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	checkRequireCases(t, root, cases, output.String())

	root = copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"node_modules/cases/guarded.cjs": "module.exports = function (name) {\n" +
			"  return typeof require === 'function' ? require('unknown-optional-package') : 'safe'\n}\n",
		"unresolved.ts": "import read from 'cases/guarded.cjs'\nglobalThis.result = read('x')\n",
	})
	_, _, err := script(testdata(t), root, "unresolved.ts", io.Discard)
	if err == nil || !strings.Contains(err.Error(), `failed to resolve import "unknown-optional-package"`) {
		t.Errorf("a guarded require of a package that is not installed: %v, want the bundler's error", err)
	}
}

// The same cases, unminified as tifa dev builds them.
func TestDevRequireCases(t *testing.T) {
	cases := loadRequireCases(t)
	root := devApp(t)
	writeRequireCases(t, root, cases)
	var output strings.Builder
	d, err := NewDev(root, freePort(t), &output)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true); err != nil {
		t.Fatal(err)
	}
	// What Node printed is complete once it exited.
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	checkRequireCases(t, root, cases, output.String())
}

// A require that no source maps to, such as one a plugin appends to the chunk, gets a warning at
// its place in ssr.js, not at the source of the code before it.
func TestSSRReportsAnUnmappedRequireInTheBundle(t *testing.T) {
	root := devApp(t)
	writeTree(t, root, map[string]string{
		"vite.config.ts": "import react from '@vitejs/plugin-react'\nimport tifa from './.tifa/vite.js'\n" +
			"const digits = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/'\n" +
			"function vlq(n: number) {\n  let v = n < 0 ? (-n << 1) | 1 : n << 1, s = ''\n" +
			"  do { let d = v & 31; v >>>= 5; if (v) d |= 32; s += digits[d] } while (v)\n  return s\n}\n" +
			"const append = {\n  name: 'append-unmapped-require',\n  applyToEnvironment: (env: any) => env.name === 'ssr',\n" +
			"  renderChunk(code: string, chunk: any) {\n    const lines = code.trimEnd().split('\\n')\n" +
			"    // Each line maps to itself from column 0, and the appended code to no source.\n" +
			"    const mappings = lines.map((_, i) => 'AA' + (i ? 'C' : 'A') + 'A')\n" +
			"    mappings[mappings.length - 1] += ',' + vlq(lines[lines.length - 1].length)\n" +
			"    return { code: lines.join('\\n') + 'require(globalThis.name);', map: { version: 3, sources: [chunk.fileName], " +
			"sourcesContent: [code], names: [], mappings: mappings.join(';') } }\n  },\n}\n" +
			"export default { plugins: [tifa(), react(), append] }\n",
	})
	var output strings.Builder
	n := NewNode(root, &output)
	if _, err := App(n, root, filepath.Join(root, "public"), true); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "\nssr.js:") || strings.Contains(got, "server.ts:") {
		t.Fatalf("the build prints:\n%s\nwant the place in ssr.js", got)
	}
}

// The check takes typeof require to be "undefined" only where require is not declared. Here the
// test reads a local require, so the code goes on to the require of the host, which fails in
// QuickJS. Taking the local typeof to be "undefined" too would drop that require.
func TestSSRKeepsTheTypeofOfALocalRequire(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"node_modules/cases/local.cjs": "module.exports = function (name) {\n" +
			"  { let require = () => 1; if (typeof require === 'undefined') return 'safe' }\n" +
			"  return require(name)\n}\n",
		"local.ts": "import read from 'cases/local.cjs'\nglobalThis.result = read('x')\n",
	})
	var output strings.Builder
	if _, _, err := script(testdata(t), root, "local.ts", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), filepath.Join("cases", "local.cjs")+":3:10") {
		t.Fatalf("the build prints:\n%s\nwant the place of the require", output.String())
	}
}
