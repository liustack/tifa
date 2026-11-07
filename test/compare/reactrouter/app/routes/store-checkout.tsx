import type { MetaArgs } from 'react-router'
import CheckoutPage, { head } from '@/app/store/checkout/page'
import { Error } from '@/app/store/layout_'
import { storeBoundary } from '../compare/store-boundary'
import { loadCheckout } from '../compare/store-load'
import { storeMeta } from '../compare/store-meta'

export const loader = loadCheckout
export const meta = (args: MetaArgs) =>
  storeMeta(args, head({ loaderData: args.loaderData, params: args.params, matches: args.matches } as never))
export const ErrorBoundary = storeBoundary(Error)
export default CheckoutPage
