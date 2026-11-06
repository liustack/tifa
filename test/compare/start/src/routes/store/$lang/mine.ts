import { createFileRoute } from '@tanstack/react-router'
import { markSeen, mine } from '../../../compare/store-handlers'

// What the storefront knows about the visitor: the cart and the products looked at last. GET
// answers with both, POST remembers a product, as app/store/lang+/mine/route.go does.
export const Route = createFileRoute('/store/$lang/mine')({
  server: {
    handlers: {
      GET: ({ request, params }) => mine(request, params.lang),
      POST: ({ request, params }) => markSeen(request, params.lang),
    },
  },
})
