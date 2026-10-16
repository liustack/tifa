// Pure computation APIs required by React and the router. No host I/O is exposed.
// Intl and the toLocale* methods are not here: internal/ssr registers them on every VM from
// Go (github.com/liustack/quickjs/intl), in every CLDR locale, with en-US and UTC as the
// defaults. In the browser they are the browser's own.
import 'core-js/web/url.js'
import { TextEncoder, TextDecoder } from '@kayahr/text-encoding/no-encodings'
import AbortController from 'abort-controller'
import { Headers, Response } from 'whatwg-fetch'

// whatwg-fetch also installs a fetch function as a side effect. Remove it before
// running any application code. No XMLHttpRequest or other transport exists.
Reflect.deleteProperty(globalThis, 'fetch')
Reflect.deleteProperty(globalThis, 'Request')

Object.assign(globalThis, {
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
