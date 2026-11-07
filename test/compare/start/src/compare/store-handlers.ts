// The writes of the storefront as server routes on the URLs of the Tifa handlers. They answer as
// Tifa does, to a plain request and to a call from the browser: the answers are made by
// test/compare/domain/store/calls.ts, shared by the three apps. This module only names them for
// the routes.

import { methodNotAllowed } from '../../../domain/store/calls.ts'

export {
  callCheckout as placeOrder,
  callCoupon as applyCoupon,
  callMine as mine,
  callProduct as addToCart,
  callQuantity as changeQuantity,
  callSeen as markSeen,
} from '../../../domain/store/calls.ts'

// refusing makes the handlers that answer the methods a URL does not take as Tifa does, with a 405
// and the Allow header. A route of Start that has a page answers any method it has no handler for
// with the page, so each is named.
type Refused = Partial<Record<'POST' | 'PUT' | 'PATCH' | 'DELETE', () => Response>>

export const refusing = (allow: string, ...methods: Array<'POST' | 'PUT' | 'PATCH' | 'DELETE'>): Refused =>
  Object.fromEntries(methods.map((m) => [m, () => methodNotAllowed(allow)]))
