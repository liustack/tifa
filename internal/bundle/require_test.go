package bundle

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// requireCase is one module of testdata/require-cases.json: a dependency that exports a function
// of a module name. Runs tells whether calling it in QuickJS, which has no require, runs, or
// fails because require is missing. Post, when set, is code run on the result value.
type requireCase struct {
	Name string
	Code string
	Post string
	Runs bool
}

// requireLimits are the cases where the check knowingly answers otherwise than QuickJS, and why.
// Most are rejected although they run: the check is conservative. The rest are accepted although
// they fail, by code that defines a global require, eval or with.
var requireLimits = map[string]string{
	// A require the code catches the error of: the check does not follow try.
	"try-catch-optional-fallback": "rejected, a missing require only throws inside try",
	// The code creates require itself. The check takes typeof require to be "undefined", which
	// holds only while no code writes require, so a write counts as a use.
	"assign-free-read":       "rejected, the code assigns require before it reads it",
	"assign-only":            "rejected, the code assigns require",
	"assign-destructure":     "rejected, the code assigns require",
	"for-in-assignment-free": "rejected, the code assigns require",
	"for-of-assignment-free": "rejected, the code assigns require",
	"global-assigned-read":   "rejected, the code sets globalThis.require before it reads it",
	// The minifier keeps the names in code with a direct eval or with, so a require declared
	// there counts as the host's.
	"eval-bound":  "rejected, eval declares require",
	"eval-shadow": "rejected, a direct eval keeps the name of the local require",
	"with-local":  "rejected, the require a with object gives is not declared",
	// The check takes typeof require to be "undefined" and does not follow code that sets
	// globalThis.require and removes it again.
	"guard-delete":                    "accepted, the code sets globalThis.require and deletes it",
	"guard-await-mutation":            "accepted, the code sets globalThis.require and deletes it",
	"guard-callback-mutation":         "accepted, the code sets globalThis.require and deletes it",
	"guard-mutation-conditional-test": "accepted, the code sets globalThis.require and deletes it",
	// Code in a string is not checked.
	"eval-free":  "accepted, the require is in a string that eval runs",
	"eval-alias": "accepted, the require is in a string that eval runs",
	// The minifier renames the names inside with as if with bound none, here the local require
	// that the with object hides.
	"with-shadow-free": "accepted, the minifier renames the require inside with",
}

// unresolvedCase requires a package that is not installed. The bundler fails to resolve it before
// the check runs, so it is built on its own.
const unresolvedCase = "guard-static-nonnode"

func loadRequireCases(t *testing.T) (accept, reject []requireCase) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "require-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []requireCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if c.Name == unresolvedCase {
			continue
		}
		_, limit := requireLimits[c.Name]
		if c.Runs != limit {
			accept = append(accept, c)
		} else {
			reject = append(reject, c)
		}
	}
	return accept, reject
}

// writeRequireCases writes the cases as modules under dir/node_modules/cases and returns an entry
// module that calls each of them by the import path prefix and stores the results by name as
// JSON in globalThis.result. Each call ends by removing a require a case set on globalThis.
func writeRequireCases(t *testing.T, dir, prefix string, cases []requireCase) string {
	t.Helper()
	files := map[string]string{}
	var entry strings.Builder
	for i, c := range cases {
		files[filepath.Join("node_modules", "cases", c.Name+".cjs")] = c.Code
		fmt.Fprintf(&entry, "import c%d from '%scases/%s.cjs'\n", i, prefix, c.Name)
	}
	writeTree(t, dir, files)
	entry.WriteString("const results: Record<string, string> = {}\n" +
		"function run(name: string, f: (name: string) => any, post: (value: any) => any) {\n" +
		"  try {\n    const value = post(f('node:path'))\n" +
		"    results[name] = value && typeof value.then === 'function' ? 'promise' : String(value)\n" +
		"  } catch (err) {\n    results[name] = 'ERROR:' + err\n  } finally {\n    delete (globalThis as any).require\n  }\n}\n")
	for i, c := range cases {
		fmt.Fprintf(&entry, "run(%q, c%d, (value: any) => { %s; return value });\n", c.Name, i, c.Post)
	}
	entry.WriteString("(globalThis as any).result = JSON.stringify(results)\n")
	return entry.String()
}

// checkRequireCases checks the results of building the cases to accept and to reject.
func checkRequireCases(t *testing.T, accept []requireCase, code string, buildErr error) {
	t.Helper()
	if buildErr != nil {
		t.Fatalf("building the cases to accept: %v", buildErr)
	}
	var results map[string]string
	if err := json.Unmarshal([]byte(evalResult(t, code)), &results); err != nil {
		t.Fatal(err)
	}
	for _, c := range accept {
		// A case of requireLimits that fails in QuickJS fails here too.
		if got := results[c.Name]; c.Runs && strings.Contains(got, "'require' is not defined") {
			t.Errorf("%s gives %q in QuickJS", c.Name, got)
		}
	}
}

func checkRejected(t *testing.T, reject []requireCase, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("the cases to reject built")
	}
	for _, c := range reject {
		if !strings.Contains(err.Error(), filepath.Join("cases", c.Name+".cjs")+":") {
			t.Errorf("%s: not rejected", c.Name)
		}
	}
}

// The check of the SSR bundle agrees with QuickJS on each case of testdata/require-cases.json:
// a case that fails there because require is missing fails the build, one that runs builds,
// except requireLimits. Minified as tifa build makes it.
func TestSSRRequireCases(t *testing.T) {
	accept, reject := loadRequireCases(t)
	root := copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"accept.ts": writeRequireCases(t, root, "", accept),
	})
	code, _, err := script(testdata(t), root, "accept.ts", io.Discard)
	checkRequireCases(t, accept, code, err)

	root = copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{"reject.ts": writeRequireCases(t, root, "", reject)})
	_, _, err = script(testdata(t), root, "reject.ts", io.Discard)
	checkRejected(t, reject, err)

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
	accept, reject := loadRequireCases(t)
	root := devApp(t)
	vendor := filepath.Join(root, "vendor")
	d, err := NewDev(root, freePort(t), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	host := "import './runtime/host.js'\n"
	writeTree(t, root, map[string]string{".tifa/server.ts": host + writeRequireCases(t, vendor, "../vendor/node_modules/", accept)})
	_, err = d.Build(filepath.Join(root, "public"), true)
	var code []byte
	if err == nil {
		code, err = os.ReadFile(filepath.Join(root, ".tifa", "dist", "ssr.js"))
	}
	checkRequireCases(t, accept, string(code), err)

	writeTree(t, root, map[string]string{".tifa/server.ts": host + writeRequireCases(t, vendor, "../vendor/node_modules/", reject)})
	_, err = d.Build(filepath.Join(root, "public"), true)
	checkRejected(t, reject, err)
}

// A require that no source maps to, such as one a plugin appends to the chunk, is reported at its
// place in ssr.js, not at the source of the code before it.
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
	n := NewNode(root, io.Discard)
	defer n.Close()
	_, err := App(n, root, filepath.Join(root, "public"), true)
	if err == nil || !strings.Contains(err.Error(), "\nssr.js:") || strings.Contains(err.Error(), "server.ts:") {
		t.Fatalf("err = %v, want the place in ssr.js", err)
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
	_, _, err := script(testdata(t), root, "local.ts", io.Discard)
	if err == nil || !strings.Contains(err.Error(), filepath.Join("cases", "local.cjs")+":3:10") {
		t.Fatalf("err = %v, want the place of the require", err)
	}
}
