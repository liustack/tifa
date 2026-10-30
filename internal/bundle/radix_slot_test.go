package bundle

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// radixSlotPage renders Slot of @radix-ui/react-slot over children, which merges the props of the
// Slot with those of its child, with every kind of prop that mergeProps tells apart. The child
// is a component that records the props it gets: their keys in order, the values that are not
// functions, and what the functions do when called. It counts the tests of the regexp that tells
// a handler from other props.
const radixSlotPage = `import './host'
import { renderToString } from 'react-dom/server.edge'
import { Slot } from '@radix-ui/react-slot'

let handlerTests = 0
const test = RegExp.prototype.test
RegExp.prototype.test = function (s: string) {
  if (this.source === '^on[A-Z]') handlerTests++
  return test.call(this, s)
}

const calls: string[] = []
const fn = (name: string, result?: unknown) =>
  function (this: unknown, ...args: unknown[]) {
    calls.push(name + '(' + args.map(String).join() + ')')
    return result
  }

const seen: any[] = []
function Child(props: any) {
  seen.push(props)
  return null
}
class Named {
  constructor(private n: string) {}
  toString() {
    return 'N:' + this.n
  }
  valueOf() {
    return 'V:' + this.n
  }
}

const cases: [string, any, any][] = [
  ['typical', { className: 'a b', onClick: fn('slot click', 's'), 'data-x': 1, style: { color: 'red', margin: 1 } }, { className: 'c', onClick: fn('child click', 'c'), to: '/', style: { margin: 2 } }],
  ['order', { b: 1, a: 2, onMouseDown: fn('md'), z: 3 }, { c: 4, a: 5, z: 6, onMouseDown: fn('child md'), y: 7 }],
  ['slot handler only', { onClick: fn('slot click') }, { onClick: undefined, id: 'x' }],
  ['child handler only', { onClick: undefined }, { onClick: fn('child click'), id: 'x' }],
  ['slot handler, none in child', { onClick: fn('slot click') }, { id: 'x' }],
  ['no handlers', { onClick: undefined, onFocus: null }, { onClick: null, onFocus: 0 }],
  ['not handlers', { onclick: fn('lower'), on: fn('on'), onA1: fn('a1'), on1: fn('one'), On: fn('On'), onÄ: fn('umlaut'), 'on-x': fn('dash'), onaB: fn('aB'), oxA: fn('oxA'), xnA: fn('xnA') }, { onclick: fn('child lower'), on: fn('child on'), onA1: fn('child a1'), on1: fn('child one'), On: fn('child On'), onÄ: fn('child umlaut'), 'on-x': fn('child dash'), onaB: fn('child aB'), oxA: fn('child oxA'), xnA: fn('child xnA') }],
  ['edge names', { 'on@': fn('at'), 'on[': fn('bracket'), 'onA': fn('A'), 'onZ': fn('Z'), 'on{': fn('brace'), 'ok': fn('ok'), 'o': fn('o') }, { 'on@': fn('child at'), 'on[': fn('child bracket'), 'onA': fn('child A'), 'onZ': fn('child Z'), 'on{': fn('child brace'), 'ok': fn('child ok'), 'o': fn('child o') }],
  ['handlers', { onA: fn('A'), onZ: fn('Z'), onKeyDown: fn('KeyDown'), onPointerDownOutside: fn('PDO') }, { onA: fn('child A'), onZ: fn('child Z'), onKeyDown: fn('child KeyDown'), onPointerDownOutside: fn('child PDO') }],
  ['class none', { id: 's' }, { className: undefined }],
  ['class slot', { className: 's' }, { className: undefined }],
  ['class child', { className: undefined }, { className: 'c' }],
  ['class both', { className: 's' }, { className: 'c' }],
  ['class falsy', { className: 0 }, { className: '' }],
  ['class falsy slot', { className: '' }, { className: 'c' }],
  ['class numbers', { className: 1 }, { className: 2 }],
  ['class number child', { className: 0 }, { className: 7 }],
  ['class objects', { className: new Named('s') }, { className: new Named('c') }],
  ['class object child', { className: undefined }, { className: new Named('c') }],
  ['class array', { className: ['a', 'b'] }, { className: ['c'] }],
  ['class symbol', { className: Symbol('s') }, { className: 'c' }],
  ['class symbol child', { className: undefined }, { className: Symbol('c') }],
  ['class missing in child', { className: 's' }, { id: 'x' }],
  ['style both', { style: { color: 'red' } }, { style: { color: 'blue', margin: 1 } }],
  ['style slot undefined', { style: undefined }, { style: { margin: 1 } }],
  ['style child undefined', { style: { margin: 1 } }, { style: undefined }],
  ['style null', { style: null }, { style: null }],
  ['style string', { style: 'color:red' }, { style: 'margin:1px' }],
  ['aria both', { 'aria-describedby': 'a b' }, { 'aria-describedby': ' b  c ' }],
  ['aria child', { 'aria-describedby': undefined }, { 'aria-describedby': 'c' }],
  ['aria none', { 'aria-describedby': undefined }, { 'aria-describedby': undefined }],
  ['aria numbers', { 'aria-describedby': 1 }, { 'aria-describedby': 'x' }],
  ['child only keys', { a: 1 }, { b: 2, className: 'x', style: { c: 3 }, onClick: fn('click') }],
  ['slot only keys', { a: 1, className: 'x', style: { c: 3 }, onClick: fn('click') }, { b: 2 }],
  ['integer keys', { 2: 'a', b: 1, 1: 'c' }, { 3: 'd', 1: 'e', c: 2 }],
]

const out: any[] = []
for (const [name, slotProps, childProps] of cases) {
  seen.length = 0
  calls.length = 0
  let error = ''
  let html = ''
  try {
    html = renderToString(
      <Slot {...slotProps}>
        <Child {...childProps} />
      </Slot>,
    )
  } catch (e) {
    error = String((e as Error).message).slice(0, 60)
  }
  const props = seen[seen.length - 1]
  const record: any = { name, html, error, handlerCalls: 0 }
  if (props) {
    record.keys = Reflect.ownKeys(props).map(String)
    record.values = Reflect.ownKeys(props).map((k) => {
      const v = props[k]
      if (typeof v === 'function') return 'fn'
      if (typeof v === 'symbol') return String(v)
      if (v !== null && typeof v === 'object' && !Array.isArray(v) && v.constructor === Named) return String(v)
      return JSON.stringify(v) ?? 'undefined'
    })
    calls.length = 0
    record.results = Reflect.ownKeys(props)
      .filter((k) => typeof props[k] === 'function' && k !== 'ref')
      .map((k) => String(k) + '=' + String(props[k](1, 2)))
    record.calls = calls.slice()
  }
  out.push(record)
}
globalThis.result = JSON.stringify({ out, handlerTests })
`

