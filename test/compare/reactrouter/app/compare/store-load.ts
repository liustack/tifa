// What the loaders of the storefront call. Each one runs the ported Go function for every request,
// as tifa runs the Layout and Load functions: nothing is kept between requests and nothing is
// shared between the loaders of one request (React Router runs them in parallel, and the Go
// functions of the storefront do not use each other's results).

import { data } from 'react-router'
import { Ctx, NotFound, loadRoute } from '../../../domain/store/index.ts'
import { languageOf } from '@/runtime/languages'

// loader makes the loader of the route with a Tifa route id. A page that has no data answers 404,
// as tifa does for tifa.ErrNotFound.
const loader =
  (id: string) =>
  ({ request, params }: { request: Request; params: Record<string, string | undefined> }) => {
    try {
      return loadRoute(id, new Ctx(request.headers.get('cookie')), { ...params, lang: languageOf(new URL(request.url).pathname) }, new URL(request.url).searchParams) as never
    } catch (e) {
      if (e instanceof NotFound) throw data(null, { status: 404 })
      throw e
    }
  }

export const loadLayout = loader('app/store/layout')
export const loadHome = loader('app/store/route')
export const loadProducts = loader('app/store/products/route')
export const loadProduct = loader('app/store/products/product+/route')
export const loadCart = loader('app/store/cart/route')
export const loadCheckout = loader('app/store/checkout/route')
export const loadJournal = loader('app/store/journal/route')
export const loadPost = loader('app/store/journal/post+/route')

// loadUnknown is the loader of the splat: a URL of the storefront that no page has.
export const loadUnknown = () => {
  throw data(null, { status: 404 })
}

