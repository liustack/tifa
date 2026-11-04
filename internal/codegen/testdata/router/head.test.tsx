// Run by TestGeneratedRouterBehaves in Node, bundled for the browser: the routers here run as
// they do in the browser, with the reactive stores the head code subscribes to, against a
// document that has just what that code uses.
import { readFileSync } from 'node:fs'
import { createMemoryHistory, lazyRouteComponent } from '@tanstack/react-router'
import { htmlAttributes, routeTree } from './.tifa/routes'
import { createAppRouter } from './.tifa/runtime/router'
import { bootElementId, collector, rootElementId, type RouteResult } from './.tifa/runtime/data'
import { setRouter } from './.tifa/runtime/call'
import { resetReloads, watchChunks } from './.tifa/runtime/skew'
import { POST } from './app/api/notes/note+/route.gen'
import { browserErrorEvent, hydrate, rootOptions, takeOver } from './.tifa/runtime/browser'
import { attributes, followHead, headHTML, htmlAttributesOf, setAttributes, syncHead, syncHTMLAttributes, type Tag } from './.tifa/runtime/head'

const tests: Array<[string, () => Promise<void>]> = []
const test = (name: string, fn: () => Promise<void>) => tests.push([name, fn])

function assert(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message)
}
function lacks(text: string, ...parts: string[]) {
  for (const part of parts) assert(!text.includes(part), `${part} in:\n${text}`)
}
const ok = (data: unknown): RouteResult => ({ status: 200, data })

// The browser globals a router with scroll restoration touches.
const g = globalThis as any
const browserDocument = { addEventListener() {}, removeEventListener() {}, querySelector: () => null, getElementById: () => null }
Object.assign(g, {
  window: g,
  self: g,
  addEventListener() {},
  removeEventListener() {},
  scrollTo() {},
  history: { scrollRestoration: 'auto' },
  sessionStorage: { getItem: () => null, setItem() {}, removeItem() {} },
  location: { href: 'http://tifa.test/', origin: 'http://tifa.test' },
  document: browserDocument,
})

// client loads url on a router whose source answers with answer.
async function client(url: string, answer: (url: string, id: string) => RouteResult) {
  const router = createAppRouter(routeTree, async (u, id) => answer(u, id), {
    history: createMemoryHistory({ initialEntries: [url] }),
  })
  await router.load()
  return { router }
}

// A document with just what the head code of the browser uses. It has no HTML parser, so
// building elements from HTML fails, and it records the order elements are inserted in, which
// is the order inline scripts run in.
class FakeNode {
  parentNode: FakeNode | null = null
  childNodes: FakeNode[] = []
  constructor(
    readonly nodeType: number,
    readonly nodeValue: string | null = null,
  ) {}
  get nextSibling(): FakeNode | null {
    const siblings = this.parentNode?.childNodes ?? []
    return siblings[siblings.indexOf(this) + 1] ?? null
  }
  insertBefore(node: FakeNode, before: FakeNode | null) {
    if (node.parentNode) {
      node.parentNode.childNodes.splice(node.parentNode.childNodes.indexOf(node), 1)
      fakeDocument.moved.push(node)
    } else fakeDocument.inserted.push(node)
    const at = before ? this.childNodes.indexOf(before) : this.childNodes.length
    if (at < 0) throw new Error('insertBefore: not a child')
    this.childNodes.splice(at, 0, node)
    node.parentNode = this
    // A document with a Content-Security-Policy hides the nonce of an element once it is in the
    // document: the attribute reads empty and only the nonce property holds the value.
    if (node instanceof FakeElement && node.getAttribute('nonce')) node.setAttribute('nonce', '')
    return node
  }
  append(...nodes: FakeNode[]) {
    for (const node of nodes) this.insertBefore(node, null)
  }
  remove() {
    if (this.parentNode) this.parentNode.childNodes.splice(this.parentNode.childNodes.indexOf(this), 1)
    this.parentNode = null
  }
}

