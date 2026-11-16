package bundle

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// reactCallbackPage calls useCallback in server renders the ways React tells apart: dependencies
// that stay the same, change, change length, are missing or null, compare with Object.is, and an
// object with getters that counts its reads. Components that update their state while rendering
// render again, where useCallback compares the dependencies with those it kept. The order of the
// hooks changes between two renders of one component, so useMemo and useRef read what useCallback
// kept and the other way round. useCallback is also called outside a render.
const reactCallbackPage = `import './host'
import * as React from 'react'
import { renderToString } from 'react-dom/server.edge'

const log: string[] = []
const f1 = () => 'f1'
const f2 = () => 'f2'
let reads = 0
const counted = () => ({
  get length() {
    reads++
    return 2
  },
  get 0() {
    reads++
    return 'a'
  },
  get 1() {
    reads++
    return 'b'
  },
})
const show = (v: unknown) => (typeof v === 'function' ? v.name || 'fn' : Array.isArray(v) ? 'array of ' + v.length : String(v))

function Again({ change }: { change: string }) {
  const [n, setN] = React.useState(0)
  const pick = n === 0 ? f1 : f2
  const same = React.useCallback(pick, [1, 'a'])
  const changed = React.useCallback(pick, [n])
  const none = React.useCallback(pick)
  const nulled = React.useCallback(pick, null as any)
  const longer = React.useCallback(pick, n === 0 ? [1] : [1, 2])
  const shorter = React.useCallback(pick, n === 0 ? [1, 2] : [1])
  const nan = React.useCallback(pick, [NaN, n === 0 ? 0 : -0])
  const getters = React.useCallback(pick, counted() as any)
  const number = React.useCallback(pick, 5 as any)
  const object = React.useCallback(pick, { length: 1, 0: change } as any)
  if (n < 2) setN(n + 1)
  log.push([n, same, changed, none, nulled, longer, shorter, nan, getters, number, object].map(show).join())
  return <b>{n}</b>
}
function Swap({ order }: { order: string }) {
  const [n, setN] = React.useState(0)
  let v: unknown
  if (order === 'memo') v = n === 0 ? React.useCallback(f1, [1]) : React.useMemo(() => f2, [1])
  if (order === 'callback') v = n === 0 ? React.useMemo(() => f2, [1]) : React.useCallback(f1, [1])
  if (order === 'ref') v = n === 0 ? React.useCallback(f1, [1]) : React.useRef(f2).current
  if (order === 'state') v = n === 0 ? React.useCallback(f1, [1]) : React.useState(f2)[0]
  if (n < 1) setN(1)
  log.push(order + ' ' + show(v))
  return null
}
function Thrower() {
  React.useCallback(f1, { get length(): number { throw new Error('length read') } } as any)
  const [n, setN] = React.useState(0)
  if (n < 1) setN(1)
  return null
}

const out: any[] = []
for (const [name, element] of [
  ['again', <Again change="x" />],
  ['memo', <Swap order="memo" />],
  ['callback', <Swap order="callback" />],
  ['ref', <Swap order="ref" />],
  ['state', <Swap order="state" />],
  ['thrower', <Thrower />],
] as const) {
  log.length = 0
  reads = 0
  let html = ''
  let error = ''
  try {
    html = renderToString(element)
  } catch (e) {
    error = String((e as Error).message)
  }
  out.push({ name, html, error, log: log.slice(), reads })
}
let outside = ''
try {
  React.useCallback(f1, [])
} catch (e) {
  outside = String((e as Error).message)
}
globalThis.result = JSON.stringify({ out, outside })
`

// reactCallbackWrapper is useCallback of React DOM's server hooks as it is without the plugin,
// minified: a function made for useMemo on every call.
var reactCallbackWrapper = regexp.MustCompile(`useCallback:function\((\w+),(\w+)\)\{return [\w$]+\(function\(\)\{return (\w+)\},(\w+)\)\}`)

func reactCallbackRender(t *testing.T, root string) (string, string, string) {
	t.Helper()
	writeTree(t, root, map[string]string{"callback.tsx": reactCallbackPage})
	code, printed := buildPrinting(t, root, "callback.tsx")
	result := evalResult(t, code)
	var r struct {
		Out     []json.RawMessage
		Outside string
	}
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Out) != 6 || r.Outside == "" {
		t.Fatalf("result: %s", result)
	}
	return result, code, printed
}

// useCallback of React DOM's server hooks hands useMemo a function it makes on every call. The
// tifa plugin has it keep the callback as useMemo keeps a value, without that function, and leaves
// a React DOM whose useCallback is not the one it knows alone with a warning. The callbacks, the
// HTML, the reads of the dependencies and the errors are the same.
func TestSSRUseCallbackMakesNoFunction(t *testing.T) {
	changed, code, printed := reactCallbackRender(t, copyTestdata(t, "ssr", true))
	if strings.Contains(printed, "useCallback") {
		t.Errorf("the build prints:\n%s", printed)
	}
	if reactCallbackWrapper.MatchString(code) {
		t.Errorf("useCallback still makes a function for useMemo: %s", reactCallbackWrapper.FindString(code))
	}
	root := withChangedPackage(t, "react-dom", filepath.Join("cjs", "react-dom-server-legacy.browser.production.js"), func(source string) string {
		const callback = "    useCallback: function (callback, deps) {\n      return useMemo(function () {"
		if !strings.Contains(source, callback) {
			t.Fatal("react-dom has no useCallback that hands useMemo a function")
		}
		return strings.Replace(source, callback, "    useCallback: function (callback, deps) {\n      /* changed */ return useMemo(function () {", 1)
	})
	plain, code, printed := reactCallbackRender(t, root)
	if strings.Count(printed, "has no useMemo and useCallback in the form the tifa plugin knows") != 1 {
		t.Errorf("a React DOM the plugin does not know: the build prints:\n%s", printed)
	}
	if !reactCallbackWrapper.MatchString(code) {
		t.Errorf("a React DOM the plugin does not know: useCallback makes no function for useMemo")
	}
	if changed != plain {
		t.Errorf("useCallback gives\n%s\nwant\n%s", changed, plain)
	}
}
