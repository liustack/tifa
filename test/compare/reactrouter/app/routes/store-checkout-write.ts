import { postCheckoutRoute } from '../../../domain/store/index.ts'
import { handlers } from '../compare/store-wire'

// POST of app/store/lang+/checkout: places the order and redirects to its confirmation.
const route = handlers({ POST: postCheckoutRoute })
export const loader = route.loader
export const action = route.action
