import { storeBoundary } from '../compare/store-boundary'
import { storeMeta } from '../compare/store-meta'
import Store, { Error } from '@/app/store/layout_'
import type { MetaArgs } from 'react-router'

export const meta = (args: MetaArgs) => storeMeta(args)
export const ErrorBoundary = storeBoundary(Error)
export default Store
