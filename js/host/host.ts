// Web APIs for SSR. Only crypto, for random bytes, and AsyncLocalStorage, for the context promise
// jobs carry, call the Go host.
// Intl and the toLocale* methods are not here: internal/ssr registers them on every VM from
// Go (github.com/liustack/slowjs/intl), in every CLDR locale, with en-US and UTC as the
// defaults. In the browser they are the browser's own.
import { makeStructuredClone } from './structured-clone'
import { crypto } from './crypto'
import { DOMException } from './dom-exception'
import { atob, btoa } from './base64'
import 'core-js/web/url.js'
import { TextEncoder, TextDecoder } from '@kayahr/text-encoding/no-encodings'
import AbortController from 'abort-controller'
import { Headers, Response } from 'whatwg-fetch'
import { AsyncLocalStorage, AsyncResource } from './async-local-storage'

// whatwg-fetch also installs a fetch function as a side effect. Remove it before
// running any application code. No XMLHttpRequest or other transport exists.
Reflect.deleteProperty(globalThis, 'fetch')
Reflect.deleteProperty(globalThis, 'Request')

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
    log() {},
    info() {},
    debug() {},
    warn() {},
    assert(condition: unknown, ...args: unknown[]) {
      if (!condition) throw new Error(`Assertion failed: ${args.map(String).join(' ')}`)
    },
    error(...args: unknown[]) {
      throw new Error(args.map(String).join(' '))
    },
  },
})

globalThis.structuredClone = makeStructuredClone([URL.prototype, URLSearchParams.prototype,
  AbortController.prototype, Object.getPrototypeOf(new AbortController().signal),
  Headers.prototype, Response.prototype, TextEncoder.prototype, TextDecoder.prototype])

// The stand-in of node:async_hooks reads its two classes here (see nodeModuleCode in
// js/vite/plugin.js).
Object.defineProperty(globalThis, '__tifa_async_hooks__', { value: Object.freeze({ AsyncLocalStorage, AsyncResource }) })
