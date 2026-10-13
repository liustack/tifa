import { createRoot, hydrateRoot } from 'react-dom/client'
import { RouterProvider, type AnyRoute } from '@tanstack/react-router'
import { RouterClient } from '@tanstack/react-router/ssr/client'
import { createAppRouter } from './router'
import { bootElementId, collector, injected, sameURL, type Boot, type Source } from './data'

// hydrate takes over the document the server rendered. A page rendered without SSR carries
// its data instead, and the browser renders it from that data without a request.
export function hydrate(routeTree: AnyRoute) {
  const boot = document.getElementById(bootElementId)
  if (!boot) {
    let buildId = ''
    const router = createAppRouter(routeTree, collector(() => buildId), {
      onHydrate: (id) => (buildId = id),
    })
    hydrateRoot(document, <RouterClient router={router} />)
    return
  }
  const input: Boot = JSON.parse(boot.textContent ?? '')
  const live = collector(() => input.buildId)
  const local = injected(input)
  let first = true
  // The first load reads the injected data. Later loads, invalidations included, ask the server.
  const source: Source = (url, id) =>
    first && id in input.data && sameURL(url, input.url) ? local(url, id) : live(url, id)
  const router = createAppRouter(routeTree, source, { buildId: input.buildId, assets: input.assets })
  void router.load().then(() => {
    first = false
    createRoot(document).render(<RouterProvider router={router} />)
  })
}
