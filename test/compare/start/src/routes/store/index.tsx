import { createFileRoute } from '@tanstack/react-router'
import { redirectToPreferred } from '../../compare/store-handlers'

// GET /store sends a visitor to the storefront in the language their browser prefers, as the GET
// handler of app/store/route.go does.
export const Route = createFileRoute('/store/')({
  server: {
    handlers: {
      GET: ({ request }) => redirectToPreferred(request),
    },
  },
})