class FakeElement extends FakeNode {
  attributes: Array<{ name: string; value: string }> = []
  textContent = ''
  nonce = ''
  constructor(readonly localName: string) {
    super(1)
  }
  setAttribute(name: string, value: string) {
    if (!/^[^\s"'>\/=]+$/.test(name)) throw new Error(`InvalidCharacterError: ${name}`)
    name = name.toLowerCase()
    const existing = this.attributes.find((a) => a.name === name)
    if (existing) existing.value = value
    else this.attributes.push({ name, value })
    // Setting the attribute sets the property, hiding it leaves the property as it is.
    if (name === 'nonce' && value !== '') this.nonce = value
  }
  getAttribute(name: string) {
    return this.attributes.find((a) => a.name === name)?.value ?? null
  }
  removeAttribute(name: string) {
    this.attributes = this.attributes.filter((a) => a.name !== name)
  }
}

const fakeDocument = {
  ...browserDocument,
  inserted: [] as FakeNode[],
  moved: [] as FakeNode[],
  head: new FakeElement('head'),
  documentElement: new FakeElement('html'),
  createElement: (name: string) => new FakeElement(name),
  createComment: (value: string) => new FakeNode(8, value),
  getElementById: (_id: string): FakeElement | null => null,
}

// withDocument runs fn against a fresh fake document.
async function withDocument(fn: () => Promise<void>) {
  const saved = g.document
  fakeDocument.inserted = []
  fakeDocument.moved = []
  fakeDocument.head = new FakeElement('head')
  fakeDocument.documentElement = new FakeElement('html')
  g.document = fakeDocument
  try {
    await fn()
  } finally {
    g.document = saved
  }
}

const managed = () => fakeDocument.head.childNodes.filter((node): node is FakeElement => node.nodeType === 1)
const describe = (element: FakeElement) =>
  element.localName + element.attributes.map((a) => ` ${a.name}=${a.value}`).join('') + (element.textContent ? ` [${element.textContent}]` : '')
const page = (head: { meta?: object[]; links?: object[]; styles?: object[]; headScripts?: object[] }) => [{ status: 'success', ...head }]

test('head attributes are the ones React DOM writes', async () => {
  const tags: Tag[] = [
    {
      tag: 'link',
      attrs: {
        rel: 'stylesheet',
        href: '/x.css',
        onError: 'alert(1)',
        onload: 'alert(2)',
        className: 'c',
        crossOrigin: 'anonymous',
        'data-on': true,
        hidden: false,
        title: null,
        media: undefined,
        integrity: () => 'x',
        'bad name': 'x',
        unknown: true,
        suppressHydrationWarning: true,
      },
    },
    { tag: 'script', attrs: { src: 'javascript:alert(1)', async: true, noModule: 'yes', defer: false } },
    { tag: 'meta', attrs: { httpEquiv: 'refresh', content: '5', charSet: 'utf-8' } },
  ]
  const html = headHTML(tags)
  const want =
    '<link rel="stylesheet" href="/x.css" class="c" crossorigin="anonymous" data-on="true">' +
    `<script src="javascript:throw new Error('React has blocked a javascript: URL as a security precaution.')" async="" noModule=""></script>` +
    '<meta http-equiv="refresh" content="5" charSet="utf-8">'
  assert(html === want, `head HTML\n${html}\nwant\n${want}`)
  await withDocument(async () => {
    const element = fakeDocument.createElement('link')
    setAttributes(element as unknown as Element, tags[0]!.attrs)
    assert(
      describe(element) === 'link rel=stylesheet href=/x.css class=c crossorigin=anonymous data-on=true',
      `element ${describe(element)}`,
    )
  })
})

// Under a Content-Security-Policy the server writes the nonce of the render as the first attribute
// of a style and of a script that runs, and Go takes it out again. A nonce of the tag with that
// value goes, so the page without the marks is the page the browser compares its tags with.
test('under a CSP the head marks its scripts and styles with the nonce of the render', async () => {
  const n = '0123456789abcdef0123456789abcdef'
  const tags: Tag[] = [
    { tag: 'script', attrs: { nonce: n, id: 'a' }, children: 'run()' },
    { tag: 'script', attrs: { type: 'application/ld+json' }, children: '{}' },
    { tag: 'style', attrs: { media: 'print' }, children: 'p{}' },
    { tag: 'link', attrs: { rel: 'icon', href: '/i.svg' } },
    { tag: 'script', attrs: { src: '/a.js', nonce: 'other' } },
  ]
  const html = headHTML(tags, n)
  const want =
    `<script nonce="${n}" id="a">run()</script><script type="application/ld+json">{}</script>` +
    `<style nonce="${n}" media="print">p{}</style><link rel="icon" href="/i.svg"><script nonce="${n}" src="/a.js" nonce="other"></script>`
  assert(html === want, `${html}\nwant\n${want}`)
  const unmarked = tags.map((tag) => (tag.attrs.nonce === n ? { ...tag, attrs: { id: 'a' } } : tag))
  assert(html.split(` nonce="${n}"`).join('') === headHTML(unmarked), 'the head without the marks differs')
})

test('a style attribute fails with a message that fits <html> too', async () => {
  let message = ''
  try {
    attributes({ lang: 'en', style: 'color: red' })
  } catch (error) {
    message = error instanceof Error ? error.message : String(error)
  }
  assert(message === 'head: the head and <html> take no style attribute, set styles in a stylesheet', `message ${message}`)
})

test('attributes named like properties of every object are written as any other', async () => {
  const attrs = { constructor: 'a', toString: 'b', hasOwnProperty: 'c' }
  const html = attributes(attrs)
  assert(html === ' constructor="a" toString="b" hasOwnProperty="c"', `attributes ${html}`)
  const merged = htmlAttributesOf([], { lang: 'en', ...attrs })
  const keys = Object.keys(merged).join(' ')
  assert(keys === 'lang constructor toString hasOwnProperty', `htmlAttributesOf ${keys}`)
})

test('the browser builds head elements without parsing their content', async () => {
  await withDocument(async () => {
    syncHead(page({ headScripts: [{ children: 'var a = "</script><script>alert(1)</script>"' }] }))
    const elements = managed().filter((e) => e.localName === 'script')
    assert(elements.length === 1, `head ${managed().map(describe)}`)
    // The content is the one the server writes for the same tag.
    const server = headHTML([{ tag: 'script', attrs: {}, children: 'var a = "</script><script>alert(1)</script>"' }])
    assert(server === `<script>${elements[0]!.textContent}</script>`, `server ${server}, browser ${elements[0]!.textContent}`)
    lacks(server.slice('<script>'.length, -'</script>'.length), '</script', '<script')
  })
})

test('new head elements go in in document order', async () => {
  await withDocument(async () => {
    syncHead(page({ headScripts: [{ children: 'first()' }] }))
    syncHead(page({ headScripts: [{ children: 'first()' }, { children: 'second()' }, { children: 'third()' }] }))
    const order = fakeDocument.inserted.filter((n) => n.nodeType === 1 && (n as FakeElement).localName === 'script').map((n) => (n as FakeElement).textContent)
    assert(JSON.stringify(order) === '["first()","second()","third()"]', `inserted ${JSON.stringify(order)}`)
    const inHead = managed().filter((e) => e.localName === 'script').map((e) => e.textContent)
    assert(JSON.stringify(inHead) === '["first()","second()","third()"]', `head ${JSON.stringify(inHead)}`)
  })
})

test('kept head elements move into the new order', async () => {
  await withDocument(async () => {
    syncHead(page({ links: [{ rel: 'stylesheet', href: '/a.css' }, { rel: 'stylesheet', href: '/b.css' }] }))
    const [, a, b] = managed()
    syncHead(page({ links: [{ rel: 'stylesheet', href: '/b.css' }, { rel: 'stylesheet', href: '/a.css' }] }))
    const after = managed()
    assert(after[1] === b && after[2] === a, `head ${after.map(describe)}`)
    assert(fakeDocument.inserted.length === 5, `elements were created again: ${fakeDocument.inserted.length}`)
  })
})

test('a new order moves only the head elements out of place', async () => {
  await withDocument(async () => {
    const styles = (...names: string[]) => page({ styles: names.map((name) => ({ children: `.${name}{}` })) })
    syncHead(styles('a', 'b', 'c', 'd'))
    syncHead(styles('b', 'c', 'd', 'a'))
    const order = managed().filter((e) => e.localName === 'style').map((e) => e.textContent)
    assert(JSON.stringify(order) === '[".b{}",".c{}",".d{}",".a{}"]', `head ${JSON.stringify(order)}`)
    // Moving a style element builds its style sheet again, and a stylesheet link is processed
    // again: only the one element whose place changed moves.
    const moved = fakeDocument.moved.map((n) => (n as FakeElement).textContent)
    assert(JSON.stringify(moved) === '[".a{}"]', `moved ${JSON.stringify(moved)}`)
  })
})

test('a script with a nonce stays in place while its tag stays', async () => {
  await withDocument(async () => {
    const head = page({ headScripts: [{ nonce: 'n1', children: 'run()' }], links: [{ rel: 'icon', href: '/i.svg', nonce: 'n1' }] })
    syncHead(head)
    const before = managed()
    const inserted = fakeDocument.inserted.length
    syncHead(head)
    syncHead(page({ headScripts: [{ nonce: 'n1', children: 'run()' }], links: [{ rel: 'icon', href: '/i.svg', nonce: 'n1' }] }))
    assert(fakeDocument.inserted.length === inserted, `elements were created again, the script runs again: ${managed().map(describe)}`)
    assert(managed().every((e, i) => e === before[i]), `head ${managed().map(describe)}`)
  })
})

test('a page without SSR gets the html attributes React DOM would write', async () => {
  await withDocument(async () => {
    const root = new FakeElement('div')
    const boot = new FakeElement('script')
    boot.textContent = JSON.stringify({ url: '/teams/x/one', buildId: 'b1', data: {} })
    const byId: Record<string, FakeElement> = { [rootElementId]: root, [bootElementId]: boot }
    const saved = { getElementById: fakeDocument.getElementById, fetch: g.fetch, history: g.history, location: g.location }
    g.location = { href: 'http://tifa.test/teams/x/one', origin: 'http://tifa.test', pathname: '/teams/x/one', search: '', hash: '' }
    // The browser history a router without a memory history takes.
    g.history = { scrollRestoration: 'auto', state: null, length: 1, pushState() {}, replaceState() {}, go() {}, back() {}, forward() {} }
    fakeDocument.getElementById = (id: string) => byId[id] ?? null
    // The page's routes have Loads whose data request never answers, so the page stops before
    // React renders into the fake root.
    let requested = false
    g.fetch = () => {
      requested = true
      return new Promise(() => {})
    }
    try {
      hydrate(routeTree, { lang: 'xx', className: 'dark', htmlFor: 'x', onClick: 'alert(1)' })
      for (let i = 0; i < 100 && !requested; i++) await new Promise((resolve) => setTimeout(resolve, 0))
      assert(requested, 'the page asked for no data')
    } finally {
      fakeDocument.getElementById = saved.getElementById
      g.fetch = saved.fetch
      g.history = saved.history
      g.location = saved.location
    }
    const html = describe(fakeDocument.documentElement)
    assert(html === 'html lang=xx class=dark for=x', `html ${html}`)
  })
})

test('the attributes of <html> follow the matched routes', async () => {
  await withDocument(async () => {
    const html = fakeDocument.documentElement
    // An attribute the page sets itself, such as a theme, is not tifa's to take away.
    html.setAttribute('data-color', 'dark')
    // The Layout's data is the language in the URL, as its Go function reads it.
    const { router } = await client('/intl/fr/rtl', (url, id) => ok(id === 'app/intl/lang+/layout' ? url.split('/')[2] : id))
    const base = { lang: 'xx', 'data-theme': 't' }
    followHead(router, base)
    syncHead(router.stores.matches.get())
    syncHTMLAttributes(router.stores.matches.get(), base)
    assert(describe(html) === 'html data-color=dark lang=fr dir=rtl class=wide title=a"<b', `html ${describe(html)}`)
    const changes = () => describe(html)
    await router.navigate({ to: '/intl/$lang', params: { lang: 'de' } })
    for (let i = 0; i < 100 && !changes().includes('lang=de'); i++) await new Promise((resolve) => setTimeout(resolve, 0))
    // The page's title goes, its class gives way to the Layout's, the root Layout's data-theme comes back.
    assert(describe(html) === 'html data-color=dark lang=de dir=ltr class=outer data-theme=t', `after navigating ${describe(html)}`)
    await router.navigate({ to: '/hello' })
    for (let i = 0; i < 100 && changes().includes('dir='); i++) await new Promise((resolve) => setTimeout(resolve, 0))
    assert(describe(html) === 'html data-color=dark lang=xx data-theme=t', `after leaving ${describe(html)}`)
  })
})

// takeOverPage runs the scripts of a page the server rendered for url, which router.test.tsx
// writes into file, with <html> holding the attributes the server wrote, and hydrates a browser
// router from it as the browser entry does. before runs before the hydration, as an inline
// script of the page does.
async function takeOverPage(file: string, url: string, before = () => {}) {
  const html = readFileSync(file, 'utf8')
  const start = html.slice(0, html.indexOf('<head>'))
  for (const [, name, value] of start.matchAll(/ ([^\s=]+)="([^"]*)"/g))
    fakeDocument.documentElement.setAttribute(name!, value!.replace(/&quot;/g, '"').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&'))
  const server = describe(fakeDocument.documentElement)
  before()
  // The page's scripts leave the dehydrated router where TanStack's hydrate finds it.
  const saved = { document: g.document, location: g.location }
  g.location = { href: 'http://tifa.test' + url, origin: 'http://tifa.test', pathname: url, search: '', hash: '' }
  try {
    for (const [, code] of html.matchAll(/<script[^>]*>([\s\S]*?)<\/script>/g)) {
      if (code!.trim() === '') continue
      g.document = { ...fakeDocument, currentScript: { previousElementSibling: null, remove() {} } }
      new Function(code!)()
    }
  } finally {
    Object.assign(g, saved)
  }
  const router = createAppRouter(routeTree, async (_, id) => ok(id), { history: createMemoryHistory({ initialEntries: [url] }) })
  await takeOver(router, htmlAttributes)
  return { router, server }
}

// navigateTo navigates router to `to` and waits until the attributes of <html> changed, or a while.
async function navigateTo(router: any, options: object) {
  const before = describe(fakeDocument.documentElement)
  await router.navigate(options)
  for (let i = 0; i < 100 && describe(fakeDocument.documentElement) === before; i++) await new Promise((resolve) => setTimeout(resolve, 0))
}

test('a hydrated page takes away the attributes of <html> the server wrote for it', async () => {
  await withDocument(async () => {
    const { router, server } = await takeOverPage('intl-fr-rtl.html', '/intl/fr/rtl')
    assert(server === 'html lang=fr class=wide dir=rtl title=a"<b', `server html ${server}`)
    assert(describe(fakeDocument.documentElement) === server, `hydration changed <html>: ${describe(fakeDocument.documentElement)}`)
    await navigateTo(router, { to: '/hello' })
    assert(describe(fakeDocument.documentElement) === 'html lang=xx class=h-full data-theme=a"b', `html after navigating ${describe(fakeDocument.documentElement)}`)
  })
})

test('what scripts of the page set on <html> stays until a route changes that attribute', async () => {
  await withDocument(async () => {
    const html = fakeDocument.documentElement
    // A dark mode script adds a class before the hydration, a translation of the browser sets lang after it.
    const { router, server } = await takeOverPage('hello.html', '/hello', () => html.setAttribute('class', 'h-full dark'))
    assert(server === 'html lang=xx data-theme=a"b class=h-full', `server html ${server}`)
    assert(describe(html) === 'html lang=xx data-theme=a"b class=h-full dark', `after hydrating ${describe(html)}`)
    html.setAttribute('lang', 'de')
    html.setAttribute('class', 'h-full dark contrast')
    // No route of /a/one sets an attribute of <html>: the root Layout's are the same as before.
    await router.navigate({ to: '/a/one' })
    await new Promise((resolve) => setTimeout(resolve, 20))
    assert(router.state.location.pathname === '/a/one', `at ${router.state.location.pathname}`)
    assert(describe(html) === 'html lang=de data-theme=a"b class=h-full dark contrast', `after navigating ${describe(html)}`)
    // The routes of /intl/fr set lang and class: theirs win, and data-theme, which they leave, stays.
    await navigateTo(router, { to: '/intl/$lang', params: { lang: 'fr' } })
    assert(describe(html) === 'html lang=app/intl/lang+/layout data-theme=a"b class=outer dir=ltr', `on /intl/fr ${describe(html)}`)
    await navigateTo(router, { to: '/hello' })
    assert(describe(html) === 'html lang=xx data-theme=a"b class=h-full', `back on /hello ${describe(html)}`)
  })
})

// The server writes 0 as the time each match loaded at, and the hydrated router keeps it, so React
// hydrates what the server rendered. Just before the router first loads or preloads, it dates them
// by the age of the data Go sends in Server-Timing, counted back from when the browser sent the
// request, and leaves them at 0, data of unknown age, when it cannot read the age or the page came
// from the history.
test('a hydrated page dates its matches by the age of its data', async () => {
  const real = performance.getEntriesByType
  const navigation = (entry: object) => {
    ;(performance as any).getEntriesByType = (type: string) => (type === 'navigation' ? [entry] : [])
  }
  const dates = async () => {
    const { router } = await takeOverPage('hello.html', '/hello')
    const matches = router.stores.matches.get()
    const hydrated = matches.map((match: { updatedAt: number }) => match.updatedAt)
    assert(hydrated.length > 1 && hydrated.every((at: number) => at === 0), `hydrated with dates ${hydrated}`)
    // The first load or preload dates them, here a preload of another page.
    await router.preloadRoute({ to: '/a/one' })
    return matches.map((match: { updatedAt: number }) => match.updatedAt)
  }
  const age = { name: 'tifa-age', duration: 30, description: '' }
  try {
    await withDocument(async () => {
      navigation({ type: 'navigate', requestStart: 120, serverTiming: [{ name: 'tifa-dev', duration: 0, description: 'v1' }, age] })
      const dated = await dates()
      const want = performance.timeOrigin + 120 - 30
      assert(dated.length > 1 && dated.every((at) => at === want), `dates ${dated}, want ${want}`)
      for (const entry of [
        { type: 'navigate', requestStart: 120 },
        { type: 'navigate', requestStart: 120, serverTiming: [] },
        { type: 'back_forward', requestStart: 120, serverTiming: [age] },
      ]) {
        navigation(entry)
        const undated = await dates()
        assert(undated.length > 1 && undated.every((at) => at === 0), `dates ${undated} for ${JSON.stringify(entry)}`)
      }
    })
  } finally {
    performance.getEntriesByType = real
  }
})

test('the head follows data the router refetches in the background', async () => {
  await withDocument(async () => {
    let team = 'x'
    const { router } = await client('/teams/x/two', (_, id) => ok(id === 'app/teams/team+/layout' ? team : id))
    followHead(router, { lang: "xx" })
    syncHead(router.stores.matches.get())
    const title = () => managed().find((e) => e.localName === 'title')?.textContent
    assert(title() === 'TEAM x', `title ${title()}`)
    team = 'y'
    await router.invalidate()
    for (let i = 0; i < 100 && title() !== 'TEAM y'; i++) await new Promise((resolve) => setTimeout(resolve, 0))
    assert(title() === 'TEAM y', `title after invalidate ${title()}`)
  })
})

// asked reads a data request the way the server does: the page URL, and the Loads of its chain
// that the digits of _tifa ask for. The chains are those of the pages of the test app.
const chains: Record<string, string[]> = {
  '/a/one': ['app/layout', 'app/a/layout'],
  '/a/two': ['app/layout', 'app/a/layout', 'app/a/two/route'],
}
function asked(url: string) {
  const parsed = new URL(url, 'http://tifa.test')
  const value = parsed.searchParams.get('_tifa') ?? ''
  parsed.searchParams.delete('_tifa')
  const digits = value.split('.')[1] ?? ''
  const chain = chains[parsed.pathname]
  assert(chain !== undefined && digits.length === chain.length, `data request ${url}`)
  return { url: parsed.pathname + parsed.search, routes: chain.filter((_, i) => digits[i] === '1') }
}

// The router of a browser build loads in the background, which the Node build of
// router.test.tsx does not.
test('a write sends its own request and reloads nothing until the app invalidates', async () => {
  const saved = { fetch: g.fetch }
  const fetched: string[] = []
  g.fetch = async (url: string, init: RequestInit) => {
    fetched.push(init.method === 'POST' ? 'POST ' + url : url)
    if (init.method === 'POST') return Response.json({ result: { text: 'saved', next: null } })
    const { routes } = asked(url)
    return Response.json({ routes: Object.fromEntries(routes.map((id) => [id, ok(`${id} ${fetched.length}`)])) })
  }
  try {
    const router = createAppRouter(routeTree, collector(() => 'b1'), { history: createMemoryHistory({ initialEntries: ['/a/two'] }) })
    await router.load()
    setRouter(router, () => 'b1')
    fetched.splice(0)
    const shown = () => router.state.matches.map((m) => m.loaderData).filter((d) => d !== undefined).join(', ')
    const before = shown()
    await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
    await new Promise((resolve) => setTimeout(resolve, 20))
    assert(fetched.join(' | ') === 'POST /api/notes/1', `requests ${fetched.join(' | ')}`)
    assert(shown() === before, `the write changed the page: ${before} -> ${shown()}`)
    // The app's own invalidate loads the page's Loads again, merged into one request.
    await router.invalidate({ sync: true })
    assert(fetched.length === 2 && fetched[1].startsWith('/a/two?_tifa=b1.'), `requests ${fetched.join(' | ')}`)
    assert(shown() !== before, `invalidate kept the old data: ${shown()}`)
  } finally {
    Object.assign(g, saved)
  }
})

// A redirect of a call is a navigation. The data of the routes already loaded follows the
// router's defaults: shown at once, loaded again in the background.
test('a redirect of a handler call navigates, and a revisit shows the cached data while it loads again in the background', async () => {
  const saved = { fetch: g.fetch, location: g.location }
  const fetched: string[] = []
  let target = '/a/two'
  let answers = 0
  g.fetch = async (url: string, init: RequestInit) => {
    const headers = new Headers(init.headers)
    if (headers.get('X-Tifa-Call')) return Response.json({ redirect: target, status: 303 }, { headers: { 'X-Tifa-Redirect': '303' } })
    const { url: page, routes } = asked(url)
    routes.sort()
    fetched.push(page + ' ' + routes.join(','))
    answers++
    return Response.json({ routes: Object.fromEntries(routes.map((id) => [id, ok(`${id} ${answers}`)])) })
  }
  g.location = { ...saved.location, assign: (url: string) => fetched.push('assign ' + url) }
  try {
    const router = createAppRouter(routeTree, collector(() => 'b1'), { history: createMemoryHistory({ initialEntries: ['/a/one'] }) })
    await router.load()
    setRouter(router, () => 'b1')
    fetched.splice(0)
    const shown = () =>
      router.state.location.pathname + ' ' + router.state.matches.map((m) => m.loaderData).filter((d) => d !== undefined).join(', ')
    // The call marks nothing stale. The Layouts the target shares with the page stay as they are,
    // and only the Load that is new asks the server.
    await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
    await new Promise((resolve) => setTimeout(resolve, 20))
    assert(fetched.join(' | ') === '/a/two app/a/two/route', `requests ${fetched.join(' | ')}`)
    assert(shown() === '/a/two app/layout 1, app/a/layout 1, app/a/two/route 2', `shown ${shown()}`)
    // Leaving for a page whose Loads are all loaded asks for nothing.
    target = '/a/one'
    fetched.splice(0)
    await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
    await new Promise((resolve) => setTimeout(resolve, 20))
    assert(fetched.length === 0 && shown() === '/a/one app/layout 1, app/a/layout 1', `leaving: ${fetched.join(' | ')} ${shown()}`)
    // Back to a page visited before: the router shows its data at once and loads it again in the
    // background (stale-while-revalidate).
    target = '/a/two'
    const visit = POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
    await visit
    assert(shown().startsWith('/a/two ') && shown().includes('app/a/two/route 2'), `revisit shown at once ${shown()}`)
    await new Promise((resolve) => setTimeout(resolve, 20))
    assert(fetched.join(' | ') === '/a/two app/a/two/route', `revisit requests ${fetched.join(' | ')}`)
    assert(shown() === '/a/two app/layout 1, app/a/layout 1, app/a/two/route 3', `revisit shown after ${shown()}`)
  } finally {
    Object.assign(g, saved)
  }
})

// A revalidation in the background that is still on its way when the user moves on must not
// leave the page with the data of the page behind, lose the answer of the next navigation, or
// make a request nobody waits for fail loudly.
test('background revalidations still out when the user moves on and back settle cleanly', async () => {
  const saved = { fetch: g.fetch }
  const pending: Array<{ page: string; routes: string[]; answer: () => void }> = []
  let seq = 0
  g.fetch = (url: string) => {
    const { url: page, routes } = asked(url)
    const n = ++seq
    return new Promise<Response>((resolve) => {
      pending.push({ page, routes, answer: () => resolve(Response.json({ routes: Object.fromEntries(routes.map((id) => [id, ok(`${id} ${n}`)])) })) })
    })
  }
  const tick = () => new Promise((resolve) => setTimeout(resolve, 10))
  try {
    const router = createAppRouter(routeTree, collector(() => 'b1'), { history: createMemoryHistory({ initialEntries: ['/a/one'] }) })
    const first = router.load()
    await tick()
    pending.shift()!.answer()
    await first
    const shown = () =>
      router.state.location.pathname + ' ' + router.state.matches.map((m) => m.loaderData).filter((d) => d !== undefined).join(', ')
    assert(shown() === '/a/one app/layout 1, app/a/layout 1', `first ${shown()}`)
    // Visit /a/two once, so that its data is cached, and leave it.
    const toTwo = router.navigate({ to: '/a/two' })
    await tick()
    assert(pending.length === 1 && pending[0].routes.join() === 'app/a/two/route', `first visit asked ${pending.map((p) => p.routes)}`)
    pending.shift()!.answer()
    await toTwo
    await router.navigate({ to: '/a/one' })
    // The revisit shows the cached data and asks again in the background. The user leaves before the
    // answer arrives and comes back, so the page asks a second time while the first is still out.
    await router.navigate({ to: '/a/two' })
    await tick()
    assert(shown() === '/a/two app/layout 1, app/a/layout 1, app/a/two/route 2', `revisit ${shown()}`)
    assert(pending.length === 1, `background revalidation did not ask: ${pending.length}`)
    await router.navigate({ to: '/a/one' })
    await router.navigate({ to: '/a/two' })
    await tick()
    // Answers arrive in the reverse order they were asked in.
    for (const request of pending.splice(0).reverse()) request.answer()
    await tick()
    for (let i = 0; i < 20 && pending.length > 0; i++) {
      pending.splice(0).forEach((request) => request.answer())
      await tick()
    }
    await tick()
    assert(router.state.location.pathname === '/a/two', `ended at ${router.state.location.pathname}`)
    assert(router.state.status === 'idle' && router.state.matches.every((m) => m.status === 'success'), `state ${router.state.status} ${router.state.matches.map((m) => m.status)}`)
    assert(router.state.matches.map((m) => m.loaderData).filter((d) => d !== undefined).length === 3, `data ${shown()}`)
  } finally {
    Object.assign(g, saved)
  }
})

test('a redirect to the page that shows, spelled another way, stays where it is and loads nothing', async () => {
  const saved = { fetch: g.fetch, location: g.location }
  const fetched: string[] = []
  g.fetch = async (url: string, init: RequestInit) => {
    const headers = new Headers(init.headers)
    // The server writes the query of the page in another order and encoding than the router.
    if (headers.get('X-Tifa-Call')) return Response.json({ redirect: '/a/two?a=1&b=x%20y', status: 303 }, { headers: { 'X-Tifa-Redirect': '303' } })
    const { url: page, routes } = asked(url)
    routes.sort()
    fetched.push(page + ' ' + routes.join(','))
    return Response.json({ routes: Object.fromEntries(routes.map((id) => [id, ok(`${id} ${fetched.length}`)])) })
  }
  g.location = { ...saved.location, assign: (url: string) => fetched.push('assign ' + url) }
  try {
    const history = createMemoryHistory({ initialEntries: ['/a/two?b=x+y&a=1'] })
    const router = createAppRouter(routeTree, collector(() => 'b1'), { history })
    await router.load()
    setRouter(router, () => 'b1')
    fetched.splice(0)
    const href = router.state.location.href
    const entries = history.length
    const shown = () => router.state.matches.map((m) => m.loaderData).filter((d) => d !== undefined).join(', ')
    const before = shown()
    await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
    await new Promise((resolve) => setTimeout(resolve, 20))
    assert(fetched.length === 0, `requests ${fetched.join(' | ')}`)
    assert(shown() === before, `shown ${shown()}`)
    assert(router.state.location.href === href && history.length === entries, `at ${router.state.location.href}, ${history.length} entries`)
  } finally {
    Object.assign(g, saved)
  }
})

// TanStack Router reloads the page by itself, once for each message, when the module of a lazy
// route is missing. That reload is the page's, so a page that has reloaded for its build must
// not reload a second time because of it: the failure goes to the Error of the route.
test('a route chunk that still fails after the reload shows its error and the router does not reload again', async () => {
  const saved = { location: g.location, sessionStorage: g.sessionStorage, addEventListener: g.addEventListener }
  const items = new Map<string, string>()
  let reloads = 0
  let listener: (event: any) => void = () => {}
  g.location = { reload: () => reloads++ }
  g.sessionStorage = { getItem: (key: string) => items.get(key) ?? null, setItem: (key: string, value: string) => void items.set(key, value) }
  g.addEventListener = (type: string, fn: (event: any) => void) => void (type === 'vite:preloadError' && (listener = fn))
  resetReloads()
  try {
    watchChunks(() => 'b1')
    const failure = () => new TypeError('Failed to fetch dynamically imported module: http://tifa.test/assets/chunk-1.js')
    const event = () => ({ payload: failure(), defaultPrevented: false, preventDefault() { this.defaultPrevented = true } })
    // What the router does with a failed module on its own: it reloads, then suspends for good.
    const alone = failure()
    const Alone: any = lazyRouteComponent(() => Promise.reject(alone))
    await Alone.preload()
    let thrown: unknown
    try {
      Alone({})
    } catch (error) {
      thrown = error
    }
    assert(thrown instanceof Promise && reloads === 1, `the router alone: thrown ${thrown}, reloads ${reloads}`)
    items.clear()
    reloads = 0
    // The first failure of the page reloads it. The reloaded page, with a storage that knows it,
    // gets the failure as an error.
    const first = event()
    listener(first)
    assert(first.defaultPrevented && reloads === 1, `first: prevented ${first.defaultPrevented}, reloads ${reloads}`)
    resetReloads()
    const second = event()
    listener(second)
    assert(!second.defaultPrevented && reloads === 1, `second: prevented ${second.defaultPrevented}, reloads ${reloads}`)
    const Lazy: any = lazyRouteComponent(() => Promise.reject(second.payload))
    await Lazy.preload()
    thrown = undefined
    try {
      Lazy({})
    } catch (error) {
      thrown = error
    }
    assert(thrown === second.payload && reloads === 1, `the router after the page reloaded: thrown ${thrown}, reloads ${reloads}`)
  } finally {
    Object.assign(g, saved)
  }
})

// A hydration mismatch needs a real DOM, which this harness does not have, so the callbacks
// React calls are called by hand with what React hands them.
test('a dev build tells the overlay about the errors React reports', async () => {
  const saved = { dispatchEvent: g.dispatchEvent, error: console.error }
  const seen: any[] = []
  const logged: unknown[][] = []
  g.dispatchEvent = (event: any) => seen.push(event)
  console.error = (...args: unknown[]) => logged.push(args)
  try {
    const onRecoverableError = (rootOptions as any).onRecoverableError
    const onUncaughtError = (rootOptions as any).onUncaughtError
    assert(typeof onRecoverableError === 'function' && typeof onUncaughtError === 'function', 'a dev build sets no callbacks')
    const mismatch = new Error("Hydration failed because the server rendered HTML didn't match the client.")
    onRecoverableError(mismatch, { componentStack: '\n    at Clock' })
    onUncaughtError(new Error('boom'), {})
    onUncaughtError('plain', {})
    assert(seen.length === 3 && seen.every((e) => e.type === browserErrorEvent), `events ${seen.map((e) => e.type)}`)
    assert(browserErrorEvent === 'tifa:browser-error', browserErrorEvent)
    const [a, b, c] = seen.map((e) => e.detail)
    assert(a.kind === 'recoverable' && a.message === mismatch.message && a.stack.includes('Hydration failed') && a.componentStack === '\n    at Clock', JSON.stringify(a))
    assert(b.kind === 'uncaught' && b.message === 'boom' && b.componentStack === '', JSON.stringify(b))
    assert(c.kind === 'uncaught' && c.message === 'plain' && c.stack === '', JSON.stringify(c))
    assert(logged.length === 3, `console.error called ${logged.length} times`)
  } finally {
    Object.assign(g, { dispatchEvent: saved.dispatchEvent })
    console.error = saved.error
  }
})

let failed = 0
for (const [name, fn] of tests) {
  try {
    await fn()
    console.log(`ok   ${name}`)
  } catch (error) {
    failed++
    console.log(`FAIL ${name}\n${error instanceof Error ? error.stack : error}`)
  }
}
if (failed > 0) throw new Error(`${failed} of ${tests.length} tests failed`)
