// A tiny SSR bundle for the pool tests. It follows the same contract as a real
// bundle: startRender(json) receives {buildId, url, data} and leaves
// globalThis.renderResult as the HTML on success, or a JSON string, {redirect} or {error}.
// data.mode selects the behaviour, data.text is echoed into the HTML.

let renders = 0
let dirty = false

const page = (input) =>
  `<main data-build="${input.buildId}" data-url="${input.url}" data-renders="${renders}" data-dirty="${dirty}">${input.data.text ?? ''}</main>`

// Tree is a component that renders itself without end, as a page with a cycle in its data can.
const Tree = (depth) => '<li>' + Tree(depth + 1) + '</li>'

// caught runs work the way the runtime's startRender does: in an async function whose catch
// reports any error as {error}, and whose finally cleans up.
const caught = (work) => {
  void (async () => {
    try {
      await null
      globalThis.renderResult = work()
    } catch (error) {
      globalThis.renderResult = JSON.stringify({ error: `${error.name}: ${error.message}\n${error.stack}` })
    } finally {
      globalThis.cleanedUp = true
    }
  })()
}

// fill allocates until the VM's memory limit stops it, in large or in small pieces.
const fill = (large) => {
  const kept = []
  if (large) {
    // Doubling keeps the failing allocation large.
    for (let size = 64 * 1024; ; size *= 2) kept.push(new Uint8Array(size).fill(1))
  }
  for (;;) kept.push({ index: kept.length })
}

globalThis.startRender = (json) => {
  const input = JSON.parse(json)
  const mode = input.data.mode ?? 'ok'
  renders++
  if (mode !== 'ok' && mode !== 'garbage' && mode !== 'cycles' && mode !== 'hold' && mode !== 'globals') dirty = true
  switch (mode) {
    case 'throw':
      throw new Error('intentional failure')
    case 'fakeinterrupt':
      // Values page code can throw that look like the engine's uncatchable errors.
      throw new InternalError('interrupted')
    case 'fakeoom':
      throw new InternalError('out of memory')
    case 'fakeoomobject':
      throw { name: 'InternalError', message: 'out of memory' }
    case 'loop':
      for (;;) {}
    case 'jobs':
      Promise.resolve().then(() => {
        for (;;) {}
      })
      return
    case 'pending':
      return
    case 'interrupt':
      // A Go function the test registers interrupts the VM, then the render runs on until the
      // interrupt stops it.
      globalThis.goInterrupt()
      for (;;) {}
    case 'gopanic':
      // A Go function the test registers panics in the middle of the render.
      globalThis.goPanic()
      return
    case 'oom':
      fill(true)
    case 'oomsmall':
      fill(false)
    case 'oomcaught':
      caught(() => fill(true))
      return
    case 'loopcaught':
      caught(() => {
        for (;;) {}
      })
      return
    case 'recurse':
      Tree(0)
    case 'recursecaught':
      caught(() => Tree(0))
      return
    case 'depth': {
      // How deep calls nest before the engine throws its RangeError.
      let depth = 0
      const down = () => {
        depth++
        down()
      }
      try {
        down()
      } catch (error) {
        globalThis.renderResult = `<p data-depth="${depth}">${error.name}: ${error.message}</p>`
      }
      return
    }
    case 'error':
      globalThis.renderResult = JSON.stringify({ error: 'page failed' })
      return
    case 'redirect':
      globalThis.renderResult = JSON.stringify({ redirect: { location: '/a?q=1', status: 307 } })
      return
    case 'globals': {
      const io = ['fetch', 'XMLHttpRequest', 'WebSocket', 'localStorage', 'sessionStorage', 'indexedDB', 'document', 'window', 'require', 'process', 'std', 'os', 'setTimeout', 'setInterval']
      globalThis.renderResult = '<pre>' + JSON.stringify(io.filter((name) => typeof globalThis[name] !== 'undefined')) + '</pre>'
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
        globalThis.renderResult = page(input) + rows.length
      })
      return
    }
    case 'hold': {
      // data.text objects that the render holds until it ends, as a large page holds its tree,
      // and that are freed as soon as it ends.
      const held = []
      for (let i = 0; i < Number(input.data.text); i++) held.push({ index: i })
      globalThis.renderResult = page(input) + held.length
      return
    }
    case 'cycles': {
      // data.text objects that point at themselves, garbage only a GC frees.
      for (let i = 0; i < Number(input.data.text); i++) {
        const node = { index: i }
        node.self = node
      }
      break
    }
  }
  Promise.resolve().then(() => {
    globalThis.renderResult = page(input)
  })
}
