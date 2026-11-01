package bundle

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/liustack/tifa/js"
)

// keyPathUnitsOnNode runs keyPathUnits of the Vite plugin on each of sources with the parser of
// Vite, and returns for each the fingerprint of every part that sees a key path.
func keyPathUnitsOnNode(t *testing.T, sources []string) []map[string]string {
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
		"const out = JSON.parse(process.argv[2]).map((source) => Object.fromEntries(plugin.keyPathUnits(parseAst(source), source)))\n" +
		"process.stdout.write(JSON.stringify(out))\n"
	cmd := exec.Command("node", "--input-type=module", "-e", script, plugin, string(input))
	cmd.Dir = testdata(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("keyPathUnits: %v", err)
	}
	var results []map[string]string
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return results
}

// The fingerprint of a part of React's string renderer is the same for the same code written with
// other white space, other comments and other numbers on the names the compiler made, and differs
// for code that does something else.
func TestKeyPathFingerprintsTellCodeApart(t *testing.T) {
	for _, c := range []struct {
		name, a, b string
		same       bool
	}{
		{"white space", "function f(keyPath) {\n  return keyPath[0] ;\n}", "function f( keyPath ){return keyPath[ 0 ];}", true},
		{"comments", "function f(keyPath) {\n  g(); // one\n  return keyPath; /* two */\n}", "function f(keyPath) { g(); return keyPath; }", true},
		{"a comment that ends a line", "function f(keyPath) {\n  g(); // one\n  h();\n}", "function f(keyPath) {\n  g(); /* one */ h();\n}", true},
		{"numbers of the compiler", "function f(keyPath) { var a$12 = keyPath, a$jscomp$3 = 1; return a$12 + a$jscomp$3 }", "function f(keyPath) { var a$99 = keyPath, a$jscomp$7 = 1; return a$99 + a$jscomp$7 }", true},
		{"names swapped", "function f(keyPath) { var a$1 = 1, a$2 = 2; return a$1 - a$2 }", "function f(keyPath) { var a$1 = 1, a$2 = 2; return a$2 - a$1 }", false},
		{"names merged", "function f(keyPath) { var a$1 = 1, a$2 = 2; return a$1 - a$2 }", "function f(keyPath) { var a$1 = 1, a$1 = 2; return a$1 - a$1 }", false},
		{"a space in a string", `function f(keyPath) { return "a b" }`, `function f(keyPath) { return "a  b" }`, false},
		{"a plus and an increment", "function f(keyPath) { return a + +b }", "function f(keyPath) { return a++ + b }", false},
		{"a number and a dot", "function f(keyPath) { return 1 .x }", "function f(keyPath) { return 1. .x }", false},
		{"another read", "function f(keyPath) { return keyPath[0] }", "function f(keyPath) { return keyPath[1] }", false},
		{"a new read", "function f(keyPath) { return keyPath }", "function f(keyPath) { return keyPath.length }", false},
	} {
		got := keyPathUnitsOnNode(t, []string{c.a, c.b})
		if len(got[0]) != 1 || len(got[1]) != 1 || got[0]["f"] == "" || got[1]["f"] == "" {
			t.Errorf("%s: parts %v and %v, want f alone in each", c.name, got[0], got[1])
			continue
		}
		if same := got[0]["f"] == got[1]["f"]; same != c.same {
			t.Errorf("%s: same fingerprint = %v, want %v", c.name, same, c.same)
		}
	}
}

// The parts that see a key path are the functions and variables with a name that has keyPath in
// it, and the functions they call with one. A name used twice makes a part of a name of its own.
func TestKeyPathFingerprintsFindTheParts(t *testing.T) {
	for _, c := range []struct {
		name, source string
		want         []string
	}{
		{"a function and what it calls", "function g(x) { return x.length }\nfunction h() {}\nfunction f(keyPath) { return g(keyPath) }", []string{"f", "g"}},
		{"a call with a property", "function g(x) {}\nfunction f(task) { g(task.keyPath) }", []string{"f", "g"}},
		{"a call that passes a derived value", "function g(x) {}\nfunction f(keyPath) { g([keyPath, 1]) }", []string{"f", "g"}},
		{"a call that passes nothing of one", "function g(x) {}\nfunction f(keyPath) { g(1) }", []string{"f"}},
		{"one declarator of a list", "var p = null, currentlyRenderingKeyPath = null, q = 1", []string{"currentlyRenderingKeyPath"}},
		{"a name twice", "var a = keyPath\nvar a = keyPath[0]", []string{"a", "a#"}},
		{"a statement that is no declaration", "task.keyPath = null", []string{"ExpressionStatement"}},
		{"a name inside a function", "function f() { function keyPathReader() {} }", []string{"f"}},
		{"no key path", "function f(a) { return a }", nil},
	} {
		units := keyPathUnitsOnNode(t, []string{c.source})[0]
		got := make([]string, 0, len(units))
		for name := range units {
			got = append(got, name)
		}
		slices.Sort(got)
		if !slices.Equal(got, c.want) && !(len(got) == 0 && len(c.want) == 0) {
			t.Errorf("%s: parts %v, want %v", c.name, got, c.want)
		}
	}
}
