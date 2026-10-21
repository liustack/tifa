import { DOMException } from './dom-exception'
import { getter, uncurry } from './intrinsics'

const { Object, Array, Number, String, Boolean, BigInt, Symbol, Date, RegExp, Map, Set,
  ArrayBuffer, SharedArrayBuffer, Uint8Array, DataView, TypeError, Error, AggregateError } = globalThis
const { getPrototypeOf, getOwnPropertyDescriptor, defineProperty, keys, hasOwn } = Object
const isArray = Array.isArray
const isView = ArrayBuffer.isView
const mapHas = uncurry(Map.prototype.has), mapGet = uncurry(Map.prototype.get)
const mapSet = uncurry(Map.prototype.set), mapForEach = uncurry(Map.prototype.forEach)
const setHas = uncurry(Set.prototype.has), setAdd = uncurry(Set.prototype.add)
const setForEach = uncurry(Set.prototype.forEach)
const includes = uncurry(Array.prototype.includes)
const dateTime = uncurry(Date.prototype.getTime)
const setBytes = uncurry(Uint8Array.prototype.set)
const bufferResizable = getter(ArrayBuffer.prototype, 'resizable')
const bufferMaxLength = getter(ArrayBuffer.prototype, 'maxByteLength')
const regexpSource = getter(RegExp.prototype, 'source')
const regexpFlags = ['hasIndices','global','ignoreCase','multiline','dotAll','unicode','unicodeSets','sticky']
  .map(property => getter(RegExp.prototype, property))
const boxed = { Number, String, Boolean, BigInt }
const unbox = Object.fromEntries(Object.entries(boxed).map(([name, C]) => [name, uncurry(C.prototype.valueOf)]))

const constructors = { Number, String, Boolean, BigInt, Date, RegExp, Map, Set,
  Int8Array, Uint8Array, Uint8ClampedArray, Int16Array, Uint16Array, Int32Array,
  Uint32Array, BigInt64Array, BigUint64Array, Float16Array, Float32Array, Float64Array, DataView }
// AggregateError consumes an iterable even when initially empty. Keep that
// iterator private too, rather than consulting Array.prototype during a clone.
const emptyErrors = { [Symbol.iterator]() { return { next: () => ({ done: true }) } } }
const errors = { Error, EvalError, RangeError, ReferenceError, SyntaxError, TypeError, URIError, AggregateError }
const typedArray = getPrototypeOf(Uint8Array.prototype)
const viewGetters = (prototype: object, length: string) => ({
  buffer: getter(prototype, 'buffer'), offset: getter(prototype, 'byteOffset'), length: getter(prototype, length),
})
const arrayView = viewGetters(typedArray, 'length')
const dataView = viewGetters(DataView.prototype, 'byteLength')
const arrayValues = uncurry(typedArray.values)
const define = (object: object, key: string, value: unknown, enumerable = false) =>
  defineProperty(object, key, { value, writable: true, configurable: true, enumerable })
const fail = (message = 'Value cannot be cloned'): never => { throw new DOMException(message, 'DataCloneError') }

