package bundle

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liustack/tifa/js"
)

// unitsOnNode runs reactDomUnits of the Vite plugin on each of sources with the parser of Vite, and
// returns for each the fingerprint of every part by name.
func unitsOnNode(t *testing.T, sources []string) []map[string]string {
	t.Helper()
	testModules(t)
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	script := "const plugin = await import(process.argv[1])\n" +
		"const { parseAst } = await import('vite')\n" +
		"const out = JSON.parse(process.argv[2]).map((source) => Object.fromEntries(plugin.reactDomUnits(parseAst(source), source).units.map((u) => [u.name, u.fingerprint])))\n" +
		"process.stdout.write(JSON.stringify(out))\n"
	cmd := exec.Command("node", "--input-type=module", "-e", script, plugin, string(input))
	cmd.Dir = testdata(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("reactDomUnits: %v", err)
	}
	var results []map[string]string
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return results
}

// The fingerprint of a part of React's string renderer is the same for the same code written with
// other white space, other comments, other brackets, other quotes and other names and numbers on
// what the part declares, and differs for code that does something else.
func TestKeyPathFingerprintsTellCodeApart(t *testing.T) {
	for _, c := range []struct {
		name, a, b string
		same       bool
	}{
		{"white space", "function f(keyPath) {\n  return keyPath[0] ;\n}", "function f( keyPath ){return keyPath[ 0 ];}", true},
		{"comments", "function f(keyPath) {\n  g(); // one\n  return keyPath; /* two */\n}", "function f(keyPath) { g(); return keyPath; }", true},
		{"brackets", "function f(keyPath) { return (keyPath); }", "function f(keyPath) { return keyPath; }", true},
		{"quotes", `function f(keyPath) { return keyPath['a']; }`, `function f(keyPath) { return keyPath["a"]; }`, true},
		{"numbers of the compiler", "function f(keyPath) { var a$12 = keyPath, a$jscomp$3 = 1; return a$12 + a$jscomp$3 }", "function f(keyPath) { var a$99 = keyPath, a$jscomp$7 = 1; return a$99 + a$jscomp$7 }", true},
		{"other names for what the part declares", "function f(keyPath) { var saved = keyPath; return saved }", "function f(keyPath) { var other = keyPath; return other }", true},
		{"names swapped", "function f(keyPath) { var a$1 = 1, a$2 = 2; return a$1 - a$2 }", "function f(keyPath) { var a$1 = 1, a$2 = 2; return a$2 - a$1 }", false},
		{"names merged", "function f(keyPath) { var a$1 = 1, a$2 = 2; return a$1 - a$2 }", "function f(keyPath) { var a$1 = 1, a$1 = 2; return a$1 - a$1 }", false},
		{"a space in a string", `function f(keyPath) { return "a b" }`, `function f(keyPath) { return "a  b" }`, false},
		{"a plus and an increment", "function f(keyPath) { return a + +b }", "function f(keyPath) { return a++ + b }", false},
		// A line break after return ends the statement, and a letter next to a keyword makes a name.
		{"a line break after return", "function f(keyPath) { return keyPath; }", "function f(keyPath) {\n  return\n  keyPath;\n}", false},
		{"a letter next to return", "function f(keyPath) { return α; }", "function f(keyPath) { returnα; }", false},
		{"a line break after a name", "function f(keyPath) { var a = keyPath\n(1) }", "function f(keyPath) { var a = keyPath; (1) }", false},
		// A property name and a name no declaration gives are not renamed, and a name that an outer
		// declaration gives is not one that nothing gives.
		{"another property name", "function f(keyPath) { return keyPath.a$1; }", "function f(keyPath) { return keyPath.a$2; }", false},
		{"another global", "function f(keyPath) { return a$1; }", "function f(keyPath) { return a$2; }", false},
		{"another outer name", "var a$1, a$2; function f(keyPath) { return a$1; }", "var a$1, a$2; function f(keyPath) { return a$2; }", false},
		{"a global that an outer name hides", "function f(keyPath) { return a; }", "var a; function f(keyPath) { return a; }", false},
		{"another read", "function f(keyPath) { return keyPath[0] }", "function f(keyPath) { return keyPath[1] }", false},
		{"a new read", "function f(keyPath) { return keyPath }", "function f(keyPath) { return keyPath.length }", false},
	} {
		got := unitsOnNode(t, []string{c.a, c.b})
		if got[0]["f"] == "" || got[1]["f"] == "" {
			t.Errorf("%s: parts %v and %v, want f in each", c.name, got[0], got[1])
			continue
		}
		if same := got[0]["f"] == got[1]["f"]; same != c.same {
			t.Errorf("%s: same fingerprint = %v, want %v", c.name, same, c.same)
		}
	}
}

