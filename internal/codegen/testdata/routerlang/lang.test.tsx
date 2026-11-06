// Run by TestGeneratedRouterPrefixesLanguages in Node, against the route tree generated for this
// app: a store in zh-CN and en-US next to a page outside every area. The URLs a router builds, the
// requests it makes, which navigations leave the document, and one VM rendering the two languages
// one after another.
import { createMemoryHistory, isRedirect } from '@tanstack/react-router'
import { routeTree, htmlAttributes } from './.tifa/routes'
import { i18n } from './.tifa/i18n.gen'
import { createAppRouter, load, type RouterContext } from './.tifa/runtime/router'
import { makeRewrite, reaches } from './.tifa/runtime/prefix'
import { routerHref, type Click } from './.tifa/runtime/html'
import { serve } from './.tifa/runtime/server'
import { setRouter } from './.tifa/runtime/call'
import { POST } from './app/store/mine/route.gen'
import type { RouteResult } from './.tifa/runtime/data'

const tests: Array<[string, () => Promise<void>]> = []
const test = (name: string, fn: () => Promise<void>) => tests.push([name, fn])

function assert(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message)
}
if (!i18n) throw new Error('the app has no languages')
const languages = i18n

serve(routeTree, htmlAttributes)

// client loads url on a browser-side router for language, whose source records each call. The
// window records the documents the browser is sent to.
async function client(url: string, language: string, always = false) {
  const calls: string[] = []
  const assigned: string[] = []
  ;(globalThis as any).window = {
    location: { href: 'http://shop.test' + url, origin: 'http://shop.test', pathname: url.split('?')[0], assign: (u: string) => assigned.push(u) },
  }
  const router = createAppRouter(
    routeTree,
    async (u, id, slot): Promise<RouteResult> => {
      calls.push(`${id} ${u} [${slot.chain().join(',')}]${slot.preload ? ' preload' : ''}`)
      return { status: 200, data: id }
    },
    { history: createMemoryHistory({ initialEntries: [url] }), rewrite: makeRewrite({ ...languages, always }, language) },
  )
  await router.load()
  calls.length = 0
  return { router, calls, assigned }
}

const href = (router: any, to: string, search?: object) => router.buildLocation({ to, search }).publicHref

test('a router builds URLs with its own prefix, only for routes that have the language', async () => {
  const en = (await client('/en-US/store', 'en-US')).router
  assert(href(en, '/store/cart') === '/en-US/store/cart', href(en, '/store/cart'))
  assert(href(en, '/store') === '/en-US/store', href(en, '/store'))
  assert(href(en, '/projects') === '/projects', href(en, '/projects'))
  assert(href(en, '/store/cart', { q: 'a b' }) === '/en-US/store/cart?q=a+b', href(en, '/store/cart', { q: 'a b' }))
  const zh = (await client('/store', 'zh-CN')).router
  assert(href(zh, '/store/cart') === '/store/cart', href(zh, '/store/cart'))
  assert(href(zh, '/projects') === '/projects', href(zh, '/projects'))
  const always = (await client('/zh-CN/store', 'zh-CN', true)).router
  assert(href(always, '/store/cart') === '/zh-CN/store/cart', href(always, '/store/cart'))
  assert(href(always, '/projects') === '/projects', href(always, '/projects'))
  assert(always.state.location.pathname === '/store', always.state.location.pathname)
})

test('the data request sends the public URL, and the chain is the one of the plain path', async () => {
  const { router, calls } = await client('/en-US/store', 'en-US')
  router.history.push('/en-US/store/cart?x=1')
  await router.load()
  assert(calls.length > 0 && calls.every((c) => c.includes(' /en-US/store/cart?x=1 ')), JSON.stringify(calls))
  assert(calls.some((c) => c.includes('app/store/cart/route')), JSON.stringify(calls))
  assert(router.state.location.href === '/en-US/store/cart?x=1' || router.state.location.publicHref === '/en-US/store/cart?x=1', router.state.location.publicHref)
})

