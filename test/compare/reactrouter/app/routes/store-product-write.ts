import { callProduct } from '../../../domain/store/calls.ts'
import { handlers } from '../compare/store-wire'

// POST of app/store/products/product+: puts the product into the cart.
const route = handlers({ POST: callProduct }, true)
export const loader = route.loader
export const action = route.action
