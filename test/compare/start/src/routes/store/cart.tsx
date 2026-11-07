import { requestLanguage } from '../../compare/language'
import { languageOf } from '@/runtime/languages'
import { notFoundWith } from '../../compare/not-found'
import { createFileRoute } from '@tanstack/react-router'
import CartPage, { head } from '@/app/store/cart/page'
import { Error } from '@/app/store/layout_'
import { cartFn } from '../../compare/store-fns'
import { applyCoupon, changeQuantity, refusing } from '../../compare/store-handlers'

// The cart. PATCH changes the quantity of a line and POST puts on a coupon, as the handlers of
// app/store/cart/route.go do.
export const Route = createFileRoute('/store/cart')({
  component: CartPage,
  head,
  errorComponent: Error,
  notFoundComponent: notFoundWith(Error),
  wrapInSuspense: false,
  loader: ({ location }) => cartFn({ data: languageOf(location.publicHref) }),
  server: {
    handlers: {
      PATCH: ({ request, params }) => changeQuantity(request, { lang: requestLanguage(request) }),
      POST: ({ request, params }) => applyCoupon(request, { lang: requestLanguage(request) }),
      ...refusing('GET, HEAD, POST, PATCH', 'PUT', 'DELETE'),
    },
  },
})