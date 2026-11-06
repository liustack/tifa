import { callSeen } from '../../../domain/store/calls.ts'
import { handlers, mine } from '../compare/store-wire'

// GET and POST of app/store/mine: the cart and the products seen, and remembering one.
const route = handlers({ GET: mine, POST: callSeen })
export const loader = route.loader
export const action = route.action
