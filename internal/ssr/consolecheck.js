// What tifa dev runs on each VM before the bundle: the place console.log, info, debug and warn
// of the host script hand their calls to, __tifa_console__. It writes each call down, with its
// text and its stack, and the pool reads the list after each render with __tifa_console_take__()
// and logs it. Nothing here prints or reaches out of the VM. A built app does not run this
// script, so there the host script's console does nothing.
(function () {
  'use strict'
  const g = globalThis
  const maxCalls = 100
  const maxText = 4000
  let found = []
  let dropped = 0

  function replacer(_key, value) {
    if (typeof value === 'bigint') return String(value)
    if (value instanceof Map) return { Map: Array.from(value) }
    if (value instanceof Set) return { Set: Array.from(value) }
    return value
  }

  // show is one value as text: a string as it is, an error with its stack, the rest as JSON when
  // that works, as String does when it does not (a getter that throws, a cycle).
  function show(value) {
    if (typeof value === 'string') return value
    if (value instanceof Error) return String(value.stack || value)
    if (typeof value === 'function') return '[Function ' + (value.name || 'anonymous') + ']'
    if (typeof value !== 'object' || value === null) return String(value)
    try {
      const text = JSON.stringify(value, replacer)
      if (text !== undefined) return text
    } catch (_) {
      // fall through
    }
    try {
      return String(value)
    } catch (_) {
      return '[object]'
    }
  }

  // format writes the first argument as the format of the rest when it has % codes, as Node does
  // for %s, %d, %i, %f, %j, %o, %O, %c and %%.
  function format(args) {
    let rest = args
    let text = ''
    if (typeof args[0] === 'string' && args[0].includes('%')) {
      let next = 1
      text = args[0].replace(/%[sdifjoOc%]/g, (code) => {
        if (code === '%%') return '%'
        if (next >= args.length) return code
        const value = args[next++]
        switch (code) {
          case 's': return show(value)
          case 'd': return typeof value === 'object' ? 'NaN' : String(Number(value))
          case 'i': return typeof value === 'object' ? 'NaN' : String(parseInt(value, 10))
          case 'f': return String(parseFloat(value))
          case 'c': return ''
          default: return show(value)
        }
      })
      rest = args.slice(next)
      if (rest.length === 0) return text
      return text + ' ' + rest.map(show).join(' ')
    }
    return rest.map(show).join(' ')
  }

  Object.defineProperty(g, '__tifa_console__', {
    value: function __tifaConsole(level, args) {
      if (found.length >= maxCalls) {
        dropped++
        return
      }
      let text = format(args)
      if (text.length > maxText) text = text.slice(0, maxText) + '...'
      const stack = String(new Error().stack)
        .split('\n')
        .filter((frame) => frame.trim() !== '' && !frame.includes('__tifaConsole'))
        .slice(0, 16)
        .join('\n')
      found.push({ level: level, text: text, stack: stack })
    },
    configurable: true,
  })

  Object.defineProperty(g, '__tifa_console_take__', {
    value: function () {
      const list = JSON.stringify({ calls: found, dropped: dropped })
      found = []
      dropped = 0
      return list
    },
    configurable: true,
  })
})()
