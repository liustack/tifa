import { callQuantity, callCoupon } from '../../../domain/store/calls.ts'
import { handlers } from '../compare/store-wire'

// PATCH (a quantity) and POST (a coupon) of app/store/cart.
const route = handlers({ POST: callCoupon, PATCH: callQuantity }, true)
export const loader = route.loader
export const action = route.action
