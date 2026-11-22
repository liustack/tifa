package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/liustack/tifa/js"
)

// reuseStep is what reusingParse of the tifa Vite plugin did with one source: whether its syntax
// tree equals the one parse gives, and whether it parsed less than the whole source.
type reuseStep struct{ Equal, Reused bool }

// reusingParseOnNode runs reusingParse on each sequence of sources, one after the other.
func reusingParseOnNode(t *testing.T, sequences [][]string) [][]reuseStep {
	t.Helper()
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(sequences)
	if err != nil {
		t.Fatal(err)
	}
	script := `
const plugin = await import(process.argv[1])
const { parseAst } = await import('vite')
const { isDeepStrictEqual } = await import('node:util')
const out = JSON.parse(await (await import('node:stream/consumers')).text(process.stdin)).map((sequence) => {
  let whole = true
  const rp = plugin.reusingParse((source) => {
    whole = source === current
    return parseAst(source)
  })
  let current = ''
  return sequence.map((code) => {
    current = code
    const got = rp(code)
    return { Equal: isDeepStrictEqual(got, parseAst(code)), Reused: !whole }
  })
})
process.stdout.write(JSON.stringify(out))
`
	cmd := nodeWithInput(testdata(t), script, input, plugin)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("reusingParse: %v", err)
	}
	var results [][]reuseStep
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return results
}

func TestReusingParse(t *testing.T) {
	const base = "var a = 1;\nfunction f(x) {\n  return x + 1;\n}\nvar b = f(a), c = 'ü😀';\nexport { b, c };\n"
	cases := []struct {
		name    string
		sources []string
		reused  []bool // for each source after the first: whether it was not parsed whole
	}{
		{"one line of a function", []string{base, "var a = 1;\nfunction f(x) {\n  return x + 100;\n}\nvar b = f(a), c = 'ü😀';\nexport { b, c };\n"}, []bool{true}},
		{"the places after a longer line", []string{base, "var a = 1000000;\nfunction f(x) {\n  return x + 1;\n}\nvar b = f(a), c = 'ü😀';\nexport { b, c };\n"}, []bool{true}},
		{"a shorter line with other text after it", []string{base, "var a = 1;\nfunction f(x) {\n  return x + 1;\n}\nvar b = f(a), c = 'ü';\nexport { b, c };\n"}, []bool{true}},
		{"statements that share a line", []string{"var a = 1; var b = 2; var c = 3;\nvar d = 4;\n", "var a = 10; var b = 20; var c = 3;\nvar d = 4;\n"}, []bool{true}},
		{"the last statement of a shared line", []string{"var a = 1; var b = 2;\nvar d = 4;\n", "var a = 1; var b = 200;\nvar d = 4;\n"}, []bool{true}},
		{"a statement without a semicolon", []string{"var a = 1\nvar b = a\nf()\nfunction f() {}\n", "var a = 1\nvar b = a + 22\nf()\nfunction f() {}\n"}, []bool{true}},
		{"a template over lines", []string{"var a = `x\ny${1}\nz`;\nvar b = 2;\n", "var a = `x\ny${111}\nz`;\nvar b = 2;\n"}, []bool{true}},
		{"text added at the end", []string{base, base + "function g() {}\n"}, []bool{true}},
		{"text added at the end of a source without a line end", []string{"var a = 1;", "var a = 1;\nfunction g() {}"}, []bool{true}},
		{"a change and text added at the end", []string{base, "var a = 1;\nfunction f(x) {\n  return x + 2;\n}\nvar b = f(a), c = 'ü😀';\nexport { b, c };\nfunction g() {}\n"}, []bool{true}},
		{"three changes in a row", []string{base, "var a = 2;\nfunction f(x) {\n  return x + 1;\n}\nvar b = f(a), c = 'ü😀';\nexport { b, c };\n", "var a = 2;\nfunction f(x) {\n  return x + 1;\n}\nvar b = f(a) + 1, c = 'ü😀';\nexport { b, c };\n", "var a = 2;\nfunction f(x) {\n  return x * 3;\n}\nvar b = f(a) + 1, c = 'ü😀';\nexport { b, c };\n"}, []bool{true, true, true}},
		{"the same source", []string{base, base}, []bool{false}},
		{"a line added", []string{base, "var a = 1;\nvar z = 0;\nfunction f(x) {\n  return x + 1;\n}\nvar b = f(a), c = 'ü😀';\nexport { b, c };\n"}, []bool{false}},
		{"a line less", []string{base, "var a = 1;\nvar b = 1, c = 2;\nexport { b, c };\n"}, []bool{false}},
		{"a comment changed", []string{"// one\nvar a = 1;\n", "// two\nvar a = 1;\n"}, []bool{false}},
		{"a statement that is two lines in one", []string{"var a = 1;\nvar b = 2;\nvar c = 3;\n", "var a = 1; var b = 2;\nvar c = 3;\n"}, []bool{false}},
		{"a different source", []string{base, "let q = 1;\n"}, []bool{false}},
	}
	sequences := make([][]string, len(cases))
	for i, c := range cases {
		sequences[i] = c.sources
	}
	results := reusingParseOnNode(t, sequences)
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			steps := results[i]
			for n := 1; n < len(steps); n++ {
				if !steps[n].Equal {
					t.Errorf("source %d: the syntax tree differs from the one of parse", n)
				}
				if steps[n].Reused != c.reused[n-1] {
					t.Errorf("source %d: reused is %v, want %v", n, steps[n].Reused, c.reused[n-1])
				}
			}
		})
	}
}
