// Run by TestDevClientShowsBrowserErrors with the path of js/dev/client.js: runs the dev client
// in a vm against a small fake document and checks the panel of browser errors.
import { readFileSync } from 'node:fs'
import vm from 'node:vm'

const code = readFileSync(process.argv[2], 'utf8')

class El {
  children = []
  listeners = {}
  parent = null
  className = ''
  textContent = ''
  shadow = null
  constructor(tag) {
    this.tag = tag
  }
  // The build error overlay is built from an HTML string, which this fake does not parse.
  set innerHTML(_) {}
  querySelector(selector) {
    return ((this.found ??= {})[selector] ??= new El(selector))
  }
  attachShadow() {
    this.shadow = new El('#shadow')
    return this.shadow
  }
  append(...nodes) {
    for (const n of nodes) {
      n.parent = this
      this.children.push(n)
    }
  }
  appendChild(n) {
    this.append(n)
    return n
  }
  remove() {
    if (this.parent) this.parent.children.splice(this.parent.children.indexOf(this), 1)
    this.parent = null
  }
  addEventListener(type, fn) {
    ;(this.listeners[type] ??= []).push(fn)
  }
  click() {
    for (const fn of this.listeners.click ?? []) fn({})
  }
  walk(fn) {
    fn(this)
    for (const c of this.children) c.walk(fn)
    if (this.shadow) this.shadow.walk(fn)
  }
  all(match) {
    const found = []
    this.walk((n) => match(n) && found.push(n))
    return found
  }
}

function boot() {
  const listeners = {}
  const sources = []
  const html = new El('html')
  const sent = []
  const win = {
    __tifaDevServed: 'v1',
    __tifaDevRendered: 1,
    document: { createElement: (tag) => new El(tag), documentElement: html },
    performance: { getEntriesByType: () => [] },
    location: { href: 'http://127.0.0.1:9527/orders?page=2', reload() {} },
    fetch: (url, init) => {
      sent.push({ url, init })
      return Promise.resolve({ ok: true, status: 204 })
    },
    EventSource: class {
      constructor() {
        sources.push(this)
      }
      handlers = {}
      addEventListener(type, fn) {
        this.handlers[type] = fn
      }
      close() {}
    },
    addEventListener: (type, fn) => (listeners[type] ??= []).push(fn),
    setTimeout,
    clearTimeout,
    console,
  }
  win.window = win
  vm.createContext(win)
  vm.runInContext(code, win)
  const fire = (type, event) => (listeners[type] ?? []).forEach((fn) => fn(event))
  const make = (src) => vm.runInContext(src, win)
  const status = (s) => sources[0].handlers.status({ data: JSON.stringify({ version: 'v1', render: 1, ...s }) })
  return { html, fire, make, status, sent }
}

const failures = []
function check(cond, message) {
  if (!cond) failures.push(message)
}

const panelOf = (html) => html.children.find((c) => c.tag === 'tifa-dev-errors')
const entries = (html) => {
  const panel = panelOf(html)
  return panel ? panel.all((n) => n.className === 'entry') : []
}
const titleOf = (entry) => entry.all((n) => n.tag === 'h2')[0].textContent
const countOf = (entry) => entry.all((n) => n.className === 'count')[0]?.textContent ?? ''
const bodyOf = (entry) => entry.all((n) => n.tag === 'pre')[0].textContent
const button = (entry, label) => entry.all((n) => n.tag === 'button' && n.textContent === label)[0]
const report = (fire, message, stack = '', kind = 'uncaught') => fire('tifa:browser-error', { detail: { kind, message, stack } })

{
  // The event of the runtime renders an entry per error, newest first, titled by kind.
  const { html, fire } = boot()
  check(panelOf(html) === undefined, 'a panel shows with no error')
  fire('tifa:browser-error', { detail: { kind: 'recoverable', message: 'Hydration failed because x', stack: 'Error: Hydration failed because x\n    at a', componentStack: '\n    at Clock' } })
  report(fire, 'Minified React error #418', '', 'recoverable')
  report(fire, 'boom', 'Error: boom\n    at b')
  const shown = entries(html)
  check(shown.length === 3, `${shown.length} entries`)
  check(shown.map(titleOf).join('|') === 'Uncaught error|Recoverable error|Hydration mismatch', `titles ${shown.map(titleOf)}`)
  check(bodyOf(shown[2]).includes('at Clock') && bodyOf(shown[2]).includes('Component stack:'), `body ${bodyOf(shown[2])}`)
  check(bodyOf(shown[1]) === 'Minified React error #418', `body without stack ${bodyOf(shown[1])}`)
}

{
  // window errors and rejections, with Error objects and without.
  const { html, fire, make } = boot()
  fire('error', { error: make("new Error('late')"), message: 'late' })
  fire('error', { error: null, message: 'Script error.', filename: 'http://x/a.js', lineno: 3, colno: 4 })
  fire('unhandledrejection', { reason: make("new Error('nope')") })
  fire('unhandledrejection', { reason: 'text reason' })
  fire('unhandledrejection', { reason: make('({code: 7})') })
  const shown = entries(html)
  check(shown.map(titleOf).join('|') === 'Unhandled rejection|Unhandled rejection|Unhandled rejection|Uncaught error|Uncaught error', `titles ${shown.map(titleOf)}`)
  check(bodyOf(shown[2]).startsWith('Error: nope') && bodyOf(shown[1]) === 'text reason' && bodyOf(shown[0]) === '{"code":7}', `bodies ${shown.map(bodyOf).join(' / ')}`)
  check(bodyOf(shown[3]).includes('http://x/a.js:3:4') && bodyOf(shown[4]).includes('late'), `bodies ${shown.map(bodyOf).join(' / ')}`)
}

