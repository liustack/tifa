import type { MetaArgs } from 'react-router'
import StoreHome from '@/app/store/page'
import { Error } from '@/app/store/layout_'
import { storeBoundary } from '../compare/store-boundary'
import { loadHome } from '../compare/store-load'
import { storeMeta } from '../compare/store-meta'

export const loader = loadHome
export const meta = (args: MetaArgs) => storeMeta(args)
export const ErrorBoundary = storeBoundary(Error)
export default StoreHome
