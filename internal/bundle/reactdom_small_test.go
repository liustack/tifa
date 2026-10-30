package bundle

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liustack/tifa/js"
)

// reactDOMLoaded runs the load hook of the tifa plugin on the string renderer of the react-dom in
// root, as the build does, and returns what the hook gives and the warnings it prints.
func reactDOMLoaded(t *testing.T, root string) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	plugin := filepath.Join(dir, "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "import fs from 'node:fs'\n" +
		"import path from 'node:path'\n" +
		"const plugin = await import(process.argv[1])\n" +
		"const vite = await import('vite')\n" +
		"const root = fs.realpathSync(process.argv[2])\n" +
		"const ssr = plugin.ssrPlugin(() => root, async () => vite, {})[0]\n" +
		"ssr.buildStart.call({})\n" +
		"const warnings = []\n" +
		"const context = { warn: (message) => warnings.push(message), parse: (code) => vite.parseAst(code), error: (message) => { throw new Error(message) } }\n" +
		"const id = fs.realpathSync(path.join(root, 'node_modules', 'react-dom', 'cjs', 'react-dom-server-legacy.browser.production.js'))\n" +
		"const code = await ssr.load.call(context, id)\n" +
		"process.stdout.write(JSON.stringify({ code: typeof code === 'string' ? code : code.code, warnings }))\n"
	cmd := exec.Command("node", "--input-type=module", "-e", script, plugin, root)
	cmd.Dir = testdata(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("load React DOM: %v %s", err, out)
	}
	var loaded struct {
		Code     string
		Warnings []string
	}
	if err := json.Unmarshal(out, &loaded); err != nil {
		t.Fatal(err)
	}
	return loaded.Code, loaded.Warnings
}

// reactDOMOf is reactDOMLoaded for the react-dom of testdata/ssr, which the plugin knows.
func reactDOMOf(t *testing.T) (string, []string) {
	t.Helper()
	return reactDOMLoaded(t, copyTestdata(t, "ssr", true))
}

// between returns the text of code from the first from to the next to after it, or fails.
func between(t *testing.T, code, from, to string) string {
	t.Helper()
	i := strings.Index(code, from)
	if i < 0 {
		t.Fatalf("the source has no %q", from)
	}
	j := strings.Index(code[i+len(from):], to)
	if j < 0 {
		t.Fatalf("the source has no %q after %q", to, from)
	}
	return code[i : i+len(from)+j]
}

// An attribute whose value is a string asks for the type of the value once. React DOM asks three
// times whether the value is a function, a symbol or a boolean, which it does not write, and a
// string is the value of almost every attribute. The tifa plugin asks first whether it is a string.
// The page renders the same as with React DOM as it is, for the values of every type, see
// TestSSRWritesAttributesWithFewerCalls.
func TestSSRLetsAStringThroughWithOneQuestionAboutItsType(t *testing.T) {
	code, warnings := reactDOMOf(t)
	if len(warnings) != 0 {
		t.Errorf("the plugin warns: %v", warnings)
	}
	push := between(t, code, "function pushAttribute(target, name, value) {", "\nfunction pushInnerHTML(")
	for _, tc := range []struct{ name, from, to string }{
		{"className", `case "className":`, "break;"},
		{"tabIndex", `case "tabIndex":`, "break;"},
		{"dir, role, viewBox, width and height", `case "height":`, "break;"},
		{"xlink:actuate", `case "xlinkActuate":`, "break;"},
		{"xml:space", `case "xmlSpace":`, "break;"},
		{"src, href, action and formAction", `case "formAction":`, "break;"},
		{"xlinkHref", `case "xlinkHref":`, "break;"},
		{"the other names", "default:", "\n  } }"},
	} {
		text := between(t, push, tc.from, tc.to)
		if !strings.Contains(text, `"string" === typeof value`) && !strings.Contains(text, `"string" !== typeof value`) {
			t.Errorf("%s: pushAttribute does not ask first whether the value is a string:\n%s", tc.name, text)
		}
	}
	if strings.Contains(push, "switch (typeof value) {") && !strings.Contains(push, `if ("string" !== typeof value) switch (typeof value) {`) {
		t.Error("pushAttribute switches on the type of a string value")
	}
}
