import { createFileRoute } from '@tanstack/react-router'
import ProductPage, { head } from '@/app/store/products/product+/page'
import { Error } from '@/app/store/layout_'
import { notFoundWith } from '../../../../compare/not-found'
import { pick } from '../../../../compare/search'
import { productFn } from '../../../../compare/store-fns'
import { addToCart, refusing } from '../../../../compare/store-handlers'

// The page of a product. Its POST puts the product into the cart, as the handler of
// app/store/products/product+/route.go does.
export const Route = createFileRoute('/store/$lang/products/$product')({
  component: ProductPage,
  // A function of its own that names the params of the route: the shop's head declares only the language, and that would hide the others from the types of the route.
  head: (match: Parameters<typeof head>[0] & { params: { lang: string; product: string } }) => head(match),
  errorComponent: Error,
  notFoundComponent: notFoundWith(Error),
  wrapInSuspense: false,
  loaderDeps: ({ search }) => pick(search, ['reviews']),
  loader: ({ params, deps }) => productFn({ data: { lang: params.lang, product: params.product, query: deps } }),
  remountDeps: ({ params }) => [params.lang, params.product],
  server: {
    handlers: {
      POST: ({ request, params }) => addToCart(request, params),
      ...refusing('GET, HEAD, POST', 'PUT', 'PATCH', 'DELETE'),
    },
  },
})
