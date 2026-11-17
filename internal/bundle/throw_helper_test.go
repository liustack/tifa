package bundle

import (
	"regexp"
	"strings"
	"testing"

	"github.com/liustack/tifa/js"
)

// resolveCurrentlyRendering is React DOM's resolveCurrentlyRenderingComponent as a minified
// function declaration: a test and a throw, then a return of a variable. Every hook calls it.
var resolveCurrentlyRendering = regexp.MustCompile(`function \w+\(\)\{if\(\w+===null\)throw Error\(\w+\(321\)\);return \w+\}`)

// The SSR bundle of an app starts with the host script, which has a throwHelper of its own, built by
// the same plugin. The pass that writes out small functions must use it: with a helper it does not
// know, the throwing bodies of React DOM stayed functions and every hook paid a call for them,
// without a warning or a failing test.
func TestSSRWritesOutThrowingFunctionsInABundleWithTheHostScript(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	host, err := js.Runtime.ReadFile("runtime/host.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(host), "function tifaThrow(") {
		t.Fatal("the host script has no tifaThrow: change this test to what it has")
	}
	writeTree(t, root, map[string]string{
		"host.js": string(host),
		"hooks.tsx": "import './host.js'\n" +
			"import { useState } from 'react'\n" +
			"import { renderToString } from 'react-dom/server.edge'\n" +
			"function Count() { const [n] = useState(3); return <b>{n}</b> }\n" +
			"globalThis.result = renderToString(<Count />)\n",
	})
	code, printed := buildPrinting(t, root, "hooks.tsx")
	if strings.Contains(printed, "small functions") {
		t.Errorf("the build prints:\n%s", printed)
	}
	if !strings.Contains(code, "tifaThrow") && !strings.Contains(code, "(321)") {
		t.Fatal("the bundle has no React DOM that throws for a hook outside a component: change this test")
	}
	if found := resolveCurrentlyRendering.FindString(code); found != "" {
		t.Errorf("a hook still calls resolveCurrentlyRenderingComponent: %s", found)
	}
	if got := evalResult(t, code); got != "<b>3</b>" {
		t.Errorf("the page is %q", got)
	}
}