type radixRecord struct {
	Name    string
	HTML    string
	Error   string
	Keys    []string
	Values  []string
	Results []string
	Calls   []string
}

type radixResult struct {
	Out          []radixRecord
	HandlerTests int
}

func radixRender(t *testing.T, root string) (radixResult, string) {
	t.Helper()
	writeTree(t, root, map[string]string{"radix.tsx": radixSlotPage})
	code, printed := buildPrinting(t, root, "radix.tsx")
	var r radixResult
	if err := json.Unmarshal([]byte(evalResult(t, code)), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Out) < 30 {
		t.Fatalf("%d cases", len(r.Out))
	}
	return r, printed
}

// radixOldMergeProps is mergeProps of react-slot 1.2.1 to 1.3, which does not merge
// aria-describedby.
const radixOldMergeProps = `function mergeProps(slotProps, childProps) {
  const overrideProps = { ...childProps };
  for (const propName in childProps) {
    const slotPropValue = slotProps[propName];
    const childPropValue = childProps[propName];
    const isHandler = /^on[A-Z]/.test(propName);
    if (isHandler) {
      if (slotPropValue && childPropValue) {
        overrideProps[propName] = (...args) => {
          const result = childPropValue(...args);
          slotPropValue(...args);
          return result;
        };
      } else if (slotPropValue) {
        overrideProps[propName] = slotPropValue;
      }
    } else if (propName === "style") {
      overrideProps[propName] = { ...slotPropValue, ...childPropValue };
    } else if (propName === "className") {
      overrideProps[propName] = [slotPropValue, childPropValue].filter(Boolean).join(" ");
    }
  }
  return { ...slotProps, ...overrideProps };
}
`

