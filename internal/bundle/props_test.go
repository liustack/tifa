package bundle

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// propsPage makes elements with createElement and cloneElement from configs of every kind a
// program could pass, and for each reports what a program can see: the reads and calls that
// happened (getters, proxy traps, setters on Object.prototype), the key and the props of the
// element in order with their values and attributes, and the error. The same page runs on the
// React the tifa plugin changes and on one it cannot change, and every case must give the same.
//
// A case with trap set has a Proxy as config. The page reports its calls, but the test does not
// compare them: no code can tell a Proxy from an object without calling one of its traps, so the
// plugin cannot send it to React's loop, and the traps it calls are other than React's.
// A case with open set is a difference the plugin does not remove, and the test only reports it.
const propsPage = `import { createElement, cloneElement } from 'react'

type Case = { name: string; trap?: boolean; open?: boolean; make: () => any }
let events: string[] = []
const note = (s: string) => void events.push(s)
const undos: (() => void)[] = []
const later = (f: () => void) => void undos.push(f)
function show(v: any): string {
  if (typeof v === 'symbol') return v.toString()
  if (typeof v === 'string') return JSON.stringify(v)
  if (typeof v === 'function') return 'function'
  if (v !== null && typeof v === 'object') return Array.isArray(v) ? '[array]' : '{object}'
  return String(v)
}
function describeProps(p: any): string {
  return Reflect.ownKeys(p)
    .map((k) => {
      const d: any = Object.getOwnPropertyDescriptor(p, k)
      const flags = d.enumerable && d.writable && d.configurable ? '' : '!flags'
      return show(k) + ':' + ('value' in d ? show(d.value) : 'accessor') + flags
    })
    .join(',')
}
const handler = (tag: string) => {
  const h: any = {}
  for (const trap of ['get', 'set', 'has', 'ownKeys', 'getOwnPropertyDescriptor', 'getPrototypeOf', 'defineProperty', 'deleteProperty', 'isExtensible', 'preventExtensions', 'setPrototypeOf']) {
    h[trap] = (...args: any[]) => {
      note(tag + '.' + trap + (['get', 'has', 'getOwnPropertyDescriptor'].includes(trap) ? ' ' + show(args[1]) : ''))
      return (Reflect as any)[trap](...args)
    }
  }
  return h
}
function onObjectPrototype(name: string, descriptor: PropertyDescriptor): void {
  Object.defineProperty(Object.prototype, name, { configurable: true, ...descriptor })
  later(() => void delete (Object.prototype as any)[name])
}

const cases: Case[] = []
const add = (name: string, make: () => any, more: Partial<Case> = {}) => void cases.push({ name, make, ...more })

add('plain', () => ({ a: 1, b: 'x', children: 'c' }))
add('empty', () => ({}))
add('no config', () => undefined)
add('null config', () => null)
add('one prop', () => ({ className: 'x' }))
add('key string', () => ({ key: 'k', a: 1 }))
add('key number zero', () => ({ key: 0, a: 1 }))
add('key null', () => ({ key: null, a: 1 }))
add('key undefined', () => ({ key: undefined, a: 1 }))
add('key object', () => ({ key: { toString: () => (note('key.toString'), 'T') }, a: 1 }))
add('key symbol', () => ({ key: Symbol('s'), a: 1 }))
add('__self and __source', () => ({ __self: 1, __source: 2, a: 1 }))
add('children, ref and style', () => ({ ref: () => {}, children: [1, 2], style: { color: 'red' }, onClick: () => {} }))
add('ref undefined', () => ({ ref: undefined, a: 1 }))
add('ref null', () => ({ ref: null, a: 1 }))
add('ref string', () => ({ a: 1, ref: 'r', b: 2 }))
add('ref last', () => ({ a: 1, b: 2, ref: 'r' }))
add('integer keys', () => ({ b: 1, 2: 'two', a: 2, 1: 'one', 0: 'zero' }))
add('non-enumerable own', () => Object.defineProperty({ a: 1 }, 'h', { value: 2, enumerable: false }))
add('many keys', () => Object.fromEntries(Array.from({ length: 100 }, (_, i) => ['k' + i, i])))
add('frozen', () => Object.freeze({ a: 1, key: 'k' }))
add('sealed', () => Object.seal({ a: 1 }))
add('not extensible', () => Object.preventExtensions({ a: 1 }))
add('getter on key', () => ({ get key() { note('get key'); return 'K' }, a: 1 }))
add('getter on key undefined', () => ({ get key() { note('get key'); return undefined }, a: 1 }))
add('getter on key throws', () => ({ get key(): never { throw new Error('boom key') }, a: 1 }))
add('setter only on key', () => ({ set key(_v: any) {}, a: 1 }))
add('getter on __self', () => ({ get __self() { note('get __self'); return 1 }, a: 1 }))
add('getter on __source', () => ({ get __source() { note('get __source'); return 1 }, a: 1 }))
add('getter on ref', () => ({ get ref() { note('get ref'); return 'R' }, a: 1 }))
add('getter on ref undefined', () => ({ get ref() { note('get ref'); return undefined }, a: 1 }))
add('getters on props', () => ({ get a() { note('get a'); return 1 }, b: 2, get c() { note('get c'); return 3 } }))
add('getter throws', () => ({ a: 1, get b(): never { note('get b'); throw new Error('boom') }, get c() { note('get c'); return 3 } }))
add('getter changes a later value', () => { const o: any = { get a() { note('get a'); o.b = 'changed'; return 1 }, b: 2 }; return o })
add('getter adds a key', () => { const o: any = { get a() { note('get a'); o.z = 9; return 1 }, b: 2 }; return o })
add('getter makes a later key not enumerable', () => { const o: any = { get a() { note('get a'); Object.defineProperty(o, 'b', { enumerable: false }); return 1 }, b: 2, c: 3 }; return o })
add('getter on key changes a prop', () => { const o: any = { get key() { note('get key'); o.b = 'changed'; return 'K' }, a: 1, b: 2 }; return o })
add('getter deletes a later key', () => { const o: any = { get a() { note('get a'); delete o.b; return 1 }, b: 2, c: 3 }; return o }, { open: true })
add('symbol key', () => ({ a: 1, [Symbol('s')]: 2 }))
add('symbol key not enumerable', () => Object.defineProperty({ a: 1 }, Symbol('s'), { value: 2, enumerable: false }))
add('symbol getter', () => Object.defineProperty({ a: 1 }, Symbol('s'), { enumerable: true, get() { note('get symbol'); return 2 } }))
add('inherited enumerable', () => Object.assign(Object.create({ inh: 1 }), { own: 2 }))
add('inherited key', () => Object.create({ key: 'inherited' }, { a: { value: 1, enumerable: true } }))
add('inherited getter on key', () => Object.create({ get key() { note('get inherited key'); return 'IK' } }, { a: { value: 1, enumerable: true } }))
add('inherited getter on __self', () => Object.create({ get __self() { note('get inherited __self'); return 1 } }, { a: { value: 1, enumerable: true } }))
add('inherited ref', () => Object.create({ ref: 'inherited' }, { a: { value: 1, enumerable: true } }))
add('same name own and inherited', () => Object.assign(Object.create({ a: 'inherited' }), { a: 'own' }))
add('class instance', () => new (class { x = 1; get y() { note('get y'); return 2 } })())
add('null prototype', () => Object.assign(Object.create(null), { a: 1, b: 2 }))
add('null prototype with key', () => Object.assign(Object.create(null), { key: 'k', a: 1 }))
add('prototype is a Proxy', () => Object.assign(Object.create(new Proxy({ inh: 1 }, handler('proto'))), { a: 1 }), { trap: true })
add('array', () => ['x', 'y'])
add('array with a prop', () => Object.assign(['x'], { extra: 1 }))
add('function with props', () => Object.assign(() => {}, { a: 1, b: 2 }))
add('string', () => 'ab')
add('number', () => 5)
add('boolean', () => true)
add('symbol', () => Symbol('s'))
add('bigint', () => BigInt(10))
add('String object', () => new String('ab'))
add('string with String.prototype.key', () => { (String.prototype as any).key = 'SK'; later(() => void delete (String.prototype as any).key); return 'ab' })
add('Date', () => new Date(0))
add('Map with a prop', () => Object.assign(new Map(), { a: 1 }))
add('arguments', () => (function (..._a: any[]) { return arguments })(1, 2))
add('typed array', () => new Uint8Array([1, 2]))
add('error', () => Object.assign(new Error('x'), { a: 1 }))
add('own __proto__ object', () => JSON.parse('{"__proto__": {"x": 1}, "a": 1}'))
add('own __proto__ null', () => JSON.parse('{"__proto__": null, "a": 1}'))
add('own __proto__ number', () => JSON.parse('{"__proto__": 5, "a": 1}'))
add('computed __proto__', () => ({ ['__proto__']: { x: 1 }, a: 1 }))
add('__proto__ in a literal sets the prototype', () => ({ __proto__: { x: 1 }, a: 1 }))
add('Object.prototype enumerable', () => { (Object.prototype as any).bar = 1; later(() => void delete (Object.prototype as any).bar); return { a: 1 } })
add('Object.prototype writable foo', () => { (Object.prototype as any).foo = 1; later(() => void delete (Object.prototype as any).foo); return { a: 1, foo: 5 } })
add('Object.prototype key', () => { (Object.prototype as any).key = 'OK'; later(() => void delete (Object.prototype as any).key); return { a: 1 } })
add('Object.prototype getter on key', () => { onObjectPrototype('key', { get() { note('get Object.prototype.key'); return 'OK' } }); return { a: 1 } })
add('Object.prototype getter on __self', () => { onObjectPrototype('__self', { get() { note('get Object.prototype.__self'); return 1 } }); return { a: 1 } })
add('Object.prototype getter on ref', () => { onObjectPrototype('ref', { get() { note('get Object.prototype.ref'); return 'OR' } }); return { a: 1 } })
add('Function.prototype.call replaced', () => {
  const call = Function.prototype.call
  Function.prototype.call = function (this: any, ...args: any[]) { note('call replaced'); return (call as any).apply(this, args) } as any
  later(() => void (Function.prototype.call = call))
  return { a: 1, b: 2 }
})
add('hasOwnProperty.call has an own call', () => {
  const call = Object.prototype.hasOwnProperty.call
  Object.defineProperty(Object.prototype.hasOwnProperty, 'call', { configurable: true, writable: true, value(this: any, ...args: any[]) { note('hasOwnProperty.call'); return (call as any).apply(this, args) } })
  later(() => void delete (Object.prototype.hasOwnProperty as any).call)
  return { a: 1 }
})
add('Object.getOwnPropertySymbols replaced', () => {
  const original = Object.getOwnPropertySymbols
  Object.getOwnPropertySymbols = function (o: any) { note('getOwnPropertySymbols replaced'); return original(o) }
  later(() => void (Object.getOwnPropertySymbols = original))
  return { a: 1 }
})
add('Object.prototype setter', () => { onObjectPrototype('foo', { set(v: any) { note('Object.prototype.foo setter ' + v) } }); return { a: 1, foo: 5 } }, { open: true })
add('Object.prototype read-only', () => { onObjectPrototype('foo', { value: 1, writable: false }); return { a: 1, foo: 5 } }, { open: true })
add('Object.prototype getter only', () => { onObjectPrototype('foo', { get() { return 1 } }); return { a: 1, foo: 5 } }, { open: true })
add('Proxy', () => new Proxy({ a: 1, b: 2, key: 'k' }, handler('P')), { trap: true })
add('Proxy without key', () => new Proxy({ a: 1, b: 2 }, handler('P')), { trap: true })
add('Proxy of an array', () => new Proxy([1, 2], handler('P')), { trap: true })
add('Proxy that adds a key', () => new Proxy({ a: 1 }, {
  ownKeys: () => ['a', 'ghost'],
  getOwnPropertyDescriptor: (t, k) => (k === 'ghost' ? { value: 1, enumerable: true, configurable: true } : Reflect.getOwnPropertyDescriptor(t, k)),
  get: (t: any, k) => (k === 'ghost' ? 'G' : t[k]),
}), { trap: true })
add('Proxy with get only', () => new Proxy({ a: 1 }, { get: (t: any, k) => (note('get ' + show(k)), t[k]) }), { trap: true })
add('revoked Proxy', () => { const r = Proxy.revocable({}, {}); r.revoke(); return r.proxy }, { trap: true })

type Outcome = { name: string; kind: string; trap: boolean; open: boolean; log: string; err: string | null; key?: string; keys?: string; props?: string; ref?: string; proto?: string }

function outcome(c: Case, kind: string): Outcome {
  events = []
  const o: Outcome = { name: c.name, kind, trap: !!c.trap, open: !!c.open, log: '', err: null }
  try {
    const base = kind === 'cloneElement' ? createElement('div', { a: 'base', z: 'last', ref: 'baseRef' } as any) : null
    const config = c.make()
    const element: any = base ? cloneElement(base, config, 'child') : createElement('div', config, 'child')
    o.key = show(element.key)
    o.props = describeProps(element.props)
    o.keys = Reflect.ownKeys(element.props).map(show).join(',')
    o.ref = show(element.ref)
    o.proto = Object.getPrototypeOf(element.props) === Object.prototype ? 'Object.prototype' : 'other'
  } catch (e: any) {
    o.err = e && e.name + ': ' + e.message
  } finally {
    while (undos.length) undos.pop()!()
  }
  o.log = events.join(' | ')
  return o
}

const out: Outcome[] = []
for (const kind of ['createElement', 'cloneElement']) for (const c of cases) out.push(outcome(c, kind))
globalThis.result = JSON.stringify(out)
`

