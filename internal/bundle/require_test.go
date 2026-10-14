package bundle

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
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

// writeRequireCases writes the cases as modules under dir/node_modules/cases and returns an entry
// module that calls each of them in turn by the import path prefix, waits for a promise one
// returns, and stores the results by name as JSON in globalThis.result. Each call ends by removing
// a require a case set on globalThis.
func writeRequireCases(t *testing.T, dir, prefix string, cases []requireCase) string {
	t.Helper()
	files := map[string]string{}
	var entry strings.Builder
	for i, c := range cases {
		files[filepath.Join("node_modules", "cases", c.Name+".cjs")] = c.Code
		fmt.Fprintf(&entry, "import c%d from '%scases/%s.cjs'\n", i, prefix, c.Name)
	}
	writeTree(t, dir, files)
	entry.WriteString("const cases: Array<[string, (name: string) => any, (value: any) => any]> = [\n")
	for i, c := range cases {
		fmt.Fprintf(&entry, "  [%q, c%d, (value: any) => { %s; return value }],\n", c.Name, i, c.Post)
	}
	entry.WriteString("]\nconst results: Record<string, string> = {}\n" +
		"void (async () => {\n  for (const [name, f, post] of cases) {\n" +
		"    try {\n      results[name] = String(await post(f('node:path')))\n" +
		"    } catch (err) {\n      results[name] = 'ERROR:' + err\n" +
		"    } finally {\n      delete (globalThis as any).require\n    }\n  }\n" +
		"  ;(globalThis as any).result = JSON.stringify(results)\n})()\n")
	return entry.String()
}

// checkRequireCases checks the warning of the build and the result in QuickJS of each case: a
// case that fails there because require is missing gets a warning at its place, one that runs gets
// none, except requireLimits.
func checkRequireCases(t *testing.T, cases []requireCase, code, output string) {
	t.Helper()
	var results map[string]string
	if err := json.Unmarshal([]byte(evalResult(t, code)), &results); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		got, ok := results[c.Name]
		if failed := strings.Contains(got, "'require' is not defined"); !ok || failed == c.Runs {
			t.Errorf("%s gives %q in QuickJS, want runs = %v", c.Name, got, c.Runs)
		}
		_, limit := requireLimits[c.Name]
		// Rolldown warns of eval at the same place, in another form.
		place := regexp.MustCompile(regexp.QuoteMeta(filepath.Join("cases", c.Name+".cjs")) + `:\d+:\d+ \(cases\): `)
		if warned := place.MatchString(output); warned != (c.Runs == limit) {
			t.Errorf("%s: warned = %v, want %v (runs = %v, limit %q)", c.Name, warned, !warned, c.Runs, requireLimits[c.Name])
		}
	}
}

// The build warns of each case of testdata/require-cases.json that fails in QuickJS because
// require is missing, and of none that runs, except requireLimits. Minified as tifa build makes
// it. The warning does not fail the build.
func TestSSRRequireCases(t *testing.T) {
	cases := loadRequireCases(t)
	root := copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{"cases.ts": writeRequireCases(t, root, "", cases)})
	var output strings.Builder
	code, _, err := script(testdata(t), root, "cases.ts", &output)
	if err != nil {
		t.Fatal(err)
	}
	checkRequireCases(t, cases, code, output.String())

	root = copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"node_modules/cases/guarded.cjs": "module.exports = function (name) {\n" +
			"  return typeof require === 'function' ? require('unknown-optional-package') : 'safe'\n}\n",
		"unresolved.ts": "import read from 'cases/guarded.cjs'\nglobalThis.result = read('x')\n",
	})
	_, _, err = script(testdata(t), root, "unresolved.ts", io.Discard)
	if err == nil || !strings.Contains(err.Error(), `failed to resolve import "unknown-optional-package"`) {
		t.Errorf("a guarded require of a package that is not installed: %v, want the bundler's error", err)
	}
}

// The same cases, unminified as tifa dev builds them.
func TestDevRequireCases(t *testing.T) {
	cases := loadRequireCases(t)
	root := devApp(t)
	var output strings.Builder
	d, err := NewDev(root, freePort(t), &output)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	host := "import './runtime/host.js'\n"
	writeTree(t, root, map[string]string{
		".tifa/server.ts": host + writeRequireCases(t, filepath.Join(root, "vendor"), "../vendor/node_modules/", cases),
	})
	if _, err := d.Build(filepath.Join(root, "public"), true); err != nil {
		t.Fatal(err)
	}
	code, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	// What Node printed is complete once it exited.
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	checkRequireCases(t, cases, string(code), output.String())
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
