// What tifa dev runs on each VM before the bundle: it wraps what reads the time or a random number,
// Date.now(), new Date() and Date() without a value, Math.random() and the secure random bytes of
// crypto, so that each read is written down with its stack. The pool reads the list after each
// render with __tifa_reads__(), and names the places of the app's code in the warning of a page
// that renders differently each time. The reads themselves behave as before.
(function () {
  'use strict'
  const g = globalThis
  const found = []
  const seen = new Set()

  function __tifaReadRecord(call) {
    if (found.length >= 64) return
    const stack = String(new Error().stack)
      .split('\n')
      .filter((frame) => frame.trim() !== '' && !frame.includes('__tifaRead'))
      .slice(0, 16)
      .join('\n')
    // The same read from the same place counts once, so the reads of the framework in each render
    // do not crowd out those of the app.
    const key = call + '\n' + stack
    if (seen.has(key)) return
    seen.add(key)
    found.push({ call: call, stack: stack })
  }

  const RealDate = g.Date
  const now = RealDate.now
  Object.defineProperty(RealDate, 'now', {
    value: function now__tifaRead() {
      __tifaReadRecord('Date.now()')
      return now.call(RealDate)
    },
    writable: true,
    configurable: true,
  })
  g.Date = new Proxy(RealDate, {
    construct: function __tifaReadConstruct(target, args, newTarget) {
      if (args.length === 0) __tifaReadRecord('new Date()')
      return Reflect.construct(target, args, newTarget === g.Date ? target : newTarget)
    },
    apply: function __tifaReadApply(target, self, args) {
      __tifaReadRecord('Date()')
      return Reflect.apply(target, self, args)
    },
  })
  // A date names the wrapped Date as its constructor, so that code comparing the constructor of a
  // value with Date, as seroval does, sees a date as before.
  Object.defineProperty(RealDate.prototype, 'constructor', { value: g.Date, writable: true, configurable: true })

  const random = Math.random
  Object.defineProperty(Math, 'random', {
    value: function random__tifaRead() {
      __tifaReadRecord('Math.random()')
      return random.call(Math)
    },
    writable: true,
    configurable: true,
  })

  // crypto.getRandomValues and crypto.randomUUID of the host polyfills ask Go for the bytes.
  const bytes = g.__tifa_random_bytes__
  if (typeof bytes === 'function') {
    g.__tifa_random_bytes__ = function bytes__tifaRead(n) {
      __tifaReadRecord('crypto')
      return bytes(n)
    }
  }

  Object.defineProperty(g, '__tifa_reads__', {
    value: function () {
      const list = JSON.stringify(found)
      found.length = 0
      seen.clear()
      return list
    },
    configurable: true,
  })
})()
