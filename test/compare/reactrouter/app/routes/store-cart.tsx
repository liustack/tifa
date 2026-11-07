import type { MetaArgs } from 'react-router'
import CartPage, { head } from '@/app/store/cart/page'
import { Error } from '@/app/store/layout_'
import { storeBoundary } from '../compare/store-boundary'
import { loadCart } from '../compare/store-load'
import { storeMeta } from '../compare/store-meta'

export const loader = loadCart
export const meta = (args: MetaArgs) =>
  storeMeta(args, head({ loaderData: args.loaderData, params: args.params, matches: args.matches } as never))
export const ErrorBoundary = storeBoundary(Error)
export default CartPage
