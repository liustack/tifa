package bundle

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// emotionStyledPage renders styled components of the base of @emotion/styled, as its ES module and
// as its CommonJS file, with the props React gives and with props of every odd shape a caller can
// hand the render function of a component (a symbol key, an inherited key, no prototype, an own key
// named __proto__, getters, a theme or none), and with class names of registered styles and of none.
// The style function writes what it sees of the props it is given, so the CSS, and with it the class
// name, shows any difference between the props the plugin hands and the ones the loop made.
const emotionStyledPage = `import './host'
import { createElement as h } from 'react'
import { renderToString } from 'react-dom/server.edge'
import esm from '@emotion/styled/base'
import cjs from './styled.cjs'

let reads = 0
const describe = (p: any) => JSON.stringify([
  Object.getPrototypeOf(p) === Object.prototype,
  Object.getPrototypeOf(p) === null,
  Reflect.ownKeys(p).map(String),
  (() => { const keys: string[] = []; for (const k in p) keys.push(k); return keys })(),
  Object.prototype.hasOwnProperty.call(p, '__proto__'),
  Object.keys(p).map((k) => typeof p[k]),
  typeof p.theme,
  Object.isFrozen(p),
  Object.isExtensible(p),
  reads,
])
const symbol = Symbol('s')
const proto = { inherited: 1 }
const getters = Object.defineProperties({}, {
  g: { enumerable: true, get() { reads++; return 'got' } },
  h: { enumerable: false, get() { return 'hidden' } },
})
class Props { a = 1; b = 2 }
const oddProps: [string, () => any][] = [
  ['plain', () => ({ a: 1, b: 'x' })],
  ['empty', () => ({})],
  ['a theme', () => ({ a: 1, theme: { t: 1 } })],
  ['a null theme', () => ({ a: 1, theme: null })],
  ['an undefined theme', () => ({ a: 1, theme: undefined })],
  ['a symbol key', () => ({ a: 1, [symbol]: 2 })],
  ['a hidden symbol key', () => Object.defineProperty({ a: 1 }, symbol, { value: 2, enumerable: false })],
  ['an inherited key', () => Object.assign(Object.create(proto), { a: 1 })],
  ['an inherited theme', () => Object.create({ theme: { t: 'inherited' } })],
  ['no prototype', () => Object.assign(Object.create(null), { a: 1 })],
  ['an own __proto__', () => JSON.parse('{"__proto__":{"z":1},"a":2}')],
  ['getters', () => getters],
  ['an instance', () => new Props()],
  ['frozen', () => Object.freeze({ a: 1, b: 2 })],
  ['sealed', () => Object.seal({ a: 1, b: 2 })],
  ['integer keys', () => ({ b: 1, 2: 'two', 1: 'one', a: 2 })],
  ['a hidden key', () => Object.defineProperty({ a: 1 }, 'hid', { value: 1, enumerable: false })],
  ['twenty keys', () => Object.fromEntries(Array.from({ length: 20 }, (_, i) => ['k' + i, i]))],
]

const out: string[] = []
for (const [form, base] of [['esm', esm], ['cjs', cjs]] as [string, any][]) {
  const Box = base('div', { shouldForwardProp: (key: string) => key === 'className' || key === 'children' })((p: any) => ({ '--seen': '"' + describe(p) + '"' }))
  const Plain = base('div')({ color: 'red' })
  const Mixed = base('div')({ margin: 1 }, 'padding:2px;', (p: any) => (p.className ? { top: 1 } : null))
  const render = (type: any, props: any) => {
    const element = h(type, null)
    ;(element as any).props = props
    try {
      return renderToString(element)
    } catch (e) {
      return 'throws ' + (e as Error).message
    }
  }
  for (const [name, make] of oddProps) out.push(form + ' ' + name + ' ' + render(Box, make()))
  // The class names of registered styles, of unregistered ones and of no string.
  const registered = /class="([^"]+)"/.exec(renderToString(h(Plain)))![1]
  const mixed = /class="([^"]+)"/.exec(renderToString(h(Mixed)))![1]
  for (const className of [registered, registered + ' extra', 'extra ' + registered, mixed + '  ' + registered, 'extra', '', ' ', 'css-nope-1', null, undefined, 5, ['x']]) {
    out.push(form + ' class ' + JSON.stringify(className) + ' ' + render(Box, { className }) + render(Mixed, { className, children: 'c' }))
  }
}
;(globalThis as any).result = JSON.stringify(out)
`

// styledConcatCall is the call of serializeStyles with the copy of the styles as it is without the
// plugin, minified.
var styledConcatCall = regexp.MustCompile(`\(\w+\.concat\(\w+\),\w+\.registered,\w+\)`)

func emotionStyledRender(t *testing.T, root string) ([]string, string, string) {
	t.Helper()
	writeTree(t, root, map[string]string{
		"styled.tsx": emotionStyledPage,
		"styled.cjs": "module.exports = require('./node_modules/@emotion/styled/base/dist/emotion-styled-base.cjs.js').default\n",
	})
	code, printed := buildPrinting(t, root, "styled.tsx")
	var out []string
	if err := json.Unmarshal([]byte(evalResult(t, code)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2*(18+12) {
		t.Fatalf("%d pages", len(out))
	}
	return out, code, printed
}

// The render function of a styled component of @emotion/styled hands serializeStyles a copy of its
// array of styles with the registered styles of its className added, a copy of the array too when
// there are none. The tifa plugin hands the array as it is then. The pages are the same as without
// the change, for every form of props and className, in the ES module and in the CommonJS file, and
// a styled base of another text is left as it is, with a warning.
func TestSSREmotionStyledHandsTheStylesWithoutACopy(t *testing.T) {
	changed, code, printed := emotionStyledRender(t, copyTestdata(t, "ssr", true))
	if strings.Contains(printed, notRecognized) {
		t.Errorf("the build prints:\n%s", printed)
	}
	if n := len(styledConcatCall.FindAllString(code, -1)); n != 0 {
		t.Errorf("%d styled components copy the styles on every render", n)
	}
	all := strings.Join(changed, "\n")
	for _, want := range []string{"extra", "padding:2px", "top:1px", "css-nope-1", `"a","inherited"`, `[true,false,["a","b","theme"]`} {
		if !strings.Contains(all, want) {
			t.Errorf("no page has %s", want)
		}
	}
	for _, file := range []string{"emotion-styled-base.edge-light.esm.js", "emotion-styled-base.cjs.js"} {
		root := withChangedPackage(t, "@emotion/styled", filepath.Join("base", "dist", file), func(source string) string {
			const call = "(styles.concat(classInterpolations), cache.registered, mergedProps)"
			if strings.Count(source, call) != 1 {
				t.Fatalf("%s does not have %q once", file, call)
			}
			return strings.Replace(source, call, "(styles.concat(classInterpolations) /* changed */, cache.registered, mergedProps)", 1)
		})
		plain, code, printed := emotionStyledRender(t, root)
		if !strings.Contains(printed, notRecognized+"@emotion/styled 11.14.1: ") || !strings.Contains(printed, "has no styled component the tifa plugin knows") {
			t.Errorf("an unknown %s: the build prints:\n%s", file, printed)
		}
		if n := len(styledConcatCall.FindAllString(code, -1)); n != 1 {
			t.Errorf("an unknown %s: %d styled components copy the styles, want 1", file, n)
		}
		for i, p := range plain {
			if changed[i] != p {
				t.Errorf("an unknown %s: page %d is\n%s\nwant\n%s", file, i, p, changed[i])
			}
		}
	}
}
