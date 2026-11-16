package bundle

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// emotionRegisteredPage calls getRegisteredStyles of @emotion/utils, as an ES module and as a
// CommonJS file, and the function as it is without the plugin (reference), with a table of class
// names, and gives what each call returns, pushes and reads of the registered styles. The registered
// object is a plain object as emotion's is, so a name such as constructor is an inherited value
// that is not undefined. One name has the value undefined and the empty name has a style.
const emotionRegisteredPage = `import './host'
import { getRegisteredStyles as esm } from '@emotion/utils'
import cjs from './registered.cjs'

function reference(registered: any, registeredStyles: string[], classNames: string) {
  var rawClassName = ''
  classNames.split(' ').forEach(function (className) {
    if (registered[className] !== undefined) {
      registeredStyles.push(registered[className] + ';')
    } else if (className) {
      rawClassName += className + ' '
    }
  })
  return rawClassName
}

const inputs = [
  '', ' ', '  ', '   ', 'a', 'a b', ' a', 'a ', ' a ', 'a  b', 'x', 'x y', 'a x b y', 'x a', 'b a b',
  'a a', 'u', 'u a', 'constructor', 'a constructor toString', '__proto__ a', 'hasOwnProperty',
  'a\tb', 'a b', 'a\nb', '  a  x  ', 'css-1abc-Button x css-2def-Root', 'e', ' e', 'e ', 'e  e', '0', '1 a',
]
const run = (f: (r: any, s: string[], c: string) => string, classNames: string) => {
  const reads: string[] = []
  const registered = new Proxy({ a: 'color:red', b: 'margin:0', e: 'x:1', u: undefined, '': 'empty:1', 0: 'zero:0' } as any, {
    get(target, key, receiver) {
      reads.push(String(key))
      return Reflect.get(target, key, receiver)
    },
  })
  const registeredStyles = ['pre']
  const raw = f(registered, registeredStyles, classNames)
  return JSON.stringify([raw, registeredStyles, reads])
}
const out: string[] = []
for (const input of inputs) {
  const want = run(reference, input)
  out.push(JSON.stringify([input, want, run(esm, input) === want, run(cjs, input) === want]))
}
;(globalThis as any).result = JSON.stringify(out)
`

// emotionRegisteredLoop is getRegisteredStyles as it is without the plugin, minified: a split of the
// class names, a forEach and a function made for each call.
var emotionRegisteredLoop = regexp.MustCompile("\\.split\\(` `\\)\\.forEach\\(function\\(\\w+\\)\\{if\\(\\w+\\[\\w+\\]===void 0\\)")

func emotionRegisteredRender(t *testing.T, root string) ([]string, string, string) {
	t.Helper()
	writeTree(t, root, map[string]string{
		"registered.tsx": emotionRegisteredPage,
		"registered.cjs": "module.exports = require('./node_modules/@emotion/utils/dist/emotion-utils.cjs.js').getRegisteredStyles\n",
	})
	code, printed := buildPrinting(t, root, "registered.tsx")
	var out []string
	if err := json.Unmarshal([]byte(evalResult(t, code)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) < 30 {
		t.Fatalf("%d inputs: %s", len(out), strings.Join(out, "\n"))
	}
	return out, code, printed
}

// getRegisteredStyles of @emotion/utils, which emotion and every MUI component call with the class
// names of a component, splits them at each space and walks the pieces with a function made for each
// call. The tifa plugin walks the string itself. The same pieces, in the same order, give the same
// returned names, the same pushed styles and the same reads of the registered object (empty pieces
// included), in the ES module and in the CommonJS file. A getRegisteredStyles of another text is
// left as it is, with a warning.
func TestSSREmotionGetRegisteredStylesWalksTheString(t *testing.T) {
	changed, code, printed := emotionRegisteredRender(t, copyTestdata(t, "ssr", true))
	if strings.Contains(printed, "@emotion/utils") {
		t.Errorf("the build prints:\n%s", printed)
	}
	if n := len(emotionRegisteredLoop.FindAllString(code, -1)); n != 1 {
		t.Errorf("getRegisteredStyles splits the class names in %d places, want 1 (the page's own copy): %v", n, emotionRegisteredLoop.FindAllString(code, -1))
	}
	for _, step := range changed {
		var s []any
		if err := json.Unmarshal([]byte(step), &s); err != nil {
			t.Fatal(err)
		}
		if s[2] != true || s[3] != true {
			t.Errorf("%q differs from the original: esm %v, cjs %v, original %v", s[0], s[2], s[3], s[1])
		}
	}
	// The table has the cases it says: the original pushes the empty name and the inherited constructor.
	want := map[string]string{
		"a x b y": `["x y ",["pre","color:red;","margin:0;"],["a","a","x","b","b","y"]]`,
		"  ":      `["",["pre","empty:1;","empty:1;","empty:1;"],["","","","","",""]]`,
	}
	for _, step := range changed {
		var s []any
		_ = json.Unmarshal([]byte(step), &s)
		if w, ok := want[s[0].(string)]; ok && s[1] != w {
			t.Errorf("%q gives %v, want %s", s[0], s[1], w)
		}
	}
	// With the change of one file the plugin does not know, that file stays as it is.
	for _, file := range []string{"emotion-utils.edge-light.esm.js", "emotion-utils.cjs.js"} {
		root := withChangedPackage(t, "@emotion/utils", filepath.Join("dist", file), func(source string) string {
			const loop = "  classNames.split(' ').forEach(function (className) {"
			if strings.Count(source, loop) != 1 {
				t.Fatalf("%s does not have %q once", file, loop)
			}
			return strings.Replace(source, loop, "  /* changed */ classNames.split(' ').forEach(function (className) {", 1)
		})
		plain, code, printed := emotionRegisteredRender(t, root)
		if !strings.Contains(printed, notRecognized+"@emotion/utils 1.4.2: ") || !strings.Contains(printed, "has no getRegisteredStyles the tifa plugin knows") {
			t.Errorf("an unknown %s: the build prints:\n%s", file, printed)
		}
		if n := len(emotionRegisteredLoop.FindAllString(code, -1)); n != 2 {
			t.Errorf("an unknown %s: %d places still split the class names, want 2 (it and the page's own copy)", file, n)
		}
		for i, p := range plain {
			if changed[i] != p {
				t.Errorf("an unknown %s: input %d gives %s, want %s", file, i, p, changed[i])
			}
		}
	}
}