// These intrinsics check internal slots, without reading user properties or
// Symbol.toStringTag. In particular, a Proxy cannot impersonate its target's brand.
const typedArrayName = getter(typedArray, Symbol.toStringTag)
const isError = Error.isError
const brands: [string, Function][] = [
  ['ArrayBuffer', getter(ArrayBuffer.prototype, 'byteLength')],
  ['Date', dateTime],
  ['Map', getter(Map.prototype, 'size')],
  ['Set', getter(Set.prototype, 'size')],
  ['RegExp', getter(RegExp.prototype, 'global')],
  ...[Number, String, Boolean, BigInt, Symbol].map(C => [C.name, uncurry(C.prototype.valueOf)] as [string, Function]),
  ['SharedArrayBuffer', getter(SharedArrayBuffer.prototype, 'byteLength')],
  ['WeakMap', uncurry(WeakMap.prototype.has)],
  ['WeakSet', uncurry(WeakSet.prototype.has)],
  ['WeakRef', uncurry(WeakRef.prototype.deref)],
]
function kindOf(value: object): string {
  // Array.isArray throws for a revoked Proxy. Other unsupported values below
  // also become DataCloneError rather than leaking an intrinsic TypeError.
  try { if (isArray(value)) return 'Array' } catch { return '' }
  if (isError(value)) return 'Error'
  const name = typedArrayName(value)
  if (name !== undefined) return name
  if (isView(value)) {
    try { dataView.length(value); return 'DataView' } catch { return '' }
  }
  for (let i = 0; i < brands.length; i++) {
    const name = brands[i][0], check = brands[i][1]
    try {
      // RegExp.prototype is the one non-RegExp accepted by its flag getters.
      if (check(value) === undefined && name === 'RegExp') continue
      return name
    } catch { /* A different brand. */ }
  }
  return 'Object'
}

