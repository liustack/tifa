// Web APIs for SSR. Only crypto, for random bytes, and AsyncLocalStorage, for the context promise
// jobs carry, call the Go host.
// Intl and the toLocale* methods are not here: internal/ssr registers them on every VM from
// Go (github.com/liustack/beastjs/intl), in every CLDR locale, with en-US and UTC as the
// defaults. In the browser they are the browser's own. URL and URLSearchParams are not here either:
// internal/ssr registers the engine's (github.com/liustack/beastjs/url) before this script runs,
// and structuredClone below takes their prototypes.
import { makeStructuredClone } from './structured-clone'
import { crypto } from './crypto'
import { DOMException } from './dom-exception'
import { performance } from './performance'
import { atob, btoa } from './base64'
import { TextEncoder, TextDecoder } from '@kayahr/text-encoding/no-encodings'
import AbortController from 'abort-controller'
import { Headers, Response } from 'whatwg-fetch'
import { AsyncLocalStorage, AsyncResource } from './async-local-storage'

// whatwg-fetch also installs a fetch function as a side effect. Remove it before
// running any application code. No XMLHttpRequest or other transport exists.
Reflect.deleteProperty(globalThis, 'fetch')
Reflect.deleteProperty(globalThis, 'Request')

// Under tifa dev, internal/ssr/consolecheck.js defines __tifa_console__ before this script runs, and
// the calls of log, info, debug and warn go to it, so that the host (Go) can print them. In a built
// app there is none, and they do nothing. The VM never prints or touches a file or the network.
function dev(level: string, args: unknown[]): undefined {
  const hook = (globalThis as { __tifa_console__?: (level: string, args: unknown[]) => void }).__tifa_console__
  if (typeof hook === 'function') hook(level, args)
  return undefined
}

Object.assign(globalThis, {
  AsyncLocalStorage,
  crypto,
  DOMException,
  atob,
  btoa,
  TextEncoder,
  TextDecoder,
  AbortController,
  Headers,
  Response,
  queueMicrotask: (callback: () => void) => {
    void Promise.resolve().then(callback)
  },
  console: {
    log: (...args: unknown[]) => dev('log', args),
    info: (...args: unknown[]) => dev('info', args),
    debug: (...args: unknown[]) => dev('debug', args),
    warn: (...args: unknown[]) => dev('warn', args),
    // table and dir print as log under tifa dev. group prints its label, if it has one.
    table: (...args: unknown[]) => dev('log', args.slice(0, 1)),
    dir: (...args: unknown[]) => dev('log', args.slice(0, 1)),
    group: (...args: unknown[]) => args.length > 0 && dev('log', args),
    groupCollapsed: (...args: unknown[]) => args.length > 0 && dev('log', args),
    groupEnd() {},
    time() {},
    timeEnd() {},
    timeLog() {},
    count() {},
    countReset() {},
    trace() {},
    assert(condition: unknown, ...args: unknown[]) {
      if (!condition) throw new Error(`Assertion failed: ${args.map(String).join(' ')}`)
    },
    error(...args: unknown[]) {
      throw new Error(args.map(String).join(' '))
    },
  },
})

// A performance the VM already has stays.
if (!('performance' in globalThis)) globalThis.performance = performance

globalThis.structuredClone = makeStructuredClone([URL.prototype, URLSearchParams.prototype,
  AbortController.prototype, Object.getPrototypeOf(new AbortController().signal),
  Headers.prototype, Response.prototype, TextEncoder.prototype, TextDecoder.prototype])

// The stand-in of node:async_hooks reads its two classes here (see nodeModuleCode in
// js/vite/plugin.js).
Object.defineProperty(globalThis, '__tifa_async_hooks__', { value: Object.freeze({ AsyncLocalStorage, AsyncResource }) })
