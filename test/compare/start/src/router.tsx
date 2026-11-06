import { createRouter } from '@tanstack/react-router'
import { parseSearch, stringifySearch } from './compare/search'
import { routeTree } from './routeTree.gen'
import { setRouter } from '@/runtime/call'

export function getRouter() {
  const router = createRouter({
    routeTree,
    defaultPreload: 'intent',
    scrollRestoration: true,
    parseSearch,
    stringifySearch,
  })
  // The handler functions of the storefront follow a redirect through the router.
  if (typeof document !== 'undefined') setRouter(router)
  return router
}
