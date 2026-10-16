// What tifa dev runs on each VM after Intl and before the bundle: it wraps the Intl
// constructors and the toLocale* methods so that a call that leaves the locale, or for a date
// the time zone, to the default is written down with its stack. The pool reads the list after
// each render with __tifa_intl_warnings__() and logs each place once. The calls themselves
// behave as before.
(function () {
  'use strict'
  const g = globalThis
  // The first use of Intl installs it, so the real functions are in place to be wrapped.
  const intl = g.Intl
  const found = []
  // depth counts the wrapped calls in progress, so a call one of them makes is not checked.
  let depth = 0

  function __tifaIntlRecord(call, locale, zone) {
    if (found.length >= 32) return
    const frames = String(new Error().stack)
      .split('\n')
      .filter((frame) => frame.trim() !== '' && !frame.includes('__tifaIntl'))
    found.push({ call: call, locale: locale, zone: zone, stack: frames.slice(0, 8).join('\n') })
  }

  function __tifaIntlCheck(call, args, dates) {
    if (depth > 0) return
    const locales = args[0]
    const options = args[1]
    const locale = locales === undefined || (Array.isArray(locales) && locales.length === 0)
    const zone = dates && (options === undefined || options === null || options.timeZone === undefined)
    if (locale || zone) __tifaIntlRecord(call, locale, zone)
  }

  function wrap(owner, name, wrapped) {
    const d = Object.getOwnPropertyDescriptor(owner, name)
    Object.defineProperty(owner, name, { value: wrapped, writable: d.writable, enumerable: d.enumerable, configurable: d.configurable })
  }

  function wrapConstructor(name, dates) {
    const call = 'Intl.' + name
    wrap(intl, name, new Proxy(intl[name], {
      construct: function __tifaIntlConstruct(target, args, newTarget) {
        __tifaIntlCheck(call, args, dates)
        depth++
        try {
          return Reflect.construct(target, args, newTarget)
        } finally {
          depth--
        }
      },
      apply: function __tifaIntlApply(target, self, args) {
        __tifaIntlCheck(call, args, dates)
        depth++
        try {
          return Reflect.apply(target, self, args)
        } finally {
          depth--
        }
      },
    }))
  }

  function wrapMethod(owner, type, name, dates) {
    const call = type + '.prototype.' + name
    const real = owner[name]
    wrap(owner, name, function __tifaIntlMethod() {
      __tifaIntlCheck(call, arguments, dates)
      depth++
      try {
        return real.apply(this, arguments)
      } finally {
        depth--
      }
    })
  }

  wrapConstructor('DateTimeFormat', true)
  wrapConstructor('NumberFormat', false)
  wrapConstructor('PluralRules', false)
  wrapMethod(Date.prototype, 'Date', 'toLocaleString', true)
  wrapMethod(Date.prototype, 'Date', 'toLocaleDateString', true)
  wrapMethod(Date.prototype, 'Date', 'toLocaleTimeString', true)
  wrapMethod(Number.prototype, 'Number', 'toLocaleString', false)
  wrapMethod(BigInt.prototype, 'BigInt', 'toLocaleString', false)

  Object.defineProperty(g, '__tifa_intl_warnings__', {
    value: function () {
      const list = JSON.stringify(found)
      found.length = 0
      return list
    },
    configurable: true,
  })
})()
