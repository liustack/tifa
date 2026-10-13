import { hydrateRoot } from 'react-dom/client'
import type { AnyRoute } from '@tanstack/react-router'
import { RouterClient } from '@tanstack/react-router/ssr/client'
import { createAppRouter } from './router'
import { collector } from './data'

// hydrate takes over the document that the server rendered.
export function hydrate(routeTree: AnyRoute) {
  let buildId = ''
  const router = createAppRouter(
    routeTree,
    collector(() => buildId),
    { onHydrate: (id) => (buildId = id) },
  )
  hydrateRoot(document, <RouterClient router={router} />)
}
