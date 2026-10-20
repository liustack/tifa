import { createRoot, hydrateRoot } from 'react-dom/client'
import { Await, RouterProvider, type AnyRoute, type AnyRouter } from '@tanstack/react-router'
import { hydrate as hydrateRouter } from '@tanstack/react-router/ssr/client'
import { createAppRouter } from './router'
import { bootElementId, collector, injected, rootElementId, sameURL, type Boot, type Source } from './data'
import { defaultHTMLAttributes, followHead, syncHead, syncHTMLAttributes, type HTMLAttributes } from './head'
import { setRouter } from './call'

// hydrate takes over the application root the server rendered. A page rendered without SSR
// carries its data instead, and the browser renders it from that data without a request. Either
// way the head and the attributes of <html> follow the matched routes from then on.
export function hydrate(routeTree: AnyRoute, htmlAttributes: HTMLAttributes = defaultHTMLAttributes) {
  const root = document.getElementById(rootElementId)
  if (!root) throw new Error(`the page has no element #${rootElementId} to render into`)
  const boot = document.getElementById(bootElementId)
  if (!boot) {
    let buildId = ''
    const router = createAppRouter(routeTree, collector(() => buildId), {
      onHydrate: (id) => (buildId = id),
    })
    setRouter(router)
    const hydrated = takeOver(router, htmlAttributes)
    // What TanStack's RouterClient renders, with the promise of takeOver in place of its own.
    hydrateRoot(root, <Await promise={hydrated}>{() => <RouterProvider router={router} />}</Await>)
    return
  }
  // Go writes the document of a page without SSR, the attributes of <html> come from here: those
  // of the root Layout at once, and those of the routes once they have loaded.
  syncHTMLAttributes([], htmlAttributes)
  const input: Boot = JSON.parse(boot.textContent ?? '')
  const live = collector(() => input.buildId)
  const local = injected(input)
  let first = true
  // The first load reads the injected data. Later loads, invalidations included, ask the server.
  const source: Source = (url, id) =>
    first && id in input.data && sameURL(url, input.url) ? local(url, id) : live(url, id)
  const router = createAppRouter(routeTree, source, { buildId: input.buildId })
  void router.load().then(() => {
    first = false
    followHead(router, htmlAttributes)
    setRouter(router)
    syncHead(router.stores.matches.get())
    syncHTMLAttributes(router.stores.matches.get(), htmlAttributes)
    createRoot(root).render(<RouterProvider router={router} />)
  })
}

// takeOver hydrates the router from the scripts of the page the server rendered, as TanStack's
// RouterClient does, then has the head and the attributes of <html> follow its matches. It first
// syncs them with the matches the server rendered. That changes nothing on the page, but notes
// which attributes of <html> the server wrote, so that the first navigation takes away those the
// next page does not give. It does not wait for the store of the matches to report the hydration.
export function takeOver(router: AnyRouter, htmlAttributes: HTMLAttributes): Promise<void> {
  return hydrateRouter(router)
    .finally(() => window.$_TSR!.h())
    .then(() => {
      const matches = router.stores.matches.get()
      syncHead(matches)
      syncHTMLAttributes(matches, htmlAttributes)
      followHead(router, htmlAttributes)
    })
}
