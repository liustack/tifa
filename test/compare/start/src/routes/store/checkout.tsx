import { requestLanguage } from '../../compare/language'
import { languageOf } from '@/runtime/languages'
import { notFoundWith } from '../../compare/not-found'
import { createFileRoute } from '@tanstack/react-router'
import CheckoutPage, { head } from '@/app/store/checkout/page'
import { Error } from '@/app/store/layout_'
import { pick } from '../../compare/search'
import { checkoutFn } from '../../compare/store-fns'
import { placeOrder, refusing } from '../../compare/store-handlers'

// The checkout. Its POST places the order and redirects to the confirmation, as the handler of
// app/store/checkout/route.go does.
export const Route = createFileRoute('/store/checkout')({
  component: CheckoutPage,
  head,
  errorComponent: Error,
  notFoundComponent: notFoundWith(Error),
  wrapInSuspense: false,
  loaderDeps: ({ search }) => pick(search, ['placed']),
  loader: ({ params, deps, location }) => checkoutFn({ data: { lang: languageOf(location.publicHref), query: deps } }),
  server: {
    handlers: {
      POST: ({ request, params }) => placeOrder(request, { lang: requestLanguage(request) }),
      ...refusing('GET, HEAD, POST', 'PUT', 'PATCH', 'DELETE'),
    },
  },
})