test('a route outside the area, or another language, leaves the document', async () => {
  const { router } = await client('/en-US/store', 'en-US')
  const beforeLoad = (routeTree.options as { beforeLoad: (a: unknown) => void }).beforeLoad
  const context = router.options.context
  const leaves = (pathname: string, publicHref: string) => {
    try {
      beforeLoad({ context, location: { pathname, href: pathname, publicHref } })
    } catch (error) {
      assert(isRedirect(error), String(error))
      const options = (error as unknown as { options: { href: string; reloadDocument: boolean } }).options
      assert(options.reloadDocument === true && options.href === publicHref, JSON.stringify(options))
      return true
    }
    return false
  }
  assert(leaves('/projects', '/projects'), '/projects stays in the document')
  assert(!leaves('/store/cart', '/en-US/store/cart'), '/store/cart leaves the document')
  assert(!leaves('/store/nope', '/en-US/store/nope'), 'a 404 inside the store leaves the document')
  const zh = await client('/projects', 'zh-CN')
  const zhContext = zh.router.options.context
  beforeLoad({ context: zhContext, location: { pathname: '/store/cart', href: '/store/cart', publicHref: '/store/cart' } })
  beforeLoad({ context: zhContext, location: { pathname: '/projects', href: '/projects', publicHref: '/projects' } })
  // A server render has nowhere to go.
  beforeLoad({ context: { ...context, browser: false }, location: { pathname: '/projects', href: '/projects', publicHref: '/projects' } })
})

test('a preload of a page outside the language asks for no data', async () => {
  const { router, calls } = await client('/en-US/store', 'en-US')
  await router.preloadRoute({ to: '/projects' as never })
  assert(calls.length === 0, JSON.stringify(calls))
  await router.preloadRoute({ to: '/store/cart' as never })
  assert(calls.length > 0 && calls.every((c) => c.includes(' /en-US/store/cart ') && c.endsWith(' preload')), JSON.stringify(calls))
})

test('a navigation to a page outside the area asks for a document load and no data', async () => {
  const { router, calls } = await client('/en-US/store', 'en-US')
  await router.navigate({ to: '/projects' as never }).catch(() => {})
  assert(!calls.some((c) => c.includes('app/projects')), JSON.stringify(calls))
})

test('a redirect of a Load to another language loads the document, to the same language navigates', async () => {
  const redirectTo = async (client_: Awaited<ReturnType<typeof client>>, location: string) => {
    const context: RouterContext = { ...client_.router.options.context, source: async () => ({ status: 303, location }) }
    const loading = load(context, 'app/store/route', { pathname: '/store', searchStr: '', publicHref: '/en-US/store' }, false)
    return Promise.race([loading.then(() => 'done', (error) => error), new Promise((resolve) => setTimeout(() => resolve('pending'), 20))])
  }
  const en = await client('/en-US/store', 'en-US')
  // A preload never leaves the page.
  const preloading = { ...en.router.options.context, source: async () => ({ status: 302, location: '/projects' }) } as RouterContext
  const dropped = await load(preloading, 'app/store/route', { pathname: '/store', searchStr: '', publicHref: '/en-US/store' }, true).then(() => undefined, (error) => error)
  assert(isRedirect(dropped) && en.assigned.length === 0, `preload: ${String(dropped)} ${en.assigned}`)
  assert((await redirectTo(en, '/store/cart')) === 'pending', 'an unprefixed URL is the default language')
  assert(en.assigned.join() === '/store/cart', en.assigned.join())
  en.assigned.length = 0
  const same = await redirectTo(en, '/en-US/store/cart')
  assert(isRedirect(same) && en.assigned.length === 0, `same language: ${String(same)} ${en.assigned}`)
  const zh = await client('/store', 'zh-CN')
  assert((await redirectTo(zh, '/en-US/store/cart')) === 'pending' && zh.assigned.join() === '/en-US/store/cart', zh.assigned.join())
  zh.assigned.length = 0
  assert(isRedirect(await redirectTo(zh, '/projects')) && zh.assigned.length === 0, 'projects from zh-CN')
})

