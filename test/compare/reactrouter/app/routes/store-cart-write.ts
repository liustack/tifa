import { patchCartRoute, postCartRoute } from '../../../domain/store/index.ts'
import { handlers } from '../compare/store-wire'

// PATCH (a quantity) and POST (a coupon) of app/store/lang+/cart.
const route = handlers({ POST: postCartRoute, PATCH: patchCartRoute }, true)
export const loader = route.loader
export const action = route.action
