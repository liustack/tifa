// AsyncLocalStorage and AsyncResource of node:async_hooks, as Cloudflare Workers has them: run,
// exit, getStore, bind and snapshot, without enterWith and disable. The engine keeps an object
// across promise jobs (slowjs.GetAsyncContext): a then or an await runs with the object set when
// it was registered. A context is a Map from each AsyncLocalStorage to its store, never changed
// once set: run sets a new one and gives the old one back after.
import { uncurry } from './intrinsics'

type Frame = Map<AsyncLocalStorage<unknown>, unknown> | undefined
type Callback<A extends unknown[], R> = (...args: A) => R

// Installed by ssr.Load before any bundle module runs. Capture it in this module and remove the
// private global before application code runs. The script alone, without it, runs but has no
// AsyncLocalStorage that works.
const context = (globalThis as any).__tifa_async_context__ as { get(): Frame; set(frame: Frame): void }
Reflect.deleteProperty(globalThis, '__tifa_async_context__')
const get = () => context.get()
const set = (frame: Frame) => context.set(frame)

const { Map, TypeError } = globalThis
const apply = Reflect.apply
const mapGet = uncurry(Map.prototype.get), mapSet = uncurry(Map.prototype.set)
const mapHas = uncurry(Map.prototype.has), mapDelete = uncurry(Map.prototype.delete)
const mapForEach = uncurry(Map.prototype.forEach)

function copy(frame: Frame) {
  const next = new Map<AsyncLocalStorage<unknown>, unknown>()
  if (frame !== undefined) mapForEach(frame, (store: unknown, key: AsyncLocalStorage<unknown>) => mapSet(next, key, store))
  return next
}

function runIn(frame: Frame, fn: Function, self: unknown, args: unknown[]) {
  const outer = get()
  set(frame)
  try {
    return apply(fn, self, args)
  } finally {
    set(outer)
  }
}

function checkFunction(fn: unknown, name: string) {
  if (typeof fn !== 'function') throw new TypeError(`The "${name}" argument must be of type function`)
}

export class AsyncLocalStorage<T> {
  // bind returns fn, which runs in the context of the call of bind.
  static bind<F extends Function>(fn: F): F {
    checkFunction(fn, 'fn')
    const frame = get()
    return function (this: unknown, ...args: unknown[]) {
      return runIn(frame, fn, this, args)
    } as unknown as F
  }

  // snapshot returns a function that calls its argument in the context of the call of snapshot.
  static snapshot() {
    const frame = get()
    return <A extends unknown[], R>(fn: Callback<A, R>, ...args: A): R => {
      checkFunction(fn, 'fn')
      return runIn(frame, fn, undefined, args)
    }
  }

  run<A extends unknown[], R>(store: T, callback: Callback<A, R>, ...args: A): R {
    checkFunction(callback, 'callback')
    const frame = copy(get())
    mapSet(frame, this, store)
    return runIn(frame, callback, undefined, args)
  }

  exit<A extends unknown[], R>(callback: Callback<A, R>, ...args: A): R {
    checkFunction(callback, 'callback')
    const frame = get()
    if (frame === undefined || !mapHas(frame, this)) return apply(callback, undefined, args)
    const next = copy(frame)
    mapDelete(next, this)
    return runIn(next, callback, undefined, args)
  }

  getStore(): T | undefined {
    const frame = get()
    return frame === undefined ? undefined : mapGet(frame, this)
  }
}

// An AsyncResource keeps the context it was made in, for callbacks that run later.
export class AsyncResource {
  #frame: Frame

  static bind<F extends Function>(fn: F, type?: string, thisArg?: unknown): F {
    return new AsyncResource(type || 'bound-anonymous-fn').bind(fn, thisArg)
  }

  constructor(type: string) {
    if (typeof type !== 'string') throw new TypeError('The "type" argument must be of type string')
    this.#frame = get()
  }

  runInAsyncScope<A extends unknown[], R>(fn: (this: unknown, ...args: A) => R, thisArg?: unknown, ...args: A): R {
    checkFunction(fn, 'fn')
    return runIn(this.#frame, fn, thisArg, args)
  }

  // bind returns fn, which runs in the context of the resource, with thisArg as this when given.
  bind<F extends Function>(fn: F, thisArg?: unknown): F {
    checkFunction(fn, 'fn')
    const frame = this.#frame
    const bound = thisArg === undefined
      ? function (this: unknown, ...args: unknown[]) { return runIn(frame, fn, this, args) }
      : (...args: unknown[]) => runIn(frame, fn, thisArg, args)
    return bound as unknown as F
  }
}