test('navigate and preload with an href of another language load that document', async () => {
  const { router, calls, assigned } = await client('/en-US/store', 'en-US')
  for (const href of ['/store/cart', '/zh-CN/store/cart', '/projects']) {
    await router.navigate({ href } as never)
    await router.preloadRoute({ href } as never)
  }
  assert(assigned.join() === '/store/cart,/zh-CN/store/cart,/projects', assigned.join())
  assert(calls.length === 0, JSON.stringify(calls))
  assert(router.history.location.href === '/en-US/store', router.history.location.href)
  assigned.length = 0
  // Node's TanStack does not commit a navigation by href (test/e2e checks the browser): here only
  // the decision is checked, a URL of this language is not sent to a document load.
  await router.navigate({ href: '/en-US/store/cart' } as never)
  assert(assigned.length === 0, assigned.join())
  const zh = await client('/store', 'zh-CN')
  await zh.router.navigate({ href: '/en-US/store/cart' } as never)
  assert(zh.assigned.join() === '/en-US/store/cart', zh.assigned.join())
})

test('the home URL of a language has no trailing slash, and a trailing slash is redirected away', async () => {
  // The fixture has no area at the root. With every route in the area, the home URL is /en-US.
  const all = Object.fromEntries(Object.keys((await client('/en-US/store', 'en-US')).router.routesById).map((id) => [id, ['zh-CN', 'en-US']]))
  const home = createAppRouter(routeTree, async (): Promise<RouteResult> => ({ status: 200, data: null }), {
    history: createMemoryHistory({ initialEntries: ['/en-US/store'] }),
    rewrite: makeRewrite({ ...languages, routes: all }, 'en-US'),
  })
  assert(href(home, '/') === '/en-US', href(home, '/'))
  for (const [url, language, location] of [['/en-US/store/', 'en-US', '/en-US/store'], ['/store/', 'zh-CN', '/store'], ['/en-US/', 'en-US', '/en-US']]) {
    globalThis.renderResult = undefined
    globalThis.startRender(JSON.stringify({ url, buildId: 'b1', data: {}, language }))
    for (let i = 0; i < 1000 && globalThis.renderResult === undefined; i++) await new Promise((resolve) => setTimeout(resolve, 0))
    const result = globalThis.renderResult ?? ''
    assert(result.includes(`"location":"${location}"`), `${url}: ${result.slice(0, 200)}`)
  }
})

test('a prefix the route has no language for matches no route', async () => {
  const { router } = await client('/en-US/projects', 'en-US')
  assert(router.state.location.pathname === '/en-US/projects', router.state.location.pathname)
  for (const url of ['/en-US/projects', '/zh-CN/projects']) {
    globalThis.renderResult = undefined
    globalThis.startRender(JSON.stringify({ url, buildId: 'b1', data: {}, language: 'en-US', error: { status: 404, message: 'Not Found' } }))
    for (let i = 0; i < 1000 && globalThis.renderResult === undefined; i++) await new Promise((resolve) => setTimeout(resolve, 0))
    const result = globalThis.renderResult ?? ''
    assert(result.startsWith('<') && !result.includes('PAGE_PROJECTS'), `${url}: ${result.slice(0, 200)}`)
  }
})

test('reaches tells which URLs the document can show', async () => {
  const { router } = await client('/en-US/store', 'en-US')
  const want: Array<[string, boolean]> = [
    ['/en-US/store/cart?x=1', true],
    ['/store/cart', false],
    ['/zh-CN/store/cart', false],
    ['/projects', false],
    ['/en-US/projects', false],
  ]
  for (const [url, ok] of want) assert(reaches(router, url) === ok, `en-US ${url}`)
  const zh = (await client('/store', 'zh-CN')).router
  for (const [url, ok] of [['/store/cart', true], ['/projects', true], ['/zh-CN/store', true], ['/en-US/store', false]] as Array<[string, boolean]>)
    assert(reaches(zh, url) === ok, `zh-CN ${url}`)
})

