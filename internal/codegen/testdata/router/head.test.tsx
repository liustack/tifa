// Run by TestGeneratedRouterBehaves in Node, bundled for the browser: the routers here run as
// they do in the browser, with the reactive stores the head code subscribes to, against a
// document that has just what that code uses.
import { createMemoryHistory } from '@tanstack/react-router'
import { routeTree } from './.tifa/routes'
import { createAppRouter } from './.tifa/runtime/router'
import { type RouteResult } from './.tifa/runtime/data'
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
    if (node.parentNode) node.parentNode.childNodes.splice(node.parentNode.childNodes.indexOf(node), 1)
    else fakeDocument.inserted.push(node)
    const at = before ? this.childNodes.indexOf(before) : this.childNodes.length
    if (at < 0) throw new Error('insertBefore: not a child')
    this.childNodes.splice(at, 0, node)
    node.parentNode = this
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
  constructor(readonly localName: string) {
    super(1)
  }
  setAttribute(name: string, value: string) {
    if (!/^[^\s"'>\/=]+$/.test(name)) throw new Error(`InvalidCharacterError: ${name}`)
    name = name.toLowerCase()
    const existing = this.attributes.find((a) => a.name === name)
    if (existing) existing.value = value
    else this.attributes.push({ name, value })
  }
  getAttribute(name: string) {
    return this.attributes.find((a) => a.name === name)?.value ?? null
  }
}

const fakeDocument = {
  ...browserDocument,
  inserted: [] as FakeNode[],
  head: new FakeElement('head'),
  documentElement: new FakeElement('html'),
  createElement: (name: string) => new FakeElement(name),
  createComment: (value: string) => new FakeNode(8, value),
}

// withDocument runs fn against a fresh fake document.
async function withDocument(fn: () => Promise<void>) {
  const saved = g.document
  fakeDocument.inserted = []
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

test('the head follows data the router refetches in the background', async () => {
  await withDocument(async () => {
    let team = 'x'
    const { router } = await client('/teams/x/two', (_, id) => ok(id === 'app/team' ? team : id))
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
