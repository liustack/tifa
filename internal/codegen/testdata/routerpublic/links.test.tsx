// Run by TestGeneratedRouterLeavesPublicFiles in Node, against the route tree generated for this
// app: which links inside <HTML> the router follows, by routerHref. Node has no DOM, so the links
// are small stand-ins with the few methods routerHref calls, and the test sets the location and
// the base URL of the document.
import { createMemoryHistory } from '@tanstack/react-router'
import { routeTree } from './.tifa/routes'
import { createAppRouter } from './.tifa/runtime/router'
import { routerHref, type Click } from './.tifa/runtime/html'
import type { RouteResult } from './.tifa/runtime/data'

const tests: Array<[string, () => Promise<void>]> = []
const test = (name: string, fn: () => Promise<void>) => tests.push([name, fn])

function assert(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message)
}

// Node, an element of the markup: a tag, attributes and the element it sits in.
class Node {
  constructor(
    readonly tag: string,
    readonly attributes: Record<string, string> = {},
    readonly parent: Node | null = null,
  ) {}
  getAttribute(name: string) {
    return Object.hasOwn(this.attributes, name) ? this.attributes[name] : null
  }
  hasAttribute(name: string) {
    return Object.hasOwn(this.attributes, name)
  }
  // closest knows the one selector routerHref asks for.
  closest(selector: string): Node | null {
    assert(selector === 'a[href], area[href]', `closest(${selector})`)
    for (let node: Node | null = this; node !== null; node = node.parent) if ((node.tag === 'a' || node.tag === 'area') && node.hasAttribute('href')) return node
    return null
  }
  contains(other: Node) {
    for (let node: Node | null = other; node !== null; node = node.parent) if (node === this) return true
    return false
  }
}
;(globalThis as { Element?: unknown }).Element = Node

const origin = 'http://shop.test'

// at places the document at url, with base as its base URL (the URL itself without a <base>).
function at(url: string, base = url) {
  ;(globalThis as { location?: unknown }).location = new URL(origin + url)
  ;(globalThis as { document?: unknown }).document = { baseURI: base.startsWith('http') ? base : origin + base }
}

const router = createAppRouter(
  routeTree,
  async (): Promise<RouteResult> => ({ status: 200, data: null }),
  { history: createMemoryHistory({ initialEntries: ['/en'] }) },
)
await router.load()

const plain: Click = { button: 0, defaultPrevented: false, ctrlKey: false, metaKey: false, shiftKey: false, altKey: false }

// href asks routerHref about a click on, or with no click a hover of, the text inside a link with
// attributes in an <HTML> at /en?x=1.
function href(attributes: Record<string, string>, click: Click | undefined = plain, tag = 'a') {
  const container = new Node('article')
  const text = new Node('em', {}, new Node(tag, attributes, new Node('p', {}, container)))
  return routerHref(router, container as unknown as Element, text as unknown as EventTarget, click)
}

function check(cases: Array<[string, string | null, string | null]>, ask: (link: string) => string | null) {
  for (const [name, link, want] of cases) {
    const got = ask(link!)
    assert(got === want, `${name} (${link}): ${JSON.stringify(got)}, want ${JSON.stringify(want)}`)
  }
}

test('a plain left click on a link to a page goes through the router, with its query and hash', async () => {
  at('/en?x=1')
  check(
    [
      ['a page', '/about', '/about'],
      ['a page with a query and a hash', '/about?a=b#c', '/about?a=b#c'],
      ['a page of a parameter folder', '/fr', '/fr'],
      ['an absolute URL of the site', origin + '/about', '/about'],
      ['a URL relative to the page', 'about', '/about'],
    ],
    (link) => href({ href: link }),
  )
  assert(href({ href: '/about' }, plain, 'area') === '/about', 'an area is no link')
  // A hover or a focus asks without a click.
  assert(href({ href: '/about' }, undefined) === '/about', 'a hover of a link to a page')
})