{
  // Identical errors count up instead of stacking, a different one starts its own entry.
  const { html, fire } = boot()
  report(fire, 'again', 'Error: again\n    at c')
  check(countOf(entries(html)[0]) === '', 'a single error shows a count')
  report(fire, 'again', 'Error: again\n    at c')
  report(fire, 'again', 'Error: again\n    at c')
  check(entries(html).length === 1 && countOf(entries(html)[0]) === '×3', `count ${entries(html).length} ${countOf(entries(html)[0])}`)
  report(fire, 'again', 'Error: again\n    at other')
  check(entries(html).length === 2, `a different stack gives ${entries(html).length} entries`)
}

{
  // Dismiss removes one entry, Dismiss all removes the panel, a repeat after dismissing starts at one.
  const { html, fire } = boot()
  for (const m of ['one', 'two', 'three']) report(fire, m)
  button(entries(html)[0], 'Dismiss').click()
  check(entries(html).map(bodyOf).join() === 'two,one', `after dismissing ${entries(html).map(bodyOf)}`)
  report(fire, 'two')
  check(entries(html).length === 2 && countOf(entries(html)[0]) === '×2', 'a repeat of a live entry did not count')
  const all = panelOf(html).all((n) => n.tag === 'button' && n.textContent === 'Dismiss all')[0]
  check(all !== undefined, 'no Dismiss all with several entries')
  all.click()
  check(panelOf(html) === undefined, 'Dismiss all left the panel')
  report(fire, 'two')
  check(entries(html).length === 1 && countOf(entries(html)[0]) === '', 'a repeat after dismissing was counted')
  button(entries(html)[0], 'Dismiss').click()
  check(panelOf(html) === undefined, 'dismissing the last entry left the panel')
}

{
  // At most 20 entries stay: the oldest go.
  const { html, fire } = boot()
  for (let i = 0; i < 25; i++) report(fire, `e${i}`)
  const shown = entries(html)
  check(shown.length === 20, `${shown.length} entries kept`)
  check(bodyOf(shown[0]) === 'e24' && bodyOf(shown[19]) === 'e5', `kept ${bodyOf(shown[0])} to ${bodyOf(shown[19])}`)
}

{
  // A build error shows its overlay next to the panel (which sits one layer below it), and the
  // panel is still there after the fix.
  const { html, fire, status } = boot()
  const tags = () => html.children.map((c) => c.tag).sort().join()
  report(fire, 'x')
  check(tags() === 'tifa-dev-errors', `elements ${tags()}`)
  status({ error: 'build failed', title: 'Build failed' })
  check(tags() === 'tifa-dev-errors,tifa-dev-overlay', `elements ${tags()}`)
  report(fire, 'y')
  check(entries(html).length === 2 && tags() === 'tifa-dev-errors,tifa-dev-overlay', 'a new browser error hid the build overlay: ' + tags())
  status({})
  check(tags() === 'tifa-dev-errors' && entries(html).length === 2, 'the panel did not survive the fix: ' + tags())
}

{
  // Each error the panel shows goes to tifa dev too, repeats included, with the page it happened
  // on. A long text is cut so that the report stays under the limit of tifa dev.
  const { fire, sent } = boot()
  fire('tifa:browser-error', { detail: { kind: 'recoverable', message: 'Hydration failed', stack: 'Error: Hydration failed\n    at a', componentStack: '\n    at Clock' } })
  fire('tifa:browser-error', { detail: { kind: 'recoverable', message: 'Hydration failed', stack: 'Error: Hydration failed\n    at a', componentStack: '\n    at Clock' } })
  fire('unhandledrejection', { reason: 'x'.repeat(200000) })
  check(sent.length === 3, `${sent.length} reports sent`)
  const first = sent[0]
  check(first.url === '/__tifa/dev/errors' && first.init.method === 'POST' && first.init.headers['Content-Type'] === 'application/json', `request ${JSON.stringify(first)}`)
  const body = JSON.parse(first.init.body)
  const want = { kind: 'recoverable', title: 'Hydration mismatch', message: 'Hydration failed', stack: 'Error: Hydration failed\n    at a', text: 'Error: Hydration failed\n    at a\n\nComponent stack:\n    at Clock', url: 'http://127.0.0.1:9527/orders?page=2' }
  check(JSON.stringify(body) === JSON.stringify(want), `body ${first.init.body}`)
  check(sent[1].init.body === first.init.body, 'a repeat was not sent the same')
  const long = sent[2].init.body
  check(new TextEncoder().encode(long).length <= 128 * 1024 && JSON.parse(long).kind === 'rejection', `long report of ${long.length} bytes`)
}

{
  // The limit is on the bytes of the JSON: control characters, which JSON writes as \u0000, and
  // characters of several bytes are cut until the report fits.
  const { fire, make, sent } = boot()
  fire('error', { error: make("new Error('\\u0000'.repeat(20000))") })
  fire('error', { error: make("new Error('汉'.repeat(60000) + '😀'.repeat(20000))") })
  for (const report of sent) {
    const bytes = new TextEncoder().encode(report.init.body).length
    const body = JSON.parse(report.init.body)
    check(bytes <= 128 * 1024 && body.kind === 'uncaught' && body.text.length > 1000, `a report of ${bytes} bytes, text of ${body.text.length}`)
  }
  check(sent.length === 2, `${sent.length} reports`)
}

if (failures.length > 0) {
  console.error(failures.join('\n'))
  process.exit(1)
}
console.log('ok')
