import { callCheckout } from '../../../domain/store/calls.ts'
import { handlers } from '../compare/store-wire'

// POST of app/store/checkout: places the order and redirects to its confirmation.
const route = handlers({ POST: callCheckout }, true)
export const loader = route.loader
export const action = route.action
