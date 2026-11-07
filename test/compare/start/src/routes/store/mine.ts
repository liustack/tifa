import { requestLanguage } from '../../compare/language'
import { createFileRoute } from '@tanstack/react-router'
import { markSeen, mine, refusing } from '../../compare/store-handlers'

// What the storefront knows about the visitor: the cart and the products looked at last. GET
// answers with both, POST remembers a product, as app/store/mine/route.go does.
export const Route = createFileRoute('/store/mine')({
  server: {
    handlers: {
      GET: ({ request, params }) => mine(request, { lang: requestLanguage(request) }),
      POST: ({ request, params }) => markSeen(request, { lang: requestLanguage(request) }),
      ...refusing('GET, HEAD, POST', 'PUT', 'PATCH', 'DELETE'),
    },
  },
})
