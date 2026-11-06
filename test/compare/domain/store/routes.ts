// The storefront's routes by their ids, as tifa names them, each with the function that runs
// its Layout or Load. A framework that wants to treat the pages alike can call loadRoute; the
// ones that wire each page by hand call the function in the table directly.
import { Ctx } from './ctx.ts'
import { loadCart } from './cart/route.ts'
import { loadCheckout } from './checkout.ts'
import { layoutLang, loadHome } from './home.ts'
import { loadJournal, loadPost } from './journal/route.ts'
import { getMine } from './mine.ts'
import { loadProduct } from './product.ts'
import { loadList } from './products/index.ts'

export interface Params {
  lang: string
  product?: string
  post?: string
}

// loadRoute runs the Layout or Load of a route for a request: its cookies, its path parameters
// and its query string. It throws NotFound, as Go returns tifa.ErrNotFound.
export function loadRoute(id: string, c: Ctx, params: Params, query: URLSearchParams, now = Date.now()): unknown {
  switch (id) {
    case 'app/store/layout':
      return layoutLang(params)
    case 'app/store/route':
      return loadHome(params)
    case 'app/store/products/route':
      return loadList(params, query)
    case 'app/store/products/product+/route':
      return loadProduct({ lang: params.lang, product: params.product! }, query, now)
    case 'app/store/cart/route':
      return loadCart(c, params, now)
    case 'app/store/checkout/route':
      return loadCheckout(c, params, query, now)
    case 'app/store/journal/route':
      return loadJournal(params)
    case 'app/store/journal/post+/route':
      return loadPost({ lang: params.lang, post: params.post! })
    case 'app/store/mine/route':
      return getMine(c, params)
  }
  throw new Error('unknown route ' + id)
}