test('1. a click that is handled, not the left button or with a modifier goes to the browser', async () => {
  at('/en?x=1')
  for (const click of [{ defaultPrevented: true }, { button: 1 }, { button: 2 }, { ctrlKey: true }, { metaKey: true }, { shiftKey: true }, { altKey: true }])
    assert(href({ href: '/about' }, { ...plain, ...click }) === null, `the router takes a click with ${JSON.stringify(click)}`)
})

test('2. no link with an href inside the container goes to the browser', async () => {
  at('/en?x=1')
  const container = new Node('article')
  const loose = new Node('em', {}, new Node('p', {}, container))
  assert(routerHref(router, container as unknown as Element, loose as unknown as EventTarget, plain) === null, 'text outside a link')
  assert(href({ name: 'anchor' }) === null, 'a link without href')
  // The <HTML> sits inside a link of the app: that link is not the markup's.
  const outer = new Node('a', { href: '/about' })
  const inner = new Node('span', {}, outer)
  const text = new Node('em', {}, inner)
  assert(routerHref(router, inner as unknown as Element, text as unknown as EventTarget, plain) === null, 'a link around the container')
  assert(routerHref(router, inner as unknown as Element, null, plain) === null, 'no target')
})

test('3. a target other than _self, download or rel="external" goes to the browser', async () => {
  at('/en?x=1')
  check(
    [
      ['target _blank', '_blank', null],
      ['target of a frame', 'side', null],
      ['target _self', '_self', '/about'],
      ['an empty target', '', '/about'],
    ],
    (target) => href({ href: '/about', target }),
  )
  assert(href({ href: '/about', download: '' }) === null, 'download')
  assert(href({ href: '/about', rel: 'nofollow  external' }) === null, 'rel external')
  assert(href({ href: '/about', rel: 'noopener externally' }) === '/about', 'rel without external')
})

test('4. another origin or scheme, from the base URL of the document, goes to the browser', async () => {
  at('/en?x=1')
  check(
    [
      ['another site', 'https://other.test/about', null],
      ['another site without a scheme', '//other.test/about', null],
      ['another port', 'http://shop.test:8080/about', null],
      ['https of the same host', 'https://shop.test/about', null],
      ['mail', 'mailto:a@shop.test', null],
      ['a phone', 'tel:+1', null],
      ['script', 'javascript:void(0)', null],
      ['not a URL', 'http://[', null],
    ],
    (link) => href({ href: link }),
  )
  // A <base> of another site moves every relative link there.
  at('/en?x=1', 'https://other.test/')
  assert(href({ href: '/about' }) === null, 'a link relative to a base of another site')
  // A relative link resolves against the base, not the page.
  at('/en/deep/page', '/')
  assert(href({ href: 'about' }) === '/about', 'a link relative to the base')
  at('/about', '/en/')
  assert(href({ href: 'missing' }) === null, 'a link relative to the base that is no page')
})

test('5. a hash on the page that shows goes to the browser', async () => {
  at('/en?x=1')
  check(
    [
      ['a hash', '#top', null],
      ['a hash with the query', '?x=1#top', null],
      ['a hash with the path and query', '/en?x=1#top', null],
      ['another query with a hash', '/en#top', '/en#top'],
      ['another query', '/en?x=2', '/en?x=2'],
      ['another page with a hash', '/about#top', '/about#top'],
      // The browser would load the page again, the router shows it again.
      ['the page itself', '/en?x=1', '/en?x=1'],
    ],
    (link) => href({ href: link }),
  )
})

test('6. a file Go serves goes to the browser', async () => {
  at('/en?x=1')
  check(
    [
      ['a public file a page also matches', '/favicon.svg', null],
      ['a public file in a folder', '/img/logo.png', null],
      ['a public file with a query', '/favicon.svg?v=2', null],
      ['a file of the build', '/assets/app.js', null],
      ['a page below /assets/', '/assets/report', null],
      ['what tifa serves', '/__tifa/session', null],
    ],
    (link) => href({ href: link }),
  )
})

test('7. a path without a page goes to the browser', async () => {
  at('/en?x=1')
  check(
    [
      ['an API route', '/api/ping', null],
      ['a URL below a page', '/en/missing', null],
      ['the root, which has no page', '/', null],
    ],
    (link) => href({ href: link }),
  )
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
