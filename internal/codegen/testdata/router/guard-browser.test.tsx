// Run by TestTanStackMembersStay in Node, bundled with the browser build of TanStack, where the
// matches are a store one can subscribe to (on the server they are a plain value).
import { createMemoryHistory } from '@tanstack/react-router'
import { routeTree } from './.tifa/routes'
import { createAppRouter } from './.tifa/runtime/router'

const g = globalThis as any
Object.assign(g, {
  window: g,
  self: g,
  addEventListener() {},
  removeEventListener() {},
  scrollTo() {},
  history: { scrollRestoration: 'auto' },
  sessionStorage: { getItem: () => null, setItem() {}, removeItem() {} },
  location: { href: 'http://tifa.test/', origin: 'http://tifa.test' },
  document: { addEventListener() {}, removeEventListener() {}, querySelector: () => null, getElementById: () => null },
})
const router: any = createAppRouter(routeTree, async (_u, id) => ({ status: 200, data: id }), {
  history: createMemoryHistory({ initialEntries: ['/hello'] }),
})
if (typeof router.stores.matches.subscribe !== 'function' || typeof router.stores.matches.get !== 'function') {
  throw new Error(
    'TanStack Router changed under tifa:\n- router.stores.matches.subscribe is gone from the browser build. ' +
      'Breaks: the head and the attributes of <html> do not follow navigations and invalidations. Used in: js/runtime/head.ts (followHead), browser.tsx',
  )
}
console.log('ok   the matches of a browser router can be subscribed to')
