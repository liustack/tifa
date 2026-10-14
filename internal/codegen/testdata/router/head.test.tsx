// Run by TestGeneratedRouterBehaves in Node, bundled for the browser: the routers here run as
// they do in the browser, with the reactive stores the head code subscribes to, against a
// document that has just what that code uses.
import { createMemoryHistory } from '@tanstack/react-router'
import { routeTree } from './.tifa/routes'
import { createAppRouter } from './.tifa/runtime/router'
import { bootElementId, rootElementId, type RouteResult } from './.tifa/runtime/data'
import { hydrate } from './.tifa/runtime/browser'
import { followHead, headHTML, setAttributes, syncHead, type Tag } from './.tifa/runtime/head'

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

test('the head follows data the router refetches in the background', async () => {
  await withDocument(async () => {
    let team = 'x'
    const { router } = await client('/teams/x/two', (_, id) => ok(id === 'app/teams/team+/layout' ? team : id))
    followHead(router)
    syncHead(router.stores.matches.get())
    const title = () => managed().find((e) => e.localName === 'title')?.textContent
    assert(title() === 'TEAM x', `title ${title()}`)
    team = 'y'
    await router.invalidate()
    for (let i = 0; i < 100 && title() !== 'TEAM y'; i++) await new Promise((resolve) => setTimeout(resolve, 0))
    assert(title() === 'TEAM y', `title after invalidate ${title()}`)
  })
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
