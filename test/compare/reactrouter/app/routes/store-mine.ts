import { postMineRoute } from '../../../domain/store/index.ts'
import { handlers, mine } from '../compare/store-wire'

// GET and POST of app/store/lang+/mine: the cart and the products seen, and remembering one.
const route = handlers({ GET: mine, POST: postMineRoute })
export const loader = route.loader
export const action = route.action