// Host shims are ordinary JS objects to QuickJS, but have no clone semantics.
// Supply their prototypes after host.ts installs them. No instance or input graph
// is retained between calls.
export function makeStructuredClone(unsupported: object[]) {
  // A failed brand check must not turn an ordinary wrapper around a built-in
  // into a valid clone. This also conservatively rejects forged built-in prototypes.
  // Collect native iterator prototypes without ever advancing an input iterator.
  const iterators = [[][Symbol.iterator](), new Map().entries(), new Set().values(),
    ''[Symbol.iterator](), ''.matchAll(/./g), Iterator.from({ next() { return { done: true } } }),
    Iterator.from([]).map(value => value), Iterator.concat([])].map(getPrototypeOf)
  const rejected = [...unsupported, ...iterators, ...Object.values(constructors).map(C => C.prototype),
    ...Object.values(errors).map(C => C.prototype), Symbol.prototype, ArrayBuffer.prototype, SharedArrayBuffer.prototype,
    WeakMap.prototype, WeakSet.prototype, WeakRef.prototype, FinalizationRegistry.prototype,
    Promise.prototype, getPrototypeOf(getPrototypeOf((function* () {})())),
    getPrototypeOf(getPrototypeOf((async function* () {})()))]
  function hasPrototype(value: object, prototypes: object[]) {
    // JS cannot detect transparent proxies. Their own enumerable properties
    // are copied through traps, just like those of ordinary objects and arrays.
    const visited = new Set<object>()
    try {
      for (let p = getPrototypeOf(value); p !== null; p = getPrototypeOf(p)) {
        if (includes(prototypes, p)) return true
        if (p === Object.prototype || setHas(visited, p)) return false
        setAdd(visited, p)
      }
    } catch {
      // An inaccessible prototype does not prevent copying an object's own keys.
    }
    return false
  }
  return function structuredClone<T>(value: T, options?: StructuredSerializeOptions): T {
    if (!arguments.length) throw new TypeError('structuredClone requires an argument')
    if (options != null && typeof options !== 'object' && typeof options !== 'function') {
      throw new TypeError('Expected structuredClone options')
    }
    const transfer = options?.transfer
    if (transfer !== undefined) {
      for (const entry of transfer) fail('structuredClone transfer is not supported')
    }
    const seen = new Map<object, any>()
    function clone(value: any): any {
      if (typeof value === 'symbol' || typeof value === 'function') fail()
      if (value === null || typeof value !== 'object') return value
      if (mapHas(seen, value)) return mapGet(seen, value)
      const kind = kindOf(value)
      if (!kind) fail()
      let result: any
      switch (kind) {
        case 'Symbol': case 'SharedArrayBuffer': case 'WeakMap': case 'WeakSet': case 'WeakRef':
          fail()
        case 'Object':
          if (hasPrototype(value, rejected)) fail()
          // Intl is lazy. Reading its descriptor leaves an unused VM lazy.
          const intl = getOwnPropertyDescriptor(globalThis, 'Intl')?.value
          const names = ['NumberFormat', 'DateTimeFormat', 'PluralRules']
          for (let i = 0; intl && i < names.length; i++) {
            if (intl[names[i]] && hasPrototype(value, [intl[names[i]].prototype])) fail()
          }
          result = {}
          break
        case 'Array': result = new Array(value.length); break
        case 'Date': result = new Date(dateTime(value)); break
        case 'RegExp': {
          let flags = ''
          for (let i = 0; i < regexpFlags.length; i++) {
            if (regexpFlags[i](value)) flags += 'dgimsuvy'[i]
          }
          result = new RegExp(regexpSource(value), flags)
          break
        }
        case 'Map': result = new Map(); break
        case 'Set': result = new Set(); break
        case 'ArrayBuffer': {
          // Construct a byte view first: detached buffers must throw, including
          // zero-length ones. Do not consult species or a user-defined slice.
          try {
            const bytes = new Uint8Array(value)
            result = bufferResizable(value)
              ? new ArrayBuffer(arrayView.length(bytes), { maxByteLength: bufferMaxLength(value) })
              : new ArrayBuffer(arrayView.length(bytes))
            setBytes(new Uint8Array(result), bytes)
          } catch { fail('Detached ArrayBuffer cannot be cloned') }
          break
        }
        case 'Error': {
          if (value instanceof DOMException) {
            result = new DOMException(value.message, value.name)
          } else {
            const name = value.name
            const C = typeof name === 'string' && hasOwn(errors, name) ? errors[name] : Error
            const message = getOwnPropertyDescriptor(value, 'message')
            const text = message && 'value' in message ? `${message.value}` : undefined
            result = C === AggregateError ? new AggregateError(emptyErrors, text) : new C(text)
          }
          break
        }
        case 'Number': case 'String': case 'Boolean': case 'BigInt':
          result = Object(unbox[kind](value))
          break
        default: {
          const view = kind === 'DataView' ? dataView : arrayView
          let buffer, offset, length
          try {
            buffer = view.buffer(value)
            offset = view.offset(value)
            length = view.length(value)
            // Typed array getters return zero for detached or out-of-bounds
            // views, but the intrinsic iterator validates both conditions.
            if (kind !== 'DataView') arrayValues(value)
            new Uint8Array(buffer)
          } catch { fail('Detached ArrayBufferView cannot be cloned') }
          if (kindOf(buffer) !== 'ArrayBuffer') fail()
          if (bufferResizable(buffer)) {
            fail('Views on resizable ArrayBuffers cannot be cloned')
          }
          result = new constructors[kind](clone(buffer), offset, length)
        }
      }
      mapSet(seen, value, result)
      if (kind === 'Object' || kind === 'Array') {
        const ownKeys = keys(value)
        for (let i = 0; i < ownKeys.length; i++) {
          const key = ownKeys[i]
          if (hasOwn(value, key)) define(result, key, clone(value[key]), true)
        }
      } else if (kind === 'Map') {
        // Snapshot entries before getters in a key or value can change the map.
        const entries: any[] = []
        mapForEach(value, (entry, key) => { entries[entries.length] = [key, entry] })
        for (let i = 0; i < entries.length; i++) mapSet(result, clone(entries[i][0]), clone(entries[i][1]))
      } else if (kind === 'Set') {
        const entries: any[] = []
        setForEach(value, entry => { entries[entries.length] = entry })
        for (let i = 0; i < entries.length; i++) setAdd(result, clone(entries[i]))
      } else if (kind === 'Error') {
        const stack = getOwnPropertyDescriptor(value, 'stack')
        if (stack && typeof stack.value === 'string') define(result, 'stack', stack.value)
        if (!(value instanceof DOMException)) {
          const cause = getOwnPropertyDescriptor(value, 'cause')
          if (cause && 'value' in cause) define(result, 'cause', clone(cause.value))
          if (result instanceof AggregateError) define(result, 'errors', clone(value.errors))
        }
      }
      return result
    }
    return clone(value)
  }
}
