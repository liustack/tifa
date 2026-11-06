import { createFileRoute } from '@tanstack/react-router'
import ProductPage, { head } from '@/app/store/lang+/products/product+/page'
import { Error } from '@/app/store/lang+/layout'
import { notFoundWith } from '../../../../compare/not-found'
import { pick } from '../../../../compare/search'
import { productFn } from '../../../../compare/store-fns'
import { addToCart } from '../../../../compare/store-handlers'

// The page of a product. Its POST puts the product into the cart, as the handler of
// app/store/lang+/products/product+/route.go does.
export const Route = createFileRoute('/store/$lang/products/$product')({
  component: ProductPage,
  head,
  errorComponent: Error,
  notFoundComponent: notFoundWith(Error),
  wrapInSuspense: false,
  loaderDeps: ({ search }) => pick(search, ['reviews']),
  loader: ({ params, deps }) => productFn({ data: { lang: params.lang, product: params.product, query: deps } }),
  remountDeps: ({ params }) => [params.lang, params.product],
  server: {
    handlers: {
      POST: ({ request, params }) => addToCart(request, params.product, params.lang),
    },
  },
})