// withRadixMergeProps returns a copy of testdata/ssr whose react-slot has its mergeProps changed
// by change, which gets the source of mergeProps as react-slot has it.
func withRadixMergeProps(t *testing.T, change func(string) string) string {
	t.Helper()
	return withChangedPackage(t, "@radix-ui/react-slot", filepath.Join("dist", "index.mjs"), func(source string) string {
		start := strings.Index(source, "function mergeProps(")
		end := start + strings.Index(source[start:], "\n}\n") + len("\n}\n")
		if start < 0 || end < start {
			t.Fatal("react-slot has no mergeProps")
		}
		return source[:start] + change(source[start:end]) + source[end:]
	})
}

// Slot of @radix-ui/react-slot merges the props of the Slot into those of its child with
// mergeProps, which tells a handler from other props with a regexp on every prop and builds the
// class name with filter and join. The tifa plugin writes it with a loop that does the same, for
// the two forms of mergeProps that react-slot has had since 1.2.1, and leaves a mergeProps it does
// not know alone with a warning. Props come out with the same keys in the same order, the same
// values and the same results of the handlers it joins.
func TestSSRMergesThePropsOfRadixSlotWithALoop(t *testing.T) {
	unknown := func(source string) string {
		return strings.Replace(source, "const isHandler = /^on[A-Z]/.test(propName);", "const isHandler = /* changed */ /^on[A-Z]/.test(propName);", 1)
	}
	for _, form := range []struct {
		name string
		old  bool
	}{{"1.5", false}, {"1.2", true}} {
		root := copyTestdata(t, "ssr", true)
		if form.old {
			root = withRadixMergeProps(t, func(string) string { return radixOldMergeProps })
		}
		changed, printed := radixRender(t, root)
		if strings.Contains(printed, "@radix-ui/react-slot") {
			t.Errorf("%s: the build prints:\n%s", form.name, printed)
		}
		if changed.HandlerTests != 0 {
			t.Errorf("%s: mergeProps tested %d props with the regexp of handlers", form.name, changed.HandlerTests)
		}
		plain, printed := radixRender(t, withRadixMergeProps(t, func(source string) string {
			if form.old {
				source = radixOldMergeProps
			}
			return unknown(source)
		}))
		if strings.Count(printed, "@radix-ui/react-slot 1.5.0") != 1 || !strings.Contains(printed, "has no mergeProps the tifa plugin knows") {
			t.Errorf("%s: the build prints:\n%s", form.name, printed)
		}
		if plain.HandlerTests == 0 {
			t.Errorf("%s: the regexp of handlers is not tested when the plugin leaves mergeProps alone", form.name)
		}
		for i, p := range plain.Out {
			c := changed.Out[i]
			want, _ := json.Marshal(p)
			got, _ := json.Marshal(c)
			if string(got) != string(want) {
				t.Errorf("%s: %s: props give\n%s\nwant\n%s", form.name, p.Name, got, want)
			}
		}
	}
	// A mergeProps that differs in its last lines is not the one the plugin knows either.
	root := withRadixMergeProps(t, func(source string) string {
		return strings.Replace(source, "return { ...slotProps, ...overrideProps };", "return { ...overrideProps, ...slotProps };", 1)
	})
	other, printed := radixRender(t, root)
	if !strings.Contains(printed, "has no mergeProps the tifa plugin knows") || other.HandlerTests == 0 {
		t.Errorf("a changed return: the build prints:\n%s\nand tests %d props", printed, other.HandlerTests)
	}
}