// A part is known by its name as well as its body. The name of a function or a variable, and the
// names of the other top-level parts it uses, are in its fingerprint as they are, and only the names
// it declares inside itself are numbered. So a body under another name, or a call of another
// function, is not the part that was reviewed.
func TestUnitFingerprintsKeepTheNamesOfTheTopLevel(t *testing.T) {
	for _, c := range []struct {
		name, a, b string
		unitA      string
		unitB      string
		same       bool
	}{
		{"a body under another name", "function f(a) { return a + 1 }", "function g(a) { return a + 1 }", "f", "g", false},
		{"a variable under another name", "var a = 1;", "var b = 1;", "var a", "var b", false},
		{"a call of another function", "function g() {}\nfunction h() {}\nfunction f() { return g() }", "function g() {}\nfunction h() {}\nfunction f() { return h() }", "f", "f", false},
		{"a call of itself and of another", "function g() {}\nfunction f() { return f() }", "function g() {}\nfunction f() { return g() }", "f", "f", false},
		{"a use of another variable", "var a = 1, b = 1;\nfunction f() { return a }", "var a = 1, b = 1;\nfunction f() { return b }", "f", "f", false},
		{"the same part", "function g() {}\nfunction f(x) { var y = x; return g(y) }", "function g() {}\nfunction f(p) { var q = p; return g(q) }", "f", "f", true},
	} {
		got := unitsOnNode(t, []string{c.a, c.b})
		if got[0][c.unitA] == "" || got[1][c.unitB] == "" {
			t.Errorf("%s: parts %v and %v", c.name, got[0], got[1])
			continue
		}
		if same := got[0][c.unitA] == got[1][c.unitB]; same != c.same {
			t.Errorf("%s: same fingerprint = %v, want %v", c.name, same, c.same)
		}
	}
}

// The top of the file runs in order, so the order and the kind of its statements are a part of their own.
func TestReactDOMUnitsIncludeTheOrderOfTheTopLevel(t *testing.T) {
	const order = "the order of the top level"
	got := unitsOnNode(t, []string{
		"var a = 1;\nvar b = 2;\nfunction f() {}",
		"var b = 2;\nvar a = 1;\nfunction f() {}",
		"function f() {}\nvar a = 1;\nvar b = 2;",
		"let a = 1;\nvar b = 2;\nfunction f() {}",
		"var a = 1; /* c */\nvar b = 2;\n\nfunction f() {}",
	})
	if got[0]["var a"] != got[1]["var a"] || got[0]["f"] != got[1]["f"] {
		t.Errorf("a part differs with the order of the file: %v and %v", got[0], got[1])
	}
	for i, what := range []string{"two variables swapped", "a function moved up", "let for var"} {
		if got[0][order] == got[i+1][order] {
			t.Errorf("%s: the same order part", what)
		}
	}
	// The names are in the order part, not only the kinds of the statements.
	names := unitsOnNode(t, []string{"function a() {}\nfunction b() {}", "function b() {}\nfunction a() {}", "function a() {}\nfunction c() {}", "var a = 1;\nvar b = 1;", "var b = 1;\nvar a = 1;"})
	if names[0][order] == names[1][order] || names[0][order] == names[2][order] || names[3][order] == names[4][order] {
		t.Errorf("the order part does not tell function names or variable names apart: %v", names)
	}
	if got[0][order] != got[4][order] {
		t.Error("a comment and a blank line change the order part")
	}
}

// Lists the parts of the React DOM that is installed whose fingerprints are not in reviewedReactDOM
// of the plugin, with the lines they are on, for a review of a new release (docs/build.md). It
// prints nothing to fail on and is skipped unless TIFA_REACT_UNITS=1:
//
//	TIFA_REACT_UNITS=1 go test -run TestListUnreviewedReactDOMUnits -v ./internal/bundle
//
// With TIFA_REACT_UNITS=all it lists every part, which is how the table is first made.
func TestListUnreviewedReactDOMUnits(t *testing.T) {
	mode := os.Getenv("TIFA_REACT_UNITS")
	if mode == "" {
		t.Skip("set TIFA_REACT_UNITS=1 to list the parts of React DOM that reviewedReactDOM does not have")
	}
	testModules(t)
	dir := filepath.Join(testdata(t), "node_modules", "react-dom")
	file := filepath.Join(dir, "cjs", "react-dom-server-legacy.browser.production.js")
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "import fs from 'node:fs'\n" +
		"const plugin = await import(process.argv[1])\n" +
		"const { parseAst } = await import('vite')\n" +
		"const source = fs.readFileSync(process.argv[2], 'utf8')\n" +
		"const program = parseAst(source)\n" +
		"const { units } = plugin.reactDomUnits(program, source)\n" +
		"const reviewed = plugin.reviewedReactDOMUnits()\n" +
		"const line = (at) => source.slice(0, at).split('\\n').length\n" +
		"process.stdout.write(JSON.stringify(units.map((u) => ({ name: u.name, fingerprint: u.fingerprint, from: u.end ? line(u.start) : 0, to: u.end ? line(u.end) : 0, known: !!reviewed.get(u.name)?.has(u.fingerprint), named: reviewed.has(u.name) }))))\n"
	cmd := exec.Command("node", "--input-type=module", "-e", script, plugin, file)
	cmd.Dir = testdata(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var units []struct {
		Name, Fingerprint string
		From, To          int
		Known, Named      bool
	}
	if err := json.Unmarshal(out, &units); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	version := installedVersion(t, "react-dom")
	var lines []string
	for _, u := range units {
		if u.Known && mode != "all" {
			continue
		}
		kind := "new"
		if u.Named {
			kind = "changed"
		}
		if u.Known {
			kind = "known"
		}
		where := fmt.Sprintf("lines %d-%d", u.From, u.To)
		if u.To == 0 {
			where = "the whole top level"
		}
		t.Logf("%-8s %-40s %s of %s", kind, u.Name, where, file)
		lines = append(lines, u.Fingerprint+" "+u.Name)
	}
	if len(lines) == 0 {
		t.Logf("react-dom %s: every part is in reviewedReactDOM", version)
		return
	}
	t.Logf("react-dom %s: %d parts to read. Read them for what they do with a key path (docs/build.md), then add these lines to reviewedReactDOM in js/vite/plugin.js:\n%s", version, len(lines), strings.Join(lines, "\n"))
}
