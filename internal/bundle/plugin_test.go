package bundle

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/liustack/tifa/js"
)

// callPlugin calls the function name of the tifa Vite plugin on Node with args and decodes what
// it returns into result.
func callPlugin(t *testing.T, name string, args []any, result any) {
	t.Helper()
	testModules(t)
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	script := "const plugin = await import(process.argv[1])\n" +
		"process.stdout.write(JSON.stringify(await plugin." + name + "(...JSON.parse(process.argv[2]))))\n"
	out, err := exec.Command("node", "--input-type=module", "-e", script, plugin, string(input)).Output()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if err := json.Unmarshal(out, result); err != nil {
		t.Fatalf("%s: %v: %s", name, err, out)
	}
}

// The check of the SSR bundle skips a bundle that never names require or import() in its code.
// The words in comments, strings and regular expressions do not count: Vite keeps the comments of
// packages in a development bundle, and a check costs more than a rebuild of it.
func TestPluginSeesRequireAndImportOnlyInCode(t *testing.T) {
	for code, want := range map[string]bool{
		"// protocol-relative URLs require a separate check\nx()": false,
		"/* import(name) */ x()":                                  false,
		"x('require'); y(\"require(\")":                           false,
		"x(`require ${a} import(b)`)":                             false,
		"x(/require\\(/.test(s))":                                 false,
		"const requireX = 1, $require = 2, require$ = 3":          false,
		"require('fs')":                                           true,
		"f = () => require(name)":                                 true,
		"import(name)":                                            true,
		"import /* c */ (name)":                                   true,
		"import\n(name)":                                          true,
		"x(`a ${require('fs')} b`)":                               true,
		"x(`a ${{ a: `${1}` }.a} ${require('fs')}`)":              true,
		"x = a / 2; require('y')":                                 true,
		"x = (a) / 2 / b; require('y')":                           true,
		"return /'/.test(s); require('y')":                        true,
		"if (x) y = /`/; require('y')":                            true,
		"x['require'](name)":                                      false,
		"x.require(name)":                                         true,
	} {
		var got bool
		callPlugin(t, "mayLoadModules", []any{code}, &got)
		if got != want {
			t.Errorf("mayLoadModules(%q) = %v, want %v", code, got, want)
		}
	}
}
