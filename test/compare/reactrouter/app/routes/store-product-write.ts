import { postProductRoute } from '../../../domain/store/index.ts'
import { handlers } from '../compare/store-wire'

// POST of app/store/lang+/products/product+: puts the product into the cart.
const route = handlers({
  POST: (request, params) => postProductRoute(request, params as { lang: string; product: string }),
}, true)
export const loader = route.loader
export const action = route.action
