import { createRouter } from '@tanstack/react-router'
import { inLanguage, languageOf, withoutLanguage } from '@/runtime/languages'
import { parseSearch, stringifySearch } from './compare/search'
import { routeTree } from './routeTree.gen'
import { setRouter } from '@/runtime/call'

// The storefront is at /store, and in English at /en-US/store. The router matches the path without
// the prefix, as tifa does, and writes the prefix of the language of the page on the links to the
// storefront.
export function getRouter() {
  let located: { latestLocation?: { publicHref: string } } | undefined
  const router = createRouter({
    rewrite: {
      input: ({ url }) => {
        url.pathname = withoutLanguage(url.pathname)
        return url
      },
      output: ({ url }) => {
        url.pathname = inLanguage(url.pathname, languageOf(located?.latestLocation?.publicHref ?? '/'))
        return url
      },
    },
    routeTree,
    defaultPreload: 'intent',
    scrollRestoration: true,
    parseSearch,
    stringifySearch,
  })
  located = router
  // The handler functions of the storefront follow a redirect through the router.
  if (typeof document !== 'undefined') setRouter(router)
  return router
}
