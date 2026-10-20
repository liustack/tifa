import { createRoot, hydrateRoot } from 'react-dom/client'
import { RouterProvider, type AnyRoute } from '@tanstack/react-router'
import { RouterClient } from '@tanstack/react-router/ssr/client'
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
    followHead(router, htmlAttributes)
    setRouter(router)
    hydrateRoot(root, <RouterClient router={router} />)
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
