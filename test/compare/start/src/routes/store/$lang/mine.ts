import { createFileRoute } from '@tanstack/react-router'
import { markSeen, mine, refusing } from '../../../compare/store-handlers'

// What the storefront knows about the visitor: the cart and the products looked at last. GET
// answers with both, POST remembers a product, as app/store/mine/route.go does.
export const Route = createFileRoute('/store/$lang/mine')({
  server: {
    handlers: {
      GET: ({ request, params }) => mine(request, params),
      POST: ({ request, params }) => markSeen(request, params),
      ...refusing('GET, HEAD, POST', 'PUT', 'PATCH', 'DELETE'),
    },
  },
})
