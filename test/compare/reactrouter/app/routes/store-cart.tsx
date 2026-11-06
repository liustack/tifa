import type { MetaArgs } from 'react-router'
import CartPage, { head } from '@/app/store/lang+/cart/page'
import { Error } from '@/app/store/lang+/layout'
import { storeBoundary } from '../compare/store-boundary'
import { loadCart } from '../compare/store-load'
import { storeMeta } from '../compare/store-meta'

export const loader = loadCart
export const meta = (args: MetaArgs) =>
  storeMeta(args, head({ loaderData: args.loaderData, params: args.params } as never))
export const ErrorBoundary = storeBoundary(Error)
export default CartPage
