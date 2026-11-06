import type { MetaArgs } from 'react-router'
import ProductPage, { head } from '@/app/store/lang+/products/product+/page'
import { Error } from '@/app/store/lang+/layout'
import { storeBoundary } from '../compare/store-boundary'
import { loadProduct } from '../compare/store-load'
import { storeMeta } from '../compare/store-meta'

export const loader = loadProduct
export const meta = (args: MetaArgs) =>
  storeMeta(args, head({ loaderData: args.loaderData, params: args.params } as never))
export const ErrorBoundary = storeBoundary(Error)
export default ProductPage
