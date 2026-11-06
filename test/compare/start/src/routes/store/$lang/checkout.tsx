import { notFoundWith } from '../../../compare/not-found'
import { createFileRoute } from '@tanstack/react-router'
import CheckoutPage, { head } from '@/app/store/lang+/checkout/page'
import { Error } from '@/app/store/lang+/layout'
import { pick } from '../../../compare/search'
import { checkoutFn } from '../../../compare/store-fns'
import { placeOrder, refusing } from '../../../compare/store-handlers'

// The checkout. Its POST places the order and redirects to the confirmation, as the handler of
// app/store/lang+/checkout/route.go does.
export const Route = createFileRoute('/store/$lang/checkout')({
  component: CheckoutPage,
  head,
  errorComponent: Error,
  notFoundComponent: notFoundWith(Error),
  wrapInSuspense: false,
  loaderDeps: ({ search }) => pick(search, ['placed']),
  loader: ({ params, deps }) => checkoutFn({ data: { lang: params.lang, query: deps } }),
  remountDeps: ({ params }) => [params.lang],
  server: {
    handlers: {
      POST: ({ request, params }) => placeOrder(request, params),
      ...refusing('GET, HEAD, POST', 'PUT', 'PATCH', 'DELETE'),
    },
  },
})