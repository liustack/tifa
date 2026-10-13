// A tiny SSR bundle for the pool tests. It follows the same contract as a real
// bundle: startRender(json) receives {buildId, url, data} and leaves
// globalThis.renderResult as a JSON string, {html} on success or {error}.
// data.mode selects the behaviour, data.text is echoed into the HTML.

let renders = 0
let dirty = false

const page = (input) =>
  `<main data-build="${input.buildId}" data-url="${input.url}" data-renders="${renders}" data-dirty="${dirty}">${input.data.text ?? ''}</main>`

globalThis.startRender = (json) => {
  const input = JSON.parse(json)
  const mode = input.data.mode ?? 'ok'
  renders++
  if (mode !== 'ok' && mode !== 'garbage' && mode !== 'globals') dirty = true
  switch (mode) {
    case 'throw':
      throw new Error('intentional failure')
    case 'loop':
      for (;;) {}
    case 'jobs':
      Promise.resolve().then(() => {
        for (;;) {}
      })
      return
    case 'pending':
      return
    case 'oom': {
      const kept = []
      for (;;) kept.push(new Uint8Array(256 * 1024).fill(1))
    }
    case 'error':
      globalThis.renderResult = JSON.stringify({ error: 'page failed' })
      return
    case 'globals': {
      const io = ['fetch', 'XMLHttpRequest', 'WebSocket', 'localStorage', 'sessionStorage', 'indexedDB', 'document', 'window', 'require', 'process', 'std', 'os', 'setTimeout', 'setInterval']
      globalThis.renderResult = JSON.stringify({ html: JSON.stringify(io.filter((name) => typeof globalThis[name] !== 'undefined')) })
      return
    }
    case 'garbage': {
      // Short-lived objects, strings and cycles, like a real render produces.
      const rows = []
      for (let i = 0; i < 200; i++) {
        const row = { id: i, label: `row-${i}-${input.data.text}`, bytes: new Uint8Array(64) }
        row.self = row
        rows.push(row)
      }
      const cache = new WeakMap()
      for (const row of rows) cache.set(row, { row })
      Promise.resolve().then(() => {
        globalThis.renderResult = JSON.stringify({ html: page(input) + rows.length })
      })
      return
    }
  }
  Promise.resolve().then(() => {
    globalThis.renderResult = JSON.stringify({ html: page(input) })
  })
}