type propsOutcome struct {
	Name, Kind string
	Trap, Open bool
	Log        string
	Err        string
	Key        string
	Keys       string
	Props      string
	Ref        string
	Proto      string
}

func propsRun(t *testing.T, root string) ([]propsOutcome, string, string) {
	t.Helper()
	writeTree(t, root, map[string]string{"props.tsx": propsPage})
	code, printed := buildPrinting(t, root, "props.tsx")
	var out []propsOutcome
	if err := json.Unmarshal([]byte(evalResult(t, code)), &out); err != nil {
		t.Fatal(err)
	}
	return out, printed, code
}

// reactWithLoopsChanged returns a copy of testdata/ssr whose React has a comment inside the loop of
// each function named in which (createElement, cloneElement), so that the plugin does not know it.
func reactWithLoopsChanged(t *testing.T, which ...string) string {
	t.Helper()
	loops := map[string]string{
		"createElement": "hasOwnProperty.call(config, propName) &&",
		"cloneElement":  "!hasOwnProperty.call(config, propName) ||",
	}
	return withChangedPackage(t, "react", filepath.Join("cjs", "react.production.js"), func(source string) string {
		for _, name := range which {
			loop := loops[name]
			if strings.Count(source, loop) != 1 {
				t.Fatalf("the production file of React does not have the loop of %s once", name)
			}
			source = strings.Replace(source, loop, loop[:strings.LastIndex(loop, " ")]+" /* changed */"+loop[strings.LastIndex(loop, " "):], 1)
		}
		return source
	})
}

