import { notFoundWith } from '../../../compare/not-found'
import { createFileRoute } from '@tanstack/react-router'
import CartPage, { head } from '@/app/store/lang+/cart/page'
import { Error } from '@/app/store/lang+/layout'
import { cartFn } from '../../../compare/store-fns'
import { applyCoupon, changeQuantity, refusing } from '../../../compare/store-handlers'

// The cart. PATCH changes the quantity of a line and POST puts on a coupon, as the handlers of
// app/store/lang+/cart/route.go do.
export const Route = createFileRoute('/store/$lang/cart')({
  component: CartPage,
  head,
  errorComponent: Error,
  notFoundComponent: notFoundWith(Error),
  wrapInSuspense: false,
  loader: ({ params }) => cartFn({ data: params.lang }),
  remountDeps: ({ params }) => [params.lang],
  server: {
    handlers: {
      PATCH: ({ request, params }) => changeQuantity(request, params),
      POST: ({ request, params }) => applyCoupon(request, params),
      ...refusing('GET, HEAD, POST, PATCH', 'PUT', 'DELETE'),
    },
  },
})