class Node {
  constructor(readonly tag: string, readonly attributes: Record<string, string> = {}, readonly parent: Node | null = null) {}
  getAttribute(name: string) { return Object.hasOwn(this.attributes, name) ? this.attributes[name] : null }
  hasAttribute(name: string) { return Object.hasOwn(this.attributes, name) }
  closest(): Node | null {
    for (let node: Node | null = this; node !== null; node = node.parent) if (node.tag === 'a' && node.hasAttribute('href')) return node
    return null
  }
  contains(other: Node) {
    for (let node: Node | null = other; node !== null; node = node.parent) if (node === this) return true
    return false
  }
}
;(globalThis as { Element?: unknown }).Element = Node
const plain: Click = { button: 0, defaultPrevented: false, ctrlKey: false, metaKey: false, shiftKey: false, altKey: false }

test('a link inside <HTML> is followed with its prefix, and only in the language of the page', async () => {
  const ask = (router: any, link: string) => {
    ;(globalThis as any).location = new URL('http://shop.test' + (router.state.location.publicHref ?? '/'))
    ;(globalThis as any).document = { baseURI: 'http://shop.test/' }
    const container = new Node('article')
    const a = new Node('a', { href: link }, container)
    return routerHref(router, container as unknown as Element, a as unknown as EventTarget, plain)
  }
  const en = (await client('/en-US/store', 'en-US')).router
  assert(ask(en, '/en-US/store/cart?x=1#a') === '/en-US/store/cart?x=1#a', 'own prefix')
  for (const link of ['/store/cart', '/zh-CN/store/cart', '/projects', '/en-US/projects'])
    assert(ask(en, link) === null, `en-US ${link}`)
  const zh = (await client('/store', 'zh-CN')).router
  assert(ask(zh, '/store/cart') === '/store/cart' && ask(zh, '/projects') === '/projects', 'zh-CN')
  assert(ask(zh, '/en-US/store/cart') === null, 'zh-CN to en-US')
})

test('a handler function calls the URL with the prefix of the page', async () => {
  const { router, assigned } = await client('/en-US/store', 'en-US')
  const requests: string[] = []
  ;(globalThis as any).document = {}
  globalThis.fetch = (async (url: string) => {
    requests.push(url)
    return Response.json({ redirect: '/projects', status: 303 }, { headers: { 'X-Tifa-Redirect': '303' } })
  }) as typeof fetch
  setRouter(router as never, () => 'b1')
  await POST()
  assert(requests.join() === '/en-US/store/mine', requests.join())
  // The redirect goes to a URL of the default language: a document load.
  assert(assigned.join() === 'http://shop.test/projects', assigned.join())
  ;(globalThis as any).window.location.pathname = '/store'
  requests.length = 0
  await POST()
  assert(requests.join() === '/store/mine', requests.join())
})

// One VM renders one language after another: the prefix of a render is never the one before.
async function render(url: string, language: string): Promise<string> {
  globalThis.renderResult = undefined
  const data = { 'app/store/route': { status: 200, text: 0 }, 'app/projects/route': { status: 200, text: 0 } }
  globalThis.startRender(JSON.stringify({ url, buildId: 'b1', data, language }), '"x"')
  for (let i = 0; i < 1000 && globalThis.renderResult === undefined; i++) await new Promise((resolve) => setTimeout(resolve, 0))
  const result = globalThis.renderResult ?? ''
  assert(result.startsWith('<'), `${url}: ${result.slice(0, 300)}`)
  return result
}

test('one VM renders zh-CN, en-US and zh-CN in a row', async () => {
  const runs: Array<[string, string, string]> = [
    ['/store', 'zh-CN', 'href="/store/cart"'],
    ['/en-US/store', 'en-US', 'href="/en-US/store/cart"'],
    ['/store', 'zh-CN', 'href="/store/cart"'],
    ['/en-US/store?x=1', 'en-US', 'href="/en-US/store/cart"'],
  ]
  for (const [url, language, want] of runs) {
    const html = await render(url, language)
    assert(html.includes('PAGE_STORE') && !html.includes('Page not found'), `${url}: not the store page`)
    assert(html.includes(want) && html.includes('href="/projects"'), `${url} lacks ${want}:\n${html}`)
    assert(html.includes(`"language":"${language}"`) || html.includes(language), `${url}: no language`)
  }
  const projects = await render('/projects', 'zh-CN')
  assert(projects.includes('PAGE_PROJECTS') && projects.includes('href="/store/cart"'), 'projects')
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