// Elements made after the tifa plugin changed React behave as with the React the plugin cannot
// change: for every kind of config, createElement and cloneElement give the same key, the same
// props in the same order with the same attributes, the same errors, and read and call the same
// things in the same order. The plugin sends a config to its copy by rest only when it can tell
// that the copy does what React's loop does, and every other config goes through React's loop.
// Where the plugin does not know a function, that function keeps React's loop and the build warns.
func TestSSRCopiesPropsLikeReact(t *testing.T) {
	// React with both loops changed: the plugin leaves it as it is.
	plain, printed, plainCode := propsRun(t, reactWithLoopsChanged(t, "createElement", "cloneElement"))
	if strings.Count(printed, "react 19.3.0") != 2 {
		t.Errorf("the build prints for a React it does not know:\n%s", printed)
	}
	if strings.Contains(plainCode, "__lookupGetter__") {
		t.Error("the bundle of a React the plugin does not know has the copy")
	}
	if len(plain) < 100 {
		t.Fatalf("%d outcomes", len(plain))
	}
	for _, c := range []struct {
		name    string
		root    string
		warning int
	}{
		{"both known", copyTestdata(t, "ssr", true), 0},
		{"cloneElement unknown", reactWithLoopsChanged(t, "cloneElement"), 1},
		{"createElement unknown", reactWithLoopsChanged(t, "createElement"), 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, printed, code := propsRun(t, c.root)
			if n := strings.Count(printed, "react 19.3.0"); n != c.warning {
				t.Errorf("the build prints %d warnings, want %d:\n%s", n, c.warning, printed)
			}
			if !strings.Contains(code, "__lookupGetter__") {
				t.Error("the bundle has no copy of props")
			}
			if len(got) != len(plain) {
				t.Fatalf("%d outcomes, %d with React as it is", len(got), len(plain))
			}
			for i, want := range plain {
				if got[i].Name != want.Name || got[i].Kind != want.Kind {
					t.Fatalf("outcome %d is %s %s, want %s %s", i, got[i].Kind, got[i].Name, want.Kind, want.Name)
				}
				if want.Trap {
					// What the traps of a Proxy see differs. What the element holds does not.
					got[i].Log, want.Log = "", ""
				}
				if got[i] == want {
					continue
				}
				if !want.Open {
					t.Errorf("%s %s differs:\n got %+v\nwant %+v", want.Kind, want.Name, got[i], want)
				} else if c.name == "both known" {
					t.Logf("%s %s differs, as listed:\n got %+v\nwant %+v", want.Kind, want.Name, got[i], want)
				}
			}
		})
	}
}
